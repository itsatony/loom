// paraphrase produces the HARD-MODE PARAPHRASE TIER (MASTERPLAN §4.3):
// episodes_paraphrased.jsonl next to a dataset's episodes.jsonl, with every
// rendered text line rewritten by an LLM into varied natural business prose
// while a mechanical validator guarantees ground truth survives verbatim
// (identifiers, numbers, formal atom expressions, conditional structure —
// see validate.go). Episodes that fail validation after retries keep their
// ORIGINAL text and are counted; >10% fallbacks invalidates the tier and
// the command exits nonzero.
//
// The paraphrasing model MUST be outside the evaluated model matrix
// (currently qwen36-nvfp4 / gpt-5-mini / claude-haiku-4-5) — a model must
// never grade text it wrote. The caller enforces this via env:
//
//	PARAPHRASE_LLM_BASE_URL   e.g. https://api.anthropic.com/v1
//	PARAPHRASE_LLM_MODEL      e.g. claude-sonnet-5
//	PARAPHRASE_LLM_KEY        bearer token
//	PARAPHRASE_LLM_CACHE      cassette dir (mandatory — replayable tiers)
//	PARAPHRASE_LLM_CACHE_MODE auto (default) | record | replay
//	PARAPHRASE_LLM_TEMPERATURE  float, or "none" to omit the field (models
//	                            that deprecate temperature, e.g. claude-sonnet-5)
//	PARAPHRASE_LLM_EXTRA_PARAMS optional JSON merged into requests
//
// Structured payloads, episode IDs, and days are copied through untouched:
// easy mode (C2a) is identical on both streams by construction; only the
// text that hard-mode conditions read changes. Event texts are rewritten
// in lockstep with the episode text (line i ↔ event i).
//
// Usage:
//
//	go run ./cmd/paraphrase -dir <dataset>
//	go run ./cmd/harness  -dir <dataset> -episodes episodes_paraphrased.jsonl
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/vaudience/synthworld/gen"
	"github.com/vaudience/synthworld/harness"
)

const systemPrompt = `You rewrite lines from a synthetic business operations log into varied, natural business English. You are a careful technical editor: fluent prose, zero information drift.

Hard rules — violating any of them makes the output unusable:
1. Copy VERBATIM every identifier that contains an underscore (e.g. fct_0042, rul_007, sup_003, customer_03, registry_A, partner_feed). CASE-SENSITIVE: never capitalize one, even at the start of a sentence (write "registry_A reported...", never "Registry_A reported..."). Never reformat, translate, or drop one — the source name (e.g. audit_note, customer_disclosure) must always be mentioned; never invent new identifiers.
2. Copy VERBATIM every formal expression of the form name(slot=value, ...) — including ?X variables — exactly as written, punctuation and all. Weave prose AROUND these expressions.
3. Keep every number as digits (days, authority levels). Never spell numbers out, never add or drop one. COUNT REPETITIONS: if the input line states a number twice (e.g. "[day 294] ... valid from day 294"), your line must also contain it twice (e.g. "Logged on day 294: ... in force starting day 294").
4. Preserve meaning exactly: observation lines state a fact and its validity days; policy lines state a conditional (conditions imply a conclusion) with authority and effectivity, possibly with exceptions; notice lines state that one policy supersedes another from a day. Conditionals must remain clearly conditional (if/when/provided...); exceptions must remain clearly exceptional (unless/except...).
5. Vary sentence structure and vocabulary between lines — do not reuse one template. Change word order, voice, and scaffolding words freely.
6. Output format: exactly one output line per input line, numbered the same way ("1: ...", "2: ..."). No extra lines, no commentary, no code fences.

Example (input → acceptable output):
IN:  1: [day 12] Observation (fct_0007, source audit_note, valid from day 12): supplies(customer=customer_02, product=product_05).
OUT: 1: An audit_note entry logged on day 12 — reference fct_0007, effective from day 12 — confirms that supplies(customer=customer_02, product=product_05).`

type episodeResult struct {
	ID       string `json:"id"`
	Attempts int    `json:"attempts"`
	Fallback bool   `json:"fallback"`
	Error    string `json:"error,omitempty"`
}

type report struct {
	Dataset      string          `json:"dataset"`
	Model        string          `json:"model"`
	Episodes     int             `json:"episodes"`
	Paraphrased  int             `json:"paraphrased"`
	Fallbacks    int             `json:"fallbacks"`
	FallbackRate float64         `json:"fallback_rate"`
	Usage        json.RawMessage `json:"usage,omitempty"`
	PerEpisode   []episodeResult `json:"per_episode"`
}

func main() {
	dir := flag.String("dir", "dataset", "dataset directory")
	in := flag.String("in", "episodes.jsonl", "input episodes file (relative to -dir)")
	out := flag.String("out", "episodes_paraphrased.jsonl", "output file (relative to -dir)")
	reportPath := flag.String("report", "paraphrase-report.json", "report file (relative to -dir)")
	maxRetries := flag.Int("retries", 3, "per-episode paraphrase retries before falling back to original text")
	flag.Parse()

	model := os.Getenv("PARAPHRASE_LLM_MODEL")
	cacheDir := os.Getenv("PARAPHRASE_LLM_CACHE")
	if model == "" {
		fail(fmt.Errorf("PARAPHRASE_LLM_MODEL is required"))
	}
	if cacheDir == "" {
		fail(fmt.Errorf("PARAPHRASE_LLM_CACHE is required — paraphrase tiers must be replayable"))
	}
	var shape harness.RequestShape
	switch tv := os.Getenv("PARAPHRASE_LLM_TEMPERATURE"); tv {
	case "":
		// legacy default: temperature:0
	case "none":
		shape.OmitTemperature = true // gpt-5 / claude-sonnet-5 era models reject the field
	default:
		var f float64
		if _, err := fmt.Sscanf(tv, "%g", &f); err != nil {
			fail(fmt.Errorf("PARAPHRASE_LLM_TEMPERATURE=%q: not a float and not \"none\"", tv))
		}
		shape.Temperature = &f
	}
	if ep := os.Getenv("PARAPHRASE_LLM_EXTRA_PARAMS"); ep != "" {
		if err := json.Unmarshal([]byte(ep), &shape.ExtraParams); err != nil {
			fail(fmt.Errorf("PARAPHRASE_LLM_EXTRA_PARAMS: %w", err))
		}
	}
	var llm harness.LLMClient
	if base := os.Getenv("PARAPHRASE_LLM_BASE_URL"); base != "" {
		llm = &harness.OpenAICompatClient{
			BaseURL: base,
			APIKey:  os.Getenv("PARAPHRASE_LLM_KEY"),
			ModelID: model,
			Shape:   shape,
			Timeout: 240 * time.Second,
		}
	}
	llm = &harness.CachingLLMClient{
		Inner: llm, Dir: cacheDir,
		Mode:    harness.CacheMode(os.Getenv("PARAPHRASE_LLM_CACHE_MODE")),
		ModelID: model, Shape: shape,
	}
	meter := harness.NewMeteredLLM(llm)

	var episodes []gen.Episode
	readJSONL(filepath.Join(*dir, *in), func(raw []byte) {
		var ep gen.Episode
		fail2(json.Unmarshal(raw, &ep))
		episodes = append(episodes, ep)
	})
	if len(episodes) == 0 {
		fail(fmt.Errorf("no episodes in %s", filepath.Join(*dir, *in)))
	}

	rep := report{Dataset: *dir, Model: model, Episodes: len(episodes)}

	// Episodes are independent; paraphrase them with a bounded worker pool
	// (PARAPHRASE_CONCURRENCY, default 8 — the cassette cache makes reruns
	// incremental and the output below is written strictly in order).
	workers := 8
	if v := os.Getenv("PARAPHRASE_CONCURRENCY"); v != "" {
		fmt.Sscanf(v, "%d", &workers)
	}
	if workers < 1 {
		workers = 1
	}
	results := make([]episodeResult, len(episodes))
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i := range episodes {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = paraphraseEpisode(meter, &episodes[i], *maxRetries)
		}(i)
	}
	wg.Wait()

	outF, err := os.Create(filepath.Join(*dir, *out))
	fail2(err)
	defer outF.Close()
	enc := json.NewEncoder(outF)
	for i := range episodes {
		res := results[i]
		rep.PerEpisode = append(rep.PerEpisode, res)
		if res.Fallback {
			rep.Fallbacks++
			fmt.Fprintf(os.Stderr, "FALLBACK %s after %d attempts: %s\n", res.ID, res.Attempts, res.Error)
		} else {
			rep.Paraphrased++
		}
		fail2(enc.Encode(&episodes[i]))
	}

	rep.FallbackRate = float64(rep.Fallbacks) / float64(rep.Episodes)
	if u, err := json.Marshal(meter.Stats()); err == nil {
		rep.Usage = u
	}
	rb, err := json.MarshalIndent(rep, "", "  ")
	fail2(err)
	fail2(os.WriteFile(filepath.Join(*dir, *reportPath), rb, 0o644))

	fmt.Printf("paraphrased %d/%d episodes (%d fallbacks, rate %.1f%%) → %s\n",
		rep.Paraphrased, rep.Episodes, rep.Fallbacks, 100*rep.FallbackRate, filepath.Join(*dir, *out))
	if rep.FallbackRate > 0.10 {
		fmt.Fprintln(os.Stderr, "ERROR: fallback rate exceeds 10% — the paraphrase tier is INVALID (too much of the corpus is still templated). Fix the prompt or the paraphraser model.")
		os.Exit(1)
	}
}

// paraphraseEpisode rewrites one episode's text in place (episode text and
// per-event texts in lockstep). On unrecoverable validation failure the
// episode keeps its original text.
func paraphraseEpisode(llm harness.LLMClient, ep *gen.Episode, maxRetries int) episodeResult {
	res := episodeResult{ID: ep.ID}
	lines := strings.Split(ep.Text, "\n")
	if len(lines) == 0 {
		return res
	}
	header := lines[0] // "=== Episode ep_NNN (day D) ===" stays verbatim
	content := lines[1:]
	if len(content) != len(ep.Events) {
		// Defensive: the generator emits exactly one line per event.
		res.Fallback = true
		res.Error = fmt.Sprintf("line/event count mismatch: %d lines vs %d events", len(content), len(ep.Events))
		return res
	}

	var sb strings.Builder
	for i, l := range content {
		fmt.Fprintf(&sb, "%d: %s\n", i+1, l)
	}
	prompt := "Rewrite each numbered line. Same number of lines, same numbering.\n\n" + sb.String()

	feedback := ""
	for attempt := 1; attempt <= maxRetries; attempt++ {
		res.Attempts = attempt
		reply, err := llm.Complete(context.Background(), systemPrompt, prompt+feedback)
		if err != nil {
			res.Error = err.Error()
			continue
		}
		para, err := parseNumbered(reply, len(content))
		if err != nil {
			feedback = fmt.Sprintf("\n\nYour previous reply was rejected: %v. Reply with EXACTLY %d numbered lines.", err, len(content))
			res.Error = err.Error()
			continue
		}
		var violations []string
		for i := range content {
			if verr := validateLine(content[i], para[i]); verr != nil {
				violations = append(violations, fmt.Sprintf("line %d: %v", i+1, verr))
			}
		}
		if len(violations) > 0 {
			feedback = "\n\nYour previous reply was rejected by a mechanical validator:\n- " +
				strings.Join(violations, "\n- ") +
				"\nRewrite ALL lines again, fixing these violations. Identifiers, numbers, and name(slot=value, ...) expressions must be copied verbatim."
			res.Error = strings.Join(violations, "; ")
			continue
		}
		// Accepted: rewrite episode text and event texts in lockstep.
		rebuilt := append([]string{header}, para...)
		ep.Text = strings.Join(rebuilt, "\n")
		for i := range ep.Events {
			ep.Events[i].Text = para[i]
		}
		res.Error = ""
		return res
	}
	res.Fallback = true
	return res
}

func readJSONL(path string, handle func([]byte)) {
	f, err := os.Open(path)
	fail2(err)
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
	fail2(sc.Err())
}

func fail(err error) { fmt.Fprintln(os.Stderr, "error:", err); os.Exit(1) }
func fail2(err error) {
	if err != nil {
		fail(err)
	}
}
