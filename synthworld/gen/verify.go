package gen

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/vaudience/synthworld/oracle"
	"github.com/vaudience/synthworld/world"
)

// VerifyReport is the result of VerifyDataset: per-slice/type counts of
// checked queries plus every problem found, in encounter order. Problems are
// pre-formatted strings (the exact lines cmd/validate prints).
type VerifyReport struct {
	Checked  map[string]int
	Problems []string
}

// OK reports whether the dataset passed every check.
func (r *VerifyReport) OK() bool { return len(r.Problems) == 0 }

// VerifyDataset re-loads a generated dataset and independently verifies the
// guarantees in DESIGN.md §5: ground truth reproduces under the oracle,
// revision queries flip / retain as labeled, composition provenance spans
// >= 2 episodes, and no query depends on knowledge revealed after its day.
// This is the single verification implementation, shared by cmd/validate
// (CLI) and cmd/batch (in-process gating).
func VerifyDataset(dir string) (*VerifyReport, error) {
	var w world.World
	if err := readJSONFile(filepath.Join(dir, "world.json"), &w); err != nil {
		return nil, err
	}
	if err := w.Validate(); err != nil {
		return nil, fmt.Errorf("world invalid: %w", err)
	}

	epDay := map[string]int{}
	if err := readJSONLFile(filepath.Join(dir, "episodes.jsonl"), func(raw []byte) error {
		var ep Episode
		if err := json.Unmarshal(raw, &ep); err != nil {
			return err
		}
		epDay[ep.ID] = ep.Day
		return nil
	}); err != nil {
		return nil, err
	}

	var queries []Query
	if err := readJSONLFile(filepath.Join(dir, "queries.jsonl"), func(raw []byte) error {
		var q Query
		if err := json.Unmarshal(raw, &q); err != nil {
			return err
		}
		queries = append(queries, q)
		return nil
	}); err != nil {
		return nil, err
	}

	revealedBy := func(epID string) (int, bool) {
		d, ok := epDay[epID]
		return d, ok
	}

	// closures per distinct (query day, frame) — lazy: frames datasets query
	// many frames, v0 datasets exactly one
	type clKey struct {
		day   int
		frame string
	}
	closures := map[clKey]*oracle.Closure{}
	getCl := func(day int, frame string) (*oracle.Closure, error) {
		k := clKey{day, world.NormFrame(frame)}
		if c, ok := closures[k]; ok {
			return c, nil
		}
		c, err := oracle.Eval(&w, day, oracle.Options{RevealedBy: revealedBy, Frame: k.frame})
		if err != nil {
			return nil, err
		}
		closures[k] = c
		return c, nil
	}
	stales := map[int]*oracle.Closure{}
	getStale := func(day int) (*oracle.Closure, error) {
		if c, ok := stales[day]; ok {
			return c, nil
		}
		c, err := oracle.Eval(&w, day, oracle.Options{IgnoreSupersessions: true, RevealedBy: revealedBy})
		if err != nil {
			return nil, err
		}
		stales[day] = c
		return c, nil
	}
	// frame universe for which_frames queries: actual + every declared frame
	frameIDs := []string{world.ActualFrame}
	{
		var rest []string
		for _, f := range w.Frames {
			rest = append(rest, f.ID)
		}
		sort.Strings(rest)
		frameIDs = append(frameIDs, rest...)
	}

	rep := &VerifyReport{Checked: map[string]int{}}
	problem := func(format string, args ...any) {
		rep.Problems = append(rep.Problems, fmt.Sprintf(format, args...))
	}
	// guarantee 5b: every support in a query's derivation is homed on the
	// query frame's ancestor chain (its visibility cone)
	var coneCheck func(q *Query, d *oracle.Derivation, cone map[string]world.ConeMember)
	coneCheck = func(q *Query, d *oracle.Derivation, cone map[string]world.ConeMember) {
		if d == nil {
			return
		}
		if _, ok := cone[world.NormFrame(d.Frame)]; !ok {
			problem("FAIL %s: trace support (frame %s) outside the cone of query frame %s",
				q.ID, world.NormFrame(d.Frame), world.NormFrame(q.Frame))
		}
		for _, s := range d.Supports {
			coneCheck(q, s, cone)
		}
	}
	for i := range queries {
		q := queries[i]
		cl, err := getCl(q.AtDay, q.Frame)
		if err != nil {
			return nil, err
		}
		switch q.Type {
		case "holds":
			got := cl.Holds(*q.Atom)
			if q.Answer == nil {
				problem("FAIL %s: holds query without answer", q.ID)
				continue
			}
			if got != *q.Answer {
				problem("FAIL %s [%s]: oracle says %v, ground truth says %v for %s",
					q.ID, q.Slice, got, *q.Answer, q.Atom.Key())
			}
			if got {
				cone, cerr := w.Cone(world.NormFrame(q.Frame), q.AtDay)
				if cerr != nil {
					problem("FAIL %s: cone: %v", q.ID, cerr)
				} else {
					coneCheck(&q, cl.Get(*q.Atom), cone)
				}
			}
			if q.Slice == "revision" {
				st, serr := getStale(q.AtDay)
				if serr != nil {
					return nil, serr
				}
				if q.StaleAnswer == nil {
					problem("FAIL %s: revision query without stale answer", q.ID)
				} else if st.Holds(*q.Atom) != *q.StaleAnswer {
					problem("FAIL %s: stale oracle says %v, recorded stale answer %v",
						q.ID, st.Holds(*q.Atom), *q.StaleAnswer)
				}
			}
			if q.Slice == "composition" && q.Answer != nil && *q.Answer {
				if len(q.ProvenanceEpisodes) < 2 {
					problem("FAIL %s: composition positive with provenance %d < 2 episodes",
						q.ID, len(q.ProvenanceEpisodes))
				}
			}
		case "find":
			rel := w.RelationByID(q.Pattern.Relation)
			matchValues := func(c *oracle.Closure) []string {
				var vals []string
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
						vals = append(vals, d.Atom.Args[q.FindSlot])
					}
				}
				sort.Strings(vals)
				return vals
			}
			if len(q.FramesScope) > 0 {
				// ideation: (value, frame) pairs across the explicit scope
				var got []FramedValue
				for _, fid := range q.FramesScope {
					fcl, ferr := getCl(q.AtDay, fid)
					if ferr != nil {
						return nil, ferr
					}
					seenVals := map[string]bool{}
					for _, v := range matchValues(fcl) {
						if !seenVals[v] {
							seenVals[v] = true
							got = append(got, FramedValue{Value: v, Frame: fid})
						}
					}
				}
				sort.Slice(got, func(a, b int) bool {
					if got[a].Frame != got[b].Frame {
						return got[a].Frame < got[b].Frame
					}
					return got[a].Value < got[b].Value
				})
				if !equalFramedValues(got, q.AnswerFramed) {
					problem("FAIL %s: framed find answer mismatch\n  oracle: %v\n  stored: %v", q.ID, got, q.AnswerFramed)
				}
			} else {
				got := matchValues(cl)
				if !equalStringSlices(got, q.AnswerSet) {
					problem("FAIL %s: find answer set mismatch\n  oracle: %v\n  stored: %v", q.ID, got, q.AnswerSet)
				}
			}
		case "which_frames":
			var got []string
			for _, fid := range frameIDs {
				fcl, ferr := getCl(q.AtDay, fid)
				if ferr != nil {
					return nil, ferr
				}
				if fcl.Holds(*q.Atom) {
					got = append(got, fid)
				}
			}
			sort.Strings(got)
			if !equalStringSlices(got, q.AnswerFrames) {
				problem("FAIL %s: which_frames answer mismatch\n  oracle: %v\n  stored: %v", q.ID, got, q.AnswerFrames)
			}
		default:
			problem("FAIL %s: unknown query type %s", q.ID, q.Type)
		}
		// leak check: provenance episodes revealed by query day
		for _, ep := range q.ProvenanceEpisodes {
			d, ok := epDay[ep]
			if !ok {
				problem("FAIL %s: provenance episode %s not found", q.ID, ep)
			} else if d > q.AtDay {
				problem("FAIL %s: provenance episode %s revealed on day %d after query day %d",
					q.ID, ep, d, q.AtDay)
			}
		}
		rep.Checked[q.Slice+"/"+q.Type]++
	}
	return rep, nil
}

func equalFramedValues(a, b []FramedValue) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func readJSONFile(path string, v any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}

func readJSONLFile(path string, handle func([]byte) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		if err := handle(line); err != nil {
			return err
		}
	}
	return sc.Err()
}
