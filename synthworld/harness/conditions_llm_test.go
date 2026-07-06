package harness

import (
	"context"
	"strings"
	"testing"

	"github.com/vaudience/synthworld/gen"
	"github.com/vaudience/synthworld/world"
)

// promptSpy records the prompts it receives so tests can assert what a
// condition actually shows the LLM.
type promptSpy struct {
	resp    string
	systems []string
	users   []string
}

func (s *promptSpy) Model() string { return "spy" }
func (s *promptSpy) Complete(_ context.Context, system, user string) (string, error) {
	s.systems = append(s.systems, system)
	s.users = append(s.users, user)
	return s.resp, nil
}

func testEpisodes() []gen.Episode {
	return []gen.Episode{
		{ID: "ep_001", Day: 3, Text: "On day 3, customer_01 enrolled in plan_gold."},
		{ID: "ep_002", Day: 9, Text: "Policy r1: IF enrolled THEN discounted."},
	}
}

func holdsQuery(id string) SanitizedQuery {
	return SanitizedQuery{
		ID: id, Type: "holds", AtDay: 360,
		Atom: &world.Atom{Relation: "discounted", Args: map[string]string{"who": "customer_01"}},
		Text: "As of day 360, is customer_01 discounted?",
	}
}

func TestC0SeesNoEpisodes(t *testing.T) {
	spy := &promptSpy{resp: "true"}
	c0 := &C0Condition{LLM: spy}
	if c0.Name() != "c0-no-memory" {
		t.Fatalf("name: %s", c0.Name())
	}
	if err := c0.Ingest(testEpisodes()); err != nil {
		t.Fatal(err)
	}
	got, err := c0.AnswerHolds(holdsQuery("q1"))
	if err != nil || !got {
		t.Fatalf("holds: %v %v", got, err)
	}
	prompt := spy.systems[0] + spy.users[0]
	if strings.Contains(prompt, "customer_01 enrolled") {
		t.Fatal("C0 must not see episode text")
	}
	if !strings.Contains(spy.users[0], "is customer_01 discounted") {
		t.Fatal("C0 must see the question text")
	}
	if !strings.Contains(spy.users[0], `"discounted"`) {
		t.Fatal("C0 must see the structured atom")
	}
}

func TestD6SeesExactlyProvenance(t *testing.T) {
	spy := &promptSpy{resp: "true"}
	d6 := NewPerfectRetrievalCondition(spy, map[string][]string{"q1": {"ep_001"}})
	if d6.Name() != "d6-perfect-retrieval" {
		t.Fatalf("name: %s", d6.Name())
	}
	if err := d6.Ingest(testEpisodes()); err != nil {
		t.Fatal(err)
	}
	if _, err := d6.AnswerHolds(holdsQuery("q1")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(spy.users[0], "customer_01 enrolled") {
		t.Fatal("D6 must include the provenance episode text")
	}
	if strings.Contains(spy.users[0], "Policy r1") {
		t.Fatal("D6 must include ONLY provenance episodes, not the rest")
	}

	// a query without provenance (negative control) gets an empty context
	if _, err := d6.AnswerHolds(holdsQuery("q_negative")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(spy.users[1], "(no episodes)") {
		t.Fatalf("no-provenance query must get an empty context, got: %s", spy.users[1])
	}

	// unknown provenance episode is a loud error, not silence
	bad := NewPerfectRetrievalCondition(spy, map[string][]string{"q1": {"ep_missing"}})
	if err := bad.Ingest(testEpisodes()); err != nil {
		t.Fatal(err)
	}
	if _, err := bad.AnswerHolds(holdsQuery("q1")); err == nil {
		t.Fatal("missing provenance episode must error")
	}
}
