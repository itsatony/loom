package harness

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Shared LLM answer parsing, used by every LLM-backed condition (RAG, C0,
// D6). Keeping one parser means a parsing quirk biases every condition
// identically instead of silently favoring one.

// parseHoldsAnswer maps a completion to a boolean. Strict prefix match
// first; then a symmetric lenient scan (exactly one of "true"/"false"
// appears anywhere). Anything else is an error — counted as an error by the
// scorer, never coerced into an answer.
func parseHoldsAnswer(out string) (bool, error) {
	ans := strings.ToLower(strings.TrimSpace(out))
	switch {
	case strings.HasPrefix(ans, "true"):
		return true, nil
	case strings.HasPrefix(ans, "false"):
		return false, nil
	}
	hasTrue := strings.Contains(ans, "true")
	hasFalse := strings.Contains(ans, "false")
	switch {
	case hasTrue && !hasFalse:
		return true, nil
	case hasFalse && !hasTrue:
		return false, nil
	}
	return false, fmt.Errorf("unparseable holds answer: %q", firstN(out, 120))
}

// parseFindAnswer extracts the first JSON array of strings from a
// completion.
func parseFindAnswer(out string) ([]string, error) {
	start := strings.Index(out, "[")
	end := strings.LastIndex(out, "]")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("unparseable find answer: %q", firstN(out, 120))
	}
	var list []string
	if err := json.Unmarshal([]byte(out[start:end+1]), &list); err != nil {
		return nil, fmt.Errorf("find answer JSON: %w", err)
	}
	return list, nil
}

// episodesUserPrompt is the uniform user prompt for every episode-fed LLM
// condition (RAG, D6, C1c): context episodes, the question, and the same
// structured atom the deterministic planner sees — so the comparison is
// about memory, never about question parsing (MASTERPLAN §4.2).
func episodesUserPrompt(contextText string, q SanitizedQuery) string {
	return fmt.Sprintf("Episodes:\n\n%s\nQuestion: %s\nStructured form: %s",
		contextText, q.Text, structuredQueryJSON(q))
}

// structuredQueryJSON renders the sanitized query's structured part (atom or
// pattern, evaluation day) for inclusion in an LLM prompt. Giving every
// LLM-backed condition the same structured atom the deterministic planner
// sees keeps the comparison about memory, not about question parsing
// (MASTERPLAN §4.2).
func structuredQueryJSON(q SanitizedQuery) string {
	m := map[string]any{"at_day": q.AtDay}
	if q.Atom != nil {
		m["atom"] = q.Atom
	}
	if q.Pattern != nil {
		m["pattern"] = q.Pattern
		m["find_slot"] = q.FindSlot
	}
	raw, _ := json.Marshal(m)
	return string(raw)
}
