package harness

import (
	"errors"
	"reflect"
	"testing"

	"github.com/vaudience/synthworld/gen"
)

// scriptedCondition answers from injected functions; the scoring tests build
// tiny synthetic query sets and drive Run through every tally path.
type scriptedCondition struct {
	name  string
	holds func(q SanitizedQuery) (bool, error)
	find  func(q SanitizedQuery) ([]string, error)
}

func (c *scriptedCondition) Name() string                 { return c.name }
func (c *scriptedCondition) Ingest(_ []gen.Episode) error { return nil }
func (c *scriptedCondition) AnswerHolds(q SanitizedQuery) (bool, error) {
	return c.holds(q)
}
func (c *scriptedCondition) AnswerFind(q SanitizedQuery) ([]string, error) {
	return c.find(q)
}

func constCondition(v bool) *scriptedCondition {
	return &scriptedCondition{
		name:  "const",
		holds: func(SanitizedQuery) (bool, error) { return v, nil },
		find:  func(SanitizedQuery) ([]string, error) { return nil, nil },
	}
}

func holdsQ(id, slice string, answer bool, depth int) gen.Query {
	a := answer
	return gen.Query{ID: id, Slice: slice, Type: "holds", Answer: &a, Depth: depth}
}

func revQ(id string, answer bool, stale *bool) gen.Query {
	a := answer
	return gen.Query{ID: id, Slice: "revision", Type: "holds", Answer: &a, StaleAnswer: stale}
}

func findQ(id string, answerSet []string) gen.Query {
	return gen.Query{ID: id, Slice: "composition", Type: "find", AnswerSet: answerSet}
}

func bp(v bool) *bool { return &v }

// --- flip vs retained classification ---

func TestRevisionFlipRetainedClassification(t *testing.T) {
	cases := []struct {
		name        string
		q           gen.Query
		wantFlips   int
		wantRetains int
	}{
		{"stale differs from truth => flip", revQ("q1", true, bp(false)), 1, 0},
		{"stale differs, inverted polarity => flip", revQ("q2", false, bp(true)), 1, 0},
		{"stale equals truth => retained", revQ("q3", true, bp(true)), 0, 1},
		{"stale equals truth, false => retained", revQ("q4", false, bp(false)), 0, 1},
		{"missing stale answer => retained bucket", revQ("q5", true, nil), 0, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rep, err := Run(constCondition(true), nil, []gen.Query{tc.q})
			if err != nil {
				t.Fatal(err)
			}
			if rep.Revision.FlipTotal != tc.wantFlips {
				t.Errorf("FlipTotal = %d, want %d", rep.Revision.FlipTotal, tc.wantFlips)
			}
			if rep.Revision.RetainTotal != tc.wantRetains {
				t.Errorf("RetainTotal = %d, want %d", rep.Revision.RetainTotal, tc.wantRetains)
			}
		})
	}
}

// --- stale-agreement counting ---

func TestRevisionStaleAgreement(t *testing.T) {
	// Flip: truth=true, stale=false. A wrong answer on a boolean flip is
	// necessarily the stale answer (want != stale, so !want == stale) — the
	// stale-agreement counter must catch it.
	flip := revQ("f1", true, bp(false))
	rep, err := Run(constCondition(false), nil, []gen.Query{flip})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Revision.FlipCorrect != 0 || rep.Revision.StaleAgreements != 1 {
		t.Errorf("wrong flip answer matching stale: FlipCorrect=%d StaleAgreements=%d, want 0/1",
			rep.Revision.FlipCorrect, rep.Revision.StaleAgreements)
	}

	// Correct flip answer must not count as stale agreement.
	rep, err = Run(constCondition(true), nil, []gen.Query{flip})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Revision.FlipCorrect != 1 || rep.Revision.StaleAgreements != 0 {
		t.Errorf("correct flip answer: FlipCorrect=%d StaleAgreements=%d, want 1/0",
			rep.Revision.FlipCorrect, rep.Revision.StaleAgreements)
	}

	// Wrong answer on a RETAINED control (truth == stale) is wrong but does
	// not agree with a stale belief distinct from truth; StaleAgreements
	// must stay 0.
	retained := revQ("r1", true, bp(true))
	rep, err = Run(constCondition(false), nil, []gen.Query{retained})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Revision.RetainCorrect != 0 || rep.Revision.StaleAgreements != 0 {
		t.Errorf("wrong retained answer: RetainCorrect=%d StaleAgreements=%d, want 0/0",
			rep.Revision.RetainCorrect, rep.Revision.StaleAgreements)
	}
}

// --- positives and negatives tracked separately; the always-true signature ---

func TestSlicePolaritySeparation(t *testing.T) {
	queries := []gen.Query{
		holdsQ("rp1", "repetition", true, 0),
		holdsQ("rp2", "repetition", true, 0),
		holdsQ("rn1", "repetition", false, 0),
		holdsQ("cp1", "composition", true, 1),
		holdsQ("cn1", "composition", false, 1),
		holdsQ("cn2", "composition", false, 2),
	}
	rep, err := Run(constCondition(true), nil, queries)
	if err != nil {
		t.Fatal(err)
	}
	// always-true: all positives right, all negatives wrong — per slice.
	if rep.Repetition.PosCorrect != 2 || rep.Repetition.PosTotal != 2 ||
		rep.Repetition.NegCorrect != 0 || rep.Repetition.NegTotal != 1 {
		t.Errorf("repetition = %+v, want 2/2 pos, 0/1 neg", rep.Repetition)
	}
	if rep.Composition.PosCorrect != 1 || rep.Composition.PosTotal != 1 ||
		rep.Composition.NegCorrect != 0 || rep.Composition.NegTotal != 2 {
		t.Errorf("composition = %+v, want 1/1 pos, 0/2 neg", rep.Composition)
	}

	// always-false is the mirror image.
	rep, err = Run(constCondition(false), nil, queries)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Repetition.PosCorrect != 0 || rep.Repetition.NegCorrect != 1 ||
		rep.Composition.PosCorrect != 0 || rep.Composition.NegCorrect != 2 {
		t.Errorf("always-false: rep=%+v comp=%+v", rep.Repetition, rep.Composition)
	}
}

// --- composition per-depth bucketing ---

func TestCompositionPerDepthBuckets(t *testing.T) {
	queries := []gen.Query{
		holdsQ("d1a", "composition", true, 1),
		holdsQ("d2a", "composition", true, 2),
		holdsQ("d2b", "composition", false, 2),
		holdsQ("d3a", "composition", true, 3),
		// repetition must NOT land in PerDepth
		holdsQ("rp1", "repetition", true, 5),
	}
	rep, err := Run(constCondition(true), nil, queries)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.PerDepth) != 3 {
		t.Fatalf("PerDepth has %d buckets, want 3 (got %v)", len(rep.PerDepth), rep.PerDepth)
	}
	if s := rep.PerDepth[2]; s == nil || s.PosTotal != 1 || s.NegTotal != 1 || s.PosCorrect != 1 || s.NegCorrect != 0 {
		t.Errorf("depth-2 bucket = %+v, want pos 1/1 neg 0/1", rep.PerDepth[2])
	}
	if _, ok := rep.PerDepth[5]; ok {
		t.Error("repetition query leaked into PerDepth")
	}
	// bucket totals must sum to the composition slice totals
	sumPos, sumNeg := 0, 0
	for _, s := range rep.PerDepth {
		sumPos += s.PosTotal
		sumNeg += s.NegTotal
	}
	if sumPos != rep.Composition.PosTotal || sumNeg != rep.Composition.NegTotal {
		t.Errorf("PerDepth totals (%d,%d) != composition totals (%d,%d)",
			sumPos, sumNeg, rep.Composition.PosTotal, rep.Composition.NegTotal)
	}
}

// --- find scoring ---

func TestFindScoring(t *testing.T) {
	cases := []struct {
		name       string
		got, want  []string
		exact      bool
		tp, fp, fn int
	}{
		{"exact match order-independent", []string{"b", "a"}, []string{"a", "b"}, true, 2, 0, 0},
		{"duplicates collapse to set", []string{"a", "a"}, []string{"a"}, true, 1, 0, 0},
		{"partial overlap", []string{"a", "c"}, []string{"a", "b"}, false, 1, 1, 1},
		{"empty prediction, nonempty truth", nil, []string{"a", "b"}, false, 0, 0, 2},
		{"nonempty prediction, empty truth", []string{"x"}, nil, false, 0, 1, 0},
		{"both empty is an exact match", nil, nil, true, 0, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cond := &scriptedCondition{
				name:  "find",
				holds: func(SanitizedQuery) (bool, error) { return false, nil },
				find:  func(SanitizedQuery) ([]string, error) { return tc.got, nil },
			}
			rep, err := Run(cond, nil, []gen.Query{findQ("f1", tc.want)})
			if err != nil {
				t.Fatal(err)
			}
			if got := rep.Find.ExactMatches == 1; got != tc.exact {
				t.Errorf("exact = %v, want %v", got, tc.exact)
			}
			if rep.Find.MicroTP != tc.tp || rep.Find.MicroFP != tc.fp || rep.Find.MicroFN != tc.fn {
				t.Errorf("micro TP/FP/FN = %d/%d/%d, want %d/%d/%d",
					rep.Find.MicroTP, rep.Find.MicroFP, rep.Find.MicroFN, tc.tp, tc.fp, tc.fn)
			}
		})
	}
}

func TestFindMicroF1Arithmetic(t *testing.T) {
	// Two find queries: q1 got {a,c} want {a,b}; q2 got {d} want {d}.
	// Pooled micro: TP=2, FP=1, FN=1 → P=2/3, R=2/3, F1=2/3.
	answers := map[string][]string{"f1": {"a", "c"}, "f2": {"d"}}
	cond := &scriptedCondition{
		name:  "find",
		holds: func(SanitizedQuery) (bool, error) { return false, nil },
		find:  func(q SanitizedQuery) ([]string, error) { return answers[q.ID], nil },
	}
	rep, err := Run(cond, nil, []gen.Query{
		findQ("f1", []string{"a", "b"}),
		findQ("f2", []string{"d"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	const want = 2.0 / 3.0
	if diff := rep.Find.MicroPrecision - want; diff > 1e-12 || diff < -1e-12 {
		t.Errorf("micro precision = %v, want %v", rep.Find.MicroPrecision, want)
	}
	if diff := rep.Find.MicroRecall - want; diff > 1e-12 || diff < -1e-12 {
		t.Errorf("micro recall = %v, want %v", rep.Find.MicroRecall, want)
	}
	if diff := rep.Find.F1 - want; diff > 1e-12 || diff < -1e-12 {
		t.Errorf("micro F1 = %v, want %v", rep.Find.F1, want)
	}
	if rep.Find.ExactMatches != 1 || rep.Find.Total != 2 {
		t.Errorf("exact = %d/%d, want 1/2", rep.Find.ExactMatches, rep.Find.Total)
	}
}

// --- errors do not corrupt tallies ---

func TestErrorsCountedNotScored(t *testing.T) {
	cond := &scriptedCondition{
		name: "flaky",
		holds: func(q SanitizedQuery) (bool, error) {
			if q.ID == "bad" {
				return false, errors.New("boom")
			}
			return true, nil
		},
		find: func(SanitizedQuery) ([]string, error) { return nil, errors.New("boom") },
	}
	rep, err := Run(cond, nil, []gen.Query{
		holdsQ("bad", "composition", true, 1),
		holdsQ("ok", "composition", true, 1),
		findQ("fbad", []string{"a"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Errors != 2 {
		t.Errorf("Errors = %d, want 2", rep.Errors)
	}
	if rep.Composition.PosTotal != 1 || rep.Composition.PosCorrect != 1 {
		t.Errorf("errored query leaked into tallies: %+v", rep.Composition)
	}
	if rep.Find.Total != 0 {
		t.Errorf("errored find query leaked into tallies: %+v", rep.Find)
	}
}

// --- determinism: scoring is a pure function of (condition, queries) ---

func TestScoringDeterministic(t *testing.T) {
	queries := []gen.Query{
		holdsQ("rp1", "repetition", true, 0),
		holdsQ("cp1", "composition", true, 2),
		holdsQ("cn1", "composition", false, 1),
		revQ("rv1", true, bp(false)),
		revQ("rv2", false, bp(false)),
		findQ("f1", []string{"a", "b"}),
	}
	r1, err := Run(constCondition(true), nil, queries)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := Run(constCondition(true), nil, queries)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r1, r2) {
		t.Errorf("identical runs differ:\n%+v\n%+v", r1, r2)
	}
}
