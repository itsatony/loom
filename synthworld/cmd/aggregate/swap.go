// Swap / portability arithmetic (H6 for frames, MASTERPLAN §176-190 + §40).
//
// REGISTRATION NOTICE (same discipline as main.go): the retention statistic
// and its CI procedure are registered in MASTERPLAN §10 (dated amendment
// 2026-07-19). Retention = perf_B / perf_A (ratio of per-slice seed MEANS,
// A = reference leg, B = swapped leg), verdict on the POINT retention against
// the registered H6 bands (>= 0.95 PASS, < 0.90 KILL, between = SMALL-LOSS).
// CI = ratio-of-means bootstrap: 10,000 PAIRED resamples over the shared
// seeds (RNG seed 42), nearest-rank 2.5th/97.5th percentiles — identical
// resample count / seed / percentile rule as §1.1's bootstrapCI, applied to
// the ratio of the two resampled means. Reported for honesty; the verdict
// keys off the point estimate exactly as the registered H6 wording reads.
//
// The swap for a *compiled* substrate collapses to the EXTRACTION surface:
// C2b answering is the deterministic op-planner (harness/loom_c2b.go), so an
// LLM swap only changes the store an extractor produced. Answering-swap
// retention is 1.000 by construction and is NOT measured here (structural
// ceiling, MASTERPLAN §187). This kernel measures extraction portability:
// the same condition (loom-c2b-frames) extracted by different models, scored
// on identical locked seeds.
package main

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strings"
)

// Registered H6 retention bands (MASTERPLAN §40).
const (
	swapPassBound = 0.95 // point retention >= this => PASS
	swapKillBound = 0.90 // point retention <  this => KILL
)

// swapSlice names a metric column and its accessor into FrameSeedMetrics.
// The set matches the pre-registered PLAN §2 portability table exactly.
type swapSlice struct {
	name string
	get  func(FrameSeedMetrics) float64
}

func swapSlices() []swapSlice {
	return []swapSlice{
		{"repetition", func(m FrameSeedMetrics) float64 { return m.RepBA }},
		{"composition", func(m FrameSeedMetrics) float64 { return m.CompBA }},
		{"revision flip", func(m FrameSeedMetrics) float64 { return float64(m.FlipRate) }},
		{"revision retain", func(m FrameSeedMetrics) float64 { return float64(m.RetainRate) }},
		{"find micro-F1", func(m FrameSeedMetrics) float64 { return m.FindMicroF1 }},
		{"contamination", func(m FrameSeedMetrics) float64 { return m.ContaminationBA }},
		{"isolation", func(m FrameSeedMetrics) float64 { return m.IsolationBA }},
		{"pinning", func(m FrameSeedMetrics) float64 { return float64(m.PinningBA) }},
		{"promotion", func(m FrameSeedMetrics) float64 { return float64(m.PromotionBA) }},
		{"misattribution F1", func(m FrameSeedMetrics) float64 { return m.MisattributionF1 }},
		{"ideation F1", func(m FrameSeedMetrics) float64 { return m.IdeationF1 }},
	}
}

// v0 lift slices: the LLM-bound floor (C0) only answers these meaningfully.
var swapLiftSlices = []string{"repetition", "composition", "revision flip", "revision retain", "find micro-F1"}

// LegSpec is one extractor leg: a display label + a glob of its per-seed
// harness report files.
type LegSpec struct {
	Label string
	Glob  string
}

// parseLegs parses "label=glob,label=glob,..." into ordered LegSpecs.
func parseLegs(spec string) ([]LegSpec, error) {
	var out []LegSpec
	seen := map[string]bool{}
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		eq := strings.Index(part, "=")
		if eq <= 0 || eq == len(part)-1 {
			return nil, fmt.Errorf("bad leg spec %q: want label=glob", part)
		}
		label := strings.TrimSpace(part[:eq])
		glob := strings.TrimSpace(part[eq+1:])
		if seen[label] {
			return nil, fmt.Errorf("duplicate leg label %q", label)
		}
		seen[label] = true
		out = append(out, LegSpec{Label: label, Glob: glob})
	}
	if len(out) < 2 {
		return nil, fmt.Errorf("swap needs >= 2 legs, got %d", len(out))
	}
	return out, nil
}

// SwapSliceRetention is one slice's retention of leg B relative to the ref.
type SwapSliceRetention struct {
	Slice     string `json:"slice"`
	RefMean   NFloat `json:"ref_mean"`
	LegMean   NFloat `json:"leg_mean"`
	Retention NFloat `json:"retention"` // leg_mean / ref_mean
	CILower   NFloat `json:"ci_lower"`
	CIUpper   NFloat `json:"ci_upper"`
	N         int    `json:"n"` // shared seeds with both sides defined
	Verdict   string `json:"verdict"`
}

// SwapLiftSlice is one slice's substrate lift (cond − C0) for a leg.
type SwapLiftSlice struct {
	Slice   string `json:"slice"`
	Cond    NFloat `json:"cond_mean"`
	C0      NFloat `json:"c0_mean"`
	Lift    NFloat `json:"lift"`
	CILower NFloat `json:"ci_lower"`
	CIUpper NFloat `json:"ci_upper"`
	N       int    `json:"n"`
}

// SwapLegResult holds one non-reference leg's full comparison to the ref.
type SwapLegResult struct {
	Label       string               `json:"label"`
	SharedSeeds []string             `json:"shared_seeds"`
	Slices      []SwapSliceRetention `json:"slices"`
	MinRetSlice string               `json:"min_retention_slice"`
	MinRet      NFloat               `json:"min_retention"`
	Verdict     string               `json:"verdict"` // worst-slice roll-up
	Lift        []SwapLiftSlice      `json:"lift,omitempty"`
	LiftNote    string               `json:"lift_note,omitempty"`
}

// SwapResult is the full portability analysis.
type SwapResult struct {
	Condition string          `json:"condition"`
	RefLeg    string          `json:"ref_leg"`
	Legs      []SwapLegResult `json:"legs"`
	Warnings  []string        `json:"warnings,omitempty"`
}

// bootstrapRatioCI: registered ratio-of-means bootstrap (MASTERPLAN §10
// 2026-07-19). Paired resample of the shared seeds — draw n indices with
// replacement, sum a[idx] and b[idx], ratio = sumB/sumA — 10k times, RNG
// seed 42, nearest-rank 2.5/97.5 percentiles. Deterministic. Returns NaN
// bounds if any resample hits a zero reference sum (undefined ratio).
func bootstrapRatioCI(a, b []float64) (lo, hi float64) {
	rng := rand.New(rand.NewSource(bootstrapSeed))
	n := len(a)
	ratios := make([]float64, bootstrapResamples)
	for r := 0; r < bootstrapResamples; r++ {
		var sa, sb float64
		for i := 0; i < n; i++ {
			idx := rng.Intn(n)
			sa += a[idx]
			sb += b[idx]
		}
		if sa == 0 {
			ratios[r] = math.NaN()
			continue
		}
		ratios[r] = sb / sa
	}
	sort.Float64s(ratios) // NaNs sort to the front; if any present, bounds degrade to NaN
	loIdx, hiIdx := int(0.025*bootstrapResamples)-1, int(0.975*bootstrapResamples)-1
	return ratios[loIdx], ratios[hiIdx]
}

// pairedDefined collects seed values where BOTH ref and leg are non-NaN.
func pairedDefined(refVals, legVals []float64) (ref, leg []float64, n int) {
	for i := range refVals {
		if math.IsNaN(refVals[i]) || math.IsNaN(legVals[i]) {
			continue
		}
		ref = append(ref, refVals[i])
		leg = append(leg, legVals[i])
	}
	return ref, leg, len(ref)
}

func swapVerdict(ret float64) string {
	switch {
	case math.IsNaN(ret):
		return VerdictNotEvaluable
	case ret >= swapPassBound:
		return VerdictPass
	case ret < swapKillBound:
		return "KILL"
	default:
		return "SMALL-LOSS"
	}
}

// legMetrics extracts per-seed FrameSeedMetrics for cond across a leg's
// seeds, keyed by seed label. A seed missing the condition or erroring is
// simply absent (the caller pairs on shared, defined seeds).
func legMetrics(seeds []SeedReports, cond string) (map[string]FrameSeedMetrics, error) {
	out := map[string]FrameSeedMetrics{}
	for _, sr := range seeds {
		r, ok := sr.Reports[cond]
		if !ok {
			continue
		}
		m, err := frameMetricsFor(sr.Seed, r)
		if err != nil {
			return nil, err
		}
		out[sr.Seed] = m
	}
	return out, nil
}

// AnalyzeSwap computes extraction-portability retention of every non-ref leg
// against the reference leg, plus optional substrate lift (cond − c0) for
// legs carrying the c0 condition. legSeeds is label -> loaded seed reports.
func AnalyzeSwap(legOrder []string, legSeeds map[string][]SeedReports, cond, refLabel, c0Cond string) (*SwapResult, error) {
	res := &SwapResult{Condition: cond, RefLeg: refLabel}

	refMetrics, err := legMetrics(legSeeds[refLabel], cond)
	if err != nil {
		return nil, fmt.Errorf("reference leg %q: %w", refLabel, err)
	}
	if len(refMetrics) == 0 {
		return nil, fmt.Errorf("reference leg %q has no %q metrics", refLabel, cond)
	}

	slices := swapSlices()
	for _, label := range legOrder {
		if label == refLabel {
			continue
		}
		legM, err := legMetrics(legSeeds[label], cond)
		if err != nil {
			return nil, fmt.Errorf("leg %q: %w", label, err)
		}
		// Shared seeds present in both legs, sorted for determinism.
		var shared []string
		for s := range legM {
			if _, ok := refMetrics[s]; ok {
				shared = append(shared, s)
			}
		}
		sort.Strings(shared)

		leg := SwapLegResult{Label: label, SharedSeeds: shared, MinRet: NFloat(math.Inf(1))}
		for _, sl := range slices {
			refVals := make([]float64, len(shared))
			legVals := make([]float64, len(shared))
			for i, s := range shared {
				refVals[i] = sl.get(refMetrics[s])
				legVals[i] = sl.get(legM[s])
			}
			ref, lv, n := pairedDefined(refVals, legVals)
			sr := SwapSliceRetention{Slice: sl.name, N: n}
			if n == 0 {
				nan := NFloat(math.NaN())
				sr.RefMean, sr.LegMean, sr.Retention = nan, nan, nan
				sr.CILower, sr.CIUpper, sr.Verdict = nan, nan, VerdictNotEvaluable
				leg.Slices = append(leg.Slices, sr)
				continue
			}
			rm, lm := mean(ref), mean(lv)
			sr.RefMean, sr.LegMean = NFloat(rm), NFloat(lm)
			ret := math.NaN()
			if rm != 0 {
				ret = lm / rm
			}
			sr.Retention = NFloat(ret)
			lo, hi := bootstrapRatioCI(ref, lv)
			sr.CILower, sr.CIUpper = NFloat(lo), NFloat(hi)
			sr.Verdict = swapVerdict(ret)
			if !math.IsNaN(ret) && ret < float64(leg.MinRet) {
				leg.MinRet, leg.MinRetSlice = NFloat(ret), sl.name
			}
			leg.Slices = append(leg.Slices, sr)
		}
		if math.IsInf(float64(leg.MinRet), 1) {
			leg.MinRet = NFloat(math.NaN())
			leg.Verdict = VerdictNotEvaluable
		} else {
			leg.Verdict = swapVerdict(float64(leg.MinRet))
		}
		res.Legs = append(res.Legs, leg)
	}

	// ---- optional substrate lift (cond − C0) ----
	if c0Cond != "" {
		addLift(res, legOrder, legSeeds, cond, c0Cond)
	}
	return res, nil
}

// addLift computes cond − c0 on the v0 slices for every leg carrying c0
// (including the reference). Legs without c0 get a note, not a failure.
func addLift(res *SwapResult, legOrder []string, legSeeds map[string][]SeedReports, cond, c0Cond string) {
	sliceGet := map[string]func(FrameSeedMetrics) float64{}
	for _, sl := range swapSlices() {
		sliceGet[sl.name] = sl.get
	}
	byLabel := map[string]*SwapLegResult{}
	for i := range res.Legs {
		byLabel[res.Legs[i].Label] = &res.Legs[i]
	}
	for _, label := range legOrder {
		condM, err := legMetrics(legSeeds[label], cond)
		if err != nil {
			continue
		}
		c0M, err := legMetrics(legSeeds[label], c0Cond)
		if err != nil || len(c0M) == 0 {
			if lr := byLabel[label]; lr != nil {
				lr.LiftNote = fmt.Sprintf("no %s in this leg — lift not computable (answering-swap contrast is Leg B)", c0Cond)
			}
			continue
		}
		var shared []string
		for s := range c0M {
			if _, ok := condM[s]; ok {
				shared = append(shared, s)
			}
		}
		sort.Strings(shared)
		var lifts []SwapLiftSlice
		for _, name := range swapLiftSlices {
			get := sliceGet[name]
			condVals := make([]float64, len(shared))
			c0Vals := make([]float64, len(shared))
			for i, s := range shared {
				condVals[i] = get(condM[s])
				c0Vals[i] = get(c0M[s])
			}
			cv, zv, n := pairedDefined(condVals, c0Vals)
			ls := SwapLiftSlice{Slice: name, N: n}
			if n == 0 {
				nan := NFloat(math.NaN())
				ls.Cond, ls.C0, ls.Lift, ls.CILower, ls.CIUpper = nan, nan, nan, nan, nan
				lifts = append(lifts, ls)
				continue
			}
			diffs := make([]float64, n)
			for i := range cv {
				diffs[i] = cv[i] - zv[i]
			}
			ls.Cond, ls.C0 = NFloat(mean(cv)), NFloat(mean(zv))
			ls.Lift = NFloat(mean(diffs))
			lo, hi := bootstrapCI(diffs)
			ls.CILower, ls.CIUpper = NFloat(lo), NFloat(hi)
			lifts = append(lifts, ls)
		}
		lr := byLabel[label]
		if lr == nil {
			// reference leg has no SwapLegResult row; attach as a note-only entry.
			ref := SwapLegResult{Label: label + " (ref)", Lift: lifts}
			res.Legs = append([]SwapLegResult{ref}, res.Legs...)
			continue
		}
		lr.Lift = lifts
	}
}

// Render prints the retention spectrum table and the lift block.
func (res *SwapResult) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "swap / portability: condition=%s  reference leg=%s\n", res.Condition, res.RefLeg)
	fmt.Fprintf(&b, "extraction-surface retention = leg_mean / ref_mean (H6 bands: PASS>=%.2f, KILL<%.2f)\n",
		swapPassBound, swapKillBound)
	fmt.Fprint(&b, "answering-swap retention = 1.000 by construction (op-planner is LLM-free) — not shown.\n\n")

	for _, leg := range res.Legs {
		if len(leg.Slices) == 0 && len(leg.Lift) > 0 {
			// reference-only lift row
			fmt.Fprintf(&b, "== %s : substrate lift (cond - C0) ==\n", leg.Label)
			renderLift(&b, leg.Lift)
			b.WriteString("\n")
			continue
		}
		fmt.Fprintf(&b, "== leg %s vs %s (%d shared seeds) : %s (min retention %.4f on %q) ==\n",
			leg.Label, res.RefLeg, len(leg.SharedSeeds), leg.Verdict, float64(leg.MinRet), leg.MinRetSlice)
		fmt.Fprintf(&b, "  %-20s %9s %9s %10s %-22s %6s %s\n",
			"slice", "ref", "leg", "retention", "95% CI", "n", "verdict")
		for _, s := range leg.Slices {
			fmt.Fprintf(&b, "  %-20s %9.4f %9.4f %10.4f [%7.4f,%7.4f] %6d %s\n",
				s.Slice, float64(s.RefMean), float64(s.LegMean), float64(s.Retention),
				float64(s.CILower), float64(s.CIUpper), s.N, s.Verdict)
		}
		if len(leg.Lift) > 0 {
			fmt.Fprintf(&b, "  -- substrate lift (%s - C0) --\n", res.Condition)
			renderLift(&b, leg.Lift)
		} else if leg.LiftNote != "" {
			fmt.Fprintf(&b, "  -- lift: %s\n", leg.LiftNote)
		}
		b.WriteString("\n")
	}
	for _, w := range res.Warnings {
		fmt.Fprintf(&b, "WARNING: %s\n", w)
	}
	return b.String()
}

func renderLift(b *strings.Builder, lifts []SwapLiftSlice) {
	fmt.Fprintf(b, "  %-20s %9s %9s %9s %-22s %s\n", "slice", "cond", "C0", "lift", "95% CI", "n")
	for _, l := range lifts {
		fmt.Fprintf(b, "  %-20s %9.4f %9.4f %+9.4f [%+7.4f,%+7.4f] %6d\n",
			l.Slice, float64(l.Cond), float64(l.C0), float64(l.Lift),
			float64(l.CILower), float64(l.CIUpper), l.N)
	}
}
