// batch implements the pre-registered seed protocol (MASTERPLAN E0.6; frames
// edition §9.6.8): generate candidate seeds start..start+candidates-1 IN
// ORDER, verify every dataset guarantee in-process (gen.VerifyDataset — same
// implementation as cmd/validate), apply the manifest quality gates, and keep
// the FIRST `keep` passers in numeric order.
//
// The seed list is thereby fixed by protocol, not by anyone's choice: no
// seed may be added or dropped afterwards for any reason other than a
// validator failure (which is an instrument bug to fix, not a data
// exclusion). This removes seed selection as a cherry-picking surface —
// results cannot be improved by quietly re-rolling unlucky seeds.
//
// For the frames preset (§9.6.8) candidates are additionally gated on the
// frames gates (gap traps, scenario chains, pinned+live scenario presence,
// per-frame firing hygiene) ON TOP of all v0 gates.
//
// Candidates may generate concurrently (-workers > 1); the keep/skip
// decision is still made strictly in numeric order after all in-flight
// candidates settle, so the locked list — and the batch manifest — are
// identical to a sequential run. Seeds past the quota point have their
// datasets removed and are recorded as skipped, exactly as if never built.
//
// Outputs:
//   - <out>/seed-<N>/            kept datasets (world/episodes/queries/manifest)
//   - <out>/rejected-manifests/  manifest.json of each rejected seed, for the record
//   - <out>/batch-manifest.json  protocol parameters, per-candidate verdicts +
//     gate stats + rejection reasons, and the final locked seed list
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/vaudience/synthworld/gen"
)

type candidateRecord struct {
	Seed        int64                `json:"seed"`
	Verdict     string               `json:"verdict"` // kept | rejected | skipped
	Reasons     []string             `json:"reasons,omitempty"`
	Stats       gen.GateStats        `json:"stats"`
	FramesStats *gen.FramesGateStats `json:"frames_stats,omitempty"`
}

type batchManifest struct {
	Protocol struct {
		Preset      string                    `json:"preset"`
		Start       int64                     `json:"start"`
		Candidates  int                       `json:"candidates"`
		Keep        int                       `json:"keep"`
		Gates       gen.GateThresholds        `json:"gates"`
		FramesGates *gen.FramesGateThresholds `json:"frames_gates,omitempty"`
	} `json:"protocol"`
	Candidates []candidateRecord `json:"candidates"`
	KeptSeeds  []int64           `json:"kept_seeds"`
}

func main() {
	out := flag.String("out", "batch-out", "output directory")
	preset := flag.String("preset", "batch", "generation preset")
	candidates := flag.Int("candidates", 40, "number of candidate seeds to try, in order")
	keep := flag.Int("keep", 20, "number of passing seeds to keep (first passers in numeric order)")
	start := flag.Int64("start", 1, "first candidate seed")
	workers := flag.Int("workers", 1, "concurrent candidate builds (verdicts still applied in numeric order)")
	flag.Parse()

	if *workers < 1 {
		fatal(fmt.Errorf("-workers must be >= 1"))
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fatal(err)
	}
	rejDir := filepath.Join(*out, "rejected-manifests")

	var bm batchManifest
	bm.Protocol.Preset = *preset
	bm.Protocol.Start = *start
	bm.Protocol.Candidates = *candidates
	bm.Protocol.Keep = *keep
	bm.Protocol.Gates = gen.DefaultGateThresholds()
	if gen.FramesPreset(*preset) != nil {
		th := gen.DefaultFramesGateThresholds()
		bm.Protocol.FramesGates = &th
	}
	bm.KeptSeeds = []int64{}

	// Run candidates through a worker pool. The dispatcher stops handing out
	// new seeds once the contiguous decided prefix already contains `keep`
	// passers — the exact point a sequential run would have stopped.
	results := make([]*candidateRecord, *candidates)
	var mu sync.Mutex

	quotaMet := func() bool {
		kept := 0
		for _, r := range results {
			if r == nil {
				return false // prefix not contiguous yet
			}
			if r.Verdict == "kept" {
				kept++
				if kept >= *keep {
					return true
				}
			}
		}
		return false
	}

	idxCh := make(chan int)
	go func() {
		defer close(idxCh)
		for i := 0; i < *candidates; i++ {
			mu.Lock()
			stop := quotaMet()
			mu.Unlock()
			if stop {
				return
			}
			idxCh <- i
		}
	}()

	var wg sync.WaitGroup
	for w := 0; w < *workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range idxCh {
				seed := *start + int64(i)
				dir := filepath.Join(*out, fmt.Sprintf("seed-%d", seed))
				rec := candidateRecord{Seed: seed}
				stats, fstats, reasons, err := runSeed(seed, *preset, dir)
				rec.Stats = stats
				rec.FramesStats = fstats
				if err != nil {
					rec.Verdict = "rejected"
					rec.Reasons = []string{err.Error()}
				} else if len(reasons) > 0 {
					rec.Verdict = "rejected"
					rec.Reasons = reasons
				} else {
					rec.Verdict = "kept" // provisional; numeric-order pass below finalizes
				}
				if rec.Verdict == "rejected" {
					if aerr := archiveRejected(dir, rejDir, seed); aerr != nil {
						fatal(fmt.Errorf("archive rejected seed %d: %w", seed, aerr))
					}
				}
				fmt.Printf("seed %-6d %-8s %v\n", seed, rec.Verdict, rec.Reasons)
				mu.Lock()
				results[i] = &rec
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	// Finalize in numeric order: first `keep` passers are the locked set;
	// everything after the quota point is skipped (dataset removed), whether
	// it was built or not — identical to the sequential protocol.
	kept := 0
	for i := 0; i < *candidates; i++ {
		seed := *start + int64(i)
		rec := results[i]
		if kept >= *keep {
			if rec != nil && rec.Verdict == "kept" {
				if err := os.RemoveAll(filepath.Join(*out, fmt.Sprintf("seed-%d", seed))); err != nil {
					fatal(err)
				}
			}
			bm.Candidates = append(bm.Candidates, candidateRecord{
				Seed:    seed,
				Verdict: "skipped",
				Reasons: []string{"quota already met"},
			})
			continue
		}
		if rec == nil {
			// dispatcher stopped before this seed but quota not met — only
			// possible if workers died, which fatal() already handles
			fatal(fmt.Errorf("internal: seed %d undecided with quota unmet", seed))
		}
		if rec.Verdict == "kept" {
			kept++
			bm.KeptSeeds = append(bm.KeptSeeds, seed)
		}
		bm.Candidates = append(bm.Candidates, *rec)
	}

	if err := writeJSON(filepath.Join(*out, "batch-manifest.json"), &bm); err != nil {
		fatal(err)
	}
	fmt.Printf("kept %d/%d seeds: %v\n", len(bm.KeptSeeds), *keep, bm.KeptSeeds)
	if len(bm.KeptSeeds) < *keep {
		fatal(fmt.Errorf("only %d of %d requested seeds passed the gates; widen -candidates or fix the generator (never the thresholds)", len(bm.KeptSeeds), *keep))
	}
}

// runSeed generates, writes, verifies, and gates one candidate seed.
// Returns the gate stats, frames gate stats (nil for v0 presets), gate-
// violation reasons (nil = pass), and any generation/verification error.
func runSeed(seed int64, preset, dir string) (gen.GateStats, *gen.FramesGateStats, []string, error) {
	cfg, err := gen.PresetConfig(preset, seed)
	if err != nil {
		return gen.GateStats{}, nil, nil, err
	}
	b := gen.NewBuilder(cfg)
	fc := gen.FramesPreset(preset)
	if fc != nil {
		b.EnableFrames(fc)
	}
	if err := b.BuildWorld(); err != nil {
		return gen.GateStats{}, nil, nil, fmt.Errorf("build world: %w", err)
	}
	episodes := b.BuildEpisodes()
	qs, err := b.BuildQueries()
	if err != nil {
		return gen.GateStats{}, nil, nil, fmt.Errorf("build queries: %w", err)
	}
	if err := b.WriteDataset(dir, episodes, qs); err != nil {
		return gen.GateStats{}, nil, nil, err
	}
	stats := gen.ComputeGateStats(qs, b.Stats)
	var fstats *gen.FramesGateStats
	if fc != nil {
		fs := gen.ComputeFramesGateStats(qs, b.FrameStats)
		fstats = &fs
	}
	// full guarantee verification, same code path as cmd/validate
	rep, err := gen.VerifyDataset(dir)
	if err != nil {
		return stats, fstats, nil, fmt.Errorf("verify: %w", err)
	}
	if !rep.OK() {
		return stats, fstats, nil, fmt.Errorf("verify: %d guarantee violations (instrument bug — fix the generator): %s", len(rep.Problems), rep.Problems[0])
	}
	reasons := gen.EvaluateGates(stats, gen.DefaultGateThresholds())
	if fstats != nil {
		reasons = append(reasons, gen.EvaluateFramesGates(*fstats, gen.DefaultFramesGateThresholds())...)
	}
	return stats, fstats, reasons, nil
}

// archiveRejected preserves the rejected seed's manifest for the record and
// removes the dataset itself.
func archiveRejected(dir, rejDir string, seed int64) error {
	src := filepath.Join(dir, "manifest.json")
	if _, err := os.Stat(src); err == nil {
		if err := os.MkdirAll(rejDir, 0o755); err != nil {
			return err
		}
		raw, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(rejDir, fmt.Sprintf("seed-%d.manifest.json", seed)), raw, 0o644); err != nil {
			return err
		}
	}
	return os.RemoveAll(dir)
}

func writeJSON(path string, v any) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return err
	}
	return f.Close()
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
