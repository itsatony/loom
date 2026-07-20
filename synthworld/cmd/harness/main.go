// harness runs memory conditions against a synthworld dataset and prints a
// per-slice score table. v0 ships the diagnostic conditions (floors,
// oracle ceiling, stale oracle, grep baseline); real conditions (C1 RAG,
// C2 Loom, C3 LoRA) implement harness.Condition and register here.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/vaudience/synthworld/gen"
	"github.com/vaudience/synthworld/harness"
	"github.com/vaudience/synthworld/loom"
	"github.com/vaudience/synthworld/world"
)

func main() {
	dir := flag.String("dir", "dataset", "dataset directory")
	jsonOut := flag.String("json", "", "optional path for JSON report")
	episodesFile := flag.String("episodes", "episodes.jsonl",
		"episodes file relative to -dir (e.g. episodes_paraphrased.jsonl for the hard-mode paraphrase tier)")
	condFilter := flag.String("conditions", "",
		"comma-separated condition names to run (default: all registered); lets one driver run C1 and C2 passes under different LLM request shapes")
	handlesPath := flag.String("handles", "",
		"naturalize-report.json carrying the frame ID→handle table for a naturalized episode stream; \"auto\" resolves it next to -dir when -episodes contains \"natural\". Query text is rewritten to handle vocabulary (uniform for every condition); structured Frame fields keep canonical IDs")
	flag.Parse()

	var w world.World
	mustReadJSON(filepath.Join(*dir, "world.json"), &w)

	var episodes []gen.Episode
	readJSONL(filepath.Join(*dir, *episodesFile), func(raw []byte) {
		var ep gen.Episode
		must(json.Unmarshal(raw, &ep))
		episodes = append(episodes, ep)
	})
	var queries []gen.Query
	readJSONL(filepath.Join(*dir, "queries.jsonl"), func(raw []byte) {
		var q gen.Query
		must(json.Unmarshal(raw, &q))
		queries = append(queries, q)
	})

	// Frame handles (tier-M naturalized streams): the naturalize report maps
	// canonical frame IDs to the surface names the TEXT uses (raw frame IDs
	// are banned from tier-M text, §9.6.6). Presenting queries in the text's
	// own vocabulary is a naming affordance applied uniformly to every
	// condition — which LINE belongs to which frame remains entirely the
	// system-under-test's problem. Structured Frame fields keep canonical
	// IDs; scoring is unchanged.
	frameNames := loadFrameHandles(*handlesPath, *dir, *episodesFile)
	if len(frameNames) > 0 {
		rewriteQueryText(queries, frameNames)
	}
	// Conditions always receive a frame-name table on frames datasets: the
	// tier-M handle map when naturalized, otherwise identity (tier-E text
	// carries raw frame IDs). The frame ID list is already public to every
	// condition via query text (misattribution queries enumerate it).
	condFrameNames := frameNames
	if len(condFrameNames) == 0 && len(w.Frames) > 0 {
		condFrameNames = map[string]string{}
		for _, f := range w.Frames {
			condFrameNames[f.ID] = f.ID
		}
	}
	if len(w.Frames) > 0 {
		classifyCues(queries, episodes, condFrameNames)
		classifyFilterability(queries)
	}

	// Seeded relation vocabulary for S2 compilation (spec §4): relation
	// IDs, names, and slot NAMES from the dataset's relation table — the
	// domain schema a production deployment would be given. Never facts,
	// rules, supersessions, or entities; this is the only world.json
	// content any C2 condition receives.
	vocab := loom.Vocabulary{}
	for _, r := range w.Relations {
		rv := loom.RelationVocab{ID: r.ID, Name: r.Name}
		for _, s := range r.Slots {
			rv.Slots = append(rv.Slots, s.Name)
		}
		vocab.Relations = append(vocab.Relations, rv)
	}
	pipelineWorkers := 1
	if v := os.Getenv("HARNESS_CONCURRENCY"); v != "" {
		fmt.Sscanf(v, "%d", &pipelineWorkers)
	}

	conditions := []harness.Condition{
		&harness.ConstCondition{Value: true},
		&harness.ConstCondition{Value: false},
		&harness.GrepCondition{},
		&harness.OracleCondition{W: &w, Stale: true},
		&harness.OracleCondition{W: &w},
		harness.NewLoomCondition(),
		// c2b-det: pipeline CONTROL (template-inverse extractor). Oracle-
		// equal scores validate the compile path, never the thesis.
		// FrameBlind: a v0 store answers every query from its one world
		// (frozen §9.6.5 row: loom-c2b-det == v0 oracle in every cell).
		&harness.LoomC2bCondition{Label: "loom-c2b-det", Vocab: vocab,
			Extractor: loom.DeterministicExtractor{}, Workers: pipelineWorkers,
			FrameBlind: true},
	}
	// Frame diagnostics (MASTERPLAN §9.6.5): registered only on frames
	// datasets; each breaks frame semantics in exactly one way.
	if len(w.Frames) > 0 {
		conditions = append(conditions,
			&harness.FrameOracleCondition{W: &w},
			&harness.MonoWorldCondition{W: &w},
			&harness.IsolationistCondition{W: &w},
			&harness.LiteralistCondition{W: &w},
			// c2b-frames-det: frame-aware pipeline CONTROL (tier-E template
			// inverse). frame-oracle-equal scores prove the frame-aware
			// compile path is lossless; expected (and pre-registered) to
			// collapse on naturalized text.
			&harness.LoomC2bCondition{Label: "loom-c2b-frames-det", Vocab: vocab,
				Extractor: loom.FramesDeterministicExtractor{}, Workers: pipelineWorkers,
				FrameNames: condFrameNames},
		)
	}

	// Embedding client: configured by environment; nil when unset.
	//   HARNESS_EMBED_BASE_URL    e.g. https://api.openai.com/v1
	//   HARNESS_EMBED_MODEL       e.g. text-embedding-3-small
	//   HARNESS_EMBED_KEY         bearer token
	//   HARNESS_EMBED_CACHE       embedding cassette dir (mandatory for use)
	//   HARNESS_EMBED_CACHE_MODE  auto (default) | replay (miss = loud error)
	var embedClient *harness.EmbeddingClient
	if model := os.Getenv("HARNESS_EMBED_MODEL"); model != "" {
		embedClient = &harness.EmbeddingClient{
			BaseURL:    os.Getenv("HARNESS_EMBED_BASE_URL"),
			APIKey:     os.Getenv("HARNESS_EMBED_KEY"),
			ModelID:    model,
			CacheDir:   os.Getenv("HARNESS_EMBED_CACHE"),
			ReplayOnly: os.Getenv("HARNESS_EMBED_CACHE_MODE") == "replay",
		}
		if embedClient.CacheDir == "" {
			must(fmt.Errorf("HARNESS_EMBED_MODEL set but HARNESS_EMBED_CACHE empty — the embedding cache is mandatory (replayable runs)"))
		}
	}
	newEmbedRetriever := func() *harness.EmbeddingRetriever {
		return &harness.EmbeddingRetriever{Client: embedClient}
	}
	newHybridRetriever := func() *harness.HybridRetriever {
		return &harness.HybridRetriever{
			Components: []harness.Retriever{&harness.BM25Retriever{}, newEmbedRetriever()},
			Label:      "hybrid-bm25-embed",
		}
	}

	// LLM-backed conditions: configured entirely by environment so runs on
	// real infra need no code change. Self-skip when unset (tmr e2e pattern).
	//   HARNESS_LLM_BASE_URL   e.g. http://babylon:8000/v1
	//   HARNESS_LLM_MODEL      e.g. qwen3.5-35b
	//   HARNESS_LLM_KEY        optional bearer token
	//   HARNESS_RAG_K          top-k episodes (default 8)
	//   HARNESS_RAG_RETRIEVER  bm25 | embed | hybrid | tmr (default: tmr if
	//                          HARNESS_TMR_BIN set, else bm25)
	//   HARNESS_TMR_BIN/_DIR   tmr binary + ingested memo folder
	//   HARNESS_LLM_CACHE      cassette dir; enables record/replay
	//   HARNESS_LLM_CACHE_MODE auto (default) | record | replay
	//   HARNESS_CONCURRENCY    worker pool size for queries (default 1)
	//   HARNESS_LLM_TEMPERATURE  float, or "none" to omit the field entirely
	//                            (gpt-5 family rejects it); unset =
	//                            temperature:0 (legacy default)
	//   HARNESS_LLM_EXTRA_PARAMS raw JSON object merged top-level into every
	//                            chat request, e.g. '{"reasoning_effort":"minimal"}'
	//                            or '{"chat_template_kwargs":{"enable_thinking":false}}';
	//                            invalid JSON fails fast; part of the cache key
	// With cache mode=replay a base URL is unnecessary: LLM conditions run
	// fully offline from cassettes (HARNESS_LLM_MODEL still required — it is
	// part of the cache key).
	meters := map[string]*harness.MeteredLLM{}
	base := os.Getenv("HARNESS_LLM_BASE_URL")
	cacheDir := os.Getenv("HARNESS_LLM_CACHE")
	cacheMode := harness.CacheMode(os.Getenv("HARNESS_LLM_CACHE_MODE"))
	if base != "" || (cacheDir != "" && cacheMode == harness.CacheReplay) {
		var shape harness.RequestShape
		switch tv := os.Getenv("HARNESS_LLM_TEMPERATURE"); tv {
		case "":
			// legacy default: temperature:0
		case "none":
			shape.OmitTemperature = true
		default:
			var f float64
			if _, err := fmt.Sscanf(tv, "%g", &f); err != nil {
				must(fmt.Errorf("HARNESS_LLM_TEMPERATURE=%q: not a float and not \"none\"", tv))
			}
			shape.Temperature = &f
		}
		if ep := os.Getenv("HARNESS_LLM_EXTRA_PARAMS"); ep != "" {
			if err := json.Unmarshal([]byte(ep), &shape.ExtraParams); err != nil {
				must(fmt.Errorf("HARNESS_LLM_EXTRA_PARAMS is not a JSON object: %w", err))
			}
			for _, forbidden := range []string{"model", "messages"} {
				if _, ok := shape.ExtraParams[forbidden]; ok {
					must(fmt.Errorf("HARNESS_LLM_EXTRA_PARAMS must not override %q", forbidden))
				}
			}
		}

		var llm harness.LLMClient
		if base != "" {
			llm = &harness.OpenAICompatClient{
				BaseURL: base,
				APIKey:  os.Getenv("HARNESS_LLM_KEY"),
				ModelID: os.Getenv("HARNESS_LLM_MODEL"),
				Shape:   shape,
				Timeout: 240 * time.Second, // long-context prompts are slow
			}
		}
		if cacheDir != "" {
			llm = &harness.CachingLLMClient{
				Inner: llm, Dir: cacheDir, Mode: cacheMode,
				ModelID: os.Getenv("HARNESS_LLM_MODEL"),
				Shape:   shape, // key hashes exactly what the inner client sends
			}
		}
		k := 8
		if v := os.Getenv("HARNESS_RAG_K"); v != "" {
			fmt.Sscanf(v, "%d", &k)
		}
		choice := os.Getenv("HARNESS_RAG_RETRIEVER")
		if choice == "" {
			if os.Getenv("HARNESS_TMR_BIN") != "" {
				choice = "tmr"
			} else {
				choice = "bm25"
			}
		}
		var ret harness.Retriever
		switch choice {
		case "bm25":
			ret = &harness.BM25Retriever{}
		case "embed":
			if embedClient == nil {
				must(fmt.Errorf("HARNESS_RAG_RETRIEVER=embed requires HARNESS_EMBED_MODEL (+cache)"))
			}
			ret = newEmbedRetriever()
		case "hybrid":
			if embedClient == nil {
				must(fmt.Errorf("HARNESS_RAG_RETRIEVER=hybrid requires HARNESS_EMBED_MODEL (+cache)"))
			}
			ret = newHybridRetriever()
		case "tmr":
			ret = &harness.TmrRetriever{Binary: os.Getenv("HARNESS_TMR_BIN"), Folder: os.Getenv("HARNESS_TMR_DIR"), Mode: "hybrid"}
		default:
			must(fmt.Errorf("unknown HARNESS_RAG_RETRIEVER %q (bm25|embed|hybrid|tmr)", choice))
		}
		// Each LLM condition gets its own meter around the shared (cached)
		// client, so token spend is attributable per condition. C0 floor,
		// C1 RAG, C1c long-context, and the D6 perfect-retrieval DIAGNOSTIC
		// (ceiling, never a competitor). D6's provenance map is built here —
		// the one place with the unsanitized QuerySet — and injected at
		// construction; conditions themselves still only see SanitizedQuery.
		metered := func(name string) *harness.MeteredLLM {
			m := harness.NewMeteredLLM(llm)
			meters[name] = m
			return m
		}
		prov := make(map[string][]string)
		for _, q := range queries {
			if len(q.ProvenanceEpisodes) > 0 {
				prov[q.ID] = q.ProvenanceEpisodes
			}
		}
		c0 := &harness.C0Condition{LLM: metered("c0-no-memory")}
		rag := &harness.RAGCondition{Retriever: ret, LLM: metered("rag-" + ret.Name()), K: k}
		c1c := &harness.C1cLongContext{LLM: metered("c1c-longcontext")}
		d6 := harness.NewPerfectRetrievalCondition(metered("d6-perfect-retrieval"), prov)
		// loom-c2b: THE condition under test — LLM extraction compiles the
		// episode text into the store; extraction spend is metered like any
		// query-time condition (it is the compile-once cost, H7).
		c2b := &harness.LoomC2bCondition{Label: "loom-c2b", Vocab: vocab,
			Extractor: loom.NewLLMExtractor(metered("loom-c2b"), vocab), Workers: pipelineWorkers,
			FrameBlind: true}
		conditions = append(conditions, c0, rag, c1c, d6, c2b)
		if len(w.Frames) > 0 {
			// loom-c2b-frames: THE frames condition under test (F-E1..F-E4).
			// c2b-prov: the registered honest null — same frame-blind
			// extraction as loom-c2b (cassettes shared), frameless store,
			// query-time metadata filtering. loom-c2b above doubles as the
			// frame-blind contamination confirmation (§9.6.3).
			// Quarantine threshold (§9.6.1 safety gate) is off by default;
			// HARNESS_C2B_QUARANTINE_CONF=<0..1> enables it for the
			// leakage-fix leg (low-confidence actual-homing → quarantine).
			quarConf := 0.0
			if v := os.Getenv("HARNESS_C2B_QUARANTINE_CONF"); v != "" {
				fmt.Sscanf(v, "%g", &quarConf)
			}
			c2bf := &harness.LoomC2bCondition{Label: "loom-c2b-frames", Vocab: vocab,
				Extractor: loom.NewFramesLLMExtractor(metered("loom-c2b-frames"), vocab),
				Workers:   pipelineWorkers, FrameNames: condFrameNames,
				QuarantineActualBelowConfidence: quarConf}
			prov := &harness.C2bProvCondition{Label: "c2b-prov", Vocab: vocab,
				Extractor: loom.NewLLMExtractor(metered("c2b-prov"), vocab),
				Workers:   pipelineWorkers, FrameNames: condFrameNames}
			// Self-consistency extraction (§10 2026-07-20), opt-in via
			// HARNESS_C2B_SC_K=<odd K>. K sampled extractions, majority-voted
			// frame homing. Run with HARNESS_LLM_TEMPERATURE>0 so samples
			// diverge. DEV-ONLY per pre-registration; never a locked-set rerun.
			if v := os.Getenv("HARNESS_C2B_SC_K"); v != "" {
				scK := 5
				fmt.Sscanf(v, "%d", &scK)
				scCond := &harness.LoomC2bCondition{Label: "loom-c2b-frames-sc", Vocab: vocab,
					Extractor: loom.NewFramesSelfConsistExtractor(
						loom.NewFramesLLMExtractor(metered("loom-c2b-frames-sc"), vocab), scK),
					Workers: pipelineWorkers, FrameNames: condFrameNames,
					QuarantineActualBelowConfidence: quarConf}
				conditions = append(conditions, scCond)
			}
			// frame-rag: the CEILING null (§10 2026-07-18) — strongest
			// query-time frame reasoning: frameless RAG over full episode
			// text + frame semantics, deciding frames per query.
			framerag := &harness.FrameRAGCondition{Label: "frame-rag", Retriever: ret,
				LLM: metered("frame-rag"), K: k, FrameNames: condFrameNames}
			conditions = append(conditions, c2bf, prov, framerag)
		}
	}

	if *condFilter != "" {
		want := map[string]bool{}
		for _, n := range strings.Split(*condFilter, ",") {
			want[strings.TrimSpace(n)] = true
		}
		var kept []harness.Condition
		for _, c := range conditions {
			if want[c.Name()] {
				kept = append(kept, c)
				delete(want, c.Name())
			}
		}
		if len(want) > 0 {
			var missing []string
			for n := range want {
				missing = append(missing, n)
			}
			sort.Strings(missing)
			fmt.Fprintf(os.Stderr, "error: -conditions names not registered (check env gating): %s\n", strings.Join(missing, ", "))
			os.Exit(1)
		}
		conditions = kept
	}

	var reports []*harness.Report
	for _, c := range conditions {
		r, err := harness.Run(c, episodes, queries)
		must(err)
		if m, ok := meters[r.Condition]; ok {
			r.Usage = m.Stats()
		}
		if r.UnknownSlice > 0 {
			fmt.Fprintf(os.Stderr, "WARNING: %s: %d queries had an unknown slice/type and were scored NOWHERE — dataset or harness bug\n",
				r.Condition, r.UnknownSlice)
		}
		reports = append(reports, r)
	}

	fmt.Printf("dataset: %s (%d episodes, %d queries)\n\n", *dir, len(episodes), len(queries))
	fmt.Print(harness.Table(reports))

	if ut := harness.UsageTable(reports); ut != "" {
		fmt.Println("\nllm token usage (spent = live network; replayed = cassettes):")
		fmt.Print(ut)
	}

	// Retrieval provenance probe: LLM-free ceiling for any RAG condition —
	// an LLM cannot combine episodes retrieval never gave it. Always probes
	// BM25; probes embedding + hybrid retrieval when HARNESS_EMBED_* is
	// configured; when HARNESS_TMR_BIN/_DIR are set, also probes tmr in the
	// modes from HARNESS_TMR_MODES (default "semantic,hybrid").
	probeRetrievers := []harness.Retriever{&harness.BM25Retriever{}}
	if embedClient != nil {
		probeRetrievers = append(probeRetrievers, newEmbedRetriever(), newHybridRetriever())
	}
	if bin := os.Getenv("HARNESS_TMR_BIN"); bin != "" && os.Getenv("HARNESS_TMR_DIR") != "" {
		modes := os.Getenv("HARNESS_TMR_MODES")
		if modes == "" {
			modes = "semantic,hybrid"
		}
		for _, m := range strings.Split(modes, ",") {
			if m = strings.TrimSpace(m); m != "" {
				probeRetrievers = append(probeRetrievers, &harness.TmrRetriever{
					Binary: bin, Folder: os.Getenv("HARNESS_TMR_DIR"), Mode: m,
				})
			}
		}
	}
	fmt.Println("\nretrieval provenance probe (upper bound for RAG conditions):")
	for _, ret := range probeRetrievers {
		var probes []*harness.RetrievalReport
		for _, k := range []int{4, 8, 16} {
			p, err := harness.ProbeRetrieval(ret, episodes, queries, k)
			must(err)
			probes = append(probes, p)
		}
		fmt.Printf("\n[%s]\n", ret.Name())
		fmt.Print(harness.RetrievalTable(probes))
	}

	if *jsonOut != "" {
		f, err := os.Create(*jsonOut)
		must(err)
		defer f.Close()
		enc := json.NewEncoder(f)
		enc.SetIndent("", "  ")
		must(enc.Encode(reports))
		fmt.Printf("\nJSON report: %s\n", *jsonOut)
	}
}

// classifyCues assigns the F-E2 cued partition (SCORING-ONLY, in-memory;
// conditions never see it): a contamination trap is METADATA-CUED iff every
// episode line asserting its atom outside actual (or non-assertively) is
// filed out of actual by the registered lexical filter (harness.SpanFrameTag
// — the exact rule set the C2b-prov null uses at query time); CONTENT-CUED
// iff at least one such line would leak into a lexical filter's actual.
// Paired controls inherit their trap's class (emission adjacency).
func classifyCues(queries []gen.Query, episodes []gen.Episode, frameNames map[string]string) {
	lastClass := ""
	for i := range queries {
		q := &queries[i]
		if q.Slice != "contamination" {
			continue
		}
		if strings.HasSuffix(q.Subpop, "-control") {
			q.CueClass = lastClass
			continue
		}
		lines := 0
		metadata := true
		for _, ep := range episodes {
			for _, ev := range ep.Events {
				if ev.Kind != gen.EvFact || ev.Fact == nil {
					continue
				}
				if ev.Fact.Atom.Key() != q.Atom.Key() {
					continue
				}
				if world.NormFrame(ev.Fact.FrameID) == world.ActualFrame && ev.AssertionType == "" {
					continue
				}
				lines++
				if harness.SpanFrameTag(ev.Text, frameNames) == "" {
					metadata = false
				}
			}
		}
		if lines == 0 {
			// no locatable trap line (should not happen): leave unclassified
			q.CueClass = ""
			lastClass = ""
			continue
		}
		if metadata {
			q.CueClass = "metadata"
		} else {
			q.CueClass = "content"
		}
		lastClass = q.CueClass
	}
}

// classifyFilterability assigns the re-specified F-E2 filterability class
// (SCORING-ONLY, in-memory) to every holds-type frame-slice query, purely
// from ground-truth slice+subpop — never from any condition's output, so
// the partition cannot be gamed by the system under test (MASTERPLAN §10
// 2026-07-18). "resistant": the correct answer needs closure computation no
// per-item metadata carries. "decidable": frame-membership + cone lookup
// suffices. Query types other than holds (misattribution/ideation finds)
// are reported on their own metrics, not pooled here.
func classifyFilterability(queries []gen.Query) {
	for i := range queries {
		q := &queries[i]
		if q.Type != "holds" {
			continue
		}
		switch q.Slice {
		case "contamination":
			if q.Subpop == "sarcasm" {
				q.FilterClass = "resistant" // non-assertive: no item to filter on
			} else {
				q.FilterClass = "decidable" // fiction/quote membership + controls
			}
		case "isolation":
			switch q.Subpop {
			case "override-blocked", "override-active", "chain", "chain-control":
				q.FilterClass = "resistant" // delta overlay / derivation
			case "inherited", "override-actual":
				q.FilterClass = "decidable" // actual-fact membership via the cone
			}
		case "pinning":
			q.FilterClass = "resistant" // pin-day inheritance arithmetic, both polarities
		case "promotion":
			if q.Subpop == "source-frame" {
				q.FilterClass = "decidable" // the claim IS asserted in its source frame
			} else {
				q.FilterClass = "resistant" // pre/post-resolution + unresolved: time-gated belief
			}
		}
	}
}

// loadFrameHandles resolves the frame ID→handle table for a naturalized
// episode stream. spec "" = none (tier E: text carries raw IDs); "auto" =
// look for naturalize-report.json in the dataset dir when the episodes file
// looks naturalized; any other value = explicit report path.
func loadFrameHandles(spec, dir, episodesFile string) map[string]string {
	path := spec
	if spec == "auto" {
		if !strings.Contains(episodesFile, "natural") {
			return nil
		}
		path = filepath.Join(dir, "naturalize-report.json")
	}
	if path == "" {
		return nil
	}
	var rep struct {
		Handles []struct {
			FrameID string `json:"frame_id"`
			Handle  string `json:"handle"`
		} `json:"handles"`
	}
	mustReadJSON(path, &rep)
	if len(rep.Handles) == 0 {
		must(fmt.Errorf("%s: no frame handles found — wrong report file?", path))
	}
	out := map[string]string{}
	for _, h := range rep.Handles {
		out[h.FrameID] = h.Handle
	}
	return out
}

// rewriteQueryText replaces canonical frame IDs in query text with the
// handles the episode text uses. Longest-first so psp_<entity> rewrites
// before any embedded token could match.
func rewriteQueryText(queries []gen.Query, frameNames map[string]string) {
	ids := make([]string, 0, len(frameNames))
	for id := range frameNames {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if len(ids[i]) != len(ids[j]) {
			return len(ids[i]) > len(ids[j])
		}
		return ids[i] < ids[j]
	})
	for i := range queries {
		for _, id := range ids {
			queries[i].Text = strings.ReplaceAll(queries[i].Text, id, frameNames[id])
		}
	}
}

func mustReadJSON(path string, v any) {
	raw, err := os.ReadFile(path)
	must(err)
	must(json.Unmarshal(raw, v))
}

func readJSONL(path string, handle func([]byte)) {
	f, err := os.Open(path)
	must(err)
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		cp := make([]byte, len(line))
		copy(cp, line)
		handle(cp)
	}
	must(sc.Err())
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
