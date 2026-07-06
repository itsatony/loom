package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/vaudience/synthworld/harness"
)

func TestBalancedAcc(t *testing.T) {
	cases := []struct {
		name string
		s    harness.SliceScore
		want float64
	}{
		{"both sides", harness.SliceScore{PosCorrect: 30, PosTotal: 40, NegCorrect: 20, NegTotal: 20}, (0.75 + 1.0) / 2},
		{"always-true gaming punished", harness.SliceScore{PosCorrect: 40, PosTotal: 40, NegCorrect: 0, NegTotal: 33}, 0.5},
		{"pos side only", harness.SliceScore{PosCorrect: 3, PosTotal: 4}, 0.75},
		{"neg side only", harness.SliceScore{NegCorrect: 1, NegTotal: 2}, 0.5},
	}
	for _, c := range cases {
		if got := balancedAcc(c.s); math.Abs(got-c.want) > 1e-12 {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
	if !math.IsNaN(balancedAcc(harness.SliceScore{})) {
		t.Error("empty slice score should be NaN")
	}
}

func TestSeedLabel(t *testing.T) {
	cases := map[string]string{
		"results/seed-42/report.json":  "42",
		"results/seed_7/report.json":   "7",
		"out/seed-2026.json":           "2026",
		"deep/seed-1/sub/seed-99.json": "99", // last match wins
		"plain/report.json":            "report",
	}
	for path, want := range cases {
		if got := seedLabel(path); got != want {
			t.Errorf("seedLabel(%q) = %q, want %q", path, got, want)
		}
	}
}

func report(cond string, compPos, compPosT, compNeg, compNegT, repPos, repPosT, repNeg, repNegT int) *harness.Report {
	return &harness.Report{
		Condition:   cond,
		Composition: harness.SliceScore{PosCorrect: compPos, PosTotal: compPosT, NegCorrect: compNeg, NegTotal: compNegT},
		Repetition:  harness.SliceScore{PosCorrect: repPos, PosTotal: repPosT, NegCorrect: repNeg, NegTotal: repNegT},
	}
}

// seedsWithDiff builds n seeds where A's composition balanced acc exceeds
// B's by exactly diff (both sides of each slice identical => balanced acc
// == plain rate), and repetition is identical (diff 0).
func seedsWithDiff(n int, accB, diff float64) []SeedReports {
	var out []SeedReports
	for i := 0; i < n; i++ {
		accA := accB + diff
		mk := func(cond string, acc float64) *harness.Report {
			c := int(math.Round(acc * 100))
			return report(cond, c, 100, c, 100, 90, 100, 90, 100)
		}
		out = append(out, SeedReports{
			Seed:    string(rune('a' + i)),
			Reports: map[string]*harness.Report{"A": mk("A", accA), "B": mk("B", accB)},
		})
	}
	return out
}

func TestBootstrapDeterminism(t *testing.T) {
	diffs := []float64{0.1, 0.2, 0.15, 0.3, 0.25}
	lo1, hi1 := bootstrapCI(diffs)
	lo2, hi2 := bootstrapCI(diffs)
	if lo1 != lo2 || hi1 != hi2 {
		t.Fatalf("bootstrap not deterministic: [%v,%v] vs [%v,%v]", lo1, hi1, lo2, hi2)
	}
	if !(lo1 <= hi1) || lo1 < 0.1 || hi1 > 0.3 {
		t.Fatalf("CI [%v,%v] outside data range", lo1, hi1)
	}
}

func TestVerdictBoundaries(t *testing.T) {
	cases := []struct {
		name    string
		seeds   []SeedReports
		verdict string
	}{
		// identical diffs => every bootstrap mean identical => CI lower == diff
		{"exactly at +15pp passes", seedsWithDiff(6, 0.60, 0.15), VerdictPass},
		{"just below +15pp fails", seedsWithDiff(6, 0.60, 0.14), VerdictFail},
		{"far above passes", seedsWithDiff(6, 0.50, 0.30), VerdictPass},
		{"too few seeds", seedsWithDiff(4, 0.50, 0.30), VerdictNotEvaluable},
	}
	for _, c := range cases {
		res, err := Analyze(c.seeds, "A", "B")
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if res.Verdict != c.verdict {
			t.Errorf("%s: verdict %s (why: %s), want %s", c.name, res.Verdict, res.VerdictWhy, c.verdict)
		}
	}
}

func TestRepetitionNonInferiority(t *testing.T) {
	// A wins composition big but loses repetition by 5pp on every seed:
	// repetition CI lower = -0.05 < -0.02 => FAIL.
	var seeds []SeedReports
	for i := 0; i < 6; i++ {
		a := report("A", 90, 100, 90, 100, 85, 100, 85, 100)
		bb := report("B", 50, 100, 50, 100, 90, 100, 90, 100)
		seeds = append(seeds, SeedReports{Seed: string(rune('a' + i)), Reports: map[string]*harness.Report{"A": a, "B": bb}})
	}
	res, err := Analyze(seeds, "A", "B")
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict != VerdictFail {
		t.Fatalf("verdict %s, want FAIL on repetition non-inferiority (why: %s)", res.Verdict, res.VerdictWhy)
	}
}

func TestMissingConditionErrors(t *testing.T) {
	seeds := seedsWithDiff(5, 0.5, 0.2)
	delete(seeds[2].Reports, "B")
	if _, err := Analyze(seeds, "A", "B"); err == nil {
		t.Fatal("expected error for missing condition in one seed")
	}
}

func TestNaNSeedsSkippedInEndpoint(t *testing.T) {
	// flip rate undefined (zero flip totals) on one seed: that seed drops
	// from the flip endpoint but stays in the primary.
	seeds := seedsWithDiff(6, 0.5, 0.2)
	for i := range seeds {
		if i == 0 {
			continue // seed 0: no revision queries at all => NaN flip rate
		}
		seeds[i].Reports["A"].Revision = harness.RevisionScore{FlipCorrect: 5, FlipTotal: 6, RetainCorrect: 6, RetainTotal: 6}
		seeds[i].Reports["B"].Revision = harness.RevisionScore{FlipCorrect: 2, FlipTotal: 6, RetainCorrect: 6, RetainTotal: 6}
	}
	res, err := Analyze(seeds, "A", "B")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Primary.Diffs) != 6 {
		t.Errorf("primary should keep all 6 seeds, got %d", len(res.Primary.Diffs))
	}
	if len(res.FlipRate.Diffs) != 5 || res.FlipRate.Skipped != 1 {
		t.Errorf("flip endpoint should skip 1 seed: n=%d skipped=%d", len(res.FlipRate.Diffs), res.FlipRate.Skipped)
	}
	if math.Abs(float64(res.FlipRate.MeanDiff)-0.5) > 1e-12 {
		t.Errorf("flip mean diff = %v, want 0.5", res.FlipRate.MeanDiff)
	}
}

func TestStaleShareAndFindMetrics(t *testing.T) {
	r := &harness.Report{
		Condition: "X",
		Revision:  harness.RevisionScore{FlipCorrect: 2, FlipTotal: 6, StaleAgreements: 4, RetainCorrect: 5, RetainTotal: 6},
		Find:      harness.FindScore{ExactMatches: 7, Total: 10, F1: 0.83},
	}
	m := metricsFor("s", r)
	if math.Abs(float64(m.StaleShare)-1.0) > 1e-12 { // 4 stale agreements / 4 flip errors
		t.Errorf("stale share %v, want 1.0", m.StaleShare)
	}
	if math.Abs(float64(m.FindExactRate)-0.7) > 1e-12 || m.FindMicroF1 != 0.83 {
		t.Errorf("find metrics wrong: %v %v", m.FindExactRate, m.FindMicroF1)
	}
}

func TestLoadRealOrFixtureReport(t *testing.T) {
	real := "/tmp/claude-1000/-home-itsatony-code-loom/fe3cd112-61fb-4703-88ef-c60421d2df2b/scratchpad/e2-smoke-qwen-v2.json"
	path := real
	if _, err := os.Stat(real); err != nil {
		// fall back to a miniature fixture written on the fly
		dir := t.TempDir()
		path = filepath.Join(dir, "seed-1.json")
		fixture := []*harness.Report{
			report("oracle", 40, 40, 33, 33, 30, 30, 28, 28),
			report("always-false", 0, 40, 33, 33, 0, 30, 28, 28),
		}
		raw, _ := json.Marshal(fixture)
		if err := os.WriteFile(path, raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	seeds, err := loadSeedReports([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(seeds) != 1 || len(seeds[0].Reports) < 2 {
		t.Fatalf("unexpected load result: %d seeds, %d conditions", len(seeds), len(seeds[0].Reports))
	}
}

func TestDuplicateSeedLabelErrors(t *testing.T) {
	dir := t.TempDir()
	raw, _ := json.Marshal([]*harness.Report{report("A", 1, 1, 1, 1, 1, 1, 1, 1)})
	p1 := filepath.Join(dir, "seed-5.json")
	sub := filepath.Join(dir, "seed-5")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	p2 := filepath.Join(sub, "report.json")
	for _, p := range []string{p1, p2} {
		if err := os.WriteFile(p, raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := loadSeedReports([]string{p1, p2}); err == nil {
		t.Fatal("expected duplicate-seed error")
	}
}

func TestAnalyzeJSONRoundTrip(t *testing.T) {
	res, err := Analyze(seedsWithDiff(5, 0.5, 0.2), "A", "B")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	var back Result
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.Verdict != res.Verdict || !reflect.DeepEqual(back.Primary.Diffs, res.Primary.Diffs) {
		t.Fatal("round trip mismatch")
	}
}
