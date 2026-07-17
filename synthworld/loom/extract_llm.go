// LLMExtractor — the S2 stage under test. Schema-prompted per-episode
// extraction: the model receives the relation vocabulary (names + slot
// names only), the candidate JSON schema, and fixed few-shot examples
// written against FICTIONAL relations (never dataset content), and must
// return strict JSON. Parsing is lenient about wrappers (code fences,
// leading prose) but loud about content: a non-JSON reply or an
// unrecognizable candidate is a trace problem, never a silent drop.
package loom

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/vaudience/synthworld/gen"
)

// Completer is the minimal LLM surface the extractor needs. Deliberately
// duplicated from harness (loom cannot import harness — harness imports
// loom); harness's cached/metered clients satisfy it structurally.
type Completer interface {
	Complete(ctx context.Context, system, user string) (string, error)
	Model() string
}

type LLMExtractor struct {
	LLM      Completer
	vocabRef Vocabulary
}

func (e *LLMExtractor) Name() string { return "llm:" + e.LLM.Model() }

const extractSystemPrompt = `You are a knowledge compiler. You turn episode text into structured items: facts, rules (policies), and supersession notices. You reply with STRICT JSON only — no prose, no code fences.

Output schema:
{
  "facts": [{"fact_id": "...", "relation": "<relation name>", "args": {"<slot>": "<entity_id>"}, "valid_from": <int>, "valid_to": <int 0 if open>, "source": "...", "confidence": <0..1>, "line": "<the exact input line>"}],
  "rules": [{"rule_id": "...", "name": "...", "authority": <1..5>, "issued_at": <int>, "effective_from": <int>, "effective_to": <int 0 if open>,
             "conditions": [{"relation": "...", "args": {"<slot>": "<entity_id or ?VAR>"}}],
             "conclusion": {"relation": "...", "args": {...}},
             "exceptions": [{"relation": "...", "args": {...}}],
             "confidence": <0..1>, "line": "<the exact input line>"}],
  "supersessions": [{"notice_id": "...", "old_rule": "...", "new_rule": "...", "from": <int>, "confidence": <0..1>, "line": "<the exact input line>"}]
}

Rules of extraction:
- Variables are written ?A, ?B … in the text; keep them verbatim in args values.
- "valid from day X" with no end means valid_to = 0. "until day Y" means valid_to = Y.
- Extract EVERY fact, policy, and notice line. Do not invent items that are not stated.
- Use only relation names from the provided vocabulary; copy slot names exactly as written in the text.
- Empty categories are empty arrays. Reply with the JSON object only.

Example (fictional relations, for format only):
Input line: [day 3] Observation (fct_9001, source hr_feed, valid from day 3): mentors(person=person_02, person2=person_05).
Output fact: {"fact_id":"fct_9001","relation":"mentors","args":{"person":"person_02","person2":"person_05"},"valid_from":3,"valid_to":0,"source":"hr_feed","confidence":1.0,"line":"[day 3] Observation (fct_9001, source hr_feed, valid from day 3): mentors(person=person_02, person2=person_05)."}
Input line: [day 9] Policy rul_900 ("mentor gate", authority level 2, effective from day 9): IF mentors(person=?A, person2=?B) AND certified(person=?A) THEN may_review(person=?A, person2=?B), UNLESS suspended(person=?A).
Output rule: {"rule_id":"rul_900","name":"mentor gate","authority":2,"issued_at":9,"effective_from":9,"effective_to":0,"conditions":[{"relation":"mentors","args":{"person":"?A","person2":"?B"}},{"relation":"certified","args":{"person":"?A"}}],"conclusion":{"relation":"may_review","args":{"person":"?A","person2":"?B"}},"exceptions":[{"relation":"suspended","args":{"person":"?A"}}],"confidence":1.0,"line":"..."}
Input line: [day 12] Notice sup_900: policy rul_900 is superseded by policy rul_901, effective day 12. From that day, rul_900 no longer applies.
Output supersession: {"notice_id":"sup_900","old_rule":"rul_900","new_rule":"rul_901","from":12,"confidence":1.0,"line":"..."}`

// llmCandidateEnvelope mirrors the prompt's output schema.
type llmCandidateEnvelope struct {
	Facts []struct {
		FactCand
		Confidence float64 `json:"confidence"`
		Line       string  `json:"line"`
	} `json:"facts"`
	Rules []struct {
		RuleCand
		Confidence float64 `json:"confidence"`
		Line       string  `json:"line"`
	} `json:"rules"`
	Supersessions []struct {
		SupCand
		Confidence float64 `json:"confidence"`
		Line       string  `json:"line"`
	} `json:"supersessions"`
}

func (e *LLMExtractor) Extract(ep gen.Episode) ([]Candidate, []string, error) {
	user := fmt.Sprintf("Relation vocabulary (name: slots):\n%s\nEpisode text:\n%s\n\nExtract all items as JSON.",
		e.vocabLines(), ep.Text)
	out, err := e.LLM.Complete(context.Background(), extractSystemPrompt, user)
	if err != nil {
		return nil, nil, fmt.Errorf("episode %s: %w", ep.ID, err)
	}
	env, perr := parseExtractionJSON(out)
	if perr != nil {
		return nil, nil, fmt.Errorf("episode %s: %v", ep.ID, perr)
	}
	var cands []Candidate
	var problems []string
	for _, f := range env.Facts {
		fc := f.FactCand
		cands = append(cands, Candidate{Kind: CandFact, Fact: &fc, Confidence: clamp01(f.Confidence), SourceSpan: f.Line})
	}
	for _, r := range env.Rules {
		rc := r.RuleCand
		cands = append(cands, Candidate{Kind: CandRule, Rule: &rc, Confidence: clamp01(r.Confidence), SourceSpan: r.Line})
	}
	for _, s := range env.Supersessions {
		sc := s.SupCand
		cands = append(cands, Candidate{Kind: CandSupersession, Sup: &sc, Confidence: clamp01(s.Confidence), SourceSpan: s.Line})
	}
	// Coverage check: an event line the model returned nothing for is a
	// problem worth tracing (recall loss visible at compile time, not
	// first query).
	nLines := 0
	for _, line := range strings.Split(ep.Text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "=== Episode") {
			nLines++
		}
	}
	if len(cands) < nLines {
		problems = append(problems, fmt.Sprintf("%s: %d event lines but %d candidates extracted", ep.ID, nLines, len(cands)))
	}
	return cands, problems, nil
}

// NewLLMExtractor binds the completer and the seeded vocabulary.
func NewLLMExtractor(llm Completer, vocab Vocabulary) *LLMExtractor {
	return &LLMExtractor{LLM: llm, vocabRef: vocab}
}

// vocabLines renders the seeded vocabulary for the prompt, sorted for
// deterministic prompts (and therefore cache keys).
func (e *LLMExtractor) vocabLines() string { return vocabPromptLines(e.vocabRef) }

// vocabPromptLines is the shared prompt rendering of the seeded vocabulary
// (v0 and frames extractors must produce identical vocabulary blocks so
// cassettes stay comparable across extractor variants).
func vocabPromptLines(v Vocabulary) string {
	rels := append([]RelationVocab(nil), v.Relations...)
	sort.Slice(rels, func(i, j int) bool { return rels[i].Name < rels[j].Name })
	var b strings.Builder
	for _, r := range rels {
		fmt.Fprintf(&b, "- %s: %s\n", r.Name, strings.Join(r.Slots, ", "))
	}
	return b.String()
}

// parseExtractionJSON accepts the raw model reply, tolerating code fences
// and stray prose around exactly one JSON object.
func parseExtractionJSON(out string) (*llmCandidateEnvelope, error) {
	s := strings.TrimSpace(out)
	if i := strings.Index(s, "{"); i > 0 {
		s = s[i:]
	}
	if i := strings.LastIndex(s, "}"); i >= 0 {
		s = s[:i+1]
	}
	var env llmCandidateEnvelope
	if err := json.Unmarshal([]byte(s), &env); err != nil {
		return nil, fmt.Errorf("extraction reply is not the expected JSON (%v); reply head: %.160s", err, out)
	}
	return &env, nil
}

func clamp01(f float64) float64 {
	if f <= 0 {
		return 0.5 // model omitted confidence: middling, visible in provenance
	}
	if f > 1 {
		return 1
	}
	return f
}
