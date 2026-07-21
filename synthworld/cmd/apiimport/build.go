package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/vaudience/synthworld/gen"
	"github.com/vaudience/synthworld/oracle"
	"github.com/vaudience/synthworld/world"
)

// entity types
const (
	tSymbol  world.EntityType = "symbol"
	tVersion world.EntityType = "version"
)

// relation IDs
const (
	relShipped        = "shipped"            // base: symbol exists in the API
	relDeprecatedIn   = "deprecated_in"      // base: symbol deprecated in a version
	relReplacedBy     = "replaced_by"        // base: symbol replaced by another
	relRemovedMark    = "removed"            // base: marker — symbol has been removed
	relDeprTarget     = "deprecation_target" // base: policy schedule (source ver -> removal ver)
	relIsMajor        = "is_major"           // base: version is a major (.0) release
	relAvailable      = "available"          // derived s1: symbol is still callable
	relRemovalTarget  = "removal_target"     // derived s1: policy-mandated removal version
	relBlockedUpgrade = "blocked_upgrade"    // derived s2: removal lands on a major release
)

// rule / supersession IDs
const (
	ruleAvailableV1   = "rule_available"
	ruleAvailableV2   = "rule_available_v2"
	ruleRemovalTarget = "rule_removal_target"
	ruleBlockedUp     = "rule_blocked_upgrade"
	supAvailable      = "sup_available"
)

// fixed episode IDs (the non-per-symbol ones)
const (
	epCatalog  = "ep_catalog"
	epPolicy   = "ep_policy"
	epCalendar = "ep_calendar"
	epRemoval  = "ep_removal"
)

// builder accumulates the world under construction with dedup + name maps.
type builder struct {
	horizon int

	dayCatalog int // when the API surface is first cataloged (earliest modeled release)
	dayPolicy  int // when the deprecation policy + schedule are stated
	dayRemoval int // the 6.1 release: removals take effect (supersession day)

	types    []world.EntityType
	entities map[string]world.Entity
	rels     []world.RelationSchema
	facts    []world.BaseFact
	rules    []world.Rule
	sups     []world.Supersession

	nameOf map[string]string // entity id -> display name (surface form)
	verDay map[string]int    // version id -> release day (modeled releases only)

	// homing: fact index -> episode id, plus per-episode prose text
	factEp   map[int]string
	epDay    map[string]int
	depProse map[string]string // per-symbol deprecation episode prose
	// depVersion homes each deprecation episode under the version it was
	// released in ("6.1", "5.2"). Real Django release notes publish each
	// deprecation under a versioned heading ("Features deprecated in 6.1");
	// the episode text restores that heading so the version — the deprecated_in
	// fact's second argument — is LEXICALLY present, as it is in the real
	// source. Without it the version lives only in the structured payload, and
	// no text-consuming condition (C1 or C2b) can recover which release a
	// deprecation belongs to.
	depVersion map[string]string
	remProse   string // shared removal-notice prose (real, concatenated)
}

func buildDataset(snap *Snapshot, outDir string) error {
	b := &builder{
		entities:   map[string]world.Entity{},
		nameOf:     map[string]string{},
		verDay:     map[string]int{},
		factEp:     map[int]string{},
		epDay:      map[string]int{},
		depProse:   map[string]string{},
		depVersion: map[string]string{},
	}
	b.types = []world.EntityType{tSymbol, tVersion}
	b.defineRelations()

	// ----- timeline: modeled release days from the snapshot -----
	for _, r := range snap.Releases {
		vid := b.versionEntity(r.Version)
		if d, ok := parseDay(r.Date); ok {
			b.verDay[vid] = d
		}
	}
	// Anchor the three modeled event days. 6.0 = policy/catalog anchor,
	// pin (6.1) = removal/horizon. Fall back defensively if a date is absent.
	b.dayCatalog = b.verDayOr("6.0", mustDay("2025-12-03"))
	b.dayPolicy = b.dayCatalog
	b.dayRemoval = mustDay(snap.PinDate)
	b.horizon = b.dayRemoval

	// ----- symbols -> entities + base facts + per-symbol episodes -----
	b.ingestSymbols(snap)

	// ----- policy schedule facts (deprecation_target) + is_major facts -----
	b.addScheduleFacts(snap)

	// ----- rules + the removal supersession -----
	b.addRules()

	// ----- assemble episodes (homes every fact/rule/sup) -----
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

	qs := b.buildQueries(w, cur, stale)
	return b.writeDataset(outDir, w, episodes, qs, snap)
}

// ---------- schema ----------

func (b *builder) defineRelations() {
	b.rels = []world.RelationSchema{
		{ID: relShipped, Name: relShipped, Stratum: 0, Slots: []world.SlotDef{
			{Name: "symbol", Type: tSymbol}}},
		{ID: relDeprecatedIn, Name: relDeprecatedIn, Stratum: 0, Slots: []world.SlotDef{
			{Name: "symbol", Type: tSymbol}, {Name: "version", Type: tVersion}}},
		{ID: relReplacedBy, Name: relReplacedBy, Stratum: 0, Slots: []world.SlotDef{
			{Name: "old", Type: tSymbol}, {Name: "new", Type: tSymbol}}},
		{ID: relRemovedMark, Name: relRemovedMark, Stratum: 0, Slots: []world.SlotDef{
			{Name: "symbol", Type: tSymbol}}},
		{ID: relDeprTarget, Name: relDeprTarget, Stratum: 0, Slots: []world.SlotDef{
			{Name: "source", Type: tVersion}, {Name: "target", Type: tVersion}}},
		{ID: relIsMajor, Name: relIsMajor, Stratum: 0, Slots: []world.SlotDef{
			{Name: "version", Type: tVersion}}},
		{ID: relAvailable, Name: relAvailable, Stratum: 1, Slots: []world.SlotDef{
			{Name: "symbol", Type: tSymbol}}},
		{ID: relRemovalTarget, Name: relRemovalTarget, Stratum: 1, Slots: []world.SlotDef{
			{Name: "symbol", Type: tSymbol}, {Name: "version", Type: tVersion}}},
		{ID: relBlockedUpgrade, Name: relBlockedUpgrade, Stratum: 2, Slots: []world.SlotDef{
			{Name: "symbol", Type: tSymbol}, {Name: "version", Type: tVersion}}},
	}
}

func (b *builder) addEntity(id string, t world.EntityType, name string) {
	if _, ok := b.entities[id]; !ok {
		// Name carries the surface form into world.json so the harness can
		// seed the S2 extractor's symbol catalog (entity grounding).
		b.entities[id] = world.Entity{ID: id, Type: t, Name: name}
	}
	if name != "" {
		b.nameOf[id] = name
	}
}

func (b *builder) symbolEntity(surface string) string {
	id := "sym_" + slug(surface)
	b.addEntity(id, tSymbol, surface)
	return id
}

func (b *builder) versionEntity(v string) string {
	id := "ver_" + slug(v)
	b.addEntity(id, tVersion, v)
	return id
}

func (b *builder) verDayOr(v string, fallback int) int {
	if d, ok := b.verDay["ver_"+slug(v)]; ok {
		return d
	}
	return fallback
}

// ---------- symbols -> facts ----------

func (b *builder) ingestSymbols(snap *Snapshot) {
	for _, s := range snap.Symbols {
		sid := b.symbolEntity(s.ID)

		// Every symbol ships (base plumbing; catalog lines are synthesized
		// scaffolding — see manifest). Homed in the catalog episode.
		shIdx := b.addFact(world.BaseFact{
			ID:   "f_shipped_" + sid,
			Atom: world.Atom{Relation: relShipped, Args: map[string]string{"symbol": sid}},
			From: 0, To: 0, Source: "django:api-surface",
		})
		b.factEp[shIdx] = epCatalog

		// Replacement target: ensure the replacement symbol also exists/ships.
		var tid string
		if s.ReplacedBy != "" {
			tid = b.symbolEntity(s.ReplacedBy)
			if !b.hasShipped(tid) {
				ri := b.addFact(world.BaseFact{
					ID:   "f_shipped_" + tid,
					Atom: world.Atom{Relation: relShipped, Args: map[string]string{"symbol": tid}},
					From: 0, To: 0, Source: "django:api-surface",
				})
				b.factEp[ri] = epCatalog
			}
		}

		switch s.StatusAtPin {
		case "deprecated":
			// deprecated_in fact — homed in a per-symbol deprecation episode
			// carrying the REAL release-note prose (only if we have prose to
			// present, so the C2b extractor has something to read).
			if s.DeprecatedIn != "" && s.DeprecationProse != "" {
				vid := b.versionEntity(s.DeprecatedIn)
				di := b.addFact(world.BaseFact{
					ID:   "f_dep_" + sid,
					Atom: world.Atom{Relation: relDeprecatedIn, Args: map[string]string{"symbol": sid, "version": vid}},
					From: b.verDayOr(s.DeprecatedIn, b.dayRemoval), To: 0, Source: "django:release-notes",
				})
				epID := "ep_dep_" + sid
				b.factEp[di] = epID
				b.depProse[epID] = s.DeprecationProse
				b.depVersion[epID] = s.DeprecatedIn
				b.epDay[epID] = b.verDayOr(s.DeprecatedIn, b.dayRemoval)
				if tid != "" {
					rb := b.addFact(world.BaseFact{
						ID:   "f_repl_" + sid,
						Atom: world.Atom{Relation: relReplacedBy, Args: map[string]string{"old": sid, "new": tid}},
						From: b.verDayOr(s.DeprecatedIn, b.dayRemoval), To: 0, Source: "django:release-notes",
					})
					b.factEp[rb] = epID // replacement is stated with the deprecation
				}
			}

		case "removed":
			// removed marker + (real) removal-notice prose, homed in the shared
			// removal episode at the 6.1 release day. deprecated_in from the
			// ORIGINAL (5.2) version if we have its prose (composition source).
			rm := b.addFact(world.BaseFact{
				ID:   "f_removed_" + sid,
				Atom: world.Atom{Relation: relRemovedMark, Args: map[string]string{"symbol": sid}},
				From: b.dayRemoval, To: 0, Source: "django:release-notes",
			})
			b.factEp[rm] = epRemoval
			if s.RemovalProse != "" {
				b.remProse = appendProse(b.remProse, s.RemovalProse)
			}
			if s.DeprecatedIn != "" && s.DeprecationProse != "" {
				vid := b.versionEntity(s.DeprecatedIn)
				di := b.addFact(world.BaseFact{
					ID:   "f_dep_" + sid,
					Atom: world.Atom{Relation: relDeprecatedIn, Args: map[string]string{"symbol": sid, "version": vid}},
					From: b.verDayOr(s.DeprecatedIn, b.dayCatalog), To: 0, Source: "django:release-notes",
				})
				epID := "ep_dep_" + sid
				b.factEp[di] = epID
				b.depProse[epID] = s.DeprecationProse
				b.depVersion[epID] = s.DeprecatedIn
				b.epDay[epID] = b.verDayOr(s.DeprecatedIn, b.dayCatalog)
			}
			if tid != "" {
				rb := b.addFact(world.BaseFact{
					ID:   "f_repl_" + sid,
					Atom: world.Atom{Relation: relReplacedBy, Args: map[string]string{"old": sid, "new": tid}},
					From: b.dayRemoval, To: 0, Source: "django:release-notes",
				})
				b.factEp[rb] = epRemoval // replacement stated in the removal note
			}

		default: // "stable" — no deprecation/removal; retained control + repetition base
		}
	}
}

func (b *builder) addFact(f world.BaseFact) int {
	b.facts = append(b.facts, f)
	return len(b.facts) - 1
}

func (b *builder) hasShipped(sid string) bool {
	for i := range b.facts {
		if b.facts[i].Atom.Relation == relShipped && b.facts[i].Atom.Args["symbol"] == sid {
			return true
		}
	}
	return false
}

// addScheduleFacts encodes the deprecation POLICY as data: for every source
// version that actually appears among the symbols, the removal version implied
// by Django's arithmetic policy (removed in the next major .0, or .1 if the
// deprecation was in the last feature release of its series). Plus is_major
// markers for the .0 releases. All homed in the calendar episode.
func (b *builder) addScheduleFacts(snap *Snapshot) {
	sources := map[string]bool{}
	for _, s := range snap.Symbols {
		if s.DeprecatedIn != "" && s.DeprecationProse != "" {
			sources[s.DeprecatedIn] = true
		}
	}
	var srcList []string
	for v := range sources {
		srcList = append(srcList, v)
	}
	sort.Strings(srcList)
	for _, v := range srcList {
		target := removalVersionFor(v)
		if target == "" {
			continue
		}
		srcID := b.versionEntity(v)
		tgtID := b.versionEntity(target)
		fi := b.addFact(world.BaseFact{
			ID:   fmt.Sprintf("f_deptarget_%s_%s", slug(v), slug(target)),
			Atom: world.Atom{Relation: relDeprTarget, Args: map[string]string{"source": srcID, "target": tgtID}},
			From: 0, To: 0, Source: "django:deprecation-policy",
		})
		b.factEp[fi] = epCalendar
	}
	// is_major for every modeled version whose minor is 0.
	var verIDs []string
	for id, e := range b.entities {
		if e.Type == tVersion {
			verIDs = append(verIDs, id)
		}
	}
	sort.Strings(verIDs)
	for _, vid := range verIDs {
		if isMajorVersion(b.nameOf[vid]) {
			fi := b.addFact(world.BaseFact{
				ID:   "f_ismajor_" + vid,
				Atom: world.Atom{Relation: relIsMajor, Args: map[string]string{"version": vid}},
				From: 0, To: 0, Source: "django:release-schedule",
			})
			b.factEp[fi] = epCalendar
		}
	}
}

// removalVersionFor implements Django's stated policy over its versioning
// scheme (feature releases A.0, A.1, A.2 then (A+1).0). A deprecation in A.x is
// removed in (A+1).0, unless A.x is the last feature release of series A
// (x == 2), in which case it is removed in (A+1).1.
func removalVersionFor(v string) string {
	maj, min, ok := parseVersion(v)
	if !ok {
		return ""
	}
	const lastMinorOfSeries = 2 // Django ships A.0, A.1, A.2 then (A+1).0
	if min >= lastMinorOfSeries {
		return fmt.Sprintf("%d.1", maj+1)
	}
	return fmt.Sprintf("%d.0", maj+1)
}

func isMajorVersion(v string) bool {
	_, min, ok := parseVersion(v)
	return ok && min == 0
}

func parseVersion(v string) (maj, min int, ok bool) {
	parts := strings.SplitN(strings.TrimSpace(v), ".", 3)
	if len(parts) < 2 {
		return 0, 0, false
	}
	a, err1 := strconv.Atoi(parts[0])
	c, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return a, c, true
}

// ---------- rules ----------

func (b *builder) addRules() {
	// Availability: v1 says everything shipped is available; v2 (from the 6.1
	// release) adds the removed-marker exception. The supersession retires v1.
	b.rules = append(b.rules,
		world.Rule{
			ID:   ruleAvailableV1,
			Name: "a shipped symbol is available to call",
			Conditions: []world.PatternAtom{
				{Relation: relShipped, Args: map[string]world.Term{"symbol": world.V("S")}},
			},
			Conclusion: world.PatternAtom{Relation: relAvailable, Args: map[string]world.Term{"symbol": world.V("S")}},
			Assert:     true, Authority: 3, IssuedAt: b.dayPolicy, EffectiveFrom: 0, EffectiveTo: 0,
		},
		world.Rule{
			ID:   ruleAvailableV2,
			Name: "a shipped symbol is available to call, unless it has been removed",
			Conditions: []world.PatternAtom{
				{Relation: relShipped, Args: map[string]world.Term{"symbol": world.V("S")}},
			},
			Conclusion: world.PatternAtom{Relation: relAvailable, Args: map[string]world.Term{"symbol": world.V("S")}},
			Exceptions: []world.PatternAtom{{Relation: relRemovedMark, Args: map[string]world.Term{"symbol": world.V("S")}}},
			Assert:     true, Authority: 3, IssuedAt: b.dayRemoval, EffectiveFrom: 0, EffectiveTo: 0,
		},
		// removal_target: policy-mandated removal version (depth 1 composition).
		world.Rule{
			ID:   ruleRemovalTarget,
			Name: "a deprecated symbol's removal version follows from the deprecation policy schedule",
			Conditions: []world.PatternAtom{
				{Relation: relDeprecatedIn, Args: map[string]world.Term{"symbol": world.V("S"), "version": world.V("V")}},
				{Relation: relDeprTarget, Args: map[string]world.Term{"source": world.V("V"), "target": world.V("R")}},
			},
			Conclusion: world.PatternAtom{Relation: relRemovalTarget, Args: map[string]world.Term{"symbol": world.V("S"), "version": world.V("R")}},
			Assert:     true, Authority: 3, IssuedAt: b.dayPolicy, EffectiveFrom: 0, EffectiveTo: 0,
		},
		// blocked_upgrade: removal lands on a major release (depth 2 — chains
		// off the derived removal_target).
		world.Rule{
			ID:   ruleBlockedUp,
			Name: "a symbol whose policy removal lands on a major release blocks that upgrade",
			Conditions: []world.PatternAtom{
				{Relation: relRemovalTarget, Args: map[string]world.Term{"symbol": world.V("S"), "version": world.V("R")}},
				{Relation: relIsMajor, Args: map[string]world.Term{"version": world.V("R")}},
			},
			Conclusion: world.PatternAtom{Relation: relBlockedUpgrade, Args: map[string]world.Term{"symbol": world.V("S"), "version": world.V("R")}},
			Assert:     true, Authority: 3, IssuedAt: b.dayPolicy, EffectiveFrom: 0, EffectiveTo: 0,
		},
	)
	b.sups = append(b.sups, world.Supersession{
		ID: supAvailable, OldRule: ruleAvailableV1, NewRule: ruleAvailableV2, From: b.dayRemoval,
	})
}

// ---------- episodes ----------

func (b *builder) buildEpisodes(snap *Snapshot) []gen.Episode {
	epByID := map[string]*gen.Episode{}
	ensure := func(id string, day int, text string) *gen.Episode {
		if e, ok := epByID[id]; ok {
			return e
		}
		e := &gen.Episode{ID: id, Day: day, Text: text}
		epByID[id] = e
		return e
	}

	// Fixed episodes.
	ensure(epCatalog, b.dayCatalog,
		"Django public API surface catalog: the following symbols ship as part of the framework.")
	ensure(epPolicy, b.dayPolicy, policyEpisodeText(snap))
	calDay, calText := b.dayCalendarText()
	ensure(epCalendar, calDay, calText)
	ensure(epRemoval, b.dayRemoval, removalEpisodeText(b.remProse))

	// Per-symbol deprecation episodes (real prose).
	depIDs := make([]string, 0, len(b.depProse))
	for id := range b.depProse {
		depIDs = append(depIDs, id)
	}
	sort.Strings(depIDs)
	for _, id := range depIDs {
		ensure(id, b.epDay[id], depEpisodeText(b.depVersion[id], b.depProse[id]))
	}

	// Home every fact into its episode as a structured Event (so loom-C2a's
	// structured ingest is exact, exactly as in Rung 0).
	for i := range b.facts {
		f := &b.facts[i]
		epID := b.factEp[i]
		if epID == "" {
			epID = epCatalog
		}
		f.EpisodeID = epID
		e := epByID[epID]
		e.Events = append(e.Events, gen.Event{Kind: gen.EvFact, Day: e.Day, Fact: cloneFact(f), Text: b.factLine(f)})
	}

	// Rules: available v1 + removal_target + blocked_upgrade in the policy
	// episode; available v2 + supersession in the removal episode.
	b.ruleByID(ruleAvailableV1).EpisodeID = epPolicy
	b.ruleByID(ruleRemovalTarget).EpisodeID = epPolicy
	b.ruleByID(ruleBlockedUp).EpisodeID = epPolicy
	pol := epByID[epPolicy]
	for _, rid := range []string{ruleAvailableV1, ruleRemovalTarget, ruleBlockedUp} {
		r := b.ruleByID(rid)
		pol.Events = append(pol.Events, gen.Event{Kind: gen.EvRule, Day: pol.Day, Rule: cloneRule(r), Text: r.Name})
	}
	b.ruleByID(ruleAvailableV2).EpisodeID = epRemoval
	b.sups[0].EpisodeID = epRemoval
	rem := epByID[epRemoval]
	rem.Events = append(rem.Events,
		gen.Event{Kind: gen.EvRule, Day: rem.Day, Rule: cloneRule(b.ruleByID(ruleAvailableV2)), Text: "availability rule revised (removed exception)"},
		gen.Event{Kind: gen.EvSupersession, Day: rem.Day, Supersession: cloneSup(&b.sups[0]), Text: "old availability rule superseded"},
	)

	// Emit sorted by (day, id) — matches the harness's ordering expectations.
	var episodes []gen.Episode
	for _, e := range epByID {
		// Drop episodes that ended up with no events (defensive).
		if len(e.Events) == 0 && e.ID != epCatalog {
			continue
		}
		episodes = append(episodes, *e)
	}
	sort.Slice(episodes, func(i, j int) bool {
		if episodes[i].Day != episodes[j].Day {
			return episodes[i].Day < episodes[j].Day
		}
		return episodes[i].ID < episodes[j].ID
	})
	return episodes
}

func (b *builder) dayCalendarText() (int, string) {
	return b.dayPolicy, "Django release schedule and deprecation timeline: feature releases follow " +
		"A.0, A.1, A.2 and then (A+1).0. A feature deprecated in a given release is removed on the " +
		"schedule fixed by the deprecation policy."
}

func policyEpisodeText(snap *Snapshot) string {
	p := snap.PolicyRuleText
	if p == "" {
		p = "Deprecated features are removed on a fixed schedule tied to the release series."
	}
	return "Django deprecation policy. " + p
}

// depEpisodeText restores the versioned release-note heading the real Django
// docs publish deprecations under ("Django 6.1 release notes — features
// deprecated in 6.1"), so the deprecated_in fact's version argument is
// lexically present in the text (as in the real source), not only in the
// structured payload. Composition is unaffected: the REMOVAL version is still
// derived from the policy + calendar, never stated here.
func depEpisodeText(version, prose string) string {
	prose = strings.TrimSpace(prose)
	if version == "" {
		return prose
	}
	return fmt.Sprintf("Django %s release notes — features deprecated in %s. %s", version, version, prose)
}

func removalEpisodeText(real string) string {
	head := "Features removed in Django 6.1. "
	if strings.TrimSpace(real) == "" {
		return head + "Deprecated features whose removal schedule has elapsed are removed in this release."
	}
	return head + real
}

func appendProse(acc, s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return acc
	}
	if acc == "" {
		return s
	}
	if strings.Contains(acc, s) { // dedupe identical shared removal sentences
		return acc
	}
	return acc + " " + s
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
	return fmt.Sprintf("%s(%s)", rel.Name, strings.Join(parts, ", "))
}

// ---------- queries ----------

func (b *builder) buildQueries(w *world.World, cur, stale *oracle.Closure) *gen.QuerySet {
	qs := &gen.QuerySet{AtDay: b.horizon}
	n := 0
	nextID := func() string { n++; return fmt.Sprintf("qry_%04d", n) }

	// ----- repetition: stated base facts valid at t_eval -----
	baseAtoms := b.validBaseAtoms(cur)
	repTarget, repCount := 12, 0
	for _, rel := range []string{relDeprecatedIn, relReplacedBy} {
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

	// ----- composition: policy-derived removal_target (d1) + blocked_upgrade (d2) -----
	compTarget, compCount := 12, 0
	for _, rel := range []string{relRemovalTarget, relBlockedUpgrade} {
		for _, k := range sortedDerivedKeys(cur, rel) {
			if compCount >= compTarget {
				break
			}
			d := cur.Atoms[k]
			prov := oracle.ProvenanceEpisodes(w, d)
			if len(prov) < 2 {
				continue // enforce the >=2-disjoint-episodes composition invariant
			}
			b.addHolds(qs, nextID(), "composition", d.Atom, true, nil, d.Depth, prov, b.questionText(d.Atom))
			compCount++
			if neg, ok := b.perturb(d.Atom, cur, stale); ok && compCount < compTarget {
				b.addHolds(qs, nextID(), "composition", neg, false, nil, d.Depth, nil, b.questionText(neg))
				compCount++
			}
		}
	}

	// ----- composition find: which symbols are removal-scheduled for 7.0? -----
	b.addFind(qs, nextID(), cur, w, relRemovalTarget, "symbol", "version")

	// ----- revision: removals flip available; deprecated/stable are retained -----
	revTarget, revCount := 8, 0
	// flips: available(removed symbol) — stale true, current false
	for _, k := range sortedStaleKeys(stale, relAvailable) {
		if revCount >= revTarget {
			break
		}
		sd := stale.Atoms[k]
		if cur.Holds(sd.Atom) {
			continue // not a flip
		}
		prov := b.revisionProv(w, sd)
		b.addHolds(qs, nextID(), "revision", sd.Atom, false, boolp(true), sd.Depth, prov, b.questionText(sd.Atom))
		revCount++
	}
	// retained controls: available symbols that did NOT flip (deprecated-not-
	// removed + stable) — stale true, current true. Punish over-revision.
	retained := revTarget
	retCount := 0
	for _, k := range sortedDerivedKeys(cur, relAvailable) {
		if retCount >= retained {
			break
		}
		d := cur.Atoms[k]
		if !stale.Holds(d.Atom) {
			continue
		}
		// Prefer deprecated-not-removed symbols as controls (the sharp case:
		// a system might wrongly mark every deprecated symbol unavailable).
		prov := b.revisionProv(w, d)
		b.addHolds(qs, nextID(), "revision", d.Atom, true, boolp(true), d.Depth, prov, b.questionText(d.Atom))
		retCount++
	}

	return qs
}

// revisionProv attaches the removal-notice episode (where the supersession
// lives) to a derived-atom's provenance — the revision-relevant episode.
func (b *builder) revisionProv(w *world.World, d *oracle.Derivation) []string {
	set := map[string]bool{epRemoval: true}
	for _, p := range oracle.ProvenanceEpisodes(w, d) {
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

func (b *builder) addFind(qs *gen.QuerySet, id string, cur *oracle.Closure, w *world.World, relID, freeSlot, boundSlot string) {
	rel := b.relByID(relID)
	// pick the bound value (a version) that yields the largest answer set.
	best := ""
	bestAns := []string{}
	var bestDepth int
	var bestProv []string
	// candidate bound values = versions appearing as that slot in derived atoms
	candVals := map[string]bool{}
	for _, k := range sortedDerivedKeys(cur, relID) {
		candVals[cur.Atoms[k].Atom.Args[boundSlot]] = true
	}
	var cands []string
	for v := range candVals {
		cands = append(cands, v)
	}
	sort.Strings(cands)
	for _, bv := range cands {
		var answers []string
		var depth int
		var prov []string
		for _, k := range sortedDerivedKeys(cur, relID) {
			d := cur.Atoms[k]
			if d.Atom.Args[boundSlot] != bv {
				continue
			}
			answers = append(answers, d.Atom.Args[freeSlot])
			depth = d.Depth
			prov = oracle.ProvenanceEpisodes(w, d)
		}
		if len(answers) > len(bestAns) {
			best, bestAns, bestDepth, bestProv = bv, answers, depth, prov
		}
	}
	if best == "" || len(bestAns) == 0 {
		return
	}
	sort.Strings(bestAns)
	pattern := world.PatternAtom{Relation: relID, Args: map[string]world.Term{
		freeSlot: world.V("X"), boundSlot: world.C(best),
	}}
	qs.Queries = append(qs.Queries, gen.Query{
		ID: id, Slice: "composition", Type: "find", AtDay: b.horizon,
		Pattern: &pattern, FindSlot: freeSlot, AnswerSet: bestAns,
		Depth: bestDepth, ProvenanceEpisodes: bestProv,
		Text: fmt.Sprintf("On day %d, list every %s such that %s holds for %s %s. Answer with entity IDs.",
			b.horizon, freeSlot, rel.Name, boundSlot, b.nameOf[best]),
	})
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

func (b *builder) validBaseAtoms(cur *oracle.Closure) map[string][]world.Atom {
	out := map[string][]world.Atom{}
	byKey := map[string]world.Atom{}
	keys := map[string][]string{}
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

func (b *builder) writeDataset(dir string, w *world.World, episodes []gen.Episode, qs *gen.QuerySet, snap *Snapshot) error {
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
		"generator":         "apiimport-rung1",
		"package":           snap.Package,
		"pinned_version":    snap.PinnedVersion,
		"pin_date":          snap.PinDate,
		"num_entities":      len(w.Entities),
		"num_facts":         len(w.Facts),
		"num_rules":         len(w.Rules),
		"num_supersessions": len(w.Supersessions),
		"num_episodes":      len(episodes),
		"num_queries":       len(qs.Queries),
		"eval_day":          qs.AtDay,
		"note": "Rung 1: REAL Django API version-history / deprecations (pinned at " +
			snap.PinnedVersion + ", " + snap.PinDate + ", i.e. post model-cutoff) imported into " +
			"the synthworld dataset format so cmd/validate + cmd/harness run unchanged. THE falsifying " +
			"rung (kill criterion §7 binds). Deprecation/removal PROSE is real Django release-note text; " +
			"the deprecation POLICY schedule (removal_target) and the shipped-catalog lines are structured " +
			"scaffolding derived from the real policy. Removal = supersession (available flips); " +
			"removal_target = policy-derived composition (depth 1); blocked_upgrade = depth 2.",
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

func cloneAtom(a world.Atom) *world.Atom { c := cloneAtomVal(a); return &c }

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

func cloneRule(r *world.Rule) *world.Rule { c := *r; return &c }

func cloneSup(s *world.Supersession) *world.Supersession { c := *s; return &c }
