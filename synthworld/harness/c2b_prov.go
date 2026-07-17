// C2bProvCondition — the registered honest null for F-E2 (MASTERPLAN
// §9.6.3, FRAMES-DESIGN-NOTES §B.8): a FRAMELESS store compiled from the
// same episode text by a frame-blind extractor, with episode/source
// METADATA kept per item, and frame queries answered by QUERY-TIME metadata
// filtering. No compile-time frame decisions are made; the null's whole
// frame competence is lexical filtering over stored source spans.
//
// Filtering policy (deterministic, registered here before any measured run):
//
//   - an item whose source span mentions a fiction title or scenario
//     codename (the dataset's frame handles — the same naming affordance
//     every text-mode condition receives) is tagged with that frame;
//   - an item whose span carries a QUOTED atom expression ("rel(...)"
//     inside double quotes) is speech: excluded from actual, attributed to
//     the perspective frame of the first mentioned narrator entity if one
//     matches, otherwise unattributed (in no frame at all);
//   - everything else is actual.
//
// Query-time: actual = actual-tagged items only; a scenario frame sees
// actual-tagged + its own tagged items (no pin semantics, no block
// overlays — a frameless store cannot represent either); fiction and
// perspective frames see only their own tagged items.
//
// Registered prediction of where this fails: content-cued frame content
// (unmarked narration, attributed-but-unquoted claims, deadpan sarcasm)
// and scenario delta overlays/pinning. If C2b(frames) does NOT beat this
// null by the F-E2 margin on content-cued traps, query-time provenance
// filtering suffices and the compile-time-frames bet is falsified.
package harness

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/vaudience/synthworld/gen"
	"github.com/vaudience/synthworld/loom"
	"github.com/vaudience/synthworld/world"
)

type C2bProvCondition struct {
	Label     string
	Vocab     loom.Vocabulary
	Extractor loom.Extractor // frame-BLIND extractor (v0 prompt); shares cassettes with loom-c2b
	Workers   int
	// FrameNames: canonical frame ID → surface handle (identity on tier E).
	FrameNames map[string]string

	pipeline *loom.Pipeline
	Compile_ *loom.CompileReport

	// tag per item index (facts/rules/sups separately), computed at ingest.
	factTag, ruleTag, supTag []string

	mu     sync.Mutex
	stores map[string]*loom.Store // per query frame: filtered frameless store
}

func (c *C2bProvCondition) Name() string { return c.Label }

var reQuotedAtom = regexp.MustCompile(`[\x{201C}"]\s*\w+\([^)"\x{201D}]*\)`)

// SpanFrameTag classifies one source text line by lexical metadata alone:
// the canonical frame ID whose handle the line mentions, "" for actual, or
// "speech:<frameID-or-empty>" for a quoted atom expression. This rule set is
// BOTH the null's query-time filter and the scorer's operational definition
// of a metadata-cued trap (a trap whose line this tagger files correctly is
// metadata-cued; anything else is content-cued) — F-E2's cued partition is
// therefore fixed by construction, not by any measured condition's output.
func SpanFrameTag(span string, frameNames map[string]string) string {
	// fiction/scenario handles: multiword titles and codenames, matched as
	// substrings; longest handle first for determinism.
	type fh struct{ id, handle string }
	var hs []fh
	for id, h := range frameNames {
		if strings.HasPrefix(id, "fic_") || strings.HasPrefix(id, "scn_") {
			hs = append(hs, fh{id, h})
		}
	}
	sort.Slice(hs, func(i, j int) bool {
		if len(hs[i].handle) != len(hs[j].handle) {
			return len(hs[i].handle) > len(hs[j].handle)
		}
		return hs[i].id < hs[j].id
	})
	for _, h := range hs {
		if h.handle != "" && strings.Contains(span, h.handle) {
			return h.id
		}
	}
	if reQuotedAtom.MatchString(span) {
		// attribute to the first perspective narrator mentioned in the span
		best := -1
		bestID := ""
		for id, h := range frameNames {
			if !strings.HasPrefix(id, "psp_") || h == "" {
				continue
			}
			if i := strings.Index(span, h); i >= 0 && (best == -1 || i < best || (i == best && id < bestID)) {
				best, bestID = i, id
			}
		}
		return "speech:" + bestID
	}
	return ""
}

func (c *C2bProvCondition) spanTag(span string) string { return SpanFrameTag(span, c.FrameNames) }

func (c *C2bProvCondition) Ingest(episodes []gen.Episode) error {
	c.pipeline = loom.NewPipeline(c.Vocab, c.Extractor)
	c.pipeline.Workers = c.Workers
	rep, err := c.pipeline.Compile(episodes)
	c.Compile_ = rep
	if err != nil {
		return fmt.Errorf("%s compile: %w", c.Label, err)
	}
	st := c.pipeline.Store
	c.factTag = make([]string, len(st.Facts))
	for i := range st.Facts {
		c.factTag[i] = c.spanTag(st.Facts[i].Provenance.Span)
	}
	c.ruleTag = make([]string, len(st.Rules))
	for i := range st.Rules {
		c.ruleTag[i] = c.spanTag(st.Rules[i].Provenance.Span)
	}
	c.supTag = make([]string, len(st.Supersessions))
	for i := range st.Supersessions {
		c.supTag[i] = c.spanTag(st.Supersessions[i].Provenance.Span)
	}
	c.stores = map[string]*loom.Store{}
	return nil
}

// storeFor lazily builds the filtered frameless store for a query frame.
func (c *C2bProvCondition) storeFor(frame string) (*loom.Store, error) {
	frame = world.NormFrame(frame)
	c.mu.Lock()
	defer c.mu.Unlock()
	if s, ok := c.stores[frame]; ok {
		return s, nil
	}
	include := func(tag string) bool {
		switch {
		case frame == world.ActualFrame:
			return tag == ""
		case strings.HasPrefix(frame, "scn_"):
			return tag == "" || tag == frame
		default: // fiction / perspective: own items only
			if tag == frame {
				return true
			}
			return strings.HasPrefix(frame, "psp_") && tag == "speech:"+frame
		}
	}
	src := c.pipeline.Store
	dst := loom.NewStore()
	for i := range src.Facts {
		if !include(c.factTag[i]) {
			continue
		}
		f := src.Facts[i].Fact
		if err := dst.CommitFact(f, src.Facts[i].Provenance); err != nil {
			return nil, err
		}
	}
	for i := range src.Rules {
		if !include(c.ruleTag[i]) {
			continue
		}
		if err := dst.CommitRule(src.Rules[i].Rule, src.Rules[i].Provenance); err != nil {
			return nil, err
		}
	}
	for i := range src.Supersessions {
		if !include(c.supTag[i]) {
			continue
		}
		if err := dst.CommitSupersession(src.Supersessions[i].Supersession, src.Supersessions[i].Provenance); err != nil {
			return nil, err
		}
	}
	c.stores[frame] = dst
	return dst, nil
}

func (c *C2bProvCondition) AnswerHolds(q SanitizedQuery) (bool, error) {
	s, err := c.storeFor(q.Frame)
	if err != nil {
		return false, err
	}
	ok, _, err := s.Holds(*q.Atom, q.AtDay)
	return ok, err
}

func (c *C2bProvCondition) AnswerFind(q SanitizedQuery) ([]string, error) {
	s, err := c.storeFor(q.Frame)
	if err != nil {
		return nil, err
	}
	return s.Find(*q.Pattern, q.FindSlot, q.AtDay)
}

// frameUniverse: actual + every frame the handle table names (the null has
// no compiled frame table; the naming affordance is all it knows).
func (c *C2bProvCondition) frameUniverse() []string {
	ids := []string{world.ActualFrame}
	var rest []string
	for id := range c.FrameNames {
		rest = append(rest, id)
	}
	sort.Strings(rest)
	return append(ids, rest...)
}

func (c *C2bProvCondition) AnswerWhichFrames(q SanitizedQuery) ([]string, error) {
	var out []string
	for _, fid := range c.frameUniverse() {
		s, err := c.storeFor(fid)
		if err != nil {
			return nil, err
		}
		ok, _, err := s.Holds(*q.Atom, q.AtDay)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, fid)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (c *C2bProvCondition) AnswerFindFramed(q SanitizedQuery) ([]gen.FramedValue, error) {
	var out []gen.FramedValue
	for _, fid := range q.FramesScope {
		s, err := c.storeFor(fid)
		if err != nil {
			return nil, err
		}
		vals, err := s.Find(*q.Pattern, q.FindSlot, q.AtDay)
		if err != nil {
			return nil, err
		}
		for _, v := range vals {
			out = append(out, gen.FramedValue{Value: v, Frame: fid})
		}
	}
	return out, nil
}
