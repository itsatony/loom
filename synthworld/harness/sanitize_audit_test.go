package harness

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/vaudience/synthworld/gen"
)

// D7 sanitization audit (masterplan §3): the whole campaign is invalid if a
// condition can see ground truth. These tests fail if sanitize() ever leaks
// answers, slice labels, traces, or provenance — including via fields added
// to SanitizedQuery in the future (the allowlist below must be extended
// deliberately, with a justification like the one on ID).

// sanitizedAllowlist is every field SanitizedQuery is permitted to carry.
// ID is allowed because it is an opaque generation-order index (qry_NNNN)
// carrying no answer information; D6 uses it harness-side.
var sanitizedAllowlist = map[string]bool{
	"ID":       true,
	"Type":     true,
	"AtDay":    true,
	"Atom":     true,
	"Pattern":  true,
	"FindSlot": true,
	"Text":     true,
}

func TestSanitizedQueryFieldAllowlist(t *testing.T) {
	typ := reflect.TypeOf(SanitizedQuery{})
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		if !sanitizedAllowlist[name] {
			t.Errorf("SanitizedQuery gained field %q not on the sanitization allowlist; "+
				"adding condition-visible fields requires a leakage review", name)
		}
	}
}

func loadSampleQueries(t *testing.T) []gen.Query {
	t.Helper()
	path := filepath.Join("..", "sample-dataset", "queries.jsonl")
	f, err := os.Open(path)
	if err != nil {
		t.Skipf("sample dataset not available: %v", err)
	}
	defer f.Close()
	var queries []gen.Query
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var q gen.Query
		if err := json.Unmarshal([]byte(line), &q); err != nil {
			t.Fatalf("parse query: %v", err)
		}
		queries = append(queries, q)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(queries) == 0 {
		t.Fatal("sample dataset has no queries")
	}
	return queries
}

func TestSanitizeStripsGroundTruthFields(t *testing.T) {
	for _, q := range loadSampleQueries(t) {
		sq := sanitize(q)
		// Field-level: the survivors must be the pass-through fields, verbatim.
		if sq.ID != q.ID || sq.Type != q.Type || sq.AtDay != q.AtDay ||
			sq.FindSlot != q.FindSlot || sq.Text != q.Text {
			t.Fatalf("%s: pass-through fields mutated by sanitize()", q.ID)
		}
		// Serialized-form audit: marshal the sanitized query and assert no
		// forbidden key or forbidden value survives, so the check keeps
		// working even if the struct changes shape.
		raw, err := json.Marshal(sq)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		for key := range m {
			if !sanitizedAllowlist[key] {
				t.Errorf("%s: serialized SanitizedQuery contains non-allowlisted key %q", q.ID, key)
			}
		}
		s := string(raw)
		if q.Trace != "" && len(q.Trace) >= 12 && strings.Contains(s, q.Trace) {
			t.Errorf("%s: derivation trace leaked into sanitized query", q.ID)
		}
		for _, ep := range q.ProvenanceEpisodes {
			if strings.Contains(s, ep) {
				t.Errorf("%s: provenance episode %s leaked into sanitized query", q.ID, ep)
			}
		}
		if q.Slice != "" && strings.Contains(s, `"`+q.Slice+`"`) {
			// slice labels are lowercase words; a quoted exact match in the
			// JSON would mean a field carries the label
			t.Errorf("%s: slice label %q leaked into sanitized query", q.ID, q.Slice)
		}
	}
}

var opaqueQueryID = regexp.MustCompile(`^qry_\d{4,}$`)

func TestSanitizedIDIsOpaque(t *testing.T) {
	for _, q := range loadSampleQueries(t) {
		sq := sanitize(q)
		if !opaqueQueryID.MatchString(sq.ID) {
			t.Errorf("query ID %q is not an opaque generation-order index; "+
				"D6's ID passthrough is only leak-free while IDs stay opaque", sq.ID)
		}
	}
}
