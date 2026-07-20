package loom

import (
	"testing"

	"github.com/vaudience/synthworld/gen"
)

func scFactCand(line, frame string) Candidate {
	return Candidate{Kind: CandFact, SourceSpan: line,
		Fact: &FactCand{Relation: "certified", Args: map[string]string{"person": "person_02"}, Frame: frame}}
}

// A line homed to "" in 3 of 5 runs and to "Fiction" in 2 must vote "".
// A line present in only 2 of 5 runs is unstable and must be dropped.
func TestReconcileFrameVote(t *testing.T) {
	ep := gen.Episode{ID: "ep_x", Text: "line A: certified person_02\nline B: rare"}
	runs := [][]Candidate{
		{scFactCand("line A: certified person_02", ""), scFactCand("line B: rare", "Fiction")},
		{scFactCand("line A: certified person_02", "Fiction"), scFactCand("line B: rare", "Fiction")},
		{scFactCand("line A: certified person_02", "")},
		{scFactCand("line A: certified person_02", "Fiction")},
		{scFactCand("line A: certified person_02", "")},
	}
	out, _, err := reconcileFrameVote(runs, 5, ep)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 stable candidate (line A), got %d", len(out))
	}
	if out[0].Fact.Frame != "" {
		t.Errorf("line A frame voted %q, want \"\" (3 vs 2 majority)", out[0].Fact.Frame)
	}
	// line B appeared in 2/5 runs (< majority 3) → dropped
	for _, c := range out {
		if c.SourceSpan == "line B: rare" {
			t.Error("line B (2/5 runs) should have been dropped as unstable")
		}
	}
}

// The rul_019 archetype: the correct home wins 4:1 → voted correctly.
func TestReconcileFrameVoteRuleHoming(t *testing.T) {
	rule := func(frame string) Candidate {
		return Candidate{Kind: CandRule, SourceSpan: "rul_019 line",
			Rule: &RuleCand{Name: "priority case", Frame: frame}}
	}
	ep := gen.Episode{ID: "ep_133", Text: "rul_019 line"}
	runs := [][]Candidate{
		{rule("")}, {rule("")}, {rule("The Unsigned Page")}, {rule("")}, {rule("")},
	}
	out, _, err := reconcileFrameVote(runs, 5, ep)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Rule.Frame != "" {
		t.Fatalf("rul_019 should home to \"\" (4:1), got %+v", out)
	}
}
