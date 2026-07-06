// Package loom is the compiled-agent-memory substrate, S1 scope: typed
// symbolic store with lifecycle + provenance, structured ingest (easy mode),
// schema inference, and evaluation via the synthworld oracle. See
// docs/loom-substrate-v0-spec.md. Text-mode compilation (S2) plugs in behind
// the same commit path.
//
// S1 deliberately lives in this monorepo and consumes gen.Episode directly;
// the standalone repo split (own event types, journal, Postgres adapter)
// happens when S2 starts.
package loom

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/vaudience/synthworld/gen"
	"github.com/vaudience/synthworld/oracle"
	"github.com/vaudience/synthworld/world"
)

// ---------- Items: payload + lifecycle + provenance ----------

type Lifecycle string

const (
	Proposed    Lifecycle = "proposed"
	Active      Lifecycle = "active"
	Superseded  Lifecycle = "superseded" // set lazily for audit; evaluator applies supersessions itself
	Retracted   Lifecycle = "retracted"
	Quarantined Lifecycle = "quarantined"
)

// Provenance is mandatory on every stored item (spec §3.3).
type Provenance struct {
	EpisodeIDs []string `json:"episode_ids"`
	Confidence float64  `json:"confidence"` // 1.0 for structured ingest
	Extractor  string   `json:"extractor"`  // "structured" | "llm:<model>" | "operator"
}

type StoredFact struct {
	Fact       world.BaseFact `json:"fact"`
	Lifecycle  Lifecycle      `json:"lifecycle"`
	Provenance Provenance     `json:"provenance"`
}

type StoredRule struct {
	Rule       world.Rule `json:"rule"`
	Lifecycle  Lifecycle  `json:"lifecycle"`
	Provenance Provenance `json:"provenance"`
}

type StoredSupersession struct {
	Supersession world.Supersession `json:"supersession"`
	Lifecycle    Lifecycle          `json:"lifecycle"`
	Provenance   Provenance         `json:"provenance"`
}

// ---------- Store ----------

type Store struct {
	Facts         []StoredFact         `json:"facts"`
	Rules         []StoredRule         `json:"rules"`
	Supersessions []StoredSupersession `json:"supersessions"`

	factKeys map[string]bool // dedupe: atom key + interval
	ruleIDs  map[string]bool
	supIDs   map[string]bool

	// caches, invalidated on commit
	view     *world.World
	closures map[closureKey]*oracle.Closure
}

type closureKey struct {
	t     int
	stale bool
}

func NewStore() *Store {
	return &Store{
		factKeys: map[string]bool{},
		ruleIDs:  map[string]bool{},
		supIDs:   map[string]bool{},
		closures: map[closureKey]*oracle.Closure{},
	}
}

func (s *Store) invalidate() {
	s.view = nil
	s.closures = map[closureKey]*oracle.Closure{}
}

// CommitFact stores a fact unless an identical one (atom + interval) exists;
// duplicate provenance is merged.
func (s *Store) CommitFact(f world.BaseFact, prov Provenance) error {
	if len(prov.EpisodeIDs) == 0 {
		return fmt.Errorf("fact %s: provenance is mandatory", f.ID)
	}
	key := fmt.Sprintf("%s|%d|%d", f.Atom.Key(), f.From, f.To)
	if s.factKeys[key] {
		for i := range s.Facts {
			ef := &s.Facts[i]
			if fmt.Sprintf("%s|%d|%d", ef.Fact.Atom.Key(), ef.Fact.From, ef.Fact.To) == key {
				ef.Provenance.EpisodeIDs = mergeIDs(ef.Provenance.EpisodeIDs, prov.EpisodeIDs)
				break
			}
		}
		return nil
	}
	s.factKeys[key] = true
	s.Facts = append(s.Facts, StoredFact{Fact: f, Lifecycle: Active, Provenance: prov})
	s.invalidate()
	return nil
}

func (s *Store) CommitRule(r world.Rule, prov Provenance) error {
	if len(prov.EpisodeIDs) == 0 {
		return fmt.Errorf("rule %s: provenance is mandatory", r.ID)
	}
	if s.ruleIDs[r.ID] {
		return nil
	}
	s.ruleIDs[r.ID] = true
	s.Rules = append(s.Rules, StoredRule{Rule: r, Lifecycle: Active, Provenance: prov})
	s.invalidate()
	return nil
}

func (s *Store) CommitSupersession(sp world.Supersession, prov Provenance) error {
	if len(prov.EpisodeIDs) == 0 {
		return fmt.Errorf("supersession %s: provenance is mandatory", sp.ID)
	}
	if s.supIDs[sp.ID] {
		return nil
	}
	s.supIDs[sp.ID] = true
	s.Supersessions = append(s.Supersessions, StoredSupersession{Supersession: sp, Lifecycle: Active, Provenance: prov})
	s.invalidate()
	return nil
}

func mergeIDs(a, b []string) []string {
	set := map[string]bool{}
	for _, x := range a {
		set[x] = true
	}
	for _, x := range b {
		set[x] = true
	}
	out := make([]string, 0, len(set))
	for x := range set {
		out = append(out, x)
	}
	sort.Strings(out)
	return out
}

// ---------- Structured ingest (easy mode: extraction is a parser) ----------

// IngestReport is the compilation trace for one ingest call (spec §5).
type IngestReport struct {
	Episodes      int      `json:"episodes"`
	Facts         int      `json:"facts"`
	Rules         int      `json:"rules"`
	Supersessions int      `json:"supersessions"`
	Problems      []string `json:"problems,omitempty"`
}

// IngestStructured consumes episodes' structured payloads. Confidence 1.0;
// nothing is silently dropped — every skipped event lands in Problems.
func (s *Store) IngestStructured(episodes []gen.Episode) (*IngestReport, error) {
	rep := &IngestReport{Episodes: len(episodes)}
	for _, ep := range episodes {
		prov := Provenance{EpisodeIDs: []string{ep.ID}, Confidence: 1.0, Extractor: "structured"}
		for _, ev := range ep.Events {
			switch ev.Kind {
			case gen.EvFact:
				if ev.Fact == nil {
					rep.Problems = append(rep.Problems, fmt.Sprintf("%s: fact event without payload", ep.ID))
					continue
				}
				if err := s.CommitFact(*ev.Fact, prov); err != nil {
					return rep, err
				}
				rep.Facts++
			case gen.EvRule:
				if ev.Rule == nil {
					rep.Problems = append(rep.Problems, fmt.Sprintf("%s: rule event without payload", ep.ID))
					continue
				}
				if err := s.CommitRule(*ev.Rule, prov); err != nil {
					return rep, err
				}
				rep.Rules++
			case gen.EvSupersession:
				if ev.Supersession == nil {
					rep.Problems = append(rep.Problems, fmt.Sprintf("%s: supersession event without payload", ep.ID))
					continue
				}
				if err := s.CommitSupersession(*ev.Supersession, prov); err != nil {
					return rep, err
				}
				rep.Supersessions++
			default:
				rep.Problems = append(rep.Problems, fmt.Sprintf("%s: unknown event kind %q", ep.ID, ev.Kind))
			}
		}
	}
	// force schema inference now so structural problems surface at ingest
	// time, not first query
	if _, err := s.worldView(); err != nil {
		return rep, err
	}
	return rep, nil
}

// ---------- Schema inference + world view ----------

// worldView synthesizes a world.World from stored content. Relation strata
// are inferred from the rule dependency graph (a relation never concluded by
// any rule is base; otherwise 1 + max stratum of condition relations).
// Nothing is read from world.json: the store's schema knowledge comes from
// what it ingested, exactly as a production substrate's would.
func (s *Store) worldView() (*world.World, error) {
	if s.view != nil {
		return s.view, nil
	}
	w := &world.World{}
	relSeen := map[string]bool{}
	addRel := func(id string) {
		if !relSeen[id] {
			relSeen[id] = true
		}
	}
	for i := range s.Facts {
		if s.Facts[i].Lifecycle == Active {
			addRel(s.Facts[i].Fact.Atom.Relation)
			w.Facts = append(w.Facts, s.Facts[i].Fact)
		}
	}
	concludedBy := map[string][]*world.Rule{} // relation -> rules concluding it
	for i := range s.Rules {
		if s.Rules[i].Lifecycle != Active && s.Rules[i].Lifecycle != Superseded {
			continue // quarantined/retracted rules never reach the evaluator
		}
		r := &s.Rules[i].Rule
		addRel(r.Conclusion.Relation)
		for _, c := range r.Conditions {
			addRel(c.Relation)
		}
		for _, e := range r.Exceptions {
			addRel(e.Relation)
		}
		concludedBy[r.Conclusion.Relation] = append(concludedBy[r.Conclusion.Relation], r)
		w.Rules = append(w.Rules, *r)
	}
	for i := range s.Supersessions {
		if s.Supersessions[i].Lifecycle == Active {
			w.Supersessions = append(w.Supersessions, s.Supersessions[i].Supersession)
		}
	}

	// stratum inference: memoized DFS over the rule dependency graph
	strata := map[string]int{}
	visiting := map[string]bool{}
	var stratumOf func(rel string) (int, error)
	stratumOf = func(rel string) (int, error) {
		if st, ok := strata[rel]; ok {
			return st, nil
		}
		rules := concludedBy[rel]
		if len(rules) == 0 {
			strata[rel] = 0
			return 0, nil
		}
		if visiting[rel] {
			return 0, fmt.Errorf("cyclic rule dependency through relation %s", rel)
		}
		visiting[rel] = true
		max := 0
		for _, r := range rules {
			for _, c := range r.Conditions {
				st, err := stratumOf(c.Relation)
				if err != nil {
					return 0, err
				}
				if st > max {
					max = st
				}
			}
		}
		visiting[rel] = false
		strata[rel] = max + 1
		return max + 1, nil
	}
	var relIDs []string
	for id := range relSeen {
		relIDs = append(relIDs, id)
	}
	sort.Strings(relIDs)
	for _, id := range relIDs {
		st, err := stratumOf(id)
		if err != nil {
			return nil, err
		}
		w.Relations = append(w.Relations, world.RelationSchema{ID: id, Name: id, Stratum: st})
	}
	s.view = w
	return w, nil
}

// ---------- Operations (spec §6, evaluator = synthworld oracle) ----------

func (s *Store) closureAt(t int, stale bool) (*oracle.Closure, error) {
	key := closureKey{t: t, stale: stale}
	if c, ok := s.closures[key]; ok {
		return c, nil
	}
	w, err := s.worldView()
	if err != nil {
		return nil, err
	}
	c, err := oracle.Eval(w, t, oracle.Options{IgnoreSupersessions: stale})
	if err != nil {
		return nil, err
	}
	s.closures[key] = c
	return c, nil
}

// Holds evaluates a ground atom at time t and returns the derivation (nil
// when the atom does not hold).
func (s *Store) Holds(a world.Atom, t int) (bool, *oracle.Derivation, error) {
	c, err := s.closureAt(t, false)
	if err != nil {
		return false, nil, err
	}
	d := c.Get(a)
	return d != nil, d, nil
}

// Find enumerates satisfiers of a pattern with exactly one free slot.
func (s *Store) Find(p world.PatternAtom, freeSlot string, t int) ([]string, error) {
	c, err := s.closureAt(t, false)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for _, d := range c.Atoms {
		if d.Atom.Relation != p.Relation {
			continue
		}
		match := true
		for slot, term := range p.Args {
			if term.Const != "" && d.Atom.Args[slot] != term.Const {
				match = false
				break
			}
		}
		if match {
			v := d.Atom.Args[freeSlot]
			if v != "" && !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

// Diff reports atoms whose truth differs between t1 and t2 (spec §6).
func (s *Store) Diff(t1, t2 int) (gained, lost []world.Atom, err error) {
	c1, err := s.closureAt(t1, false)
	if err != nil {
		return nil, nil, err
	}
	c2, err := s.closureAt(t2, false)
	if err != nil {
		return nil, nil, err
	}
	for k, d := range c2.Atoms {
		if _, ok := c1.Atoms[k]; !ok {
			gained = append(gained, d.Atom)
		}
	}
	for k, d := range c1.Atoms {
		if _, ok := c2.Atoms[k]; !ok {
			lost = append(lost, d.Atom)
		}
	}
	return gained, lost, nil
}

// Stats: store health (spec §6). Firing ratio here is atoms-per-relation
// (the entity-pool denominator of synthworld isn't knowable from ingested
// content alone — reported as counts, thresholding is the caller's policy).
type Stats struct {
	Facts, Rules, Supersessions int            `json:"-"`
	FactCount                   int            `json:"facts"`
	RuleCount                   int            `json:"rules"`
	SupersessionCount           int            `json:"supersessions"`
	DerivedAtomsPerRelation     map[string]int `json:"derived_atoms_per_relation"`
	Quarantined                 int            `json:"quarantined"`
}

func (s *Store) StatsAt(t int) (*Stats, error) {
	c, err := s.closureAt(t, false)
	if err != nil {
		return nil, err
	}
	st := &Stats{
		FactCount:               len(s.Facts),
		RuleCount:               len(s.Rules),
		SupersessionCount:       len(s.Supersessions),
		DerivedAtomsPerRelation: map[string]int{},
	}
	for _, d := range c.Atoms {
		if d.RuleID != "" {
			st.DerivedAtomsPerRelation[d.Atom.Relation]++
		}
	}
	for _, r := range s.Rules {
		if r.Lifecycle == Quarantined {
			st.Quarantined++
		}
	}
	return st, nil
}

// ---------- Persistence (S1: snapshot; journal comes with the repo split) ----------

func (s *Store) Save(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(s)
}

func Load(path string) (*Store, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s := NewStore()
	if err := json.Unmarshal(raw, s); err != nil {
		return nil, err
	}
	for _, sf := range s.Facts {
		s.factKeys[fmt.Sprintf("%s|%d|%d", sf.Fact.Atom.Key(), sf.Fact.From, sf.Fact.To)] = true
	}
	for _, sr := range s.Rules {
		s.ruleIDs[sr.Rule.ID] = true
	}
	for _, sp := range s.Supersessions {
		s.supIDs[sp.Supersession.ID] = true
	}
	return s, nil
}
