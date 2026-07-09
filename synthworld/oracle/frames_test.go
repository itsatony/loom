package oracle

import (
	"testing"

	"github.com/vaudience/synthworld/world"
)

// frameWorld builds a small hand-checked world exercising every frame
// mechanism: fiction (non-inheriting), live + pinned scenarios (inheriting
// actual with deltas), a perspective frame, a block fact, a scenario-scoped
// supersession of an actual rule, and an actual revision after the pin day.
//
// Timeline: horizon 100.
//
//	actual: status(a)=ok from day 0; status(b)=ok from day 0 to day 50,
//	        then status(b)=down from day 50 (the revision).
//	rule R_act (actual, stratum 1): alert(x) if status(x)=down.
//	fiction:f1: status(c)=down (gap-style: c has no actual status).
//	scenario:live (inherits actual, live): delta fact status(a)=down,
//	        block fact removing status(b)=down... (block targets the
//	        post-revision atom), plus scenario supersession of R_act.
//	scenario:pin40 (inherits actual, pinned at day 40): sees the
//	        pre-revision world at eval day 90.
//	perspective:src1: status(d)=down claimed (d has no actual status).
func frameWorld() *world.World {
	ent := func(id, ty string) world.Entity { return world.Entity{ID: id, Type: world.EntityType(ty)} }
	atom := func(rel, x, v string) world.Atom {
		return world.Atom{Relation: rel, Args: map[string]string{"x": x, "v": v}}
	}
	w := &world.World{
		Seed:    1,
		Horizon: 100,
		Types:   []world.EntityType{"node", "state"},
		Entities: []world.Entity{
			ent("a", "node"), ent("b", "node"), ent("c", "node"), ent("d", "node"),
			ent("ok", "state"), ent("down", "state"),
		},
		Relations: []world.RelationSchema{
			{ID: "status", Name: "status", Slots: []world.SlotDef{{Name: "x", Type: "node"}, {Name: "v", Type: "state"}}, Stratum: 0},
			{ID: "alert", Name: "alert", Slots: []world.SlotDef{{Name: "x", Type: "node"}, {Name: "v", Type: "state"}}, Stratum: 1},
		},
		Frames: []world.Frame{
			{ID: "fiction:f1", Kind: world.FrameFiction, CreatedDay: 10},
			{ID: "scenario:live", Kind: world.FrameScenario, Parents: []string{"actual"}, Basis: world.FrameLive, CreatedDay: 60},
			{ID: "scenario:pin40", Kind: world.FrameScenario, Parents: []string{"actual"}, Basis: world.FramePinned, PinDay: 40, CreatedDay: 60},
			{ID: "perspective:src1", Kind: world.FramePerspective, CreatedDay: 20},
		},
		Facts: []world.BaseFact{
			{ID: "f_a_ok", Atom: atom("status", "a", "ok"), From: 0, EpisodeID: "e1"},
			{ID: "f_b_ok", Atom: atom("status", "b", "ok"), From: 0, To: 50, EpisodeID: "e1"},
			{ID: "f_b_down", Atom: atom("status", "b", "down"), From: 50, EpisodeID: "e2"},
			// fiction gap fact: c never has an actual status
			{ID: "f_c_down_fic", Atom: atom("status", "c", "down"), From: 0, EpisodeID: "e3", FrameID: "fiction:f1"},
			// scenario delta: a goes down in the what-if
			{ID: "f_a_down_sc", Atom: atom("status", "a", "down"), From: 60, EpisodeID: "e4", FrameID: "scenario:live"},
			// scenario block: remove the inherited b-down revision inside the what-if
			{ID: "f_b_down_blk", Atom: atom("status", "b", "down"), From: 60, EpisodeID: "e4", FrameID: "scenario:live", Block: true},
			// perspective claim: d is down according to src1
			{ID: "f_d_down_p", Atom: atom("status", "d", "down"), From: 0, EpisodeID: "e5", FrameID: "perspective:src1"},
		},
		Rules: []world.Rule{
			{
				ID: "R_act", Name: "alert-on-down",
				Conditions: []world.PatternAtom{{Relation: "status", Args: map[string]world.Term{"x": world.V("X"), "v": world.C("down")}}},
				Conclusion: world.PatternAtom{Relation: "alert", Args: map[string]world.Term{"x": world.V("X"), "v": world.C("down")}},
				Assert:     true, Authority: 3, IssuedAt: 5, EffectiveFrom: 0, EpisodeID: "e1",
			},
		},
		Supersessions: []world.Supersession{
			// scenario-scoped: inside scenario:live, R_act stops firing from day 70
			{ID: "S_sc", NewRule: "R_act", OldRule: "R_act", From: 70, EpisodeID: "e4", FrameID: "scenario:live"},
		},
	}
	return w
}

func holdsIn(t *testing.T, w *world.World, day int, frame, rel, x, v string) bool {
	t.Helper()
	cl, err := Eval(w, day, Options{Frame: frame})
	if err != nil {
		t.Fatalf("Eval(%s,%d): %v", frame, day, err)
	}
	return cl.Holds(world.Atom{Relation: rel, Args: map[string]string{"x": x, "v": v}})
}

func TestFrameSemantics(t *testing.T) {
	w := frameWorld()
	if err := w.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	cases := []struct {
		name  string
		day   int
		frame string
		x, v  string
		rel   string
		want  bool
	}{
		// contamination: fiction gap fact invisible in actual, visible in its frame
		{"fiction invisible in actual", 90, "actual", "c", "down", "status", false},
		{"fiction visible in fiction", 90, "fiction:f1", "c", "down", "status", true},
		{"fiction rule-derived alert stays in fiction", 90, "fiction:f1", "c", "down", "alert", false}, // R_act home=actual ∉ cone(fiction)
		{"no fiction alert in actual", 90, "actual", "c", "down", "alert", false},
		// perspective: claim visible only frame-open; not in actual
		{"perspective claim not in actual", 90, "actual", "d", "down", "status", false},
		{"perspective claim in its frame", 90, "perspective:src1", "d", "down", "status", true},
		// isolation: scenario sees untouched inherited actual facts
		{"scenario inherits actual fact", 90, "scenario:live", "b", "ok", "status", false},                 // b_ok expired day 50
		{"scenario inherits revision pre-block-window", 65, "scenario:live", "b", "down", "status", false}, // blocked from 60
		{"scenario delta holds", 90, "scenario:live", "a", "down", "status", true},
		{"scenario delta invisible in actual", 90, "actual", "a", "down", "status", false},
		// block: inherited b-down suppressed inside scenario from day 60
		{"block removes inherited atom", 90, "scenario:live", "b", "down", "status", false},
		{"blocked atom still in actual", 90, "actual", "b", "down", "status", true},
		// actual rule fires on scenario facts (visibility-monotone) — before scenario supersession
		{"actual rule fires on scenario delta at 65", 65, "scenario:live", "a", "down", "alert", true},
		// scenario-scoped supersession stops it from day 70, only inside the scenario
		{"scenario supersession stops rule at 90", 90, "scenario:live", "a", "down", "alert", false},
		{"actual rule unaffected by scenario supersession", 90, "actual", "b", "down", "alert", true},
		// pinning: pinned-at-40 scenario never sees the day-50 revision at eval 90
		{"pinned scenario frozen pre-revision", 90, "scenario:pin40", "b", "ok", "status", true},
		{"pinned scenario misses revision", 90, "scenario:pin40", "b", "down", "status", false},
		{"live scenario would track revision (block aside: a-alert)", 90, "scenario:pin40", "b", "down", "alert", false},
		// actual at pinned day for comparison
		{"actual at day 90 has revision", 90, "actual", "b", "ok", "status", false},
	}
	for _, c := range cases {
		if got := holdsIn(t, w, c.day, c.frame, c.rel, c.x, c.v); got != c.want {
			t.Errorf("%s: holds(%s(%s,%s), t=%d, frame=%s) = %v, want %v",
				c.name, c.rel, c.x, c.v, c.day, c.frame, got, c.want)
		}
	}
}

func TestFrameProximityPrecedence(t *testing.T) {
	// A high-authority actual rule vs a low-authority scenario rule concluding
	// a conflicting candidate for the same atom: proximity must win.
	w := frameWorld()
	// scenario block-rule (Assert=false) with authority 1 targeting the alert
	// that actual's authority-3 rule asserts: inside the scenario the nearer
	// block rule wins; in actual the alert stands.
	w.Rules = append(w.Rules, world.Rule{
		ID: "R_sc_block", Name: "scenario-suppress-alert",
		Conditions: []world.PatternAtom{{Relation: "status", Args: map[string]world.Term{"x": world.V("X"), "v": world.C("down")}}},
		Conclusion: world.PatternAtom{Relation: "alert", Args: map[string]world.Term{"x": world.V("X"), "v": world.C("down")}},
		Assert:     false, Authority: 1, IssuedAt: 61, EffectiveFrom: 0, EpisodeID: "e4", FrameID: "scenario:pin40",
	})
	if err := w.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	// In scenario:pin40 at day 90: pinned world has no down facts (b frozen
	// at ok), so use day 45 < pin? Instead check actual is unaffected and
	// scenario suppresses where a down fact exists. Add a scenario-local fact.
	w.Facts = append(w.Facts, world.BaseFact{
		ID: "f_e_down_sc", Atom: world.Atom{Relation: "status", Args: map[string]string{"x": "a", "v": "down"}},
		From: 0, EpisodeID: "e4", FrameID: "scenario:pin40",
	})
	if holdsIn(t, w, 90, "scenario:pin40", "alert", "a", "down") {
		t.Errorf("proximity precedence: scenario block rule (authority 1) must beat actual assert rule (authority 3) inside the scenario")
	}
	if !holdsIn(t, w, 90, "actual", "alert", "b", "down") {
		t.Errorf("actual alert must be unaffected by scenario-frame block rule")
	}
}

func TestV0BackwardsCompat(t *testing.T) {
	// Strip all frame content: evaluation must equal a pre-frames world.
	w := frameWorld()
	w.Frames = nil
	var facts []world.BaseFact
	for _, f := range w.Facts {
		if f.FrameID == "" {
			facts = append(facts, f)
		}
	}
	w.Facts = facts
	w.Supersessions = nil
	if err := w.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	clDefault, err := Eval(w, 90, Options{})
	if err != nil {
		t.Fatal(err)
	}
	clActual, err := Eval(w, 90, Options{Frame: "actual"})
	if err != nil {
		t.Fatal(err)
	}
	if len(clDefault.Atoms) != len(clActual.Atoms) {
		t.Fatalf("default vs explicit actual closure size mismatch: %d vs %d", len(clDefault.Atoms), len(clActual.Atoms))
	}
	for k := range clDefault.Atoms {
		if _, ok := clActual.Atoms[k]; !ok {
			t.Errorf("atom %s missing from explicit-actual closure", k)
		}
	}
	// expected: status(a,ok), status(b,down), alert(b,down)
	if len(clDefault.Atoms) != 3 {
		t.Errorf("v0 world closure size = %d, want 3", len(clDefault.Atoms))
	}
}

func TestFrameValidation(t *testing.T) {
	base := func() *world.World { return frameWorld() }

	w := base()
	w.Frames = append(w.Frames, world.Frame{ID: "actual", Kind: world.FrameScenario})
	if err := w.Validate(); err == nil {
		t.Error("declaring actual must fail")
	}

	w = base()
	w.Frames = append(w.Frames, world.Frame{ID: "p2", Kind: world.FramePerspective, Parents: []string{"actual"}})
	if err := w.Validate(); err == nil {
		t.Error("perspective with parents must fail")
	}

	w = base()
	w.Frames = append(w.Frames, world.Frame{ID: "s2", Kind: world.FrameScenario, Parents: []string{"perspective:src1"}})
	if err := w.Validate(); err == nil {
		t.Error("inheriting from a perspective frame must fail")
	}

	w = base()
	w.Frames = append(w.Frames,
		world.Frame{ID: "s2", Kind: world.FrameScenario, Parents: []string{"s3"}},
		world.Frame{ID: "s3", Kind: world.FrameScenario, Parents: []string{"s2"}},
	)
	if err := w.Validate(); err == nil {
		t.Error("frame cycle must fail")
	}

	w = base()
	w.Facts[0].FrameID = "nope"
	if err := w.Validate(); err == nil {
		t.Error("unknown item frame must fail")
	}

	w = base()
	w.Facts[0].Block = true // actual-frame block
	if err := w.Validate(); err == nil {
		t.Error("block fact in actual must fail")
	}

	w = base()
	w.Frames[2].PinDay = 999 // beyond horizon
	if err := w.Validate(); err == nil {
		t.Error("pin_day beyond horizon must fail")
	}
}
