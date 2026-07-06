package harness

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ---------- LLM record/replay cache ----------

// CacheMode selects cassette behavior.
//
//	CacheAuto:   replay on hit, call the network and record on miss.
//	CacheRecord: always call the network, always (re)write the cassette.
//	CacheReplay: never call the network; a miss is a loud error. This is the
//	             mode for registered runs and the reproduction package — a
//	             cache miss there means the run is not the run that was
//	             recorded, and must be visible, never silently papered over
//	             by a live call.
type CacheMode string

const (
	CacheAuto   CacheMode = "auto"
	CacheRecord CacheMode = "record"
	CacheReplay CacheMode = "replay"
)

// Cassette is one recorded LLM exchange. One JSON file per key: independent
// files dedupe naturally on the content-addressed name, survive concurrent
// writers (temp file + rename), and stay individually human-inspectable and
// deletable — a single journal file would need locking and merging across
// parallel condition runs for no benefit at this scale.
type Cassette struct {
	Key        string `json:"key"`
	Model      string `json:"model"`
	System     string `json:"system"`
	User       string `json:"user"`
	Response   string `json:"response"`
	Usage      *Usage `json:"usage,omitempty"` // as reported when recorded; replayed, not re-spent
	RecordedAt string `json:"recorded_at"`     // RFC3339; informational only, never part of the key
}

// CachingLLMClient wraps an LLMClient with a content-addressed cassette
// store. The key is SHA-256 over the canonical request payload (model,
// messages, temperature) — the same logical request OpenAICompatClient
// sends — so a cassette is invalidated exactly when the request changes.
type CachingLLMClient struct {
	Inner   LLMClient    // may be nil in CacheReplay mode (pure offline replay)
	Dir     string       // cassette directory; created on first write
	Mode    CacheMode    // defaults to CacheAuto
	ModelID string       // required when Inner is nil; else defaults to Inner.Model()
	Shape   RequestShape // must mirror the inner client's shaping — the key hashes what is sent
	Clock   func() time.Time

	// per-key locks: concurrent workers on distinct prompts proceed in
	// parallel; two workers racing the same key make one network call.
	// File writes are atomic (temp+rename) regardless.
	locks sync.Map // key string → *sync.Mutex
}

func (c *CachingLLMClient) keyLock(key string) *sync.Mutex {
	m, _ := c.locks.LoadOrStore(key, &sync.Mutex{})
	return m.(*sync.Mutex)
}

func (c *CachingLLMClient) Model() string {
	if c.ModelID != "" {
		return c.ModelID
	}
	if c.Inner != nil {
		return c.Inner.Model()
	}
	return ""
}

func (c *CachingLLMClient) mode() CacheMode {
	if c.Mode == "" {
		return CacheAuto
	}
	return c.Mode
}

func (c *CachingLLMClient) now() time.Time {
	if c.Clock != nil {
		return c.Clock()
	}
	return time.Now()
}

// CacheKey is the content address of a request: SHA-256 over the canonical
// JSON of the EXACT request payload (RequestShape.buildPayload — the same
// map the HTTP client marshals; json.Marshal sorts map keys, so the bytes
// are canonical). An omitted field is absent from the hash, not zero-valued,
// so a cassette is invalidated precisely when the request shape changes —
// temperature, reasoning_effort, chat_template_kwargs, anything.
func (c *CachingLLMClient) CacheKey(system, user string) string {
	raw, err := json.Marshal(c.Shape.buildPayload(c.Model(), system, user))
	if err != nil {
		panic(fmt.Sprintf("llmcache: marshal key payload (ExtraParams must be JSON-encodable): %v", err))
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (c *CachingLLMClient) path(key string) string {
	return filepath.Join(c.Dir, key+".json")
}

func (c *CachingLLMClient) load(key string) (*Cassette, bool, error) {
	raw, err := os.ReadFile(c.path(key))
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("llmcache read %s: %w", key, err)
	}
	var cs Cassette
	if err := json.Unmarshal(raw, &cs); err != nil {
		return nil, false, fmt.Errorf("llmcache corrupt cassette %s: %w", key, err)
	}
	return &cs, true, nil
}

func (c *CachingLLMClient) store(cs *Cassette) error {
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return fmt.Errorf("llmcache mkdir: %w", err)
	}
	raw, err := json.MarshalIndent(cs, "", "  ")
	if err != nil {
		return err
	}
	// temp + rename: atomic against concurrent writers of the same key
	tmp, err := os.CreateTemp(c.Dir, cs.Key+".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), c.path(cs.Key))
}

func (c *CachingLLMClient) Complete(ctx context.Context, system, user string) (string, error) {
	resp, err := c.CompleteUsage(ctx, system, user)
	return resp.Text, err
}

func (c *CachingLLMClient) CompleteUsage(ctx context.Context, system, user string) (LLMResponse, error) {
	key := c.CacheKey(system, user)
	lk := c.keyLock(key)
	lk.Lock()
	defer lk.Unlock()

	mode := c.mode()

	if mode != CacheRecord {
		cs, ok, err := c.load(key)
		if err != nil {
			return LLMResponse{}, err
		}
		if ok {
			out := LLMResponse{Text: cs.Response, CacheHit: true}
			if cs.Usage != nil {
				out.Usage = *cs.Usage
			}
			return out, nil
		}
		if mode == CacheReplay {
			return LLMResponse{}, fmt.Errorf(
				"llmcache: REPLAY MISS key=%s model=%s user-prompt=%q — this run is not the recorded run; refusing to call the network",
				key, c.Model(), firstN(user, 120))
		}
	}

	if c.Inner == nil {
		return LLMResponse{}, fmt.Errorf("llmcache: no inner LLM client (mode=%s, key=%s)", mode, key)
	}
	resp, err := completeUsage(c.Inner, ctx, system, user)
	if err != nil {
		return LLMResponse{}, err
	}
	cs := &Cassette{
		Key: key, Model: c.Model(), System: system, User: user,
		Response: resp.Text, RecordedAt: c.now().UTC().Format(time.RFC3339),
	}
	if resp.Usage != (Usage{}) {
		cs.Usage = &resp.Usage
	}
	if err := c.store(cs); err != nil {
		return LLMResponse{}, err
	}
	return LLMResponse{Text: resp.Text, Usage: resp.Usage}, nil
}
