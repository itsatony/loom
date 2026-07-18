// aggregate implements the pre-registered endpoint arithmetic of
// MASTERPLAN §1.1 (the H5 kill-criterion test) over per-seed harness
// reports.
//
// REGISTRATION NOTICE: this tool is part of the campaign registration.
// After E4 data exists, changes to the arithmetic here (metric
// definitions, bootstrap parameters, percentile method, thresholds)
// require a dated MASTERPLAN §10 amendment — never a silent edit.
//
// Registered constants: 10,000 bootstrap resamples over seeds with
// replacement; RNG = math/rand with fixed seed 42; CI = nearest-rank
// 2.5th/97.5th percentiles (sorted ascending, indices 249 and 9749);
// verdict PASS iff composition-difference CI lower bound >= +0.15 AND
// repetition-difference CI lower bound >= -0.02.
//
// Usage:
//
//	aggregate -reports 'results/seed-*/report.json' -a loom-C2b -b rag-bm25 [-json out.json]
//
// Each report file is the -json output of cmd/harness for one seed and
// must contain both conditions. The seed label is parsed from the file
// path (last "seed-<label>" or "seed_<label>" segment; else the base
// filename). The primary endpoint is the paired per-seed difference in
// composition balanced accuracy (A − B); secondary endpoints reuse the
// same machinery.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/vaudience/synthworld/harness"
)

func main() {
	reportsFlag := flag.String("reports", "", "glob or comma-separated list of per-seed harness report JSON files")
	condA := flag.String("a", "", "condition A (the challenger, e.g. loom-C2b)")
	condB := flag.String("b", "", "condition B (the baseline, e.g. strongest C1)")
	jsonOut := flag.String("json", "", "optional path for the JSON result")
	framesMode := flag.Bool("frames", false, "frames-v1 endpoint arithmetic (F-E1/F-E2/F-E4, MASTERPLAN §9.6.7): -a = frames condition, -b = primary null")
	condB2 := flag.String("b2", "", "frames mode: optional ceiling null (e.g. frame-rag); ratified F-E2 requires -a to beat the HARDER of -b and -b2 on filtering-resistant")
	flag.Parse()

	if *reportsFlag == "" || *condA == "" || *condB == "" {
		fmt.Fprintln(os.Stderr, "usage: aggregate -reports <glob-or-list> -a <conditionA> -b <conditionB> [-json out.json]")
		os.Exit(2)
	}

	paths, err := resolveReportPaths(*reportsFlag)
	if err != nil {
		fatal(err)
	}
	seeds, err := loadSeedReports(paths)
	if err != nil {
		fatal(err)
	}
	var render string
	var result any
	if *framesMode {
		res, err := AnalyzeFrames(seeds, *condA, *condB, *condB2)
		if err != nil {
			fatal(err)
		}
		render, result = res.Render(), res
	} else {
		res, err := Analyze(seeds, *condA, *condB)
		if err != nil {
			fatal(err)
		}
		render, result = res.Render(), res
	}
	fmt.Print(render)

	if *jsonOut != "" {
		f, err := os.Create(*jsonOut)
		if err != nil {
			fatal(err)
		}
		enc := json.NewEncoder(f)
		enc.SetIndent("", " ")
		if err := enc.Encode(result); err != nil {
			fatal(err)
		}
		f.Close()
		fmt.Printf("\nJSON result: %s\n", *jsonOut)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

// resolveReportPaths expands a glob or a comma-separated list.
func resolveReportPaths(spec string) ([]string, error) {
	var paths []string
	if strings.Contains(spec, ",") {
		for _, p := range strings.Split(spec, ",") {
			if p = strings.TrimSpace(p); p != "" {
				paths = append(paths, p)
			}
		}
	} else {
		matches, err := filepath.Glob(spec)
		if err != nil {
			return nil, fmt.Errorf("bad glob %q: %w", spec, err)
		}
		paths = matches
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no report files match %q", spec)
	}
	sort.Strings(paths)
	return paths, nil
}

// SeedReports is one seed's harness output: condition name -> report.
type SeedReports struct {
	Seed    string
	Reports map[string]*harness.Report
}

// loadSeedReports reads each file (a JSON array of harness.Report) and
// labels it with the seed parsed from its path. Duplicate seed labels are
// an error: silent overwrites would corrupt pairing.
func loadSeedReports(paths []string) ([]SeedReports, error) {
	var out []SeedReports
	seen := map[string]string{}
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var reports []*harness.Report
		if err := json.Unmarshal(raw, &reports); err != nil {
			return nil, fmt.Errorf("%s: not a harness report array: %w", p, err)
		}
		seed := seedLabel(p)
		if prev, dup := seen[seed]; dup {
			return nil, fmt.Errorf("seed label %q parsed from both %s and %s — ambiguous pairing", seed, prev, p)
		}
		seen[seed] = p
		sr := SeedReports{Seed: seed, Reports: map[string]*harness.Report{}}
		for _, r := range reports {
			sr.Reports[r.Condition] = r
		}
		out = append(out, sr)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seed < out[j].Seed })
	return out, nil
}

// seedLabel extracts the seed from a report path: the LAST path segment
// piece matching seed-<label> / seed_<label> (case-insensitive), else the
// base filename without extension.
func seedLabel(path string) string {
	lower := strings.ToLower(path)
	idx := strings.LastIndex(lower, "seed-")
	if j := strings.LastIndex(lower, "seed_"); j > idx {
		idx = j
	}
	if idx >= 0 {
		rest := path[idx+len("seed-"):]
		end := strings.IndexAny(rest, "/\\.")
		if end == -1 {
			end = len(rest)
		}
		if end > 0 {
			return rest[:end]
		}
	}
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}
