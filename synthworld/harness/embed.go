package harness

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/vaudience/synthworld/gen"
)

// ---------- OpenAI-compatible embeddings client with mandatory disk cache ----------

// embedCassette is one cached embedding. Same philosophy as the LLM
// cassettes: content-addressed file name, atomic temp+rename writes,
// human-inspectable JSON, replayable offline.
type embedCassette struct {
	Key        string    `json:"key"`
	Model      string    `json:"model"`
	Input      string    `json:"input"`
	Embedding  []float64 `json:"embedding"`
	RecordedAt string    `json:"recorded_at"`
}

// EmbeddingClient talks to an OpenAI-compatible /embeddings endpoint. The
// disk cache is mandatory: embeddings are derived state (spec §7) and every
// registered run must be replayable without the network. With ReplayOnly
// set (or no BaseURL), a cache miss is a loud error, never a network call.
type EmbeddingClient struct {
	BaseURL    string
	APIKey     string
	ModelID    string
	CacheDir   string
	ReplayOnly bool
	BatchSize  int // inputs per API call; default 64
	Timeout    time.Duration
	HTTP       *http.Client
	Clock      func() time.Time
}

func (e *EmbeddingClient) key(input string) string {
	payload, err := json.Marshal(struct {
		Model string `json:"model"`
		Input string `json:"input"`
	}{e.ModelID, input})
	if err != nil {
		panic(fmt.Sprintf("embed: marshal key payload: %v", err))
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func (e *EmbeddingClient) path(key string) string {
	return filepath.Join(e.CacheDir, key+".json")
}

func (e *EmbeddingClient) load(key string) ([]float64, bool, error) {
	raw, err := os.ReadFile(e.path(key))
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("embed cache read %s: %w", key, err)
	}
	var cs embedCassette
	if err := json.Unmarshal(raw, &cs); err != nil {
		return nil, false, fmt.Errorf("embed cache corrupt cassette %s: %w", key, err)
	}
	return cs.Embedding, true, nil
}

func (e *EmbeddingClient) store(cs *embedCassette) error {
	if err := os.MkdirAll(e.CacheDir, 0o755); err != nil {
		return fmt.Errorf("embed cache mkdir: %w", err)
	}
	raw, err := json.Marshal(cs)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(e.CacheDir, cs.Key+".tmp-*")
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
	return os.Rename(tmp.Name(), e.path(cs.Key))
}

func (e *EmbeddingClient) now() time.Time {
	if e.Clock != nil {
		return e.Clock()
	}
	return time.Now()
}

// Embed returns one vector per input, in input order. Cached inputs are
// served from disk; misses are batched to the API (BatchSize per call) and
// recorded. Duplicate inputs are embedded once.
func (e *EmbeddingClient) Embed(ctx context.Context, inputs []string) ([][]float64, error) {
	if e.CacheDir == "" {
		return nil, fmt.Errorf("embed: cache dir is mandatory (registered runs must be replayable)")
	}
	out := make([][]float64, len(inputs))
	missIdx := map[string][]int{} // unique missing input → positions
	var missOrder []string
	for i, in := range inputs {
		vec, ok, err := e.load(e.key(in))
		if err != nil {
			return nil, err
		}
		if ok {
			out[i] = vec
			continue
		}
		if _, seen := missIdx[in]; !seen {
			missOrder = append(missOrder, in)
		}
		missIdx[in] = append(missIdx[in], i)
	}
	if len(missOrder) == 0 {
		return out, nil
	}
	if e.ReplayOnly || e.BaseURL == "" {
		return nil, fmt.Errorf(
			"embed: %d cache misses in replay-only mode (first: %q) — refusing to call the network",
			len(missOrder), firstN(missOrder[0], 120))
	}
	batch := e.BatchSize
	if batch <= 0 {
		batch = 64
	}
	for start := 0; start < len(missOrder); start += batch {
		end := start + batch
		if end > len(missOrder) {
			end = len(missOrder)
		}
		chunk := missOrder[start:end]
		vecs, err := e.embedRemote(ctx, chunk)
		if err != nil {
			return nil, err
		}
		for j, in := range chunk {
			cs := &embedCassette{
				Key: e.key(in), Model: e.ModelID, Input: in,
				Embedding: vecs[j], RecordedAt: e.now().UTC().Format(time.RFC3339),
			}
			if err := e.store(cs); err != nil {
				return nil, err
			}
			for _, pos := range missIdx[in] {
				out[pos] = vecs[j]
			}
		}
	}
	return out, nil
}

func (e *EmbeddingClient) embedRemote(ctx context.Context, inputs []string) ([][]float64, error) {
	if e.HTTP == nil {
		e.HTTP = &http.Client{Timeout: max(e.Timeout, 120*time.Second)}
	}
	body, _ := json.Marshal(map[string]any{"model": e.ModelID, "input": inputs})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(e.BaseURL, "/")+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if e.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.APIKey)
	}
	resp, err := e.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("embed response parse: %w", err)
	}
	if out.Error != nil {
		return nil, fmt.Errorf("embed error: %s", out.Error.Message)
	}
	if len(out.Data) != len(inputs) {
		return nil, fmt.Errorf("embed: sent %d inputs, got %d embeddings (status %d)",
			len(inputs), len(out.Data), resp.StatusCode)
	}
	vecs := make([][]float64, len(inputs))
	for _, d := range out.Data {
		if d.Index < 0 || d.Index >= len(inputs) {
			return nil, fmt.Errorf("embed: response index %d out of range", d.Index)
		}
		vecs[d.Index] = d.Embedding
	}
	for i, v := range vecs {
		if v == nil {
			return nil, fmt.Errorf("embed: no embedding returned for input %d", i)
		}
	}
	return vecs, nil
}

// ---------- Embedding (semantic) retriever ----------

// EmbeddingRetriever ranks episodes by cosine similarity between the query
// embedding and episode-text embeddings. The semantic side of the H1
// question: does embedding retrieval reach provenance that BM25's lexical
// match cannot (especially revision, where the supersession notice shares no
// vocabulary with the question)?
type EmbeddingRetriever struct {
	Client *EmbeddingClient

	episodes []gen.Episode
	vecs     [][]float64
}

func (r *EmbeddingRetriever) Name() string { return "embed-" + r.Client.ModelID }

func (r *EmbeddingRetriever) Index(episodes []gen.Episode) error {
	// full reset: Index must be idempotent (probe re-indexes per k)
	r.episodes = episodes
	r.vecs = nil
	texts := make([]string, len(episodes))
	for i, ep := range episodes {
		texts[i] = ep.Text
	}
	vecs, err := r.Client.Embed(context.Background(), texts)
	if err != nil {
		return fmt.Errorf("embed index: %w", err)
	}
	r.vecs = vecs
	return nil
}

func cosine(a, b []float64) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func (r *EmbeddingRetriever) Retrieve(query string, k int) ([]ScoredEpisode, error) {
	qv, err := r.Client.Embed(context.Background(), []string{query})
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	scores := make([]float64, len(r.episodes))
	for i, v := range r.vecs {
		scores[i] = cosine(qv[0], v)
	}
	idx := make([]int, len(r.episodes))
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(a, c int) bool {
		if scores[idx[a]] != scores[idx[c]] {
			return scores[idx[a]] > scores[idx[c]]
		}
		return r.episodes[idx[a]].ID < r.episodes[idx[c]].ID
	})
	if k > len(idx) {
		k = len(idx)
	}
	out := make([]ScoredEpisode, 0, k)
	for _, i := range idx[:k] {
		out = append(out, ScoredEpisode{EpisodeID: r.episodes[i].ID, Score: scores[i], Text: r.episodes[i].Text})
	}
	return out, nil
}

// ---------- Hybrid retriever (Reciprocal Rank Fusion) ----------

// rrfC is the standard RRF constant (Cormack et al.): score(d) = Σ 1/(60+rank).
const rrfC = 60.0

// HybridRetriever fuses component rankings with Reciprocal Rank Fusion.
// Each component contributes 1/(60+rank) per document; ties break by
// episode ID. Components are consulted to a fetch depth deeper than k so
// fusion has real rankings to work with.
type HybridRetriever struct {
	Components []Retriever
	Label      string // e.g. "hybrid-bm25-embed"
	FetchK     int    // per-component depth; default max(32, 4k)
}

func (h *HybridRetriever) Name() string {
	if h.Label != "" {
		return h.Label
	}
	var names []string
	for _, c := range h.Components {
		names = append(names, c.Name())
	}
	return "hybrid-" + strings.Join(names, "-")
}

func (h *HybridRetriever) Index(episodes []gen.Episode) error {
	for _, c := range h.Components {
		if err := c.Index(episodes); err != nil {
			return fmt.Errorf("hybrid index %s: %w", c.Name(), err)
		}
	}
	return nil
}

func (h *HybridRetriever) Retrieve(query string, k int) ([]ScoredEpisode, error) {
	fetch := h.FetchK
	if fetch <= 0 {
		fetch = 4 * k
		if fetch < 32 {
			fetch = 32
		}
	}
	fused := map[string]float64{}
	texts := map[string]string{}
	for _, c := range h.Components {
		res, err := c.Retrieve(query, fetch)
		if err != nil {
			return nil, fmt.Errorf("hybrid retrieve %s: %w", c.Name(), err)
		}
		for rank, se := range res {
			fused[se.EpisodeID] += 1.0 / (rrfC + float64(rank+1))
			if _, ok := texts[se.EpisodeID]; !ok {
				texts[se.EpisodeID] = se.Text
			}
		}
	}
	ids := make([]string, 0, len(fused))
	for id := range fused {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(a, b int) bool {
		if fused[ids[a]] != fused[ids[b]] {
			return fused[ids[a]] > fused[ids[b]]
		}
		return ids[a] < ids[b]
	})
	if k > len(ids) {
		k = len(ids)
	}
	out := make([]ScoredEpisode, 0, k)
	for _, id := range ids[:k] {
		out = append(out, ScoredEpisode{EpisodeID: id, Score: fused[id], Text: texts[id]})
	}
	return out, nil
}
