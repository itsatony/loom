// Frames layer of the generator — frames-v1 build step 2 (MASTERPLAN §9.6.4,
// FRAMES-DESIGN-NOTES §B.1/B.3). Adds frame-bearing content on top of a
// finished v0 world: fiction frames (contradiction + gap trap facts, tracked
// separately, REUSING actual's vocabulary and entities), one live + one
// pinned planning scenario (deltas = frame-scoped supersessions + block/
// override facts + composition chains mixing inherited facts with scenario
// deltas), perspective frames (unreliable narrators, reported speech,
// predictions with resolution/promotion), sarcasm events (non-assertive,
// stored nowhere), and the six frame query slices with paired controls in
// both directions.
//
// INVARIANT: buildFrames never mutates the actual frame. Every fact/rule/
// supersession it adds homes in a non-actual frame; sarcasm literals exist
// only as episode events. The actual closure — and with it every v0 slice
// and the v0 diagnostics pattern — is bit-identical before and after the
// frames layer (asserted at the end of buildFrames).
//
// All query ground truth comes from oracle.Eval with the query's frame,
// never from construction assumptions; cmd/validate re-verifies it
// independently (guarantee 5).
package gen

import (
	"fmt"
	"sort"

	"github.com/vaudience/synthworld/oracle"
	"github.com/vaudience/synthworld/world"
)

// FramesConfig extends a v0 preset with the frames layer. It is deliberately
// NOT part of Config (Config is embedded in manifest.json; new fields there
// would break byte-identity of v0 datasets). Frames datasets record it under
// the manifest's "frames_config" key instead.
type FramesConfig struct {
	NumFictionFrames      int `json:"num_fiction_frames"`
	ContraFactsPerFiction int `json:"contra_facts_per_fiction"`
	GapFactsPerFiction    int `json:"gap_facts_per_fiction"`

	NumPerspectives      int `json:"num_perspectives"`
	ContestedTopics      int `json:"contested_topics"`
	QuotesPerPerspective int `json:"quotes_per_perspective"`
	SarcasmEvents        int `json:"sarcasm_events"`

	NumPredictions       int `json:"num_predictions"`
	ConfirmedPredictions int `json:"confirmed_predictions"`

	OverridesPerScenario         int `json:"overrides_per_scenario"`
	RuleSupersessionsPerScenario int `json:"rule_supersessions_per_scenario"`
	ChainsPerScenario            int `json:"chains_per_scenario"`

	// Query caps per slice (traps; paired controls are added alongside).
	NumContaminationContra int `json:"num_contamination_contra"`
	NumContaminationGap    int `json:"num_contamination_gap"`
	NumContaminationSpeech int `json:"num_contamination_speech"`
	NumIsolationInherited  int `json:"num_isolation_inherited"`
	NumPinning             int `json:"num_pinning"`
	NumMisattribution      int `json:"num_misattribution"`
	NumIdeation            int `json:"num_ideation"`
}

// DefaultFramesConfig sizes the layer per FRAMES-DESIGN-NOTES §B.9 and the
// §9.6.8 gates (>= 15 gap traps, >= 10 scenario chains, 1 pinned + 1 live
// scenario per seed), with generation headroom above the query caps.
func DefaultFramesConfig() FramesConfig {
	return FramesConfig{
		NumFictionFrames:      2,
		ContraFactsPerFiction: 12,
		GapFactsPerFiction:    12,

		NumPerspectives:      3,
		ContestedTopics:      6,
		QuotesPerPerspective: 2,
		SarcasmEvents:        6,

		NumPredictions:       10,
		ConfirmedPredictions: 6,

		OverridesPerScenario:         4,
		RuleSupersessionsPerScenario: 2,
		ChainsPerScenario:            10,

		NumContaminationContra: 20,
		NumContaminationGap:    20,
		NumContaminationSpeech: 12,
		NumIsolationInherited:  12,
		NumPinning:             16,
		NumMisattribution:      20,
		NumIdeation:            10,
	}
}

// FramesPreset returns the frames extension for the named preset, nil for
// v0 presets.
func FramesPreset(name string) *FramesConfig {
	if name == "frames" {
		fc := DefaultFramesConfig()
		return &fc
	}
	return nil
}

// EnableFrames turns on the frames layer. Must be called before BuildWorld.
func (b *Builder) EnableFrames(fc *FramesConfig) { b.frames = fc }

// ---------- Bookkeeping types ----------

// frameTrap is one frame-homed atom used as contamination/misattribution
// query material, with the actual atom it was derived from (if any) as the
// paired control.
type frameTrap struct {
	FactID    string
	Atom      world.Atom
	Source    world.Atom // actual atom this trap perturbs (contradiction/quote/sarcasm)
	HasSource bool
	Slot      string // perturbed slot (ideation patterns free this slot)
	Frame     string
}

// contestedTopic is one unreliable-narrator dispute: per-perspective
// variants of a common pattern; actual renders no verdict.
type contestedTopic struct {
	Pattern  world.PatternAtom // free slot = FreeSlot
	FreeSlot string
	Variants []frameTrap // one per perspective frame
}

// overrideDelta is one scenario fact override: block the inherited atom,
// assert a replacement.
type overrideDelta struct {
	Frame string
	Orig  world.Atom
	Repl  world.Atom
	Slot  string
}

// prediction is one episode-type-6 item: a claim in its origin's
// perspective frame, optionally resolved by an actual observation and an
// explicit promotion event.
type prediction struct {
	FactID       string
	Atom         world.Atom
	Origin       string // narrator entity ID
	Frame        string
	ClaimDay     int
	ResolveDay   int    // 0 = unresolved at t_eval
	ActualFactID string // the confirming actual observation
	Confirmed    bool
}

// FramesStats lands in the manifest's "frames_quality" section.
type FramesStats struct {
	NumFrames          int                           `json:"num_frames"`
	FictionContraFacts int                           `json:"fiction_contra_facts"`
	FictionGapFacts    int                           `json:"fiction_gap_facts"`
	Overrides          int                           `json:"overrides"`
	ScenarioSupersede  int                           `json:"scenario_supersessions"`
	ChainFactsPlanted  int                           `json:"chain_facts_planted"`
	Predictions        int                           `json:"predictions"`
	PerFrameFiring     map[string]map[string]float64 `json:"per_frame_firing"`
	PinDay             int                           `json:"pin_day"`
	PinnedScenarios    int                           `json:"pinned_scenarios"`
	LiveScenarios      int                           `json:"live_scenarios"`
}

// ---------- Build ----------

// AssertionType values carried on Event (ground truth in easy mode).
// Exported: loom's structured ingest keys its speech-act discipline on them.
const (
	AssertQuote        = "quote"
	AssertNonAssertive = "non-assertive"
)

// buildFrames runs after the v0 world is final (rules repaired, revision
// pairs built, explosions probed). It only appends non-actual content.
func (b *Builder) buildFrames() error {
	fc := b.frames
	h := b.cfg.Horizon
	b.assertKind = map[string]string{}
	b.frameUsed = map[string]bool{}
	b.predictionByFact = map[string]*prediction{}

	// Snapshot of the actual world before the layer, for the invariant check.
	clBefore, err := oracle.Eval(b.w, h, oracle.Options{})
	if err != nil {
		return fmt.Errorf("frames: pre-layer closure: %w", err)
	}

	// Narrators: distinct entities, deterministic pick.
	if len(b.w.Entities) < fc.NumPerspectives {
		return fmt.Errorf("frames: %d entities < %d perspectives", len(b.w.Entities), fc.NumPerspectives)
	}
	perm := b.rng.Perm(len(b.w.Entities))
	for i := 0; i < fc.NumPerspectives; i++ {
		b.narrators = append(b.narrators, b.w.Entities[perm[i]].ID)
	}

	// PinDay: freeze out every v0 revision (earliest supersession - 1) so the
	// pinned scenario still believes the pre-revision rules.
	pinDay := h / 2
	if len(b.w.Supersessions) > 0 {
		earliest := b.w.Supersessions[0].From
		for _, s := range b.w.Supersessions {
			if s.From < earliest {
				earliest = s.From
			}
		}
		if earliest > 1 {
			pinDay = earliest - 1
		}
	}

	// Frame table + creation events.
	for i := 0; i < fc.NumFictionFrames; i++ {
		id := fmt.Sprintf("fic_%02d", i+1)
		day := 3 + b.rng.Intn(h/3)
		b.w.Frames = append(b.w.Frames, world.Frame{ID: id, Kind: world.FrameFiction, CreatedDay: day})
		b.fictionIDs = append(b.fictionIDs, id)
		b.addExtraEvent(day, "frm_"+id, Event{
			Kind: EvFrame, Day: day, Frame: &b.w.Frames[len(b.w.Frames)-1],
			Text: fmt.Sprintf("[day %d] Frame declaration: fiction frame %s opens. Its contents are a story — narrative, not observations.", day, id),
		})
	}
	for i := 0; i < fc.NumPerspectives; i++ {
		id := "psp_" + b.narrators[i]
		day := 3 + b.rng.Intn(h/3)
		b.w.Frames = append(b.w.Frames, world.Frame{ID: id, Kind: world.FramePerspective, CreatedDay: day})
		b.perspectiveIDs = append(b.perspectiveIDs, id)
		b.addExtraEvent(day, "frm_"+id, Event{
			Kind: EvFrame, Day: day, Frame: &b.w.Frames[len(b.w.Frames)-1],
			Text: fmt.Sprintf("[day %d] Frame declaration: perspective frame %s opens for claims attributed to %s.", day, id, b.narrators[i]),
		})
	}
	liveDay := h/3 + b.rng.Intn(h/6)
	b.frameLive = "scn_live"
	b.w.Frames = append(b.w.Frames, world.Frame{ID: b.frameLive, Kind: world.FrameScenario, Basis: world.FrameLive, CreatedDay: liveDay})
	b.addExtraEvent(liveDay, "frm_"+b.frameLive, Event{
		Kind: EvFrame, Day: liveDay, Frame: &b.w.Frames[len(b.w.Frames)-1],
		Text: fmt.Sprintf("[day %d] Frame declaration: planning scenario %s opens, tracking the live actual world.", liveDay, b.frameLive),
	})
	pinCreated := pinDay + 5 + b.rng.Intn(20)
	if pinCreated > h-30 {
		pinCreated = h - 30
	}
	b.framePinned = "scn_pin"
	b.w.Frames = append(b.w.Frames, world.Frame{ID: b.framePinned, Kind: world.FrameScenario, Basis: world.FramePinned, PinDay: pinDay, CreatedDay: pinCreated})
	b.addExtraEvent(pinCreated, "frm_"+b.framePinned, Event{
		Kind: EvFrame, Day: pinCreated, Frame: &b.w.Frames[len(b.w.Frames)-1],
		Text: fmt.Sprintf("[day %d] Frame declaration: planning scenario %s opens, pinned to the actual world as of day %d. Later actual revisions do not enter it.", pinCreated, b.framePinned, pinDay),
	})
	b.FrameStats.PinDay = pinDay

	// Snapshot of actual base facts (frame content must not perturb this list
	// while iterating).
	var actualFacts []world.BaseFact
	for _, f := range b.w.Facts {
		if world.NormFrame(f.FrameID) == world.ActualFrame {
			actualFacts = append(actualFacts, f)
		}
	}

	// --- Fiction frames: contradiction + gap trap facts (episode type 1) ---
	for _, fid := range b.fictionIDs {
		created := b.w.FrameByID(fid).CreatedDay
		src := "manuscript_" + fid
		// contradiction facts: one-slot perturbations of actual facts valid
		// at t_eval — recoverable by conflict detection even without frames
		made := 0
		order := b.rng.Perm(len(actualFacts))
		for _, oi := range order {
			if made >= fc.ContraFactsPerFiction {
				break
			}
			af := actualFacts[oi]
			if !af.ValidAt(h) || b.frameUsed["src:"+af.Atom.Key()] {
				continue
			}
			cand, slot, ok := b.perturbSlot(af.Atom, clBefore)
			if !ok {
				continue
			}
			id := b.addFrameFact(cand, fid, created+b.rng.Intn(h-10-created), 0, false, src)
			b.contraFacts = append(b.contraFacts, frameTrap{FactID: id, Atom: cand, Source: af.Atom, HasSource: true, Slot: slot, Frame: fid})
			b.frameUsed["src:"+af.Atom.Key()] = true
			made++
		}
		// gap facts: plausible atoms about groundings actual never populates
		// (no actual fact within one slot substitution) — ONLY frame
		// assignment keeps them out; the sharp traps
		for made = 0; made < fc.GapFactsPerFiction; {
			atom, ok := b.gapAtom(clBefore)
			if !ok {
				break
			}
			id := b.addFrameFact(atom, fid, created+b.rng.Intn(h-10-created), 0, false, src)
			b.gapFacts = append(b.gapFacts, frameTrap{FactID: id, Atom: atom, Frame: fid})
			made++
		}
	}

	// --- Scenarios: overrides, frame-scoped supersessions, chains (type 2) ---
	clPin, err := oracle.Eval(b.w, pinDay, oracle.Options{})
	if err != nil {
		return fmt.Errorf("frames: closure at pin day: %w", err)
	}
	for _, sc := range []struct {
		frame  string
		base   *oracle.Closure // inherited layer as this scenario sees it
		cutoff int             // source facts must be valid/visible by this day
	}{
		{b.frameLive, clBefore, h - 30},
		{b.framePinned, clPin, pinDay},
	} {
		created := b.w.FrameByID(sc.frame).CreatedDay
		src := "planning_" + sc.frame
		// fact overrides: block the inherited atom + assert a replacement
		made := 0
		order := b.rng.Perm(len(actualFacts))
		for _, oi := range order {
			if made >= fc.OverridesPerScenario {
				break
			}
			af := actualFacts[oi]
			if af.To != 0 || af.From > sc.cutoff || b.frameUsed["src:"+af.Atom.Key()] {
				continue
			}
			repl, slot, ok := b.perturbSlot(af.Atom, clBefore)
			if !ok {
				continue
			}
			day := created + b.rng.Intn(h-10-created)
			b.addFrameFact(af.Atom, sc.frame, day, 0, true, src) // block
			b.addFrameFact(repl, sc.frame, day, 0, false, src)   // override
			b.overrides = append(b.overrides, overrideDelta{Frame: sc.frame, Orig: af.Atom, Repl: repl, Slot: slot})
			b.frameUsed["src:"+af.Atom.Key()] = true
			made++
		}
		// frame-scoped rule supersessions: a narrowed copy of an actual rule,
		// homed in the scenario, supersedes the original within the scenario
		b.scenarioSupersessions(sc.frame, sc.base, created, fc.RuleSupersessionsPerScenario)
		// composition chains: plant ONE missing base condition (homed in the
		// scenario) under a binding whose other conditions already hold in
		// the inherited layer — the conclusion becomes a scenario-only
		// derived atom whose trace mixes inherited and delta supports
		b.plantScenarioChains(sc.frame, sc.base, created, fc.ChainsPerScenario)
	}

	// --- Perspectives: contested topics + quotes (types 3-4) ---
	for made := 0; made < fc.ContestedTopics; {
		topic, ok := b.contestedTopic(clBefore)
		if !ok {
			break
		}
		b.contested = append(b.contested, topic)
		made++
	}
	for i, pid := range b.perspectiveIDs {
		created := b.w.FrameByID(pid).CreatedDay
		for q := 0; q < fc.QuotesPerPerspective; q++ {
			atom, ok := b.gapAtom(clBefore)
			if !ok {
				break
			}
			day := created + b.rng.Intn(h-10-created)
			id := b.addFrameFact(atom, pid, day, 0, false, "narrator_"+b.narrators[i])
			b.assertKind[id] = AssertQuote
			b.quoteFacts = append(b.quoteFacts, frameTrap{FactID: id, Atom: atom, Frame: pid})
		}
	}

	// --- Sarcasm (type 5): literal atom false in actual, non-assertive;
	// exists ONLY as an episode event — no frame stores it ---
	for made := 0; made < fc.SarcasmEvents; {
		oi := b.rng.Intn(len(actualFacts))
		af := actualFacts[oi]
		if !af.ValidAt(h) || b.frameUsed["src:"+af.Atom.Key()] {
			made++ // bounded walk, not a guarantee: sarcasm count is best-effort
			continue
		}
		cand, slot, ok := b.perturbSlot(af.Atom, clBefore)
		if !ok {
			made++
			continue
		}
		day := 10 + b.rng.Intn(h-20)
		speaker := b.narrators[b.rng.Intn(len(b.narrators))]
		sid := fmt.Sprintf("sar_%02d", len(b.sarcasmTraps)+1)
		fact := &world.BaseFact{ID: sid, Atom: cand, From: day, Source: "remark_" + speaker}
		b.addExtraEvent(day, sid, Event{
			Kind: EvFact, Day: day, Fact: fact, AssertionType: AssertNonAssertive,
			Text: fmt.Sprintf("[day %d] Sarcastic remark by %s (non-assertive; the speaker does not believe the literal content): \"Oh sure, %s — obviously.\"", day, speaker, b.atomText(cand)),
		})
		b.sarcasmTraps = append(b.sarcasmTraps, frameTrap{FactID: sid, Atom: cand, Source: af.Atom, HasSource: true, Slot: slot, Frame: world.ActualFrame})
		b.frameUsed["src:"+af.Atom.Key()] = true
		b.frameUsed[cand.Key()] = true
		made++
	}

	// --- Predictions with resolution + promotion (type 6) ---
	b.buildPredictions(actualFacts, clBefore, h, fc)

	// --- Probe every frame closure (join guards must hold per frame) ---
	frameIDs := b.allFrameIDs()
	for _, fid := range frameIDs {
		if _, err := oracle.Eval(b.w, h, oracle.Options{Frame: fid}); err != nil {
			return fmt.Errorf("frames: closure of %s: %w", fid, err)
		}
	}

	// --- Invariant: the actual closure is untouched by the frames layer ---
	clAfter, err := oracle.Eval(b.w, h, oracle.Options{})
	if err != nil {
		return err
	}
	if len(clAfter.Atoms) != len(clBefore.Atoms) {
		return fmt.Errorf("frames layer mutated the actual closure: %d atoms before, %d after", len(clBefore.Atoms), len(clAfter.Atoms))
	}
	for k := range clBefore.Atoms {
		if _, ok := clAfter.Atoms[k]; !ok {
			return fmt.Errorf("frames layer mutated the actual closure: atom %s lost", k)
		}
	}

	b.FrameStats.NumFrames = len(b.w.Frames)
	for _, f := range b.w.Frames {
		if f.Kind == world.FrameScenario {
			if f.Basis == world.FramePinned {
				b.FrameStats.PinnedScenarios++
			} else {
				b.FrameStats.LiveScenarios++
			}
		}
	}
	b.FrameStats.FictionContraFacts = len(b.contraFacts)
	b.FrameStats.FictionGapFacts = len(b.gapFacts)
	b.FrameStats.Overrides = len(b.overrides)
	b.FrameStats.Predictions = len(b.predictions)
	return nil
}

// allFrameIDs returns actual + every declared frame, sorted, actual first.
func (b *Builder) allFrameIDs() []string {
	ids := []string{world.ActualFrame}
	var rest []string
	for _, f := range b.w.Frames {
		rest = append(rest, f.ID)
	}
	sort.Strings(rest)
	return append(ids, rest...)
}

// addFrameFact appends a frame-homed base fact directly (never through
// ensureFact: frame facts may deliberately duplicate actual atom keys —
// blocks and overrides — and must not dedupe against them).
func (b *Builder) addFrameFact(atom world.Atom, frame string, from, to int, block bool, source string) string {
	b.factCounter++
	id := fmt.Sprintf("fct_%04d", b.factCounter)
	b.w.Facts = append(b.w.Facts, world.BaseFact{
		ID: id, Atom: atom, From: from, To: to, Source: source, FrameID: frame, Block: block,
	})
	b.factRevealDay[id] = from
	if !block {
		b.frameUsed[atom.Key()] = true
	}
	return id
}

func (b *Builder) addExtraEvent(day int, key string, ev Event) {
	b.extraEvents = append(b.extraEvents, ev)
	b.extraKeys = append(b.extraKeys, key)
	_ = day
}

// perturbSlot replaces one argument with a different same-typed entity such
// that the result holds in no given closure and is unused by frame content.
// Returns the perturbed slot name.
func (b *Builder) perturbSlot(atom world.Atom, cls ...*oracle.Closure) (world.Atom, string, bool) {
	rel := b.w.RelationByID(atom.Relation)
	slotOrder := b.rng.Perm(len(rel.Slots))
	for _, si := range slotOrder {
		slot := rel.Slots[si]
		ents := b.w.EntitiesOfType(slot.Type)
		perm := b.rng.Perm(len(ents))
		for _, ei := range perm {
			e := ents[ei]
			if e == atom.Args[slot.Name] {
				continue
			}
			cand := world.Atom{Relation: atom.Relation, Args: map[string]string{}}
			for k, v := range atom.Args {
				cand.Args[k] = v
			}
			cand.Args[slot.Name] = e
			if b.frameUsed[cand.Key()] {
				continue
			}
			bad := false
			for _, cl := range cls {
				if cl.Holds(cand) {
					bad = true
					break
				}
			}
			if !bad {
				return cand, slot.Name, true
			}
		}
	}
	return world.Atom{}, "", false
}

// gapAtom samples a base-relation grounding at Hamming distance >= 2 from
// every actual fact of that relation (no near neighbor: only frame
// assignment can keep it out of actual), absent from the closure, unused.
func (b *Builder) gapAtom(cl *oracle.Closure) (world.Atom, bool) {
	var baseRels []*world.RelationSchema
	for i := range b.w.Relations {
		if b.w.Relations[i].Stratum == 0 {
			baseRels = append(baseRels, &b.w.Relations[i])
		}
	}
	for try := 0; try < 200; try++ {
		rel := baseRels[b.rng.Intn(len(baseRels))]
		atom := world.Atom{Relation: rel.ID, Args: map[string]string{}}
		for _, s := range rel.Slots {
			ents := b.w.EntitiesOfType(s.Type)
			atom.Args[s.Name] = ents[b.rng.Intn(len(ents))]
		}
		if b.frameUsed[atom.Key()] || cl.Holds(atom) {
			continue
		}
		near := false
		for _, f := range b.w.Facts {
			if world.NormFrame(f.FrameID) != world.ActualFrame || f.Atom.Relation != rel.ID {
				continue
			}
			diff := 0
			for _, s := range rel.Slots {
				if f.Atom.Args[s.Name] != atom.Args[s.Name] {
					diff++
				}
			}
			if diff <= 1 {
				near = true
				break
			}
		}
		if near {
			continue
		}
		return atom, true
	}
	return world.Atom{}, false
}

// contestedTopic builds one unreliable-narrator dispute: a common pattern
// with one free slot, a distinct variant per perspective frame; no variant
// holds in actual (actual renders no verdict).
func (b *Builder) contestedTopic(cl *oracle.Closure) (contestedTopic, bool) {
	base, ok := b.gapAtom(cl)
	if !ok {
		return contestedTopic{}, false
	}
	rel := b.w.RelationByID(base.Relation)
	slot := rel.Slots[b.rng.Intn(len(rel.Slots))]
	ents := b.w.EntitiesOfType(slot.Type)
	if len(ents) < len(b.perspectiveIDs)+1 {
		return contestedTopic{}, false
	}
	pattern := world.PatternAtom{Relation: rel.ID, Args: map[string]world.Term{}}
	for _, s := range rel.Slots {
		if s.Name == slot.Name {
			pattern.Args[s.Name] = world.V("X")
		} else {
			pattern.Args[s.Name] = world.C(base.Args[s.Name])
		}
	}
	topic := contestedTopic{Pattern: pattern, FreeSlot: slot.Name}
	perm := b.rng.Perm(len(ents))
	h := b.cfg.Horizon
	pi := 0
	for _, ei := range perm {
		if pi >= len(b.perspectiveIDs) {
			break
		}
		variant := world.Atom{Relation: rel.ID, Args: map[string]string{}}
		for k, v := range base.Args {
			variant.Args[k] = v
		}
		variant.Args[slot.Name] = ents[ei]
		if b.frameUsed[variant.Key()] || cl.Holds(variant) {
			continue
		}
		pid := b.perspectiveIDs[pi]
		created := b.w.FrameByID(pid).CreatedDay
		id := b.addFrameFact(variant, pid, created+b.rng.Intn(h-10-created), 0, false, "narrator_"+b.narrators[pi])
		topic.Variants = append(topic.Variants, frameTrap{FactID: id, Atom: variant, Slot: slot.Name, Frame: pid})
		pi++
	}
	if len(topic.Variants) < 2 {
		return contestedTopic{}, false
	}
	return topic, true
}

// scenarioSupersessions narrows n actual rules within the scenario: a copy
// with one added exception, homed in the scenario, supersedes the original
// there. The exception fact for one live binding is planted (scenario-homed)
// so the flip is real inside the scenario.
func (b *Builder) scenarioSupersessions(frame string, base *oracle.Closure, created, n int) {
	var cands []*world.Rule
	for i := range b.w.Rules {
		r := &b.w.Rules[i]
		if world.NormFrame(r.FrameID) != world.ActualFrame || !r.Assert || r.EffectiveTo != 0 || b.isRevisionRule(r.ID) {
			continue
		}
		cands = append(cands, r)
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].ID < cands[j].ID })
	b.rng.Shuffle(len(cands), func(i, j int) { cands[i], cands[j] = cands[j], cands[i] })
	made := 0
	for _, old := range cands {
		if made >= n {
			break
		}
		// need a live firing in the inherited layer to flip
		var deriv *oracle.Derivation
		var keys []string
		for k, d := range base.Atoms {
			if d.RuleID == old.ID {
				keys = append(keys, k)
			}
		}
		if len(keys) == 0 {
			continue
		}
		sort.Strings(keys)
		deriv = base.Atoms[keys[b.rng.Intn(len(keys))]]
		varType := map[string]world.EntityType{}
		for _, c := range old.Conditions {
			rel := b.w.RelationByID(c.Relation)
			for _, s := range rel.Slots {
				if t := c.Args[s.Name]; t.Var != "" {
					varType[t.Var] = s.Type
				}
			}
		}
		exc := b.buildException(varType, old.Conditions)
		if exc == nil {
			continue
		}
		issued := created + b.rng.Intn(20)
		b.ruleCounter++
		newRule := world.Rule{
			ID:            fmt.Sprintf("rul_%03d", b.ruleCounter),
			Name:          old.Name + " (scenario-narrowed)",
			Conditions:    old.Conditions,
			Conclusion:    old.Conclusion,
			Assert:        true,
			Exceptions:    append(append([]world.PatternAtom{}, old.Exceptions...), *exc),
			Authority:     old.Authority,
			IssuedAt:      issued,
			EffectiveFrom: issued,
			FrameID:       frame,
		}
		b.w.Rules = append(b.w.Rules, newRule)
		b.supCounter++
		b.w.Supersessions = append(b.w.Supersessions, world.Supersession{
			ID: fmt.Sprintf("sup_%03d", b.supCounter), NewRule: newRule.ID, OldRule: old.ID,
			From: issued, FrameID: frame,
		})
		b.scenarioSupOld[frame] = append(b.scenarioSupOld[frame], old.ID)
		// plant the exception fact (scenario-homed) under the firing binding
		rel := b.w.RelationByID(exc.Relation)
		atom := world.Atom{Relation: exc.Relation, Args: map[string]string{}}
		ok := true
		for _, s := range rel.Slots {
			t := exc.Args[s.Name]
			switch {
			case t.Const != "":
				atom.Args[s.Name] = t.Const
			case t.Var != "":
				v, bound := deriv.Binding[t.Var]
				if !bound {
					ok = false
				}
				atom.Args[s.Name] = v
			}
		}
		if ok {
			b.addFrameFact(atom, frame, issued+b.rng.Intn(20), 0, false, "planning_"+frame)
		}
		made++
	}
	b.FrameStats.ScenarioSupersede += made
}

// plantScenarioChains plants up to n scenario-homed base facts, each the one
// missing condition of an actual rule under a binding whose remaining
// conditions hold in the inherited layer. The conclusion then derives in the
// scenario only, with a trace mixing inherited and scenario supports.
func (b *Builder) plantScenarioChains(frame string, base *oracle.Closure, created, n int) {
	superseded := map[string]bool{}
	for _, id := range b.scenarioSupOld[frame] {
		superseded[id] = true
	}
	var rules []*world.Rule
	for i := range b.w.Rules {
		r := &b.w.Rules[i]
		if world.NormFrame(r.FrameID) != world.ActualFrame || !r.Assert || b.isRevisionRule(r.ID) || superseded[r.ID] {
			continue
		}
		if !r.EffectiveAt(base.T) {
			continue
		}
		rules = append(rules, r)
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].ID < rules[j].ID })
	b.rng.Shuffle(len(rules), func(i, j int) { rules[i], rules[j] = rules[j], rules[i] })
	h := b.cfg.Horizon
	planted := 0
	for _, r := range rules {
		if planted >= n {
			break
		}
		for i, cond := range r.Conditions {
			crel := b.w.RelationByID(cond.Relation)
			if crel.Stratum != 0 || len(r.Conditions) < 2 {
				continue
			}
			rest := make([]world.PatternAtom, 0, len(r.Conditions)-1)
			for j, c := range r.Conditions {
				if j != i {
					rest = append(rest, c)
				}
			}
			bindings := matchClosurePatterns(base, rest, nil, 20000)
			if len(bindings) == 0 {
				continue
			}
			start := b.rng.Intn(len(bindings))
			found := false
			for k := 0; k < len(bindings) && k < 50; k++ {
				bind := bindings[(start+k)%len(bindings)]
				// ground the missing condition, sampling unbound vars
				atom := world.Atom{Relation: cond.Relation, Args: map[string]string{}}
				full := map[string]string{}
				for kk, vv := range bind {
					full[kk] = vv
				}
				for _, s := range crel.Slots {
					t := cond.Args[s.Name]
					switch {
					case t.Const != "":
						atom.Args[s.Name] = t.Const
					case t.Var != "":
						v, bound := full[t.Var]
						if !bound {
							ents := b.w.EntitiesOfType(s.Type)
							v = ents[b.rng.Intn(len(ents))]
							full[t.Var] = v
						}
						atom.Args[s.Name] = v
					}
				}
				if base.Holds(atom) || b.frameUsed[atom.Key()] {
					continue
				}
				concl, err := groundPattern(r.Conclusion, full)
				if err != nil || base.Holds(concl) {
					continue
				}
				// the rule must actually fire: exceptions must not match
				if len(r.Exceptions) > 0 && len(matchClosurePatterns(base, r.Exceptions, full, 1000)) > 0 {
					continue
				}
				b.addFrameFact(atom, frame, created+b.rng.Intn(h-10-created), 0, false, "planning_"+frame)
				planted++
				found = true
				break
			}
			if found {
				break // one chain per rule, then move on
			}
		}
	}
	b.FrameStats.ChainFactsPlanted += planted
}

// buildPredictions creates episode-type-6 items: confirmed predictions claim
// an atom (in the origin's perspective frame) before the actual observation
// reveals it, then an explicit promotion event fires on the resolution day;
// unresolved predictions claim gap atoms that never resolve.
func (b *Builder) buildPredictions(actualFacts []world.BaseFact, cl *oracle.Closure, h int, fc *FramesConfig) {
	var cands []world.BaseFact
	for _, f := range actualFacts {
		if f.To == 0 && f.From >= h/3 && f.From <= h*5/6 && !b.frameUsed["src:"+f.Atom.Key()] {
			cands = append(cands, f)
		}
	}
	b.rng.Shuffle(len(cands), func(i, j int) { cands[i], cands[j] = cands[j], cands[i] })
	confirmed := 0
	for _, f := range cands {
		if confirmed >= fc.ConfirmedPredictions {
			break
		}
		origin := b.narrators[confirmed%len(b.narrators)]
		pid := "psp_" + origin
		claim := f.From - 20 - b.rng.Intn(40)
		if claim < 5 {
			claim = 5
		}
		id := b.addFrameFact(f.Atom, pid, claim, 0, false, "forecast_"+origin)
		p := prediction{FactID: id, Atom: f.Atom, Origin: origin, Frame: pid,
			ClaimDay: claim, ResolveDay: f.From, ActualFactID: f.ID, Confirmed: true}
		b.predictions = append(b.predictions, p)
		b.predictionByFact[id] = &b.predictions[len(b.predictions)-1]
		b.frameUsed["src:"+f.Atom.Key()] = true
		prm := fmt.Sprintf("prm_%02d", confirmed+1)
		b.addExtraEvent(f.From, prm, Event{
			Kind: EvPromotion, Day: f.From,
			Promotion: &PromotionEvent{PredictionFactID: id, ActualFactID: f.ID, FromFrame: pid, Day: f.From},
			Text: fmt.Sprintf("[day %d] Promotion notice %s: forecast %s by %s is confirmed by observation %s; the claim enters the actual record from day %d.",
				f.From, prm, id, origin, f.ID, f.From),
		})
		confirmed++
	}
	for u := 0; u < fc.NumPredictions-confirmed; u++ {
		atom, ok := b.gapAtom(cl)
		if !ok {
			break
		}
		origin := b.narrators[u%len(b.narrators)]
		pid := "psp_" + origin
		claim := h/3 + b.rng.Intn(h/3)
		id := b.addFrameFact(atom, pid, claim, 0, false, "forecast_"+origin)
		p := prediction{FactID: id, Atom: atom, Origin: origin, Frame: pid, ClaimDay: claim}
		b.predictions = append(b.predictions, p)
		b.predictionByFact[id] = &b.predictions[len(b.predictions)-1]
	}
}

// groundPattern grounds p under bind (like the oracle's ground, local copy —
// the oracle's is unexported).
func groundPattern(p world.PatternAtom, bind map[string]string) (world.Atom, error) {
	a := world.Atom{Relation: p.Relation, Args: map[string]string{}}
	for slot, term := range p.Args {
		switch {
		case term.Const != "":
			a.Args[slot] = term.Const
		case term.Var != "":
			v, ok := bind[term.Var]
			if !ok {
				return a, fmt.Errorf("unbound var %s", term.Var)
			}
			a.Args[slot] = v
		default:
			return a, fmt.Errorf("empty term in slot %s", slot)
		}
	}
	return a, nil
}

// matchClosurePatterns enumerates bindings extending init that satisfy every
// pattern against the closure. Deterministic; returns nil if the enumeration
// exceeds cap (caller skips the candidate — the oracle's own join guard
// still protects evaluation).
func matchClosurePatterns(cl *oracle.Closure, pats []world.PatternAtom, init map[string]string, cap int) []map[string]string {
	bindings := []map[string]string{copyStrMap(init)}
	for _, p := range pats {
		var keys []string
		for k, d := range cl.Atoms {
			if d.Atom.Relation == p.Relation {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		var next []map[string]string
		for _, bnd := range bindings {
			for _, k := range keys {
				atom := cl.Atoms[k].Atom
				nb := copyStrMap(bnd)
				ok := true
				for slot, term := range p.Args {
					val := atom.Args[slot]
					switch {
					case term.Const != "":
						if term.Const != val {
							ok = false
						}
					case term.Var != "":
						if bv, seen := nb[term.Var]; seen {
							if bv != val {
								ok = false
							}
						} else {
							nb[term.Var] = val
						}
					}
					if !ok {
						break
					}
				}
				if ok {
					next = append(next, nb)
					if len(next) > cap {
						return nil
					}
				}
			}
		}
		bindings = next
		if len(bindings) == 0 {
			return nil
		}
	}
	return bindings
}

func copyStrMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// derivationFrames collects the set of home frames appearing anywhere in a
// derivation tree.
func derivationFrames(d *oracle.Derivation, set map[string]bool) {
	if d == nil {
		return
	}
	set[world.NormFrame(d.Frame)] = true
	for _, s := range d.Supports {
		derivationFrames(s, set)
	}
}
