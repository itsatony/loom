package loom

import (
	"testing"

	"github.com/vaudience/synthworld/gen"
	"github.com/vaudience/synthworld/world"
)

// tinyVocab is a minimal seeded schema for hand-built candidates.
func tinyVocab() Vocabulary {
	return Vocabulary{Relations: []RelationVocab{
		{ID: "rel_certified", Name: "certified", Slots: []string{"person"}},
	}}
}

// framesStubExtractor returns fixed candidates for a single episode.
type framesStubExtractor struct{ cands []Candidate }

func (s framesStubExtractor) Name() string { return "frames-stub" }
func (s framesStubExtractor) Extract(gen.Episode) ([]Candidate, []string, error) {
	return s.cands, nil, nil
}

func compileStub(t *testing.T, frameNames map[string]string, cands ...Candidate) (*Pipeline, *CompileReport) {
	t.Helper()
	p := NewPipeline(tinyVocab(), framesStubExtractor{cands: cands})
	p.FrameNames = frameNames
	rep, err := p.Compile([]gen.Episode{{ID: "ep_001", Day: 1}})
	if err != nil {
		t.Fatal(err)
	}
	return p, rep
}

func frameFactCand(frame, assertion string, block bool) Candidate {
	return Candidate{Kind: CandFact, Confidence: 1, SourceSpan: "line", Fact: &FactCand{
		FactID: "fct_1", Relation: "certified", Args: map[string]string{"person": "p1"},
		From: 3, Frame: frame, Block: block, Assertion: assertion,
	}}
}

// Non-assertive candidates must be skipped (committed nowhere) and counted.
func TestNonAssertiveSkip(t *testing.T) {
	p, rep := compileStub(t, nil, frameFactCand("", AssertionNonAssertive, false))
	if rep.NonAssertive != 1 {
		t.Fatalf("NonAssertive = %d, want 1", rep.NonAssertive)
	}
	if len(p.Store.Facts) != 0 {
		t.Fatalf("sarcasm literal committed: %+v", p.Store.Facts)
	}
	if rep.Traces[0].Items[0].Verdict != VNonAssertive {
		t.Fatalf("verdict %s, want %s", rep.Traces[0].Items[0].Verdict, VNonAssertive)
	}
}

// Surface handles resolve to canonical frame IDs via FrameNames; the frame
// is auto-registered provisionally when no declaration was extracted.
func TestFrameHandleResolutionAndProvisional(t *testing.T) {
	names := map[string]string{"fic_01": "The Glass Harbor"}
	p, rep := compileStub(t, names, frameFactCand("The Glass Harbor", "", false))
	if len(p.Store.Facts) != 1 {
		t.Fatalf("fact not committed")
	}
	if got := p.Store.Facts[0].Fact.FrameID; got != "fic_01" {
		t.Fatalf("frame = %q, want fic_01", got)
	}
	if rep.Provisional != 1 {
		t.Fatalf("Provisional = %d, want 1", rep.Provisional)
	}
	if len(p.Store.Frames) != 1 || p.Store.Frames[0].Frame.Kind != world.FrameFiction {
		t.Fatalf("provisional frame wrong: %+v", p.Store.Frames)
	}
}

// A declaration arriving after a provisional registration upgrades it.
func TestDeclarationUpgradesProvisional(t *testing.T) {
	names := map[string]string{"scn_pin": "Coldharbor"}
	p, _ := compileStub(t, names,
		frameFactCand("Coldharbor", "", false),
		Candidate{Kind: CandFrame, Confidence: 1, SourceSpan: "decl", Frame: &FrameCand{
			Name: "Coldharbor", Kind: "scenario", Basis: "pinned", PinDay: 120, CreatedDay: 130,
		}},
	)
	if len(p.Store.Frames) != 1 {
		t.Fatalf("want 1 frame, got %d", len(p.Store.Frames))
	}
	f := p.Store.Frames[0].Frame
	if f.ID != "scn_pin" || f.Kind != world.FrameScenario || f.Basis != world.FramePinned || f.PinDay != 120 {
		t.Fatalf("upgraded frame wrong: %+v", f)
	}
}

// A declared frame is immutable: a conflicting re-declaration must not
// overwrite it (caught live: a story-flavored line alias-resolved onto the
// pinned scenario and flipped its kind to fiction).
func TestRedeclarationDoesNotOverwrite(t *testing.T) {
	names := map[string]string{"scn_pin": "Coldharbor"}
	p, rep := compileStub(t, names,
		Candidate{Kind: CandFrame, Confidence: 1, SourceSpan: "decl", Frame: &FrameCand{
			Name: "Coldharbor", Kind: "scenario", Basis: "pinned", PinDay: 120, CreatedDay: 130,
		}},
		Candidate{Kind: CandFrame, Confidence: 1, SourceSpan: "spurious", Frame: &FrameCand{
			Name: "the Coldharbor tale", Kind: "fiction", CreatedDay: 140,
		}},
	)
	if len(p.Store.Frames) != 1 {
		t.Fatalf("want 1 frame, got %d", len(p.Store.Frames))
	}
	f := p.Store.Frames[0].Frame
	if f.Kind != world.FrameScenario || f.Basis != world.FramePinned || f.PinDay != 120 {
		t.Fatalf("declared frame overwritten: %+v", f)
	}
	if rep.Conflicts != 1 {
		t.Fatalf("Conflicts = %d, want 1", rep.Conflicts)
	}
}

// Block facts in actual are schema violations and must be dropped loudly.
func TestBlockInActualDropped(t *testing.T) {
	p, rep := compileStub(t, nil, frameFactCand("", "", true))
	if len(p.Store.Facts) != 0 {
		t.Fatalf("actual block fact committed")
	}
	if rep.Traces[0].Items[0].Verdict != VDropped {
		t.Fatalf("verdict %s, want dropped", rep.Traces[0].Items[0].Verdict)
	}
}

// Quote candidates commit as ordinary assertions homed in the given
// (perspective) frame.
func TestQuoteHomedInFrame(t *testing.T) {
	names := map[string]string{"psp_p7": "p7"}
	p, _ := compileStub(t, names, frameFactCand("p7", AssertionQuote, false))
	if len(p.Store.Facts) != 1 || p.Store.Facts[0].Fact.FrameID != "psp_p7" {
		t.Fatalf("quote not homed in perspective frame: %+v", p.Store.Facts)
	}
}

// FramesDeterministicExtractor round trip on a frames dev world: compiling
// tier-E text must reproduce every world fact/rule/supersession with content
// identity (the property behind loom-c2b-frames-det == frame-oracle).
func TestFramesDetExtractorRoundTrip(t *testing.T) {
	cfg, err := gen.PresetConfig("frames", 99)
	if err != nil {
		t.Fatal(err)
	}
	b := gen.NewBuilder(cfg)
	if fc := gen.FramesPreset("frames"); fc != nil {
		b.EnableFrames(fc)
	}
	if err := b.BuildWorld(); err != nil {
		t.Fatal(err)
	}
	episodes := b.BuildEpisodes()
	w := b.World()

	p := NewPipeline(vocabOf(w), FramesDeterministicExtractor{})
	rep, err := p.Compile(episodes)
	if err != nil {
		t.Fatal(err)
	}
	for _, tr := range rep.Traces {
		for _, prob := range tr.Problems {
			t.Errorf("trace problem: %s", prob)
		}
	}
	// every world fact must exist in the store with atom+interval+frame+block
	type key struct {
		atom, frame string
		from, to    int
		block       bool
	}
	stored := map[key]bool{}
	for _, sf := range p.Store.Facts {
		stored[key{sf.Fact.Atom.Key(), world.NormFrame(sf.Fact.FrameID), sf.Fact.From, sf.Fact.To, sf.Fact.Block}] = true
	}
	for _, f := range w.Facts {
		k := key{f.Atom.Key(), world.NormFrame(f.FrameID), f.From, f.To, f.Block}
		if !stored[k] {
			t.Errorf("world fact %s not reproduced (%+v)", f.ID, k)
		}
	}
	// frame table exact
	for _, wf := range w.Frames {
		found := false
		for _, sf := range p.Store.Frames {
			if sf.Frame.ID == wf.ID {
				found = true
				if sf.Frame.Kind != wf.Kind || sf.Frame.PinDay != wf.PinDay {
					t.Errorf("frame %s: got %+v want %+v", wf.ID, sf.Frame, wf)
				}
			}
		}
		if !found {
			t.Errorf("frame %s not recovered", wf.ID)
		}
	}
	// rules + sups by ID with frame
	for _, wr := range w.Rules {
		sr := p.Store.ruleByID(wr.ID)
		if sr == nil {
			t.Errorf("rule %s not committed", wr.ID)
			continue
		}
		g, want := sr.Rule, wr
		g.EpisodeID, want.EpisodeID = "", ""
		if !RulesEquivalent(&g, &want) {
			t.Errorf("rule %s content mismatch", wr.ID)
		}
	}
	for _, ws := range w.Supersessions {
		found := false
		for _, sp := range p.Store.Supersessions {
			s := sp.Supersession
			if s.ID == ws.ID && s.OldRule == ws.OldRule && s.NewRule == ws.NewRule &&
				s.From == ws.From && world.NormFrame(s.FrameID) == world.NormFrame(ws.FrameID) {
				found = true
			}
		}
		if !found {
			t.Errorf("supersession %s not reproduced", ws.ID)
		}
	}
	if rep.NonAssertive == 0 {
		t.Errorf("expected sarcasm events to be skipped as non-assertive, got 0")
	}
}
