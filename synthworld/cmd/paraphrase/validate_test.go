package main

import (
	"strings"
	"testing"
)

const origFact = `[day 30] Observation (fct_0259, source registry_A, valid from day 30 until day 65): classified_as(product=product_03, partner=partner_02).`

const origRule = `[day 31] Policy rul_012 ("flagged_for policy 1", authority level 4, effective from day 31): IF requires_review(customer=?A, customer2=customer_08) AND holds(customer=customer_08, partner=?B) THEN flagged_for(customer=?A, partner=?B), UNLESS classified_as(product=product_08, partner=?B).`

const origSup = `[day 90] Notice sup_002: policy rul_005 is superseded by policy rul_014, effective day 90. From that day, rul_005 no longer applies.`

func TestValidateLineAcceptsFaithfulParaphrases(t *testing.T) {
	cases := []struct{ name, orig, para string }{
		{"fact reworded", origFact,
			`A field entry dated day 30 (record fct_0259, provided by registry_A) establishes classified_as(product=product_03, partner=partner_02) — in force from day 30 until day 65.`},
		{"rule reworded", origRule,
			`On day 31 the directive rul_012 ("flagged_for policy 1") took effect (authority level 4, in force from day 31): whenever requires_review(customer=?A, customer2=customer_08) AND holds(customer=customer_08, partner=?B) both apply, flagged_for(customer=?A, partner=?B) follows — except where classified_as(product=product_08, partner=?B) is the case.`},
		{"sup reworded", origSup,
			`Bulletin sup_002, day 90: rul_014 replaces rul_005 as of day 90, and rul_005 ceases to have effect from that day onward.`},
		{"atom spacing normalized", origFact,
			`Day 30 filing fct_0259 from registry_A, good from day 30 until day 65, records classified_as(product=product_03,partner=partner_02).`},
	}
	for _, c := range cases {
		if err := validateLine(c.orig, c.para); err != nil {
			t.Errorf("%s: rejected a faithful paraphrase: %v", c.name, err)
		}
	}
}

func TestValidateLineRejectsCorruption(t *testing.T) {
	cases := []struct{ name, orig, para, wantSub string }{
		{"entity swapped", origFact,
			`A field entry dated day 30 (record fct_0259, provided by registry_A) establishes classified_as(product=product_04, partner=partner_02) — in force from day 30 until day 65.`,
			"atom expressions"},
		{"validity end dropped", origFact,
			`A field entry dated day 30 (record fct_0259, provided by registry_A) establishes classified_as(product=product_03, partner=partner_02) — in force from day 30.`,
			"numbers changed"},
		{"source renamed", origFact,
			`A field entry dated day 30 (record fct_0259, provided by registry_B, valid day 30 until day 65) establishes classified_as(product=product_03, partner=partner_02).`,
			"identifiers changed"},
		{"number spelled out", origRule,
			strings.Replace(faithfulRule(), "authority level 4", "authority level four", 1),
			"numbers changed"},
		{"conditional lost", origRule,
			`Directive rul_012 ("flagged_for policy 1"), authority level 4, day 31, in force from day 31, concerns requires_review(customer=?A, customer2=customer_08) AND holds(customer=customer_08, partner=?B) plus flagged_for(customer=?A, partner=?B), except classified_as(product=product_08, partner=?B).`,
			"conditional connective"},
		{"exception lost", origRule,
			`On day 31 the directive rul_012 ("flagged_for policy 1") took effect (authority level 4, from day 31): whenever requires_review(customer=?A, customer2=customer_08) AND holds(customer=customer_08, partner=?B) both apply, flagged_for(customer=?A, partner=?B) follows; also note classified_as(product=product_08, partner=?B).`,
			"exception language"},
		{"hallucinated id", origSup,
			`Bulletin sup_002, day 90: rul_014 replaces rul_005 as of day 90, and rul_005 plus rul_099 cease to have effect from that day onward.`,
			"identifiers changed"},
		{"variable renamed inside atom", origRule,
			strings.Replace(faithfulRule(), "flagged_for(customer=?A, partner=?B)", "flagged_for(customer=?C, partner=?B)", 1),
			"atom expressions"},
	}
	for _, c := range cases {
		err := validateLine(c.orig, c.para)
		if err == nil {
			t.Errorf("%s: corruption accepted", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.wantSub) {
			t.Errorf("%s: wrong violation class: got %v, want substring %q", c.name, err, c.wantSub)
		}
	}
}

func faithfulRule() string {
	return `On day 31 the directive rul_012 ("flagged_for policy 1") took effect (authority level 4, in force from day 31): whenever requires_review(customer=?A, customer2=customer_08) AND holds(customer=customer_08, partner=?B) both apply, flagged_for(customer=?A, partner=?B) follows — except where classified_as(product=product_08, partner=?B) is the case.`
}

func TestParseNumbered(t *testing.T) {
	good := "1: alpha\n2: beta\n\n3: gamma"
	lines, err := parseNumbered(good, 3)
	if err != nil || len(lines) != 3 || lines[2] != "gamma" {
		t.Fatalf("good reply mis-parsed: %v %v", lines, err)
	}
	if _, err := parseNumbered("1: a\n3: c", 2); err == nil {
		t.Error("numbering gap accepted")
	}
	if _, err := parseNumbered("1: a\n2: b", 3); err == nil {
		t.Error("short reply accepted")
	}
	if _, err := parseNumbered("1: a\nstray continuation", 1); err == nil {
		t.Error("unnumbered content accepted")
	}
	if lines, err := parseNumbered("```\n1: a\n```", 1); err != nil || lines[0] != "a" {
		t.Errorf("code fences not tolerated: %v %v", lines, err)
	}
}

func TestFallbackAccounting(t *testing.T) {
	// A reply that always fails validation must lead to Fallback=true after
	// maxRetries attempts and leave the episode text untouched.
	// (Exercised via paraphraseEpisode with a canned bad client.)
	ep := testEpisode()
	origText := ep.Text
	bad := cannedClient("1: totally rewritten without any identifiers")
	res := paraphraseEpisode(bad, &ep, 2)
	if !res.Fallback || res.Attempts != 2 {
		t.Fatalf("expected fallback after 2 attempts, got %+v", res)
	}
	if ep.Text != origText {
		t.Error("fallback must keep the original text")
	}
}

func TestParaphraseEpisodeAcceptsAndRewritesEvents(t *testing.T) {
	ep := testEpisode()
	good := cannedClient("1: Filed on day 30 under fct_0259 by registry_A, valid from day 30 until day 65: classified_as(product=product_03, partner=partner_02).")
	res := paraphraseEpisode(good, &ep, 2)
	if res.Fallback {
		t.Fatalf("faithful paraphrase rejected: %+v", res)
	}
	if !strings.Contains(ep.Text, "Filed on day 30") || !strings.HasPrefix(ep.Text, "=== Episode ep_900") {
		t.Errorf("episode text not rewritten with header preserved: %q", ep.Text)
	}
	if !strings.Contains(ep.Events[0].Text, "Filed on day 30") {
		t.Error("event text not rewritten in lockstep")
	}
}
