package harness

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
)

// ---------- Token/usage accounting ----------

// Usage is token consumption for one LLM exchange, as reported by the
// endpoint. Zero-valued when the endpoint reports nothing.
type Usage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
}

// LLMResponse is the rich result of a completion: the text, the token usage,
// and whether it was served from a cassette (a replayed token is not a spent
// token — the distinction is what makes the cost table honest).
type LLMResponse struct {
	Text     string
	Usage    Usage
	CacheHit bool
}

// usageCompleter is the optional rich completion surface. LLMClient stays
// the minimal interface conditions program against; clients that know their
// usage (OpenAICompatClient, CachingLLMClient, MeteredLLM) also implement
// this, and completeUsage upgrades when possible.
type usageCompleter interface {
	CompleteUsage(ctx context.Context, system, user string) (LLMResponse, error)
}

// completeUsage calls the rich path when the client supports it, else falls
// back to plain Complete with unknown (zero) usage.
func completeUsage(c LLMClient, ctx context.Context, system, user string) (LLMResponse, error) {
	if uc, ok := c.(usageCompleter); ok {
		return uc.CompleteUsage(ctx, system, user)
	}
	text, err := c.Complete(ctx, system, user)
	return LLMResponse{Text: text}, err
}

// UsageStats aggregates LLM consumption for one condition. Spent = tokens
// that hit the network this run; Replayed = tokens served from cassettes.
type UsageStats struct {
	Calls              int64 `json:"calls"`
	Errors             int64 `json:"errors"`
	CacheHits          int64 `json:"cache_hits"`
	SpentPrompt        int64 `json:"spent_prompt_tokens"`
	SpentCompletion    int64 `json:"spent_completion_tokens"`
	ReplayedPrompt     int64 `json:"replayed_prompt_tokens"`
	ReplayedCompletion int64 `json:"replayed_completion_tokens"`
}

// MeteredLLM wraps an LLMClient and counts calls and tokens. cmd/harness
// gives each LLM-backed condition its own meter (around a shared cache), so
// usage is attributable per condition. Counters are atomic: conditions may
// be driven by concurrent workers (HARNESS_CONCURRENCY).
type MeteredLLM struct {
	Inner LLMClient

	calls, errs, hits                  atomic.Int64
	spentPrompt, spentCompletion       atomic.Int64
	replayedPrompt, replayedCompletion atomic.Int64
}

func NewMeteredLLM(inner LLMClient) *MeteredLLM { return &MeteredLLM{Inner: inner} }

func (m *MeteredLLM) Model() string { return m.Inner.Model() }

func (m *MeteredLLM) CompleteUsage(ctx context.Context, system, user string) (LLMResponse, error) {
	m.calls.Add(1)
	resp, err := completeUsage(m.Inner, ctx, system, user)
	if err != nil {
		m.errs.Add(1)
		return resp, err
	}
	if resp.CacheHit {
		m.hits.Add(1)
		m.replayedPrompt.Add(resp.Usage.PromptTokens)
		m.replayedCompletion.Add(resp.Usage.CompletionTokens)
	} else {
		m.spentPrompt.Add(resp.Usage.PromptTokens)
		m.spentCompletion.Add(resp.Usage.CompletionTokens)
	}
	return resp, nil
}

func (m *MeteredLLM) Complete(ctx context.Context, system, user string) (string, error) {
	resp, err := m.CompleteUsage(ctx, system, user)
	return resp.Text, err
}

// UsageTable renders per-condition token accounting for reports that carry
// usage. Spent tokens hit the network this run; replayed came from
// cassettes. Empty string when no report has usage (LLM-free run).
func UsageTable(reports []*Report) string {
	var b strings.Builder
	any := false
	for _, r := range reports {
		if r.Usage != nil {
			any = true
			break
		}
	}
	if !any {
		return ""
	}
	fmt.Fprintf(&b, "%-22s %7s %7s %6s %13s %13s %16s %16s\n",
		"condition", "calls", "hits", "errs", "spent-prompt", "spent-compl", "replayed-prompt", "replayed-compl")
	for _, r := range reports {
		u := r.Usage
		if u == nil {
			continue
		}
		fmt.Fprintf(&b, "%-22s %7d %7d %6d %13d %13d %16d %16d\n",
			r.Condition, u.Calls, u.CacheHits, u.Errors,
			u.SpentPrompt, u.SpentCompletion, u.ReplayedPrompt, u.ReplayedCompletion)
	}
	return b.String()
}

// Stats snapshots the counters.
func (m *MeteredLLM) Stats() *UsageStats {
	return &UsageStats{
		Calls:              m.calls.Load(),
		Errors:             m.errs.Load(),
		CacheHits:          m.hits.Load(),
		SpentPrompt:        m.spentPrompt.Load(),
		SpentCompletion:    m.spentCompletion.Load(),
		ReplayedPrompt:     m.replayedPrompt.Load(),
		ReplayedCompletion: m.replayedCompletion.Load(),
	}
}
