// Package world defines the schema of the synthetic hyper-relational world:
// typed entities, n-ary time-scoped facts, stratified rules with exceptions
// and authority, and supersession records. See DESIGN.md for semantics.
package world

import (
	"fmt"
	"sort"
	"strings"
)

// ---------- Entities & relations ----------

type EntityType string

type Entity struct {
	ID   string     `json:"id"`
	Type EntityType `json:"type"`
	// Name is the human-readable surface form as it appears in episode text
	// (e.g. the dotted symbol path "django.utils.functional.lazy_property"
	// for entity id "sym_django_utils_functional_lazy_property"). Empty when
	// the ID is itself the surface form — every synthetic v0/frames world,
	// where episode text carries the raw entity ID (person_02). omitempty
	// keeps those worlds' JSON byte-identical. Real-domain importers set it
	// so the S2 extractor can be given a symbol catalog for entity grounding.
	Name string `json:"name,omitempty"`
}

type SlotDef struct {
	Name string     `json:"name"`
	Type EntityType `json:"type"`
}

// RelationSchema is an n-ary relation with named, typed slots.
// Stratum 0 relations hold observed base facts only; stratum >= 1 relations
// are derived: populated exclusively by rules whose conditions reference
// strictly lower strata.
type RelationSchema struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Slots   []SlotDef `json:"slots"`
	Stratum int       `json:"stratum"`
}

// ---------- Atoms ----------

// Atom is a ground atom: relation + slotName->entityID.
type Atom struct {
	Relation string            `json:"relation"`
	Args     map[string]string `json:"args"`
}

// Key returns a canonical string identity for the ground atom.
func (a Atom) Key() string {
	names := make([]string, 0, len(a.Args))
	for n := range a.Args {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString(a.Relation)
	b.WriteByte('(')
	for i, n := range names {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%s=%s", n, a.Args[n])
	}
	b.WriteByte(')')
	return b.String()
}

// Term is either a variable or a constant (exactly one set).
type Term struct {
	Var   string `json:"var,omitempty"`
	Const string `json:"const,omitempty"`
}

func V(v string) Term { return Term{Var: v} }
func C(c string) Term { return Term{Const: c} }

// PatternAtom is an atom with variables allowed in argument positions.
type PatternAtom struct {
	Relation string          `json:"relation"`
	Args     map[string]Term `json:"args"`
}

// ---------- Facts, rules, supersession ----------

// BaseFact is an observed ground atom with validity interval [From, To).
// To == 0 means open-ended. FrameID "" means actual. Block, in a non-actual
// frame, suppresses the same atom inherited from farther frames (frame-scoped
// fact removal — the delta mechanism; MASTERPLAN §9.6.2 decision 2).
type BaseFact struct {
	ID        string `json:"id"`
	Atom      Atom   `json:"atom"`
	From      int    `json:"from"`
	To        int    `json:"to"`
	Source    string `json:"source"`
	EpisodeID string `json:"episode_id"`      // episode that revealed it
	FrameID   string `json:"frame,omitempty"` // "" = actual
	Block     bool   `json:"block,omitempty"` // frame-scoped removal of an inherited atom
}

func (f BaseFact) ValidAt(t int) bool {
	return f.From <= t && (f.To == 0 || t < f.To)
}

// Rule: IF Conditions THEN Conclusion, with exceptions, authority, validity.
// Safe: every variable in Conclusion appears in Conditions.
type Rule struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	Conditions    []PatternAtom `json:"conditions"`
	Conclusion    PatternAtom   `json:"conclusion"`
	Assert        bool          `json:"assert"` // true=assert, false=block
	Exceptions    []PatternAtom `json:"exceptions,omitempty"`
	Authority     int           `json:"authority"` // 1..5, higher wins
	IssuedAt      int           `json:"issued_at"`
	EffectiveFrom int           `json:"effective_from"`
	EffectiveTo   int           `json:"effective_to"`    // 0 = open
	EpisodeID     string        `json:"episode_id"`      // episode that revealed it
	FrameID       string        `json:"frame,omitempty"` // "" = actual; rule fires in F iff home ∈ cone(F)
}

func (r Rule) EffectiveAt(t int) bool {
	return r.EffectiveFrom <= t && (r.EffectiveTo == 0 || t < r.EffectiveTo)
}

// Supersession: from day From, OldRule no longer fires. If Condition is
// non-empty, only for bindings where Condition is satisfiable.
type Supersession struct {
	ID        string        `json:"id"`
	NewRule   string        `json:"new_rule"`
	OldRule   string        `json:"old_rule"`
	Condition []PatternAtom `json:"condition,omitempty"`
	From      int           `json:"from"`
	EpisodeID string        `json:"episode_id"`
	FrameID   string        `json:"frame,omitempty"` // "" = actual; applies in F iff home ∈ cone(F)
}

// ---------- World ----------

type World struct {
	Seed          int64            `json:"seed"`
	Horizon       int              `json:"horizon"` // timeline is [0, Horizon]
	Types         []EntityType     `json:"types"`
	Entities      []Entity         `json:"entities"`
	Relations     []RelationSchema `json:"relations"`
	Facts         []BaseFact       `json:"facts"`
	Rules         []Rule           `json:"rules"`
	Supersessions []Supersession   `json:"supersessions"`
	Frames        []Frame          `json:"frames,omitempty"` // empty = v0 world (actual only)
}

func (w *World) RelationByID(id string) *RelationSchema {
	for i := range w.Relations {
		if w.Relations[i].ID == id {
			return &w.Relations[i]
		}
	}
	return nil
}

func (w *World) RuleByID(id string) *Rule {
	for i := range w.Rules {
		if w.Rules[i].ID == id {
			return &w.Rules[i]
		}
	}
	return nil
}

// EntitiesOfType returns entity IDs of the given type, in stable order.
func (w *World) EntitiesOfType(t EntityType) []string {
	var out []string
	for _, e := range w.Entities {
		if e.Type == t {
			out = append(out, e.ID)
		}
	}
	return out
}

// Validate checks structural invariants: typed slots resolve, rules are safe
// and stratified, references resolve.
func (w *World) Validate() error {
	types := map[EntityType]bool{}
	for _, t := range w.Types {
		types[t] = true
	}
	ents := map[string]EntityType{}
	for _, e := range w.Entities {
		if !types[e.Type] {
			return fmt.Errorf("entity %s has unknown type %s", e.ID, e.Type)
		}
		ents[e.ID] = e.Type
	}
	rels := map[string]*RelationSchema{}
	for i := range w.Relations {
		r := &w.Relations[i]
		for _, s := range r.Slots {
			if !types[s.Type] {
				return fmt.Errorf("relation %s slot %s has unknown type %s", r.ID, s.Name, s.Type)
			}
		}
		rels[r.ID] = r
	}
	checkGround := func(a Atom, where string) error {
		rs, ok := rels[a.Relation]
		if !ok {
			return fmt.Errorf("%s: unknown relation %s", where, a.Relation)
		}
		if len(a.Args) != len(rs.Slots) {
			return fmt.Errorf("%s: arity mismatch for %s", where, a.Relation)
		}
		for _, s := range rs.Slots {
			eid, ok := a.Args[s.Name]
			if !ok {
				return fmt.Errorf("%s: missing slot %s of %s", where, s.Name, a.Relation)
			}
			et, ok := ents[eid]
			if !ok {
				return fmt.Errorf("%s: unknown entity %s", where, eid)
			}
			if et != s.Type {
				return fmt.Errorf("%s: entity %s type %s != slot type %s", where, eid, et, s.Type)
			}
		}
		return nil
	}
	for _, f := range w.Facts {
		if err := checkGround(f.Atom, "fact "+f.ID); err != nil {
			return err
		}
		if rels[f.Atom.Relation].Stratum != 0 {
			return fmt.Errorf("fact %s targets derived relation %s", f.ID, f.Atom.Relation)
		}
	}
	for _, r := range w.Rules {
		crel, ok := rels[r.Conclusion.Relation]
		if !ok {
			return fmt.Errorf("rule %s: unknown conclusion relation", r.ID)
		}
		if crel.Stratum < 1 {
			return fmt.Errorf("rule %s concludes into base relation %s", r.ID, crel.ID)
		}
		condVars := map[string]bool{}
		for _, c := range r.Conditions {
			prel, ok := rels[c.Relation]
			if !ok {
				return fmt.Errorf("rule %s: unknown condition relation %s", r.ID, c.Relation)
			}
			if prel.Stratum >= crel.Stratum {
				return fmt.Errorf("rule %s: condition %s stratum %d >= conclusion stratum %d (not stratified)",
					r.ID, c.Relation, prel.Stratum, crel.Stratum)
			}
			for _, tm := range c.Args {
				if tm.Var != "" {
					condVars[tm.Var] = true
				}
			}
		}
		for slot, tm := range r.Conclusion.Args {
			if tm.Var != "" && !condVars[tm.Var] {
				return fmt.Errorf("rule %s: unsafe — conclusion var %s (slot %s) not bound by conditions", r.ID, tm.Var, slot)
			}
		}
		if r.Authority < 1 || r.Authority > 5 {
			return fmt.Errorf("rule %s: authority %d out of range", r.ID, r.Authority)
		}
	}
	for _, s := range w.Supersessions {
		if w.RuleByID(s.OldRule) == nil || w.RuleByID(s.NewRule) == nil {
			return fmt.Errorf("supersession %s references unknown rule", s.ID)
		}
	}
	return w.validateFrames()
}
