package harness

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// captureChatServer records the raw request body it receives and returns a
// minimal valid completion.
func captureChatServer(t *testing.T, captured *map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("bad body: %v", err)
		}
		*captured = body
		w.Write([]byte(`{"choices":[{"message":{"content":"true"}}],"usage":{"prompt_tokens":7,"completion_tokens":1}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestShapeDefaultSendsTemperatureZero(t *testing.T) {
	var got map[string]any
	srv := captureChatServer(t, &got)
	c := &OpenAICompatClient{BaseURL: srv.URL, ModelID: "m"}
	resp, err := c.CompleteUsage(context.Background(), "s", "u")
	if err != nil {
		t.Fatal(err)
	}
	if temp, ok := got["temperature"].(float64); !ok || temp != 0 {
		t.Fatalf("legacy default must send temperature:0, body: %v", got)
	}
	if _, ok := got["reasoning_effort"]; ok {
		t.Fatal("no extra params by default")
	}
	if resp.Usage.PromptTokens != 7 || resp.Usage.CompletionTokens != 1 {
		t.Fatalf("usage must flow through: %+v", resp.Usage)
	}
}

func TestShapeOmitTemperatureAndExtraParams(t *testing.T) {
	var got map[string]any
	srv := captureChatServer(t, &got)
	c := &OpenAICompatClient{BaseURL: srv.URL, ModelID: "gpt-5-mini",
		Shape: RequestShape{OmitTemperature: true, ExtraParams: map[string]any{"reasoning_effort": "minimal"}}}
	if _, err := c.CompleteUsage(context.Background(), "s", "u"); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["temperature"]; ok {
		t.Fatalf("OmitTemperature must drop the field entirely, body: %v", got)
	}
	if got["reasoning_effort"] != "minimal" {
		t.Fatalf("extra params must merge top-level, body: %v", got)
	}
}

func TestShapeNestedExtraParams(t *testing.T) {
	var got map[string]any
	srv := captureChatServer(t, &got)
	c := &OpenAICompatClient{BaseURL: srv.URL, ModelID: "qwen",
		Shape: RequestShape{ExtraParams: map[string]any{"chat_template_kwargs": map[string]any{"enable_thinking": false}}}}
	if _, err := c.CompleteUsage(context.Background(), "s", "u"); err != nil {
		t.Fatal(err)
	}
	ctk, ok := got["chat_template_kwargs"].(map[string]any)
	if !ok || ctk["enable_thinking"] != false {
		t.Fatalf("nested extra params must survive intact, body: %v", got)
	}
	if temp, ok := got["temperature"].(float64); !ok || temp != 0 {
		t.Fatal("extra params must not disturb the temperature default")
	}
}

func TestShapeExtraParamsCannotOverrideModelOrMessages(t *testing.T) {
	var got map[string]any
	srv := captureChatServer(t, &got)
	c := &OpenAICompatClient{BaseURL: srv.URL, ModelID: "real-model",
		Shape: RequestShape{ExtraParams: map[string]any{"model": "evil", "messages": "gone"}}}
	if _, err := c.CompleteUsage(context.Background(), "s", "u"); err != nil {
		t.Fatal(err)
	}
	if got["model"] != "real-model" {
		t.Fatalf("model must be protected, body: %v", got)
	}
	if _, ok := got["messages"].([]any); !ok {
		t.Fatalf("messages must be protected, body: %v", got)
	}
}

func TestCacheKeyVariesWithShape(t *testing.T) {
	base := &CachingLLMClient{ModelID: "m"}
	k0 := base.CacheKey("s", "u")

	omit := &CachingLLMClient{ModelID: "m", Shape: RequestShape{OmitTemperature: true}}
	if omit.CacheKey("s", "u") == k0 {
		t.Fatal("omitting temperature must change the key (omitted ≠ zero)")
	}
	effort := &CachingLLMClient{ModelID: "m",
		Shape: RequestShape{ExtraParams: map[string]any{"reasoning_effort": "minimal"}}}
	if effort.CacheKey("s", "u") == k0 {
		t.Fatal("extra params must be part of the key")
	}
	nested := &CachingLLMClient{ModelID: "m",
		Shape: RequestShape{ExtraParams: map[string]any{"chat_template_kwargs": map[string]any{"enable_thinking": false}}}}
	if nested.CacheKey("s", "u") == effort.CacheKey("s", "u") {
		t.Fatal("different extra params must produce different keys")
	}
	// determinism across instances with equal shapes
	effort2 := &CachingLLMClient{ModelID: "m",
		Shape: RequestShape{ExtraParams: map[string]any{"reasoning_effort": "minimal"}}}
	if effort.CacheKey("s", "u") != effort2.CacheKey("s", "u") {
		t.Fatal("equal shapes must produce equal keys")
	}
}

// TestCacheKeyMatchesSentPayload is the drift guard: the key must hash the
// byte-identical JSON of the payload the HTTP client sends.
func TestCacheKeyMatchesSentPayload(t *testing.T) {
	shape := RequestShape{OmitTemperature: true,
		ExtraParams: map[string]any{"reasoning_effort": "low"}}
	sent, err := json.Marshal(shape.buildPayload("m", "s", "u"))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	srv := captureChatServer(t, &got)
	c := &OpenAICompatClient{BaseURL: srv.URL, ModelID: "m", Shape: shape}
	if _, err := c.CompleteUsage(context.Background(), "s", "u"); err != nil {
		t.Fatal(err)
	}
	roundTrip, _ := json.Marshal(got)
	var a, b map[string]any
	json.Unmarshal(sent, &a)
	json.Unmarshal(roundTrip, &b)
	as, _ := json.Marshal(a)
	bs, _ := json.Marshal(b)
	if string(as) != string(bs) {
		t.Fatalf("key payload and sent payload drifted:\nkey:  %s\nsent: %s", as, bs)
	}
}
