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
type Derivation struct {
	Atom     world.Atom        `json:"atom"`
	FactID   string            `json:"fact_id,omitempty"` // set iff base-fact leaf
	RuleID   string            `json:"rule_id,omitempty"` // set iff rule application
	Binding  map[string]string `json:"binding,omitempty"`
	Supports []*Derivation     `json:"supports,omitempty"`
	Depth    int               `json:"depth"`
}

// Closure is the set of atoms holding at time t, each with one canonical
// (highest-precedence, first-found) derivation.
type Closure struct {
	T     int
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
}

// Eval computes the closure of w at time t.
func Eval(w *world.World, t int, opt Options) (*Closure, error) {
	cl := &Closure{T: t, Atoms: map[string]*Derivation{}, w: w}

	revealed := func(epID string) bool {
		if opt.RevealedBy == nil {
			return true
		}
		day, ok := opt.RevealedBy(epID)
		return ok && day <= t
	}

	// Stratum 0: base facts valid at t.
	for i := range w.Facts {
		f := &w.Facts[i]
		if !f.ValidAt(t) || !revealed(f.EpisodeID) {
			continue
		}
		k := f.Atom.Key()
		if _, dup := cl.Atoms[k]; !dup {
			cl.Atoms[k] = &Derivation{Atom: f.Atom, FactID: f.ID, Depth: 0}
		}
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
				if !r.EffectiveAt(t) || !revealed(r.EpisodeID) {
					continue
				}
				if !opt.IgnoreSupersessions && fullySuperseded(w, r.ID, t, revealed) {
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
						sup, err := conditionallySuperseded(w, cl, r.ID, t, b, revealed)
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
						rule: r, atom: atom, binding: b,
						deriv: &Derivation{Atom: atom, RuleID: r.ID, Binding: b, Supports: supports, Depth: depth + 1},
					})
				}
			}
			added := false
			for key, cands := range candsByAtom {
				if _, exists := cl.Atoms[key]; exists {
					continue // base facts and earlier derivations are not retracted
				}
				sort.Slice(cands, func(i, j int) bool { return precede(cands[i].rule, cands[j].rule) })
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

func fullySuperseded(w *world.World, ruleID string, t int, revealed func(string) bool) bool {
	for i := range w.Supersessions {
		s := &w.Supersessions[i]
		if s.OldRule == ruleID && s.From <= t && len(s.Condition) == 0 && revealed(s.EpisodeID) {
			return true
		}
	}
	return false
}

func conditionallySuperseded(w *world.World, cl *Closure, ruleID string, t int, b map[string]string, revealed func(string) bool) (bool, error) {
	for i := range w.Supersessions {
		s := &w.Supersessions[i]
		if s.OldRule != ruleID || s.From > t || len(s.Condition) == 0 || !revealed(s.EpisodeID) {
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
