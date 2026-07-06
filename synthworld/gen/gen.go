// Package gen builds seeded synthetic worlds, episode streams, and
// oracle-verified query sets. Determinism: everything flows from Config.Seed;
// map iteration is always over sorted keys.
package gen

import (
	"errors"
	"fmt"
	"math/rand"
	"sort"

	"github.com/vaudience/synthworld/oracle"
	"github.com/vaudience/synthworld/world"
)

// ---------- Config ----------

type Range struct {
	Min, Max int
}

func (r Range) sample(rng *rand.Rand) int {
	if r.Max <= r.Min {
		return r.Min
	}
	return r.Min + rng.Intn(r.Max-r.Min+1)
}

type Config struct {
	Seed    int64 `json:"seed"`
	Horizon int   `json:"horizon"`

	NumEntityTypes  int   `json:"num_entity_types"`
	EntitiesPerType Range `json:"entities_per_type"`

	NumBaseRelations    int `json:"num_base_relations"`
	NumStrata           int `json:"num_strata"` // derived strata 1..NumStrata
	RelationsPerStratum int `json:"relations_per_stratum"`

	RulesPerDerivedRelation Range   `json:"rules_per_derived_relation"`
	ConditionsPerRule       Range   `json:"conditions_per_rule"`
	ExceptionProb           float64 `json:"exception_prob"`

	SeedChainsPerRule              Range `json:"seed_chains_per_rule"`
	BackgroundFactsPerBaseRelation Range `json:"background_facts_per_base_relation"`

	NumRevisionPairs int `json:"num_revision_pairs"`

	EpisodeEvents Range `json:"episode_events"`

	// Query counts per slice (positives; negatives are added ~1:1).
	NumRepetitionQueries  int `json:"num_repetition_queries"`
	NumCompositionQueries int `json:"num_composition_queries"`
	NumFindQueries        int `json:"num_find_queries"`
	NumRevisionQueries    int `json:"num_revision_queries"` // flip+retained pairs
}

func DefaultConfig(seed int64) Config {
	return Config{
		Seed:                           seed,
		Horizon:                        360,
		NumEntityTypes:                 6,
		EntitiesPerType:                Range{8, 16},
		NumBaseRelations:               8,
		NumStrata:                      3,
		RelationsPerStratum:            2,
		RulesPerDerivedRelation:        Range{1, 3},
		ConditionsPerRule:              Range{2, 3},
		ExceptionProb:                  0.35,
		SeedChainsPerRule:              Range{4, 7},
		BackgroundFactsPerBaseRelation: Range{6, 14},
		NumRevisionPairs:               6,
		EpisodeEvents:                  Range{2, 5},
		NumRepetitionQueries:           30,
		NumCompositionQueries:          40,
		NumFindQueries:                 10,
		NumRevisionQueries:             12,
	}
}

// ---------- Vocabulary (compliance-flavored skin) ----------

var typePool = []string{"Customer", "Product", "Sector", "Jurisdiction", "Partner", "Asset", "Facility", "Licence"}

var baseRelPool = []string{
	"registered_in", "operates_in", "classified_as", "offered_in", "supplies",
	"holds", "partnered_with", "located_in", "rated_as", "member_of", "sourced_from", "audited_by",
}

var derivedRelPool = []string{
	"eligible_for", "restricted_in", "requires_review", "approved_for",
	"exempt_from", "flagged_for", "cleared_for", "priority_case",
}

var sourcePool = []string{"registry_A", "registry_B", "field_report", "customer_disclosure", "audit_note", "partner_feed"}

// ---------- Builder ----------

type Builder struct {
	cfg Config
	rng *rand.Rand
	w   *world.World

	factCounter, ruleCounter, supCounter int
	factKeys                             map[string]string // atom key -> fact ID (dedupe)

	// reveal day chosen per object (before episode chunking)
	factRevealDay map[string]int // factID -> day
	ruleRevealDay map[string]int
	supRevealDay  map[string]int

	// revision bookkeeping: oldRuleID -> newRuleID
	SupersededBy map[string]string

	// Stats is populated by BuildQueries for manifest reporting: firing
	// ratios per derived relation and closure depth histogram. A dataset
	// whose deep strata over-fire or whose depth histogram is flat is a bad
	// seed — visible here instead of only on manual inspection.
	Stats DatasetStats
}

// DatasetStats reports generation-quality metrics.
type DatasetStats struct {
	FiringRatios        map[string]float64 `json:"firing_ratios"`         // derived relation -> holds/possible
	ClosureDepthCounts  map[string]int     `json:"closure_depth_counts"`  // "d0".."dN" -> atom count in closure
	OverFiringRelations []string           `json:"over_firing_relations"` // ratio > 0.5, excluded from comp/find queries
}

func NewBuilder(cfg Config) *Builder {
	return &Builder{
		cfg:           cfg,
		rng:           rand.New(rand.NewSource(cfg.Seed)),
		w:             &world.World{Seed: cfg.Seed, Horizon: cfg.Horizon},
		factKeys:      map[string]string{},
		factRevealDay: map[string]int{},
		ruleRevealDay: map[string]int{},
		supRevealDay:  map[string]int{},
		SupersededBy:  map[string]string{},
	}
}

func (b *Builder) World() *world.World { return b.w }

// BuildWorld constructs types, entities, relations, rules, seeded fact
// chains, background facts, then runs the quality repair loop (tighten
// over-firing rules, re-seed dead ones) BEFORE revision pairs are built —
// revision pairs share conditions with their predecessors, so rules must be
// final when pairs are constructed.
func (b *Builder) BuildWorld() error {
	b.buildTypesAndEntities()
	b.buildRelations()
	if err := b.buildRules(); err != nil {
		return err
	}
	b.seedChains()
	b.backgroundFacts()
	if err := b.repairQuality(3); err != nil {
		return err
	}
	b.buildRevisionPairs()
	if err := b.probeExplosions(5); err != nil {
		return err
	}
	return b.w.Validate()
}

func (b *Builder) buildTypesAndEntities() {
	n := b.cfg.NumEntityTypes
	if n > len(typePool) {
		n = len(typePool)
	}
	for i := 0; i < n; i++ {
		t := world.EntityType(typePool[i])
		b.w.Types = append(b.w.Types, t)
		count := b.cfg.EntitiesPerType.sample(b.rng)
		for j := 0; j < count; j++ {
			b.w.Entities = append(b.w.Entities, world.Entity{
				ID:   fmt.Sprintf("%s_%02d", lower(string(t)), j),
				Type: t,
			})
		}
	}
}

func lower(s string) string {
	out := []rune(s)
	if len(out) > 0 && out[0] >= 'A' && out[0] <= 'Z' {
		out[0] = out[0] - 'A' + 'a'
	}
	return string(out)
}

func (b *Builder) buildRelations() {
	// Base relations, stratum 0.
	for i := 0; i < b.cfg.NumBaseRelations; i++ {
		name := baseRelPool[i%len(baseRelPool)]
		if i >= len(baseRelPool) {
			name = fmt.Sprintf("%s_%d", name, i/len(baseRelPool))
		}
		arity := 2
		if b.rng.Float64() < 0.3 {
			arity = 3
		}
		b.w.Relations = append(b.w.Relations, world.RelationSchema{
			ID:      fmt.Sprintf("rel_b%02d_%s", i, name),
			Name:    name,
			Slots:   b.sampleSlots(arity),
			Stratum: 0,
		})
	}
	// Derived relations, strata 1..NumStrata. Slot types are restricted to
	// types actually hosted by lower-stratum relations, otherwise rules
	// concluding the relation could never bind their variables.
	idx := 0
	for s := 1; s <= b.cfg.NumStrata; s++ {
		hosted := map[world.EntityType]bool{}
		for i := range b.w.Relations {
			if b.w.Relations[i].Stratum < s {
				for _, sl := range b.w.Relations[i].Slots {
					hosted[sl.Type] = true
				}
			}
		}
		var allowed []world.EntityType
		for _, t := range b.w.Types {
			if hosted[t] {
				allowed = append(allowed, t)
			}
		}
		for k := 0; k < b.cfg.RelationsPerStratum; k++ {
			name := derivedRelPool[idx%len(derivedRelPool)]
			if idx >= len(derivedRelPool) {
				name = fmt.Sprintf("%s_%d", name, idx/len(derivedRelPool))
			}
			idx++
			arity := 2
			if b.rng.Float64() < 0.25 {
				arity = 3
			}
			b.w.Relations = append(b.w.Relations, world.RelationSchema{
				ID:      fmt.Sprintf("rel_d%d%02d_%s", s, k, name),
				Name:    name,
				Slots:   b.sampleSlotsFrom(arity, allowed),
				Stratum: s,
			})
		}
	}
}

func (b *Builder) sampleSlotsFrom(arity int, allowed []world.EntityType) []world.SlotDef {
	slots := make([]world.SlotDef, 0, arity)
	used := map[string]int{}
	for i := 0; i < arity; i++ {
		t := allowed[b.rng.Intn(len(allowed))]
		name := lower(string(t))
		if c := used[name]; c > 0 {
			used[name]++
			name = fmt.Sprintf("%s%d", name, c+1)
		} else {
			used[name] = 1
		}
		slots = append(slots, world.SlotDef{Name: name, Type: t})
	}
	return slots
}

func (b *Builder) sampleSlots(arity int) []world.SlotDef {
	slots := make([]world.SlotDef, 0, arity)
	used := map[string]int{}
	for i := 0; i < arity; i++ {
		t := b.w.Types[b.rng.Intn(len(b.w.Types))]
		name := lower(string(t))
		if c := used[name]; c > 0 {
			used[name]++
			name = fmt.Sprintf("%s%d", name, c+1)
		} else {
			used[name] = 1
		}
		slots = append(slots, world.SlotDef{Name: name, Type: t})
	}
	return slots
}

// relationsBelowStratum returns relations with stratum < s (sorted by ID).
func (b *Builder) relationsBelowStratum(s int, mustStratum int) []*world.RelationSchema {
	var out []*world.RelationSchema
	for i := range b.w.Relations {
		r := &b.w.Relations[i]
		if mustStratum >= 0 {
			if r.Stratum == mustStratum {
				out = append(out, r)
			}
		} else if r.Stratum < s {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ---------- Rules ----------

func (b *Builder) buildRules() error {
	varNames := []string{"A", "B", "C", "D", "E", "F", "G", "H"}
	for i := range b.w.Relations {
		rel := &b.w.Relations[i]
		if rel.Stratum == 0 {
			continue
		}
		n := b.cfg.RulesPerDerivedRelation.sample(b.rng)
		for k := 0; k < n; k++ {
			rule, err := b.buildRule(rel, varNames, k)
			if err != nil {
				return err
			}
			b.w.Rules = append(b.w.Rules, *rule)
		}
	}
	return nil
}

func (b *Builder) buildRule(concl *world.RelationSchema, varNames []string, ordinal int) (*world.Rule, error) {
	varIdx := 0
	freshVar := func() string {
		v := varNames[varIdx%len(varNames)]
		if varIdx >= len(varNames) {
			v = fmt.Sprintf("%s%d", v, varIdx/len(varNames))
		}
		varIdx++
		return v
	}
	varType := map[string]world.EntityType{}

	// Conclusion: fresh var per slot.
	conclArgs := map[string]world.Term{}
	var conclVars []string
	for _, s := range concl.Slots {
		v := freshVar()
		varType[v] = s.Type
		conclArgs[s.Name] = world.V(v)
		conclVars = append(conclVars, v)
	}

	unbound := map[string]bool{}
	for _, v := range conclVars {
		unbound[v] = true
	}
	// vars that have appeared in already-built conditions (connectivity check)
	seenCondVars := map[string]bool{}

	nCond := b.cfg.ConditionsPerRule.sample(b.rng)
	var conds []world.PatternAtom

	// For strata >= 2, force at least one condition on the stratum directly
	// below, so real multi-hop depth exists in the world.
	forceLower := concl.Stratum >= 2

	for c := 0; c < nCond || len(unbound) > 0; c++ {
		if c > nCond+4 { // safety: add hosting conditions but never loop forever
			break
		}
		var pool []*world.RelationSchema
		if forceLower && c == 0 {
			pool = b.relationsBelowStratum(concl.Stratum, concl.Stratum-1)
		} else {
			pool = b.relationsBelowStratum(concl.Stratum, 0) // mostly base
			if b.rng.Float64() < 0.3 {
				pool = b.relationsBelowStratum(concl.Stratum, -1) // any lower
			}
		}
		if len(pool) == 0 {
			pool = b.relationsBelowStratum(concl.Stratum, -1)
		}
		// If unbound conclusion vars remain, prefer a relation that can host one.
		var target string
		for _, v := range conclVars {
			if unbound[v] {
				target = v
				break
			}
		}
		var rel *world.RelationSchema
		if target != "" {
			tt := varType[target]
			var hosting []*world.RelationSchema
			for _, r := range pool {
				for _, s := range r.Slots {
					if s.Type == tt {
						hosting = append(hosting, r)
						break
					}
				}
			}
			if len(hosting) == 0 {
				// widen to any lower-stratum relation hosting the type
				for _, r := range b.relationsBelowStratum(concl.Stratum, -1) {
					for _, s := range r.Slots {
						if s.Type == tt {
							hosting = append(hosting, r)
							break
						}
					}
				}
			}
			if len(hosting) == 0 {
				return nil, fmt.Errorf("no relation below stratum %d hosts type %s", concl.Stratum, tt)
			}
			rel = hosting[b.rng.Intn(len(hosting))]
		} else {
			rel = pool[b.rng.Intn(len(pool))]
		}

		args := map[string]world.Term{}
		hostedTarget := false
		var hostedSlot string
		for _, s := range rel.Slots {
			// priority: host the targeted unbound conclusion var once
			if target != "" && !hostedTarget && varType[target] == s.Type {
				args[s.Name] = world.V(target)
				delete(unbound, target)
				hostedTarget = true
				hostedSlot = s.Name
				continue
			}
			// chain to an existing var of matching type — high probability:
			// shared variables create joint constraints across conditions,
			// which is the structural defense against over-firing rules
			if b.rng.Float64() < 0.65 {
				var cands []string
				for v, vt := range varType {
					if vt == s.Type {
						cands = append(cands, v)
					}
				}
				sort.Strings(cands)
				if len(cands) > 0 {
					v := cands[b.rng.Intn(len(cands))]
					args[s.Name] = world.V(v)
					delete(unbound, v)
					continue
				}
			}
			// constant sometimes
			if b.rng.Float64() < 0.2 {
				ents := b.w.EntitiesOfType(s.Type)
				args[s.Name] = world.C(ents[b.rng.Intn(len(ents))])
				continue
			}
			v := freshVar()
			varType[v] = s.Type
			args[s.Name] = world.V(v)
		}
		// Connectivity: a condition sharing no variable with the previous
		// conditions turns the oracle's join into a Cartesian product
		// (semantically a rule about unlinked things, computationally a
		// blow-up). Force at least one slot to reuse a var already seen in
		// earlier conditions; unbound conclusion vars don't count — they
		// haven't appeared in the fact-side join yet.
		if len(conds) > 0 {
			connected := false
			for _, t := range args {
				if t.Var != "" && seenCondVars[t.Var] {
					connected = true
					break
				}
			}
			if !connected {
				slotNames := make([]string, 0, len(rel.Slots))
				for _, s := range rel.Slots {
					slotNames = append(slotNames, s.Name)
				}
				b.rng.Shuffle(len(slotNames), func(i, j int) { slotNames[i], slotNames[j] = slotNames[j], slotNames[i] })
			rewire:
				for _, sn := range slotNames {
					if sn == hostedSlot {
						continue // never displace the conclusion var being hosted
					}
					var st world.EntityType
					for _, s := range rel.Slots {
						if s.Name == sn {
							st = s.Type
							break
						}
					}
					var cands []string
					for v := range seenCondVars {
						if varType[v] == st {
							cands = append(cands, v)
						}
					}
					sort.Strings(cands)
					if len(cands) > 0 {
						args[sn] = world.V(cands[b.rng.Intn(len(cands))])
						connected = true
						break rewire
					}
				}
				// if no type-compatible rewiring exists, the condition stays
				// disconnected; the oracle's binding guard fails loudly if
				// this ever produces an intractable join
			}
		}
		for _, t := range args {
			if t.Var != "" {
				seenCondVars[t.Var] = true
			}
		}
		conds = append(conds, world.PatternAtom{Relation: rel.ID, Args: args})
		if c >= nCond-1 && len(unbound) == 0 {
			break
		}
	}
	if len(unbound) > 0 {
		return nil, fmt.Errorf("rule for %s: could not bind conclusion vars %v", concl.ID, unbound)
	}

	// Exception: pattern over a base relation reusing one condition var.
	var exceptions []world.PatternAtom
	if b.rng.Float64() < b.cfg.ExceptionProb {
		if exc := b.buildException(varType, conds); exc != nil {
			exceptions = append(exceptions, *exc)
		}
	}

	issued := b.rng.Intn(b.cfg.Horizon / 2)
	effTo := 0
	if b.rng.Float64() < 0.12 { // some rules expire before evaluation: temporal distractors
		effTo = issued + 30 + b.rng.Intn(b.cfg.Horizon/3)
	}
	b.ruleCounter++
	return &world.Rule{
		ID:            fmt.Sprintf("rul_%03d", b.ruleCounter),
		Name:          fmt.Sprintf("%s policy %d", concl.Name, ordinal+1),
		Conditions:    conds,
		Conclusion:    world.PatternAtom{Relation: concl.ID, Args: conclArgs},
		Assert:        true,
		Exceptions:    exceptions,
		Authority:     1 + b.rng.Intn(5),
		IssuedAt:      issued,
		EffectiveFrom: issued,
		EffectiveTo:   effTo,
	}, nil
}

func (b *Builder) buildException(varType map[string]world.EntityType, conds []world.PatternAtom) *world.PatternAtom {
	// pick a var used in conditions
	var vars []string
	for v := range varType {
		vars = append(vars, v)
	}
	sort.Strings(vars)
	if len(vars) == 0 {
		return nil
	}
	v := vars[b.rng.Intn(len(vars))]
	vt := varType[v]
	// find base relation hosting vt
	var cands []*world.RelationSchema
	for i := range b.w.Relations {
		r := &b.w.Relations[i]
		if r.Stratum != 0 {
			continue
		}
		for _, s := range r.Slots {
			if s.Type == vt {
				cands = append(cands, r)
				break
			}
		}
	}
	if len(cands) == 0 {
		return nil
	}
	rel := cands[b.rng.Intn(len(cands))]
	args := map[string]world.Term{}
	hosted := false
	for _, s := range rel.Slots {
		if !hosted && s.Type == vt {
			args[s.Name] = world.V(v)
			hosted = true
			continue
		}
		// exceptions ground remaining slots with constants: crisp, checkable
		ents := b.w.EntitiesOfType(s.Type)
		args[s.Name] = world.C(ents[b.rng.Intn(len(ents))])
	}
	return &world.PatternAtom{Relation: rel.ID, Args: args}
}

// ---------- Chain seeding ----------

// seedChains materializes base-fact support for sampled rule bindings so the
// closure is non-trivially populated, recursing through derived conditions.
func (b *Builder) seedChains() {
	rules := make([]*world.Rule, 0, len(b.w.Rules))
	for i := range b.w.Rules {
		rules = append(rules, &b.w.Rules[i])
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].ID < rules[j].ID })
	for _, r := range rules {
		n := b.cfg.SeedChainsPerRule.sample(b.rng)
		// deep rules (stratum >= 2 conclusions) get extra chains: multi-hop
		// coverage is the scarce resource in the composition slice
		if rel := b.w.RelationByID(r.Conclusion.Relation); rel != nil && rel.Stratum >= 2 {
			n += 3
		}
		for k := 0; k < n; k++ {
			b.seedRule(r, map[string]string{}, 0)
		}
	}
}

func (b *Builder) seedRule(r *world.Rule, binding map[string]string, depth int) {
	if depth > 4 {
		return
	}
	// complete the binding for all vars in conditions
	bind := map[string]string{}
	for k, v := range binding {
		bind[k] = v
	}
	for _, c := range r.Conditions {
		rel := b.w.RelationByID(c.Relation)
		for _, s := range rel.Slots {
			term := c.Args[s.Name]
			if term.Var != "" {
				if _, ok := bind[term.Var]; !ok {
					ents := b.w.EntitiesOfType(s.Type)
					bind[term.Var] = ents[b.rng.Intn(len(ents))]
				}
			}
		}
	}
	// materialize each condition
	for _, c := range r.Conditions {
		rel := b.w.RelationByID(c.Relation)
		atom := world.Atom{Relation: c.Relation, Args: map[string]string{}}
		for _, s := range rel.Slots {
			term := c.Args[s.Name]
			if term.Const != "" {
				atom.Args[s.Name] = term.Const
			} else {
				atom.Args[s.Name] = bind[term.Var]
			}
		}
		if rel.Stratum == 0 {
			b.ensureFact(atom, 0, r.EffectiveFrom)
		} else {
			// find rules concluding this relation; try to seed one whose
			// conclusion unifies with atom
			var cands []*world.Rule
			for i := range b.w.Rules {
				rr := &b.w.Rules[i]
				if rr.Conclusion.Relation == rel.ID && rr.ID != r.ID && rr.EffectiveTo == 0 {
					cands = append(cands, rr)
				}
			}
			sort.Slice(cands, func(i, j int) bool { return cands[i].ID < cands[j].ID })
			b.rng.Shuffle(len(cands), func(i, j int) { cands[i], cands[j] = cands[j], cands[i] })
			// try candidates until one unifies with the required ground atom
			for _, sub := range cands {
				subBind := map[string]string{}
				ok := true
				for slot, term := range sub.Conclusion.Args {
					want := atom.Args[slot]
					if term.Const != "" {
						if term.Const != want {
							ok = false
							break
						}
					} else {
						subBind[term.Var] = want
					}
				}
				if ok {
					b.seedRule(sub, subBind, depth+1)
					break
				}
			}
		}
	}
}

// ensureFact adds a base fact for atom if absent. Reveal/validity day is
// sampled in [minFrom, maxFrom] so chains scatter across the timeline.
func (b *Builder) ensureFact(atom world.Atom, minFrom, maxFrom int) string {
	key := atom.Key()
	if id, ok := b.factKeys[key]; ok {
		return id
	}
	if maxFrom <= minFrom {
		maxFrom = minFrom + 1
	}
	from := minFrom + b.rng.Intn(maxFrom-minFrom)
	b.factCounter++
	id := fmt.Sprintf("fct_%04d", b.factCounter)
	b.w.Facts = append(b.w.Facts, world.BaseFact{
		ID:     id,
		Atom:   atom,
		From:   from,
		To:     0,
		Source: sourcePool[b.rng.Intn(len(sourcePool))],
	})
	b.factKeys[key] = id
	b.factRevealDay[id] = from
	return id
}

// ---------- Revision pairs ----------

// buildRevisionPairs creates (old, new) rule pairs: new = old + one more
// exception, issued later, with a total supersession. For at least one seeded
// binding the new exception matches (answer flips at t_eval); others retain.
func (b *Builder) buildRevisionPairs() {
	// candidates: assert rules with open validity and >= 1 condition
	var cands []*world.Rule
	for i := range b.w.Rules {
		r := &b.w.Rules[i]
		if r.Assert && r.EffectiveTo == 0 && len(r.Conditions) > 0 {
			if _, done := b.SupersededBy[r.ID]; !done {
				cands = append(cands, r)
			}
		}
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].ID < cands[j].ID })
	b.rng.Shuffle(len(cands), func(i, j int) { cands[i], cands[j] = cands[j], cands[i] })

	made := 0
	for _, old := range cands {
		if made >= b.cfg.NumRevisionPairs {
			break
		}
		// collect var types of old
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
		issued := b.cfg.Horizon/2 + b.rng.Intn(b.cfg.Horizon/3)
		b.ruleCounter++
		newRule := world.Rule{
			ID:            fmt.Sprintf("rul_%03d", b.ruleCounter),
			Name:          old.Name + " (revised)",
			Conditions:    old.Conditions,
			Conclusion:    old.Conclusion,
			Assert:        true,
			Exceptions:    append(append([]world.PatternAtom{}, old.Exceptions...), *exc),
			Authority:     old.Authority,
			IssuedAt:      issued,
			EffectiveFrom: issued,
			EffectiveTo:   0,
		}
		b.w.Rules = append(b.w.Rules, newRule)
		b.supCounter++
		b.w.Supersessions = append(b.w.Supersessions, world.Supersession{
			ID:      fmt.Sprintf("sup_%03d", b.supCounter),
			NewRule: newRule.ID,
			OldRule: old.ID,
			From:    issued,
		})
		b.SupersededBy[old.ID] = newRule.ID

		// Materialize the flip: seed a binding for old, then add a base fact
		// making the new exception match under that binding.
		b.seedFlipBinding(old, exc, issued)
		made++
	}
}

func (b *Builder) seedFlipBinding(old *world.Rule, exc *world.PatternAtom, issued int) {
	// choose entities for all condition vars
	bind := map[string]string{}
	for _, c := range old.Conditions {
		rel := b.w.RelationByID(c.Relation)
		for _, s := range rel.Slots {
			if t := c.Args[s.Name]; t.Var != "" {
				if _, ok := bind[t.Var]; !ok {
					ents := b.w.EntitiesOfType(s.Type)
					bind[t.Var] = ents[b.rng.Intn(len(ents))]
				}
			}
		}
	}
	b.seedRule(old, bind, 0)
	// exception fact: ground exc under bind (its non-var slots are constants)
	rel := b.w.RelationByID(exc.Relation)
	atom := world.Atom{Relation: exc.Relation, Args: map[string]string{}}
	for _, s := range rel.Slots {
		t := exc.Args[s.Name]
		if t.Const != "" {
			atom.Args[s.Name] = t.Const
		} else {
			v, ok := bind[t.Var]
			if !ok {
				ents := b.w.EntitiesOfType(s.Type)
				v = ents[b.rng.Intn(len(ents))]
			}
			atom.Args[s.Name] = v
		}
	}
	// reveal the exception fact around the supersession day (it is the "news")
	b.ensureFact(atom, issued-20, issued+10)
}

// ---------- Background facts ----------

func (b *Builder) backgroundFacts() {
	for i := range b.w.Relations {
		rel := &b.w.Relations[i]
		if rel.Stratum != 0 {
			continue
		}
		n := b.cfg.BackgroundFactsPerBaseRelation.sample(b.rng)
		for k := 0; k < n; k++ {
			atom := world.Atom{Relation: rel.ID, Args: map[string]string{}}
			for _, s := range rel.Slots {
				ents := b.w.EntitiesOfType(s.Type)
				atom.Args[s.Name] = ents[b.rng.Intn(len(ents))]
			}
			if _, exists := b.factKeys[atom.Key()]; exists {
				continue
			}
			from := b.rng.Intn(b.cfg.Horizon - 10)
			to := 0
			if b.rng.Float64() < 0.15 { // temporal distractors: expire pre-eval
				to = from + 20 + b.rng.Intn(b.cfg.Horizon/2)
				if to >= b.cfg.Horizon {
					to = 0
				}
			}
			b.factCounter++
			id := fmt.Sprintf("fct_%04d", b.factCounter)
			b.w.Facts = append(b.w.Facts, world.BaseFact{
				ID: id, Atom: atom, From: from, To: to,
				Source: sourcePool[b.rng.Intn(len(sourcePool))],
			})
			b.factKeys[atom.Key()] = id
			b.factRevealDay[id] = from
		}
	}
}

// ---------- Quality repair loop ----------

// repairQuality measures firing ratios against the oracle and repairs two
// failure modes, in up to maxRounds rounds:
//   - over-firing relations (ratio > 0.5): tighten each concluding rule by
//     grounding its "hub" variable — the non-conclusion variable joining the
//     most conditions — to a constant, cutting the firing scope by roughly
//     the size of that entity pool; then re-seed so the tightened rule still
//     fires somewhere.
//   - dead relations (ratio == 0): re-seed their rules.
//
// Non-convergence after maxRounds is not an error: residual over-firing
// relations are excluded from queries and reported in DatasetStats.
func (b *Builder) repairQuality(maxRounds int) error {
	for round := 0; round < maxRounds; round++ {
		cl, err := oracle.Eval(b.w, b.cfg.Horizon, oracle.Options{})
		if err != nil {
			// A join explosion is a repairable signal, not a fatal error:
			// tighten the offending rule and spend the round on it.
			var jee *oracle.JoinExplosionError
			if errors.As(err, &jee) && b.tightenRuleByID(jee.RuleID) {
				continue
			}
			return fmt.Errorf("repair round %d closure: %w", round, err)
		}
		ratios := b.FiringRatios(cl)
		var relIDs []string
		for id := range ratios {
			relIDs = append(relIDs, id)
		}
		sort.Strings(relIDs)

		repaired := false
		for _, relID := range relIDs {
			switch {
			case ratios[relID] > 0.5:
				var ids []string
				for i := range b.w.Rules {
					if b.w.Rules[i].Conclusion.Relation == relID {
						ids = append(ids, b.w.Rules[i].ID)
					}
				}
				for _, id := range ids {
					if b.tightenRuleByID(id) {
						repaired = true
					}
				}
			case ratios[relID] == 0:
				for i := range b.w.Rules {
					r := &b.w.Rules[i]
					if r.Conclusion.Relation != relID {
						continue
					}
					b.seedRule(r, map[string]string{}, 0)
					b.seedRule(r, map[string]string{}, 0)
					repaired = true
				}
			}
		}
		if !repaired {
			return nil
		}
	}
	return nil
}

// probeExplosions runs after revision pairs are built (which add facts and
// rules): if the final closure explodes, tighten the offending rule — and
// its revision-pair partner identically, so flip/retain semantics survive —
// and retry.
func (b *Builder) probeExplosions(maxTries int) error {
	for try := 0; try < maxTries; try++ {
		_, err := oracle.Eval(b.w, b.cfg.Horizon, oracle.Options{})
		if err == nil {
			return nil
		}
		var jee *oracle.JoinExplosionError
		if !errors.As(err, &jee) {
			return err
		}
		if !b.tightenRuleByID(jee.RuleID) {
			return fmt.Errorf("cannot tighten exploding rule %s: %w", jee.RuleID, err)
		}
	}
	_, err := oracle.Eval(b.w, b.cfg.Horizon, oracle.Options{})
	return err
}

// tightenRuleByID tightens a rule and, if it belongs to a revision pair,
// applies the identical substitution to its partner: old and new revision
// rules must keep identical conditions (they differ only by the added
// exception), or flip/retain ground truth becomes meaningless.
func (b *Builder) tightenRuleByID(id string) bool {
	r := b.w.RuleByID(id)
	if r == nil {
		return false
	}
	partner := ""
	if nr, ok := b.SupersededBy[id]; ok {
		partner = nr
	}
	for old, nr := range b.SupersededBy {
		if nr == id {
			partner = old
			break
		}
	}
	hub, konst, ok := b.pickHub(r)
	if ok {
		applyHubSubst(r, hub, konst)
		if partner != "" {
			if pr := b.w.RuleByID(partner); pr != nil {
				applyHubSubst(pr, hub, konst)
			}
		}
		b.seedRule(r, map[string]string{}, 0)
		return true
	}
	// fallback: append one grounded extra condition, shared with the partner
	extra := b.extraGroundedCondition(r)
	if extra == nil {
		return false
	}
	r.Conditions = append(deepCopyPatterns(r.Conditions), *extra)
	if partner != "" {
		if pr := b.w.RuleByID(partner); pr != nil {
			pr.Conditions = r.Conditions
		}
	}
	b.seedRule(r, map[string]string{}, 0)
	return true
}

// pickHub selects the rule's hub variable: the non-conclusion variable
// occurring in the most conditions. Grounding it to a constant cuts the
// firing scope by roughly the size of its entity pool.
func (b *Builder) pickHub(r *world.Rule) (hub, konst string, ok bool) {
	conclVars := map[string]bool{}
	for _, t := range r.Conclusion.Args {
		if t.Var != "" {
			conclVars[t.Var] = true
		}
	}
	occ := map[string]int{}
	varType := map[string]world.EntityType{}
	for _, c := range r.Conditions {
		rel := b.w.RelationByID(c.Relation)
		for _, s := range rel.Slots {
			if t := c.Args[s.Name]; t.Var != "" {
				occ[t.Var]++
				varType[t.Var] = s.Type
			}
		}
	}
	for v, n := range occ {
		if conclVars[v] {
			continue
		}
		if hub == "" || n > occ[hub] || (n == occ[hub] && v < hub) {
			hub = v
		}
	}
	if hub == "" {
		return "", "", false
	}
	ents := b.w.EntitiesOfType(varType[hub])
	if len(ents) == 0 {
		return "", "", false
	}
	return hub, ents[b.rng.Intn(len(ents))], true
}

// applyHubSubst replaces every occurrence of hub with the constant, in
// conditions and exceptions, on deep copies (revision partners may share
// slices).
func applyHubSubst(r *world.Rule, hub, konst string) {
	subst := func(pats []world.PatternAtom) []world.PatternAtom {
		out := deepCopyPatterns(pats)
		for i := range out {
			for slot, t := range out[i].Args {
				if t.Var == hub {
					out[i].Args[slot] = world.C(konst)
				}
			}
		}
		return out
	}
	r.Conditions = subst(r.Conditions)
	r.Exceptions = subst(r.Exceptions)
}

// extraGroundedCondition builds one base-relation condition hosting a
// conclusion variable, all other slots grounded — the tightening fallback
// when every rule variable is a conclusion variable.
func (b *Builder) extraGroundedCondition(r *world.Rule) *world.PatternAtom {
	varType := map[string]world.EntityType{}
	for _, c := range r.Conditions {
		rel := b.w.RelationByID(c.Relation)
		for _, s := range rel.Slots {
			if t := c.Args[s.Name]; t.Var != "" {
				varType[t.Var] = s.Type
			}
		}
	}
	var conclVars []string
	for _, t := range r.Conclusion.Args {
		if t.Var != "" {
			conclVars = append(conclVars, t.Var)
		}
	}
	sort.Strings(conclVars)
	for _, v := range conclVars {
		vt, ok := varType[v]
		if !ok {
			continue
		}
		for i := range b.w.Relations {
			rel := &b.w.Relations[i]
			if rel.Stratum != 0 {
				continue
			}
			hosts := false
			for _, s := range rel.Slots {
				if s.Type == vt {
					hosts = true
					break
				}
			}
			if !hosts {
				continue
			}
			args := map[string]world.Term{}
			hosted := false
			for _, s := range rel.Slots {
				if !hosted && s.Type == vt {
					args[s.Name] = world.V(v)
					hosted = true
					continue
				}
				ents := b.w.EntitiesOfType(s.Type)
				args[s.Name] = world.C(ents[b.rng.Intn(len(ents))])
			}
			return &world.PatternAtom{Relation: rel.ID, Args: args}
		}
	}
	return nil
}

func deepCopyPatterns(in []world.PatternAtom) []world.PatternAtom {
	out := make([]world.PatternAtom, len(in))
	for i, p := range in {
		np := world.PatternAtom{Relation: p.Relation, Args: map[string]world.Term{}}
		for k, v := range p.Args {
			np.Args[k] = v
		}
		out[i] = np
	}
	return out
}
