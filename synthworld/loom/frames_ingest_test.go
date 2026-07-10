package loom

import (
	"testing"

	"github.com/vaudience/synthworld/gen"
	"github.com/vaudience/synthworld/world"
)

// TestFramesIngest pins the frame semantics of structured ingest
// (frames-v1 build step 4): frame declarations land in the store's frame
// table, frame-homed facts (including quotes in perspective frames, blocks,
// and overrides that duplicate actual atom keys) commit distinctly,
// non-assertive speech is skipped, promotions are recorded without closure
// impact, and Holds/Find answer in the query's frame.
func TestFramesIngest(t *testing.T) {
	atom := func(p string) world.Atom {
		return world.Atom{Relation: "knows", Args: map[string]string{"person": "person_01", "person2": p}}
	}
	episodes := []gen.Episode{
		{ID: "ep_001", Events: []gen.Event{
			{Kind: gen.EvFrame, Frame: &world.Frame{ID: "fic_01", Kind: world.FrameFiction, CreatedDay: 2}},
			{Kind: gen.EvFrame, Frame: &world.Frame{ID: "psp_p2", Kind: world.FramePerspective, CreatedDay: 2}},
			{Kind: gen.EvFrame, Frame: &world.Frame{ID: "scn_live", Kind: world.FrameScenario, Basis: world.FrameLive, CreatedDay: 3}},
			{Kind: gen.EvFact, Fact: &world.BaseFact{ID: "f_act", Atom: atom("person_02"), From: 1}},
		}},
		{ID: "ep_002", Events: []gen.Event{
			// fiction contradiction fact
			{Kind: gen.EvFact, Fact: &world.BaseFact{ID: "f_fic", Atom: atom("person_03"), From: 5, FrameID: "fic_01"}},
			// quote: an assertion homed in the speaker's perspective frame
			{Kind: gen.EvFact, AssertionType: gen.AssertQuote,
				Fact: &world.BaseFact{ID: "f_quo", Atom: atom("person_04"), From: 5, FrameID: "psp_p2"}},
			// sarcasm: literal content asserted nowhere — must be skipped
			{Kind: gen.EvFact, AssertionType: gen.AssertNonAssertive,
				Fact: &world.BaseFact{ID: "f_sar", Atom: atom("person_05"), From: 5}},
			// scenario override: block the inherited atom + assert a replacement
			{Kind: gen.EvFact, Fact: &world.BaseFact{ID: "f_blk", Atom: atom("person_02"), From: 6, FrameID: "scn_live", Block: true}},
			{Kind: gen.EvFact, Fact: &world.BaseFact{ID: "f_ovr", Atom: atom("person_06"), From: 6, FrameID: "scn_live"}},
			{Kind: gen.EvPromotion, Promotion: &gen.PromotionEvent{
				PredictionFactID: "f_quo", ActualFactID: "f_act", FromFrame: "psp_p2", Day: 7}},
		}},
	}

	s := NewStore()
	rep, err := s.IngestStructured(episodes)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Frames != 3 || rep.Promotions != 1 || rep.NonAssertive != 1 {
		t.Fatalf("report frames=%d promotions=%d non-assertive=%d, want 3/1/1", rep.Frames, rep.Promotions, rep.NonAssertive)
	}
	if rep.Facts != 5 || len(rep.Problems) != 0 {
		t.Fatalf("report facts=%d problems=%v, want 5 facts, no problems", rep.Facts, rep.Problems)
	}

	holdsIn := func(frame string, a world.Atom) bool {
		ok, _, err := s.HoldsIn(frame, a, 10)
		if err != nil {
			t.Fatalf("HoldsIn(%s): %v", frame, err)
		}
		return ok
	}
	// fiction stays out of actual; visible in its home frame
	if holdsIn(world.ActualFrame, atom("person_03")) || !holdsIn("fic_01", atom("person_03")) {
		t.Error("fiction fact leaked into actual or missing from fic_01")
	}
	// quote stays in the perspective frame
	if holdsIn(world.ActualFrame, atom("person_04")) || !holdsIn("psp_p2", atom("person_04")) {
		t.Error("quote asserted in actual or missing from psp_p2")
	}
	// sarcasm literal holds nowhere
	for _, fr := range []string{world.ActualFrame, "fic_01", "psp_p2", "scn_live"} {
		if holdsIn(fr, atom("person_05")) {
			t.Errorf("sarcasm literal believed in %s", fr)
		}
	}
	// scenario override: block hides the inherited atom, replacement holds
	// in the scenario only; actual is untouched
	if holdsIn("scn_live", atom("person_02")) {
		t.Error("blocked inherited atom still holds in scn_live")
	}
	if !holdsIn("scn_live", atom("person_06")) || holdsIn(world.ActualFrame, atom("person_06")) {
		t.Error("override missing in scn_live or leaked into actual")
	}
	if !holdsIn(world.ActualFrame, atom("person_02")) {
		t.Error("actual fact lost")
	}

	// find in a frame: scenario sees the inherited fact minus block, plus override
	pat := world.PatternAtom{Relation: "knows", Args: map[string]world.Term{
		"person": world.C("person_01"), "person2": world.V("X"),
	}}
	got, err := s.FindIn("scn_live", pat, "person2", 10)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"person_06"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("FindIn(scn_live) = %v, want %v", got, want)
	}

	// re-ingest is idempotent (dedupe on frame+block+atom+interval)
	rep2, err := s.IngestStructured(episodes)
	if err != nil {
		t.Fatal(err)
	}
	if rep2.Facts != 5 || len(s.Facts) != 5 {
		t.Fatalf("re-ingest: %d committed events, %d stored facts, want 5/5", rep2.Facts, len(s.Facts))
	}
}
