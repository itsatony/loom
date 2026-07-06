package harness

import (
	"context"
	"fmt"

	"github.com/vaudience/synthworld/gen"
)

// ---------- C0: no memory ----------

// C0Condition is the no-memory floor: the LLM answers from the sanitized
// query alone — question text plus the structured atom/pattern, zero
// episodes. Every "memory lift" is measured against this. Expected to floor
// near always-false on positives (the model has no way to know the
// synthetic world); anything a memory condition scores below C0 means the
// memory has negative value.
type C0Condition struct {
	LLM LLMClient
}

func (c *C0Condition) Name() string { return "c0-no-memory" }

func (c *C0Condition) Ingest([]gen.Episode) error { return nil }

const c0SystemPrompt = `You are asked questions about a synthetic world. You are given NO background material — only the question itself. If you cannot know the answer, give your best guess in the required format anyway.
Answer format: for true/false questions reply with exactly one word, true or false. For list questions reply with a JSON array of entity IDs, e.g. ["customer_03","customer_07"], or [] if none.`

func (c *C0Condition) userPrompt(q SanitizedQuery) string {
	return fmt.Sprintf("Question: %s\nStructured form: %s", q.Text, structuredQueryJSON(q))
}

func (c *C0Condition) AnswerHolds(q SanitizedQuery) (bool, error) {
	out, err := c.LLM.Complete(context.Background(), c0SystemPrompt, c.userPrompt(q))
	if err != nil {
		return false, err
	}
	return parseHoldsAnswer(out)
}

func (c *C0Condition) AnswerFind(q SanitizedQuery) ([]string, error) {
	out, err := c.LLM.Complete(context.Background(), c0SystemPrompt, c.userPrompt(q))
	if err != nil {
		return nil, err
	}
	return parseFindAnswer(out)
}
