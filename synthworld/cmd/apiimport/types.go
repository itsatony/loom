package main

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"time"
)

// ---------- Snapshot: the pinned, self-contained corpus (Rung 1) ----------
//
// Rung 1 = API version-history / deprecations. The snapshot is REAL Django
// release-note data (symbols deprecated/removed/replaced across pinned
// feature releases) transcribed verbatim from the cited docs URLs and PINNED
// here. The build path reads this file only — no network, no time.Now, no
// randomness — so same snapshot + same binary => byte-identical dataset,
// exactly like the Rung-0 importer.
//
// The machine-readable ground truth (what is deprecated/removed/replaced, and
// — via Django's arithmetic policy — in which version a deprecation is
// removed) is carried in the structured fields; the prose fields are the
// natural-language release-note passages the LLM extractor (C2b) must read.
// The oracle computes truth from the STRUCTURE; the extractor sees only the
// PROSE. Every symbol carries its source URL for auditability.

type Snapshot struct {
	Note           string        `json:"note"`
	Package        string        `json:"package"`
	PinnedVersion  string        `json:"pinned_version"`
	PinDate        string        `json:"pin_date"`
	PolicyRuleText string        `json:"policy_rule_text"`
	PolicyURL      string        `json:"policy_url"`
	Releases       []SnapRelease `json:"feature_releases"`
	Symbols        []SnapSymbol  `json:"symbols"`
}

// SnapRelease is one feature release on the timeline: version + release date.
// The ordering (by date) drives the next_feature_release chain the policy rule
// composes over.
type SnapRelease struct {
	Version string `json:"version"`
	Date    string `json:"date"` // ISO date; planned releases use their scheduled date
	URL     string `json:"url"`
}

// SnapSymbol is one public API symbol and its lifecycle status at the pin.
//
//   - StatusAtPin "stable":     never deprecated as of the pin (repetition + retained control)
//   - StatusAtPin "deprecated": deprecated but NOT yet removed at the pin (retained control;
//     drives scheduled_removal composition)
//   - StatusAtPin "removed":    removed at the pin (revision flip: stale=available, current=gone)
type SnapSymbol struct {
	ID           string `json:"id"`   // canonical dotted symbol path (its own surface form in prose)
	Kind         string `json:"kind"` // function|class|method|kwarg|constant|attribute
	DeprecatedIn string `json:"deprecated_in,omitempty"`
	RemovedIn    string `json:"removed_in,omitempty"` // explicit if stated; else derivable via policy
	ReplacedBy   string `json:"replaced_by,omitempty"`
	StatusAtPin  string `json:"status_at_pin"`

	DeprecationProse string `json:"deprecation_prose,omitempty"`
	DeprecationURL   string `json:"deprecation_url,omitempty"`
	RemovalProse     string `json:"removal_prose,omitempty"`
	RemovalURL       string `json:"removal_url,omitempty"`
}

// sortSnapshot canonicalizes ordering so the on-disk snapshot is stable.
func sortSnapshot(s *Snapshot) {
	sort.Slice(s.Releases, func(i, j int) bool {
		if s.Releases[i].Date != s.Releases[j].Date {
			return s.Releases[i].Date < s.Releases[j].Date
		}
		return s.Releases[i].Version < s.Releases[j].Version
	})
	sort.Slice(s.Symbols, func(i, j int) bool { return s.Symbols[i].ID < s.Symbols[j].ID })
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

// ---------- date helpers (self-contained; cmd packages don't share) ----------

var epoch = time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)

func dayOf(t time.Time) int { return int(t.Sub(epoch) / (24 * time.Hour)) }

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
	d, ok := parseDay(iso)
	if !ok {
		panic("bad date: " + iso)
	}
	return d
}

// ---------- slug ----------

// symbolSlug turns a dotted symbol path into a stable id fragment while
// preserving enough structure to stay human-readable in traces.
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
