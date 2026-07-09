// Frames: named worlds with visibility inheritance (MASTERPLAN §9.6,
// FRAMES-DESIGN-NOTES App A/B). Truth is frame-relative. `actual` is
// implicit and never declared. Scenario frames inherit their parents
// (ultimately actual) with delta overlays; fiction and perspective frames
// inherit nothing (schema/vocabulary only) and may not be inherited from.
// A world with an empty Frames table is a v0 world: everything lives in
// actual and evaluation is bit-identical to pre-frame behavior.
package world

import (
	"fmt"
	"sort"
)

// ActualFrame is the implicit root frame. An empty FrameID on any item
// means ActualFrame.
const ActualFrame = "actual"

type FrameKind string

const (
	FrameFiction     FrameKind = "fiction"
	FrameScenario    FrameKind = "scenario"
	FramePerspective FrameKind = "perspective"
)

// FrameBasis controls how a scenario frame sees its inherited layer:
// live = tracks parent revisions; pinned = parent layer evaluated at
// min(t, PinDay) — one effective timestamp per inheritance edge.
// Basis is set at creation and immutable (re-pin = new frame).
type FrameBasis string

const (
	FrameLive   FrameBasis = "live"
	FramePinned FrameBasis = "pinned"
)

type Frame struct {
	ID         string     `json:"id"`
	Kind       FrameKind  `json:"kind"`
	Parents    []string   `json:"parents,omitempty"` // scenario only; fiction/perspective inherit nothing
	Basis      FrameBasis `json:"basis,omitempty"`   // scenario only; default live
	PinDay     int        `json:"pin_day,omitempty"` // meaningful iff Basis == pinned
	CreatedDay int        `json:"created_day"`
}

// NormFrame maps the empty FrameID to ActualFrame.
func NormFrame(id string) string {
	if id == "" {
		return ActualFrame
	}
	return id
}

func (w *World) FrameByID(id string) *Frame {
	for i := range w.Frames {
		if w.Frames[i].ID == id {
			return &w.Frames[i]
		}
	}
	return nil
}

// ConeMember describes one frame visible from a query frame: its shortest
// distance in inheritance edges (0 = the query frame itself) and the
// effective evaluation day for items homed there (pinning applied along
// the path; min over converging paths for determinism).
type ConeMember struct {
	Dist int
	Eff  int
}

// Cone computes the visibility cone of frameID at evaluation time t:
// the set of frames whose items are visible, with distance and effective
// time per member. Returns an error for unknown frames.
func (w *World) Cone(frameID string, t int) (map[string]ConeMember, error) {
	frameID = NormFrame(frameID)
	if frameID != ActualFrame && w.FrameByID(frameID) == nil {
		return nil, fmt.Errorf("unknown frame %q", frameID)
	}
	cone := map[string]ConeMember{frameID: {Dist: 0, Eff: t}}
	// BFS upward through Parents. The child's pin applies on each
	// child->parent edge. Deterministic: process queue in insertion order;
	// relax to min(dist), min(eff).
	queue := []string{frameID}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		cm := cone[cur]
		f := w.FrameByID(cur)
		if f == nil {
			continue // actual (or undeclared root): no parents
		}
		parentEff := cm.Eff
		if f.Basis == FramePinned && f.PinDay < parentEff {
			parentEff = f.PinDay
		}
		parents := f.Parents
		if f.Kind == FrameScenario && len(parents) == 0 {
			parents = []string{ActualFrame}
		}
		for _, p := range parents {
			p = NormFrame(p)
			pm, seen := cone[p]
			nd, ne := cm.Dist+1, parentEff
			if !seen {
				cone[p] = ConeMember{Dist: nd, Eff: ne}
				queue = append(queue, p)
				continue
			}
			changed := false
			if nd < pm.Dist {
				pm.Dist = nd
				changed = true
			}
			if ne < pm.Eff {
				pm.Eff = ne
				changed = true
			}
			if changed {
				cone[p] = pm
				queue = append(queue, p)
			}
		}
	}
	return cone, nil
}

// validateFrames checks the frame table and item frame references.
// Called from Validate.
func (w *World) validateFrames() error {
	byID := map[string]*Frame{}
	for i := range w.Frames {
		f := &w.Frames[i]
		if f.ID == "" {
			return fmt.Errorf("frame with empty ID")
		}
		if f.ID == ActualFrame {
			return fmt.Errorf("frame %q must not be declared: actual is implicit", ActualFrame)
		}
		if _, dup := byID[f.ID]; dup {
			return fmt.Errorf("duplicate frame ID %s", f.ID)
		}
		byID[f.ID] = f
	}
	for _, f := range byID {
		switch f.Kind {
		case FrameFiction, FramePerspective:
			if len(f.Parents) != 0 {
				return fmt.Errorf("frame %s: kind %s inherits nothing (parents must be empty)", f.ID, f.Kind)
			}
			if f.Basis != "" && f.Basis != FrameLive {
				return fmt.Errorf("frame %s: basis is scenario-only", f.ID)
			}
		case FrameScenario:
			switch f.Basis {
			case "", FrameLive:
				if f.PinDay != 0 {
					return fmt.Errorf("frame %s: pin_day set on live frame", f.ID)
				}
			case FramePinned:
				if f.PinDay < 0 || f.PinDay > w.Horizon {
					return fmt.Errorf("frame %s: pin_day %d outside [0,%d]", f.ID, f.PinDay, w.Horizon)
				}
			default:
				return fmt.Errorf("frame %s: unknown basis %q", f.ID, f.Basis)
			}
			for _, p := range f.Parents {
				p = NormFrame(p)
				if p == ActualFrame {
					continue
				}
				pf, ok := byID[p]
				if !ok {
					return fmt.Errorf("frame %s: unknown parent %s", f.ID, p)
				}
				if pf.Kind != FrameScenario {
					return fmt.Errorf("frame %s: parent %s has kind %s — fiction/perspective frames may not be inherited from", f.ID, p, pf.Kind)
				}
			}
		default:
			return fmt.Errorf("frame %s: unknown kind %q", f.ID, f.Kind)
		}
	}
	// Acyclicity over scenario parent edges (DFS, deterministic order).
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := map[string]int{}
	var visit func(id string) error
	visit = func(id string) error {
		if id == ActualFrame {
			return nil
		}
		switch color[id] {
		case gray:
			return fmt.Errorf("frame cycle through %s", id)
		case black:
			return nil
		}
		color[id] = gray
		for _, p := range byID[id].Parents {
			if err := visit(NormFrame(p)); err != nil {
				return err
			}
		}
		color[id] = black
		return nil
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := visit(id); err != nil {
			return err
		}
	}
	// Item frame references must resolve.
	known := func(id string) bool {
		id = NormFrame(id)
		return id == ActualFrame || byID[id] != nil
	}
	for _, f := range w.Facts {
		if !known(f.FrameID) {
			return fmt.Errorf("fact %s: unknown frame %q", f.ID, f.FrameID)
		}
		if f.Block && NormFrame(f.FrameID) == ActualFrame {
			return fmt.Errorf("fact %s: block facts are frame-delta mechanics; not allowed in actual", f.ID)
		}
	}
	for _, r := range w.Rules {
		if !known(r.FrameID) {
			return fmt.Errorf("rule %s: unknown frame %q", r.ID, r.FrameID)
		}
	}
	for _, s := range w.Supersessions {
		if !known(s.FrameID) {
			return fmt.Errorf("supersession %s: unknown frame %q", s.ID, s.FrameID)
		}
	}
	return nil
}
