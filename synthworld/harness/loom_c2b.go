package harness

import (
	"fmt"

	"github.com/vaudience/synthworld/gen"
	"github.com/vaudience/synthworld/loom"
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

	pipeline *loom.Pipeline
	// Compile report from ingest, exposed for printing/persisting.
	Compile_ *loom.CompileReport
}

func (l *LoomC2bCondition) Name() string { return l.Label }

func (l *LoomC2bCondition) Ingest(episodes []gen.Episode) error {
	l.pipeline = loom.NewPipeline(l.Vocab, l.Extractor)
	l.pipeline.Workers = l.Workers
	rep, err := l.pipeline.Compile(episodes)
	l.Compile_ = rep
	if err != nil {
		return fmt.Errorf("%s compile: %w", l.Label, err)
	}
	return nil
}

func (l *LoomC2bCondition) AnswerHolds(q SanitizedQuery) (bool, error) {
	ok, _, err := l.pipeline.Store.Holds(*q.Atom, q.AtDay)
	return ok, err
}

func (l *LoomC2bCondition) AnswerFind(q SanitizedQuery) ([]string, error) {
	return l.pipeline.Store.Find(*q.Pattern, q.FindSlot, q.AtDay)
}
