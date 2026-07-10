package harness

import (
	"sort"

	"github.com/vaudience/synthworld/gen"
	"github.com/vaudience/synthworld/loom"
	"github.com/vaudience/synthworld/world"
)

// LoomCondition is C2a: structured-episode ingest into the Loom store,
// deterministic planner (the query's structure selects the operation — the
// LLM planner enters only for natural-language-only querying, which is a
// harness-side adapter, not a substrate property).
//
// Expected S1 exit behavior: oracle-equal on every slice — any deviation is
// a compilation or schema-inference bug and the per-query IDs in the report
// are the debugging entry points. On frames datasets the same bar holds
// against the frame-oracle: the store compiles frame declarations, homes
// each item in its payload frame (quotes land in the speaker's perspective
// frame), skips non-assertive speech, and answers in the query's frame.
type LoomCondition struct {
	store *loom.Store
	// Report from ingest, exposed for the harness to print.
	Ingest_ *loom.IngestReport
}

func NewLoomCondition() *LoomCondition { return &LoomCondition{} }

func (l *LoomCondition) Name() string { return "loom-C2a" }

func (l *LoomCondition) Ingest(episodes []gen.Episode) error {
	l.store = loom.NewStore()
	rep, err := l.store.IngestStructured(episodes)
	l.Ingest_ = rep
	return err
}

func (l *LoomCondition) AnswerHolds(q SanitizedQuery) (bool, error) {
	ok, _, err := l.store.HoldsIn(q.Frame, *q.Atom, q.AtDay)
	return ok, err
}

func (l *LoomCondition) AnswerFind(q SanitizedQuery) ([]string, error) {
	return l.store.FindIn(q.Frame, *q.Pattern, q.FindSlot, q.AtDay)
}

// frameUniverse is actual + every ingested frame, sorted, actual first —
// the store-side analogue of the diagnostic oracles' frameUniverse, built
// from ingested declarations only.
func (l *LoomCondition) frameUniverse() []string {
	ids := []string{world.ActualFrame}
	var rest []string
	for _, f := range l.store.Frames {
		rest = append(rest, f.Frame.ID)
	}
	sort.Strings(rest)
	return append(ids, rest...)
}

func (l *LoomCondition) AnswerWhichFrames(q SanitizedQuery) ([]string, error) {
	var out []string
	for _, fid := range l.frameUniverse() {
		ok, _, err := l.store.HoldsIn(fid, *q.Atom, q.AtDay)
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

func (l *LoomCondition) AnswerFindFramed(q SanitizedQuery) ([]gen.FramedValue, error) {
	var out []gen.FramedValue
	for _, fid := range q.FramesScope {
		vals, err := l.store.FindIn(fid, *q.Pattern, q.FindSlot, q.AtDay)
		if err != nil {
			return nil, err
		}
		for _, v := range vals {
			out = append(out, gen.FramedValue{Value: v, Frame: fid})
		}
	}
	return out, nil
}
