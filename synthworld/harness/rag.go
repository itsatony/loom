package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/vaudience/synthworld/gen"
)

// ---------- LLM client ----------

// LLMClient is the minimal completion interface a condition needs. The
// production implementation on vAI infra is go-vaibstract; this package
// ships a plain OpenAI-compatible HTTP client so the harness stays
// dependency-free and points at any vLLM endpoint.
type LLMClient interface {
	Complete(ctx context.Context, system, user string) (string, error)
	Model() string
}

// RequestShape controls optional completion-request fields. Model families
// disagree here: vLLM/OpenAI classic accept temperature:0; the gpt-5 family
// rejects any non-default temperature and takes reasoning_effort instead;
// vLLM Qwen needs chat_template_kwargs to suppress thinking. The zero value
// preserves legacy behavior (temperature:0 sent, nothing else). An omitted
// field is NOT a zero-valued field — the cache key hashes exactly what is
// sent (buildPayload is the single source of truth for both).
type RequestShape struct {
	Temperature     *float64       // explicit value; nil → temperature:0 unless OmitTemperature
	OmitTemperature bool           // send no temperature field at all (gpt-5 family)
	ExtraParams     map[string]any // merged top-level into every request, e.g. {"reasoning_effort":"minimal"} or {"chat_template_kwargs":{"enable_thinking":false}}
}

// effectiveTemperature resolves what the request will carry: nil means the
// field is omitted entirely.
func (s RequestShape) effectiveTemperature() *float64 {
	if s.OmitTemperature {
		return nil
	}
	if s.Temperature != nil {
		return s.Temperature
	}
	zero := 0.0
	return &zero
}

// buildPayload constructs the exact chat request body. Both the HTTP client
// and the cache key marshal THIS map (json.Marshal sorts map keys, so the
// bytes are canonical), which makes drift between "what was sent" and "what
// was hashed" structurally impossible. ExtraParams cannot override model or
// messages; temperature in ExtraParams wins over the Temperature field.
func (s RequestShape) buildPayload(model, system, user string) map[string]any {
	body := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
	}
	if t := s.effectiveTemperature(); t != nil {
		body["temperature"] = *t
	}
	for k, v := range s.ExtraParams {
		if k == "model" || k == "messages" {
			continue // guarded again at env-parse time; never overridable
		}
		body[k] = v
	}
	return body
}

type OpenAICompatClient struct {
	BaseURL string // e.g. http://babylon:8000/v1
	APIKey  string
	ModelID string
	Shape   RequestShape
	Timeout time.Duration
	HTTP    *http.Client
}

func (c *OpenAICompatClient) Model() string { return c.ModelID }

func (c *OpenAICompatClient) Complete(ctx context.Context, system, user string) (string, error) {
	resp, err := c.CompleteUsage(ctx, system, user)
	return resp.Text, err
}

func (c *OpenAICompatClient) CompleteUsage(ctx context.Context, system, user string) (LLMResponse, error) {
	if c.HTTP == nil {
		c.HTTP = &http.Client{Timeout: max(c.Timeout, 60*time.Second)}
	}
	raw, err := json.Marshal(c.Shape.buildPayload(c.ModelID, system, user))
	if err != nil {
		return LLMResponse{}, fmt.Errorf("llm request marshal (ExtraParams must be JSON-encodable): %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(c.BaseURL, "/")+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return LLMResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return LLMResponse{}, err
	}
	defer resp.Body.Close()
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
		} `json:"usage"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return LLMResponse{}, err
	}
	if out.Error != nil {
		return LLMResponse{}, fmt.Errorf("llm error: %s", out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return LLMResponse{}, fmt.Errorf("llm: empty choices (status %d)", resp.StatusCode)
	}
	return LLMResponse{
		Text:  out.Choices[0].Message.Content,
		Usage: Usage{PromptTokens: out.Usage.PromptTokens, CompletionTokens: out.Usage.CompletionTokens},
	}, nil
}

// ---------- C1: episodic RAG ----------

// RAGCondition is C1: retrieve top-k episodes for the query text, hand the
// raw episode texts plus the question to an LLM. The LLM does all the
// reasoning; memory is the retrieved text. Retriever choice makes this C1a
// (tmr) or C1-bm25 etc.
type RAGCondition struct {
	Retriever Retriever
	LLM       LLMClient
	K         int
	Label     string // e.g. "rag-tmr-hybrid"; defaults to retriever name
}

func (r *RAGCondition) Name() string {
	if r.Label != "" {
		return r.Label
	}
	return "rag-" + r.Retriever.Name()
}

func (r *RAGCondition) Ingest(episodes []gen.Episode) error {
	return r.Retriever.Index(episodes)
}

// ragSystemPrompt states the instrument's full semantics contract
// (DESIGN.md §1–2). Deliberate fairness decision (MASTERPLAN §4.2): the
// LLM baseline receives the same rules-of-the-game that C2's parser gets
// for free; what it must NOT receive is help organizing the episodes.
// Measured motivation: without the explicit "no end day = holds forever"
// clause, qwen36 answered false on verbatim repetition positives whose
// observation lay hundreds of days before t_eval (temporal-persistence
// skepticism over an underspecified contract), flooring every LLM
// condition near always-false. See CAMPAIGN-LOG 2026-07-06.
const ragSystemPrompt = `You answer questions about a synthetic world using ONLY the provided episode excerpts.

Episode statements and their exact semantics:
- Observation "valid from day X": the fact holds at EVERY day >= X, forever. Facts never expire on their own.
- Observation "valid from day X until day Y": the fact holds for X <= day < Y.
- Policy (IF conditions THEN conclusion): at the evaluation day, if all conditions hold (matching shared variables like ?A consistently), the conclusion holds. Policies chain: a conclusion may satisfy another policy's condition.
- A policy applies only from its effective day onward, and only if not superseded.
- UNLESS exception: if the exception pattern is satisfiable for that binding, the policy does not fire for it.
- Supersession notice: from the stated day, the old policy stops applying (entirely, or only for bindings matching the stated condition). A superseded policy's conclusions are gone from that day unless re-derivable another way.
- Higher authority policies beat lower authority ones when they conflict; later-issued beats earlier at equal authority.

Answering:
- The question names the evaluation day. Evaluate at exactly that day.
- Answer true only if the fact is stated valid at that day or derivable via policies from facts valid at that day. Otherwise answer false.
- For true/false questions reply with exactly one word: true or false.
- For list questions reply with ONLY a JSON array of entity IDs, e.g. ["customer_03","customer_07"], or [] if none.`

func (r *RAGCondition) buildContext(query string) (string, error) {
	res, err := r.Retriever.Retrieve(query, r.K)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, se := range res {
		b.WriteString(se.Text)
		b.WriteString("\n\n")
	}
	return b.String(), nil
}

func (r *RAGCondition) AnswerHolds(q SanitizedQuery) (bool, error) {
	ctxText, err := r.buildContext(q.Text)
	if err != nil {
		return false, err
	}
	out, err := r.LLM.Complete(context.Background(), ragSystemPrompt, episodesUserPrompt(ctxText, q))
	if err != nil {
		return false, err
	}
	return parseHoldsAnswer(out)
}

func (r *RAGCondition) AnswerFind(q SanitizedQuery) ([]string, error) {
	ctxText, err := r.buildContext(q.Text)
	if err != nil {
		return nil, err
	}
	out, err := r.LLM.Complete(context.Background(), ragSystemPrompt, episodesUserPrompt(ctxText, q))
	if err != nil {
		return nil, err
	}
	return parseFindAnswer(out)
}

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func max(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}
