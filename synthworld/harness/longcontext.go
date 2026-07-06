package harness

import (
	"context"
	"fmt"
	"strings"

	"github.com/vaudience/synthworld/gen"
)

// ---------- C1c: long-context (no retrieval) ----------

// C1cLongContext is the honest 2026 competitor (MASTERPLAN §3): every
// episode, in chronological order, concatenated into the context — no
// retrieval step to fail. It is part of "strongest C1": if a long-context
// model simply reads the corpus and composes, retrieval-based C1 was a
// strawman. Its per-query token cost scales with corpus size, which is
// exactly the economics H7 measures against compile-once-query-many.
type C1cLongContext struct {
	LLM LLMClient

	corpus string
}

func (c *C1cLongContext) Name() string { return "c1c-longcontext" }

func (c *C1cLongContext) Ingest(episodes []gen.Episode) error {
	var b strings.Builder
	for _, ep := range episodes {
		fmt.Fprintf(&b, "=== Episode %s (day %d) ===\n%s\n\n", ep.ID, ep.Day, ep.Text)
	}
	c.corpus = b.String()
	return nil
}

func (c *C1cLongContext) userPrompt(q SanitizedQuery) string {
	return episodesUserPrompt(c.corpus, q)
}

func (c *C1cLongContext) AnswerHolds(q SanitizedQuery) (bool, error) {
	out, err := c.LLM.Complete(context.Background(), ragSystemPrompt, c.userPrompt(q))
	if err != nil {
		return false, err
	}
	return parseHoldsAnswer(out)
}

func (c *C1cLongContext) AnswerFind(q SanitizedQuery) ([]string, error) {
	out, err := c.LLM.Complete(context.Background(), ragSystemPrompt, c.userPrompt(q))
	if err != nil {
		return nil, err
	}
	return parseFindAnswer(out)
}
