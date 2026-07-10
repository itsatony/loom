// Frame query slices (MASTERPLAN §9.6.4, FRAMES-DESIGN-NOTES §B.3):
// contamination, isolation, pinning, misattribution, promotion, ideation —
// each with paired controls in both directions. Every answer is read off an
// oracle frame closure, never assumed from construction; cmd/validate
// guarantee 5 re-verifies each one independently.
package gen

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vaudience/synthworld/oracle"
	"github.com/vaudience/synthworld/world"
)

// FramedValue is one ideation satisfier: an entity value labeled with the
// frame in which the pattern holds for it.
type FramedValue struct {
	Value string `json:"value"`
	Frame string `json:"frame"`
}

// buildFrameQueries appends the six frame slices to qs. cl is the actual
// closure at t_eval (already computed by BuildQueries).
func (b *Builder) buildFrameQueries(qs *QuerySet, nextID func() string, seen map[string]bool, clActual *oracle.Closure) error {
	fc := b.frames
	t := b.cfg.Horizon

	// closure cache per (frame, day)
	cache := map[string]*oracle.Closure{world.ActualFrame + "@": clActual}
	getCl := func(frame string, day int) (*oracle.Closure, error) {
		frame = world.NormFrame(frame)
		key := frame + "@"
		if day != t {
			key = fmt.Sprintf("%s@%d", frame, day)
		}
		if c, ok := cache[key]; ok {
			return c, nil
		}
		c, err := oracle.Eval(b.w, day, oracle.Options{Frame: frame})
		if err != nil {
			return nil, err
		}
		cache[key] = c
		return c, nil
	}

	holdsKey := func(frame string, day int, atom world.Atom) string {
		frame = world.NormFrame(frame)
		if frame == world.ActualFrame && day == t {
			return "holds:" + atom.Key() // v0 key format: dedupe against v0 slices
		}
		return fmt.Sprintf("holds:%s:%d:%s", frame, day, atom.Key())
	}

	// addHolds emits one holds query in a frame at a day; the answer comes
	// from the oracle. Returns whether it was emitted and what the answer was.
	addHolds := func(slice, frame string, day int, atom world.Atom, subpop, notes string) (emitted, answer bool, err error) {
		k := holdsKey(frame, day, atom)
		if seen[k] {
			return false, false, nil
		}
		cl, err := getCl(frame, day)
		if err != nil {
			return false, false, err
		}
		ans := cl.Holds(atom)
		var depth int
		var prov []string
		var trace string
		if ans {
			d := cl.Get(atom)
			depth = d.Depth
			prov = oracle.ProvenanceEpisodes(b.w, d)
			trace = oracle.TraceString(d)
		}
		nf := world.NormFrame(frame)
		text := fmt.Sprintf("On day %d, does %s hold? Answer true or false.", day, b.atomText(atom))
		if nf != world.ActualFrame {
			text = fmt.Sprintf("On day %d, within frame %s, does %s hold? Answer true or false.", day, nf, b.atomText(atom))
		}
		seen[k] = true
		q := Query{
			ID: nextID(), Slice: slice, Type: "holds", AtDay: day,
			Atom: &atom, Answer: bptr(ans),
			Depth: depth, ProvenanceEpisodes: prov, Trace: trace,
			Text: text, Notes: notes, Subpop: subpop,
		}
		if nf != world.ActualFrame {
			q.Frame = nf
		}
		qs.Queries = append(qs.Queries, q)
		return true, ans, nil
	}

	// addPair emits a trap and its paired control only if both carry the
	// expected polarity (trap false-ish target, control true) — pair
	// integrity per B.3; unpaired traps are dropped.
	frameIDs := b.allFrameIDs()

	// ---------- contamination (queried in actual) ----------
	// paired control for gap-style traps: an actual fact of the same
	// relation, valid at t, not yet used as a control
	controlOfRelation := func(relID string) (world.Atom, bool) {
		for _, f := range b.w.Facts {
			if world.NormFrame(f.FrameID) != world.ActualFrame || f.Atom.Relation != relID || !f.ValidAt(t) {
				continue
			}
			if seen[holdsKey(world.ActualFrame, t, f.Atom)] {
				continue
			}
			if clActual.Holds(f.Atom) {
				return f.Atom, true
			}
		}
		return world.Atom{}, false
	}

	emitContamination := func(traps []frameTrap, limit int, subpop, trapNote string) error {
		made := 0
		for _, tr := range traps {
			if made >= limit {
				break
			}
			var control world.Atom
			var haveCtl bool
			if tr.HasSource {
				if clActual.Holds(tr.Source) && !seen[holdsKey(world.ActualFrame, t, tr.Source)] {
					control, haveCtl = tr.Source, true
				}
			}
			if !haveCtl {
				control, haveCtl = controlOfRelation(tr.Atom.Relation)
			}
			if !haveCtl {
				continue
			}
			emitted, ans, err := addHolds("contamination", world.ActualFrame, t, tr.Atom, subpop,
				fmt.Sprintf("%s (home frame %s)", trapNote, tr.Frame))
			if err != nil {
				return err
			}
			if !emitted {
				continue
			}
			if ans { // trap unexpectedly true in actual: construction bug — fail loudly
				return fmt.Errorf("contamination trap %s holds in actual", tr.Atom.Key())
			}
			if _, _, err := addHolds("contamination", world.ActualFrame, t, control, subpop+"-control",
				"paired control: near-identical atom genuinely true in actual"); err != nil {
				return err
			}
			made++
		}
		return nil
	}
	if err := emitContamination(b.contraFacts, fc.NumContaminationContra, "contradiction",
		"fiction contamination trap: contradicts an actual fact"); err != nil {
		return err
	}
	if err := emitContamination(b.gapFacts, fc.NumContaminationGap, "gap",
		"fiction contamination trap: gap fact, no actual counterpart"); err != nil {
		return err
	}
	speech := append(append([]frameTrap{}, b.sarcasmTraps...), b.quoteFacts...)
	nSar := len(b.sarcasmTraps)
	for i, tr := range speech {
		sub, note := "sarcasm", "speech-act trap: sarcastic literal, non-assertive"
		if i >= nSar {
			sub, note = "quote", "speech-act trap: quoted claim, true only in its perspective frame"
		}
		if err := emitContamination([]frameTrap{tr}, 1, sub, note); err != nil {
			return err
		}
		if i+1 >= fc.NumContaminationSpeech {
			break
		}
	}
	// unresolved predictions also contaminate if believed — covered in the
	// promotion slice (subpop "unresolved"); not duplicated here.

	// ---------- isolation (queried in the scenarios) ----------
	for _, S := range []string{b.frameLive, b.framePinned} {
		clS, err := getCl(S, t)
		if err != nil {
			return err
		}
		// inherited positives: untouched actual base atoms visible in the
		// scenario — punishes "wall off the scenario"
		var inhKeys []string
		for k, d := range clS.Atoms {
			if d.FactID != "" && world.NormFrame(d.Frame) == world.ActualFrame {
				inhKeys = append(inhKeys, k)
			}
		}
		sort.Strings(inhKeys)
		b.rng.Shuffle(len(inhKeys), func(i, j int) { inhKeys[i], inhKeys[j] = inhKeys[j], inhKeys[i] })
		made := 0
		for _, k := range inhKeys {
			if made >= fc.NumIsolationInherited/2 {
				break
			}
			emitted, ans, err := addHolds("isolation", S, t, clS.Atoms[k].Atom, "inherited",
				"inherited actual fact, untouched by scenario deltas")
			if err != nil {
				return err
			}
			if emitted && ans {
				made++
			}
		}
		// composition chains: scenario-only derived atoms whose trace mixes
		// inherited and scenario supports — the §9.6.8 gate counts these
		var chainKeys []string
		for k, d := range clS.Atoms {
			if d.RuleID == "" || clActual.Holds(d.Atom) {
				continue
			}
			fr := map[string]bool{}
			derivationFrames(d, fr)
			if fr[S] && fr[world.ActualFrame] {
				chainKeys = append(chainKeys, k)
			}
		}
		sort.Strings(chainKeys)
		b.rng.Shuffle(len(chainKeys), func(i, j int) { chainKeys[i], chainKeys[j] = chainKeys[j], chainKeys[i] })
		for i, k := range chainKeys {
			if i >= fc.ChainsPerScenario {
				break
			}
			d := clS.Atoms[k]
			if _, _, err := addHolds("isolation", S, t, d.Atom, "chain",
				fmt.Sprintf("scenario composition chain: derivation depth %d mixes inherited facts with scenario deltas", d.Depth)); err != nil {
				return err
			}
			// paired control: the same atom in actual must be false —
			// punishes "answer from actual" merging the delta back
			if _, _, err := addHolds("isolation", world.ActualFrame, t, d.Atom, "chain-control",
				"paired control: the scenario-only conclusion must NOT hold in actual"); err != nil {
				return err
			}
		}
		// override controls: blocked atom false in scenario, replacement
		// true in scenario, original still true in actual
		for _, ov := range b.overrides {
			if ov.Frame != S {
				continue
			}
			if _, _, err := addHolds("isolation", S, t, ov.Orig, "override-blocked",
				"scenario delta blocks this inherited atom"); err != nil {
				return err
			}
			if _, _, err := addHolds("isolation", S, t, ov.Repl, "override-active",
				"scenario delta overrides the inherited value"); err != nil {
				return err
			}
			if _, _, err := addHolds("isolation", world.ActualFrame, t, ov.Orig, "override-actual",
				"paired control: the overridden atom still holds in actual"); err != nil {
				return err
			}
		}
	}

	// ---------- pinning ----------
	// atoms whose pinned/live answers diverge purely through the inherited
	// layer (all trace frames == actual): revision freezes and late reveals
	clPin, err := getCl(b.framePinned, t)
	if err != nil {
		return err
	}
	clLive, err := getCl(b.frameLive, t)
	if err != nil {
		return err
	}
	inheritedOnly := func(d *oracle.Derivation) bool {
		fr := map[string]bool{}
		derivationFrames(d, fr)
		return len(fr) == 1 && fr[world.ActualFrame]
	}
	var pinFrozen, pinLate []string
	for k, d := range clPin.Atoms {
		if !clLive.Holds(d.Atom) && inheritedOnly(d) {
			pinFrozen = append(pinFrozen, k)
		}
	}
	for k, d := range clLive.Atoms {
		if !clPin.Holds(d.Atom) && inheritedOnly(d) {
			pinLate = append(pinLate, k)
		}
	}
	sort.Strings(pinFrozen)
	sort.Strings(pinLate)
	b.rng.Shuffle(len(pinFrozen), func(i, j int) { pinFrozen[i], pinFrozen[j] = pinFrozen[j], pinFrozen[i] })
	b.rng.Shuffle(len(pinLate), func(i, j int) { pinLate[i], pinLate[j] = pinLate[j], pinLate[i] })
	made := 0
	for _, k := range pinFrozen {
		if made >= fc.NumPinning/2 {
			break
		}
		atom := clPin.Atoms[k].Atom
		if _, _, err := addHolds("pinning", b.framePinned, t, atom, "pin-frozen",
			"the pin freezes out a later actual revision: still holds in the pinned scenario"); err != nil {
			return err
		}
		if _, _, err := addHolds("pinning", b.frameLive, t, atom, "pin-frozen-live",
			"paired control: the live scenario tracks the revision — must not hold"); err != nil {
			return err
		}
		made++
	}
	made = 0
	for _, k := range pinLate {
		if made >= fc.NumPinning-fc.NumPinning/2 {
			break
		}
		atom := clLive.Atoms[k].Atom
		if _, _, err := addHolds("pinning", b.frameLive, t, atom, "pin-late",
			"revealed after the pin day: visible to the live scenario"); err != nil {
			return err
		}
		if _, _, err := addHolds("pinning", b.framePinned, t, atom, "pin-late-pinned",
			"paired control: invisible to the pinned scenario"); err != nil {
			return err
		}
		made++
	}

	// ---------- misattribution (which_frames) ----------
	addWhichFrames := func(atom world.Atom, subpop, notes string) error {
		k := "which_frames:" + atom.Key()
		if seen[k] {
			return nil
		}
		var ans []string
		for _, fid := range frameIDs {
			cl, err := getCl(fid, t)
			if err != nil {
				return err
			}
			if cl.Holds(atom) {
				ans = append(ans, fid)
			}
		}
		sort.Strings(ans)
		seen[k] = true
		qs.Queries = append(qs.Queries, Query{
			ID: nextID(), Slice: "misattribution", Type: "which_frames", AtDay: t,
			Atom: &atom, AnswerFrames: ans,
			Text: fmt.Sprintf("On day %d, in which frames does %s hold? Answer with a subset of [%s] (possibly empty).",
				t, b.atomText(atom), strings.Join(frameIDs, ", ")),
			Notes: notes, Subpop: subpop,
		})
		return nil
	}
	var misTargets []struct {
		atom   world.Atom
		subpop string
		notes  string
	}
	for _, topic := range b.contested {
		for _, v := range topic.Variants {
			misTargets = append(misTargets, struct {
				atom   world.Atom
				subpop string
				notes  string
			}{v.Atom, "contested", "unreliable-narrator variant: true only in its perspective frame"})
		}
	}
	for _, ov := range b.overrides {
		misTargets = append(misTargets, struct {
			atom   world.Atom
			subpop string
			notes  string
		}{ov.Repl, "override", "scenario override: true only in its scenario"})
	}
	for i, tr := range b.contraFacts {
		if i >= 4 {
			break
		}
		misTargets = append(misTargets, struct {
			atom   world.Atom
			subpop string
			notes  string
		}{tr.Atom, "fiction", "fiction fact: true only in its fiction frame"})
	}
	// internal pairing: inherited atoms true in actual + both scenarios
	var inhSample []string
	for k, d := range clActual.Atoms {
		if d.FactID != "" {
			inhSample = append(inhSample, k)
		}
	}
	sort.Strings(inhSample)
	b.rng.Shuffle(len(inhSample), func(i, j int) { inhSample[i], inhSample[j] = inhSample[j], inhSample[i] })
	for i, k := range inhSample {
		if i >= 4 {
			break
		}
		misTargets = append(misTargets, struct {
			atom   world.Atom
			subpop string
			notes  string
		}{clActual.Atoms[k].Atom, "actual", "paired control: an actual fact, visible to actual and inheriting scenarios"})
	}
	// The inherited controls are guaranteed slots: they are the only
	// misattribution targets whose truth spans an inheritance chain
	// ([actual, scn_live, scn_pin]) — without them an isolationist store
	// aces the slice and the diagnostic loses its tooth.
	var misPick []int
	reserved := 0
	for i := range misTargets {
		if misTargets[i].subpop == "actual" {
			reserved++
		}
	}
	quota := fc.NumMisattribution - reserved
	for i := range misTargets {
		if misTargets[i].subpop == "actual" {
			misPick = append(misPick, i)
		} else if quota > 0 {
			misPick = append(misPick, i)
			quota--
		}
	}
	for _, i := range misPick {
		mt := misTargets[i]
		if err := addWhichFrames(mt.atom, mt.subpop, mt.notes); err != nil {
			return err
		}
	}

	// ---------- promotion ----------
	for _, p := range b.predictions {
		if p.Confirmed {
			rb := p.ResolveDay - 1
			if rb > 0 {
				if _, _, err := addHolds("promotion", world.ActualFrame, rb, p.Atom, "pre-resolution",
					fmt.Sprintf("confirmed prediction, queried before its resolution day %d: not yet in actual", p.ResolveDay)); err != nil {
					return err
				}
			}
			if _, _, err := addHolds("promotion", world.ActualFrame, t, p.Atom, "post-resolution",
				"confirmed prediction after resolution: holds in actual"); err != nil {
				return err
			}
			if _, _, err := addHolds("promotion", p.Frame, t, p.Atom, "source-frame",
				"paired control: the claim holds in its source frame throughout"); err != nil {
				return err
			}
		} else {
			if _, _, err := addHolds("promotion", world.ActualFrame, t, p.Atom, "unresolved",
				"unresolved prediction: never promoted, must not hold in actual"); err != nil {
				return err
			}
			if _, _, err := addHolds("promotion", p.Frame, t, p.Atom, "source-frame",
				"paired control: the unresolved claim still holds in its source frame"); err != nil {
				return err
			}
		}
	}

	// ---------- cross-frame ideation (find over an explicit frame set) ----------
	matchInFrame := func(cl *oracle.Closure, pattern world.PatternAtom, freeSlot string) []string {
		var keys []string
		for k, d := range cl.Atoms {
			if d.Atom.Relation == pattern.Relation {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		var vals []string
		for _, k := range keys {
			atom := cl.Atoms[k].Atom
			ok := true
			for slot, term := range pattern.Args {
				if term.Const != "" && atom.Args[slot] != term.Const {
					ok = false
					break
				}
			}
			if ok {
				vals = append(vals, atom.Args[freeSlot])
			}
		}
		return vals
	}
	var addIdeation func(pattern world.PatternAtom, freeSlot string, scope []string, notes string) error
	addIdeation = func(pattern world.PatternAtom, freeSlot string, scope []string, notes string) error {
		k := "ideate:" + b.patternText(pattern) + ":" + strings.Join(scope, ",")
		if seen[k] {
			return nil
		}
		var pairs []FramedValue
		framesWith := map[string]bool{}
		for _, fid := range scope {
			cl, err := getCl(fid, t)
			if err != nil {
				return err
			}
			seenVals := map[string]bool{}
			for _, v := range matchInFrame(cl, pattern, freeSlot) {
				if !seenVals[v] {
					seenVals[v] = true
					pairs = append(pairs, FramedValue{Value: v, Frame: fid})
					framesWith[fid] = true
				}
			}
		}
		sort.Slice(pairs, func(i, j int) bool {
			if pairs[i].Frame != pairs[j].Frame {
				return pairs[i].Frame < pairs[j].Frame
			}
			return pairs[i].Value < pairs[j].Value
		})
		isControl := len(scope) == 1
		if !isControl && len(framesWith) < 2 {
			return nil // positives must genuinely cross a frame boundary
		}
		seen[k] = true
		subpop := "cross-frame"
		if isControl {
			subpop = "actual-only-control"
		}
		qs.Queries = append(qs.Queries, Query{
			ID: nextID(), Slice: "ideation", Type: "find", AtDay: t,
			Pattern: &pattern, FindSlot: freeSlot, FramesScope: scope, AnswerFramed: pairs,
			Text: fmt.Sprintf("On day %d, across frames [%s], list every %s value X such that %s holds, labeling each value with the frame it holds in. Answer as (value, frame) pairs (possibly empty).",
				t, strings.Join(scope, ", "), freeSlot, b.patternText(pattern)),
			Notes: notes, Subpop: subpop,
		})
		if !isControl {
			// paired control: same pattern restricted to actual — the answer
			// set must shrink correctly (punishes sterile isolation's inverse)
			return addIdeation(pattern, freeSlot, []string{world.ActualFrame},
				"paired control: same pattern restricted to actual")
		}
		return nil
	}
	type ideaTopic struct {
		pattern  world.PatternAtom
		freeSlot string
		scope    []string
		notes    string
	}
	var topics []ideaTopic
	for _, topic := range b.contested {
		scope := append([]string{world.ActualFrame}, b.perspectiveIDs...)
		topics = append(topics, ideaTopic{topic.Pattern, topic.FreeSlot, scope,
			"contested topic: variants across perspective frames"})
	}
	for _, ov := range b.overrides {
		rel := b.w.RelationByID(ov.Orig.Relation)
		pattern := world.PatternAtom{Relation: rel.ID, Args: map[string]world.Term{}}
		for _, s := range rel.Slots {
			if s.Name == ov.Slot {
				pattern.Args[s.Name] = world.V("X")
			} else {
				pattern.Args[s.Name] = world.C(ov.Orig.Args[s.Name])
			}
		}
		topics = append(topics, ideaTopic{pattern, ov.Slot, []string{world.ActualFrame, ov.Frame},
			"override topic: actual value vs scenario override"})
	}
	for _, tr := range b.contraFacts {
		if !tr.HasSource {
			continue
		}
		rel := b.w.RelationByID(tr.Atom.Relation)
		pattern := world.PatternAtom{Relation: rel.ID, Args: map[string]world.Term{}}
		for _, s := range rel.Slots {
			if s.Name == tr.Slot {
				pattern.Args[s.Name] = world.V("X")
			} else {
				pattern.Args[s.Name] = world.C(tr.Atom.Args[s.Name])
			}
		}
		topics = append(topics, ideaTopic{pattern, tr.Slot, []string{world.ActualFrame, tr.Frame},
			"fiction topic: actual value vs fiction variant"})
	}
	madeIdeation := 0
	for _, tp := range topics {
		if madeIdeation >= fc.NumIdeation {
			break
		}
		before := len(qs.Queries)
		if err := addIdeation(tp.pattern, tp.freeSlot, tp.scope, tp.notes); err != nil {
			return err
		}
		if len(qs.Queries) > before {
			madeIdeation++
		}
	}

	// ---------- per-frame firing hygiene (for the §9.6.8 gate) ----------
	b.FrameStats.PerFrameFiring = map[string]map[string]float64{}
	for _, fid := range frameIDs {
		cl, err := getCl(fid, t)
		if err != nil {
			return err
		}
		b.FrameStats.PerFrameFiring[fid] = b.FiringRatios(cl)
	}
	return nil
}
