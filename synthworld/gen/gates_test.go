package gen

import (
	"strings"
	"testing"
)

func TestEvaluateGates(t *testing.T) {
	th := DefaultGateThresholds()
	pass := GateStats{
		CompositionPositives: 30,
		RevisionFlips:        20,
		RevisionRetained:     20,
		Depth2PlusAtoms:      5,
		OverFiringRelations:  4,
		DerivedRelations:     9,
	}
	cases := []struct {
		name    string
		mutate  func(*GateStats)
		wantSub []string // substrings expected among rejection reasons; empty = pass
	}{
		{name: "all gates pass at exact thresholds", mutate: func(*GateStats) {}},
		{
			name:    "composition positives below minimum",
			mutate:  func(g *GateStats) { g.CompositionPositives = 29 },
			wantSub: []string{"composition positives 29 < 30"},
		},
		{
			name:    "revision flips below minimum",
			mutate:  func(g *GateStats) { g.RevisionFlips = 19 },
			wantSub: []string{"revision flips 19 < 20"},
		},
		{
			name:    "revision retained below minimum",
			mutate:  func(g *GateStats) { g.RevisionRetained = 0 },
			wantSub: []string{"revision retained 0 < 20"},
		},
		{
			name:    "no depth-2 closure mass",
			mutate:  func(g *GateStats) { g.Depth2PlusAtoms = 0 },
			wantSub: []string{"depth >= 2"},
		},
		{
			name:   "over-firing at exactly half is allowed",
			mutate: func(g *GateStats) { g.OverFiringRelations = 4; g.DerivedRelations = 8 },
		},
		{
			name:    "over-firing majority rejected",
			mutate:  func(g *GateStats) { g.OverFiringRelations = 5; g.DerivedRelations = 9 },
			wantSub: []string{"over-firing relations 5/9"},
		},
		{
			name: "multiple gate failures all reported",
			mutate: func(g *GateStats) {
				g.CompositionPositives = 0
				g.RevisionFlips = 0
				g.RevisionRetained = 0
				g.Depth2PlusAtoms = 0
			},
			wantSub: []string{"composition positives", "revision flips", "revision retained", "depth >= 2"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gs := pass
			tc.mutate(&gs)
			reasons := EvaluateGates(gs, th)
			if len(tc.wantSub) == 0 {
				if len(reasons) != 0 {
					t.Fatalf("expected pass, got reasons %v", reasons)
				}
				return
			}
			if len(reasons) != len(tc.wantSub) {
				t.Fatalf("expected %d reasons, got %v", len(tc.wantSub), reasons)
			}
			joined := strings.Join(reasons, "\n")
			for _, sub := range tc.wantSub {
				if !strings.Contains(joined, sub) {
					t.Errorf("missing reason containing %q in %v", sub, reasons)
				}
			}
		})
	}
}

func TestComputeGateStats(t *testing.T) {
	pos, neg := true, false
	qs := &QuerySet{Queries: []Query{
		{Slice: "composition", Type: "holds", Answer: &pos},
		{Slice: "composition", Type: "holds", Answer: &neg},                 // negative: not counted
		{Slice: "composition", Type: "find"},                                // find: not counted
		{Slice: "repetition", Type: "holds", Answer: &pos},                  // wrong slice
		{Slice: "revision", Type: "holds", Answer: &neg, StaleAnswer: &pos}, // flip
		{Slice: "revision", Type: "holds", Answer: &pos, StaleAnswer: &pos}, // retained
		{Slice: "revision", Type: "holds", Answer: &pos},                    // malformed: no stale, ignored
	}}
	stats := DatasetStats{
		FiringRatios:        map[string]float64{"r1": 0.1, "r2": 0.7, "r3": 0.2},
		ClosureDepthCounts:  map[string]int{"d0": 100, "d1": 50, "d2": 7, "d3": 2},
		OverFiringRelations: []string{"r2"},
	}
	gs := ComputeGateStats(qs, stats)
	want := GateStats{
		CompositionPositives: 1,
		RevisionFlips:        1,
		RevisionRetained:     1,
		Depth2PlusAtoms:      9,
		OverFiringRelations:  1,
		DerivedRelations:     3,
	}
	if gs != want {
		t.Fatalf("ComputeGateStats = %+v, want %+v", gs, want)
	}
}
