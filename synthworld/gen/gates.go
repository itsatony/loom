package gen

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// GateStats are the dataset-quality measurements the batch protocol gates on.
// Computed in-process from the built query set and DatasetStats — the same
// numbers land in manifest.json, so a gated dataset can be re-audited from
// its files alone.
type GateStats struct {
	CompositionPositives int `json:"composition_positives"` // composition holds queries with answer true
	RevisionFlips        int `json:"revision_flips"`        // revision queries where stale != answer
	RevisionRetained     int `json:"revision_retained"`     // revision queries where stale == answer
	Depth2PlusAtoms      int `json:"depth2_plus_atoms"`     // closure atoms at derivation depth >= 2
	OverFiringRelations  int `json:"over_firing_relations"` // firing ratio > 0.5
	DerivedRelations     int `json:"derived_relations"`     // total derived relations
}

// GateThresholds are the pre-registered acceptance gates (MASTERPLAN E0.6).
// A seed passes iff every gate holds. Changing thresholds after the seed
// list is locked invalidates the protocol.
type GateThresholds struct {
	MinCompositionPositives int     `json:"min_composition_positives"`
	MinRevisionFlips        int     `json:"min_revision_flips"`
	MinRevisionRetained     int     `json:"min_revision_retained"`
	MinDepth2PlusAtoms      int     `json:"min_depth2_plus_atoms"`
	MaxOverFiringFraction   float64 `json:"max_over_firing_fraction"` // reject if over-firing/derived > this
}

// DefaultGateThresholds returns the E0.6 protocol gates: composition holds
// positives >= 30, revision flips >= 20 AND retained >= 20, closure depth
// histogram has d2+ mass, over-firing relations not a majority of derived
// relations.
func DefaultGateThresholds() GateThresholds {
	return GateThresholds{
		MinCompositionPositives: 30,
		MinRevisionFlips:        20,
		MinRevisionRetained:     20,
		MinDepth2PlusAtoms:      1,
		MaxOverFiringFraction:   0.5,
	}
}

// ComputeGateStats derives GateStats from a built query set and the
// builder's DatasetStats (as stored in the manifest's quality section).
func ComputeGateStats(qs *QuerySet, stats DatasetStats) GateStats {
	gs := GateStats{
		OverFiringRelations: len(stats.OverFiringRelations),
		DerivedRelations:    len(stats.FiringRatios),
	}
	for _, q := range qs.Queries {
		switch {
		case q.Slice == "composition" && q.Type == "holds" && q.Answer != nil && *q.Answer:
			gs.CompositionPositives++
		case q.Slice == "revision" && q.Type == "holds" && q.Answer != nil && q.StaleAnswer != nil:
			if *q.Answer != *q.StaleAnswer {
				gs.RevisionFlips++
			} else {
				gs.RevisionRetained++
			}
		}
	}
	for key, n := range stats.ClosureDepthCounts {
		d, err := strconv.Atoi(strings.TrimPrefix(key, "d"))
		if err == nil && d >= 2 {
			gs.Depth2PlusAtoms += n
		}
	}
	return gs
}

// FramesGateStats are the frames-edition dataset-quality measurements
// (MASTERPLAN §9.6.8): gap traps, scenario-composition chains, scenario
// presence, per-frame firing hygiene. Frames seeds are gated on ALL v0
// gates PLUS these.
type FramesGateStats struct {
	GapTrapQueries        int      `json:"gap_trap_queries"`
	ContraTrapQueries     int      `json:"contra_trap_queries"`
	SpeechTrapQueries     int      `json:"speech_trap_queries"`
	ScenarioChainQueries  int      `json:"scenario_chain_queries"`
	PinnedScenarios       int      `json:"pinned_scenarios"`
	LiveScenarios         int      `json:"live_scenarios"`
	MajorityOverFiring    []string `json:"majority_over_firing_frames"` // frames where >50% of derived relations over-fire
	MisattributionQueries int      `json:"misattribution_queries"`
	PromotionQueries      int      `json:"promotion_queries"`
	IdeationQueries       int      `json:"ideation_queries"`
}

// FramesGateThresholds are the pre-registered §9.6.8 acceptance gates.
type FramesGateThresholds struct {
	MinGapTraps       int `json:"min_gap_traps"`
	MinScenarioChains int `json:"min_scenario_chains"`
	MinPinned         int `json:"min_pinned"`
	MinLive           int `json:"min_live"`
}

// DefaultFramesGateThresholds returns the §9.6.8 gates: >= 15 gap-trap
// queries, >= 10 scenario-composition chains, >= 1 pinned + >= 1 live
// scenario, and no frame where a majority of rules over-fire.
func DefaultFramesGateThresholds() FramesGateThresholds {
	return FramesGateThresholds{MinGapTraps: 15, MinScenarioChains: 10, MinPinned: 1, MinLive: 1}
}

// ComputeFramesGateStats derives FramesGateStats from a built query set and
// the builder's FramesStats (the same numbers land in the manifest).
func ComputeFramesGateStats(qs *QuerySet, fs FramesStats) FramesGateStats {
	gs := FramesGateStats{}
	for _, q := range qs.Queries {
		switch {
		case q.Slice == "contamination" && q.Subpop == "gap":
			gs.GapTrapQueries++
		case q.Slice == "contamination" && q.Subpop == "contradiction":
			gs.ContraTrapQueries++
		case q.Slice == "contamination" && (q.Subpop == "sarcasm" || q.Subpop == "quote"):
			gs.SpeechTrapQueries++
		case q.Slice == "isolation" && q.Subpop == "chain":
			gs.ScenarioChainQueries++
		case q.Slice == "misattribution":
			gs.MisattributionQueries++
		case q.Slice == "promotion":
			gs.PromotionQueries++
		case q.Slice == "ideation":
			gs.IdeationQueries++
		}
	}
	var frames []string
	for f := range fs.PerFrameFiring {
		frames = append(frames, f)
	}
	sort.Strings(frames)
	gs.PinnedScenarios = fs.PinnedScenarios
	gs.LiveScenarios = fs.LiveScenarios
	for _, f := range frames {
		ratios := fs.PerFrameFiring[f]
		over, total := 0, 0
		for _, r := range ratios {
			total++
			if r > 0.5 {
				over++
			}
		}
		if total > 0 && float64(over)/float64(total) > 0.5 {
			gs.MajorityOverFiring = append(gs.MajorityOverFiring, f)
		}
	}
	return gs
}

// EvaluateFramesGates returns the sorted list of frames-gate violations
// (empty = pass). Scenario presence (pinned/live counts) arrives via
// FramesGateStats, computed from the builder's FramesStats.
func EvaluateFramesGates(gs FramesGateStats, th FramesGateThresholds) []string {
	var reasons []string
	if gs.GapTrapQueries < th.MinGapTraps {
		reasons = append(reasons, fmt.Sprintf("gap-trap queries %d < %d", gs.GapTrapQueries, th.MinGapTraps))
	}
	if gs.ScenarioChainQueries < th.MinScenarioChains {
		reasons = append(reasons, fmt.Sprintf("scenario-composition chain queries %d < %d", gs.ScenarioChainQueries, th.MinScenarioChains))
	}
	if gs.PinnedScenarios < th.MinPinned {
		reasons = append(reasons, fmt.Sprintf("pinned scenarios %d < %d", gs.PinnedScenarios, th.MinPinned))
	}
	if gs.LiveScenarios < th.MinLive {
		reasons = append(reasons, fmt.Sprintf("live scenarios %d < %d", gs.LiveScenarios, th.MinLive))
	}
	if len(gs.MajorityOverFiring) > 0 {
		reasons = append(reasons, fmt.Sprintf("frames with majority over-firing rules: %v", gs.MajorityOverFiring))
	}
	sort.Strings(reasons)
	return reasons
}

// EvaluateGates returns the sorted list of gate violations (empty = pass).
func EvaluateGates(gs GateStats, th GateThresholds) []string {
	var reasons []string
	if gs.CompositionPositives < th.MinCompositionPositives {
		reasons = append(reasons, fmt.Sprintf("composition positives %d < %d", gs.CompositionPositives, th.MinCompositionPositives))
	}
	if gs.RevisionFlips < th.MinRevisionFlips {
		reasons = append(reasons, fmt.Sprintf("revision flips %d < %d", gs.RevisionFlips, th.MinRevisionFlips))
	}
	if gs.RevisionRetained < th.MinRevisionRetained {
		reasons = append(reasons, fmt.Sprintf("revision retained %d < %d", gs.RevisionRetained, th.MinRevisionRetained))
	}
	if gs.Depth2PlusAtoms < th.MinDepth2PlusAtoms {
		reasons = append(reasons, fmt.Sprintf("closure depth histogram has %d atoms at depth >= 2, need %d", gs.Depth2PlusAtoms, th.MinDepth2PlusAtoms))
	}
	if gs.DerivedRelations > 0 {
		frac := float64(gs.OverFiringRelations) / float64(gs.DerivedRelations)
		if frac > th.MaxOverFiringFraction {
			reasons = append(reasons, fmt.Sprintf("over-firing relations %d/%d exceed fraction %.2f", gs.OverFiringRelations, gs.DerivedRelations, th.MaxOverFiringFraction))
		}
	}
	sort.Strings(reasons)
	return reasons
}
