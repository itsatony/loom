// Frames-v1 endpoint arithmetic (MASTERPLAN §9.6.7). Same registration
// notice as main.go: after frames E-data exists, changes here require a
// dated §10 amendment. CI arithmetic is identical to §1.1: 10,000 bootstrap
// resamples over seeds, RNG seed 42, nearest-rank 2.5th/97.5th percentiles.
//
// Endpoints:
//
//	F-E1 (co-primary, two-directional safety; condition A alone):
//	  contamination balanced-acc CI lower >= 0.85 AND isolation balanced-acc
//	  CI lower >= 0.85 (traps pooled with paired controls). KILL if either
//	  CI lower < 0.70. Between the bands: INDETERMINATE (no pass, no kill).
//	  The gap-trap sub-line is mandatory reporting, not a gate.
//	F-E2 (co-primary, superiority over the null; paired A-B):
//	  content-cued contamination balanced-acc diff CI lower >= +0.15, at
//	  non-inferiority (CI lower >= -0.02) on metadata-cued traps AND every
//	  v0 slice (repetition, composition, revision flip+retain, find F1).
//	  KILL if the content-cued CI lower < +0.15 (query-time provenance
//	  filtering suffices).
//	F-E4 (secondary): ideation micro-F1 (per-satisfier frame attribution),
//	  gate mean >= 0.90; reported with CI, no kill semantics.
//
// F-E3 (frame-assignment fidelity) is computed by cmd/fidelity, not here.
package main

import (
	"fmt"
	"math"
	"strings"

	"github.com/vaudience/synthworld/harness"
)

// Registered frames thresholds (§9.6.7, §9.6.2 #4).
const (
	fe1PassBound = 0.85
	fe1KillBound = 0.70
	fe2Margin    = 0.15
	fe2NonInf    = -0.02
	fe4Gate      = 0.90
)

// FrameSeedMetrics are one condition's per-seed frames scalars.
type FrameSeedMetrics struct {
	Seed             string  `json:"seed"`
	ContaminationBA  float64 `json:"contamination_balanced_acc"`
	GapBA            NFloat  `json:"gap_balanced_acc"`
	ContentCuedBA    NFloat  `json:"content_cued_balanced_acc"`
	MetadataCuedBA   NFloat  `json:"metadata_cued_balanced_acc"`
	FilterResistBA   NFloat  `json:"filtering_resistant_balanced_acc"`
	FilterDecideBA   NFloat  `json:"filter_decidable_balanced_acc"`
	IsolationBA      float64 `json:"isolation_balanced_acc"`
	ChainBA          NFloat  `json:"isolation_chain_balanced_acc"`
	PinningBA        NFloat  `json:"pinning_balanced_acc"`
	PromotionBA      NFloat  `json:"promotion_balanced_acc"`
	MisattributionF1 float64 `json:"misattribution_micro_f1"`
	IdeationF1       float64 `json:"ideation_micro_f1"`
	IdeationExact    NFloat  `json:"ideation_exact_rate"`
	// v0 slices for the F-E2 non-inferiority legs.
	RepBA       float64 `json:"rep_balanced_acc"`
	CompBA      float64 `json:"comp_balanced_acc"`
	FlipRate    NFloat  `json:"flip_rate"`
	RetainRate  NFloat  `json:"retain_rate"`
	FindMicroF1 float64 `json:"find_micro_f1"`

	Usage *harness.UsageStats `json:"-"`
}

func frameMetricsFor(seed string, r *harness.Report) (FrameSeedMetrics, error) {
	if r.Frames == nil {
		return FrameSeedMetrics{}, fmt.Errorf("seed %s: condition %s has no frames section (not a frames dataset?)", seed, r.Condition)
	}
	// Same data-integrity guard as metricsFor: an error-poisoned seed is a
	// non-measurement; NaN everything so endpoints drop it loudly.
	if r.Usage != nil && r.Usage.Errors > 0 {
		nan := math.NaN()
		m := FrameSeedMetrics{Seed: seed, ContaminationBA: nan, IsolationBA: nan,
			MisattributionF1: nan, IdeationF1: nan, RepBA: nan, CompBA: nan, FindMicroF1: nan,
			GapBA: NFloat(nan), ContentCuedBA: NFloat(nan), MetadataCuedBA: NFloat(nan),
			ChainBA: NFloat(nan), PinningBA: NFloat(nan), PromotionBA: NFloat(nan),
			FlipRate: NFloat(nan), RetainRate: NFloat(nan), IdeationExact: NFloat(nan),
			Usage: r.Usage}
		return m, nil
	}
	fr := r.Frames
	sub := func(m map[string]*harness.SliceScore, k string) NFloat {
		if s, ok := m[k]; ok {
			return NFloat(balancedAcc(*s))
		}
		return NFloat(math.NaN())
	}
	return FrameSeedMetrics{
		Seed:             seed,
		ContaminationBA:  balancedAcc(fr.Contamination),
		GapBA:            sub(fr.ContaminationSub, "gap"),
		ContentCuedBA:    sub(fr.CueSub, "content"),
		MetadataCuedBA:   sub(fr.CueSub, "metadata"),
		FilterResistBA:   sub(fr.FilterSub, "resistant"),
		FilterDecideBA:   sub(fr.FilterSub, "decidable"),
		IsolationBA:      balancedAcc(fr.Isolation),
		ChainBA:          NFloat(balancedAcc(fr.IsolationChain)),
		PinningBA:        NFloat(balancedAcc(fr.Pinning)),
		PromotionBA:      NFloat(balancedAcc(fr.Promotion)),
		MisattributionF1: fr.Misattribution.F1,
		IdeationF1:       fr.Ideation.F1,
		IdeationExact:    NFloat(rate(fr.Ideation.ExactMatches, fr.Ideation.Total)),
		RepBA:            balancedAcc(r.Repetition),
		CompBA:           balancedAcc(r.Composition),
		FlipRate:         NFloat(rate(r.Revision.FlipCorrect, r.Revision.FlipTotal)),
		RetainRate:       NFloat(rate(r.Revision.RetainCorrect, r.Revision.RetainTotal)),
		FindMicroF1:      r.Find.F1,
		Usage:            r.Usage,
	}, nil
}

// singleEndpoint bootstraps the per-seed VALUES of one condition (the F-E1
// form: a bound on the condition's own level, not a paired difference).
func singleEndpoint(name string, seeds []string, vals []float64) Endpoint {
	e := Endpoint{Name: name}
	for i := range seeds {
		if math.IsNaN(vals[i]) {
			e.Skipped++
			continue
		}
		e.Seeds = append(e.Seeds, seeds[i])
		e.ValuesA = append(e.ValuesA, vals[i])
		e.Diffs = append(e.Diffs, vals[i])
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

// FramesResult is the full frames analysis output.
type FramesResult struct {
	CondA     string             `json:"condition_a"`
	CondB     string             `json:"condition_b"`
	SeedCount int                `json:"seed_count"`
	MetricsA  []FrameSeedMetrics `json:"metrics_a"`
	MetricsB  []FrameSeedMetrics `json:"metrics_b"`

	// F-E1 (condition A alone)
	FE1Contamination Endpoint `json:"fe1_contamination"`
	FE1Isolation     Endpoint `json:"fe1_isolation"`
	FE1Gap           Endpoint `json:"fe1_gap_subline"` // mandatory reporting
	FE1Verdict       string   `json:"fe1_verdict"`
	FE1Why           string   `json:"fe1_why"`

	// F-E2 (paired A-B)
	FE2Content       Endpoint   `json:"fe2_content_cued"`        // reading (a), lexical markedness
	FE2Metadata      Endpoint   `json:"fe2_metadata_cued"`       // reading (a)
	FE2Resistant     Endpoint   `json:"fe2_filtering_resistant"` // re-spec (§10 2026-07-18)
	FE2Decidable     Endpoint   `json:"fe2_filter_decidable"`    // re-spec non-inferiority leg
	FE2ResistVerdict string     `json:"fe2_resistant_verdict"`
	FE2ResistWhy     string     `json:"fe2_resistant_why"`
	FE2V0            []Endpoint `json:"fe2_v0_noninferiority"`
	FE2Verdict       string     `json:"fe2_verdict"`
	FE2Why           string     `json:"fe2_why"`
	FE2Additional    []Endpoint `json:"fe2_context,omitempty"` // contamination/isolation A-B, context only

	// F-E4 (condition A alone)
	FE4Ideation Endpoint `json:"fe4_ideation_f1"`
	FE4Exact    Endpoint `json:"fe4_ideation_exact"`
	FE4Verdict  string   `json:"fe4_verdict"`
	FE4Why      string   `json:"fe4_why"`

	Warnings []string `json:"warnings,omitempty"`
}

// AnalyzeFrames computes F-E1/F-E2/F-E4 for A (frames condition) vs B (the
// C2b-prov null).
func AnalyzeFrames(seeds []SeedReports, condA, condB string) (*FramesResult, error) {
	res := &FramesResult{CondA: condA, CondB: condB, SeedCount: len(seeds)}
	var labels []string
	for _, sr := range seeds {
		ra, okA := sr.Reports[condA]
		rb, okB := sr.Reports[condB]
		if !okA || !okB {
			missing := condA
			if okA {
				missing = condB
			}
			return nil, fmt.Errorf("seed %s: condition %q missing (have: %s)",
				sr.Seed, missing, strings.Join(conditionNames(sr.Reports), ", "))
		}
		ma, err := frameMetricsFor(sr.Seed, ra)
		if err != nil {
			return nil, err
		}
		mb, err := frameMetricsFor(sr.Seed, rb)
		if err != nil {
			return nil, err
		}
		labels = append(labels, sr.Seed)
		res.MetricsA = append(res.MetricsA, ma)
		res.MetricsB = append(res.MetricsB, mb)
	}
	col := func(ms []FrameSeedMetrics, f func(FrameSeedMetrics) float64) []float64 {
		out := make([]float64, len(ms))
		for i, m := range ms {
			out[i] = f(m)
		}
		return out
	}

	// ---- F-E1 ----
	res.FE1Contamination = singleEndpoint("F-E1 contamination balanced acc (A)", labels,
		col(res.MetricsA, func(m FrameSeedMetrics) float64 { return m.ContaminationBA }))
	res.FE1Isolation = singleEndpoint("F-E1 isolation balanced acc (A)", labels,
		col(res.MetricsA, func(m FrameSeedMetrics) float64 { return m.IsolationBA }))
	res.FE1Gap = singleEndpoint("F-E1 gap-trap sub-line (A, reported)", labels,
		col(res.MetricsA, func(m FrameSeedMetrics) float64 { return float64(m.GapBA) }))
	cLo, iLo := float64(res.FE1Contamination.CILower), float64(res.FE1Isolation.CILower)
	switch {
	case len(res.FE1Contamination.Diffs) < minSeedsForVerdict || len(res.FE1Isolation.Diffs) < minSeedsForVerdict:
		res.FE1Verdict = VerdictNotEvaluable
		res.FE1Why = fmt.Sprintf("evaluable seeds < %d", minSeedsForVerdict)
	case cLo < fe1KillBound || iLo < fe1KillBound:
		res.FE1Verdict = "KILL"
		res.FE1Why = fmt.Sprintf("CI lower bound below kill line %.2f (contamination %.4f, isolation %.4f)", fe1KillBound, cLo, iLo)
	case cLo >= fe1PassBound && iLo >= fe1PassBound:
		res.FE1Verdict = VerdictPass
		res.FE1Why = fmt.Sprintf("both CI lower bounds >= %.2f (contamination %.4f, isolation %.4f)", fe1PassBound, cLo, iLo)
	default:
		res.FE1Verdict = "INDETERMINATE"
		res.FE1Why = fmt.Sprintf("between kill (%.2f) and pass (%.2f) bounds (contamination %.4f, isolation %.4f)", fe1KillBound, fe1PassBound, cLo, iLo)
	}

	// ---- F-E2 ----
	res.FE2Content = pairedEndpoint("F-E2 content-cued balanced acc (A-B)", labels,
		col(res.MetricsA, func(m FrameSeedMetrics) float64 { return float64(m.ContentCuedBA) }),
		col(res.MetricsB, func(m FrameSeedMetrics) float64 { return float64(m.ContentCuedBA) }))
	res.FE2Metadata = pairedEndpoint("F-E2 metadata-cued balanced acc (A-B)", labels,
		col(res.MetricsA, func(m FrameSeedMetrics) float64 { return float64(m.MetadataCuedBA) }),
		col(res.MetricsB, func(m FrameSeedMetrics) float64 { return float64(m.MetadataCuedBA) }))
	res.FE2Resistant = pairedEndpoint("F-E2' filtering-resistant balanced acc (A-B)", labels,
		col(res.MetricsA, func(m FrameSeedMetrics) float64 { return float64(m.FilterResistBA) }),
		col(res.MetricsB, func(m FrameSeedMetrics) float64 { return float64(m.FilterResistBA) }))
	res.FE2Decidable = pairedEndpoint("F-E2' filter-decidable balanced acc (A-B, non-inf)", labels,
		col(res.MetricsA, func(m FrameSeedMetrics) float64 { return float64(m.FilterDecideBA) }),
		col(res.MetricsB, func(m FrameSeedMetrics) float64 { return float64(m.FilterDecideBA) }))
	res.FE2V0 = []Endpoint{
		pairedEndpoint("v0 repetition balanced acc (A-B)", labels,
			col(res.MetricsA, func(m FrameSeedMetrics) float64 { return m.RepBA }),
			col(res.MetricsB, func(m FrameSeedMetrics) float64 { return m.RepBA })),
		pairedEndpoint("v0 composition balanced acc (A-B)", labels,
			col(res.MetricsA, func(m FrameSeedMetrics) float64 { return m.CompBA }),
			col(res.MetricsB, func(m FrameSeedMetrics) float64 { return m.CompBA })),
		pairedEndpoint("v0 revision flip rate (A-B)", labels,
			col(res.MetricsA, func(m FrameSeedMetrics) float64 { return float64(m.FlipRate) }),
			col(res.MetricsB, func(m FrameSeedMetrics) float64 { return float64(m.FlipRate) })),
		pairedEndpoint("v0 revision retain rate (A-B)", labels,
			col(res.MetricsA, func(m FrameSeedMetrics) float64 { return float64(m.RetainRate) }),
			col(res.MetricsB, func(m FrameSeedMetrics) float64 { return float64(m.RetainRate) })),
		pairedEndpoint("v0 find micro-F1 (A-B)", labels,
			col(res.MetricsA, func(m FrameSeedMetrics) float64 { return m.FindMicroF1 }),
			col(res.MetricsB, func(m FrameSeedMetrics) float64 { return m.FindMicroF1 })),
	}
	res.FE2Additional = []Endpoint{
		pairedEndpoint("contamination balanced acc (A-B, context)", labels,
			col(res.MetricsA, func(m FrameSeedMetrics) float64 { return m.ContaminationBA }),
			col(res.MetricsB, func(m FrameSeedMetrics) float64 { return m.ContaminationBA })),
		pairedEndpoint("isolation balanced acc (A-B, context)", labels,
			col(res.MetricsA, func(m FrameSeedMetrics) float64 { return m.IsolationBA }),
			col(res.MetricsB, func(m FrameSeedMetrics) float64 { return m.IsolationBA })),
	}
	contentLo := float64(res.FE2Content.CILower)
	nonInfFail := ""
	if float64(res.FE2Metadata.CILower) < fe2NonInf {
		nonInfFail = res.FE2Metadata.Name
	}
	for _, e := range res.FE2V0 {
		if len(e.Diffs) > 0 && float64(e.CILower) < fe2NonInf && nonInfFail == "" {
			nonInfFail = e.Name
		}
	}
	switch {
	case len(res.FE2Content.Diffs) < minSeedsForVerdict:
		res.FE2Verdict = VerdictNotEvaluable
		res.FE2Why = fmt.Sprintf("content-cued evaluable seeds < %d", minSeedsForVerdict)
	case contentLo < fe2Margin:
		res.FE2Verdict = "KILL"
		res.FE2Why = fmt.Sprintf("content-cued CI lower %.4f < +%.2f — query-time provenance filtering suffices; the compile-time-frames bet is falsified", contentLo, fe2Margin)
	case nonInfFail != "":
		res.FE2Verdict = VerdictFail
		res.FE2Why = fmt.Sprintf("superiority met (content-cued CI lower %.4f) but non-inferiority violated on %q (CI lower < %.2f)", contentLo, nonInfFail, fe2NonInf)
	default:
		res.FE2Verdict = VerdictPass
		res.FE2Why = fmt.Sprintf("content-cued CI lower %.4f >= +%.2f and all non-inferiority legs hold (margin %.2f)", contentLo, fe2Margin, fe2NonInf)
	}

	// ---- F-E2' (re-specified, filterability; §10 2026-07-18) ----
	// PROPOSED reading pending Toni's ratification. Same ±15pp / -2pp
	// arithmetic as reading (a), but on the filtering-resistant pool with
	// filter-decidable as the non-inferiority leg.
	resistLo := float64(res.FE2Resistant.CILower)
	decNonInf := float64(res.FE2Decidable.CILower) >= fe2NonInf
	v0NonInf := nonInfFail == "" // reuses the v0 legs computed above
	switch {
	case len(res.FE2Resistant.Diffs) < minSeedsForVerdict:
		res.FE2ResistVerdict = VerdictNotEvaluable
		res.FE2ResistWhy = fmt.Sprintf("filtering-resistant evaluable seeds < %d", minSeedsForVerdict)
	case resistLo < fe2Margin:
		res.FE2ResistVerdict = "KILL"
		res.FE2ResistWhy = fmt.Sprintf("filtering-resistant CI lower %.4f < +%.2f", resistLo, fe2Margin)
	case decNonInf && v0NonInf:
		res.FE2ResistVerdict = VerdictPass
		res.FE2ResistWhy = fmt.Sprintf("filtering-resistant CI lower %.4f >= +%.2f, non-inferior on filter-decidable and v0", resistLo, fe2Margin)
	default:
		res.FE2ResistVerdict = VerdictFail
		leg := "filter-decidable"
		if v0NonInf {
			leg = res.FE2Decidable.Name
		} else if nonInfFail != "" {
			leg = nonInfFail
		}
		res.FE2ResistWhy = fmt.Sprintf("superiority met (CI lower %.4f) but non-inferiority violated on %q", resistLo, leg)
	}

	// ---- F-E4 ----
	res.FE4Ideation = singleEndpoint("F-E4 ideation micro-F1 (A)", labels,
		col(res.MetricsA, func(m FrameSeedMetrics) float64 { return m.IdeationF1 }))
	res.FE4Exact = singleEndpoint("F-E4 ideation exact-set rate (A)", labels,
		col(res.MetricsA, func(m FrameSeedMetrics) float64 { return float64(m.IdeationExact) }))
	if m := float64(res.FE4Ideation.MeanDiff); !math.IsNaN(m) {
		if m >= fe4Gate {
			res.FE4Verdict = VerdictPass
			res.FE4Why = fmt.Sprintf("ideation micro-F1 mean %.4f >= %.2f", m, fe4Gate)
		} else {
			res.FE4Verdict = VerdictFail
			res.FE4Why = fmt.Sprintf("ideation micro-F1 mean %.4f < %.2f (secondary, no kill semantics)", m, fe4Gate)
		}
	} else {
		res.FE4Verdict = VerdictNotEvaluable
		res.FE4Why = "no evaluable seeds"
	}

	// Error-poisoned seeds warning (same rule as v0 Analyze).
	var poisoned []string
	for i := range labels {
		ea := res.MetricsA[i].Usage != nil && res.MetricsA[i].Usage.Errors > 0
		eb := res.MetricsB[i].Usage != nil && res.MetricsB[i].Usage.Errors > 0
		if ea || eb {
			poisoned = append(poisoned, labels[i])
		}
	}
	if len(poisoned) > 0 {
		res.Warnings = append(res.Warnings, fmt.Sprintf(
			"DATA QUALITY: %d seed(s) EXCLUDED for API errors: %s — re-run before trusting any verdict",
			len(poisoned), strings.Join(poisoned, ", ")))
	}
	if n := len(res.FE2Content.Diffs); n >= minSeedsForVerdict && n < warnSeedsBelow {
		res.Warnings = append(res.Warnings, fmt.Sprintf("only %d seeds — the registration targets 20; treat verdicts as provisional", n))
	}
	return res, nil
}

// Render prints per-seed tables, endpoints, and the three verdict blocks.
func (res *FramesResult) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "frames aggregate: A=%s vs B=%s over %d seeds\n\n", res.CondA, res.CondB, res.SeedCount)

	fmt.Fprintf(&b, "%-8s %10s %10s %10s %10s %10s %10s %10s %10s\n",
		"seed", "A cont", "A iso", "A gap", "A cont-cue", "B cont-cue", "A meta-cue", "B meta-cue", "A idea-F1")
	for i := range res.MetricsA {
		ma, mb := res.MetricsA[i], res.MetricsB[i]
		fmt.Fprintf(&b, "%-8s %10.4f %10.4f %10.4f %10.4f %10.4f %10.4f %10.4f %10.4f\n",
			ma.Seed, ma.ContaminationBA, ma.IsolationBA, float64(ma.GapBA),
			float64(ma.ContentCuedBA), float64(mb.ContentCuedBA),
			float64(ma.MetadataCuedBA), float64(mb.MetadataCuedBA), ma.IdeationF1)
	}

	b.WriteString("\nendpoints (95% bootstrap CI, 10k resamples, RNG seed 42):\n")
	all := []Endpoint{res.FE1Contamination, res.FE1Isolation, res.FE1Gap, res.FE2Content, res.FE2Metadata, res.FE2Resistant, res.FE2Decidable}
	all = append(all, res.FE2V0...)
	all = append(all, res.FE2Additional...)
	all = append(all, res.FE4Ideation, res.FE4Exact)
	for _, e := range all {
		if len(e.Diffs) == 0 {
			fmt.Fprintf(&b, "  %-46s not evaluable\n", e.Name)
			continue
		}
		skipped := ""
		if e.Skipped > 0 {
			skipped = fmt.Sprintf("  [%d seeds skipped]", e.Skipped)
		}
		fmt.Fprintf(&b, "  %-46s mean %+8.4f  CI [%+8.4f, %+8.4f]  n=%d%s\n",
			e.Name, e.MeanDiff, e.CILower, e.CIUpper, len(e.Diffs), skipped)
	}

	b.WriteString("\n==== REGISTERED VERDICTS (MASTERPLAN §9.6.7) ====\n")
	fmt.Fprintf(&b, "  F-E1 (safety, both directions):    %s — %s\n", res.FE1Verdict, res.FE1Why)
	fmt.Fprintf(&b, "  F-E2  reading (a) lexical-markedness: %s — %s\n", res.FE2Verdict, res.FE2Why)
	fmt.Fprintf(&b, "  F-E2' reading (b) filterability [PROPOSED, §10 2026-07-18, pending ratification]: %s — %s\n", res.FE2ResistVerdict, res.FE2ResistWhy)
	fmt.Fprintf(&b, "  F-E4 (ideation attribution):       %s — %s\n", res.FE4Verdict, res.FE4Why)
	for _, w := range res.Warnings {
		fmt.Fprintf(&b, "  WARNING: %s\n", w)
	}
	return b.String()
}
