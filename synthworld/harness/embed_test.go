package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/vaudience/synthworld/gen"
)

// fakeEmbedServer serves deterministic embeddings: a 4-dim vector derived
// from the input string, so tests can predict similarities. It counts
// requests and total inputs to prove batching and caching.
func fakeEmbedServer(t *testing.T) (*httptest.Server, *int, *int) {
	t.Helper()
	requests, inputs := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("bad request body: %v", err)
		}
		requests++
		inputs += len(req.Input)
		type datum struct {
			Index     int       `json:"index"`
			Embedding []float64 `json:"embedding"`
		}
		var data []datum
		for i, in := range req.Input {
			data = append(data, datum{Index: i, Embedding: vecFor(in)})
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(srv.Close)
	return srv, &requests, &inputs
}

// vecFor maps a string to a deterministic 4-dim unit-ish vector. Strings
// sharing a prefix letter cluster; that is all the geometry tests need.
func vecFor(s string) []float64 {
	v := []float64{1, 0, 0, 0}
	if strings.Contains(s, "beta") {
		v = []float64{0, 1, 0, 0}
	}
	if strings.Contains(s, "gamma") {
		v = []float64{0, 0, 1, 0}
	}
	if strings.Contains(s, "mix") {
		v = []float64{0.7, 0.7, 0, 0}
	}
	return v
}

func TestEmbedCacheRoundTripAndBatching(t *testing.T) {
	srv, reqs, inputs := fakeEmbedServer(t)
	dir := t.TempDir()
	c := &EmbeddingClient{BaseURL: srv.URL, ModelID: "fake-embed", CacheDir: dir, BatchSize: 2}

	in := []string{"alpha one", "beta two", "gamma three", "alpha one", "beta four", "gamma five"}
	got, err := c.Embed(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(in) {
		t.Fatalf("want %d vectors, got %d", len(in), len(got))
	}
	// duplicate input embedded once: 5 unique inputs, batch 2 → 3 requests
	if *reqs != 3 || *inputs != 5 {
		t.Fatalf("batching: %d requests / %d inputs (want 3/5)", *reqs, *inputs)
	}
	if !reflect.DeepEqual(got[0], got[3]) {
		t.Fatal("duplicate inputs must get identical vectors")
	}

	// second client, same cache dir, NO server: pure replay
	offline := &EmbeddingClient{ModelID: "fake-embed", CacheDir: dir, ReplayOnly: true}
	again, err := offline.Embed(context.Background(), in)
	if err != nil {
		t.Fatalf("offline replay: %v", err)
	}
	if !reflect.DeepEqual(got, again) {
		t.Fatal("replayed vectors must equal recorded vectors")
	}

	// replay miss is a loud error, never a network call
	if _, err := offline.Embed(context.Background(), []string{"never seen"}); err == nil ||
		!strings.Contains(err.Error(), "cache miss") {
		t.Fatalf("replay miss must error loudly, got %v", err)
	}
}

func TestEmbedCacheKeyVariesWithModel(t *testing.T) {
	a := &EmbeddingClient{ModelID: "m1"}
	b := &EmbeddingClient{ModelID: "m2"}
	if a.key("x") == b.key("x") {
		t.Fatal("key must vary with model")
	}
	if a.key("x") != a.key("x") || a.key("x") == a.key("y") {
		t.Fatal("key must be deterministic and vary with input")
	}
}

func embedEpisodes() []gen.Episode {
	return []gen.Episode{
		{ID: "ep_01", Day: 1, Text: "alpha topic report"},
		{ID: "ep_02", Day: 2, Text: "beta topic report"},
		{ID: "ep_03", Day: 3, Text: "gamma topic report"},
		{ID: "ep_04", Day: 4, Text: "mix of things"},
	}
}

func TestEmbeddingRetrieverRankingAndTieBreak(t *testing.T) {
	srv, _, _ := fakeEmbedServer(t)
	r := &EmbeddingRetriever{Client: &EmbeddingClient{BaseURL: srv.URL, ModelID: "fake-embed", CacheDir: t.TempDir()}}
	if r.Name() != "embed-fake-embed" {
		t.Fatalf("name: %s", r.Name())
	}
	if err := r.Index(embedEpisodes()); err != nil {
		t.Fatal(err)
	}
	res, err := r.Retrieve("beta question", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 || res[0].EpisodeID != "ep_02" {
		t.Fatalf("beta query must rank ep_02 first: %+v", res)
	}
	// mix (0.7,0.7,..) has cosine ~0.707 to beta — second place
	if res[1].EpisodeID != "ep_04" {
		t.Fatalf("mix episode must rank second: %+v", res)
	}

	// tie-break: alpha and a duplicate-text episode score identically → ID order
	eps := []gen.Episode{
		{ID: "ep_b", Day: 1, Text: "alpha same"},
		{ID: "ep_a", Day: 2, Text: "alpha same"},
	}
	if err := r.Index(eps); err != nil {
		t.Fatal(err)
	}
	res, err = r.Retrieve("alpha q", 2)
	if err != nil {
		t.Fatal(err)
	}
	if res[0].EpisodeID != "ep_a" || res[1].EpisodeID != "ep_b" {
		t.Fatalf("equal scores must tie-break by episode ID: %+v", res)
	}
}

// stubRetriever returns a fixed ranking regardless of query.
type stubRetriever struct {
	name    string
	ranking []string
}

func (s *stubRetriever) Name() string              { return s.name }
func (s *stubRetriever) Index([]gen.Episode) error { return nil }
func (s *stubRetriever) Retrieve(_ string, k int) ([]ScoredEpisode, error) {
	var out []ScoredEpisode
	for i, id := range s.ranking {
		if i >= k {
			break
		}
		out = append(out, ScoredEpisode{EpisodeID: id, Score: float64(len(s.ranking) - i), Text: "t-" + id})
	}
	return out, nil
}

func TestHybridRRFArithmetic(t *testing.T) {
	// component A ranks x,y,z; component B ranks z,y,x.
	// RRF(60): x = 1/61 + 1/63; y = 1/62 + 1/62; z = 1/63 + 1/61.
	// x and z tie exactly → ID order; y loses (1/61+1/63 > 2/62).
	h := &HybridRetriever{
		Components: []Retriever{
			&stubRetriever{name: "A", ranking: []string{"x", "y", "z"}},
			&stubRetriever{name: "B", ranking: []string{"z", "y", "x"}},
		},
		Label: "hybrid-test",
	}
	res, err := h.Retrieve("q", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 3 {
		t.Fatalf("want 3 results, got %d", len(res))
	}
	wantScoreXZ := 1.0/61 + 1.0/63
	wantScoreY := 2.0 / 62
	if res[0].EpisodeID != "x" || res[1].EpisodeID != "z" {
		t.Fatalf("tied x/z must come first in ID order: %+v", res)
	}
	if diff := res[0].Score - wantScoreXZ; diff > 1e-12 || diff < -1e-12 {
		t.Fatalf("x score %v want %v", res[0].Score, wantScoreXZ)
	}
	if res[2].EpisodeID != "y" {
		t.Fatalf("y must rank last: %+v", res)
	}
	if diff := res[2].Score - wantScoreY; diff > 1e-12 || diff < -1e-12 {
		t.Fatalf("y score %v want %v", res[2].Score, wantScoreY)
	}
	// determinism
	res2, _ := h.Retrieve("q", 3)
	if !reflect.DeepEqual(res, res2) {
		t.Fatal("hybrid retrieval must be deterministic")
	}
}

func TestHybridSurvivesDisjointComponents(t *testing.T) {
	h := &HybridRetriever{Components: []Retriever{
		&stubRetriever{name: "A", ranking: []string{"a1"}},
		&stubRetriever{name: "B", ranking: []string{"b1"}},
	}}
	res, err := h.Retrieve("q", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 || res[0].EpisodeID != "a1" || res[1].EpisodeID != "b1" {
		t.Fatalf("disjoint components fuse by RRF then ID: %+v", res)
	}
	if h.Name() != "hybrid-A-B" {
		t.Fatalf("derived name: %s", h.Name())
	}
}

func TestCosine(t *testing.T) {
	cases := []struct {
		a, b []float64
		want float64
	}{
		{[]float64{1, 0}, []float64{1, 0}, 1},
		{[]float64{1, 0}, []float64{0, 1}, 0},
		{[]float64{1, 0}, []float64{-1, 0}, -1},
		{[]float64{0, 0}, []float64{1, 0}, 0}, // zero vector guarded
		{[]float64{1}, []float64{1, 0}, 0},    // length mismatch guarded
	}
	for i, c := range cases {
		got := cosine(c.a, c.b)
		if diff := got - c.want; diff > 1e-12 || diff < -1e-12 {
			t.Fatalf("case %d: cosine=%v want %v", i, got, c.want)
		}
	}
}

func TestEmbedRequiresCacheDir(t *testing.T) {
	c := &EmbeddingClient{BaseURL: "http://unused", ModelID: "m"}
	if _, err := c.Embed(context.Background(), []string{"x"}); err == nil ||
		!strings.Contains(err.Error(), "mandatory") {
		t.Fatalf("missing cache dir must be a loud error, got %v", err)
	}
}

func TestEmbedServerErrorSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "quota exceeded"}})
	}))
	defer srv.Close()
	c := &EmbeddingClient{BaseURL: srv.URL, ModelID: "m", CacheDir: t.TempDir()}
	_, err := c.Embed(context.Background(), []string{"x"})
	if err == nil || !strings.Contains(err.Error(), "quota exceeded") {
		t.Fatalf("server error must surface: %v", err)
	}
}

func TestEmbedCountMismatchIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[]}`)
	}))
	defer srv.Close()
	c := &EmbeddingClient{BaseURL: srv.URL, ModelID: "m", CacheDir: t.TempDir()}
	if _, err := c.Embed(context.Background(), []string{"x"}); err == nil {
		t.Fatal("embedding count mismatch must error")
	}
}
