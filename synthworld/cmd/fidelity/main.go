// fidelity scores COMPILATION FIDELITY (spec §8.1, MASTERPLAN E3): it runs
// the S2 pipeline over a dataset's episode text and compares the compiled
// store against world.json ground truth.
//
// world.json ACCESS NOTICE: this command is a SCORER. It is the only
// consumer of world.json on the C2b path and its output never feeds back
// into any condition. Reading world.json here is the reason the synthetic
// domain exists (the instrument knows the true world); a condition doing
// the same would invalidate the campaign.
//
// Output: per item type (facts, rules, supersessions) precision/recall on
// CONTENT-level identity, plus the confusion decomposition that makes C2b
// failures diagnosable (MASTERPLAN §8-E4):
//
//	exact        committed and content-identical to the world item
//	mangled      extracted + committed under a world item's ID, wrong content
//	dropped      extracted but killed by consistency/hygiene (incl. quarantine)
//	missed       never extracted at all
//	hallucinated committed but matching no world item (by ID or content)
//
// Usage:
//
//	fidelity -dir dataset [-extractor det|llm] [-json out.json] [-trace out-trace.json]
//
// llm extractor env: HARNESS_LLM_BASE_URL/_MODEL/_KEY/_CACHE/_CACHE_MODE,
// HARNESS_LLM_TEMPERATURE, HARNESS_LLM_EXTRA_PARAMS, HARNESS_CONCURRENCY —
// identical semantics to cmd/harness.
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

type typeScore struct {
	World        int      `json:"world"`
	Committed    int      `json:"committed"`
	Exact        int      `json:"exact"`
	Mangled      int      `json:"mangled"`
	Dropped      int      `json:"dropped"`
	Missed       int      `json:"missed"`
	Hallucinated int      `json:"hallucinated"`
	Precision    float64  `json:"precision"`
	Recall       float64  `json:"recall"`
	MangledIDs   []string `json:"mangled_ids,omitempty"`
	DroppedIDs   []string `json:"dropped_ids,omitempty"`
	MissedIDs    []string `json:"missed_ids,omitempty"`
	HallucIDs    []string `json:"hallucinated_ids,omitempty"`
}

type fidelityReport struct {
	Dataset       string              `json:"dataset"`
	Extractor     string              `json:"extractor"`
	Facts         typeScore           `json:"facts"`
	Rules         typeScore           `json:"rules"`
	Supersessions typeScore           `json:"supersessions"`
	Frames        *framesFidelity     `json:"frames,omitempty"` // F-E3; nil on v0 datasets
	Compile       *loom.CompileReport `json:"-"`
}

func main() {
	dir := flag.String("dir", "dataset", "dataset directory")
	extractor := flag.String("extractor", "det", "det | llm | det-frames | llm-frames")
	jsonOut := flag.String("json", "", "optional JSON report path")
	traceOut := flag.String("trace", "", "optional compilation trace JSON path")
	episodesFile := flag.String("episodes", "episodes.jsonl",
		"episodes file relative to -dir (e.g. episodes_paraphrased.jsonl)")
	handlesPath := flag.String("handles", "",
		"naturalize-report.json with the frame handle table (\"auto\" = resolve next to -dir when -episodes contains \"natural\")")
	flag.Parse()

	var w world.World
	mustReadJSON(filepath.Join(*dir, "world.json"), &w)
	var episodes []gen.Episode
	readJSONL(filepath.Join(*dir, *episodesFile), func(raw []byte) {
		var ep gen.Episode
		must(json.Unmarshal(raw, &ep))
		episodes = append(episodes, ep)
	})

	vocab := loom.Vocabulary{}
	for _, r := range w.Relations {
		rv := loom.RelationVocab{ID: r.ID, Name: r.Name}
		for _, s := range r.Slots {
			rv.Slots = append(rv.Slots, s.Name)
		}
		vocab.Relations = append(vocab.Relations, rv)
	}
	// Symbol catalog (entity grounding) — mirror cmd/harness so fidelity
	// reflects the same normalization the measured conditions use.
	for _, e := range w.Entities {
		if e.Name != "" && e.Name != e.ID {
			vocab.Entities = append(vocab.Entities, loom.EntityVocab{
				ID: e.ID, Surface: e.Name, Type: string(e.Type)})
		}
	}

	var ex loom.Extractor
	switch *extractor {
	case "det":
		ex = loom.DeterministicExtractor{}
	case "det-frames":
		ex = loom.FramesDeterministicExtractor{}
	case "llm":
		llm := llmFromEnv()
		if llm == nil {
			must(fmt.Errorf("-extractor llm requires HARNESS_LLM_BASE_URL (or a replay cache); see cmd/harness env docs"))
		}
		ex = loom.NewLLMExtractor(llm, vocab)
	case "llm-frames":
		llm := llmFromEnv()
		if llm == nil {
			must(fmt.Errorf("-extractor llm-frames requires HARNESS_LLM_BASE_URL (or a replay cache); see cmd/harness env docs"))
		}
		ex = loom.NewFramesLLMExtractor(llm, vocab)
	default:
		must(fmt.Errorf("unknown extractor %q (det|llm|det-frames|llm-frames)", *extractor))
	}

	p := loom.NewPipeline(vocab, ex)
	p.FrameNames = loadFrameHandles(*handlesPath, *dir, *episodesFile, &w)
	if v := os.Getenv("HARNESS_C2B_QUARANTINE_CONF"); v != "" {
		fmt.Sscanf(v, "%g", &p.QuarantineActualBelowConfidence)
	}
	if v := os.Getenv("HARNESS_CONCURRENCY"); v != "" {
		fmt.Sscanf(v, "%d", &p.Workers)
	}
	rep, err := p.Compile(episodes)
	must(err)

	out := score(&w, p, rep)
	out.Dataset = *dir
	if len(w.Frames) > 0 {
		out.Frames = scoreFrames(&w, episodes, p)
	}

	fmt.Printf("compilation fidelity (%s extractor) — dataset %s\n\n", rep.Extractor, *dir)
	fmt.Printf("%-14s %6s %9s %6s %8s %8s %7s %13s %10s %8s\n",
		"type", "world", "committed", "exact", "mangled", "dropped", "missed", "hallucinated", "precision", "recall")
	for _, row := range []struct {
		name string
		s    typeScore
	}{{"facts", out.Facts}, {"rules", out.Rules}, {"supersessions", out.Supersessions}} {
		fmt.Printf("%-14s %6d %9d %6d %8d %8d %7d %13d %10.3f %8.3f\n",
			row.name, row.s.World, row.s.Committed, row.s.Exact, row.s.Mangled,
			row.s.Dropped, row.s.Missed, row.s.Hallucinated, row.s.Precision, row.s.Recall)
	}
	if out.Frames != nil {
		printFramesFidelity(out.Frames)
	}
	if len(rep.Hygiene) > 0 {
		fmt.Println("\nhygiene gate:")
		for _, h := range rep.Hygiene {
			fmt.Println("  " + h)
		}
	}
	nonExact := out.Facts.Mangled + out.Facts.Missed + out.Facts.Dropped +
		out.Rules.Mangled + out.Rules.Missed + out.Rules.Dropped
	if nonExact > 0 {
		fmt.Println("\nworst offenders (IDs, first 10 per class):")
		printIDs("facts mangled", out.Facts.MangledIDs)
		printIDs("facts missed", out.Facts.MissedIDs)
		printIDs("facts dropped", out.Facts.DroppedIDs)
		printIDs("rules mangled", out.Rules.MangledIDs)
		printIDs("rules missed", out.Rules.MissedIDs)
		printIDs("rules dropped", out.Rules.DroppedIDs)
	}

	if *jsonOut != "" {
		writeJSON(*jsonOut, out)
	}
	if *traceOut != "" {
		writeJSON(*traceOut, rep)
	}
}

// score computes the confusion decomposition. Content identity: facts by
// atom.Key()+interval+source; rules by semantic equality (loom's
// RulesEquivalent, EpisodeID ignored); supersessions by old/new/from.
func score(w *world.World, p *loom.Pipeline, rep *loom.CompileReport) *fidelityReport {
	out := &fidelityReport{Extractor: rep.Extractor, Compile: rep}
	outcomes := rep.OutcomeByID()

	// ---- facts ----
	// Content identity includes the home frame and block polarity (an atom
	// filed in the wrong frame is different knowledge). Source counts only
	// for actual-frame facts: frame-homed lines deliberately do not render
	// their payload source (INSTRUMENT amendment 2026-07-12 — the names
	// were type-revealing), so no text extractor can recover it and the
	// evaluator never reads it.
	factContent := func(f *world.BaseFact) string {
		src := f.Source
		if world.NormFrame(f.FrameID) != world.ActualFrame {
			src = ""
		}
		return fmt.Sprintf("%s|%d|%d|%s|%s|%v", f.Atom.Key(), f.From, f.To, src, world.NormFrame(f.FrameID), f.Block)
	}
	worldFactByID := map[string]*world.BaseFact{}
	worldFactContent := map[string]string{} // content -> id
	for i := range w.Facts {
		f := &w.Facts[i]
		worldFactByID[f.ID] = f
		worldFactContent[factContent(f)] = f.ID
	}
	fs := &out.Facts
	fs.World = len(w.Facts)
	matchedWorld := map[string]bool{}
	for i := range p.Store.Facts {
		sf := &p.Store.Facts[i]
		fs.Committed++
		if wf, ok := worldFactByID[sf.Fact.ID]; ok {
			if factContent(wf) == factContent(&sf.Fact) {
				fs.Exact++
				matchedWorld[wf.ID] = true
			} else {
				fs.Mangled++
				fs.MangledIDs = addID(fs.MangledIDs, sf.Fact.ID)
				matchedWorld[wf.ID] = true // ID accounted for; not missed
			}
		} else if id, ok := worldFactContent[factContent(&sf.Fact)]; ok {
			// right content, invented/wrong ID: evaluation-equivalent.
			// Counted exact for content-level P/R; the ID slip is visible
			// in the trace, not a knowledge error.
			fs.Exact++
			matchedWorld[id] = true
		} else {
			fs.Hallucinated++
			fs.HallucIDs = addID(fs.HallucIDs, sf.Fact.ID)
		}
	}
	for _, f := range w.Facts {
		if matchedWorld[f.ID] {
			continue
		}
		if v, ok := outcomes[f.ID]; ok && v != loom.VCommitted && v != loom.VDuplicate && v != loom.VRefinement {
			fs.Dropped++
			fs.DroppedIDs = addID(fs.DroppedIDs, f.ID)
		} else {
			fs.Missed++
			fs.MissedIDs = addID(fs.MissedIDs, f.ID)
		}
	}
	finalize(fs)

	// ---- rules ----
	rs := &out.Rules
	rs.World = len(w.Rules)
	worldRuleByID := map[string]*world.Rule{}
	for i := range w.Rules {
		worldRuleByID[w.Rules[i].ID] = &w.Rules[i]
	}
	ruleMatched := map[string]bool{}
	for i := range p.Store.Rules {
		sr := &p.Store.Rules[i]
		if sr.Lifecycle == loom.Quarantined {
			// stored for audit but killed by hygiene: counted as dropped
			// against the world item below (or hallucinated if unknown).
			if _, ok := worldRuleByID[sr.Rule.ID]; !ok {
				rs.Hallucinated++
				rs.HallucIDs = addID(rs.HallucIDs, sr.Rule.ID)
			}
			continue
		}
		rs.Committed++
		if wr, ok := worldRuleByID[sr.Rule.ID]; ok {
			g, want := sr.Rule, *wr
			g.EpisodeID, want.EpisodeID = "", ""
			if loom.RulesEquivalent(&g, &want) {
				rs.Exact++
			} else {
				rs.Mangled++
				rs.MangledIDs = addID(rs.MangledIDs, sr.Rule.ID)
			}
			ruleMatched[wr.ID] = true
		} else {
			rs.Hallucinated++
			rs.HallucIDs = addID(rs.HallucIDs, sr.Rule.ID)
		}
	}
	for _, r := range w.Rules {
		if ruleMatched[r.ID] {
			continue
		}
		if v, ok := outcomes[r.ID]; ok && v != loom.VCommitted && v != loom.VDuplicate {
			rs.Dropped++
			rs.DroppedIDs = addID(rs.DroppedIDs, r.ID)
		} else {
			rs.Missed++
			rs.MissedIDs = addID(rs.MissedIDs, r.ID)
		}
	}
	finalize(rs)

	// ---- supersessions ----
	ss := &out.Supersessions
	ss.World = len(w.Supersessions)
	worldSupByID := map[string]*world.Supersession{}
	for i := range w.Supersessions {
		worldSupByID[w.Supersessions[i].ID] = &w.Supersessions[i]
	}
	supMatched := map[string]bool{}
	for i := range p.Store.Supersessions {
		sp := &p.Store.Supersessions[i]
		ss.Committed++
		if wsup, ok := worldSupByID[sp.Supersession.ID]; ok {
			if wsup.OldRule == sp.Supersession.OldRule && wsup.NewRule == sp.Supersession.NewRule && wsup.From == sp.Supersession.From &&
				world.NormFrame(wsup.FrameID) == world.NormFrame(sp.Supersession.FrameID) {
				ss.Exact++
			} else {
				ss.Mangled++
				ss.MangledIDs = addID(ss.MangledIDs, sp.Supersession.ID)
			}
			supMatched[wsup.ID] = true
		} else {
			ss.Hallucinated++
			ss.HallucIDs = addID(ss.HallucIDs, sp.Supersession.ID)
		}
	}
	for _, s := range w.Supersessions {
		if supMatched[s.ID] {
			continue
		}
		if v, ok := outcomes[s.ID]; ok && v != loom.VCommitted && v != loom.VDuplicate {
			ss.Dropped++
			ss.DroppedIDs = addID(ss.DroppedIDs, s.ID)
		} else {
			ss.Missed++
			ss.MissedIDs = addID(ss.MissedIDs, s.ID)
		}
	}
	finalize(ss)
	return out
}

func finalize(s *typeScore) {
	if s.Committed > 0 {
		s.Precision = float64(s.Exact) / float64(s.Committed)
	}
	if s.World > 0 {
		s.Recall = float64(s.Exact) / float64(s.World)
	}
	sort.Strings(s.MangledIDs)
	sort.Strings(s.DroppedIDs)
	sort.Strings(s.MissedIDs)
	sort.Strings(s.HallucIDs)
}

func addID(ids []string, id string) []string { return append(ids, id) }

func printIDs(label string, ids []string) {
	if len(ids) == 0 {
		return
	}
	if len(ids) > 10 {
		ids = ids[:10]
	}
	fmt.Printf("  %-14s %v\n", label, ids)
}

// loadFrameHandles mirrors cmd/harness: explicit path, "auto" next to the
// dataset for naturalized streams, or identity from the world's frame table
// (tier-E text carries raw IDs; the scorer may read world.json freely).
func loadFrameHandles(spec, dir, episodesFile string, w *world.World) map[string]string {
	path := spec
	if spec == "auto" {
		if !strings.Contains(episodesFile, "natural") {
			path = ""
		} else {
			path = filepath.Join(dir, "naturalize-report.json")
		}
	}
	if path == "" {
		out := map[string]string{}
		for _, f := range w.Frames {
			out[f.ID] = f.ID
		}
		return out
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

// llmFromEnv mirrors cmd/harness's client construction (same env vars).
func llmFromEnv() loom.Completer {
	base := os.Getenv("HARNESS_LLM_BASE_URL")
	cacheDir := os.Getenv("HARNESS_LLM_CACHE")
	cacheMode := harness.CacheMode(os.Getenv("HARNESS_LLM_CACHE_MODE"))
	if base == "" && !(cacheDir != "" && cacheMode == harness.CacheReplay) {
		return nil
	}
	var shape harness.RequestShape
	switch tv := os.Getenv("HARNESS_LLM_TEMPERATURE"); tv {
	case "":
	case "none":
		shape.OmitTemperature = true
	default:
		var f float64
		if _, err := fmt.Sscanf(tv, "%g", &f); err != nil {
			must(fmt.Errorf("HARNESS_LLM_TEMPERATURE=%q invalid", tv))
		}
		shape.Temperature = &f
	}
	if ep := os.Getenv("HARNESS_LLM_EXTRA_PARAMS"); ep != "" {
		if err := json.Unmarshal([]byte(ep), &shape.ExtraParams); err != nil {
			must(fmt.Errorf("HARNESS_LLM_EXTRA_PARAMS: %w", err))
		}
	}
	var llm harness.LLMClient
	if base != "" {
		llm = &harness.OpenAICompatClient{
			BaseURL: base, APIKey: os.Getenv("HARNESS_LLM_KEY"),
			ModelID: os.Getenv("HARNESS_LLM_MODEL"), Shape: shape,
			Timeout: 240 * time.Second,
		}
	}
	if cacheDir != "" {
		llm = &harness.CachingLLMClient{Inner: llm, Dir: cacheDir, Mode: cacheMode,
			ModelID: os.Getenv("HARNESS_LLM_MODEL"), Shape: shape}
	}
	return harness.NewMeteredLLM(llm)
}

func mustReadJSON(path string, v any) {
	raw, err := os.ReadFile(path)
	must(err)
	must(json.Unmarshal(raw, v))
}

func readJSONL(path string, fn func([]byte)) {
	f, err := os.Open(path)
	must(err)
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		if len(sc.Bytes()) > 0 {
			fn(sc.Bytes())
		}
	}
	must(sc.Err())
}

func writeJSON(path string, v any) {
	f, err := os.Create(path)
	must(err)
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	must(enc.Encode(v))
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
