package gen

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vaudience/synthworld/oracle"
	"github.com/vaudience/synthworld/world"
)

// Query is one evaluation item. System under test sees Text (and optionally
// the structured Atom/Pattern); Answer/StaleAnswer/Trace are for scoring.
type Query struct {
	ID    string `json:"id"`
	Slice string `json:"slice"` // repetition | composition | revision | contamination | isolation | pinning | misattribution | promotion | ideation
	Type  string `json:"type"`  // holds | find | which_frames
	AtDay int    `json:"at_day"`

	Atom     *world.Atom        `json:"atom,omitempty"`    // holds, which_frames
	Pattern  *world.PatternAtom `json:"pattern,omitempty"` // find
	FindSlot string             `json:"find_slot,omitempty"`

	Answer      *bool    `json:"answer,omitempty"`       // holds
	AnswerSet   []string `json:"answer_set,omitempty"`   // find
	StaleAnswer *bool    `json:"stale_answer,omitempty"` // revision only

	// Frames-v1 fields (all empty for v0 queries; MASTERPLAN §9.6.4).
	Frame        string        `json:"frame,omitempty"`         // query frame; "" = actual
	FramesScope  []string      `json:"frames_scope,omitempty"`  // ideation find: explicit frame set
	AnswerFrames []string      `json:"answer_frames,omitempty"` // which_frames: frames where Atom holds
	AnswerFramed []FramedValue `json:"answer_framed,omitempty"` // ideation: (value, frame) pairs
	Subpop       string        `json:"subpop,omitempty"`        // trap sub-population (gap traps are gated separately)

	Depth              int      `json:"depth"`
	ProvenanceEpisodes []string `json:"provenance_episodes,omitempty"`
	Trace              string   `json:"trace,omitempty"`

	Text  string `json:"text"`
	Notes string `json:"notes,omitempty"`

	// CueClass is a SCORING-ONLY, in-memory annotation ("content" |
	// "metadata") computed by cmd/harness for contamination trap/control
	// pairs on frames datasets (F-E2's original lexical-markedness
	// partition, reading (a)). Never persisted; never visible to conditions.
	CueClass string `json:"-"`

	// FilterClass is a SCORING-ONLY, in-memory annotation ("resistant" |
	// "decidable") computed by cmd/harness for holds-type frame-slice
	// queries (F-E2's re-specified FILTERABILITY partition, MASTERPLAN §10
	// 2026-07-18). "resistant" = the correct answer needs closure
	// computation no per-item metadata carries (promotion time-gating,
	// pin-day inheritance, scenario delta overlay / chains, non-assertive
	// sarcasm); "decidable" = frame-membership + cone lookup suffices
	// (fiction/quote contamination + controls, inherited-isolation
	// positives, override-actual controls). Keyed purely on slice+subpop
	// ground truth — independent of any condition's output, so the
	// partition cannot be gamed by the system under test. Never persisted;
	// never visible to conditions.
	FilterClass string `json:"-"`
}

type QuerySet struct {
	AtDay   int     `json:"at_day"`
	Queries []Query `json:"queries"`
}

func bptr(v bool) *bool { return &v }

// BuildQueries synthesizes all slices at t_eval = Horizon. Requires episodes
// to have been built (EpisodeID backrefs set).
func (b *Builder) BuildQueries() (*QuerySet, error) {
	t := b.cfg.Horizon
	cl, err := oracle.Eval(b.w, t, oracle.Options{})
	if err != nil {
		return nil, fmt.Errorf("closure: %w", err)
	}
	stale, err := oracle.Eval(b.w, t, oracle.Options{IgnoreSupersessions: true})
	if err != nil {
		return nil, fmt.Errorf("stale closure: %w", err)
	}

	qs := &QuerySet{AtDay: t}
	qn := 0
	nextID := func() string { qn++; return fmt.Sprintf("qry_%04d", qn) }
	seen := map[string]bool{} // dedupe by atom key + type

	// Firing ratio per derived relation: derived atoms / possible groundings.
	// Relations that hold for most bindings are uninformative as query
	// targets (a yes-to-everything system would score well): exclude > 0.5.
	firing := b.FiringRatios(cl)
	overFiring := func(relID string) bool { return firing[relID] > 0.5 }

	// record dataset-quality stats for the manifest
	b.Stats = DatasetStats{FiringRatios: firing, ClosureDepthCounts: map[string]int{}}
	for _, d := range cl.Atoms {
		b.Stats.ClosureDepthCounts[fmt.Sprintf("d%d", d.Depth)]++
	}
	for relID, ratio := range firing {
		if ratio > 0.5 {
			b.Stats.OverFiringRelations = append(b.Stats.OverFiringRelations, relID)
		}
	}
	sort.Strings(b.Stats.OverFiringRelations)

	addHolds := func(slice string, atom world.Atom, answer bool, staleAns *bool, depth int, prov []string, trace, notes string) {
		k := "holds:" + atom.Key()
		if seen[k] {
			return
		}
		seen[k] = true
		qs.Queries = append(qs.Queries, Query{
			ID: nextID(), Slice: slice, Type: "holds", AtDay: t,
			Atom: &atom, Answer: bptr(answer), StaleAnswer: staleAns,
			Depth: depth, ProvenanceEpisodes: prov, Trace: trace,
			Text:  fmt.Sprintf("On day %d, does %s hold? Answer true or false.", t, b.atomText(atom)),
			Notes: notes,
		})
	}

	// sorted atom keys of the closure, bucketed
	var baseKeys, derivedKeys []string
	for k, d := range cl.Atoms {
		if d.FactID != "" {
			baseKeys = append(baseKeys, k)
		} else {
			derivedKeys = append(derivedKeys, k)
		}
	}
	sort.Strings(baseKeys)
	sort.Strings(derivedKeys)

	// ---------- repetition ----------
	repKeys := append([]string{}, baseKeys...)
	b.rng.Shuffle(len(repKeys), func(i, j int) { repKeys[i], repKeys[j] = repKeys[j], repKeys[i] })
	pos := 0
	for _, k := range repKeys {
		if pos >= b.cfg.NumRepetitionQueries {
			break
		}
		d := cl.Atoms[k]
		prov := oracle.ProvenanceEpisodes(b.w, d)
		addHolds("repetition", d.Atom, true, nil, 0, prov, "- fact "+d.FactID, "stated verbatim in episode")
		// negative control: perturb one argument
		if neg, ok := b.perturb(d.Atom, cl, stale); ok {
			addHolds("repetition", neg, false, nil, 0, nil, "", "negative control: perturbed argument")
		}
		pos++
	}

	// ---------- composition ----------
	// prefer depth >= 2, provenance >= 2 episodes; fill with depth 1 (still
	// cross-episode: rule episode != fact episode)
	type compCand struct {
		key  string
		d    *oracle.Derivation
		prov []string
	}
	var deep, shallow []compCand
	for _, k := range derivedKeys {
		d := cl.Atoms[k]
		if overFiring(d.Atom.Relation) {
			continue
		}
		prov := oracle.ProvenanceEpisodes(b.w, d)
		if len(prov) < 2 {
			continue
		}
		if d.Depth >= 2 {
			deep = append(deep, compCand{k, d, prov})
		} else {
			shallow = append(shallow, compCand{k, d, prov})
		}
	}
	b.rng.Shuffle(len(deep), func(i, j int) { deep[i], deep[j] = deep[j], deep[i] })
	b.rng.Shuffle(len(shallow), func(i, j int) { shallow[i], shallow[j] = shallow[j], shallow[i] })
	comps := append(deep, shallow...)
	pos = 0
	for _, c := range comps {
		if pos >= b.cfg.NumCompositionQueries {
			break
		}
		// skip atoms whose derivation rule is part of a revision pair — those
		// belong to the revision slice
		if b.isRevisionRule(c.d.RuleID) {
			continue
		}
		addHolds("composition", c.d.Atom, true, nil, c.d.Depth, c.prov, oracle.TraceString(c.d),
			fmt.Sprintf("derivation depth %d across %d episodes", c.d.Depth, len(c.prov)))
		if neg, ok := b.perturb(c.d.Atom, cl, stale); ok {
			addHolds("composition", neg, false, nil, c.d.Depth, nil, "", "negative control: perturbed argument")
		}
		pos++
	}

	// ---------- find ----------
	pos = 0
	for _, c := range comps {
		if pos >= b.cfg.NumFindQueries {
			break
		}
		rel := b.w.RelationByID(c.d.Atom.Relation)
		if len(rel.Slots) < 2 {
			continue
		}
		// free the slot with the most satisfiers variety; just pick first slot
		freeSlot := rel.Slots[b.rng.Intn(len(rel.Slots))]
		pattern := world.PatternAtom{Relation: rel.ID, Args: map[string]world.Term{}}
		for _, s := range rel.Slots {
			if s.Name == freeSlot.Name {
				pattern.Args[s.Name] = world.V("X")
			} else {
				pattern.Args[s.Name] = world.C(c.d.Atom.Args[s.Name])
			}
		}
		// answer set from closure
		var answers []string
		for _, k := range derivedKeys {
			d := cl.Atoms[k]
			if d.Atom.Relation != rel.ID {
				continue
			}
			match := true
			for _, s := range rel.Slots {
				if s.Name == freeSlot.Name {
					continue
				}
				if d.Atom.Args[s.Name] != c.d.Atom.Args[s.Name] {
					match = false
					break
				}
			}
			if match {
				answers = append(answers, d.Atom.Args[freeSlot.Name])
			}
		}
		sort.Strings(answers)
		// informative find queries only: non-empty, and well below "all
		// entities of the type satisfy"
		pool := len(b.w.EntitiesOfType(freeSlot.Type))
		maxSat := pool * 2 / 5
		if maxSat < 3 {
			maxSat = 3
		}
		pk := "find:" + b.patternText(pattern)
		if seen[pk] || len(answers) == 0 || len(answers) > maxSat || overFiring(rel.ID) {
			continue
		}
		seen[pk] = true
		qs.Queries = append(qs.Queries, Query{
			ID: nextID(), Slice: "composition", Type: "find", AtDay: t,
			Pattern: &pattern, FindSlot: freeSlot.Name, AnswerSet: answers,
			Depth: c.d.Depth, ProvenanceEpisodes: c.prov,
			Text: fmt.Sprintf("On day %d, list every %s value X such that %s holds. Answer with a list of entity IDs (possibly empty).",
				t, freeSlot.Name, b.patternText(pattern)),
			Notes: fmt.Sprintf("find query over %s, %d satisfiers", rel.Name, len(answers)),
		})
		pos++
	}

	// ---------- revision ----------
	// flips: in stale, not in current, stale derivation rule is a superseded
	// old rule. retained: in both, current derivation rule is a superseding
	// new rule.
	newRules := map[string]bool{}
	for _, nr := range b.SupersededBy {
		newRules[nr] = true
	}
	var flipCands, retainCands []string
	var staleKeys []string
	for k := range stale.Atoms {
		staleKeys = append(staleKeys, k)
	}
	sort.Strings(staleKeys)
	for _, k := range staleKeys {
		sd := stale.Atoms[k]
		if sd.RuleID == "" {
			continue
		}
		if _, isOld := b.SupersededBy[sd.RuleID]; !isOld {
			continue
		}
		if !cl.Holds(sd.Atom) {
			flipCands = append(flipCands, k)
		}
	}
	for _, k := range derivedKeys {
		d := cl.Atoms[k]
		if d.RuleID != "" && newRules[d.RuleID] && stale.Holds(d.Atom) {
			retainCands = append(retainCands, k)
		}
	}
	b.rng.Shuffle(len(flipCands), func(i, j int) { flipCands[i], flipCands[j] = flipCands[j], flipCands[i] })
	b.rng.Shuffle(len(retainCands), func(i, j int) { retainCands[i], retainCands[j] = retainCands[j], retainCands[i] })

	half := b.cfg.NumRevisionQueries / 2
	for i, k := range flipCands {
		if i >= half {
			break
		}
		sd := stale.Atoms[k]
		prov := oracle.ProvenanceEpisodes(b.w, sd)
		prov = append(prov, b.supersessionEpisodes(sd.RuleID)...)
		prov = dedupeSorted(prov)
		addHolds("revision", sd.Atom, false, bptr(true), sd.Depth, prov,
			oracle.TraceString(sd),
			fmt.Sprintf("flip: stale derivation via superseded %s would say true; current answer false", sd.RuleID))
	}
	for i, k := range retainCands {
		if i >= b.cfg.NumRevisionQueries-half {
			break
		}
		d := cl.Atoms[k]
		prov := oracle.ProvenanceEpisodes(b.w, d)
		prov = append(prov, b.supersessionEpisodes(d.RuleID)...)
		prov = dedupeSorted(prov)
		addHolds("revision", d.Atom, true, bptr(true), d.Depth, prov,
			oracle.TraceString(d),
			fmt.Sprintf("retained control: supersession of predecessor does not affect this binding (rule %s)", d.RuleID))
	}

	if b.frames != nil {
		if err := b.buildFrameQueries(qs, nextID, seen, cl); err != nil {
			return nil, fmt.Errorf("frame queries: %w", err)
		}
	}

	return qs, nil
}

func (b *Builder) isRevisionRule(ruleID string) bool {
	if ruleID == "" {
		return false
	}
	if _, old := b.SupersededBy[ruleID]; old {
		return true
	}
	for _, nr := range b.SupersededBy {
		if nr == ruleID {
			return true
		}
	}
	return false
}

// supersessionEpisodes: episodes of supersessions involving ruleID (old or new).
func (b *Builder) supersessionEpisodes(ruleID string) []string {
	var out []string
	for _, s := range b.w.Supersessions {
		if s.OldRule == ruleID || s.NewRule == ruleID {
			out = append(out, s.EpisodeID)
		}
	}
	return out
}

func dedupeSorted(in []string) []string {
	sort.Strings(in)
	out := in[:0]
	var last string
	for _, s := range in {
		if s != last || len(out) == 0 {
			out = append(out, s)
			last = s
		}
	}
	return out
}

// perturb replaces one argument of atom with a different entity of the same
// type such that the result holds in neither the current nor stale closure
// and matches no valid base fact. Returns ok=false if no perturbation found.
func (b *Builder) perturb(atom world.Atom, cl, stale *oracle.Closure) (world.Atom, bool) {
	rel := b.w.RelationByID(atom.Relation)
	slotOrder := make([]int, len(rel.Slots))
	for i := range slotOrder {
		slotOrder[i] = i
	}
	b.rng.Shuffle(len(slotOrder), func(i, j int) { slotOrder[i], slotOrder[j] = slotOrder[j], slotOrder[i] })
	for _, si := range slotOrder {
		slot := rel.Slots[si]
		ents := b.w.EntitiesOfType(slot.Type)
		perm := make([]int, len(ents))
		for i := range perm {
			perm[i] = i
		}
		b.rng.Shuffle(len(perm), func(i, j int) { perm[i], perm[j] = perm[j], perm[i] })
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
			if !cl.Holds(cand) && !stale.Holds(cand) {
				return cand, true
			}
		}
	}
	return world.Atom{}, false
}

// FiringRatios computes, per derived relation, the fraction of possible
// groundings that hold in the closure. High ratios (> ~0.5) mark relations
// whose rules over-fire; queries on them are uninformative.
func (b *Builder) FiringRatios(cl *oracle.Closure) map[string]float64 {
	counts := map[string]int{}
	for _, d := range cl.Atoms {
		if d.RuleID != "" {
			counts[d.Atom.Relation]++
		}
	}
	out := map[string]float64{}
	for i := range b.w.Relations {
		rel := &b.w.Relations[i]
		if rel.Stratum == 0 {
			continue
		}
		possible := 1
		for _, s := range rel.Slots {
			possible *= len(b.w.EntitiesOfType(s.Type))
		}
		if possible == 0 {
			continue
		}
		out[rel.ID] = float64(counts[rel.ID]) / float64(possible)
	}
	return out
}

// QueryStats summarizes a query set for logging.
func QueryStats(qs *QuerySet) string {
	counts := map[string]int{}
	depths := map[int]int{}
	for _, q := range qs.Queries {
		pol := "pos"
		if q.Answer != nil && !*q.Answer {
			pol = "neg"
		}
		if q.Type == "find" {
			pol = fmt.Sprintf("set%d", len(q.AnswerSet))
			if len(q.FramesScope) > 0 {
				pol = fmt.Sprintf("set%d", len(q.AnswerFramed))
			}
		}
		if q.Type == "which_frames" {
			pol = fmt.Sprintf("set%d", len(q.AnswerFrames))
		}
		counts[q.Slice+"/"+q.Type+"/"+pol]++
		if q.Slice == "composition" {
			depths[q.Depth]++
		}
	}
	var keys []string
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&sb, "  %-28s %d\n", k, counts[k])
	}
	var dk []int
	for d := range depths {
		dk = append(dk, d)
	}
	sort.Ints(dk)
	sb.WriteString("  composition depth histogram: ")
	for _, d := range dk {
		fmt.Fprintf(&sb, "d%d:%d ", d, depths[d])
	}
	sb.WriteString("\n")
	return sb.String()
}
