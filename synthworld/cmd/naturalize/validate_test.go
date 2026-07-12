package main

import (
	"strings"
	"testing"

	"github.com/vaudience/synthworld/gen"
	"github.com/vaudience/synthworld/world"
)

func TestValidateLinePreservation(t *testing.T) {
	orig := `[day 12] Observation (fct_0007, source audit_note, valid from day 12): supplies(customer=customer_02, product=product_05).`
	good := `An audit_note entry logged on day 12 — reference fct_0007, effective from day 12 — confirms that supplies(customer=customer_02, product=product_05).`
	if err := validateLine(orig, good, lineSpec{}); err != nil {
		t.Fatalf("good line rejected: %v", err)
	}
	bad := []struct {
		name, nat string
	}{
		{"atom dissolved", `audit_note fct_0007 on day 12, from day 12: customer_02 gets product_05.`},
		{"day dropped", `audit_note fct_0007 on day 12 confirms supplies(customer=customer_02, product=product_05).`},
		{"id capitalized", `Audit_note entry on day 12, from day 12 — fct_0007 — confirms supplies(customer=customer_02, product=product_05).`},
		{"marker word", `A frame entry fct_0007 (audit_note, day 12, from day 12) confirms supplies(customer=customer_02, product=product_05).`},
	}
	for _, c := range bad {
		if err := validateLine(orig, c.nat, lineSpec{}); err == nil {
			t.Errorf("%s: accepted but should be rejected: %q", c.name, c.nat)
		}
	}
}

func TestValidateLineFrameHandles(t *testing.T) {
	orig := `[day 154] Scenario scn_live notice sup_016: within this scenario, policy rul_015 is superseded by policy rul_038, effective day 154. Outside the scenario, rul_015 still applies.`
	sp := lineSpec{ExemptIDs: []string{"scn_live"}, Require: []string{"Blueline"}}
	good := `Logged on day 154, and for the purposes of the Blueline drill only: notice sup_016 has rul_038 replacing rul_015 effective day 154; in the desk's standing records, rul_015 remains in force.`
	if err := validateLine(orig, good, sp); err != nil {
		t.Fatalf("good scenario line rejected: %v", err)
	}
	if err := validateLine(orig, strings.ReplaceAll(good, "Blueline", "scn_live"), sp); err == nil {
		t.Error("raw frame ID survived but was accepted")
	}
	if err := validateLine(orig, strings.ReplaceAll(good, "Blueline", "Redwood"), sp); err == nil {
		t.Error("missing required handle mention was accepted")
	}
}

func TestValidateLineQuoteAndProse(t *testing.T) {
	orig := `[day 54] Report (fct_0984): according to a statement attributed to frame psp_jurisdiction_16, "rated_as(jurisdiction=jurisdiction_01, product=product_27, jurisdiction2=jurisdiction_01)" (a claim, not independently observed).`
	sp := lineSpec{
		ExemptIDs:  []string{"psp_jurisdiction_16"},
		ReqProse:   []string{"jurisdiction_16"},
		AllowExtra: []string{"jurisdiction_16"},
		IsQuote:    true,
	}
	good := `On day 54, jurisdiction_16 told the desk plainly: "rated_as(jurisdiction=jurisdiction_01, product=product_27, jurisdiction2=jurisdiction_01)" — logged as fct_0984.`
	if err := validateLine(orig, good, sp); err != nil {
		t.Fatalf("good quote line rejected: %v", err)
	}
	noQuotes := strings.ReplaceAll(good, `"`, ``)
	if err := validateLine(orig, noQuotes, sp); err == nil {
		t.Error("quote without quotation marks was accepted")
	}
}

func TestHandlesDeterministicAndUnique(t *testing.T) {
	eps := []gen.Episode{{Events: []gen.Event{
		{Kind: gen.EvFrame, Frame: &world.Frame{ID: "fic_01", Kind: world.FrameFiction}},
		{Kind: gen.EvFrame, Frame: &world.Frame{ID: "fic_02", Kind: world.FrameFiction}},
		{Kind: gen.EvFrame, Frame: &world.Frame{ID: "scn_live", Kind: world.FrameScenario}},
		{Kind: gen.EvFrame, Frame: &world.Frame{ID: "scn_pin", Kind: world.FrameScenario, PinDay: 179}},
		{Kind: gen.EvFrame, Frame: &world.Frame{ID: "psp_asset_15", Kind: world.FramePerspective}},
	}}}
	h1 := buildHandles(eps, "seed-1")
	h2 := buildHandles(eps, "seed-1")
	h3 := buildHandles(eps, "seed-2")
	if len(h1) != 5 {
		t.Fatalf("expected 5 handles, got %d", len(h1))
	}
	for id, h := range h1 {
		if h2[id].Handle != h.Handle {
			t.Errorf("handle for %s not deterministic", id)
		}
	}
	if h1["fic_01"].Handle == h1["fic_02"].Handle {
		t.Error("duplicate fiction titles within a seed")
	}
	if h1["scn_live"].Handle == h3["scn_live"].Handle {
		t.Error("scenario handle identical across seeds (salt not applied)")
	}
	if h1["psp_asset_15"].Entity != "asset_15" {
		t.Errorf("perspective entity = %q", h1["psp_asset_15"].Entity)
	}
	for _, h := range h1 {
		if h.Kind == world.FramePerspective {
			continue // perspective handles ARE the entity identifier, by design
		}
		if strings.ContainsAny(h.Handle, "_0123456789") {
			t.Errorf("handle %q contains underscore/digit (breaks preservation multisets)", h.Handle)
		}
	}
}

func TestParseJudgeLabels(t *testing.T) {
	cases := []struct {
		in   string
		want lineLabel
	}{
		{`actual | statement`, lineLabel{Kind: "actual", Type: "statement"}},
		{`view jurisdiction_16 | quote`, lineLabel{Kind: "view", Ref: "jurisdiction_16", Type: "quote"}},
		{`story "The Glass Harbor" | statement`, lineLabel{Kind: "story", Ref: "glass harbor", Type: "statement"}},
		{`exercise "Blueline" | declaration`, lineLabel{Kind: "exercise", Ref: "blueline", Type: "declaration"}},
		{`Actual | Confirmation`, lineLabel{Kind: "actual", Type: "confirmation"}},
	}
	for _, c := range cases {
		got, err := parseJudgeLabel(c.in)
		if err != nil {
			t.Errorf("parse %q: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parse %q = %+v, want %+v", c.in, got, c.want)
		}
	}
	if _, err := parseJudgeLabel("view | quote"); err == nil {
		t.Error("view without party accepted")
	}
}
