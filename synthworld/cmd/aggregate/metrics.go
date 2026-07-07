// Metric and bootstrap arithmetic for the §1.1 registration. See the
// registration notice in main.go before editing anything here.
package main

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strings"

	"github.com/vaudience/synthworld/harness"
)

// Registered bootstrap parameters (MASTERPLAN §1.1). Not flags on purpose.
const (
	bootstrapResamples = 10000
	bootstrapSeed      = 42
	killCompMargin     = 0.15  // composition diff CI lower bound must be >= this
	killRepMargin      = -0.02 // repetition diff CI lower bound must be >= this (non-inferiority)
	minSeedsForVerdict = 5
	warnSeedsBelow     = 10
)

// NFloat is a float64 that marshals NaN as JSON null (encoding/json
// rejects NaN outright; "undefined for this seed" must survive -json).
type NFloat float64

func (f NFloat) MarshalJSON() ([]byte, error) {
	if math.IsNaN(float64(f)) {
		return []byte("null"), nil
	}
	return json.Marshal(float64(f))
}

func (f *NFloat) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*f = NFloat(math.NaN())
		return nil
	}
	var v float64
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*f = NFloat(v)
	return nil
}

// SeedMetrics are one condition's per-seed scalar metrics.
type SeedMetrics struct {
	Seed            string              `json:"seed"`
	RepBalancedAcc  float64             `json:"rep_balanced_acc"`
	CompBalancedAcc float64             `json:"comp_balanced_acc"`
	FlipRate        NFloat              `json:"flip_rate"`       // null if no flips in seed
	RetainRate      NFloat              `json:"retain_rate"`     // null if no retained controls
	StaleShare      NFloat              `json:"stale_share"`     // stale agreements / flip errors; null if no flip errors
	FindExactRate   NFloat              `json:"find_exact_rate"` // null if no find queries
	FindMicroF1     float64             `json:"find_micro_f1"`
	PerDepthAcc     map[int]float64     `json:"per_depth_acc,omitempty"`
	Usage           *harness.UsageStats `json:"-"`
}

func metricsFor(seed string, r *harness.Report) SeedMetrics {
	// Data-integrity guard: a condition that had ANY API errors on this
	// seed is not a clean measurement — errored queries are excluded from
	// tallies, so the surviving score is computed over a partial, biased
	// subset. Treat the whole seed's metrics as undefined (NaN) so the
	// endpoints drop it, rather than scoring a spurious chance-level or
	// partial result. (Caught live: a flaky vLLM box silently produced a
	// FAIL verdict from two error-poisoned seeds, 2026-07-07.)
	if r.Usage != nil && r.Usage.Errors > 0 {
		nan := NFloat(math.NaN())
		return SeedMetrics{
			Seed: seed, RepBalancedAcc: math.NaN(), CompBalancedAcc: math.NaN(),
			FlipRate: nan, RetainRate: nan, StaleShare: nan, FindExactRate: nan,
			FindMicroF1: math.NaN(), Usage: r.Usage,
		}
	}
	m := SeedMetrics{
		Seed:            seed,
		RepBalancedAcc:  balancedAcc(r.Repetition),
		CompBalancedAcc: balancedAcc(r.Composition),
		FlipRate:        NFloat(rate(r.Revision.FlipCorrect, r.Revision.FlipTotal)),
		RetainRate:      NFloat(rate(r.Revision.RetainCorrect, r.Revision.RetainTotal)),
		FindExactRate:   NFloat(rate(r.Find.ExactMatches, r.Find.Total)),
		FindMicroF1:     r.Find.F1,
		Usage:           r.Usage,
	}
	flipErrors := r.Revision.FlipTotal - r.Revision.FlipCorrect
	m.StaleShare = NFloat(rate(r.Revision.StaleAgreements, flipErrors))
	if len(r.PerDepth) > 0 {
		m.PerDepthAcc = map[int]float64{}
		for d, s := range r.PerDepth {
			m.PerDepthAcc[d] = s.Accuracy()
		}
	}
	return m
}

// balancedAcc is the registered composition/repetition metric: the mean of
// positive-rate and negative-rate. If one side has no queries, the other
// side's rate stands alone (real datasets always carry both; this guard
// keeps tiny fixtures well-defined). Both sides empty => NaN.
func balancedAcc(s harness.SliceScore) float64 {
	var rates []float64
	if s.PosTotal > 0 {
		rates = append(rates, float64(s.PosCorrect)/float64(s.PosTotal))
	}
	if s.NegTotal > 0 {
		rates = append(rates, float64(s.NegCorrect)/float64(s.NegTotal))
	}
	if len(rates) == 0 {
		return math.NaN()
	}
	sum := 0.0
	for _, r := range rates {
		sum += r
	}
	return sum / float64(len(rates))
}

func rate(num, den int) float64 {
	if den == 0 {
		return math.NaN()
	}
	return float64(num) / float64(den)
}

// Endpoint is one paired A−B comparison across seeds.
type Endpoint struct {
	Name     string    `json:"name"`
	Seeds    []string  `json:"seeds"` // seeds actually used (both sides non-NaN)
	ValuesA  []float64 `json:"values_a"`
	ValuesB  []float64 `json:"values_b"`
	Diffs    []float64 `json:"diffs"`
	MeanDiff NFloat    `json:"mean_diff"`
	CILower  NFloat    `json:"ci_lower"`
	CIUpper  NFloat    `json:"ci_upper"`
	Skipped  int       `json:"skipped_seeds"` // seeds dropped because a side was NaN
}

// pairedEndpoint assembles the endpoint, dropping seeds where either side
// is NaN (e.g. a seed with zero flip queries for the flip-rate endpoint).
// The bootstrap runs over the surviving seeds.
func pairedEndpoint(name string, seeds []string, a, b []float64) Endpoint {
	e := Endpoint{Name: name}
	for i := range seeds {
		if math.IsNaN(a[i]) || math.IsNaN(b[i]) {
			e.Skipped++
			continue
		}
		e.Seeds = append(e.Seeds, seeds[i])
		e.ValuesA = append(e.ValuesA, a[i])
		e.ValuesB = append(e.ValuesB, b[i])
		e.Diffs = append(e.Diffs, a[i]-b[i])
	}
	if len(e.Diffs) == 0 {
		nan := NFloat(math.NaN())
		e.MeanDiff, e.CILower, e.CIUpper = nan, nan, nan
		return e
	}
	e.MeanDiff = NFloat(mean(e.Diffs))
	lo, hi := bootstrapCI(e.Diffs)
	e.CILower, e.CIUpper = NFloat(lo), NFloat(hi)
	return e
}

func mean(xs []float64) float64 {
	s := 0.0
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

// bootstrapCI: registered procedure — resample the per-seed diffs with
// replacement bootstrapResamples times (RNG seed bootstrapSeed), take the
// mean of each resample, and return the nearest-rank 2.5th/97.5th
// percentiles (sorted ascending, indices 249 and 9749 for B=10000).
// Deterministic by construction: the RNG seed is part of the registration.
func bootstrapCI(diffs []float64) (lo, hi float64) {
	rng := rand.New(rand.NewSource(bootstrapSeed))
	n := len(diffs)
	means := make([]float64, bootstrapResamples)
	for b := 0; b < bootstrapResamples; b++ {
		s := 0.0
		for i := 0; i < n; i++ {
			s += diffs[rng.Intn(n)]
		}
		means[b] = s / float64(n)
	}
	sort.Float64s(means)
	return means[int(0.025*bootstrapResamples)-1], means[int(0.975*bootstrapResamples)-1]
}

// Verdict values.
const (
	VerdictPass         = "PASS"
	VerdictFail         = "FAIL"
	VerdictNotEvaluable = "NOT-EVALUABLE"
)

// Result is the full analysis output.
type Result struct {
	CondA      string        `json:"condition_a"`
	CondB      string        `json:"condition_b"`
	SeedCount  int           `json:"seed_count"`
	MetricsA   []SeedMetrics `json:"metrics_a"`
	MetricsB   []SeedMetrics `json:"metrics_b"`
	Primary    Endpoint      `json:"primary_composition"`
	Repetition Endpoint      `json:"secondary_repetition"`
	FlipRate   Endpoint      `json:"secondary_flip_rate"`
	FindF1     Endpoint      `json:"secondary_find_f1"`
	Verdict    string        `json:"verdict"`
	VerdictWhy string        `json:"verdict_why"`
	Warnings   []string      `json:"warnings,omitempty"`
}

// Analyze computes all endpoints and the registered verdict for A vs B.
func Analyze(seeds []SeedReports, condA, condB string) (*Result, error) {
	res := &Result{CondA: condA, CondB: condB, SeedCount: len(seeds)}
	var labels []string
	for _, sr := range seeds {
		ra, okA := sr.Reports[condA]
		rb, okB := sr.Reports[condB]
		if !okA || !okB {
			missing := condA
			if okA {
				missing = condB
			}
			return nil, fmt.Errorf("seed %s: condition %q missing from its report (have: %s)",
				sr.Seed, missing, strings.Join(conditionNames(sr.Reports), ", "))
		}
		labels = append(labels, sr.Seed)
		res.MetricsA = append(res.MetricsA, metricsFor(sr.Seed, ra))
		res.MetricsB = append(res.MetricsB, metricsFor(sr.Seed, rb))
	}

	col := func(ms []SeedMetrics, f func(SeedMetrics) float64) []float64 {
		out := make([]float64, len(ms))
		for i, m := range ms {
			out[i] = f(m)
		}
		return out
	}
	res.Primary = pairedEndpoint("composition balanced accuracy (A-B)", labels,
		col(res.MetricsA, func(m SeedMetrics) float64 { return m.CompBalancedAcc }),
		col(res.MetricsB, func(m SeedMetrics) float64 { return m.CompBalancedAcc }))
	res.Repetition = pairedEndpoint("repetition balanced accuracy (A-B)", labels,
		col(res.MetricsA, func(m SeedMetrics) float64 { return m.RepBalancedAcc }),
		col(res.MetricsB, func(m SeedMetrics) float64 { return m.RepBalancedAcc }))
	res.FlipRate = pairedEndpoint("revision flip rate (A-B)", labels,
		col(res.MetricsA, func(m SeedMetrics) float64 { return float64(m.FlipRate) }),
		col(res.MetricsB, func(m SeedMetrics) float64 { return float64(m.FlipRate) }))
	res.FindF1 = pairedEndpoint("find micro-F1 (A-B)", labels,
		col(res.MetricsA, func(m SeedMetrics) float64 { return m.FindMicroF1 }),
		col(res.MetricsB, func(m SeedMetrics) float64 { return m.FindMicroF1 }))

	// Surface error-poisoned seeds prominently: metricsFor NaN'd them, so
	// they silently vanish from endpoints otherwise.
	var poisoned []string
	for i, sr := range seeds {
		ea := res.MetricsA[i].Usage != nil && res.MetricsA[i].Usage.Errors > 0
		eb := res.MetricsB[i].Usage != nil && res.MetricsB[i].Usage.Errors > 0
		if ea || eb {
			which := condA
			if eb && !ea {
				which = condB
			} else if ea && eb {
				which = condA + "+" + condB
			}
			poisoned = append(poisoned, sr.Seed+"("+which+")")
		}
	}
	if len(poisoned) > 0 {
		res.Warnings = append(res.Warnings,
			fmt.Sprintf("DATA QUALITY: %d seed(s) EXCLUDED for API errors (not clean measurements): %s — re-run these before trusting the verdict",
				len(poisoned), strings.Join(poisoned, ", ")))
	}

	res.Verdict, res.VerdictWhy = verdict(res)
	if n := len(res.Primary.Seeds); n >= minSeedsForVerdict && n < warnSeedsBelow {
		res.Warnings = append(res.Warnings,
			fmt.Sprintf("only %d seeds — the registration targets 20; treat the verdict as provisional", n))
	}
	return res, nil
}

func verdict(res *Result) (string, string) {
	n := len(res.Primary.Seeds)
	if n < minSeedsForVerdict {
		return VerdictNotEvaluable, fmt.Sprintf("%d evaluable seeds < required minimum %d", n, minSeedsForVerdict)
	}
	compOK := res.Primary.CILower >= killCompMargin
	repOK := res.Repetition.CILower >= killRepMargin
	switch {
	case compOK && repOK:
		return VerdictPass, fmt.Sprintf(
			"composition CI lower bound %.4f >= +%.2f AND repetition CI lower bound %.4f >= %.2f",
			res.Primary.CILower, killCompMargin, res.Repetition.CILower, killRepMargin)
	case !compOK:
		return VerdictFail, fmt.Sprintf(
			"composition CI lower bound %.4f < +%.2f (kill criterion not met)",
			res.Primary.CILower, killCompMargin)
	default:
		return VerdictFail, fmt.Sprintf(
			"repetition CI lower bound %.4f < %.2f (non-inferiority failed: information destroyed at compile time)",
			res.Repetition.CILower, killRepMargin)
	}
}

func conditionNames(m map[string]*harness.Report) []string {
	var names []string
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Render prints the per-seed table, endpoints, usage, and verdict block.
func (res *Result) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "aggregate: A=%s vs B=%s over %d seeds\n\n", res.CondA, res.CondB, res.SeedCount)

	fmt.Fprintf(&b, "%-10s %28s %28s\n", "", "composition balanced acc", "repetition balanced acc")
	fmt.Fprintf(&b, "%-10s %9s %9s %8s %9s %9s %8s\n", "seed", "A", "B", "diff", "A", "B", "diff")
	for i := range res.MetricsA {
		ma, mb := res.MetricsA[i], res.MetricsB[i]
		fmt.Fprintf(&b, "%-10s %9.4f %9.4f %+8.4f %9.4f %9.4f %+8.4f\n",
			ma.Seed, ma.CompBalancedAcc, mb.CompBalancedAcc, ma.CompBalancedAcc-mb.CompBalancedAcc,
			ma.RepBalancedAcc, mb.RepBalancedAcc, ma.RepBalancedAcc-mb.RepBalancedAcc)
	}

	b.WriteString("\nendpoints (paired per-seed differences, 95% bootstrap CI, 10k resamples, RNG seed 42):\n")
	for _, e := range []Endpoint{res.Primary, res.Repetition, res.FlipRate, res.FindF1} {
		if len(e.Diffs) == 0 {
			fmt.Fprintf(&b, "  %-40s not evaluable (no seeds with both sides defined)\n", e.Name)
			continue
		}
		skipped := ""
		if e.Skipped > 0 {
			skipped = fmt.Sprintf("  [%d seeds skipped: side undefined]", e.Skipped)
		}
		fmt.Fprintf(&b, "  %-40s mean %+8.4f  CI [%+8.4f, %+8.4f]  n=%d%s\n",
			e.Name, e.MeanDiff, e.CILower, e.CIUpper, len(e.Diffs), skipped)
	}

	if usage := renderUsage(res); usage != "" {
		b.WriteString("\npooled token usage:\n")
		b.WriteString(usage)
	}

	b.WriteString("\n==== REGISTERED VERDICT (MASTERPLAN §1.1 / §7 kill criterion) ====\n")
	b.WriteString("A beats B on composition by >=15pp (CI lower bound) at non-inferior repetition (margin 2pp)?\n")
	fmt.Fprintf(&b, "  composition: mean %+8.4f, CI lower %+8.4f (threshold >= +%.2f)\n",
		res.Primary.MeanDiff, res.Primary.CILower, killCompMargin)
	fmt.Fprintf(&b, "  repetition:  mean %+8.4f, CI lower %+8.4f (threshold >= %.2f)\n",
		res.Repetition.MeanDiff, res.Repetition.CILower, killRepMargin)
	fmt.Fprintf(&b, "  VERDICT: %s — %s\n", res.Verdict, res.VerdictWhy)
	for _, w := range res.Warnings {
		fmt.Fprintf(&b, "  WARNING: %s\n", w)
	}
	return b.String()
}

func renderUsage(res *Result) string {
	var b strings.Builder
	pool := func(name string, ms []SeedMetrics) {
		var calls, hits, spentP, spentC int64
		any := false
		for _, m := range ms {
			if m.Usage == nil {
				continue
			}
			any = true
			calls += m.Usage.Calls
			hits += m.Usage.CacheHits
			spentP += m.Usage.SpentPrompt
			spentC += m.Usage.SpentCompletion
		}
		if any {
			fmt.Fprintf(&b, "  %-24s calls %7d  cache-hits %7d  spent prompt %10d  completion %9d\n",
				name, calls, hits, spentP, spentC)
		}
	}
	pool(res.CondA, res.MetricsA)
	pool(res.CondB, res.MetricsB)
	return b.String()
}
