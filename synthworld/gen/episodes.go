package gen

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vaudience/synthworld/world"
)

// ---------- Episodes ----------

type EventKind string

const (
	EvFact         EventKind = "fact"
	EvRule         EventKind = "rule"
	EvSupersession EventKind = "supersession"
	// Frames-v1 episode kinds (MASTERPLAN §9.6.4). EvFrame declares a frame;
	// EvPromotion is the explicit door from a source frame into actual.
	EvFrame     EventKind = "frame"
	EvPromotion EventKind = "promotion"
)

// PromotionEvent is the payload of an EvPromotion event: a confirmed
// prediction crossing from its source frame into the actual record.
type PromotionEvent struct {
	PredictionFactID string `json:"prediction_fact_id"`
	ActualFactID     string `json:"actual_fact_id"`
	FromFrame        string `json:"from_frame"`
	Day              int    `json:"day"`
}

type Event struct {
	Kind EventKind `json:"kind"`
	Day  int       `json:"day"`
	Text string    `json:"text"`
	// exactly one of the following payloads is set
	Fact         *world.BaseFact     `json:"fact,omitempty"`
	Rule         *world.Rule         `json:"rule,omitempty"`
	Supersession *world.Supersession `json:"supersession,omitempty"`
	Frame        *world.Frame        `json:"frame_decl,omitempty"`
	Promotion    *PromotionEvent     `json:"promotion,omitempty"`
	// AssertionType is ground truth for frame-bearing events: "" (assert),
	// "quote" (claim attributed to a source), or "non-assertive" (sarcasm/
	// irony: the literal content is asserted nowhere). Easy mode parses it,
	// text mode never sees it, scoring always does.
	AssertionType string `json:"assertion_type,omitempty"`
}

type Episode struct {
	ID     string  `json:"id"`
	Day    int     `json:"day"`
	Text   string  `json:"text"`
	Events []Event `json:"events"`
}

// BuildEpisodes orders all reveal events chronologically, chunks them into
// episodes, renders text, and writes EpisodeID back into world objects.
func (b *Builder) BuildEpisodes() []Episode {
	type rawEvent struct {
		day   int
		kind  EventKind
		idx   int // index into the respective world slice
		key   string
		extra bool // pre-built event from b.extraEvents (frames layer)
	}
	var evs []rawEvent
	for i := range b.w.Facts {
		evs = append(evs, rawEvent{day: b.factRevealDay[b.w.Facts[i].ID], kind: EvFact, idx: i, key: b.w.Facts[i].ID})
	}
	for i := range b.w.Rules {
		evs = append(evs, rawEvent{day: b.w.Rules[i].IssuedAt, kind: EvRule, idx: i, key: b.w.Rules[i].ID})
	}
	for i := range b.w.Supersessions {
		evs = append(evs, rawEvent{day: b.w.Supersessions[i].From, kind: EvSupersession, idx: i, key: b.w.Supersessions[i].ID})
	}
	for i := range b.extraEvents {
		evs = append(evs, rawEvent{day: b.extraEvents[i].Day, kind: b.extraEvents[i].Kind, idx: i, key: b.extraKeys[i], extra: true})
	}
	sort.Slice(evs, func(i, j int) bool {
		if evs[i].day != evs[j].day {
			return evs[i].day < evs[j].day
		}
		return evs[i].key < evs[j].key
	})

	var episodes []Episode
	i := 0
	epNum := 0
	for i < len(evs) {
		size := b.cfg.EpisodeEvents.sample(b.rng)
		if i+size > len(evs) {
			size = len(evs) - i
		}
		epNum++
		ep := Episode{ID: fmt.Sprintf("ep_%03d", epNum)}
		for j := i; j < i+size; j++ {
			re := evs[j]
			var e Event
			switch {
			case re.extra:
				e = b.extraEvents[re.idx]
			case re.kind == EvFact:
				f := &b.w.Facts[re.idx]
				f.EpisodeID = ep.ID
				e = Event{Kind: EvFact, Day: re.day, Fact: f, Text: b.factText(f), AssertionType: b.assertKind[f.ID]}
			case re.kind == EvRule:
				r := &b.w.Rules[re.idx]
				r.EpisodeID = ep.ID
				e = Event{Kind: EvRule, Day: re.day, Rule: r, Text: b.ruleText(r)}
			case re.kind == EvSupersession:
				s := &b.w.Supersessions[re.idx]
				s.EpisodeID = ep.ID
				e = Event{Kind: EvSupersession, Day: re.day, Supersession: s, Text: b.supText(s)}
			}
			ep.Events = append(ep.Events, e)
			if re.day > ep.Day {
				ep.Day = re.day
			}
		}
		var lines []string
		lines = append(lines, fmt.Sprintf("=== Episode %s (day %d) ===", ep.ID, ep.Day))
		for _, e := range ep.Events {
			lines = append(lines, e.Text)
		}
		ep.Text = strings.Join(lines, "\n")
		episodes = append(episodes, ep)
		i += size
	}
	return episodes
}

// ---------- Text rendering ----------

func (b *Builder) relName(id string) string {
	if r := b.w.RelationByID(id); r != nil {
		return r.Name
	}
	return id
}

func (b *Builder) atomText(a world.Atom) string {
	rel := b.w.RelationByID(a.Relation)
	parts := make([]string, 0, len(rel.Slots))
	for _, s := range rel.Slots {
		parts = append(parts, fmt.Sprintf("%s=%s", s.Name, a.Args[s.Name]))
	}
	return fmt.Sprintf("%s(%s)", rel.Name, strings.Join(parts, ", "))
}

func (b *Builder) patternText(p world.PatternAtom) string {
	rel := b.w.RelationByID(p.Relation)
	parts := make([]string, 0, len(rel.Slots))
	for _, s := range rel.Slots {
		t := p.Args[s.Name]
		if t.Const != "" {
			parts = append(parts, fmt.Sprintf("%s=%s", s.Name, t.Const))
		} else {
			parts = append(parts, fmt.Sprintf("%s=?%s", s.Name, t.Var))
		}
	}
	return fmt.Sprintf("%s(%s)", rel.Name, strings.Join(parts, ", "))
}

func (b *Builder) factText(f *world.BaseFact) string {
	frame := world.NormFrame(f.FrameID)
	if frame == world.ActualFrame {
		// v0 template — byte-identical for v0 datasets.
		validity := fmt.Sprintf("valid from day %d", f.From)
		if f.To != 0 {
			validity = fmt.Sprintf("valid from day %d until day %d", f.From, f.To)
		}
		return fmt.Sprintf("[day %d] Observation (%s, source %s, %s): %s.",
			f.From, f.ID, f.Source, validity, b.atomText(f.Atom))
	}
	// Frame-homed facts: tier-E templates with explicit markers (harness
	// debugging only; pre-registered as non-evidence, MASTERPLAN §9.6.6).
	fr := b.w.FrameByID(frame)
	switch {
	case f.Block:
		return fmt.Sprintf("[day %d] Scenario %s assumption (%s, source %s): within this scenario, disregard %s.",
			f.From, frame, f.ID, f.Source, b.atomText(f.Atom))
	case fr != nil && fr.Kind == world.FrameScenario:
		return fmt.Sprintf("[day %d] Scenario %s assumption (%s, source %s): assume %s.",
			f.From, frame, f.ID, f.Source, b.atomText(f.Atom))
	case fr != nil && fr.Kind == world.FrameFiction:
		return fmt.Sprintf("[day %d] Story excerpt (%s, fiction frame %s, source %s): in the story, %s.",
			f.From, f.ID, frame, f.Source, b.atomText(f.Atom))
	case b.predictionByFact[f.ID] != nil:
		p := b.predictionByFact[f.ID]
		return fmt.Sprintf("[day %d] Forecast %s by %s (source frame %s): expects that %s will hold.",
			f.From, f.ID, p.Origin, frame, b.atomText(f.Atom))
	case b.assertKind[f.ID] == assertQuote:
		return fmt.Sprintf("[day %d] Report (%s): according to a statement attributed to frame %s, \"%s\" (a claim, not independently observed).",
			f.From, f.ID, frame, b.atomText(f.Atom))
	default: // perspective narration
		return fmt.Sprintf("[day %d] According to %s (perspective frame %s, source %s): %s.",
			f.From, strings.TrimPrefix(frame, "psp_"), frame, f.Source, b.atomText(f.Atom))
	}
}

func (b *Builder) ruleText(r *world.Rule) string {
	var conds []string
	for _, c := range r.Conditions {
		conds = append(conds, b.patternText(c))
	}
	prefix := "Policy"
	if fr := world.NormFrame(r.FrameID); fr != world.ActualFrame {
		prefix = fmt.Sprintf("Scenario %s policy", fr)
	}
	s := fmt.Sprintf("[day %d] %s %s (%q, authority level %d, effective from day %d",
		r.IssuedAt, prefix, r.ID, r.Name, r.Authority, r.EffectiveFrom)
	if r.EffectiveTo != 0 {
		s += fmt.Sprintf(" until day %d", r.EffectiveTo)
	}
	s += fmt.Sprintf("): IF %s THEN %s", strings.Join(conds, " AND "), b.patternText(r.Conclusion))
	if len(r.Exceptions) > 0 {
		var excs []string
		for _, e := range r.Exceptions {
			excs = append(excs, b.patternText(e))
		}
		s += fmt.Sprintf(", UNLESS %s", strings.Join(excs, " OR "))
	}
	return s + "."
}

func (b *Builder) supText(s *world.Supersession) string {
	if fr := world.NormFrame(s.FrameID); fr != world.ActualFrame {
		return fmt.Sprintf("[day %d] Scenario %s notice %s: within this scenario, policy %s is superseded by policy %s, effective day %d. Outside the scenario, %s still applies.",
			s.From, fr, s.ID, s.OldRule, s.NewRule, s.From, s.OldRule)
	}
	return fmt.Sprintf("[day %d] Notice %s: policy %s is superseded by policy %s, effective day %d. From that day, %s no longer applies.",
		s.From, s.ID, s.OldRule, s.NewRule, s.From, s.OldRule)
}
