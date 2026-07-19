package main

import (
	"math"
	"testing"

	"github.com/vaudience/synthworld/harness"
)

func TestParseLegs(t *testing.T) {
	legs, err := parseLegs("qwen=a/*.json, gpt5=b/*.json ,mini=c/*.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(legs) != 3 || legs[0].Label != "qwen" || legs[1].Glob != "b/*.json" || legs[2].Label != "mini" {
		t.Fatalf("bad parse: %+v", legs)
	}
	for _, bad := range []string{"", "only=one", "=noglob,x=y", "dup=a,dup=b"} {
		if _, err := parseLegs(bad); err == nil {
			t.Errorf("parseLegs(%q) should have errored", bad)
		}
	}
}

func TestBootstrapRatioCIDeterministicAndCentered(t *testing.T) {
	// b = 0.9 * a uniformly => every resample ratio == 0.9 exactly.
	a := []float64{0.8, 1.0, 0.6, 0.95, 0.7}
	b := make([]float64, len(a))
	for i := range a {
		b[i] = 0.9 * a[i]
	}
	lo, hi := bootstrapRatioCI(a, b)
	if math.Abs(lo-0.9) > 1e-9 || math.Abs(hi-0.9) > 1e-9 {
		t.Fatalf("degenerate ratio: lo=%v hi=%v, want ~0.9", lo, hi)
	}
	// Determinism: same inputs => identical bounds (registered RNG seed).
	lo2, hi2 := bootstrapRatioCI(a, b)
	if lo != lo2 || hi != hi2 {
		t.Fatalf("non-deterministic bootstrap: (%v,%v) vs (%v,%v)", lo, hi, lo2, hi2)
	}
	// Identical legs => retention exactly 1.0 with a zero-width CI.
	lo1, hi1 := bootstrapRatioCI(a, a)
	if math.Abs(lo1-1.0) > 1e-9 || math.Abs(hi1-1.0) > 1e-9 {
		t.Fatalf("self-ratio should be 1.0, got [%v,%v]", lo1, hi1)
	}
}

func TestSwapVerdictBands(t *testing.T) {
	cases := map[float64]string{
		0.99: VerdictPass, 0.95: VerdictPass,
		0.94: "SMALL-LOSS", 0.90: "SMALL-LOSS",
		0.899: "KILL", 0.5: "KILL",
	}
	for ret, want := range cases {
		if got := swapVerdict(ret); got != want {
			t.Errorf("swapVerdict(%.3f) = %q, want %q", ret, want, got)
		}
	}
	if swapVerdict(math.NaN()) != VerdictNotEvaluable {
		t.Error("NaN retention should be NOT-EVALUABLE")
	}
}

// frameReport builds a minimal frames report pinning composition,
// contamination and misattribution-F1 to given balanced-acc / F1 values.
func frameReport(cond string, comp, contam, misattrF1 float64) *harness.Report {
	ba := func(r float64) harness.SliceScore {
		c := int(math.Round(r * 100))
		return harness.SliceScore{PosCorrect: c, PosTotal: 100, NegCorrect: c, NegTotal: 100}
	}
	return &harness.Report{
		Condition:   cond,
		Composition: ba(comp),
		Frames: &harness.FrameReport{
			Contamination:  ba(contam),
			Isolation:      ba(1.0),
			Misattribution: harness.FindScore{F1: misattrF1},
			Ideation:       harness.FindScore{F1: 1.0},
		},
	}
}

func TestAnalyzeSwapRetentionAndRollup(t *testing.T) {
	// Reference leg: everything near-ceiling. Swapped leg: composition holds
	// (ret 0.90 => SMALL-LOSS) but contamination drops to 0.80 (ret 0.80 =>
	// KILL). Worst-slice roll-up must be KILL.
	mk := func(seeds []SeedReports, cond string, comp, contam, mis float64) {
		for i := range seeds {
			seeds[i].Reports[cond] = frameReport(cond, comp, contam, mis)
		}
	}
	newSeeds := func(n int) []SeedReports {
		out := make([]SeedReports, n)
		for i := range out {
			out[i] = SeedReports{Seed: string(rune('a' + i)), Reports: map[string]*harness.Report{}}
		}
		return out
	}
	const cond = "loom-c2b-frames"
	ref := newSeeds(6)
	mk(ref, cond, 1.0, 1.0, 1.0)
	leg := newSeeds(6)
	mk(leg, cond, 0.90, 0.80, 1.0)

	res, err := AnalyzeSwap([]string{"ref", "weak"}, map[string][]SeedReports{"ref": ref, "weak": leg}, cond, "ref", "")
	if err != nil {
		t.Fatalf("AnalyzeSwap: %v", err)
	}
	if len(res.Legs) != 1 || res.Legs[0].Label != "weak" {
		t.Fatalf("expected one non-ref leg 'weak', got %+v", res.Legs)
	}
	got := map[string]float64{}
	for _, s := range res.Legs[0].Slices {
		got[s.Slice] = float64(s.Retention)
	}
	if math.Abs(got["composition"]-0.90) > 1e-9 {
		t.Errorf("composition retention = %v, want 0.90", got["composition"])
	}
	if math.Abs(got["contamination"]-0.80) > 1e-9 {
		t.Errorf("contamination retention = %v, want 0.80", got["contamination"])
	}
	if res.Legs[0].Verdict != "KILL" || res.Legs[0].MinRetSlice != "contamination" {
		t.Errorf("roll-up = %q on %q, want KILL on contamination", res.Legs[0].Verdict, res.Legs[0].MinRetSlice)
	}
}
