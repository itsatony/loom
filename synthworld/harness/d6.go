package harness

import (
	"context"
	"fmt"
	"strings"

	"github.com/vaudience/synthworld/gen"
)

// ---------- D6: perfect-retrieval DIAGNOSTIC (ceiling, not a competitor) ----------

// PerfectRetrievalCondition is a DIAGNOSTIC, not a C1 baseline: the RAG
// prompt pipeline fed EXACTLY the query's true provenance episodes. It
// answers the question "if retrieval were perfect, could the LLM compose
// the pieces?" — separating C1's retrieval failure from its reasoning
// failure (MASTERPLAN H2/D6). It must never be reported as a competitor;
// its score is an upper bound on any retrieval-based C1 with this LLM.
//
// Ground-truth handling: conditions only receive SanitizedQuery, so the
// provenance map (query ID → provenance episode IDs) is injected at
// construction by cmd/harness, which holds the unsanitized QuerySet. The
// lookup key is SanitizedQuery.ID — an opaque identifier that encodes no
// answer, slice, trace, or provenance content, so the sanitize() invariant
// stands for every real condition; the injected map is the ceiling's
// deliberate, labeled cheat (same category as OracleCondition holding the
// true world).
type PerfectRetrievalCondition struct {
	LLM        LLMClient
	provenance map[string][]string // query ID → provenance episode IDs
	texts      map[string]string   // episode ID → text, built at Ingest
}

// NewPerfectRetrievalCondition builds the diagnostic. provenance maps query
// IDs to their true provenance episode IDs; queries absent from the map
// (negative controls have no provenance) get an empty context — the honest
// input for "no episode supports this".
func NewPerfectRetrievalCondition(llm LLMClient, provenance map[string][]string) *PerfectRetrievalCondition {
	return &PerfectRetrievalCondition{LLM: llm, provenance: provenance}
}

func (d *PerfectRetrievalCondition) Name() string { return "d6-perfect-retrieval" }

func (d *PerfectRetrievalCondition) Ingest(episodes []gen.Episode) error {
	d.texts = make(map[string]string, len(episodes))
	for _, ep := range episodes {
		d.texts[ep.ID] = ep.Text
	}
	return nil
}

func (d *PerfectRetrievalCondition) contextFor(q SanitizedQuery) (string, error) {
	ids := d.provenance[q.ID]
	if len(ids) == 0 {
		return "(no episodes)\n", nil
	}
	var b strings.Builder
	for _, id := range ids {
		text, ok := d.texts[id]
		if !ok {
			return "", fmt.Errorf("d6: provenance episode %s not in ingested episodes", id)
		}
		b.WriteString(text)
		b.WriteString("\n\n")
	}
	return b.String(), nil
}

func (d *PerfectRetrievalCondition) AnswerHolds(q SanitizedQuery) (bool, error) {
	ctxText, err := d.contextFor(q)
	if err != nil {
		return false, err
	}
	out, err := d.LLM.Complete(context.Background(), ragSystemPrompt, episodesUserPrompt(ctxText, q))
	if err != nil {
		return false, err
	}
	return parseHoldsAnswer(out)
}

func (d *PerfectRetrievalCondition) AnswerFind(q SanitizedQuery) ([]string, error) {
	ctxText, err := d.contextFor(q)
	if err != nil {
		return nil, err
	}
	out, err := d.LLM.Complete(context.Background(), ragSystemPrompt, episodesUserPrompt(ctxText, q))
	if err != nil {
		return nil, err
	}
	return parseFindAnswer(out)
}
