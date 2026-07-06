package harness

import (
	"github.com/vaudience/synthworld/gen"
	"github.com/vaudience/synthworld/oracle"
	"github.com/vaudience/synthworld/world"
)

// Diagnostic conditions: no LLM involved. Their purpose is to prove the
// instrument discriminates. Expected pattern:
//
//	always-true    aces positives, fails all negatives (calibration floor)
//	always-false   the mirror floor
//	oracle         100% everywhere (ceiling; anything less is a harness bug)
//	stale-oracle   100% everywhere EXCEPT revision flips (0%) — the
//	               revision slice detects exactly revision-blindness
//	episode-grep   aces repetition, answers false to every composition
//	               positive — episodic memory without inference
//
// If a run does not reproduce this pattern, fix the harness before
// measuring anything real.

// ---------- floors ----------

type ConstCondition struct {
	Value bool
}

func (c *ConstCondition) Name() string {
	if c.Value {
		return "always-true"
	}
	return "always-false"
}
func (c *ConstCondition) Ingest([]gen.Episode) error                  { return nil }
func (c *ConstCondition) AnswerHolds(SanitizedQuery) (bool, error)    { return c.Value, nil }
func (c *ConstCondition) AnswerFind(SanitizedQuery) ([]string, error) { return nil, nil }

// ---------- ceiling ----------

// OracleCondition answers from the true world: the harness sanity ceiling.
// It cheats by construction and must score 100%.
type OracleCondition struct {
	W     *world.World
	Stale bool // ignore supersessions: the revision-blind variant
	cl    map[int]*oracle.Closure
}

func (o *OracleCondition) Name() string {
	if o.Stale {
		return "stale-oracle"
	}
	return "oracle"
}

func (o *OracleCondition) Ingest([]gen.Episode) error {
	o.cl = map[int]*oracle.Closure{}
	return nil
}

func (o *OracleCondition) closureAt(t int) (*oracle.Closure, error) {
	if c, ok := o.cl[t]; ok {
		return c, nil
	}
	c, err := oracle.Eval(o.W, t, oracle.Options{IgnoreSupersessions: o.Stale})
	if err != nil {
		return nil, err
	}
	o.cl[t] = c
	return c, nil
}

func (o *OracleCondition) AnswerHolds(q SanitizedQuery) (bool, error) {
	c, err := o.closureAt(q.AtDay)
	if err != nil {
		return false, err
	}
	return c.Holds(*q.Atom), nil
}

func (o *OracleCondition) AnswerFind(q SanitizedQuery) ([]string, error) {
	c, err := o.closureAt(q.AtDay)
	if err != nil {
		return nil, err
	}
	rel := o.W.RelationByID(q.Pattern.Relation)
	var out []string
	for _, d := range c.Atoms {
		if d.Atom.Relation != q.Pattern.Relation {
			continue
		}
		match := true
		for _, s := range rel.Slots {
			t := q.Pattern.Args[s.Name]
			if t.Const != "" && d.Atom.Args[s.Name] != t.Const {
				match = false
				break
			}
		}
		if match {
			out = append(out, d.Atom.Args[q.FindSlot])
		}
	}
	return out, nil
}

// ---------- episodic memory without inference ----------

// GrepCondition remembers every base fact stated in episodes (structured
// payloads) and answers holds by validity-aware lookup. No rules, no
// inference: the caricature of retrieval-only memory. It should ace
// repetition and answer "false" to every derived (composition) positive.
type GrepCondition struct {
	facts []world.BaseFact
}

func (g *GrepCondition) Name() string { return "episode-grep" }

func (g *GrepCondition) Ingest(episodes []gen.Episode) error {
	for _, ep := range episodes {
		for _, ev := range ep.Events {
			if ev.Kind == gen.EvFact && ev.Fact != nil {
				g.facts = append(g.facts, *ev.Fact)
			}
		}
	}
	return nil
}

func (g *GrepCondition) AnswerHolds(q SanitizedQuery) (bool, error) {
	key := q.Atom.Key()
	for _, f := range g.facts {
		if f.Atom.Key() == key && f.ValidAt(q.AtDay) {
			return true, nil
		}
	}
	return false, nil
}

func (g *GrepCondition) AnswerFind(q SanitizedQuery) ([]string, error) {
	// derived relations never appear as base facts: empty is its honest answer
	var out []string
	rel := map[string]bool{}
	for _, f := range g.facts {
		if f.Atom.Relation != q.Pattern.Relation || !f.ValidAt(q.AtDay) {
			continue
		}
		match := true
		for slot, t := range q.Pattern.Args {
			if t.Const != "" && f.Atom.Args[slot] != t.Const {
				match = false
				break
			}
		}
		if match && !rel[f.Atom.Args[q.FindSlot]] {
			rel[f.Atom.Args[q.FindSlot]] = true
			out = append(out, f.Atom.Args[q.FindSlot])
		}
	}
	return out, nil
}
