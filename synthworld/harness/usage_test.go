package harness

import (
	"context"
	"strings"
	"testing"
)

// usageLLM is a fake that reports usage through the rich path.
type usageLLM struct {
	model  string
	resp   string
	prompt int64
	compl  int64
	calls  int
}

func (f *usageLLM) Model() string { return f.model }
func (f *usageLLM) Complete(ctx context.Context, system, user string) (string, error) {
	r, err := f.CompleteUsage(ctx, system, user)
	return r.Text, err
}
func (f *usageLLM) CompleteUsage(_ context.Context, _, _ string) (LLMResponse, error) {
	f.calls++
	return LLMResponse{Text: f.resp, Usage: Usage{PromptTokens: f.prompt, CompletionTokens: f.compl}}, nil
}

func TestMeteredLLMSplitsSpentFromReplayed(t *testing.T) {
	dir := t.TempDir()
	inner := &usageLLM{model: "m1", resp: "true", prompt: 100, compl: 5}
	cached := &CachingLLMClient{Inner: inner, Dir: dir, Mode: CacheAuto, Clock: fixedClock}
	m := NewMeteredLLM(cached)

	// first call: network, tokens spent, cassette recorded (with usage)
	if _, err := m.Complete(context.Background(), "sys", "user"); err != nil {
		t.Fatal(err)
	}
	// second identical call: cassette hit, tokens replayed not spent
	if _, err := m.Complete(context.Background(), "sys", "user"); err != nil {
		t.Fatal(err)
	}
	s := m.Stats()
	if s.Calls != 2 || s.CacheHits != 1 {
		t.Fatalf("calls/hits: %+v", s)
	}
	if s.SpentPrompt != 100 || s.SpentCompletion != 5 {
		t.Fatalf("spent must count only the network call: %+v", s)
	}
	if s.ReplayedPrompt != 100 || s.ReplayedCompletion != 5 {
		t.Fatalf("replayed must count the cassette hit: %+v", s)
	}
	if inner.calls != 1 {
		t.Fatalf("network must be hit once, got %d", inner.calls)
	}
}

func TestMeteredLLMPlainClientFallback(t *testing.T) {
	// a client without CompleteUsage still works; usage is just unknown
	spy := &promptSpy{resp: "false"}
	m := NewMeteredLLM(spy)
	if _, err := m.Complete(context.Background(), "s", "u"); err != nil {
		t.Fatal(err)
	}
	s := m.Stats()
	if s.Calls != 1 || s.SpentPrompt != 0 || s.ReplayedPrompt != 0 {
		t.Fatalf("plain client: %+v", s)
	}
}

func TestUsageTableRendersOnlyMeteredReports(t *testing.T) {
	reports := []*Report{
		{Condition: "oracle"},
		{Condition: "c0-no-memory", Usage: &UsageStats{Calls: 3, SpentPrompt: 42}},
	}
	tbl := UsageTable(reports)
	if !strings.Contains(tbl, "c0-no-memory") || strings.Contains(tbl, "oracle") {
		t.Fatalf("usage table must list metered conditions only:\n%s", tbl)
	}
	if UsageTable([]*Report{{Condition: "oracle"}}) != "" {
		t.Fatal("usage table must be empty for LLM-free runs")
	}
}

func TestC1cSeesWholeCorpusChronologically(t *testing.T) {
	spy := &promptSpy{resp: "true"}
	c := &C1cLongContext{LLM: spy}
	if c.Name() != "c1c-longcontext" {
		t.Fatalf("name: %s", c.Name())
	}
	if err := c.Ingest(testEpisodes()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AnswerHolds(holdsQuery("q1")); err != nil {
		t.Fatal(err)
	}
	u := spy.users[0]
	i1 := strings.Index(u, "Episode ep_001 (day 3)")
	i2 := strings.Index(u, "Episode ep_002 (day 9)")
	if i1 < 0 || i2 < 0 || i2 < i1 {
		t.Fatalf("corpus must contain all episodes in order:\n%s", firstN(u, 400))
	}
	if !strings.Contains(u, "customer_01 enrolled") || !strings.Contains(u, "Policy r1") {
		t.Fatal("corpus must contain every episode's text")
	}
	if !strings.Contains(u, `"discounted"`) {
		t.Fatal("C1c must see the structured atom (fairness, MASTERPLAN §4.2)")
	}
}
