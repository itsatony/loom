package harness

import (
	"github.com/vaudience/synthworld/gen"
	"github.com/vaudience/synthworld/loom"
)

// LoomCondition is C2a: structured-episode ingest into the Loom store,
// deterministic planner (the query's structure selects the operation — the
// LLM planner enters only for natural-language-only querying, which is a
// harness-side adapter, not a substrate property).
//
// Expected S1 exit behavior: oracle-equal on every slice — any deviation is
// a compilation or schema-inference bug and the per-query IDs in the report
// are the debugging entry points.
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
	ok, _, err := l.store.Holds(*q.Atom, q.AtDay)
	return ok, err
}

func (l *LoomCondition) AnswerFind(q SanitizedQuery) ([]string, error) {
	return l.store.Find(*q.Pattern, q.FindSlot, q.AtDay)
}
