// Package harness runs memory conditions (C0..C3 and diagnostics) against a
// synthworld dataset and scores them per slice. The harness is the only
// component that sees ground truth; conditions receive sanitized queries.
package harness

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/vaudience/synthworld/gen"
	"github.com/vaudience/synthworld/world"
)

// SanitizedQuery is what a condition is allowed to see: no answers, no
// traces, no provenance, no slice label, no subpopulation. Frame and
// FramesScope ARE visible — they are part of the question ("within scenario
// X, does P hold?"), not ground truth.
type SanitizedQuery struct {
	ID          string
	Type        string // holds | find | which_frames
	AtDay       int
	Atom        *world.Atom
	Pattern     *world.PatternAtom
	FindSlot    string
	Frame       string   // query frame; "" = actual
	FramesScope []string // ideation find: explicit frame set
	Text        string
}

func sanitize(q gen.Query) SanitizedQuery {
	return SanitizedQuery{
		ID: q.ID, Type: q.Type, AtDay: q.AtDay,
		Atom: q.Atom, Pattern: q.Pattern, FindSlot: q.FindSlot,
		Frame: q.Frame, FramesScope: q.FramesScope, Text: q.Text,
	}
}

// Condition is a system under test. Ingest is called once, with episodes in
// chronological order, before any query.
type Condition interface {
	Name() string
	Ingest(episodes []gen.Episode) error
	AnswerHolds(q SanitizedQuery) (bool, error)
	AnswerFind(q SanitizedQuery) ([]string, error)
}

// FrameAnswerer is the optional extension for frame-native query types
// (MASTERPLAN §9.6.4). Conditions that do not implement it are scored with
// frame-blind defaults: which_frames answers ["actual"] if the condition
// holds the atom (else empty), and ideation finds are labeled "actual" —
// the honest output of a frameless store, punished exactly where frames
// matter (misattribution, cross-frame attribution).
type FrameAnswerer interface {
	AnswerWhichFrames(q SanitizedQuery) ([]string, error)
	AnswerFindFramed(q SanitizedQuery) ([]gen.FramedValue, error)
}

// ---------- Scoring ----------

type SliceScore struct {
	PosCorrect int `json:"pos_correct"`
	PosTotal   int `json:"pos_total"`
	NegCorrect int `json:"neg_correct"`
	NegTotal   int `json:"neg_total"`
}

func (s SliceScore) Accuracy() float64 {
	t := s.PosTotal + s.NegTotal
	if t == 0 {
		return 0
	}
	return float64(s.PosCorrect+s.NegCorrect) / float64(t)
}

type FindScore struct {
	ExactMatches   int     `json:"exact_matches"`
	Total          int     `json:"total"`
	MicroTP        int     `json:"-"`
	MicroFP        int     `json:"-"`
	MicroFN        int     `json:"-"`
	MicroPrecision float64 `json:"micro_p"`
	MicroRecall    float64 `json:"micro_r"`
	F1             float64 `json:"micro_f1"`
}

type RevisionScore struct {
	FlipCorrect     int `json:"flip_correct"`
	FlipTotal       int `json:"flip_total"`
	RetainCorrect   int `json:"retain_correct"`
	RetainTotal     int `json:"retain_total"`
	StaleAgreements int `json:"stale_agreements"` // wrong flip answers matching the stale answer
}

// FrameReport scores the six frame slices (MASTERPLAN §9.6.4). Traps carry
// answer=false (negatives), paired controls answer=true (positives), so
// balanced accuracy per direction reads off SliceScore. ContaminationSub
// buckets trap+control pairs by subpopulation (gap is the F-E1 mandatory
// sub-line; sarcasm/quote form the literalist speech-act sub-slice).
type FrameReport struct {
	Contamination    SliceScore             `json:"contamination"`
	ContaminationSub map[string]*SliceScore `json:"contamination_sub,omitempty"`
	// CueSub buckets contamination trap/control pairs by cue class
	// ("content" | "metadata", from gen.Query.CueClass — the F-E2
	// partition; empty when cmd/harness had no cue table to classify with).
	CueSub         map[string]*SliceScore `json:"cue_sub,omitempty"`
	Isolation      SliceScore             `json:"isolation"`
	IsolationChain SliceScore             `json:"isolation_chain"`
	Pinning        SliceScore             `json:"pinning"`
	Promotion      SliceScore             `json:"promotion"`
	Misattribution FindScore              `json:"misattribution"` // exact frame-set + micro over frame labels
	Ideation       FindScore              `json:"ideation"`       // exact (value,frame) pair-set + micro (F-E4)
}

type Report struct {
	Condition    string              `json:"condition"`
	Repetition   SliceScore          `json:"repetition"`
	Composition  SliceScore          `json:"composition"`
	Find         FindScore           `json:"find"`
	Revision     RevisionScore       `json:"revision"`
	Frames       *FrameReport        `json:"frames,omitempty"` // nil on v0 datasets
	Errors       int                 `json:"errors"`
	UnknownSlice int                 `json:"unknown_slice,omitempty"` // queries with an unrecognized slice/type: scored nowhere, must be zero
	PerDepth     map[int]*SliceScore `json:"per_depth,omitempty"`     // composition holds, by derivation depth
	Usage        *UsageStats         `json:"usage,omitempty"`         // LLM token accounting; nil for LLM-free conditions
}

func (r *Report) frames() *FrameReport {
	if r.Frames == nil {
		r.Frames = &FrameReport{ContaminationSub: map[string]*SliceScore{}}
	}
	return r.Frames
}

// Run executes one condition over the dataset and scores it. Worker count
// comes from HARNESS_CONCURRENCY (default 1, sequential).
func Run(cond Condition, episodes []gen.Episode, queries []gen.Query) (*Report, error) {
	workers := 1
	if v := os.Getenv("HARNESS_CONCURRENCY"); v != "" {
		fmt.Sscanf(v, "%d", &workers)
	}
	return RunWorkers(cond, episodes, queries, workers)
}

// answerResult is one query's raw outcome, produced by the (possibly
// concurrent) answer phase and consumed by the sequential scoring phase.
type answerResult struct {
	holds bool
	found []string
	err   error
}

// RunWorkers executes one condition with a bounded worker pool over queries.
// The answer phase may run concurrently — conditions must tolerate
// concurrent AnswerHolds/AnswerFind calls after Ingest (the shipped LLM
// conditions are stateless post-Ingest; the caches write atomically).
// Scoring is always sequential in query order over the collected results, so
// the Report is identical for any worker count.
func RunWorkers(cond Condition, episodes []gen.Episode, queries []gen.Query, workers int) (*Report, error) {
	if err := cond.Ingest(episodes); err != nil {
		return nil, fmt.Errorf("%s ingest: %w", cond.Name(), err)
	}
	if workers < 1 {
		workers = 1
	}

	results := make([]answerResult, len(queries))
	answer := func(i int) {
		q := queries[i]
		sq := sanitize(q)
		switch q.Type {
		case "holds":
			got, err := cond.AnswerHolds(sq)
			results[i] = answerResult{holds: got, err: err}
		case "find":
			if len(q.FramesScope) > 0 {
				// ideation: (value, frame) pairs, encoded frame|value for
				// set scoring. Frame-blind conditions answer via plain find
				// with every value labeled "actual".
				if fa, ok := cond.(FrameAnswerer); ok {
					pairs, err := fa.AnswerFindFramed(sq)
					results[i] = answerResult{found: encodePairs(pairs), err: err}
				} else {
					got, err := cond.AnswerFind(sq)
					var pairs []gen.FramedValue
					for _, v := range got {
						pairs = append(pairs, gen.FramedValue{Value: v, Frame: world.ActualFrame})
					}
					results[i] = answerResult{found: encodePairs(pairs), err: err}
				}
				return
			}
			got, err := cond.AnswerFind(sq)
			results[i] = answerResult{found: got, err: err}
		case "which_frames":
			if fa, ok := cond.(FrameAnswerer); ok {
				got, err := fa.AnswerWhichFrames(sq)
				results[i] = answerResult{found: got, err: err}
			} else {
				// frame-blind default: "actual" iff the condition holds the atom
				got, err := cond.AnswerHolds(sq)
				var frames []string
				if got {
					frames = []string{world.ActualFrame}
				}
				results[i] = answerResult{found: frames, err: err}
			}
		}
	}
	if workers == 1 {
		for i := range queries {
			answer(i)
		}
	} else {
		jobs := make(chan int)
		var wg sync.WaitGroup
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range jobs {
					answer(i)
				}
			}()
		}
		for i := range queries {
			jobs <- i
		}
		close(jobs)
		wg.Wait()
	}

	rep := &Report{Condition: cond.Name(), PerDepth: map[int]*SliceScore{}}
	for i, q := range queries {
		res := results[i]
		switch q.Type {
		case "holds":
			if res.err != nil {
				rep.Errors++
				continue
			}
			want := q.Answer != nil && *q.Answer
			correct := res.holds == want
			switch q.Slice {
			case "repetition":
				tally(&rep.Repetition, want, correct)
			case "composition":
				tally(&rep.Composition, want, correct)
				ds, ok := rep.PerDepth[q.Depth]
				if !ok {
					ds = &SliceScore{}
					rep.PerDepth[q.Depth] = ds
				}
				tally(ds, want, correct)
			case "revision":
				if q.StaleAnswer != nil && *q.StaleAnswer != want {
					// flip: current truth differs from stale belief
					rep.Revision.FlipTotal++
					if correct {
						rep.Revision.FlipCorrect++
					} else if res.holds == *q.StaleAnswer {
						rep.Revision.StaleAgreements++
					}
				} else {
					rep.Revision.RetainTotal++
					if correct {
						rep.Revision.RetainCorrect++
					}
				}
			case "contamination":
				fr := rep.frames()
				tally(&fr.Contamination, want, correct)
				sub := strings.TrimSuffix(q.Subpop, "-control")
				ss, ok := fr.ContaminationSub[sub]
				if !ok {
					ss = &SliceScore{}
					fr.ContaminationSub[sub] = ss
				}
				tally(ss, want, correct)
				if q.CueClass != "" {
					if fr.CueSub == nil {
						fr.CueSub = map[string]*SliceScore{}
					}
					cs, ok := fr.CueSub[q.CueClass]
					if !ok {
						cs = &SliceScore{}
						fr.CueSub[q.CueClass] = cs
					}
					tally(cs, want, correct)
				}
			case "isolation":
				fr := rep.frames()
				tally(&fr.Isolation, want, correct)
				if q.Subpop == "chain" || q.Subpop == "chain-control" {
					tally(&fr.IsolationChain, want, correct)
				}
			case "pinning":
				tally(&rep.frames().Pinning, want, correct)
			case "promotion":
				tally(&rep.frames().Promotion, want, correct)
			default:
				// a query scored nowhere is a silent hole in the campaign;
				// count it loudly instead of vanishing it (D7-adjacent).
				rep.UnknownSlice++
			}
		case "find":
			if res.err != nil {
				rep.Errors++
				continue
			}
			if len(q.FramesScope) > 0 {
				scoreFind(&rep.frames().Ideation, res.found, encodePairs(q.AnswerFramed))
			} else {
				scoreFind(&rep.Find, res.found, q.AnswerSet)
			}
		case "which_frames":
			if res.err != nil {
				rep.Errors++
				continue
			}
			scoreFind(&rep.frames().Misattribution, res.found, q.AnswerFrames)
		default:
			rep.UnknownSlice++
		}
	}
	finalizeFind(&rep.Find)
	if rep.Frames != nil {
		finalizeFind(&rep.Frames.Misattribution)
		finalizeFind(&rep.Frames.Ideation)
	}
	return rep, nil
}

// encodePairs flattens ideation (value, frame) pairs into strings for
// exact-set + micro scoring: right value in the wrong frame is an error.
func encodePairs(pairs []gen.FramedValue) []string {
	out := make([]string, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, p.Frame+"|"+p.Value)
	}
	return out
}

func tally(s *SliceScore, want, correct bool) {
	if want {
		s.PosTotal++
		if correct {
			s.PosCorrect++
		}
	} else {
		s.NegTotal++
		if correct {
			s.NegCorrect++
		}
	}
}

func scoreFind(f *FindScore, got, want []string) {
	f.Total++
	gs, ws := toSet(got), toSet(want)
	if setsEqual(gs, ws) {
		f.ExactMatches++
	}
	for g := range gs {
		if ws[g] {
			f.MicroTP++
		} else {
			f.MicroFP++
		}
	}
	for w := range ws {
		if !gs[w] {
			f.MicroFN++
		}
	}
}

func finalizeFind(f *FindScore) {
	if f.MicroTP+f.MicroFP > 0 {
		f.MicroPrecision = float64(f.MicroTP) / float64(f.MicroTP+f.MicroFP)
	}
	if f.MicroTP+f.MicroFN > 0 {
		f.MicroRecall = float64(f.MicroTP) / float64(f.MicroTP+f.MicroFN)
	}
	if f.MicroPrecision+f.MicroRecall > 0 {
		f.F1 = 2 * f.MicroPrecision * f.MicroRecall / (f.MicroPrecision + f.MicroRecall)
	}
}

func toSet(in []string) map[string]bool {
	m := map[string]bool{}
	for _, s := range in {
		m[s] = true
	}
	return m
}

func setsEqual(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

// Table renders reports side by side.
func Table(reports []*Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-20s %9s %9s %9s %9s %9s %9s %7s\n",
		"condition", "rep+", "rep-", "comp+", "comp-", "rev.flip", "rev.ret", "find=")
	for _, r := range reports {
		fmt.Fprintf(&b, "%-20s %9s %9s %9s %9s %9s %9s %7s\n",
			r.Condition,
			frac(r.Repetition.PosCorrect, r.Repetition.PosTotal),
			frac(r.Repetition.NegCorrect, r.Repetition.NegTotal),
			frac(r.Composition.PosCorrect, r.Composition.PosTotal),
			frac(r.Composition.NegCorrect, r.Composition.NegTotal),
			frac(r.Revision.FlipCorrect, r.Revision.FlipTotal),
			frac(r.Revision.RetainCorrect, r.Revision.RetainTotal),
			frac(r.Find.ExactMatches, r.Find.Total),
		)
	}
	// frame slices (frames datasets only)
	anyFrames := false
	for _, r := range reports {
		if r.Frames != nil {
			anyFrames = true
			break
		}
	}
	if anyFrames {
		b.WriteString("\nframe slices (traps are negatives, paired controls positives):\n")
		fmt.Fprintf(&b, "%-20s %9s %9s %9s %9s %9s %9s %9s %9s %7s %7s\n",
			"condition", "cont+", "cont-", "iso+", "iso-", "pin+", "pin-", "prom+", "prom-", "mis=", "idea=")
		for _, r := range reports {
			if r.Frames == nil {
				continue
			}
			f := r.Frames
			fmt.Fprintf(&b, "%-20s %9s %9s %9s %9s %9s %9s %9s %9s %7s %7s\n",
				r.Condition,
				frac(f.Contamination.PosCorrect, f.Contamination.PosTotal),
				frac(f.Contamination.NegCorrect, f.Contamination.NegTotal),
				frac(f.Isolation.PosCorrect, f.Isolation.PosTotal),
				frac(f.Isolation.NegCorrect, f.Isolation.NegTotal),
				frac(f.Pinning.PosCorrect, f.Pinning.PosTotal),
				frac(f.Pinning.NegCorrect, f.Pinning.NegTotal),
				frac(f.Promotion.PosCorrect, f.Promotion.PosTotal),
				frac(f.Promotion.NegCorrect, f.Promotion.NegTotal),
				frac(f.Misattribution.ExactMatches, f.Misattribution.Total),
				frac(f.Ideation.ExactMatches, f.Ideation.Total),
			)
		}
		// contamination sub-populations (gap is the F-E1 mandatory sub-line)
		subSet := map[string]bool{}
		for _, r := range reports {
			if r.Frames != nil {
				for s := range r.Frames.ContaminationSub {
					subSet[s] = true
				}
			}
		}
		if len(subSet) > 0 {
			var subs []string
			for s := range subSet {
				subs = append(subs, s)
			}
			sort.Strings(subs)
			b.WriteString("\ncontamination by sub-population (trap-/control+ correct):\n")
			fmt.Fprintf(&b, "%-20s", "condition")
			for _, s := range subs {
				fmt.Fprintf(&b, " %16s", s)
			}
			b.WriteString("\n")
			for _, r := range reports {
				if r.Frames == nil {
					continue
				}
				fmt.Fprintf(&b, "%-20s", r.Condition)
				for _, s := range subs {
					if ss, ok := r.Frames.ContaminationSub[s]; ok {
						fmt.Fprintf(&b, " %16s", fmt.Sprintf("%s/%s",
							frac(ss.NegCorrect, ss.NegTotal), frac(ss.PosCorrect, ss.PosTotal)))
					} else {
						fmt.Fprintf(&b, " %16s", "-")
					}
				}
				b.WriteString("\n")
			}
		}
	}

	// per-depth breakdown for composition holds
	depthSet := map[int]bool{}
	for _, r := range reports {
		for d := range r.PerDepth {
			depthSet[d] = true
		}
	}
	if len(depthSet) > 0 {
		var depths []int
		for d := range depthSet {
			depths = append(depths, d)
		}
		sort.Ints(depths)
		b.WriteString("\ncomposition accuracy by derivation depth:\n")
		fmt.Fprintf(&b, "%-20s", "condition")
		for _, d := range depths {
			fmt.Fprintf(&b, " %8s", fmt.Sprintf("d%d", d))
		}
		b.WriteString("\n")
		for _, r := range reports {
			fmt.Fprintf(&b, "%-20s", r.Condition)
			for _, d := range depths {
				if s, ok := r.PerDepth[d]; ok {
					fmt.Fprintf(&b, " %8s", fmt.Sprintf("%.2f", s.Accuracy()))
				} else {
					fmt.Fprintf(&b, " %8s", "-")
				}
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

func frac(c, t int) string {
	if t == 0 {
		return "-"
	}
	return fmt.Sprintf("%d/%d", c, t)
}
