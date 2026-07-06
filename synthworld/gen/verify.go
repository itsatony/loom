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

	// closures per distinct query day (v0.1: a single day, but stay general)
	days := map[int]bool{}
	for _, q := range queries {
		days[q.AtDay] = true
	}
	closures := map[int]*oracle.Closure{}
	stales := map[int]*oracle.Closure{}
	for d := range days {
		cl, err := oracle.Eval(&w, d, oracle.Options{RevealedBy: revealedBy})
		if err != nil {
			return nil, err
		}
		st, err := oracle.Eval(&w, d, oracle.Options{IgnoreSupersessions: true, RevealedBy: revealedBy})
		if err != nil {
			return nil, err
		}
		closures[d] = cl
		stales[d] = st
	}

	rep := &VerifyReport{Checked: map[string]int{}}
	problem := func(format string, args ...any) {
		rep.Problems = append(rep.Problems, fmt.Sprintf(format, args...))
	}
	for _, q := range queries {
		cl := closures[q.AtDay]
		st := stales[q.AtDay]
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
			if q.Slice == "revision" {
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
			var got []string
			rel := w.RelationByID(q.Pattern.Relation)
			for _, d := range cl.Atoms {
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
					got = append(got, d.Atom.Args[q.FindSlot])
				}
			}
			sort.Strings(got)
			if !equalStringSlices(got, q.AnswerSet) {
				problem("FAIL %s: find answer set mismatch\n  oracle: %v\n  stored: %v", q.ID, got, q.AnswerSet)
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
