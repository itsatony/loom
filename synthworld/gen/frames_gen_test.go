package gen

import (
	"encoding/json"
	"testing"

	"github.com/vaudience/synthworld/world"
)

// buildFramesDataset builds the frames preset in-process. Dev seed 99 only:
// frames builds are batch-sized (~5 min each — the oracle runs per frame),
// so this suite is skipped under -short; run it explicitly with
//
//	go test ./gen/ -run TestFramesPreset -timeout 1800s
//
// Seed 42 is intractable under batch-level knobs (pre-existing rul_031 join
// explosion, identical on the plain batch preset) — it is simply a rejected
// candidate; dev seeds for frames are 99 (fast) and 7 (slower).
func buildFramesDataset(t *testing.T, seed int64, frames bool) (*Builder, []Episode, *QuerySet) {
	t.Helper()
	cfg, err := PresetConfig("frames", seed)
	if err != nil {
		t.Fatal(err)
	}
	b := NewBuilder(cfg)
	if frames {
		b.EnableFrames(FramesPreset("frames"))
	}
	if err := b.BuildWorld(); err != nil {
		t.Fatal(err)
	}
	eps := b.BuildEpisodes()
	qs, err := b.BuildQueries()
	if err != nil {
		t.Fatal(err)
	}
	return b, eps, qs
}

// One heavy test, three guarantees: (1) the frames preset generates, passes
// the independent validator (guarantee 5 included) and both gate sets;
// (2) same seed + same binary = identical dataset; (3) the frames layer
// appends only non-actual content — the v0 world prefix is untouched.
func TestFramesPreset(t *testing.T) {
	if testing.Short() {
		t.Skip("frames builds are batch-sized; run without -short and with -timeout 1800s")
	}
	const seed = 99

	b, eps, qs := buildFramesDataset(t, seed, true)

	// (1) validator + gates
	dir := t.TempDir()
	if err := b.WriteDataset(dir, eps, qs); err != nil {
		t.Fatal(err)
	}
	rep, err := VerifyDataset(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range rep.Problems {
		t.Error(p)
	}
	slices := map[string]int{}
	for _, q := range qs.Queries {
		slices[q.Slice]++
	}
	for _, s := range []string{"repetition", "composition", "revision",
		"contamination", "isolation", "pinning", "misattribution", "promotion", "ideation"} {
		if slices[s] == 0 {
			t.Errorf("slice %s has no queries", s)
		}
	}
	gs := ComputeFramesGateStats(qs, b.FrameStats)
	if reasons := EvaluateFramesGates(gs, DefaultFramesGateThresholds()); len(reasons) > 0 {
		t.Errorf("frames gates violated on dev seed %d: %v", seed, reasons)
	}
	vg := ComputeGateStats(qs, b.Stats)
	if reasons := EvaluateGates(vg, DefaultGateThresholds()); len(reasons) > 0 {
		t.Errorf("v0 gates violated on dev seed %d: %v", seed, reasons)
	}

	// (2) determinism
	b2, _, qs2 := buildFramesDataset(t, seed, true)
	w1, _ := json.Marshal(b.World())
	w2, _ := json.Marshal(b2.World())
	if string(w1) != string(w2) {
		t.Fatal("frames world not deterministic")
	}
	q1, _ := json.Marshal(qs.Queries)
	q2, _ := json.Marshal(qs2.Queries)
	if string(q1) != string(q2) {
		t.Fatal("frames queries not deterministic")
	}

	// (3) the frames layer leaves the actual world untouched: the plain
	// build (frames disabled, same knobs) must be an exact prefix, and
	// everything appended after it must home in a non-actual frame
	plain, _, _ := buildFramesDataset(t, seed, false)
	pw, fw := plain.World(), b.World()
	if len(fw.Facts) <= len(pw.Facts) {
		t.Fatal("frames layer added no facts")
	}
	for i := range pw.Facts {
		if pw.Facts[i].ID != fw.Facts[i].ID || pw.Facts[i].Atom.Key() != fw.Facts[i].Atom.Key() {
			t.Fatalf("fact %d diverged: %s vs %s", i, pw.Facts[i].ID, fw.Facts[i].ID)
		}
	}
	for _, f := range fw.Facts[len(pw.Facts):] {
		if world.NormFrame(f.FrameID) == world.ActualFrame {
			t.Errorf("frames layer added actual-homed fact %s", f.ID)
		}
	}
	for _, r := range fw.Rules[len(pw.Rules):] {
		if world.NormFrame(r.FrameID) == world.ActualFrame {
			t.Errorf("frames layer added actual-homed rule %s", r.ID)
		}
	}
	for _, s := range fw.Supersessions[len(pw.Supersessions):] {
		if world.NormFrame(s.FrameID) == world.ActualFrame {
			t.Errorf("frames layer added actual-homed supersession %s", s.ID)
		}
	}
}
