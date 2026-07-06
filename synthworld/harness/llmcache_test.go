package harness

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeLLM counts calls; used to prove the cache short-circuits the network.
type fakeLLM struct {
	model string
	resp  string
	calls int
}

func (f *fakeLLM) Model() string { return f.model }
func (f *fakeLLM) Complete(_ context.Context, _, _ string) (string, error) {
	f.calls++
	return f.resp, nil
}

func fixedClock() time.Time { return time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC) }

func TestCacheAutoRecordsThenReplays(t *testing.T) {
	dir := t.TempDir()
	inner := &fakeLLM{model: "m1", resp: "true"}
	c := &CachingLLMClient{Inner: inner, Dir: dir, Mode: CacheAuto, Clock: fixedClock}

	got, err := c.Complete(context.Background(), "sys", "user")
	if err != nil {
		t.Fatal(err)
	}
	if got != "true" || inner.calls != 1 {
		t.Fatalf("first call: got %q calls %d", got, inner.calls)
	}
	// second identical call must replay, not hit the network
	got, err = c.Complete(context.Background(), "sys", "user")
	if err != nil {
		t.Fatal(err)
	}
	if got != "true" || inner.calls != 1 {
		t.Fatalf("replay: got %q calls %d (want 1)", got, inner.calls)
	}
	// cassette is human-inspectable JSON with the injected timestamp
	key := c.CacheKey("sys", "user")
	raw, err := os.ReadFile(filepath.Join(dir, key+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var cs Cassette
	if err := json.Unmarshal(raw, &cs); err != nil {
		t.Fatal(err)
	}
	if cs.Key != key || cs.Model != "m1" || cs.System != "sys" ||
		cs.User != "user" || cs.Response != "true" ||
		cs.RecordedAt != "2026-07-06T12:00:00Z" {
		t.Fatalf("cassette mismatch: %+v", cs)
	}
}

func TestCacheReplayOffline(t *testing.T) {
	dir := t.TempDir()
	inner := &fakeLLM{model: "m1", resp: "false"}
	rec := &CachingLLMClient{Inner: inner, Dir: dir, Mode: CacheRecord, Clock: fixedClock}
	if _, err := rec.Complete(context.Background(), "s", "u"); err != nil {
		t.Fatal(err)
	}
	// pure replay: no inner client at all, model from ModelID
	rep := &CachingLLMClient{Dir: dir, Mode: CacheReplay, ModelID: "m1"}
	got, err := rep.Complete(context.Background(), "s", "u")
	if err != nil {
		t.Fatal(err)
	}
	if got != "false" {
		t.Fatalf("offline replay: got %q", got)
	}
}

func TestCacheReplayMissErrorsLoudly(t *testing.T) {
	rep := &CachingLLMClient{Dir: t.TempDir(), Mode: CacheReplay, ModelID: "m1"}
	_, err := rep.Complete(context.Background(), "s", "never recorded")
	if err == nil {
		t.Fatal("replay miss must error, never call the network")
	}
	if !strings.Contains(err.Error(), "REPLAY MISS") ||
		!strings.Contains(err.Error(), rep.CacheKey("s", "never recorded")) {
		t.Fatalf("miss error must name the key: %v", err)
	}
}

func TestCacheRecordModeAlwaysCalls(t *testing.T) {
	inner := &fakeLLM{model: "m1", resp: "x"}
	c := &CachingLLMClient{Inner: inner, Dir: t.TempDir(), Mode: CacheRecord, Clock: fixedClock}
	for i := 0; i < 2; i++ {
		if _, err := c.Complete(context.Background(), "s", "u"); err != nil {
			t.Fatal(err)
		}
	}
	if inner.calls != 2 {
		t.Fatalf("record mode must always call the network: calls %d", inner.calls)
	}
}

func TestCacheKeyProperties(t *testing.T) {
	c := &CachingLLMClient{ModelID: "m1"}
	k1 := c.CacheKey("sys", "user")
	if len(k1) != 64 {
		t.Fatalf("key must be sha256 hex (64 chars), got %d", len(k1))
	}
	if k1 != c.CacheKey("sys", "user") {
		t.Fatal("key must be deterministic")
	}
	if k1 == c.CacheKey("sys", "user2") || k1 == c.CacheKey("sys2", "user") {
		t.Fatal("key must vary with prompt content")
	}
	c2 := &CachingLLMClient{ModelID: "m2"}
	if k1 == c2.CacheKey("sys", "user") {
		t.Fatal("key must vary with model")
	}
}
