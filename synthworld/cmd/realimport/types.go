package main

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"time"
)

// ---------- Snapshot: the pinned, self-contained fetch result ----------
//
// A snapshot is the ONLY input to the (network-free) build path. Same
// snapshot + same binary => byte-identical dataset. The fetch path writes it
// once from live Wikidata + Wikipedia; nothing in the build path touches the
// network, time.Now, or randomness.

type Snapshot struct {
	Note      string        `json:"note"`
	Countries []SnapCountry `json:"countries"`
}

type SnapCountry struct {
	QID        string       `json:"qid"`
	Name       string       `json:"name"`
	OfficeQID  string       `json:"office_qid"`
	OfficeName string       `json:"office_name"`
	Holders    []SnapHolder `json:"holders"`
}

type SnapHolder struct {
	QID       string     `json:"qid"`
	Name      string     `json:"name"`
	Enwiki    string     `json:"enwiki"` // human article title (spaces, no url-encoding)
	Terms     []SnapTerm `json:"terms"`
	SpouseQID string     `json:"spouse_qid,omitempty"`
	Spouse    string     `json:"spouse,omitempty"`
	PartyQID  string     `json:"party_qid,omitempty"`
	Party     string     `json:"party,omitempty"`
	Extract   string     `json:"extract"` // real Wikipedia summary prose
}

type SnapTerm struct {
	Start string `json:"start,omitempty"` // ISO date, "" if unknown
	End   string `json:"end,omitempty"`   // ISO date, "" if open/unknown
}

// sortSnapshot canonicalizes ordering so the on-disk snapshot is stable
// regardless of the order the SPARQL endpoint returned rows in.
func sortSnapshot(s *Snapshot) {
	sort.Slice(s.Countries, func(i, j int) bool { return s.Countries[i].QID < s.Countries[j].QID })
	for ci := range s.Countries {
		hs := s.Countries[ci].Holders
		sort.Slice(hs, func(i, j int) bool { return hs[i].QID < hs[j].QID })
		for hi := range hs {
			sort.Slice(hs[hi].Terms, func(a, b int) bool {
				if hs[hi].Terms[a].Start != hs[hi].Terms[b].Start {
					return hs[hi].Terms[a].Start < hs[hi].Terms[b].Start
				}
				return hs[hi].Terms[a].End < hs[hi].Terms[b].End
			})
		}
	}
}

func writeSnapshot(path string, s *Snapshot) error {
	sortSnapshot(s)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s); err != nil {
		return err
	}
	return f.Close()
}

func readSnapshot(path string) (*Snapshot, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Snapshot
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	sortSnapshot(&s)
	return &s, nil
}

// ---------- date helpers ----------

var epoch = time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)

// dayOf returns integer days since 1900-01-01 for a UTC date.
func dayOf(t time.Time) int { return int(t.Sub(epoch) / (24 * time.Hour)) }

// parseDay parses an ISO date (possibly with a time/zone suffix) into a day
// number. ok=false for empty/unparseable input.
func parseDay(iso string) (int, bool) {
	iso = strings.TrimSpace(iso)
	if len(iso) < 10 {
		return 0, false
	}
	t, err := time.Parse("2006-01-02", iso[:10])
	if err != nil {
		return 0, false
	}
	return dayOf(t), true
}

func mustDay(iso string) int {
	t, err := time.Parse("2006-01-02", iso)
	if err != nil {
		panic(err)
	}
	return dayOf(t)
}

// ---------- slug ----------

// slug turns a label into a stable lowercase ascii identifier fragment.
func slug(s string) string {
	var b strings.Builder
	prevUnderscore := false
	for _, r := range strings.ToLower(s) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevUnderscore = false
		default:
			if !prevUnderscore {
				b.WriteByte('_')
				prevUnderscore = true
			}
		}
	}
	return strings.Trim(b.String(), "_")
}
