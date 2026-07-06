package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"regexp"
	"sort"
	"strings"

	"github.com/vaudience/synthworld/gen"
)

// ---------- Retriever ----------

type ScoredEpisode struct {
	EpisodeID string
	Score     float64
	Text      string
}

// Retriever returns the top-k episodes for a natural-language query. C1
// conditions compose a Retriever with an LLM; the provenance probe measures
// a Retriever alone.
type Retriever interface {
	Name() string
	Index(episodes []gen.Episode) error
	Retrieve(query string, k int) ([]ScoredEpisode, error)
}

// ---------- BM25 lexical retriever (pure Go, runs anywhere) ----------

type BM25Retriever struct {
	episodes []gen.Episode
	docs     []map[string]int // term frequencies per episode
	docLen   []int
	df       map[string]int
	avgLen   float64
}

func (r *BM25Retriever) Name() string { return "bm25" }

var tokenRe = regexp.MustCompile(`[a-z0-9_]+`)

func tokenize(s string) []string {
	return tokenRe.FindAllString(strings.ToLower(s), -1)
}

func (r *BM25Retriever) Index(episodes []gen.Episode) error {
	// full reset: Index must be idempotent (the probe re-indexes per k)
	r.episodes = episodes
	r.df = map[string]int{}
	r.docs = nil
	r.docLen = nil
	r.avgLen = 0
	total := 0
	for _, ep := range episodes {
		tf := map[string]int{}
		toks := tokenize(ep.Text)
		for _, t := range toks {
			tf[t]++
		}
		for t := range tf {
			r.df[t]++
		}
		r.docs = append(r.docs, tf)
		r.docLen = append(r.docLen, len(toks))
		total += len(toks)
	}
	if len(episodes) > 0 {
		r.avgLen = float64(total) / float64(len(episodes))
	}
	return nil
}

func (r *BM25Retriever) Retrieve(query string, k int) ([]ScoredEpisode, error) {
	const k1, b = 1.2, 0.75
	n := float64(len(r.episodes))
	qTerms := tokenize(query)
	scores := make([]float64, len(r.episodes))
	for _, t := range qTerms {
		df, ok := r.df[t]
		if !ok {
			continue
		}
		idf := math.Log(1 + (n-float64(df)+0.5)/(float64(df)+0.5))
		for i, tf := range r.docs {
			f := float64(tf[t])
			if f == 0 {
				continue
			}
			norm := f * (k1 + 1) / (f + k1*(1-b+b*float64(r.docLen[i])/r.avgLen))
			scores[i] += idf * norm
		}
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
		if scores[i] == 0 {
			break
		}
		out = append(out, ScoredEpisode{EpisodeID: r.episodes[i].ID, Score: scores[i], Text: r.episodes[i].Text})
	}
	return out, nil
}

// ---------- tmr shell-out retriever ----------

// TmrRetriever shells out to the tmr CLI over a folder of exported memos
// (cmd/memoexport). Index is a no-op: run `tmr ingest <folder>` beforehand.
// Requires tmr's embedder credentials in the environment; intended for
// Toni-side infrastructure, not this container.
//
// Envelope verified against tmr v0.34 live output (2026-07-06):
// {data:[{chunk_id, memo_id, score, vector_score, lexical_score, text, ...}]}.
// Episode identity travels as memo_id with the mandatory "mem_" prefix
// (cmd/memoexport writes `id: mem_<episodeID>`); chunking can return several
// chunks of one memo, so hits are deduped by episode keeping the best score.
// Requires the embedder credential (BABYLON_EMBED_KEY) in the environment
// for semantic/hybrid modes; inherited by the child process.
type TmrRetriever struct {
	Binary string // path to tmr binary
	Folder string // memo folder (already ingested)
	Mode   string // semantic | lexical | hybrid
}

func (r *TmrRetriever) Name() string { return "tmr-" + r.Mode }

func (r *TmrRetriever) Index([]gen.Episode) error { return nil }

func (r *TmrRetriever) Retrieve(query string, k int) ([]ScoredEpisode, error) {
	args := []string{"retrieve", r.Folder, "--query", query, "--top-k", fmt.Sprint(k), "--json"}
	if r.Mode != "" && r.Mode != "semantic" {
		args = append(args, "--mode", r.Mode)
	}
	cmd := exec.Command(r.Binary, args...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("tmr retrieve: %w: %s", err, errb.String())
	}
	// Primary path: memo_id ("mem_<episodeID>", the verified real envelope).
	// Legacy fallbacks (episode_id top-level or under metadata) kept for
	// older exports. Failures are loud: an unexpected envelope shape must
	// never read as "zero results".
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		return nil, fmt.Errorf("tmr output parse: %w; raw: %s", err, firstN(out.String(), 300))
	}
	if envelope.Data == nil {
		return nil, fmt.Errorf("tmr envelope has no 'data' field; raw: %s", firstN(out.String(), 300))
	}
	var items []map[string]any
	if err := json.Unmarshal(envelope.Data, &items); err != nil {
		return nil, fmt.Errorf("tmr 'data' is not an object array: %w; raw: %s", err, firstN(out.String(), 300))
	}
	var res []ScoredEpisode
	seen := make(map[string]bool)
	for _, item := range items {
		se := ScoredEpisode{}
		if v, ok := item["memo_id"].(string); ok && strings.HasPrefix(v, "mem_") {
			se.EpisodeID = strings.TrimPrefix(v, "mem_")
		} else if v, ok := item["episode_id"].(string); ok {
			se.EpisodeID = v
		} else if meta, ok := item["metadata"].(map[string]any); ok {
			if v, ok := meta["episode_id"].(string); ok {
				se.EpisodeID = v
			}
		}
		if v, ok := item["score"].(float64); ok {
			se.Score = v
		}
		if v, ok := item["content"].(string); ok {
			se.Text = v
		} else if v, ok := item["text"].(string); ok {
			se.Text = v
		}
		// tmr returns chunks; several chunks of one memo map to the same
		// episode. Results arrive score-descending, so first wins.
		if se.EpisodeID != "" && !seen[se.EpisodeID] {
			seen[se.EpisodeID] = true
			res = append(res, se)
		}
	}
	if len(items) > 0 && len(res) == 0 {
		return nil, fmt.Errorf("tmr returned %d items but none carried a memo_id/episode_id (checked memo_id, top-level and metadata); raw: %s",
			len(items), firstN(out.String(), 300))
	}
	return res, nil
}

// ---------- Provenance probe (LLM-free retrieval ceiling) ----------

// RetrievalReport measures whether retrieval can even supply the parts a
// query needs: for each query with known provenance, retrieve top-k by the
// query text and check coverage of the provenance episode set. Full-coverage
// rate upper-bounds any RAG condition built on this retriever — an LLM
// cannot combine episodes it never sees.
type RetrievalReport struct {
	Retriever string                     `json:"retriever"`
	K         int                        `json:"k"`
	PerSlice  map[string]*ProvSliceScore `json:"per_slice"`
}

type ProvSliceScore struct {
	Queries       int `json:"queries"`
	FullCoverage  int `json:"full_coverage"`  // all provenance episodes retrieved
	EpisodesHit   int `json:"episodes_hit"`   // micro
	EpisodesTotal int `json:"episodes_total"` // micro
}

func ProbeRetrieval(r Retriever, episodes []gen.Episode, queries []gen.Query, k int) (*RetrievalReport, error) {
	if err := r.Index(episodes); err != nil {
		return nil, err
	}
	rep := &RetrievalReport{Retriever: r.Name(), K: k, PerSlice: map[string]*ProvSliceScore{}}
	for _, q := range queries {
		if len(q.ProvenanceEpisodes) == 0 {
			continue // negatives have no provenance; nothing to cover
		}
		res, err := r.Retrieve(q.Text, k)
		if err != nil {
			return nil, err
		}
		got := map[string]bool{}
		for _, se := range res {
			got[se.EpisodeID] = true
		}
		s, ok := rep.PerSlice[q.Slice]
		if !ok {
			s = &ProvSliceScore{}
			rep.PerSlice[q.Slice] = s
		}
		s.Queries++
		full := true
		for _, ep := range q.ProvenanceEpisodes {
			s.EpisodesTotal++
			if got[ep] {
				s.EpisodesHit++
			} else {
				full = false
			}
		}
		if full {
			s.FullCoverage++
		}
	}
	return rep, nil
}

func RetrievalTable(reports []*RetrievalReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-14s %3s  %-12s %14s %16s\n", "retriever", "k", "slice", "full-coverage", "episode-recall")
	for _, r := range reports {
		var slices []string
		for s := range r.PerSlice {
			slices = append(slices, s)
		}
		sort.Strings(slices)
		for _, sl := range slices {
			s := r.PerSlice[sl]
			fmt.Fprintf(&b, "%-14s %3d  %-12s %14s %16s\n",
				r.Retriever, r.K, sl,
				fmt.Sprintf("%d/%d", s.FullCoverage, s.Queries),
				fmt.Sprintf("%d/%d", s.EpisodesHit, s.EpisodesTotal))
		}
	}
	return b.String()
}
