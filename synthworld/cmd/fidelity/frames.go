// Frame-assignment fidelity (F-E3, MASTERPLAN §9.6.7; FRAMES-DESIGN-NOTES
// §B.5): the compilation metric that makes frame failures diagnosable.
// Scored against world.json ground truth (SCORER-ONLY access, same notice
// as main.go).
//
// Definitions (registered here before any measured LLM run):
//
//   - The item universe for the confusion matrix is world FACTS matched to
//     committed store facts (by ID when the text carries one, else by
//     content: atom + interval + block). Matched items contribute one cell
//     (true home frame → assigned frame). macro-F1 averages per-frame F1
//     over frames with any true item.
//   - Coverage = matched world facts / world facts; items never committed
//     are ABSTENTIONS (reported as a rate, excluded from F1 — a substrate
//     must not ace fidelity by quarantining everything, so both numbers
//     print together).
//   - fiction→actual leakage = fiction-homed world facts assigned to
//     actual / ALL fiction-homed world facts (the F-E3 <2% gate).
//   - actual→non-actual exile = actual world facts assigned to a
//     non-actual frame / ALL actual world facts.
//   - Sarcasm discipline: a non-assertive event's literal atom committed
//     in ANY frame is a "sarcasm believed" error (correct compilation
//     stores it nowhere).
//   - Frame-table fidelity: declared frames recovered with correct kind /
//     basis / pin day.
package main

import (
	"fmt"
	"sort"

	"github.com/vaudience/synthworld/gen"
	"github.com/vaudience/synthworld/loom"
	"github.com/vaudience/synthworld/world"
)

type framesFidelity struct {
	// Confusion over matched facts: true frame -> assigned frame -> count.
	Confusion map[string]map[string]int `json:"confusion"`
	MacroF1   float64                   `json:"macro_f1"`
	PerFrame  map[string]*frameF1       `json:"per_frame"`

	WorldFacts     int     `json:"world_facts"`
	MatchedFacts   int     `json:"matched_facts"`
	Coverage       float64 `json:"coverage"`
	AbstentionRate float64 `json:"abstention_rate"`

	FictionItems    int     `json:"fiction_items"`
	FictionToActual int     `json:"fiction_to_actual"`
	LeakageRate     float64 `json:"fiction_to_actual_rate"` // F-E3 gate: <2%
	ActualItems     int     `json:"actual_items"`
	ActualExiled    int     `json:"actual_exiled"`
	ExileRate       float64 `json:"actual_exile_rate"`

	SarcasmEvents   int `json:"sarcasm_events"`
	SarcasmBelieved int `json:"sarcasm_believed"` // literal atom committed in any frame

	FramesDeclared  int      `json:"frames_declared"`
	FramesRecovered int      `json:"frames_recovered"`  // present in store with correct kind
	FramesExactMeta int      `json:"frames_exact_meta"` // + correct basis/pin day
	FrameTableNotes []string `json:"frame_table_notes,omitempty"`

	RuleFrameCorrect int `json:"rule_frame_correct"`
	RuleFrameWrong   int `json:"rule_frame_wrong"`
	SupFrameCorrect  int `json:"sup_frame_correct"`
	SupFrameWrong    int `json:"sup_frame_wrong"`
}

type frameF1 struct {
	TP int     `json:"tp"`
	FP int     `json:"fp"`
	FN int     `json:"fn"`
	F1 float64 `json:"f1"`
}

// factMatchKey identifies a fact by content for ID-less matching.
func factMatchKey(f *world.BaseFact) string {
	return fmt.Sprintf("%s|%d|%d|%v", f.Atom.Key(), f.From, f.To, f.Block)
}

func scoreFrames(w *world.World, episodes []gen.Episode, p *loom.Pipeline) *framesFidelity {
	out := &framesFidelity{
		Confusion: map[string]map[string]int{},
		PerFrame:  map[string]*frameF1{},
	}

	// --- fact matching + confusion ---
	storedByID := map[string]*loom.StoredFact{}
	storedByContent := map[string][]*loom.StoredFact{}
	for i := range p.Store.Facts {
		sf := &p.Store.Facts[i]
		if sf.Fact.ID != "" {
			if _, dup := storedByID[sf.Fact.ID]; !dup {
				storedByID[sf.Fact.ID] = sf
			}
		}
		k := factMatchKey(&sf.Fact)
		storedByContent[k] = append(storedByContent[k], sf)
	}
	usedStored := map[*loom.StoredFact]bool{}
	cell := func(truth, got string) {
		if out.Confusion[truth] == nil {
			out.Confusion[truth] = map[string]int{}
		}
		out.Confusion[truth][got]++
	}
	out.WorldFacts = len(w.Facts)
	for i := range w.Facts {
		wf := &w.Facts[i]
		truth := world.NormFrame(wf.FrameID)
		var match *loom.StoredFact
		if sf, ok := storedByID[wf.ID]; ok && !usedStored[sf] {
			match = sf
		} else {
			// content match: prefer an unused stored fact with the same
			// assigned frame (so duplicated atom keys across frames pair
			// correctly), else any unused one.
			cands := storedByContent[factMatchKey(wf)]
			for _, sf := range cands {
				if !usedStored[sf] && world.NormFrame(sf.Fact.FrameID) == truth {
					match = sf
					break
				}
			}
			if match == nil {
				for _, sf := range cands {
					if !usedStored[sf] {
						match = sf
						break
					}
				}
			}
		}
		if truth == world.ActualFrame {
			out.ActualItems++
		}
		fr := w.FrameByID(truth)
		isFiction := fr != nil && fr.Kind == world.FrameFiction
		if isFiction {
			out.FictionItems++
		}
		if match == nil {
			continue // abstention (never committed / dropped / quarantined)
		}
		usedStored[match] = true
		out.MatchedFacts++
		got := world.NormFrame(match.Fact.FrameID)
		cell(truth, got)
		if isFiction && got == world.ActualFrame {
			out.FictionToActual++
		}
		if truth == world.ActualFrame && got != world.ActualFrame {
			out.ActualExiled++
		}
	}
	if out.WorldFacts > 0 {
		out.Coverage = float64(out.MatchedFacts) / float64(out.WorldFacts)
		out.AbstentionRate = 1 - out.Coverage
	}
	if out.FictionItems > 0 {
		out.LeakageRate = float64(out.FictionToActual) / float64(out.FictionItems)
	}
	if out.ActualItems > 0 {
		out.ExileRate = float64(out.ActualExiled) / float64(out.ActualItems)
	}

	// macro-F1 over frames with any true matched item
	frames := map[string]bool{}
	for truth := range out.Confusion {
		frames[truth] = true
	}
	var f1sum float64
	var f1n int
	var frameIDs []string
	for f := range frames {
		frameIDs = append(frameIDs, f)
	}
	sort.Strings(frameIDs)
	for _, f := range frameIDs {
		s := &frameF1{}
		for truth, row := range out.Confusion {
			for got, n := range row {
				switch {
				case truth == f && got == f:
					s.TP += n
				case truth != f && got == f:
					s.FP += n
				case truth == f && got != f:
					s.FN += n
				}
			}
		}
		if s.TP+s.FP+s.FN > 0 {
			s.F1 = 2 * float64(s.TP) / float64(2*s.TP+s.FP+s.FN)
		}
		out.PerFrame[f] = s
		f1sum += s.F1
		f1n++
	}
	if f1n > 0 {
		out.MacroF1 = f1sum / float64(f1n)
	}

	// --- sarcasm discipline ---
	for _, ep := range episodes {
		for _, ev := range ep.Events {
			if ev.Kind != gen.EvFact || ev.Fact == nil || ev.AssertionType != gen.AssertNonAssertive {
				continue
			}
			out.SarcasmEvents++
			for i := range p.Store.Facts {
				if p.Store.Facts[i].Fact.Atom.Key() == ev.Fact.Atom.Key() {
					out.SarcasmBelieved++
					break
				}
			}
		}
	}

	// --- frame table ---
	out.FramesDeclared = len(w.Frames)
	for _, wf := range w.Frames {
		var got *world.Frame
		for i := range p.Store.Frames {
			if p.Store.Frames[i].Frame.ID == wf.ID {
				got = &p.Store.Frames[i].Frame
				break
			}
		}
		if got == nil {
			out.FrameTableNotes = append(out.FrameTableNotes, fmt.Sprintf("frame %s: not recovered", wf.ID))
			continue
		}
		if got.Kind != wf.Kind {
			out.FrameTableNotes = append(out.FrameTableNotes, fmt.Sprintf("frame %s: kind %s, want %s", wf.ID, got.Kind, wf.Kind))
			continue
		}
		out.FramesRecovered++
		wantBasis := wf.Basis
		if wf.Kind == world.FrameScenario && wantBasis == "" {
			wantBasis = world.FrameLive
		}
		gotBasis := got.Basis
		if got.Kind == world.FrameScenario && gotBasis == "" {
			gotBasis = world.FrameLive
		}
		if gotBasis == wantBasis && got.PinDay == wf.PinDay {
			out.FramesExactMeta++
		} else {
			out.FrameTableNotes = append(out.FrameTableNotes, fmt.Sprintf("frame %s: basis/pin %s/%d, want %s/%d", wf.ID, gotBasis, got.PinDay, wantBasis, wf.PinDay))
		}
	}

	// --- rule / supersession frame assignment (matched by ID) ---
	for i := range p.Store.Rules {
		sr := &p.Store.Rules[i]
		for j := range w.Rules {
			if w.Rules[j].ID == sr.Rule.ID {
				if world.NormFrame(w.Rules[j].FrameID) == world.NormFrame(sr.Rule.FrameID) {
					out.RuleFrameCorrect++
				} else {
					out.RuleFrameWrong++
				}
				break
			}
		}
	}
	for i := range p.Store.Supersessions {
		sp := &p.Store.Supersessions[i]
		for j := range w.Supersessions {
			if w.Supersessions[j].ID == sp.Supersession.ID {
				if world.NormFrame(w.Supersessions[j].FrameID) == world.NormFrame(sp.Supersession.FrameID) {
					out.SupFrameCorrect++
				} else {
					out.SupFrameWrong++
				}
				break
			}
		}
	}
	return out
}

func printFramesFidelity(ff *framesFidelity) {
	fmt.Printf("\nframe-assignment fidelity (F-E3):\n")
	fmt.Printf("  macro-F1 %.3f | coverage %.3f (abstention %.3f) | fiction→actual leakage %d/%d (%.3f, gate <0.02) | actual exile %d/%d (%.3f)\n",
		ff.MacroF1, ff.Coverage, ff.AbstentionRate,
		ff.FictionToActual, ff.FictionItems, ff.LeakageRate,
		ff.ActualExiled, ff.ActualItems, ff.ExileRate)
	fmt.Printf("  sarcasm believed %d/%d | frames recovered %d/%d (exact meta %d) | rule frames %d✓/%d✗ | sup frames %d✓/%d✗\n",
		ff.SarcasmBelieved, ff.SarcasmEvents,
		ff.FramesRecovered, ff.FramesDeclared, ff.FramesExactMeta,
		ff.RuleFrameCorrect, ff.RuleFrameWrong, ff.SupFrameCorrect, ff.SupFrameWrong)
	var truths []string
	for t := range ff.Confusion {
		truths = append(truths, t)
	}
	sort.Strings(truths)
	fmt.Println("  confusion (true → assigned: count):")
	for _, t := range truths {
		row := ff.Confusion[t]
		var gots []string
		for g := range row {
			gots = append(gots, g)
		}
		sort.Strings(gots)
		line := fmt.Sprintf("    %-22s", t)
		for _, g := range gots {
			line += fmt.Sprintf(" %s:%d", g, row[g])
		}
		fmt.Println(line)
	}
	for _, n := range ff.FrameTableNotes {
		fmt.Println("  note: " + n)
	}
}
