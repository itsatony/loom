package main

import (
	"fmt"
	"hash/fnv"
	"sort"
	"strings"

	"github.com/vaudience/synthworld/gen"
	"github.com/vaudience/synthworld/world"
)

// Frame handles — tier-M text must never contain raw frame IDs (fic_*,
// psp_*, scn_*): those are exactly the surface cues the authenticity
// certificate (MASTERPLAN §9.6.6) exists to rule out. Instead every
// fiction frame gets a deterministic story TITLE, every scenario frame a
// deterministic exercise NAME, and perspective frames are referenced by
// their narrator entity (an ordinary identifier that also occurs in
// actual lines, so its mere presence is not a frame cue). The frame→handle
// map is recorded in the naturalize report; like world.json it is
// available for SCORING and auditing only, never to a measured condition.
//
// Handles are alphabetic-only (no underscores, no digits) so they are
// invisible to the mechanical preservation multisets, and they are chosen
// by seeded hash so the same dataset always naturalizes to the same names
// while different seeds get different names (style diversity across the
// batch).

var storyTitles = []string{
	"The Glass Harbor", "A Season of Salt", "The Cartographer's Debt",
	"Nightferry", "The Orchard Ledger", "Winter in the Annex",
	"The Lantern Rooms", "Sable Crossing", "The Quiet Tariff",
	"Harbor of Small Hours", "The Paper Meridian", "Aster and Flint",
	"The Long Recess", "Millwater", "The Second Auditor",
	"A Coastline in Amber", "The Unsigned Page", "Greenhouse Standing",
	"The Ferryman's Ledger", "Low Tide at Vantage", "The Marble Index",
	"Softwood", "The Visiting Clerk", "Ten Days of Grey",
}

// Bare codenames, deliberately without a constant "Exercise" prefix: a
// fixed prefix token would be a single lexical cue shared across every
// scenario line of every seed — a gift to the surface-cue classifier the
// authenticity certificate runs. The naturalizer varies the scaffold
// ("the Blueline drill", "under Blueline's premises", ...).
var exerciseNames = []string{
	"Blueline", "Northstar", "Coldharbor", "Saffron",
	"Greenfield", "Ironbridge", "Longview", "Silvergate",
	"Tallgrass", "Whitewater", "Copperfield", "Driftwood",
	"Eastgate", "Foxglove", "Goldcrest", "Hazelwood",
	"Ivybridge", "Kestrel", "Larkspur", "Mooring",
	"Nettlebed", "Oakhaven", "Quillbrook", "Stonemere",
}

// frameHandle is how tier-M prose refers to a frame.
type frameHandle struct {
	FrameID string          `json:"frame_id"`
	Kind    world.FrameKind `json:"kind"`
	// Handle: story title (fiction), exercise name (scenario), or the
	// narrator entity (perspective).
	Handle string `json:"handle"`
	// Entity is set for perspective frames (== Handle).
	Entity string `json:"entity,omitempty"`
	PinDay int    `json:"pin_day,omitempty"`
	Basis  string `json:"basis,omitempty"`
}

// buildHandles walks the episode stream's frame declarations and assigns
// deterministic handles. salt should identify the dataset (its seed) so
// different seeds draw different names.
func buildHandles(episodes []gen.Episode, salt string) map[string]frameHandle {
	handles := map[string]frameHandle{}
	usedTitles := map[string]bool{}
	usedNames := map[string]bool{}
	pick := func(list []string, used map[string]bool, key string) string {
		h := fnv.New32a()
		h.Write([]byte(salt + "|" + key))
		idx := int(h.Sum32()) % len(list)
		if idx < 0 {
			idx += len(list)
		}
		for i := 0; i < len(list); i++ {
			cand := list[(idx+i)%len(list)]
			if !used[cand] {
				used[cand] = true
				return cand
			}
		}
		// More frames than names would be a generator change; fail loudly.
		panic(fmt.Sprintf("handle wordlist exhausted for %s", key))
	}
	for _, ep := range episodes {
		for _, ev := range ep.Events {
			if ev.Kind != gen.EvFrame || ev.Frame == nil {
				continue
			}
			f := ev.Frame
			fh := frameHandle{FrameID: f.ID, Kind: f.Kind, PinDay: f.PinDay, Basis: string(f.Basis)}
			switch f.Kind {
			case world.FrameFiction:
				fh.Handle = pick(storyTitles, usedTitles, f.ID)
			case world.FrameScenario:
				fh.Handle = pick(exerciseNames, usedNames, f.ID)
			case world.FramePerspective:
				fh.Entity = strings.TrimPrefix(f.ID, "psp_")
				fh.Handle = fh.Entity
			}
			handles[f.ID] = fh
		}
	}
	return handles
}

// sortedHandles returns handles in deterministic frame-ID order for
// report output and prompt glossaries.
func sortedHandles(handles map[string]frameHandle) []frameHandle {
	out := make([]frameHandle, 0, len(handles))
	for _, h := range handles {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FrameID < out[j].FrameID })
	return out
}
