package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Mechanical preservation validator — the piece that makes the paraphrase
// tier trustworthy. A paraphrased line is accepted only if, relative to its
// original, it preserves EXACTLY (as order-insensitive multisets):
//
//  1. every underscore identifier (entity IDs, fact/rule/notice IDs,
//     relation names like classified_as, sources like registry_A),
//  2. every integer written as digits (days, authority levels, validity
//     endpoints),
//  3. every formal atom/pattern expression `name(slot=value, ...)`
//     verbatim (whitespace-normalized) — the atoms are formal notation;
//     the prose AROUND them is what varies,
//
// and, for policy lines, still reads as a conditional (a connective word
// linking the same condition/conclusion expressions) with exception
// language preserved when the original had UNLESS.
//
// Ground truth is never entrusted to the paraphrasing LLM: a line that
// fails after retries falls back to its original text and is counted.

var (
	reUnderscoreTok = regexp.MustCompile(`\b\w+_\w+\b`)
	reInteger       = regexp.MustCompile(`\b\d+\b`)
	// atom/pattern expressions: name(...) with no nested parens
	reAtomExpr = regexp.MustCompile(`\b\w+\([^()]*\)`)
	reWordIf   = regexp.MustCompile(`(?i)\b(if|when|whenever|where|wherever|provided|in case|as long as|once)\b`)
	reWordExc  = regexp.MustCompile(`(?i)\b(unless|except|excluding|barring|save for|does not apply|not fire|exempt)\b`)
)

// tokenMultiset returns a canonical sorted list of matches (multiset form).
func tokenMultiset(re *regexp.Regexp, s string) []string {
	m := re.FindAllString(s, -1)
	sort.Strings(m)
	return m
}

// normalizeExprs canonicalizes whitespace inside atom expressions so
// "flagged_for(customer=?A,partner=?B)" == "flagged_for(customer=?A, partner=?B)".
func normalizeExprs(exprs []string) []string {
	out := make([]string, len(exprs))
	for i, e := range exprs {
		out[i] = strings.Join(strings.Fields(strings.ReplaceAll(e, ",", ", ")), " ")
	}
	sort.Strings(out)
	return out
}

// stripAtomExprs removes atom expressions before scanning for loose tokens,
// so identifiers inside atoms are checked once (via the expression multiset)
// and identifiers in prose (IDs, sources) are checked independently.
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

// isPolicyLine mirrors the generator's rule rendering closely enough for a
// structural guard: original policy lines say "Policy <id> (" and contain
// IF/THEN.
func isPolicyLine(orig string) bool {
	return strings.Contains(orig, "] Policy ") && strings.Contains(orig, "IF ") && strings.Contains(orig, " THEN ")
}

func hasExceptionClause(orig string) bool {
	return strings.Contains(orig, ", UNLESS ")
}

// validateLine checks one paraphrased line against its original.
// Returns nil if acceptable, else a description of every violation.
func validateLine(orig, para string) error {
	var problems []string

	// 3. formal expressions, verbatim modulo whitespace
	wantExprs := normalizeExprs(tokenMultiset(reAtomExpr, orig))
	gotExprs := normalizeExprs(tokenMultiset(reAtomExpr, para))
	if miss, extra := diffMultiset(wantExprs, gotExprs); len(miss)+len(extra) > 0 {
		problems = append(problems, fmt.Sprintf("atom expressions changed: missing %v, unexpected %v", miss, extra))
	}

	// 1. underscore identifiers outside atoms
	wantIDs := tokenMultiset(reUnderscoreTok, stripAtomExprs(orig))
	gotIDs := tokenMultiset(reUnderscoreTok, stripAtomExprs(para))
	if miss, extra := diffMultiset(wantIDs, gotIDs); len(miss)+len(extra) > 0 {
		problems = append(problems, fmt.Sprintf("identifiers changed: missing %v, unexpected %v", miss, extra))
	}

	// 2. integers (as digits) outside atoms — atoms carry their own args and
	// argument values are IDs, but numbers also appear only in prose scaffold
	// (days, authority). Compare on the atom-stripped strings so a paraphrase
	// cannot smuggle a day into/out of an atom either way.
	wantNums := tokenMultiset(reInteger, stripAtomExprs(orig))
	gotNums := tokenMultiset(reInteger, stripAtomExprs(para))
	if miss, extra := diffMultiset(wantNums, gotNums); len(miss)+len(extra) > 0 {
		problems = append(problems, fmt.Sprintf("numbers changed (write all numbers as digits): missing %v, unexpected %v", miss, extra))
	}

	// 4. structural guard for policies
	if isPolicyLine(orig) {
		if !reWordIf.MatchString(para) {
			problems = append(problems, "policy line lost its conditional connective (needs if/when/provided/...)")
		}
		if hasExceptionClause(orig) && !reWordExc.MatchString(para) {
			problems = append(problems, "policy line lost its exception language (needs unless/except/...)")
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
		var idx int
		var rest string
		m := reNumbered.FindStringSubmatch(line)
		if m == nil {
			// continuation of previous line? Forbid: one line in, one line out.
			return nil, fmt.Errorf("unnumbered content in reply: %q", firstN(line, 120))
		}
		fmt.Sscanf(m[1], "%d", &idx)
		rest = m[2]
		if idx != seen+1 {
			return nil, fmt.Errorf("line numbering broke: expected %d, got %d", seen+1, idx)
		}
		seen++
		out = append(out, rest)
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
