package harness

import (
	"fmt"
	"sort"

	"github.com/vaudience/synthworld/gen"
	"github.com/vaudience/synthworld/loom"
	"github.com/vaudience/synthworld/world"
)

// LoomC2bCondition is C2b: the store is built by the S2 compilation
// pipeline from episode TEXT (hard mode) — extraction → normalization →
// consistency → hygiene — then queried through the same deterministic
// planner as loom-C2a. The condition still receives only SanitizedQuery;
// the pipeline sees episode text, the seeded relation vocabulary
// (spec §4 — names and slot names only, injected by cmd/harness), and
// nothing else.
//
// Two extractor variants register in cmd/harness:
//   - "loom-c2b-det": DeterministicExtractor. A CONTROL — oracle-equal
//     scores prove the pipeline is lossless so C2b deficits are
//     attributable to LLM extraction; they never validate the thesis.
//   - "loom-c2b": LLMExtractor. The condition the campaign is about.
type LoomC2bCondition struct {
	Label     string
	Vocab     loom.Vocabulary
	Extractor loom.Extractor
	Workers   int
	// FrameNames maps canonical frame IDs to the surface names the episode
	// text uses (tier-M handles; empty on tier E where text carries raw
	// IDs). Injected by cmd/harness from the dataset's naturalize report —
	// a naming affordance shared by every text-mode condition, never
	// frame detection (see loom.Pipeline.FrameNames).
	FrameNames map[string]string
	// QuarantineActualBelowConfidence, when >0, routes low-confidence
	// actual-homed facts to quarantine (§9.6.1 safety gate); passed to the
	// pipeline. 0 = disabled.
	QuarantineActualBelowConfidence float64
	// FrameBlind answers every query from the actual closure, ignoring the
	// query frame — the honest behavior of a v0-extractor store on a frames
	// dataset (mono-world style). Required for the frozen §9.6.5 row
	// "loom-c2b-det == v0 oracle in every cell"; also the measured
	// frame-blind C2b that confirms fiction-trap contamination (§9.6.3).
	FrameBlind bool

	pipeline *loom.Pipeline
	// Compile report from ingest, exposed for printing/persisting.
	Compile_ *loom.CompileReport
}

func (l *LoomC2bCondition) Name() string { return l.Label }

func (l *LoomC2bCondition) Ingest(episodes []gen.Episode) error {
	l.pipeline = loom.NewPipeline(l.Vocab, l.Extractor)
	l.pipeline.Workers = l.Workers
	l.pipeline.FrameNames = l.FrameNames
	l.pipeline.QuarantineActualBelowConfidence = l.QuarantineActualBelowConfidence
	rep, err := l.pipeline.Compile(episodes)
	l.Compile_ = rep
	if err != nil {
		return fmt.Errorf("%s compile: %w", l.Label, err)
	}
	return nil
}

func (l *LoomC2bCondition) AnswerHolds(q SanitizedQuery) (bool, error) {
	if l.FrameBlind {
		ok, _, err := l.pipeline.Store.Holds(*q.Atom, q.AtDay)
		return ok, err
	}
	if !l.pipeline.Store.HasFrame(q.Frame) {
		return false, nil // frame never compiled: it holds nothing (an answer, not an error)
	}
	ok, _, err := l.pipeline.Store.HoldsIn(q.Frame, *q.Atom, q.AtDay)
	return ok, err
}

func (l *LoomC2bCondition) AnswerFind(q SanitizedQuery) ([]string, error) {
	if l.FrameBlind {
		return l.pipeline.Store.Find(*q.Pattern, q.FindSlot, q.AtDay)
	}
	if !l.pipeline.Store.HasFrame(q.Frame) {
		return nil, nil
	}
	return l.pipeline.Store.FindIn(q.Frame, *q.Pattern, q.FindSlot, q.AtDay)
}

// frameUniverse is actual + every compiled frame, sorted, actual first —
// built from ingested declarations (and provisional registrations) only.
func (l *LoomC2bCondition) frameUniverse() []string {
	ids := []string{world.ActualFrame}
	var rest []string
	for _, f := range l.pipeline.Store.Frames {
		rest = append(rest, f.Frame.ID)
	}
	sort.Strings(rest)
	return append(ids, rest...)
}

func (l *LoomC2bCondition) AnswerWhichFrames(q SanitizedQuery) ([]string, error) {
	if l.FrameBlind {
		// mirror the harness's frame-blind default: "actual" iff held
		ok, _, err := l.pipeline.Store.Holds(*q.Atom, q.AtDay)
		if err != nil || !ok {
			return nil, err
		}
		return []string{world.ActualFrame}, nil
	}
	var out []string
	for _, fid := range l.frameUniverse() {
		ok, _, err := l.pipeline.Store.HoldsIn(fid, *q.Atom, q.AtDay)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, fid)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (l *LoomC2bCondition) AnswerFindFramed(q SanitizedQuery) ([]gen.FramedValue, error) {
	if l.FrameBlind {
		vals, err := l.pipeline.Store.Find(*q.Pattern, q.FindSlot, q.AtDay)
		if err != nil {
			return nil, err
		}
		var out []gen.FramedValue
		for _, v := range vals {
			out = append(out, gen.FramedValue{Value: v, Frame: world.ActualFrame})
		}
		return out, nil
	}
	var out []gen.FramedValue
	for _, fid := range q.FramesScope {
		if !l.pipeline.Store.HasFrame(fid) {
			continue
		}
		vals, err := l.pipeline.Store.FindIn(fid, *q.Pattern, q.FindSlot, q.AtDay)
		if err != nil {
			return nil, err
		}
		for _, v := range vals {
			out = append(out, gen.FramedValue{Value: v, Frame: fid})
		}
	}
	return out, nil
}
