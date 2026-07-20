package loom

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vaudience/synthworld/gen"
)

// FramesSelfConsistExtractor is the pre-registered self-consistency extraction
// mode (MASTERPLAN §10 2026-07-20). It draws K independent samples per episode
// from the inner FramesLLMExtractor (each with a distinct pass nonce so the
// harness issues K distinct requests; run with temperature>0 so the model
// resamples), aligns candidates by source line, and MAJORITY-VOTES the
// frame-homing decision — the one field the seed-7 diagnostic showed to be
// stochastic. Non-homing fields are taken from a representative member
// consistent with the winning (kind, frame) so the emitted candidate stays
// internally coherent.
//
// Rationale: per-item frame homing is a low-probability stochastic slip (11/12
// correct on the seed-7 rul_019 resample); a majority-of-5 vote drives the
// still-wrong rate to ~0.4%. This wrapper changes NOTHING about the store,
// ops, or query path — only which frame an extracted item is filed in.
type FramesSelfConsistExtractor struct {
	Inner *FramesLLMExtractor
	K     int
}

// NewFramesSelfConsistExtractor wraps inner with K-sample majority voting. K is
// forced odd and >=3 (a single sample is just the inner extractor).
func NewFramesSelfConsistExtractor(inner *FramesLLMExtractor, k int) *FramesSelfConsistExtractor {
	if k < 3 {
		k = 3
	}
	if k%2 == 0 {
		k++
	}
	return &FramesSelfConsistExtractor{Inner: inner, K: k}
}

func (e *FramesSelfConsistExtractor) Name() string {
	return fmt.Sprintf("sc%d:%s", e.K, e.Inner.Name())
}

// SetFrameContext forwards to the inner extractor (implements
// FrameContextExtractor so the frame-context pre-pass still applies).
func (e *FramesSelfConsistExtractor) SetFrameContext(directory string) {
	e.Inner.SetFrameContext(directory)
}

func (e *FramesSelfConsistExtractor) Extract(ep gen.Episode) ([]Candidate, []string, error) {
	runs := make([][]Candidate, 0, e.K)
	for i := 0; i < e.K; i++ {
		cands, _, err := e.Inner.extractWith(ep, fmt.Sprintf("%s-%d", ep.ID, i))
		if err != nil {
			return nil, nil, fmt.Errorf("self-consistency sample %d: %w", i, err)
		}
		runs = append(runs, cands)
	}
	return reconcileFrameVote(runs, e.K, ep)
}

// homingField returns the frame-homing value voted per candidate kind: the
// frame for facts/rules/supersessions, the source frame for promotions, and
// the kind for frame declarations.
func homingField(c Candidate) string {
	switch c.Kind {
	case CandFact:
		if c.Fact != nil {
			return c.Fact.Frame
		}
	case CandRule:
		if c.Rule != nil {
			return c.Rule.Frame
		}
	case CandSupersession:
		if c.Sup != nil {
			return c.Sup.Frame
		}
	case CandPromotion:
		if c.Promo != nil {
			return c.Promo.FromFrame
		}
	case CandFrame:
		if c.Frame != nil {
			return c.Frame.Kind
		}
	}
	return ""
}

// alignKey groups the SAME line's candidate across runs. The source line is
// frame-independent (it is the raw episode text), so it is the natural anchor;
// the voted homing field is deliberately NOT part of the key. Kind is also
// left out so a run that disagrees on kind still lands in the group (kind is
// voted inside). Fallback to a relation/name signature when a span is absent.
func alignKey(c Candidate) string {
	if s := strings.TrimSpace(c.SourceSpan); s != "" {
		return "span\x00" + s
	}
	switch c.Kind {
	case CandFact:
		if c.Fact != nil {
			return "fact\x00" + c.Fact.Relation + "\x00" + argsSig(c.Fact.Args)
		}
	case CandRule:
		if c.Rule != nil {
			return "rule\x00" + c.Rule.Name
		}
	case CandFrame:
		if c.Frame != nil {
			return "frame\x00" + c.Frame.Name
		}
	}
	return string(c.Kind) + "\x00?"
}

func argsSig(args map[string]string) string {
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(args[k])
		b.WriteByte(';')
	}
	return b.String()
}

func modal(counts map[string]int) string {
	best, bestN := "", -1
	// deterministic: iterate sorted keys so ties break stably
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if counts[k] > bestN {
			best, bestN = k, counts[k]
		}
	}
	return best
}

// reconcileFrameVote emits one candidate per line that appears in a MAJORITY of
// the K runs; within that group it votes the (kind, homing) pair and returns a
// representative member consistent with the winners.
func reconcileFrameVote(runs [][]Candidate, k int, ep gen.Episode) ([]Candidate, []string, error) {
	type group struct {
		members []Candidate
		order   int
	}
	groups := map[string]*group{}
	var order []string
	seq := 0
	for _, run := range runs {
		for _, c := range run {
			key := alignKey(c)
			g := groups[key]
			if g == nil {
				g = &group{order: seq}
				seq++
				groups[key] = g
				order = append(order, key)
			}
			g.members = append(g.members, c)
		}
	}
	maj := k/2 + 1
	var out []Candidate
	for _, key := range order {
		g := groups[key]
		if len(g.members) < maj {
			continue // unstable across runs: a minority of samples produced it
		}
		// vote kind, then homing within the modal kind
		kindCounts := map[string]int{}
		for _, m := range g.members {
			kindCounts[string(m.Kind)]++
		}
		winKind := modal(kindCounts)
		homingCounts := map[string]int{}
		for _, m := range g.members {
			if string(m.Kind) == winKind {
				homingCounts[homingField(m)]++
			}
		}
		winHoming := modal(homingCounts)
		// representative: first member matching (kind, homing); homing is
		// applied onto it so a member matching only the kind still yields the
		// voted frame.
		var rep *Candidate
		for i := range g.members {
			m := g.members[i]
			if string(m.Kind) == winKind {
				if rep == nil {
					rep = &g.members[i]
				}
				if homingField(m) == winHoming {
					rep = &g.members[i]
					break
				}
			}
		}
		if rep == nil {
			continue
		}
		voted := *rep
		setHoming(&voted, winHoming)
		out = append(out, voted)
	}
	// soft coverage warning, mirroring the inner extractor
	var problems []string
	nLines := 0
	for _, line := range strings.Split(ep.Text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "=== Episode") {
			nLines++
		}
	}
	if len(out) < nLines {
		problems = append(problems, fmt.Sprintf("%s: %d event lines but %d voted candidates (K=%d)", ep.ID, nLines, len(out), k))
	}
	return out, problems, nil
}

// setHoming writes the voted homing value back onto a candidate. For frame
// declarations the voted value is the kind; for the rest it is the frame.
func setHoming(c *Candidate, v string) {
	switch c.Kind {
	case CandFact:
		if c.Fact != nil {
			c.Fact.Frame = v
		}
	case CandRule:
		if c.Rule != nil {
			c.Rule.Frame = v
		}
	case CandSupersession:
		if c.Sup != nil {
			c.Sup.Frame = v
		}
	case CandPromotion:
		if c.Promo != nil {
			c.Promo.FromFrame = v
		}
	case CandFrame:
		if c.Frame != nil {
			c.Frame.Kind = v
		}
	}
}
