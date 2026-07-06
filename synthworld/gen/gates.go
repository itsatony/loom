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
