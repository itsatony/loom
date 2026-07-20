package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/vaudience/synthworld/gen"
	"github.com/vaudience/synthworld/oracle"
	"github.com/vaudience/synthworld/world"
)

// entity types
const (
	tPerson  world.EntityType = "person"
	tOffice  world.EntityType = "office"
	tCountry world.EntityType = "country"
	tParty   world.EntityType = "party"
)

// relation IDs
const (
	relHoldsOffice = "holds_office"
	relOfficeOf    = "office_of"
	relSpouse      = "spouse"
	relMemberOf    = "member_of"
	relActing      = "acting"
	relHeadOf      = "head_of"
	relFirstSpouse = "first_spouse"
)

// rule / supersession IDs
const (
	ruleHeadOf      = "rule_head_of"
	ruleHeadOfV2    = "rule_head_of_v2"
	ruleFirstSpouse = "rule_first_spouse"
	supHeadOf       = "sup_head_of"
)

// episode IDs (fixed)
const (
	epRuleHeadOf        = "ep_rule_head_of"
	epRuleFirstSpouse   = "ep_rule_first_spouse"
	epRuleHeadOfRevised = "ep_rule_head_of_revised"
)

// builder accumulates the world under construction with dedup + slug maps.
type builder struct {
	horizon int
	day2010 int

	types    []world.EntityType
	entities map[string]world.Entity // id -> entity
	rels     []world.RelationSchema
	facts    []world.BaseFact
	rules    []world.Rule
	sups     []world.Supersession

	// name lookups (for query/episode text)
	nameOf map[string]string // entity id -> display name

	// person -> episode id / episode day (min office start)
	personEp    map[string]string
	personDay   map[string]int
	personFacts map[string][]int // person id -> indices into facts of that person's homed facts
	officeEp    map[string]string
	officeCty   map[string]string // office id -> country id
}

func buildDataset(snap *Snapshot, outDir string) error {
	b := &builder{
		horizon:     mustDay("2024-01-01"),
		day2010:     mustDay("2010-01-01"),
		entities:    map[string]world.Entity{},
		nameOf:      map[string]string{},
		personEp:    map[string]string{},
		personDay:   map[string]int{},
		personFacts: map[string][]int{},
		officeEp:    map[string]string{},
		officeCty:   map[string]string{},
	}
	b.types = []world.EntityType{tCountry, tOffice, tParty, tPerson}
	b.defineRelations()
	b.ingestSnapshot(snap)

	// Rules v1 + first_spouse (no revision layer yet) for the preliminary
	// closure that identifies the current head of each country.
	b.addBaseRules()

	preWorld := b.world()
	if err := preWorld.Validate(); err != nil {
		return fmt.Errorf("preliminary world invalid: %w", err)
	}
	preCl, err := oracle.Eval(preWorld, b.horizon, oracle.Options{})
	if err != nil {
		return fmt.Errorf("preliminary closure: %w", err)
	}

	// current head per country (head_of holds at t_eval), and their spouse.
	type headInfo struct {
		country, person string
	}
	var heads []headInfo
	for _, d := range preCl.Atoms {
		if d.Atom.Relation == relHeadOf {
			heads = append(heads, headInfo{country: d.Atom.Args["country"], person: d.Atom.Args["person"]})
		}
	}
	sort.Slice(heads, func(i, j int) bool { return heads[i].country < heads[j].country })

	// Choose the acting-marked person: the first current head (by country)
	// who has a spouse, so BOTH head_of and first_spouse produce a flip. If
	// none has a spouse, fall back to the first current head (head_of flips
	// only). This marker is SYNTHESIZED (Rung 0 scaffolding) — documented in
	// the manifest note.
	actingPerson, actingCountry, actingNote := "", "", ""
	for _, h := range heads {
		if b.spouseOf(h.person) != "" {
			actingPerson, actingCountry = h.person, h.country
			break
		}
	}
	if actingPerson == "" && len(heads) > 0 {
		actingPerson, actingCountry = heads[0].person, heads[0].country
	}
	if actingPerson == "" {
		return fmt.Errorf("no current head of government found in snapshot; cannot build revision slice")
	}
	actingNote = fmt.Sprintf("SYNTHESIZED acting(%s) marker (person is real, the 'acting' status is "+
		"scaffolding for the revision slice): from day %d the head-of-government rule excludes acting "+
		"holders, so head_of(%s, %s) and its first_spouse flip (stale=true, current=false) while the "+
		"other countries are retained controls.", actingPerson, b.day2010, actingCountry, actingPerson)

	// Add the acting fact (homed in that person's episode), the v2 rule with
	// the exception, and the supersession that retires v1 from 2010.
	b.addActing(actingPerson)
	b.addRevisionLayer()

	// Assign episodes (sets EpisodeID + reveal days on all facts/rules/sups).
	episodes := b.buildEpisodes(snap)

	w := b.world()
	if err := w.Validate(); err != nil {
		return fmt.Errorf("world invalid: %w", err)
	}

	epDay := map[string]int{}
	for _, ep := range episodes {
		epDay[ep.ID] = ep.Day
	}
	revealedBy := func(id string) (int, bool) { d, ok := epDay[id]; return d, ok }

	cur, err := oracle.Eval(w, b.horizon, oracle.Options{RevealedBy: revealedBy})
	if err != nil {
		return fmt.Errorf("current closure: %w", err)
	}
	stale, err := oracle.Eval(w, b.horizon, oracle.Options{IgnoreSupersessions: true, RevealedBy: revealedBy})
	if err != nil {
		return fmt.Errorf("stale closure: %w", err)
	}

	qs := b.buildQueries(w, cur, stale, actingCountry, actingPerson)

	return b.writeDataset(outDir, w, episodes, qs, actingNote)
}

// ---------- schema ----------

func (b *builder) defineRelations() {
	b.rels = []world.RelationSchema{
		{ID: relOfficeOf, Name: "office_of", Stratum: 0, Slots: []world.SlotDef{
			{Name: "office", Type: tOffice}, {Name: "country", Type: tCountry}}},
		{ID: relHoldsOffice, Name: "holds_office", Stratum: 0, Slots: []world.SlotDef{
			{Name: "person", Type: tPerson}, {Name: "office", Type: tOffice}}},
		{ID: relSpouse, Name: "spouse", Stratum: 0, Slots: []world.SlotDef{
			{Name: "person", Type: tPerson}, {Name: "spouse", Type: tPerson}}},
		{ID: relMemberOf, Name: "member_of", Stratum: 0, Slots: []world.SlotDef{
			{Name: "person", Type: tPerson}, {Name: "party", Type: tParty}}},
		{ID: relActing, Name: "acting", Stratum: 0, Slots: []world.SlotDef{
			{Name: "person", Type: tPerson}}},
		{ID: relHeadOf, Name: "head_of", Stratum: 1, Slots: []world.SlotDef{
			{Name: "country", Type: tCountry}, {Name: "person", Type: tPerson}}},
		{ID: relFirstSpouse, Name: "first_spouse", Stratum: 2, Slots: []world.SlotDef{
			{Name: "country", Type: tCountry}, {Name: "spouse", Type: tPerson}}},
	}
}

func (b *builder) addEntity(id string, t world.EntityType, name string) {
	if _, ok := b.entities[id]; !ok {
		b.entities[id] = world.Entity{ID: id, Type: t}
	}
	if name != "" {
		b.nameOf[id] = name
	}
}

// ---------- snapshot -> entities + base facts ----------

func (b *builder) ingestSnapshot(snap *Snapshot) {
	for _, c := range snap.Countries {
		cid := "c_" + slug(c.Name)
		b.addEntity(cid, tCountry, c.Name)
		oid := "off_" + slug(c.OfficeName)
		b.addEntity(oid, tOffice, c.OfficeName)
		b.officeCty[oid] = cid

		// office_of fact (open, from day 0)
		b.addFact(world.BaseFact{
			ID:   "f_office_of_" + slug(c.OfficeName),
			Atom: world.Atom{Relation: relOfficeOf, Args: map[string]string{"office": oid, "country": cid}},
			From: 0, To: 0, Source: "wikidata:P1313",
		})

		for _, h := range c.Holders {
			pid := "p_" + slug(h.Name)
			b.addEntity(pid, tPerson, h.Name)

			// holds_office facts (real intervals; drop future/degenerate)
			var kept bool
			minStart := -1
			for _, term := range h.Terms {
				from, ok := parseDay(term.Start)
				if !ok || from > b.horizon {
					continue
				}
				to := 0
				if te, ok := parseDay(term.End); ok {
					if te <= from {
						continue // degenerate interval
					}
					to = te
				}
				b.addPersonFact(pid, world.BaseFact{
					ID:   fmt.Sprintf("f_holds_%s_%s_%d", slug(h.Name), slug(c.OfficeName), from),
					Atom: world.Atom{Relation: relHoldsOffice, Args: map[string]string{"person": pid, "office": oid}},
					From: from, To: to, Source: "wikidata:P39",
				})
				kept = true
				if minStart < 0 || from < minStart {
					minStart = from
				}
			}
			if !kept {
				continue // person has no valid officeholding; skip entirely
			}
			b.personDay[pid] = minStart

			if h.SpouseQID != "" && h.Spouse != "" {
				sid := "p_" + slug(h.Spouse)
				b.addEntity(sid, tPerson, h.Spouse)
				b.addPersonFact(pid, world.BaseFact{
					ID:   "f_spouse_" + slug(h.Name),
					Atom: world.Atom{Relation: relSpouse, Args: map[string]string{"person": pid, "spouse": sid}},
					From: 0, To: 0, Source: "wikidata:P26",
				})
			}
			if h.PartyQID != "" && h.Party != "" {
				ptid := "pty_" + slug(h.Party)
				b.addEntity(ptid, tParty, h.Party)
				b.addPersonFact(pid, world.BaseFact{
					ID:   "f_member_" + slug(h.Name),
					Atom: world.Atom{Relation: relMemberOf, Args: map[string]string{"person": pid, "party": ptid}},
					From: 0, To: 0, Source: "wikidata:P102",
				})
			}
		}
	}
}

func (b *builder) addFact(f world.BaseFact) int {
	b.facts = append(b.facts, f)
	return len(b.facts) - 1
}

func (b *builder) addPersonFact(pid string, f world.BaseFact) {
	idx := b.addFact(f)
	b.personFacts[pid] = append(b.personFacts[pid], idx)
}

func (b *builder) spouseOf(pid string) string {
	for _, f := range b.facts {
		if f.Atom.Relation == relSpouse && f.Atom.Args["person"] == pid {
			return f.Atom.Args["spouse"]
		}
	}
	return ""
}

// ---------- rules ----------

func (b *builder) addBaseRules() {
	b.rules = append(b.rules,
		world.Rule{
			ID:   ruleHeadOf,
			Name: "head of government is whoever holds the country's head-of-government office",
			Conditions: []world.PatternAtom{
				{Relation: relOfficeOf, Args: map[string]world.Term{"office": world.V("O"), "country": world.V("C")}},
				{Relation: relHoldsOffice, Args: map[string]world.Term{"person": world.V("P"), "office": world.V("O")}},
			},
			Conclusion: world.PatternAtom{Relation: relHeadOf, Args: map[string]world.Term{"country": world.V("C"), "person": world.V("P")}},
			Assert:     true, Authority: 3, IssuedAt: 0, EffectiveFrom: 0, EffectiveTo: 0,
		},
		world.Rule{
			ID:   ruleFirstSpouse,
			Name: "first spouse of a country is the spouse of its head of government",
			Conditions: []world.PatternAtom{
				{Relation: relHeadOf, Args: map[string]world.Term{"country": world.V("C"), "person": world.V("P")}},
				{Relation: relSpouse, Args: map[string]world.Term{"person": world.V("P"), "spouse": world.V("S")}},
			},
			Conclusion: world.PatternAtom{Relation: relFirstSpouse, Args: map[string]world.Term{"country": world.V("C"), "spouse": world.V("S")}},
			Assert:     true, Authority: 3, IssuedAt: 0, EffectiveFrom: 0, EffectiveTo: 0,
		},
	)
}

func (b *builder) addActing(pid string) {
	b.addPersonFact(pid, world.BaseFact{
		ID:   "f_acting_" + pid,
		Atom: world.Atom{Relation: relActing, Args: map[string]string{"person": pid}},
		From: 0, To: 0, Source: "synthesized:acting-scaffold",
	})
}

func (b *builder) addRevisionLayer() {
	// v2 == v1 with an added exception excluding acting holders.
	b.rules = append(b.rules, world.Rule{
		ID:   ruleHeadOfV2,
		Name: "head of government is whoever holds the office, unless they are only acting",
		Conditions: []world.PatternAtom{
			{Relation: relOfficeOf, Args: map[string]world.Term{"office": world.V("O"), "country": world.V("C")}},
			{Relation: relHoldsOffice, Args: map[string]world.Term{"person": world.V("P"), "office": world.V("O")}},
		},
		Conclusion: world.PatternAtom{Relation: relHeadOf, Args: map[string]world.Term{"country": world.V("C"), "person": world.V("P")}},
		Exceptions: []world.PatternAtom{{Relation: relActing, Args: map[string]world.Term{"person": world.V("P")}}},
		Assert:     true, Authority: 3, IssuedAt: b.day2010, EffectiveFrom: 0, EffectiveTo: 0,
	})
	b.sups = append(b.sups, world.Supersession{
		ID: supHeadOf, OldRule: ruleHeadOf, NewRule: ruleHeadOfV2, From: b.day2010,
	})
}

// ---------- episodes ----------

func (b *builder) buildEpisodes(snap *Snapshot) []gen.Episode {
	var episodes []gen.Episode

	// office episodes (day 0)
	officeIDs := make([]string, 0, len(b.officeCty))
	for oid := range b.officeCty {
		officeIDs = append(officeIDs, oid)
	}
	sort.Strings(officeIDs)
	for _, oid := range officeIDs {
		epID := "ep_office_" + oid
		b.officeEp[oid] = epID
		cid := b.officeCty[oid]
		text := fmt.Sprintf("The office of %s is the head of government of %s.",
			b.nameOf[oid], b.nameOf[cid])
		var ev []gen.Event
		for i := range b.facts {
			f := &b.facts[i]
			if f.Atom.Relation == relOfficeOf && f.Atom.Args["office"] == oid {
				f.EpisodeID = epID
				ev = append(ev, gen.Event{Kind: gen.EvFact, Day: 0, Fact: cloneFact(f), Text: text})
			}
		}
		episodes = append(episodes, gen.Episode{ID: epID, Day: 0, Text: text, Events: ev})
	}

	// person episodes (day = first office start), text = Wikipedia extract
	extractByPerson := map[string]string{}
	for _, c := range snap.Countries {
		for _, h := range c.Holders {
			pid := "p_" + slug(h.Name)
			if h.Extract != "" {
				extractByPerson[pid] = h.Extract
			}
		}
	}
	var personIDs []string
	for pid := range b.personDay {
		personIDs = append(personIDs, pid)
	}
	sort.Strings(personIDs)
	for _, pid := range personIDs {
		epID := "ep_person_" + pid
		b.personEp[pid] = epID
		day := b.personDay[pid]
		text := extractByPerson[pid]
		if text == "" {
			text = fmt.Sprintf("%s is a head of government in the record.", b.nameOf[pid])
		}
		idxs := append([]int{}, b.personFacts[pid]...)
		sort.Slice(idxs, func(i, j int) bool { return b.facts[idxs[i]].ID < b.facts[idxs[j]].ID })
		var ev []gen.Event
		for _, idx := range idxs {
			f := &b.facts[idx]
			f.EpisodeID = epID
			ev = append(ev, gen.Event{Kind: gen.EvFact, Day: day, Fact: cloneFact(f), Text: b.factLine(f)})
		}
		episodes = append(episodes, gen.Episode{ID: epID, Day: day, Text: text, Events: ev})
	}

	// rule episodes (day 0)
	b.ruleByID(ruleHeadOf).EpisodeID = epRuleHeadOf
	b.ruleByID(ruleFirstSpouse).EpisodeID = epRuleFirstSpouse
	episodes = append(episodes,
		gen.Episode{ID: epRuleHeadOf, Day: 0,
			Text:   "Policy: the head of government of a country is whoever currently holds that country's head-of-government office.",
			Events: []gen.Event{{Kind: gen.EvRule, Day: 0, Rule: cloneRule(b.ruleByID(ruleHeadOf)), Text: "head_of policy issued"}}},
		gen.Episode{ID: epRuleFirstSpouse, Day: 0,
			Text:   "Policy: the first spouse of a country is the spouse of its head of government.",
			Events: []gen.Event{{Kind: gen.EvRule, Day: 0, Rule: cloneRule(b.ruleByID(ruleFirstSpouse)), Text: "first_spouse policy issued"}}},
	)

	// revision episode (day 2010): v2 rule + supersession
	b.ruleByID(ruleHeadOfV2).EpisodeID = epRuleHeadOfRevised
	b.sups[0].EpisodeID = epRuleHeadOfRevised
	revText := "Policy update: the head of government of a country is whoever holds the office, " +
		"UNLESS that person is only serving in an acting capacity. This supersedes the earlier " +
		"head-of-government policy."
	episodes = append(episodes, gen.Episode{
		ID: epRuleHeadOfRevised, Day: b.day2010, Text: revText,
		Events: []gen.Event{
			{Kind: gen.EvRule, Day: b.day2010, Rule: cloneRule(b.ruleByID(ruleHeadOfV2)), Text: "head_of policy revised (acting exception)"},
			{Kind: gen.EvSupersession, Day: b.day2010, Supersession: cloneSup(&b.sups[0]), Text: "old head_of policy superseded"},
		},
	})

	sort.Slice(episodes, func(i, j int) bool {
		if episodes[i].Day != episodes[j].Day {
			return episodes[i].Day < episodes[j].Day
		}
		return episodes[i].ID < episodes[j].ID
	})
	return episodes
}

func (b *builder) factLine(f *world.BaseFact) string {
	return fmt.Sprintf("[day %d] %s (source %s).", f.From, b.atomText(f.Atom), f.Source)
}

func (b *builder) atomText(a world.Atom) string {
	rel := b.relByID(a.Relation)
	parts := make([]string, 0, len(rel.Slots))
	for _, s := range rel.Slots {
		val := a.Args[s.Name]
		if n := b.nameOf[val]; n != "" {
			parts = append(parts, fmt.Sprintf("%s=%s", s.Name, n))
		} else {
			parts = append(parts, fmt.Sprintf("%s=%s", s.Name, val))
		}
	}
	return fmt.Sprintf("%s(%s)", rel.Name, joinComma(parts))
}

// ---------- queries ----------

func (b *builder) buildQueries(w *world.World, cur, stale *oracle.Closure, actingCountry, actingPerson string) *gen.QuerySet {
	qs := &gen.QuerySet{AtDay: b.horizon}
	n := 0
	nextID := func() string { n++; return fmt.Sprintf("qry_%04d", n) }

	// ----- repetition: base facts valid at t_eval -----
	// sorted current (valid-at-horizon) base atoms by relation of interest
	baseAtoms := b.validBaseAtoms(cur)
	repTarget := 12
	repCount := 0
	// prefer holds_office, then spouse, then member_of, alternating pos/neg
	for _, rel := range []string{relHoldsOffice, relSpouse, relMemberOf} {
		for _, a := range baseAtoms[rel] {
			if repCount >= repTarget {
				break
			}
			b.addHolds(qs, nextID(), "repetition", a, true, nil, 0,
				[]string{b.revealEp(cur.Get(a))}, b.questionText(a))
			repCount++
			if neg, ok := b.perturb(a, cur, stale); ok && repCount < repTarget {
				b.addHolds(qs, nextID(), "repetition", neg, false, nil, 0, nil, b.questionText(neg))
				repCount++
			}
		}
	}

	// ----- composition: derived atoms, EXCLUDING the acting country -----
	compTarget := 8
	compCount := 0
	for _, rel := range []string{relHeadOf, relFirstSpouse} {
		for _, k := range sortedDerivedKeys(cur, rel) {
			if compCount >= compTarget {
				break
			}
			d := cur.Atoms[k]
			if d.Atom.Args["country"] == actingCountry {
				continue // belongs to the revision slice
			}
			prov := oracle.ProvenanceEpisodes(w, d)
			if len(prov) < 2 {
				continue
			}
			b.addHolds(qs, nextID(), "composition", d.Atom, true, nil, d.Depth, prov, b.questionText(d.Atom))
			compCount++
			if neg, ok := b.perturb(d.Atom, cur, stale); ok && compCount < compTarget {
				b.addHolds(qs, nextID(), "composition", neg, false, nil, d.Depth, nil, b.questionText(neg))
				compCount++
			}
		}
	}

	// ----- composition find queries -----
	b.addFind(qs, nextID(), cur, w, relHeadOf, "person", actingCountry)
	b.addFind(qs, nextID(), cur, w, relFirstSpouse, "spouse", actingCountry)

	// ----- revision: flips (acting country) + retained controls (others) -----
	revCount, revTarget := 0, 6
	// flips first
	for _, rel := range []string{relHeadOf, relFirstSpouse} {
		for _, k := range sortedStaleKeys(stale, rel) {
			if revCount >= revTarget {
				break
			}
			sd := stale.Atoms[k]
			if sd.Atom.Args["country"] != actingCountry {
				continue
			}
			if cur.Holds(sd.Atom) {
				continue // not a flip
			}
			prov := oracle.ProvenanceEpisodes(w, sd)
			prov = withSupEpisode(prov)
			b.addHolds(qs, nextID(), "revision", sd.Atom, false, boolp(true), sd.Depth, prov, b.questionText(sd.Atom))
			revCount++
		}
	}
	// retained controls
	for _, rel := range []string{relHeadOf, relFirstSpouse} {
		for _, k := range sortedDerivedKeys(cur, rel) {
			if revCount >= revTarget {
				break
			}
			d := cur.Atoms[k]
			if d.Atom.Args["country"] == actingCountry {
				continue
			}
			if !stale.Holds(d.Atom) {
				continue
			}
			prov := oracle.ProvenanceEpisodes(w, d)
			prov = withSupEpisode(prov)
			b.addHolds(qs, nextID(), "revision", d.Atom, true, boolp(true), d.Depth, prov, b.questionText(d.Atom))
			revCount++
		}
	}

	return qs
}

func withSupEpisode(prov []string) []string {
	set := map[string]bool{epRuleHeadOfRevised: true}
	for _, p := range prov {
		set[p] = true
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (b *builder) addHolds(qs *gen.QuerySet, id, slice string, a world.Atom, ans bool, stale *bool, depth int, prov []string, text string) {
	qs.Queries = append(qs.Queries, gen.Query{
		ID: id, Slice: slice, Type: "holds", AtDay: b.horizon,
		Atom: cloneAtom(a), Answer: boolp(ans), StaleAnswer: stale,
		Depth: depth, ProvenanceEpisodes: prov, Text: text,
	})
}

func (b *builder) addFind(qs *gen.QuerySet, id string, cur *oracle.Closure, w *world.World, relID, freeSlot, actingCountry string) {
	rel := b.relByID(relID)
	// pick a country constant that is NOT the acting country and has >=1 answer
	var countries []string
	for _, e := range b.entities {
		if e.Type == tCountry {
			countries = append(countries, e.ID)
		}
	}
	sort.Strings(countries)
	for _, cid := range countries {
		if cid == actingCountry {
			continue
		}
		pattern := world.PatternAtom{Relation: relID, Args: map[string]world.Term{}}
		for _, s := range rel.Slots {
			if s.Name == freeSlot {
				pattern.Args[s.Name] = world.V("X")
			} else {
				pattern.Args[s.Name] = world.C(cid)
			}
		}
		var answers []string
		var depth int
		var prov []string
		for _, k := range sortedDerivedKeys(cur, relID) {
			d := cur.Atoms[k]
			match := true
			for _, s := range rel.Slots {
				if s.Name == freeSlot {
					continue
				}
				if d.Atom.Args[s.Name] != cid {
					match = false
					break
				}
			}
			if match {
				answers = append(answers, d.Atom.Args[freeSlot])
				depth = d.Depth
				prov = oracle.ProvenanceEpisodes(w, d)
			}
		}
		if len(answers) == 0 {
			continue
		}
		sort.Strings(answers)
		prov = withSupEpisode(prov)
		qs.Queries = append(qs.Queries, gen.Query{
			ID: id, Slice: "composition", Type: "find", AtDay: b.horizon,
			Pattern: &pattern, FindSlot: freeSlot, AnswerSet: answers,
			Depth: depth, ProvenanceEpisodes: prov,
			Text: fmt.Sprintf("On day %d, list every %s such that %s holds for country %s. Answer with entity IDs.",
				b.horizon, freeSlot, rel.Name, b.nameOf[cid]),
		})
		return
	}
}

// perturb swaps one argument to a different same-type entity so the result
// holds in neither closure.
func (b *builder) perturb(a world.Atom, cur, stale *oracle.Closure) (world.Atom, bool) {
	rel := b.relByID(a.Relation)
	for _, s := range rel.Slots {
		var cands []string
		for _, e := range b.entities {
			if e.Type == s.Type && e.ID != a.Args[s.Name] {
				cands = append(cands, e.ID)
			}
		}
		sort.Strings(cands)
		for _, c := range cands {
			cand := cloneAtomVal(a)
			cand.Args[s.Name] = c
			if !cur.Holds(cand) && !stale.Holds(cand) {
				return cand, true
			}
		}
	}
	return world.Atom{}, false
}

// validBaseAtoms returns, per base relation, the atoms valid at t_eval, sorted.
func (b *builder) validBaseAtoms(cur *oracle.Closure) map[string][]world.Atom {
	out := map[string][]world.Atom{}
	keys := map[string][]string{}
	byKey := map[string]world.Atom{}
	for k, d := range cur.Atoms {
		if d.FactID == "" {
			continue
		}
		keys[d.Atom.Relation] = append(keys[d.Atom.Relation], k)
		byKey[k] = d.Atom
	}
	for rel, ks := range keys {
		sort.Strings(ks)
		for _, k := range ks {
			out[rel] = append(out[rel], byKey[k])
		}
	}
	return out
}

func (b *builder) revealEp(d *oracle.Derivation) string {
	if d == nil {
		return ""
	}
	for i := range b.facts {
		if b.facts[i].ID == d.FactID {
			return b.facts[i].EpisodeID
		}
	}
	return ""
}

func (b *builder) questionText(a world.Atom) string {
	return fmt.Sprintf("On day %d, does %s hold? Answer true or false.", b.horizon, b.atomText(a))
}

// ---------- world assembly + write ----------

func (b *builder) world() *world.World {
	var ents []world.Entity
	for _, e := range b.entities {
		ents = append(ents, e)
	}
	sort.Slice(ents, func(i, j int) bool { return ents[i].ID < ents[j].ID })
	facts := append([]world.BaseFact{}, b.facts...)
	sort.Slice(facts, func(i, j int) bool { return facts[i].ID < facts[j].ID })
	rules := append([]world.Rule{}, b.rules...)
	sort.Slice(rules, func(i, j int) bool { return rules[i].ID < rules[j].ID })
	sups := append([]world.Supersession{}, b.sups...)
	sort.Slice(sups, func(i, j int) bool { return sups[i].ID < sups[j].ID })
	rels := append([]world.RelationSchema{}, b.rels...)
	sort.Slice(rels, func(i, j int) bool { return rels[i].ID < rels[j].ID })
	return &world.World{
		Seed: 0, Horizon: b.horizon,
		Types: b.types, Entities: ents, Relations: rels,
		Facts: facts, Rules: rules, Supersessions: sups,
	}
}

func (b *builder) writeDataset(dir string, w *world.World, episodes []gen.Episode, qs *gen.QuerySet, actingNote string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(dir, "world.json"), w); err != nil {
		return err
	}
	if err := writeJSONL(filepath.Join(dir, "episodes.jsonl"), func(emit func(any) error) error {
		for _, ep := range episodes {
			if err := emit(ep); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if err := writeJSONL(filepath.Join(dir, "queries.jsonl"), func(emit func(any) error) error {
		for _, q := range qs.Queries {
			if err := emit(q); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	manifest := map[string]any{
		"generator":         "realimport-rung0",
		"num_entities":      len(w.Entities),
		"num_facts":         len(w.Facts),
		"num_rules":         len(w.Rules),
		"num_supersessions": len(w.Supersessions),
		"num_episodes":      len(episodes),
		"num_queries":       len(qs.Queries),
		"eval_day":          qs.AtDay,
		"note": "Rung 0: REAL Wikidata + Wikipedia data (national heads of government, " +
			"spouses, parties) imported into the synthworld dataset format so the existing " +
			"cmd/validate + cmd/harness toolchain runs unchanged. Non-falsifying plumbing gate. " +
			actingNote,
	}
	return writeJSON(filepath.Join(dir, "manifest.json"), manifest)
}

func writeJSON(path string, v any) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return err
	}
	return f.Close()
}

func writeJSONL(path string, produce func(emit func(any) error) error) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	if err := produce(func(v any) error { return enc.Encode(v) }); err != nil {
		return err
	}
	return f.Close()
}

// ---------- small helpers ----------

func (b *builder) relByID(id string) *world.RelationSchema {
	for i := range b.rels {
		if b.rels[i].ID == id {
			return &b.rels[i]
		}
	}
	return nil
}

func (b *builder) ruleByID(id string) *world.Rule {
	for i := range b.rules {
		if b.rules[i].ID == id {
			return &b.rules[i]
		}
	}
	return nil
}

func sortedDerivedKeys(cl *oracle.Closure, relID string) []string {
	var ks []string
	for k, d := range cl.Atoms {
		if d.Atom.Relation == relID && d.RuleID != "" {
			ks = append(ks, k)
		}
	}
	sort.Strings(ks)
	return ks
}

func sortedStaleKeys(cl *oracle.Closure, relID string) []string {
	var ks []string
	for k, d := range cl.Atoms {
		if d.Atom.Relation == relID {
			ks = append(ks, k)
		}
	}
	sort.Strings(ks)
	return ks
}

func boolp(v bool) *bool { return &v }

func cloneAtom(a world.Atom) *world.Atom {
	c := cloneAtomVal(a)
	return &c
}

func cloneAtomVal(a world.Atom) world.Atom {
	args := make(map[string]string, len(a.Args))
	for k, v := range a.Args {
		args[k] = v
	}
	return world.Atom{Relation: a.Relation, Args: args}
}

func cloneFact(f *world.BaseFact) *world.BaseFact {
	c := *f
	c.Atom = cloneAtomVal(f.Atom)
	return &c
}

func cloneRule(r *world.Rule) *world.Rule {
	c := *r
	return &c
}

func cloneSup(s *world.Supersession) *world.Supersession {
	c := *s
	return &c
}

func joinComma(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}
