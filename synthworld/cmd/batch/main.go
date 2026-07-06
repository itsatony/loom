// batch implements the pre-registered seed protocol (MASTERPLAN E0.6):
// generate candidate seeds start..start+candidates-1 IN ORDER, verify every
// dataset guarantee in-process (gen.VerifyDataset — same implementation as
// cmd/validate), apply the manifest quality gates, and keep the FIRST `keep`
// passers in numeric order.
//
// The seed list is thereby fixed by protocol, not by anyone's choice: no
// seed may be added or dropped afterwards for any reason other than a
// validator failure (which is an instrument bug to fix, not a data
// exclusion). This removes seed selection as a cherry-picking surface —
// results cannot be improved by quietly re-rolling unlucky seeds.
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

	"github.com/vaudience/synthworld/gen"
)

type candidateRecord struct {
	Seed    int64         `json:"seed"`
	Verdict string        `json:"verdict"` // kept | rejected | skipped
	Reasons []string      `json:"reasons,omitempty"`
	Stats   gen.GateStats `json:"stats"`
}

type batchManifest struct {
	Protocol struct {
		Preset     string             `json:"preset"`
		Start      int64              `json:"start"`
		Candidates int                `json:"candidates"`
		Keep       int                `json:"keep"`
		Gates      gen.GateThresholds `json:"gates"`
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
	flag.Parse()

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
	bm.KeptSeeds = []int64{}

	for i := 0; i < *candidates; i++ {
		seed := *start + int64(i)
		rec := candidateRecord{Seed: seed}
		if len(bm.KeptSeeds) >= *keep {
			// protocol satisfied; remaining candidates recorded as skipped so
			// the manifest documents the full candidate range
			rec.Verdict = "skipped"
			rec.Reasons = []string{"quota already met"}
			bm.Candidates = append(bm.Candidates, rec)
			continue
		}
		dir := filepath.Join(*out, fmt.Sprintf("seed-%d", seed))
		stats, reasons, err := runSeed(seed, *preset, dir)
		rec.Stats = stats
		if err != nil {
			rec.Verdict = "rejected"
			rec.Reasons = []string{err.Error()}
		} else if len(reasons) > 0 {
			rec.Verdict = "rejected"
			rec.Reasons = reasons
		} else {
			rec.Verdict = "kept"
			bm.KeptSeeds = append(bm.KeptSeeds, seed)
		}
		if rec.Verdict == "rejected" {
			if err := archiveRejected(dir, rejDir, seed); err != nil {
				fatal(fmt.Errorf("archive rejected seed %d: %w", seed, err))
			}
		}
		fmt.Printf("seed %-6d %-8s %v\n", seed, rec.Verdict, rec.Reasons)
		bm.Candidates = append(bm.Candidates, rec)
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
// Returns the gate stats, gate-violation reasons (nil = pass), and any
// generation/verification error.
func runSeed(seed int64, preset, dir string) (gen.GateStats, []string, error) {
	cfg, err := gen.PresetConfig(preset, seed)
	if err != nil {
		return gen.GateStats{}, nil, err
	}
	b := gen.NewBuilder(cfg)
	if err := b.BuildWorld(); err != nil {
		return gen.GateStats{}, nil, fmt.Errorf("build world: %w", err)
	}
	episodes := b.BuildEpisodes()
	qs, err := b.BuildQueries()
	if err != nil {
		return gen.GateStats{}, nil, fmt.Errorf("build queries: %w", err)
	}
	if err := b.WriteDataset(dir, episodes, qs); err != nil {
		return gen.GateStats{}, nil, err
	}
	stats := gen.ComputeGateStats(qs, b.Stats)
	// full guarantee verification, same code path as cmd/validate
	rep, err := gen.VerifyDataset(dir)
	if err != nil {
		return stats, nil, fmt.Errorf("verify: %w", err)
	}
	if !rep.OK() {
		return stats, nil, fmt.Errorf("verify: %d guarantee violations (instrument bug — fix the generator): %s", len(rep.Problems), rep.Problems[0])
	}
	return stats, gen.EvaluateGates(stats, gen.DefaultGateThresholds()), nil
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
