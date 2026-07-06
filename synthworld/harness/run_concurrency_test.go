package harness

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/vaudience/synthworld/gen"
)

// parityCondition answers deterministically from the query ID, so any
// worker interleaving must produce the same Report.
type parityCondition struct{}

func (parityCondition) Name() string               { return "scripted" }
func (parityCondition) Ingest([]gen.Episode) error { return nil }
func (parityCondition) AnswerHolds(q SanitizedQuery) (bool, error) {
	if q.ID == "q_err" {
		return false, fmt.Errorf("scripted error")
	}
	// answer true iff the ID ends in an even digit
	last := q.ID[len(q.ID)-1]
	return (last-'0')%2 == 0, nil
}
func (parityCondition) AnswerFind(q SanitizedQuery) ([]string, error) {
	return []string{"e_" + q.ID}, nil
}

func boolPtr(b bool) *bool { return &b }

func concurrencyQueries() []gen.Query {
	var qs []gen.Query
	for i := 0; i < 40; i++ {
		slice := "repetition"
		if i%3 == 1 {
			slice = "composition"
		}
		if i%3 == 2 {
			slice = "revision"
		}
		q := gen.Query{
			ID: fmt.Sprintf("q%d", i), Type: "holds", Slice: slice,
			Answer: boolPtr(i%2 == 0), Depth: i % 3,
		}
		if slice == "revision" {
			q.StaleAnswer = boolPtr(i%4 == 0)
		}
		qs = append(qs, q)
	}
	for i := 0; i < 6; i++ {
		qs = append(qs, gen.Query{
			ID: fmt.Sprintf("f%d", i), Type: "find", Slice: "composition",
			AnswerSet: []string{fmt.Sprintf("e_f%d", i)},
		})
	}
	qs = append(qs, gen.Query{ID: "q_err", Type: "holds", Slice: "repetition", Answer: boolPtr(true)})
	return qs
}

func TestRunWorkersInvariantAcrossWorkerCounts(t *testing.T) {
	qs := concurrencyQueries()
	base, err := RunWorkers(parityCondition{}, nil, qs, 1)
	if err != nil {
		t.Fatal(err)
	}
	if base.Errors != 1 {
		t.Fatalf("scripted error must be counted: %+v", base)
	}
	for _, workers := range []int{2, 4, 9, 32} {
		got, err := RunWorkers(parityCondition{}, nil, qs, workers)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(base, got) {
			t.Fatalf("report differs at workers=%d:\nbase: %+v\ngot:  %+v", workers, base, got)
		}
	}
}

func TestRunCountsUnknownSliceLoudly(t *testing.T) {
	qs := []gen.Query{
		{ID: "q1", Type: "holds", Slice: "repetition", Answer: boolPtr(true)},
		{ID: "q2", Type: "holds", Slice: "typo_slice", Answer: boolPtr(true)},
		{ID: "q3", Type: "not_a_type", Slice: "repetition"},
	}
	rep, err := RunWorkers(parityCondition{}, nil, qs, 1)
	if err != nil {
		t.Fatal(err)
	}
	if rep.UnknownSlice != 2 {
		t.Fatalf("unknown slice/type queries must be counted, got %d", rep.UnknownSlice)
	}
	if rep.Repetition.PosTotal != 1 {
		t.Fatalf("known queries must still score: %+v", rep.Repetition)
	}
}
