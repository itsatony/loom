// Frame diagnostic conditions (MASTERPLAN §9.6.5): LLM-free oracles that
// each break frame semantics in exactly one pre-registered way, proving the
// frame slices discriminate. Expected pattern:
//
//	frame-oracle    100% everywhere (ceiling; anything less = harness bug)
//	mono-world      everything dumped into one frameless world: fails the
//	                contamination traps and misattribution; believes
//	                predictions at claim time (premature-belief side)
//	isolationist    no inheritance: fails inherited isolation positives,
//	                pinning, scenario chains; passes contamination
//	literalist      frames correct, but quoted/sarcastic literal content
//	                asserted in actual: fails the speech-act sub-slice only
//
// Exact per-cell counts are frozen into the committed table on the first
// gated seed batch, BEFORE any LLM-condition run. If the pattern breaks
// after a harness change, fix the harness before measuring anything.
package harness

import (
	"fmt"
	"sort"
	"sync"

	"github.com/vaudience/synthworld/gen"
	"github.com/vaudience/synthworld/oracle"
	"github.com/vaudience/synthworld/world"
)

// frameCloser caches per-(frame, day) closures of a fixed world.
type frameCloser struct {
	w  *world.World
	mu sync.Mutex
	cl map[string]*oracle.Closure
}

func newFrameCloser(w *world.World) *frameCloser {
	return &frameCloser{w: w, cl: map[string]*oracle.Closure{}}
}

func (fc *frameCloser) closure(frame string, day int) (*oracle.Closure, error) {
	frame = world.NormFrame(frame)
	key := fmt.Sprintf("%s@%d", frame, day)
	fc.mu.Lock()
	defer fc.mu.Unlock()
	if c, ok := fc.cl[key]; ok {
		return c, nil
	}
	c, err := oracle.Eval(fc.w, day, oracle.Options{Frame: frame})
	if err != nil {
		return nil, err
	}
	fc.cl[key] = c
	return c, nil
}

// frameUniverse is actual + every declared frame, sorted, actual first.
func frameUniverse(w *world.World) []string {
	ids := []string{world.ActualFrame}
	var rest []string
	for _, f := range w.Frames {
		rest = append(rest, f.ID)
	}
	sort.Strings(rest)
	return append(ids, rest...)
}

// matchValuesIn collects the free-slot values of pattern matches in a
// closure, sorted, deduped.
func matchValuesIn(w *world.World, cl *oracle.Closure, pattern *world.PatternAtom, findSlot string) []string {
	rel := w.RelationByID(pattern.Relation)
	seen := map[string]bool{}
	var out []string
	for _, d := range cl.Atoms {
		if d.Atom.Relation != pattern.Relation {
			continue
		}
		match := true
		for _, s := range rel.Slots {
			t := pattern.Args[s.Name]
			if t.Const != "" && d.Atom.Args[s.Name] != t.Const {
				match = false
				break
			}
		}
		if match && !seen[d.Atom.Args[findSlot]] {
			seen[d.Atom.Args[findSlot]] = true
			out = append(out, d.Atom.Args[findSlot])
		}
	}
	sort.Strings(out)
	return out
}

// ---------- frame-oracle: the ceiling ----------

// FrameOracleCondition answers every query from the true frame closure.
// Cheats by construction; must score 100% on every slice.
type FrameOracleCondition struct {
	W     *world.World
	Label string // "frame-oracle" by default; embedders override
	fc    *frameCloser
}

func (o *FrameOracleCondition) Name() string {
	if o.Label != "" {
		return o.Label
	}
	return "frame-oracle"
}

func (o *FrameOracleCondition) Ingest([]gen.Episode) error {
	o.fc = newFrameCloser(o.W)
	return nil
}

func (o *FrameOracleCondition) AnswerHolds(q SanitizedQuery) (bool, error) {
	cl, err := o.fc.closure(q.Frame, q.AtDay)
	if err != nil {
		return false, err
	}
	return cl.Holds(*q.Atom), nil
}

func (o *FrameOracleCondition) AnswerFind(q SanitizedQuery) ([]string, error) {
	cl, err := o.fc.closure(q.Frame, q.AtDay)
	if err != nil {
		return nil, err
	}
	return matchValuesIn(o.W, cl, q.Pattern, q.FindSlot), nil
}

func (o *FrameOracleCondition) AnswerWhichFrames(q SanitizedQuery) ([]string, error) {
	var out []string
	for _, fid := range frameUniverse(o.W) {
		cl, err := o.fc.closure(fid, q.AtDay)
		if err != nil {
			return nil, err
		}
		if cl.Holds(*q.Atom) {
			out = append(out, fid)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (o *FrameOracleCondition) AnswerFindFramed(q SanitizedQuery) ([]gen.FramedValue, error) {
	var out []gen.FramedValue
	for _, fid := range q.FramesScope {
		cl, err := o.fc.closure(fid, q.AtDay)
		if err != nil {
			return nil, err
		}
		for _, v := range matchValuesIn(o.W, cl, q.Pattern, q.FindSlot) {
			out = append(out, gen.FramedValue{Value: v, Frame: fid})
		}
	}
	return out, nil
}

// ---------- mono-world: everything in one frameless world ----------

// MonoWorldCondition believes every fact/rule/supersession simpliciter:
// frames dropped, everything rehomed to actual, block records discarded
// (a frameless store cannot represent scoped removal). This is the
// frame-DAG failure mode: fiction, quotes, scenario deltas, and prediction
// claims all become beliefs about the actual world.
type MonoWorldCondition struct {
	W  *world.World
	fc *frameCloser
}

func (m *MonoWorldCondition) Name() string { return "mono-world" }

func (m *MonoWorldCondition) Ingest([]gen.Episode) error {
	mono := &world.World{
		Seed: m.W.Seed, Horizon: m.W.Horizon,
		Types: m.W.Types, Entities: m.W.Entities, Relations: m.W.Relations,
	}
	for _, f := range m.W.Facts {
		if f.Block {
			continue
		}
		f.FrameID = ""
		mono.Facts = append(mono.Facts, f)
	}
	for _, r := range m.W.Rules {
		r.FrameID = ""
		mono.Rules = append(mono.Rules, r)
	}
	for _, s := range m.W.Supersessions {
		s.FrameID = ""
		mono.Supersessions = append(mono.Supersessions, s)
	}
	m.fc = newFrameCloser(mono)
	return nil
}

func (m *MonoWorldCondition) AnswerHolds(q SanitizedQuery) (bool, error) {
	cl, err := m.fc.closure("", q.AtDay) // one world: the frame is ignored
	if err != nil {
		return false, err
	}
	return cl.Holds(*q.Atom), nil
}

func (m *MonoWorldCondition) AnswerFind(q SanitizedQuery) ([]string, error) {
	cl, err := m.fc.closure("", q.AtDay)
	if err != nil {
		return nil, err
	}
	return matchValuesIn(m.fc.w, cl, q.Pattern, q.FindSlot), nil
}

func (m *MonoWorldCondition) AnswerWhichFrames(q SanitizedQuery) ([]string, error) {
	holds, err := m.AnswerHolds(q)
	if err != nil || !holds {
		return nil, err
	}
	return []string{world.ActualFrame}, nil
}

func (m *MonoWorldCondition) AnswerFindFramed(q SanitizedQuery) ([]gen.FramedValue, error) {
	vals, err := m.AnswerFind(q)
	if err != nil {
		return nil, err
	}
	var out []gen.FramedValue
	for _, v := range vals {
		out = append(out, gen.FramedValue{Value: v, Frame: world.ActualFrame})
	}
	return out, nil
}

// ---------- isolationist: no inheritance ----------

// IsolationistCondition gives every frame ONLY its own homed items: no
// visibility cone, no pinning, no delta overlay onto inherited facts.
// Sterile: safe against contamination, blind to everything inheritance
// provides.
type IsolationistCondition struct {
	W  *world.World
	mu sync.Mutex
	fc map[string]*frameCloser // per home frame: an isolated sub-world
}

func (c *IsolationistCondition) Name() string { return "isolationist" }

func (c *IsolationistCondition) Ingest([]gen.Episode) error {
	c.fc = map[string]*frameCloser{}
	return nil
}

func (c *IsolationistCondition) closer(frame string) *frameCloser {
	frame = world.NormFrame(frame)
	c.mu.Lock()
	defer c.mu.Unlock()
	if fc, ok := c.fc[frame]; ok {
		return fc
	}
	iso := &world.World{
		Seed: c.W.Seed, Horizon: c.W.Horizon,
		Types: c.W.Types, Entities: c.W.Entities, Relations: c.W.Relations,
	}
	for _, f := range c.W.Facts {
		if world.NormFrame(f.FrameID) != frame || f.Block {
			continue
		}
		f.FrameID = ""
		iso.Facts = append(iso.Facts, f)
	}
	for _, r := range c.W.Rules {
		if world.NormFrame(r.FrameID) != frame {
			continue
		}
		r.FrameID = ""
		iso.Rules = append(iso.Rules, r)
	}
	for _, s := range c.W.Supersessions {
		if world.NormFrame(s.FrameID) != frame {
			continue
		}
		s.FrameID = ""
		// a supersession homed here may target a rule homed elsewhere —
		// without the rule it is inert, which is exactly isolationist
		if iso.RuleByID(s.OldRule) == nil || iso.RuleByID(s.NewRule) == nil {
			continue
		}
		iso.Supersessions = append(iso.Supersessions, s)
	}
	fc := newFrameCloser(iso)
	c.fc[frame] = fc
	return fc
}

func (c *IsolationistCondition) AnswerHolds(q SanitizedQuery) (bool, error) {
	cl, err := c.closer(q.Frame).closure("", q.AtDay)
	if err != nil {
		return false, err
	}
	return cl.Holds(*q.Atom), nil
}

func (c *IsolationistCondition) AnswerFind(q SanitizedQuery) ([]string, error) {
	fc := c.closer(q.Frame)
	cl, err := fc.closure("", q.AtDay)
	if err != nil {
		return nil, err
	}
	return matchValuesIn(fc.w, cl, q.Pattern, q.FindSlot), nil
}

func (c *IsolationistCondition) AnswerWhichFrames(q SanitizedQuery) ([]string, error) {
	var out []string
	for _, fid := range frameUniverse(c.W) {
		cl, err := c.closer(fid).closure("", q.AtDay)
		if err != nil {
			return nil, err
		}
		if cl.Holds(*q.Atom) {
			out = append(out, fid)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (c *IsolationistCondition) AnswerFindFramed(q SanitizedQuery) ([]gen.FramedValue, error) {
	var out []gen.FramedValue
	for _, fid := range q.FramesScope {
		fc := c.closer(fid)
		cl, err := fc.closure("", q.AtDay)
		if err != nil {
			return nil, err
		}
		for _, v := range matchValuesIn(fc.w, cl, q.Pattern, q.FindSlot) {
			out = append(out, gen.FramedValue{Value: v, Frame: fid})
		}
	}
	return out, nil
}

// ---------- literalist: speech acts taken literally ----------

// LiteralistCondition handles the frame DAG correctly but asserts every
// quoted and sarcastic literal into actual: the assertion-type failure
// mode, separated from frame-DAG failures. It reads AssertionType from the
// episode stream (ground truth in easy mode) and rehomes those facts to
// actual on top of the true world.
type LiteralistCondition struct {
	W     *world.World
	inner FrameOracleCondition
}

func (c *LiteralistCondition) Name() string { return "literalist" }

func (c *LiteralistCondition) Ingest(episodes []gen.Episode) error {
	lit := *c.W
	lit.Facts = append([]world.BaseFact{}, c.W.Facts...)
	n := 0
	for _, ep := range episodes {
		for _, ev := range ep.Events {
			if ev.Kind != gen.EvFact || ev.Fact == nil || ev.AssertionType == "" {
				continue
			}
			f := *ev.Fact
			f.FrameID = ""
			f.Block = false
			f.ID = fmt.Sprintf("lit_%03d_%s", n, f.ID)
			n++
			lit.Facts = append(lit.Facts, f)
		}
	}
	c.inner = FrameOracleCondition{W: &lit, Label: "literalist"}
	return c.inner.Ingest(nil)
}

func (c *LiteralistCondition) AnswerHolds(q SanitizedQuery) (bool, error) {
	return c.inner.AnswerHolds(q)
}

func (c *LiteralistCondition) AnswerFind(q SanitizedQuery) ([]string, error) {
	return c.inner.AnswerFind(q)
}

func (c *LiteralistCondition) AnswerWhichFrames(q SanitizedQuery) ([]string, error) {
	return c.inner.AnswerWhichFrames(q)
}

func (c *LiteralistCondition) AnswerFindFramed(q SanitizedQuery) ([]gen.FramedValue, error) {
	return c.inner.AnswerFindFramed(q)
}
