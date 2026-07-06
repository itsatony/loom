package loom

import (
	"fmt"
	"testing"

	"github.com/vaudience/synthworld/gen"
	"github.com/vaudience/synthworld/world"
)

// buildDevWorld generates a small deterministic world + episodes for tests.
func buildDevWorld(t *testing.T, seed int64) (*gen.Builder, []gen.Episode) {
	t.Helper()
	cfg, err := gen.PresetConfig("small", seed)
	if err != nil {
		t.Fatal(err)
	}
	b := gen.NewBuilder(cfg)
	if err := b.BuildWorld(); err != nil {
		t.Fatal(err)
	}
	return b, b.BuildEpisodes()
}

func vocabOf(w *world.World) Vocabulary {
	v := Vocabulary{}
	for _, r := range w.Relations {
		rv := RelationVocab{ID: r.ID, Name: r.Name}
		for _, s := range r.Slots {
			rv.Slots = append(rv.Slots, s.Name)
		}
		v.Relations = append(v.Relations, rv)
	}
	return v
}

// TestDeterministicExtractorRoundTrip: every rendered event line parses
// back to a candidate that, normalized, equals the original world item.
// This is the inverse property that makes c2b-det a valid pipeline control.
func TestDeterministicExtractorRoundTrip(t *testing.T) {
	for _, seed := range []int64{42, 7, 99} {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			b, episodes := buildDevWorld(t, seed)
			w := b.World()
			p := NewPipeline(vocabOf(w), DeterministicExtractor{})
			rep, err := p.Compile(episodes)
			if err != nil {
				t.Fatal(err)
			}
			for _, tr := range rep.Traces {
				for _, prob := range tr.Problems {
					t.Errorf("trace problem: %s", prob)
				}
			}
			// every world fact/rule/supersession must be committed with
			// identical content
			factByID := map[string]world.BaseFact{}
			for _, sf := range p.Store.Facts {
				factByID[sf.Fact.ID] = sf.Fact
			}
			for _, wf := range w.Facts {
				got, ok := factByID[wf.ID]
				if !ok {
					t.Fatalf("fact %s missing from store", wf.ID)
				}
				if got.Atom.Key() != wf.Atom.Key() || got.From != wf.From || got.To != wf.To || got.Source != wf.Source {
					t.Errorf("fact %s mismatch:\n got %+v\nwant %+v", wf.ID, got, wf)
				}
			}
			ruleByID := map[string]world.Rule{}
			for _, sr := range p.Store.Rules {
				if sr.Lifecycle == Active {
					ruleByID[sr.Rule.ID] = sr.Rule
				}
			}
			for _, wr := range w.Rules {
				got, ok := ruleByID[wr.ID]
				if !ok {
					t.Fatalf("rule %s missing/inactive in store", wr.ID)
				}
				gotCopy, wantCopy := got, wr
				gotCopy.EpisodeID, wantCopy.EpisodeID = "", ""
				if !RulesEquivalent(&gotCopy, &wantCopy) {
					t.Errorf("rule %s mismatch:\n got %+v\nwant %+v", wr.ID, got, wr)
				}
			}
			supByID := map[string]world.Supersession{}
			for _, sp := range p.Store.Supersessions {
				supByID[sp.Supersession.ID] = sp.Supersession
			}
			for _, ws := range w.Supersessions {
				got, ok := supByID[ws.ID]
				if !ok {
					t.Fatalf("supersession %s missing from store", ws.ID)
				}
				if got.OldRule != ws.OldRule || got.NewRule != ws.NewRule || got.From != ws.From {
					t.Errorf("supersession %s mismatch: got %+v want %+v", ws.ID, got, ws)
				}
			}
		})
	}
}

func devVocab() Vocabulary {
	return Vocabulary{Relations: []RelationVocab{
		{ID: "rel_b00_knows", Name: "knows", Slots: []string{"person", "person2"}},
		{ID: "rel_b01_works_at", Name: "works_at", Slots: []string{"person", "org"}},
		{ID: "rel_d100_peer_of", Name: "peer_of", Slots: []string{"person", "person2"}},
	}}
}

func factCand(id, rel string, args map[string]string, from, to int) Candidate {
	return Candidate{Kind: CandFact, Confidence: 1,
		Fact: &FactCand{FactID: id, Relation: rel, Args: args, From: from, To: to, Source: "s"}}
}

type stubExtractor struct{ perEpisode map[string][]Candidate }

func (stubExtractor) Name() string { return "stub" }
func (s stubExtractor) Extract(ep gen.Episode) ([]Candidate, []string, error) {
	return s.perEpisode[ep.ID], nil, nil
}

func compileWith(t *testing.T, cands map[string][]Candidate, eps ...string) (*Pipeline, *CompileReport) {
	t.Helper()
	var episodes []gen.Episode
	for _, id := range eps {
		episodes = append(episodes, gen.Episode{ID: id})
	}
	p := NewPipeline(devVocab(), stubExtractor{perEpisode: cands})
	rep, err := p.Compile(episodes)
	if err != nil {
		t.Fatal(err)
	}
	return p, rep
}

func TestConsistencyVerdicts(t *testing.T) {
	args := map[string]string{"person": "person_01", "person2": "person_02"}
	_, rep := compileWith(t, map[string][]Candidate{
		"ep_001": {factCand("f1", "knows", args, 3, 0)},
		"ep_002": {
			factCand("f1b", "knows", args, 3, 0),    // exact duplicate → provenance merge
			factCand("f2", "knows", args, 5, 9),     // strictly narrower → refinement
			factCand("f3", "nosuchrel", args, 1, 0), // unknown relation → dropped
		},
	}, "ep_001", "ep_002")

	want := map[string]ItemVerdict{"f1": VCommitted, "f1b": VDuplicate, "f2": VRefinement, "f3": VDropped}
	got := rep.OutcomeByID()
	for id, v := range want {
		if got[id] != v {
			t.Errorf("%s: verdict %s, want %s", id, got[id], v)
		}
	}
	if rep.Duplicates != 1 {
		t.Errorf("duplicates = %d, want 1", rep.Duplicates)
	}
}

func TestRuleConflictAndQuarantine(t *testing.T) {
	condOK := []PatternCand{
		{Relation: "knows", Args: map[string]string{"person": "?A", "person2": "?B"}},
	}
	ruleOK := &RuleCand{RuleID: "r1", Name: "n", Authority: 2, IssuedAt: 1, EffectiveFrom: 1,
		Conditions: condOK, Conclusion: PatternCand{Relation: "peer_of", Args: map[string]string{"person": "?A", "person2": "?B"}}}
	// same ID, different authority → conflict, second dropped
	ruleClash := *ruleOK
	ruleClash.Authority = 5
	// unsafe: conclusion var ?Z unbound → quarantined
	ruleUnsafe := &RuleCand{RuleID: "r2", Name: "u", Authority: 2, IssuedAt: 1, EffectiveFrom: 1,
		Conditions: condOK, Conclusion: PatternCand{Relation: "peer_of", Args: map[string]string{"person": "?A", "person2": "?Z"}}}

	p, rep := compileWith(t, map[string][]Candidate{
		"ep_001": {
			{Kind: CandRule, Rule: ruleOK, Confidence: 1},
			{Kind: CandRule, Rule: &ruleClash, Confidence: 1},
			{Kind: CandRule, Rule: ruleUnsafe, Confidence: 1},
		},
	}, "ep_001")

	got := rep.OutcomeByID()
	if got["r1"] != VConflict && got["r1"] != VCommitted {
		// r1 appears twice in trace: committed then conflict; OutcomeByID
		// keeps the last — the conflict entry for the clashing candidate.
		t.Errorf("r1 verdict chain unexpected: %v", got["r1"])
	}
	if rep.Conflicts != 1 {
		t.Errorf("conflicts = %d, want 1", rep.Conflicts)
	}
	if got["r2"] != VQuarantined {
		t.Errorf("r2 verdict = %s, want quarantined", got["r2"])
	}
	sr := p.Store.ruleByID("r2")
	if sr == nil || sr.Lifecycle != Quarantined {
		t.Fatalf("r2 not stored as quarantined: %+v", sr)
	}
	// quarantined rules must be invisible to evaluation
	w, err := p.Store.worldView()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range w.Rules {
		if r.ID == "r2" {
			t.Error("quarantined rule leaked into the evaluated view")
		}
	}
	// the kept r1 must be the FIRST version (authority 2)
	if kept := p.Store.ruleByID("r1"); kept == nil || kept.Rule.Authority != 2 {
		t.Errorf("conflict resolution kept wrong rule version: %+v", kept)
	}
}

func TestCyclicRuleQuarantined(t *testing.T) {
	// peer_of → peer_of directly (self-cycle through the same relation)
	cyc := &RuleCand{RuleID: "rc", Name: "cyc", Authority: 2, IssuedAt: 1, EffectiveFrom: 1,
		Conditions: []PatternCand{{Relation: "peer_of", Args: map[string]string{"person": "?A", "person2": "?B"}}},
		Conclusion: PatternCand{Relation: "peer_of", Args: map[string]string{"person": "?B", "person2": "?A"}}}
	p, rep := compileWith(t, map[string][]Candidate{
		"ep_001": {{Kind: CandRule, Rule: cyc, Confidence: 1}},
	}, "ep_001")
	if rep.OutcomeByID()["rc"] != VQuarantined {
		t.Errorf("cyclic rule verdict = %s, want quarantined", rep.OutcomeByID()["rc"])
	}
	if _, err := p.Store.worldView(); err != nil {
		t.Fatalf("store view must stay evaluable after quarantining the cycle: %v", err)
	}
}

func TestNormalizationErrors(t *testing.T) {
	_, rep := compileWith(t, map[string][]Candidate{
		"ep_001": {
			factCand("fa", "knows", map[string]string{"person": "p1"}, 1, 0),                     // arity
			factCand("fb", "knows", map[string]string{"person": "p1", "nosuchslot": "p2"}, 1, 0), // slot
			factCand("fc", "knows", map[string]string{"person": "?A", "person2": "p2"}, 1, 0),    // var in ground fact
		},
	}, "ep_001")
	got := rep.OutcomeByID()
	for _, id := range []string{"fa", "fb", "fc"} {
		if got[id] != VDropped {
			t.Errorf("%s verdict = %s, want dropped", id, got[id])
		}
	}
}

func TestParallelExtractionDeterminism(t *testing.T) {
	b, episodes := buildDevWorld(t, 42)
	w := b.World()
	run := func(workers int) *CompileReport {
		p := NewPipeline(vocabOf(w), DeterministicExtractor{})
		p.Workers = workers
		rep, err := p.Compile(episodes)
		if err != nil {
			t.Fatal(err)
		}
		return rep
	}
	a, bb := run(1), run(8)
	if a.Facts != bb.Facts || a.Rules != bb.Rules || a.Supersessions != bb.Supersessions ||
		a.Quarantined != bb.Quarantined || a.Duplicates != bb.Duplicates || len(a.Traces) != len(bb.Traces) {
		t.Errorf("parallel compile diverges: %+v vs %+v", a, bb)
	}
}
