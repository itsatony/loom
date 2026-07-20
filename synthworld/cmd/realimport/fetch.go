package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

const userAgent = "loom-research/0.1 (itsatony@vaudience.ai)"

const (
	sparqlEndpoint = "https://query.wikidata.org/sparql"
	wikiSummaryURL = "https://en.wikipedia.org/api/rest_v1/page/summary/"
)

// countrySeed is the hardcoded discovery list: only country QIDs are pinned;
// the head-of-government office is discovered via P1313, its holders via P39.
var countrySeed = []struct {
	QID  string
	Name string
}{
	{"Q183", "Germany"},
	{"Q145", "United Kingdom"},
	{"Q16", "Canada"},
	{"Q408", "Australia"},
	{"Q38", "Italy"},
}

// maxHoldersPerOffice caps how many recent officeholders we keep per office,
// so the corpus stays small (Rung 0 is a plumbing gate, not a scale test).
const maxHoldersPerOffice = 6

var httpClient = &http.Client{Timeout: 60 * time.Second}

// fetchAll pulls the live snapshot. Any entity that fails is skipped and
// logged to stderr; the run does not abort.
func fetchAll() *Snapshot {
	snap := &Snapshot{
		Note: "Heads of government (P1313 office, P39 holders) for a handful of " +
			"countries, with spouse (P26) and party (P102), plus each person's " +
			"English Wikipedia REST summary. Fetched once from live Wikidata + " +
			"Wikipedia; the build path reads this file only (no network).",
	}
	for _, cs := range countrySeed {
		c, err := fetchCountry(cs.QID, cs.Name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skip country %s (%s): %v\n", cs.Name, cs.QID, err)
			continue
		}
		if len(c.Holders) == 0 {
			fmt.Fprintf(os.Stderr, "skip country %s (%s): no officeholders found\n", cs.Name, cs.QID)
			continue
		}
		snap.Countries = append(snap.Countries, *c)
	}
	return snap
}

// fetchCountry discovers the office and its holders for one country.
func fetchCountry(qid, name string) (*SnapCountry, error) {
	q := fmt.Sprintf(`SELECT ?office ?officeLabel ?person ?personLabel ?start ?end ?article ?spouse ?spouseLabel ?party ?partyLabel WHERE {
  wd:%s wdt:P1313 ?office .
  ?person p:P39 ?st .
  ?st ps:P39 ?office .
  OPTIONAL { ?st pq:P580 ?start. }
  OPTIONAL { ?st pq:P582 ?end. }
  OPTIONAL { ?article schema:about ?person ; schema:isPartOf <https://en.wikipedia.org/> . }
  OPTIONAL { ?person wdt:P26 ?spouse. }
  OPTIONAL { ?person wdt:P102 ?party. }
  SERVICE wikibase:label { bd:serviceParam wikibase:language "en". }
}`, qid)

	rows, err := runSPARQL(q)
	if err != nil {
		return nil, err
	}

	type acc struct {
		h        SnapHolder
		maxStart string
		terms    map[string]SnapTerm // dedup by start+end
		spouses  map[string]string   // qid->label
		parties  map[string]string
	}
	byPerson := map[string]*acc{}
	officeQID, officeName := "", ""

	for _, r := range rows {
		if officeQID == "" {
			officeQID = lastSegment(r["office"])
			officeName = r["officeLabel"]
		}
		pqid := lastSegment(r["person"])
		if pqid == "" {
			continue
		}
		a := byPerson[pqid]
		if a == nil {
			a = &acc{
				h:       SnapHolder{QID: pqid, Name: r["personLabel"]},
				terms:   map[string]SnapTerm{},
				spouses: map[string]string{},
				parties: map[string]string{},
			}
			byPerson[pqid] = a
		}
		if art := r["article"]; art != "" && a.h.Enwiki == "" {
			a.h.Enwiki = articleTitle(art)
		}
		start, end := isoDate(r["start"]), isoDate(r["end"])
		if start != "" || end != "" {
			a.terms[start+"|"+end] = SnapTerm{Start: start, End: end}
			if start > a.maxStart {
				a.maxStart = start
			}
		}
		if sq := lastSegment(r["spouse"]); sq != "" {
			a.spouses[sq] = r["spouseLabel"]
		}
		if pq := lastSegment(r["party"]); pq != "" {
			a.parties[pq] = r["partyLabel"]
		}
	}
	if officeQID == "" {
		return nil, fmt.Errorf("no head-of-government office (P1313)")
	}

	// rank persons by most-recent term, keep the top N
	var accs []*acc
	for _, a := range byPerson {
		accs = append(accs, a)
	}
	sort.Slice(accs, func(i, j int) bool {
		if accs[i].maxStart != accs[j].maxStart {
			return accs[i].maxStart > accs[j].maxStart // recent first
		}
		return accs[i].h.QID < accs[j].h.QID
	})
	if len(accs) > maxHoldersPerOffice {
		accs = accs[:maxHoldersPerOffice]
	}

	c := &SnapCountry{QID: qid, Name: name, OfficeQID: officeQID, OfficeName: officeName}
	for _, a := range accs {
		for _, t := range a.terms {
			a.h.Terms = append(a.h.Terms, t)
		}
		a.h.SpouseQID, a.h.Spouse = firstSorted(a.spouses)
		a.h.PartyQID, a.h.Party = firstSorted(a.parties)
		a.h.Extract = fetchExtract(a.h.Enwiki, a.h.Name)
		c.Holders = append(c.Holders, a.h)
	}
	return c, nil
}

// firstSorted returns the (key,value) with the smallest key, for determinism.
func firstSorted(m map[string]string) (string, string) {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		return "", ""
	}
	sort.Strings(keys)
	return keys[0], m[keys[0]]
}

// fetchExtract pulls the Wikipedia REST summary; on any failure it logs and
// returns an empty string (the build path composes a fallback sentence).
func fetchExtract(title, personName string) string {
	if title == "" {
		title = personName
	}
	if title == "" {
		return ""
	}
	seg := url.PathEscape(strings.ReplaceAll(title, " ", "_"))
	req, err := http.NewRequest(http.MethodGet, wikiSummaryURL+seg, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "extract %q: %v\n", title, err)
		return ""
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "extract %q: %v\n", title, err)
		return ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "extract %q: HTTP %d\n", title, resp.StatusCode)
		return ""
	}
	var summary struct {
		Extract string `json:"extract"`
	}
	if err := json.Unmarshal(body, &summary); err != nil {
		fmt.Fprintf(os.Stderr, "extract %q: decode: %v\n", title, err)
		return ""
	}
	return strings.TrimSpace(summary.Extract)
}

// runSPARQL runs a query against Wikidata and returns rows as slot->value maps.
func runSPARQL(query string) ([]map[string]string, error) {
	u := sparqlEndpoint + "?format=json&query=" + url.QueryEscape(query)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/sparql-results+json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	var parsed struct {
		Results struct {
			Bindings []map[string]struct {
				Value string `json:"value"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode sparql: %w", err)
	}
	var rows []map[string]string
	for _, b := range parsed.Results.Bindings {
		row := map[string]string{}
		for k, v := range b {
			row[k] = v.Value
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func lastSegment(uri string) string {
	if uri == "" {
		return ""
	}
	if i := strings.LastIndex(uri, "/"); i >= 0 {
		return uri[i+1:]
	}
	return uri
}

// articleTitle turns a wiki URL into a human title (underscores -> spaces,
// percent-decoded).
func articleTitle(uri string) string {
	seg := lastSegment(uri)
	if dec, err := url.PathUnescape(seg); err == nil {
		seg = dec
	}
	return strings.ReplaceAll(seg, "_", " ")
}

// isoDate normalizes a Wikidata date literal to a YYYY-MM-DD string, or "".
func isoDate(v string) string {
	if len(v) < 10 {
		return ""
	}
	if _, ok := parseDay(v); !ok {
		return ""
	}
	return v[:10]
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
