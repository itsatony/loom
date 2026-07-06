package main

import (
	"context"

	"github.com/vaudience/synthworld/gen"
)

// cannedClient returns a fixed reply to every Complete call.
type cannedClient string

func (c cannedClient) Model() string { return "canned" }
func (c cannedClient) Complete(context.Context, string, string) (string, error) {
	return string(c), nil
}

func testEpisode() gen.Episode {
	line := `[day 30] Observation (fct_0259, source registry_A, valid from day 30 until day 65): classified_as(product=product_03, partner=partner_02).`
	return gen.Episode{
		ID: "ep_900", Day: 30,
		Text:   "=== Episode ep_900 (day 30) ===\n" + line,
		Events: []gen.Event{{Kind: gen.EvFact, Day: 30, Text: line}},
	}
}
