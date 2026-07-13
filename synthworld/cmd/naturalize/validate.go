package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/vaudience/synthworld/gen"
	"github.com/vaudience/synthworld/world"
)

// Mechanical preservation validator, extended from cmd/paraphrase for the
// frames tier-M corpus (MASTERPLAN §9.6.6). A naturalized line is accepted
// only if, relative to its original tier-E line, it preserves EXACTLY:
//
//  1. every underscore identifier EXCEPT the frame IDs (fic_*/psp_*/scn_*),
//     which are exempted — they must NOT survive; frames are referenced by
//     their registered handle (story title / exercise name / narrator
//     entity) instead, and the handle mention is enforced per line,
//  2. every integer written as digits,
//  3. every formal atom/pattern expression verbatim,
//  4. structural semantics: conditionals stay conditional, exceptions stay
//     exceptional, block deltas keep removal language, forecasts keep
//     forward-looking language, quotes keep direct quoted speech,
//  5. a banned-marker list: none of the tier-E frame marker vocabulary
//     ("frame", "fiction", "perspective", "non-assertive", "sarcastic",
//     "promotion", raw frame IDs, ...) may appear — those are exactly the
//     surface cues the authenticity certificate must not find.
//
// Ground truth is never entrusted to the naturalizing LLM: a line that
// fails after retries falls back to its original text and is counted (and
// a tier-E line surviving into the tier-M corpus is a defect the fallback
// gate bounds).

var (
	reUnderscoreTok = regexp.MustCompile(`\b\w+_\w+\b`)
	reInteger       = regexp.MustCompile(`\b\d+\b`)
	reAtomExpr      = regexp.MustCompile(`\b\w+\([^()]*\)`)
	reWordIf        = regexp.MustCompile(`(?i)\b(if|when|whenever|where|wherever|provided|in case|as long as|once)\b`)
	reWordExc       = regexp.MustCompile(`(?i)\b(unless|except|excluding|barring|save for|does not apply|not fire|exempt)\b`)
	reWordBlock     = regexp.MustCompile(`(?i)\b(disregard\w*|set(s|ting)? aside|exclud\w*|ignor\w*|suspend\w*|drop\w*|remove\w*|off the table|not (to )?(hold|apply|stand)|no longer (holds?|applies|stands?)|treat\w* .{0,40}(as absent|as not)|without)\b`)
	reWordForecast  = regexp.MustCompile(`(?i)\b(expect\w*|project\w*|anticipat\w*|forecast\w*|predict\w*|foresee\w*|will|due to hold|likely)\b`)
	reQuoteMark     = regexp.MustCompile(`["“”«»]`)
	reFrameIDTok    = regexp.MustCompile(`\b(fic|psp|scn)_\w+\b`)
)

// bannedMarkers are tier-E surface cues that must not survive
// naturalization. Kept deliberately tight: only the mechanical marker
// vocabulary is banned; natural pragmatic vocabulary ("scenario",
// "assume", "story", "claims") stays legal — the authenticity certificate
// (cmd/authcert), not this list, is the arbiter of residual cue strength.
var bannedMarkers = []struct {
	re  *regexp.Regexp
	msg string
}{
	{regexp.MustCompile(`(?i)\bframes?\b|\bframing\b`), `the word "frame"`},
	{regexp.MustCompile(`(?i)\bfiction(al|alized)?\b`), `the word "fiction"`},
	{regexp.MustCompile(`(?i)\bperspective\b`), `the word "perspective"`},
	{regexp.MustCompile(`(?i)non-?assertive`), `"non-assertive"`},
	{regexp.MustCompile(`(?i)sarcas|\bironi|facetious`), `explicit sarcasm/irony labels`},
	{regexp.MustCompile(`(?i)\bpromot(e|ed|es|ing|ion|ions)\b`), `the word "promotion"`},
	{regexp.MustCompile(`(?i)not independently observed`), `"not independently observed"`},
	{regexp.MustCompile(`(?i)do(es)?n'?t believe|does not believe`), `"does not believe"`},
	{regexp.MustCompile(`(?i)story excerpt`), `"story excerpt"`},
	{regexp.MustCompile(`(?i)frame declaration`), `"frame declaration"`},
	// The next two are tier-E template phrases cmd/authcert's markerClass
	// fires on; the mechanical ban list must stay a superset of those
	// triggers (2026-07-13 batch: 6 scenario-supersession lines kept
	// "within this scenario" and failed certification).
	{regexp.MustCompile(`(?i)\bin the story\b`), `"in the story"`},
	{regexp.MustCompile(`(?i)\bwithin this scenario\b`), `"within this scenario"`},
}

// lineSpec carries the frame-aware validation requirements for one line.
type lineSpec struct {
	ExemptIDs  []string // underscore tokens allowed (required, via ban) to disappear
	Require    []string // substrings that must appear somewhere in the line (handles)
	ReqProse   []string // substrings that must appear OUTSIDE atom expressions (attributed entities)
	AllowExtra []string // tokens allowed to newly appear in prose (narrator entities)
	IsPolicy   bool
	HasExc     bool
	IsBlock    bool
	IsForecast bool
	IsQuote    bool
	// BanGenre: fiction CONTENT lines must carry membership via the title
	// alone — genre vocabulary ("story", "installment", ...) is a lexical
	// cue that transfers across seeds and hands the §9.6.6 surface-cue
	// baseline the frame type (measured on dev seeds, 2026-07-12).
	// Declarations may still say what the work is; content lines may not.
	BanGenre bool
}

var reGenreWords = regexp.MustCompile(`(?i)\b(story|stories|installment|chapter|passage|narrative|tale|plot|novel|serial|manuscript|excerpt|episode|character|protagonist|reading)\b`)

// buildSpec derives the lineSpec for one event from its structured payload
// (ground truth is available to the tier pipeline — the naturalizer is
// outside the evaluated matrix; measured conditions never see any of this).
func buildSpec(ev *gen.Event, handles map[string]frameHandle) lineSpec {
	var sp lineSpec
	frameOf := func(id string) (frameHandle, bool) {
		h, ok := handles[world.NormFrame(id)]
		return h, ok
	}
	switch ev.Kind {
	case gen.EvFrame:
		f := ev.Frame
		h := handles[f.ID]
		sp.ExemptIDs = append(sp.ExemptIDs, f.ID)
		switch f.Kind {
		case world.FramePerspective:
			sp.ReqProse = append(sp.ReqProse, h.Entity)
			sp.AllowExtra = append(sp.AllowExtra, h.Entity)
		default:
			sp.Require = append(sp.Require, h.Handle)
		}
	case gen.EvPromotion:
		if h, ok := frameOf(ev.Promotion.FromFrame); ok && h.Entity != "" {
			sp.ExemptIDs = append(sp.ExemptIDs, h.FrameID)
			sp.AllowExtra = append(sp.AllowExtra, h.Entity)
		}
	case gen.EvFact:
		f := ev.Fact
		fr := world.NormFrame(f.FrameID)
		if fr == world.ActualFrame {
			break // plain observation or sarcasm: v0 rules only
		}
		h := handles[fr]
		sp.ExemptIDs = append(sp.ExemptIDs, fr)
		switch h.Kind {
		case world.FrameFiction:
			sp.Require = append(sp.Require, h.Handle)
			sp.BanGenre = true
		case world.FrameScenario:
			sp.Require = append(sp.Require, h.Handle)
			sp.IsBlock = f.Block
		case world.FramePerspective:
			sp.ReqProse = append(sp.ReqProse, h.Entity)
			sp.AllowExtra = append(sp.AllowExtra, h.Entity)
			sp.IsQuote = ev.AssertionType == gen.AssertQuote
			sp.IsForecast = strings.Contains(ev.Text, "] Forecast ")
		}
	case gen.EvRule:
		sp.IsPolicy = true
		sp.HasExc = strings.Contains(ev.Text, ", UNLESS ")
		if fr := world.NormFrame(ev.Rule.FrameID); fr != world.ActualFrame {
			sp.ExemptIDs = append(sp.ExemptIDs, fr)
			sp.Require = append(sp.Require, handles[fr].Handle)
		}
	case gen.EvSupersession:
		if fr := world.NormFrame(ev.Supersession.FrameID); fr != world.ActualFrame {
			sp.ExemptIDs = append(sp.ExemptIDs, fr)
			sp.Require = append(sp.Require, handles[fr].Handle)
		}
	}
	return sp
}

func tokenMultiset(re *regexp.Regexp, s string) []string {
	m := re.FindAllString(s, -1)
	sort.Strings(m)
	return m
}

func normalizeExprs(exprs []string) []string {
	out := make([]string, len(exprs))
	for i, e := range exprs {
		out[i] = strings.Join(strings.Fields(strings.ReplaceAll(e, ",", ", ")), " ")
	}
	sort.Strings(out)
	return out
}

func stripAtomExprs(s string) string {
	return reAtomExpr.ReplaceAllString(s, " ")
}

func diffMultiset(want, got []string) (missing, extra []string) {
	count := map[string]int{}
	for _, w := range want {
		count[w]++
	}
	for _, g := range got {
		count[g]--
	}
	keys := make([]string, 0, len(count))
	for k := range count {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		switch {
		case count[k] > 0:
			for i := 0; i < count[k]; i++ {
				missing = append(missing, k)
			}
		case count[k] < 0:
			for i := 0; i < -count[k]; i++ {
				extra = append(extra, k)
			}
		}
	}
	return missing, extra
}

func countTok(list []string, s string) int {
	n := 0
	for _, l := range list {
		if l == s {
			n++
		}
	}
	return n
}

func inList(list []string, s string) bool {
	for _, l := range list {
		if l == s {
			return true
		}
	}
	return false
}

// validateLine checks one naturalized line against its original and spec.
func validateLine(orig, nat string, sp lineSpec) error {
	var problems []string

	// Formal expressions, verbatim modulo whitespace.
	wantExprs := normalizeExprs(tokenMultiset(reAtomExpr, orig))
	gotExprs := normalizeExprs(tokenMultiset(reAtomExpr, nat))
	if miss, extra := diffMultiset(wantExprs, gotExprs); len(miss)+len(extra) > 0 {
		problems = append(problems, fmt.Sprintf("atom expressions changed: missing %v, unexpected %v", miss, extra))
	}

	// Underscore identifiers outside atoms, with frame-ID exemptions and
	// allowed attribution entities.
	wantIDs := tokenMultiset(reUnderscoreTok, stripAtomExprs(orig))
	filtered := wantIDs[:0]
	for _, t := range wantIDs {
		if !inList(sp.ExemptIDs, t) {
			filtered = append(filtered, t)
		}
	}
	wantIDs = filtered
	gotIDs := tokenMultiset(reUnderscoreTok, stripAtomExprs(nat))
	miss, extra := diffMultiset(wantIDs, gotIDs)
	kept := extra[:0]
	for _, t := range extra {
		if !inList(sp.AllowExtra, t) {
			kept = append(kept, t)
		}
	}
	extra = kept
	if len(miss)+len(extra) > 0 {
		problems = append(problems, fmt.Sprintf("identifiers changed: missing %v, unexpected %v", miss, extra))
	}

	// Raw frame IDs must never survive (independent of the multisets: they
	// might hide inside an atom-stripped region or be introduced fresh).
	if ids := reFrameIDTok.FindAllString(nat, -1); len(ids) > 0 {
		problems = append(problems, fmt.Sprintf("raw frame tags must not appear (use the registered name instead): %v", ids))
	}

	// Integers as digits outside atoms.
	wantNums := tokenMultiset(reInteger, stripAtomExprs(orig))
	gotNums := tokenMultiset(reInteger, stripAtomExprs(nat))
	if miss, extra := diffMultiset(wantNums, gotNums); len(miss)+len(extra) > 0 {
		var parts []string
		for _, m := range miss {
			want, got := countTok(wantNums, m), countTok(gotNums, m)
			hint := ""
			if want > 1 {
				hint = " — write it in each role, e.g. \"logged on day N and effective from day N\" states N twice"
			}
			parts = append(parts, fmt.Sprintf("the number %s must appear EXACTLY %d time(s) as digits outside atom expressions; your line has %d%s",
				m, want, got, hint))
		}
		for _, x := range extra {
			want, got := countTok(wantNums, x), countTok(gotNums, x)
			if want > 0 {
				parts = append(parts, fmt.Sprintf("the number %s must appear EXACTLY %d time(s) as digits outside atom expressions; your line has %d — do not repeat it beyond that", x, want, got))
			} else {
				parts = append(parts, fmt.Sprintf("the number %s does not occur in the original prose — remove it", x))
			}
		}
		problems = append(problems, "numbers changed: "+strings.Join(parts, "; "))
	}

	// Banned tier-E marker vocabulary.
	for _, b := range bannedMarkers {
		if b.re.MatchString(nat) {
			problems = append(problems, fmt.Sprintf("banned marker vocabulary: %s", b.msg))
		}
	}

	// Required mentions.
	for _, r := range sp.Require {
		if !strings.Contains(nat, r) {
			problems = append(problems, fmt.Sprintf("must mention %q verbatim", r))
		}
	}
	for _, r := range sp.ReqProse {
		if !strings.Contains(stripAtomExprs(nat), r) {
			problems = append(problems, fmt.Sprintf("must mention %q in the prose (outside the formal expression), lowercase exactly as written", r))
		}
	}

	// Structural guards.
	if sp.IsPolicy {
		if !reWordIf.MatchString(nat) {
			problems = append(problems, "policy line lost its conditional connective (needs if/when/provided/...)")
		}
		if sp.HasExc && !reWordExc.MatchString(nat) {
			problems = append(problems, "policy line lost its exception language (needs unless/except/...)")
		}
	}
	if sp.IsBlock && !reWordBlock.MatchString(nat) {
		problems = append(problems, "block delta lost its removal language (needs set aside/disregard/no longer holds/...)")
	}
	if sp.IsForecast && !reWordForecast.MatchString(nat) {
		problems = append(problems, "forecast lost its forward-looking language (needs expects/projects/will/...)")
	}
	if sp.IsQuote && !reQuoteMark.MatchString(nat) {
		problems = append(problems, "quoted claim lost its direct quotation marks")
	}
	if sp.BanGenre {
		if m := reGenreWords.FindString(nat); m != "" {
			problems = append(problems, fmt.Sprintf("genre word %q not allowed in this line — the title mention alone carries it; report what happens in the work's world", m))
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
}

// parseNumbered parses an LLM reply of the form "1: text\n2: text..." into
// exactly n lines. Lenient about surrounding blank lines and code fences,
// loud about count or numbering mismatches.
func parseNumbered(reply string, n int) ([]string, error) {
	var out []string
	seen := 0
	for _, raw := range strings.Split(reply, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "```") {
			continue
		}
		m := reNumbered.FindStringSubmatch(line)
		if m == nil {
			return nil, fmt.Errorf("unnumbered content in reply: %q", firstN(line, 120))
		}
		var idx int
		fmt.Sscanf(m[1], "%d", &idx)
		if idx != seen+1 {
			return nil, fmt.Errorf("line numbering broke: expected %d, got %d", seen+1, idx)
		}
		seen++
		out = append(out, m[2])
	}
	if seen != n {
		return nil, fmt.Errorf("expected %d lines, got %d", n, seen)
	}
	return out, nil
}

var reNumbered = regexp.MustCompile(`^(\d+)[:.)]\s*(.+)$`)

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
