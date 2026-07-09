// Package oracle computes the ground-truth closure of a world at a point in
// time, with derivation traces. Semantics per DESIGN.md §2: stratified
// evaluation, per-atom candidate precedence (authority desc, issued_at desc,
// specificity desc, rule ID asc), exceptions and (conditional) supersession
// checked per firing binding.
package oracle

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vaudience/synthworld/world"
)

// Derivation is a proof tree node for a derived atom, or a base-fact leaf.
// Frame is the home frame of the supporting fact/rule (frame-attributed
// traces, MASTERPLAN §9.6.1).
type Derivation struct {
	Atom     world.Atom        `json:"atom"`
	FactID   string            `json:"fact_id,omitempty"` // set iff base-fact leaf
	RuleID   string            `json:"rule_id,omitempty"` // set iff rule application
	Binding  map[string]string `json:"binding,omitempty"`
	Supports []*Derivation     `json:"supports,omitempty"`
	Depth    int               `json:"depth"`
	Frame    string            `json:"frame,omitempty"` // home frame of this fact/rule ("" pre-frames)
}

// Closure is the set of atoms holding at time t in one frame, each with one
// canonical (highest-precedence, first-found) derivation.
type Closure struct {
	T     int
	Frame string                 // query frame ("actual" for v0 worlds)
	Atoms map[string]*Derivation // atom.Key() -> derivation
	w     *world.World
}

func (c *Closure) Holds(a world.Atom) bool {
	_, ok := c.Atoms[a.Key()]
	return ok
}

func (c *Closure) Get(a world.Atom) *Derivation { return c.Atoms[a.Key()] }

// Options controls oracle evaluation.
type Options struct {
	// IgnoreSupersessions computes the "stale" closure: the world as believed
	// by an agent that missed every supersession notice.
	IgnoreSupersessions bool
	// RevealedOnly, if non-nil, restricts facts/rules to those whose
	// revealing episode day is <= t (episodeDay lookup). Used by validators
	// to ensure ground truth never depends on unrevealed knowledge.
	RevealedBy func(episodeID string) (day int, ok bool)
	// Frame selects the query frame; "" means actual. Visibility is the
	// frame's cone (itself + inherited ancestors); items in pinned layers
	// are evaluated at the pin's effective day (world.Cone).
	Frame string
}

// Eval computes the closure of w at time t in opt.Frame (default actual).
func Eval(w *world.World, t int, opt Options) (*Closure, error) {
	frame := world.NormFrame(opt.Frame)
	cone, err := w.Cone(frame, t)
	if err != nil {
		return nil, err
	}
	cl := &Closure{T: t, Frame: frame, Atoms: map[string]*Derivation{}, w: w}

	// revealedAt: episode revealed by the effective day of the item's
	// home frame (a pinned layer must not see later reveals either).
	revealedAt := func(epID string, eff int) bool {
		if opt.RevealedBy == nil {
			return true
		}
		day, ok := opt.RevealedBy(epID)
		return ok && day <= eff
	}

	// Stratum 0: per atom key, the winning candidate among cone-visible
	// facts is the nearest frame's (frame proximity above everything;
	// §9.6.2 decision 1); at equal distance a Block beats an assert
	// (explicit removal wins, like supersession); then fact ID for
	// determinism. A winning Block suppresses the atom entirely.
	type factCand struct {
		f    *world.BaseFact
		dist int
	}
	candsByKey := map[string][]factCand{}
	for i := range w.Facts {
		f := &w.Facts[i]
		cm, vis := cone[world.NormFrame(f.FrameID)]
		if !vis || !f.ValidAt(cm.Eff) || !revealedAt(f.EpisodeID, cm.Eff) {
			continue
		}
		k := f.Atom.Key()
		candsByKey[k] = append(candsByKey[k], factCand{f: f, dist: cm.Dist})
	}
	for k, cands := range candsByKey {
		sort.Slice(cands, func(i, j int) bool {
			if cands[i].dist != cands[j].dist {
				return cands[i].dist < cands[j].dist
			}
			if cands[i].f.Block != cands[j].f.Block {
				return cands[i].f.Block
			}
			return cands[i].f.ID < cands[j].f.ID
		})
		win := cands[0]
		if win.f.Block {
			continue
		}
		cl.Atoms[k] = &Derivation{Atom: win.f.Atom, FactID: win.f.ID, Depth: 0, Frame: world.NormFrame(win.f.FrameID)}
	}

	maxStratum := 0
	for _, r := range w.Relations {
		if r.Stratum > maxStratum {
			maxStratum = r.Stratum
		}
	}

	// Rules grouped by conclusion stratum, in deterministic order.
	rulesByStratum := map[int][]*world.Rule{}
	for i := range w.Rules {
		r := &w.Rules[i]
		rel := w.RelationByID(r.Conclusion.Relation)
		if rel == nil {
			return nil, fmt.Errorf("rule %s: unknown conclusion relation", r.ID)
		}
		rulesByStratum[rel.Stratum] = append(rulesByStratum[rel.Stratum], r)
	}
	for s := range rulesByStratum {
		sort.Slice(rulesByStratum[s], func(i, j int) bool { return rulesByStratum[s][i].ID < rulesByStratum[s][j].ID })
	}

	type candidate struct {
		rule    *world.Rule
		dist    int // frame distance of the rule's home frame from the query frame
		atom    world.Atom
		binding map[string]string
		deriv   *Derivation
	}

	for s := 1; s <= maxStratum; s++ {
		rules := rulesByStratum[s]
		// Fixpoint within stratum (conditions may reference same-stratum
		// conclusions of *other* strata only, per stratification, so a single
		// pass suffices in theory; we still iterate defensively for
		// same-stratum independence and stop when nothing new is added).
		for {
			candsByAtom := map[string][]candidate{}
			for _, r := range rules {
				cm, vis := cone[world.NormFrame(r.FrameID)]
				if !vis || !r.EffectiveAt(cm.Eff) || !revealedAt(r.EpisodeID, cm.Eff) {
					continue
				}
				if !opt.IgnoreSupersessions && fullySuperseded(w, r.ID, cone, revealedAt) {
					continue
				}
				bindings, err := matchAll(cl, r.Conditions, map[string]string{})
				if err != nil {
					return nil, &JoinExplosionError{RuleID: r.ID, Phase: "conditions"}
				}
				for _, b := range bindings {
					exc, err := exceptionApplies(cl, r.Exceptions, b)
					if err != nil {
						return nil, &JoinExplosionError{RuleID: r.ID, Phase: "exceptions"}
					}
					if exc {
						continue
					}
					if !opt.IgnoreSupersessions {
						sup, err := conditionallySuperseded(w, cl, r.ID, b, cone, revealedAt)
						if err != nil {
							return nil, &JoinExplosionError{RuleID: r.ID, Phase: "supersession"}
						}
						if sup {
							continue
						}
					}
					atom, err := ground(r.Conclusion, b)
					if err != nil {
						return nil, fmt.Errorf("rule %s: %w", r.ID, err)
					}
					supports, depth := collectSupports(cl, r.Conditions, b)
					candsByAtom[atom.Key()] = append(candsByAtom[atom.Key()], candidate{
						rule: r, dist: cm.Dist, atom: atom, binding: b,
						deriv: &Derivation{Atom: atom, RuleID: r.ID, Binding: b, Supports: supports, Depth: depth + 1, Frame: world.NormFrame(r.FrameID)},
					})
				}
			}
			added := false
			for key, cands := range candsByAtom {
				if _, exists := cl.Atoms[key]; exists {
					continue // base facts and earlier derivations are not retracted
				}
				sort.Slice(cands, func(i, j int) bool {
					// Frame proximity is the leading precedence key
					// (§9.6.2 decision 1): a nearer frame's rule wins
					// regardless of authority.
					if cands[i].dist != cands[j].dist {
						return cands[i].dist < cands[j].dist
					}
					return precede(cands[i].rule, cands[j].rule)
				})
				win := cands[0]
				if !win.rule.Assert {
					continue // winning candidate blocks the atom
				}
				cl.Atoms[key] = win.deriv
				added = true
			}
			if !added {
				break
			}
		}
	}
	return cl, nil
}

// precede: does rule a take precedence over rule b?
func precede(a, b *world.Rule) bool {
	if a.Authority != b.Authority {
		return a.Authority > b.Authority
	}
	if a.IssuedAt != b.IssuedAt {
		return a.IssuedAt > b.IssuedAt
	}
	if len(a.Conditions) != len(b.Conditions) {
		return len(a.Conditions) > len(b.Conditions)
	}
	return a.ID < b.ID
}

// A supersession applies in the query frame iff its home frame is in the
// cone; it is timed against its home frame's effective day, so a pinned
// layer never sees supersessions issued after the pin. A scenario-frame
// supersession may target an actual rule — that is the delta mechanism —
// and is invisible outside the scenario's cone.
func fullySuperseded(w *world.World, ruleID string, cone map[string]world.ConeMember, revealedAt func(string, int) bool) bool {
	for i := range w.Supersessions {
		s := &w.Supersessions[i]
		if s.OldRule != ruleID || len(s.Condition) != 0 {
			continue
		}
		cm, vis := cone[world.NormFrame(s.FrameID)]
		if vis && s.From <= cm.Eff && revealedAt(s.EpisodeID, cm.Eff) {
			return true
		}
	}
	return false
}

func conditionallySuperseded(w *world.World, cl *Closure, ruleID string, b map[string]string, cone map[string]world.ConeMember, revealedAt func(string, int) bool) (bool, error) {
	for i := range w.Supersessions {
		s := &w.Supersessions[i]
		if s.OldRule != ruleID || len(s.Condition) == 0 {
			continue
		}
		cm, vis := cone[world.NormFrame(s.FrameID)]
		if !vis || s.From > cm.Eff || !revealedAt(s.EpisodeID, cm.Eff) {
			continue
		}
		sat, err := satisfiable(cl, s.Condition, b)
		if err != nil {
			return false, err
		}
		if sat {
			return true, nil
		}
	}
	return false, nil
}

func exceptionApplies(cl *Closure, exceptions []world.PatternAtom, b map[string]string) (bool, error) {
	if len(exceptions) == 0 {
		return false, nil
	}
	return satisfiable(cl, exceptions, b)
}

// maxJoinBindings guards against Cartesian-product blow-ups: the oracle is
// exact and never samples, so an intractable join is a generator bug that
// must fail loudly instead of hanging.
const maxJoinBindings = 200_000

var errJoinExplosion = fmt.Errorf("join exceeded %d bindings (disconnected rule conditions?)", maxJoinBindings)

// JoinExplosionError identifies the rule whose evaluation exceeded the
// binding guard, so generators can tighten it and retry.
type JoinExplosionError struct {
	RuleID string
	Phase  string // "conditions" | "exceptions" | "supersession"
}

func (e *JoinExplosionError) Error() string {
	return fmt.Sprintf("rule %s %s: %v", e.RuleID, e.Phase, errJoinExplosion)
}

// satisfiable: do the patterns match the closure under an extension of b?
func satisfiable(cl *Closure, pats []world.PatternAtom, b map[string]string) (bool, error) {
	m, err := matchAll(cl, pats, b)
	if err != nil {
		return false, err
	}
	return len(m) > 0, nil
}

// matchAll enumerates all bindings extending init that satisfy every pattern
// against the current closure. Deterministic order.
func matchAll(cl *Closure, pats []world.PatternAtom, init map[string]string) ([]map[string]string, error) {
	bindings := []map[string]string{copyBinding(init)}
	for _, p := range pats {
		var next []map[string]string
		for _, b := range bindings {
			next = append(next, matchOne(cl, p, b)...)
			if len(next) > maxJoinBindings {
				return nil, errJoinExplosion
			}
		}
		bindings = next
		if len(bindings) == 0 {
			return nil, nil
		}
	}
	return bindings, nil
}

// matchOne finds all atoms in the closure matching pattern p under binding b,
// returning extended bindings.
func matchOne(cl *Closure, p world.PatternAtom, b map[string]string) []map[string]string {
	var out []map[string]string
	// Deterministic iteration: sort atom keys of the relation.
	keys := make([]string, 0)
	for k, d := range cl.Atoms {
		if d.Atom.Relation == p.Relation {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		atom := cl.Atoms[k].Atom
		nb := copyBinding(b)
		ok := true
		for slot, term := range p.Args {
			val, has := atom.Args[slot]
			if !has {
				ok = false
				break
			}
			switch {
			case term.Const != "":
				if term.Const != val {
					ok = false
				}
			case term.Var != "":
				if bound, seen := nb[term.Var]; seen {
					if bound != val {
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
			out = append(out, nb)
		}
	}
	return out
}

func collectSupports(cl *Closure, conds []world.PatternAtom, b map[string]string) ([]*Derivation, int) {
	var supports []*Derivation
	maxDepth := 0
	for _, c := range conds {
		atom, err := ground(c, b)
		if err != nil {
			continue // conditions may contain vars not in b only if unused; ground requires full binding
		}
		if d := cl.Get(atom); d != nil {
			supports = append(supports, d)
			if d.Depth > maxDepth {
				maxDepth = d.Depth
			}
		}
	}
	return supports, maxDepth
}

func ground(p world.PatternAtom, b map[string]string) (world.Atom, error) {
	a := world.Atom{Relation: p.Relation, Args: map[string]string{}}
	for slot, term := range p.Args {
		switch {
		case term.Const != "":
			a.Args[slot] = term.Const
		case term.Var != "":
			v, ok := b[term.Var]
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

func copyBinding(b map[string]string) map[string]string {
	nb := make(map[string]string, len(b))
	for k, v := range b {
		nb[k] = v
	}
	return nb
}

// ProvenanceEpisodes walks a derivation tree and returns the sorted set of
// episode IDs of all base facts and rules involved.
func ProvenanceEpisodes(w *world.World, d *Derivation) []string {
	set := map[string]bool{}
	var walk func(n *Derivation)
	walk = func(n *Derivation) {
		if n == nil {
			return
		}
		if n.FactID != "" {
			for i := range w.Facts {
				if w.Facts[i].ID == n.FactID {
					set[w.Facts[i].EpisodeID] = true
					break
				}
			}
		}
		if n.RuleID != "" {
			if r := w.RuleByID(n.RuleID); r != nil {
				set[r.EpisodeID] = true
			}
		}
		for _, s := range n.Supports {
			walk(s)
		}
	}
	walk(d)
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TraceString renders a compact human-readable proof.
func TraceString(d *Derivation) string {
	var b strings.Builder
	var walk func(n *Derivation, indent int)
	walk = func(n *Derivation, indent int) {
		pad := strings.Repeat("  ", indent)
		if n.FactID != "" {
			fmt.Fprintf(&b, "%s- fact %s: %s\n", pad, n.FactID, n.Atom.Key())
			return
		}
		fmt.Fprintf(&b, "%s- rule %s ⊢ %s\n", pad, n.RuleID, n.Atom.Key())
		for _, s := range n.Supports {
			walk(s, indent+1)
		}
	}
	walk(d, 0)
	return b.String()
}
