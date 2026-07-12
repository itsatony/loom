// naturalize produces the FRAMES TIER-M CORPUS (MASTERPLAN §9.6.6):
// episodes_natural.jsonl next to a frames dataset's episodes.jsonl, with
// every rendered text line rewritten into naturalistic business prose in
// which frame membership is carried by PRAGMATICS (attributed claims,
// direct quotes, story passages, named planning exercises, contextual
// irony) instead of tier-E markers. Three enforcement layers:
//
//  1. mechanical content preservation (validate.go): identifiers, numbers,
//     formal atoms verbatim; frame IDs banned and replaced by registered
//     handles; structural guards; banned marker vocabulary,
//  2. frame-recoverability audit (judge.go): a 3-judge panel outside the
//     evaluated matrix must recover every frame-bearing line's ground
//     truth at ≥2/3 exact agreement, else the episode is re-naturalized
//     with feedback (bounded rounds),
//  3. the batch-level authenticity certificate lives in cmd/authcert.
//
// Naturalizers and judges MUST be outside the evaluated model matrix
// (currently qwen36-nvfp4 / gpt-5-mini / claude-haiku-4-5), naturalizers
// from ≥2 families, judges disjoint from naturalizers. The caller
// configures five models via env (temperature defaults to 0; set
// _TEMPERATURE to a float or "none"):
//
//	NATURALIZE_LLM_A_BASE_URL / _MODEL / _KEY / _TEMPERATURE / _EXTRA_PARAMS
//	NATURALIZE_LLM_B_...   (second family)
//	NATURALIZE_JUDGE_1_... / NATURALIZE_JUDGE_2_... / NATURALIZE_JUDGE_3_...
//	NATURALIZE_CACHE       cassette dir (mandatory — replayable tiers)
//	NATURALIZE_CACHE_MODE  auto (default) | record | replay
//	NATURALIZE_CONCURRENCY worker pool size (default 4)
//
// Episodes split between the two naturalizers by index parity (deterministic).
// Structured payloads, episode IDs, and days are copied through untouched:
// easy mode (C2a) is identical on both streams by construction.
//
// Usage:
//
//	go run ./cmd/naturalize -dir <dataset> [-no-judge] [-limit N]
//	go run ./cmd/harness   -dir <dataset> -episodes episodes_natural.jsonl
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

type modelCfg struct {
	Label string
	Meter *harness.MeteredLLM
	Model string
}

// llmFromEnv builds a cached+metered client from <prefix>_* env vars.
// Returns nil if <prefix>_MODEL is unset.
func llmFromEnv(prefix, cacheDir string) *modelCfg {
	model := os.Getenv(prefix + "_MODEL")
	if model == "" {
		return nil
	}
	base := os.Getenv(prefix + "_BASE_URL")
	if base == "" {
		fail(fmt.Errorf("%s_MODEL is set but %s_BASE_URL is not", prefix, prefix))
	}
	var shape harness.RequestShape
	switch tv := os.Getenv(prefix + "_TEMPERATURE"); tv {
	case "":
		// default: temperature:0
	case "none":
		shape.OmitTemperature = true
	default:
		var f float64
		if _, err := fmt.Sscanf(tv, "%g", &f); err != nil {
			fail(fmt.Errorf("%s_TEMPERATURE=%q: not a float and not \"none\"", prefix, tv))
		}
		shape.Temperature = &f
	}
	if ep := os.Getenv(prefix + "_EXTRA_PARAMS"); ep != "" {
		if err := json.Unmarshal([]byte(ep), &shape.ExtraParams); err != nil {
			fail(fmt.Errorf("%s_EXTRA_PARAMS: %w", prefix, err))
		}
	}
	var llm harness.LLMClient = &harness.OpenAICompatClient{
		BaseURL: base,
		APIKey:  os.Getenv(prefix + "_KEY"),
		ModelID: model,
		Shape:   shape,
		Timeout: 300 * time.Second,
	}
	llm = &harness.CachingLLMClient{
		Inner: llm, Dir: cacheDir,
		Mode:    harness.CacheMode(os.Getenv("NATURALIZE_CACHE_MODE")),
		ModelID: model, Shape: shape,
	}
	return &modelCfg{Label: prefix, Meter: harness.NewMeteredLLM(llm), Model: model}
}

type lineReport struct {
	Line     int    `json:"line"` // 1-based within the episode
	Expected string `json:"expected"`
	Exact    int    `json:"exact_judges"`
	Labels   string `json:"labels,omitempty"`
}

type episodeResult struct {
	ID           string       `json:"id"`
	Naturalizer  string       `json:"naturalizer"`
	MechAttempts int          `json:"mech_attempts"`
	JudgeRounds  int          `json:"judge_rounds,omitempty"`
	Fallback     bool         `json:"fallback,omitempty"`
	Unrecovered  []lineReport `json:"unrecovered,omitempty"`
	// ActualCtxMisses counts plain actual|statement lines where <2/3
	// judges got the CONTEXT right (reported control, re-naturalized too).
	ActualCtxMisses int    `json:"actual_ctx_misses,omitempty"`
	FrameLines      int    `json:"frame_lines"`
	ActualLines     int    `json:"actual_lines"`
	Error           string `json:"error,omitempty"`
}

type report struct {
	Dataset            string                     `json:"dataset"`
	Naturalizers       []string                   `json:"naturalizers"`
	Judges             []string                   `json:"judges"`
	Episodes           int                        `json:"episodes"`
	Fallbacks          int                        `json:"fallbacks"`
	FrameFallbacks     int                        `json:"frame_fallbacks"` // fallback episodes containing frame-bearing lines; must be 0
	FallbackRate       float64                    `json:"fallback_rate"`
	FrameLines         int                        `json:"frame_lines"`
	UnrecoveredLines   int                        `json:"unrecovered_lines"`
	UnrecoveredRate    float64                    `json:"unrecovered_rate"`
	ActualLinesJudged  int                        `json:"actual_lines_judged"`
	ActualCtxMisses    int                        `json:"actual_ctx_misses"`
	ActualCtxMissRate  float64                    `json:"actual_ctx_miss_rate"`
	Handles            []frameHandle              `json:"handles"`
	Usage              map[string]json.RawMessage `json:"usage,omitempty"`
	PerEpisode         []episodeResult            `json:"per_episode"`
	JudgingEnabled     bool                       `json:"judging_enabled"`
	MechRetries        int                        `json:"mech_retries"`
	JudgeRoundsAllowed int                        `json:"judge_rounds_allowed"`
}

func main() {
	dir := flag.String("dir", "dataset", "dataset directory")
	in := flag.String("in", "episodes.jsonl", "input episodes file (relative to -dir)")
	out := flag.String("out", "episodes_natural.jsonl", "output file (relative to -dir)")
	reportPath := flag.String("report", "naturalize-report.json", "report file (relative to -dir)")
	mechRetries := flag.Int("retries", 5, "per-naturalization mechanical-validation retries")
	judgeRounds := flag.Int("judge-rounds", 3, "naturalize+judge rounds before accepting with unrecovered lines")
	limit := flag.Int("limit", 0, "process only the first N episodes (0 = all; dev iteration)")
	noJudge := flag.Bool("no-judge", false, "skip the judge panel (mechanical validation only; dev iteration)")
	flag.Parse()

	cacheDir := os.Getenv("NATURALIZE_CACHE")
	if cacheDir == "" {
		fail(fmt.Errorf("NATURALIZE_CACHE is required — naturalized tiers must be replayable"))
	}
	natA := llmFromEnv("NATURALIZE_LLM_A", cacheDir)
	natB := llmFromEnv("NATURALIZE_LLM_B", cacheDir)
	if natA == nil || natB == nil {
		fail(fmt.Errorf("both NATURALIZE_LLM_A_* and NATURALIZE_LLM_B_* are required (≥2 model families, MASTERPLAN §9.6.6)"))
	}
	if natA.Model == natB.Model {
		fail(fmt.Errorf("NATURALIZE_LLM_A and NATURALIZE_LLM_B must be different models (family diversity)"))
	}
	var judges []*modelCfg
	if !*noJudge {
		for _, p := range []string{"NATURALIZE_JUDGE_1", "NATURALIZE_JUDGE_2", "NATURALIZE_JUDGE_3"} {
			j := llmFromEnv(p, cacheDir)
			if j == nil {
				fail(fmt.Errorf("%s_* is required (3-judge panel; use -no-judge for mechanical-only dev runs)", p))
			}
			for _, n := range []*modelCfg{natA, natB} {
				if j.Model == n.Model {
					fail(fmt.Errorf("judge %s duplicates naturalizer %s — a model must never grade text it wrote", j.Model, n.Model))
				}
			}
			judges = append(judges, j)
		}
	}

	var episodes []gen.Episode
	readJSONL(filepath.Join(*dir, *in), func(raw []byte) {
		var ep gen.Episode
		fail2(json.Unmarshal(raw, &ep))
		episodes = append(episodes, ep)
	})
	if len(episodes) == 0 {
		fail(fmt.Errorf("no episodes in %s", filepath.Join(*dir, *in)))
	}
	if *limit > 0 && *limit < len(episodes) {
		episodes = episodes[:*limit]
	}

	salt := datasetSalt(*dir)
	handles := buildHandles(episodes, salt)

	rep := report{
		Dataset:            *dir,
		Naturalizers:       []string{natA.Model, natB.Model},
		Episodes:           len(episodes),
		Handles:            sortedHandles(handles),
		JudgingEnabled:     !*noJudge,
		MechRetries:        *mechRetries,
		JudgeRoundsAllowed: *judgeRounds,
	}
	for _, j := range judges {
		rep.Judges = append(rep.Judges, j.Model)
	}

	// Declaration episodes are processed first, sequentially, so every
	// later episode's judges see the naturalized declaration lines a
	// sequential reader would have seen. declLines[i] = declaration lines
	// from episodes with index ≤ i, in stream order.
	declEpisode := make([]bool, len(episodes))
	for i := range episodes {
		for _, ev := range episodes[i].Events {
			if ev.Kind == gen.EvFrame {
				declEpisode[i] = true
			}
		}
	}

	workers := 4
	if v := os.Getenv("NATURALIZE_CONCURRENCY"); v != "" {
		fmt.Sscanf(v, "%d", &workers)
	}
	if workers < 1 {
		workers = 1
	}

	results := make([]episodeResult, len(episodes))
	proc := &processor{
		natA: natA, natB: natB, judges: judges,
		handles: handles, mechRetries: *mechRetries, judgeRounds: *judgeRounds,
	}

	// Pass 1: declaration episodes, in order.
	var declSoFar []string
	declAt := make([][]string, len(episodes)) // context snapshot per decl episode
	for i := range episodes {
		if !declEpisode[i] {
			continue
		}
		declAt[i] = append([]string(nil), declSoFar...)
		results[i] = proc.process(i, &episodes[i], declAt[i])
		for j, ev := range episodes[i].Events {
			if ev.Kind == gen.EvFrame {
				_ = j
				declSoFar = append(declSoFar, ev.Text) // now-naturalized decl line
			}
		}
	}
	// Context for non-declaration episodes: all declarations from earlier
	// (or same-index, impossible here) episodes. Since frame content only
	// appears after its declaration in the stream, giving every later
	// episode the full declaration list of episodes < i is exact; we
	// precompute the running list.
	ctxFor := make([][]string, len(episodes))
	var running []string
	for i := range episodes {
		ctxFor[i] = append([]string(nil), running...)
		if declEpisode[i] {
			for _, ev := range episodes[i].Events {
				if ev.Kind == gen.EvFrame {
					running = append(running, ev.Text)
				}
			}
		}
	}

	// Pass 2: everything else, worker pool.
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i := range episodes {
		if declEpisode[i] {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = proc.process(i, &episodes[i], ctxFor[i])
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
			if res.FrameLines > 0 {
				rep.FrameFallbacks++
			}
			fmt.Fprintf(os.Stderr, "FALLBACK %s after %d attempts: %s\n", res.ID, res.MechAttempts, res.Error)
		}
		rep.FrameLines += res.FrameLines
		rep.UnrecoveredLines += len(res.Unrecovered)
		rep.ActualLinesJudged += res.ActualLines
		rep.ActualCtxMisses += res.ActualCtxMisses
		fail2(enc.Encode(&episodes[i]))
	}
	rep.FallbackRate = float64(rep.Fallbacks) / float64(rep.Episodes)
	if rep.FrameLines > 0 {
		rep.UnrecoveredRate = float64(rep.UnrecoveredLines) / float64(rep.FrameLines)
	}
	if rep.ActualLinesJudged > 0 {
		rep.ActualCtxMissRate = float64(rep.ActualCtxMisses) / float64(rep.ActualLinesJudged)
	}
	rep.Usage = map[string]json.RawMessage{}
	for _, m := range append([]*modelCfg{natA, natB}, judges...) {
		if u, err := json.Marshal(m.Meter.Stats()); err == nil {
			rep.Usage[m.Model] = u
		}
	}
	rb, err := json.MarshalIndent(rep, "", "  ")
	fail2(err)
	fail2(os.WriteFile(filepath.Join(*dir, *reportPath), rb, 0o644))

	fmt.Printf("naturalized %d episodes → %s\n", rep.Episodes, filepath.Join(*dir, *out))
	fmt.Printf("  fallbacks: %d (%.1f%%)  frame-bearing lines: %d  unrecovered: %d (%.1f%%)  actual-ctx misses: %d/%d (%.1f%%)\n",
		rep.Fallbacks, 100*rep.FallbackRate, rep.FrameLines, rep.UnrecoveredLines, 100*rep.UnrecoveredRate,
		rep.ActualCtxMisses, rep.ActualLinesJudged, 100*rep.ActualCtxMissRate)

	bad := false
	if rep.FallbackRate > 0.10 {
		fmt.Fprintln(os.Stderr, "ERROR: fallback rate exceeds 10% — tier M is INVALID (too much tier-E text survives).")
		bad = true
	}
	if rep.FrameFallbacks > 0 {
		fmt.Fprintf(os.Stderr, "ERROR: %d frame-bearing episode(s) fell back to tier-E text — tier M is INVALID (marker leak on trap-bearing lines).\n", rep.FrameFallbacks)
		bad = true
	}
	if !*noJudge && rep.UnrecoveredRate > 0.05 {
		fmt.Fprintln(os.Stderr, "ERROR: >5% of frame-bearing lines are not recoverable by the judge panel — tier M is INVALID; regenerate harder (better prompts/models) and log the failure.")
		bad = true
	}
	if bad {
		os.Exit(1)
	}
}

type processor struct {
	natA, natB  *modelCfg
	judges      []*modelCfg
	handles     map[string]frameHandle
	mechRetries int
	judgeRounds int
}

// process naturalizes one episode in place and returns its result.
func (p *processor) process(idx int, ep *gen.Episode, declCtx []string) episodeResult {
	nat := p.natA
	if idx%2 == 1 {
		nat = p.natB
	}
	res := episodeResult{ID: ep.ID, Naturalizer: nat.Model}

	lines := strings.Split(ep.Text, "\n")
	if len(lines) != len(ep.Events)+1 {
		res.Fallback = true
		res.Error = fmt.Sprintf("line/event count mismatch: %d text lines vs %d events", len(lines)-1, len(ep.Events))
		return res
	}
	header := lines[0]

	specs := make([]lineSpec, len(ep.Events))
	expected := make([]lineLabel, len(ep.Events))
	for i := range ep.Events {
		specs[i] = buildSpec(&ep.Events[i], p.handles)
		expected[i] = expectedLabel(&ep.Events[i], p.handles)
		if frameBearing(expected[i]) {
			res.FrameLines++
		} else {
			res.ActualLines++
		}
	}

	ctx := context.Background()
	judgeFeedback := ""
	var natLines []string
	var lastVerdicts []judgeVerdict

	rounds := p.judgeRounds
	if len(p.judges) == 0 {
		rounds = 1
	}
	for round := 1; round <= rounds; round++ {
		nl, attempts, err := p.naturalizeOnce(ctx, nat, ep, judgeFeedback)
		res.MechAttempts += attempts
		if err != nil && res.FrameLines > 0 {
			// A tier-E frame line inside tier M is a marker leak exactly
			// where the trap atoms live — escalate frame-bearing episodes
			// to the other naturalizer before ever falling back.
			other := p.natA
			if nat == p.natA {
				other = p.natB
			}
			nl, attempts, err = p.naturalizeOnce(ctx, other, ep, judgeFeedback)
			res.MechAttempts += attempts
			if err == nil {
				res.Naturalizer = other.Model
				nat = other
			}
		}
		if err != nil {
			res.Fallback = true
			res.Error = err.Error()
			return res
		}
		natLines = nl
		if len(p.judges) == 0 {
			break
		}
		res.JudgeRounds = round
		lastVerdicts = make([]judgeVerdict, len(p.judges))
		var jwg sync.WaitGroup
		for ji, j := range p.judges {
			jwg.Add(1)
			go func(ji int, j *modelCfg) {
				defer jwg.Done()
				lastVerdicts[ji] = judgeEpisode(ctx, j.Meter, declCtx, natLines, 2)
			}(ji, j)
		}
		jwg.Wait()

		var fails []int
		for i := range expected {
			exact, ctxOnly := tallyLine(lastVerdicts, i, expected[i])
			if frameBearing(expected[i]) {
				if exact < 2 {
					fails = append(fails, i)
				}
			} else if ctxOnly < 2 {
				fails = append(fails, i)
			}
		}
		if len(fails) == 0 {
			break
		}
		if round == rounds {
			// Accept with unrecovered lines recorded.
			for _, i := range fails {
				exact, ctxOnly := tallyLine(lastVerdicts, i, expected[i])
				if frameBearing(expected[i]) {
					res.Unrecovered = append(res.Unrecovered, lineReport{
						Line: i + 1, Expected: expected[i].String(), Exact: exact,
						Labels: labelsString(lastVerdicts, i),
					})
				} else {
					_ = ctxOnly
					res.ActualCtxMisses++
				}
			}
			break
		}
		judgeFeedback = buildJudgeFeedback(fails, expected, lastVerdicts, natLines)
	}

	rebuilt := append([]string{header}, natLines...)
	ep.Text = strings.Join(rebuilt, "\n")
	for i := range ep.Events {
		ep.Events[i].Text = natLines[i]
	}
	return res
}

// naturalizeOnce runs the mechanical-validation retry loop for one episode.
func (p *processor) naturalizeOnce(ctx context.Context, nat *modelCfg, ep *gen.Episode, judgeFeedback string) ([]string, int, error) {
	prompt := naturalizeUserMsg(ep, p.handles, judgeFeedback)
	feedback := ""
	var lastErr error
	for attempt := 1; attempt <= p.mechRetries; attempt++ {
		reply, err := nat.Meter.Complete(ctx, naturalizerSystem, prompt+feedback)
		if err != nil {
			lastErr = err
			continue
		}
		natLines, err := parseNumbered(reply, len(ep.Events))
		if err != nil {
			feedback = fmt.Sprintf("\n\nYour previous reply was rejected: %v. Reply with EXACTLY %d numbered lines.", err, len(ep.Events))
			lastErr = err
			continue
		}
		var violations []string
		var passed []int
		for i := range ep.Events {
			sp := buildSpec(&ep.Events[i], p.handles)
			if verr := validateLine(ep.Events[i].Text, natLines[i], sp); verr != nil {
				violations = append(violations, fmt.Sprintf("line %d: %v", i+1, verr))
			} else {
				passed = append(passed, i+1)
			}
		}
		if len(violations) > 0 {
			if os.Getenv("NATURALIZE_DEBUG") != "" {
				fmt.Fprintf(os.Stderr, "DEBUG %s attempt %d (%s) rejected:\n%s\nviolations: %s\n",
					ep.ID, attempt, nat.Model, reply, strings.Join(violations, "; "))
			}
			var keep string
			if len(passed) > 0 {
				keep = fmt.Sprintf(" Lines %v were ACCEPTED — reproduce them VERBATIM from your previous reply; change ONLY the rejected lines.", passed)
			}
			feedback = "\n\nYour previous reply was rejected by a mechanical validator:\n- " +
				strings.Join(violations, "\n- ") +
				"\nOutput all lines again, fixing exactly these violations." + keep
			lastErr = fmt.Errorf("%s", strings.Join(violations, "; "))
			continue
		}
		return natLines, attempt, nil
	}
	return nil, p.mechRetries, fmt.Errorf("mechanical validation failed after %d attempts: %v", p.mechRetries, lastErr)
}

// buildJudgeFeedback tells the naturalizer which lines independent readers
// could not place correctly, and what each line really is.
func buildJudgeFeedback(fails []int, expected []lineLabel, verdicts []judgeVerdict, natLines []string) string {
	var sb strings.Builder
	sb.WriteString("\n\nIndependent readers were asked to classify each of your lines; the following lines were NOT recoverable. Rewrite ALL lines again (you may keep acceptable ones close to your previous wording), making each failed line's true status clear from wording and context alone — still WITHOUT the forbidden words:\n")
	for _, i := range fails {
		fmt.Fprintf(&sb, "- line %d is really [%s] but readers labeled it %s. Your wording was: %q\n",
			i+1, expected[i].String(), labelsString(verdicts, i), natLines[i])
	}
	return sb.String()
}

func labelsString(verdicts []judgeVerdict, i int) string {
	var parts []string
	for _, v := range verdicts {
		if i < len(v.Labels) && v.Labels[i] != nil {
			parts = append(parts, v.Labels[i].String())
		} else {
			parts = append(parts, "(unparseable)")
		}
	}
	return strings.Join(parts, " / ")
}

// datasetSalt reads the dataset seed from manifest.json so handle choices
// differ across seeds; falls back to the directory base name.
func datasetSalt(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err == nil {
		var m struct {
			Seed int64 `json:"seed"`
		}
		if json.Unmarshal(b, &m) == nil && m.Seed != 0 {
			return fmt.Sprintf("seed-%d", m.Seed)
		}
	}
	return filepath.Base(dir)
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
