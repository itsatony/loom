// S2 store extensions: lifecycle-aware commits and the introspection
// helpers the compilation pipeline's consistency + hygiene stages need.
// All writes still funnel through the S1 commit paths' invariants
// (mandatory provenance, dedupe, cache invalidation).
package loom

import (
	"fmt"

	"github.com/vaudience/synthworld/world"
)

type factConsistency int

const (
	factNew factConsistency = iota
	factDuplicate
	factRefinement
)

// factRelation classifies a candidate fact against the store BEFORE commit:
// exact atom+interval match = duplicate; same atom with a strictly narrower
// validity than an existing open/wider interval = refinement; otherwise new.
func (s *Store) factRelation(f world.BaseFact) factConsistency {
	if s.factKeys[factKey(f)] {
		return factDuplicate
	}
	ak := f.Atom.Key()
	frame := world.NormFrame(f.FrameID)
	for i := range s.Facts {
		ef := &s.Facts[i].Fact
		if ef.Atom.Key() != ak || world.NormFrame(ef.FrameID) != frame || ef.Block != f.Block {
			continue
		}
		if narrower(f.From, f.To, ef.From, ef.To) {
			return factRefinement
		}
	}
	return factNew
}

// narrower reports whether [f1,t1) is strictly inside [f2,t2) (0 = open).
func narrower(f1, t1, f2, t2 int) bool {
	if f1 < f2 {
		return false
	}
	if t2 == 0 {
		return t1 != 0 || f1 > f2
	}
	if t1 == 0 || t1 > t2 {
		return false
	}
	return f1 > f2 || t1 < t2
}

// ruleByID returns the stored rule (any lifecycle) or nil.
func (s *Store) ruleByID(id string) *StoredRule {
	for i := range s.Rules {
		if s.Rules[i].Rule.ID == id {
			return &s.Rules[i]
		}
	}
	return nil
}

// commitRuleWithLifecycle stores a rule in a specific lifecycle state
// (used by the hygiene gate to quarantine on arrival: stored for audit,
// invisible to the evaluator). Same provenance and dedupe invariants as
// CommitRule.
func (s *Store) commitRuleWithLifecycle(r world.Rule, prov Provenance, lc Lifecycle) error {
	if len(prov.EpisodeIDs) == 0 {
		return fmt.Errorf("rule %s: provenance is mandatory", r.ID)
	}
	if s.ruleIDs[r.ID] {
		return nil
	}
	s.ruleIDs[r.ID] = true
	s.Rules = append(s.Rules, StoredRule{Rule: r, Lifecycle: lc, Provenance: prov})
	s.invalidate()
	return nil
}

// setRuleLifecycle transitions a stored rule and invalidates caches.
func (s *Store) setRuleLifecycle(id string, lc Lifecycle) {
	for i := range s.Rules {
		if s.Rules[i].Rule.ID == id {
			if s.Rules[i].Lifecycle != lc {
				s.Rules[i].Lifecycle = lc
				s.invalidate()
			}
			return
		}
	}
}

// maxKnownDay is the latest day mentioned by any stored item — the natural
// evaluation day for post-compile hygiene when no external t is given.
func (s *Store) maxKnownDay() int {
	day := 0
	up := func(d int) {
		if d > day {
			day = d
		}
	}
	for i := range s.Facts {
		up(s.Facts[i].Fact.From)
		up(s.Facts[i].Fact.To)
	}
	for i := range s.Rules {
		up(s.Rules[i].Rule.IssuedAt)
		up(s.Rules[i].Rule.EffectiveFrom)
		up(s.Rules[i].Rule.EffectiveTo)
	}
	for i := range s.Supersessions {
		up(s.Supersessions[i].Supersession.From)
	}
	return day
}

// firingRatios estimates, per active rule, how much of the plausible
// grounding space its conclusion fills: derived atoms attributed to the
// rule / product over conclusion slots of the observed entity-pool size
// for that slot's name family (trailing digits stripped, so "customer2"
// pools with "customer" — the generator names slots after entity types).
// This is the store-side analogue of the generator's firing-ratio hygiene;
// the denominator uses only ingested content, never world.json.
func (s *Store) firingRatios(t int) map[string]float64 {
	c, err := s.closureAt(t, false, world.ActualFrame)
	if err != nil {
		return nil
	}
	// entity pools by slot-name family, from base facts
	pools := map[string]map[string]bool{}
	family := func(slot string) string {
		i := len(slot)
		for i > 0 && slot[i-1] >= '0' && slot[i-1] <= '9' {
			i--
		}
		return slot[:i]
	}
	for i := range s.Facts {
		for slot, ent := range s.Facts[i].Fact.Atom.Args {
			f := family(slot)
			if pools[f] == nil {
				pools[f] = map[string]bool{}
			}
			pools[f][ent] = true
		}
	}
	perRule := map[string]int{}
	for _, d := range c.Atoms {
		if d.RuleID != "" {
			perRule[d.RuleID]++
		}
	}
	out := map[string]float64{}
	for i := range s.Rules {
		sr := &s.Rules[i]
		if sr.Lifecycle != Active {
			continue
		}
		fired := perRule[sr.Rule.ID]
		if fired == 0 {
			continue
		}
		denom := 1
		for slot := range sr.Rule.Conclusion.Args {
			n := len(pools[family(slot)])
			if n == 0 {
				n = 1
			}
			denom *= n
		}
		if denom > 0 {
			out[sr.Rule.ID] = float64(fired) / float64(denom)
		}
	}
	return out
}

// RulesEquivalent compares semantic content (everything except EpisodeID,
// which legitimately differs when the same rule is restated).
func RulesEquivalent(a, b *world.Rule) bool {
	if a.Name != b.Name || a.Authority != b.Authority || a.IssuedAt != b.IssuedAt ||
		a.EffectiveFrom != b.EffectiveFrom || a.EffectiveTo != b.EffectiveTo ||
		a.Assert != b.Assert || world.NormFrame(a.FrameID) != world.NormFrame(b.FrameID) {
		return false
	}
	return patternsEqual(a.Conditions, b.Conditions) &&
		patternsEqual([]world.PatternAtom{a.Conclusion}, []world.PatternAtom{b.Conclusion}) &&
		patternsEqual(a.Exceptions, b.Exceptions)
}

func patternsEqual(a, b []world.PatternAtom) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Relation != b[i].Relation || len(a[i].Args) != len(b[i].Args) {
			return false
		}
		for slot, tm := range a[i].Args {
			if b[i].Args[slot] != tm {
				return false
			}
		}
	}
	return true
}
