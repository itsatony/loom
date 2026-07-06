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
)

type Event struct {
	Kind EventKind `json:"kind"`
	Day  int       `json:"day"`
	Text string    `json:"text"`
	// exactly one of the following payloads is set
	Fact         *world.BaseFact     `json:"fact,omitempty"`
	Rule         *world.Rule         `json:"rule,omitempty"`
	Supersession *world.Supersession `json:"supersession,omitempty"`
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
		day  int
		kind EventKind
		idx  int // index into the respective world slice
		key  string
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
			switch re.kind {
			case EvFact:
				f := &b.w.Facts[re.idx]
				f.EpisodeID = ep.ID
				e = Event{Kind: EvFact, Day: re.day, Fact: f, Text: b.factText(f)}
			case EvRule:
				r := &b.w.Rules[re.idx]
				r.EpisodeID = ep.ID
				e = Event{Kind: EvRule, Day: re.day, Rule: r, Text: b.ruleText(r)}
			case EvSupersession:
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
	validity := fmt.Sprintf("valid from day %d", f.From)
	if f.To != 0 {
		validity = fmt.Sprintf("valid from day %d until day %d", f.From, f.To)
	}
	return fmt.Sprintf("[day %d] Observation (%s, source %s, %s): %s.",
		f.From, f.ID, f.Source, validity, b.atomText(f.Atom))
}

func (b *Builder) ruleText(r *world.Rule) string {
	var conds []string
	for _, c := range r.Conditions {
		conds = append(conds, b.patternText(c))
	}
	s := fmt.Sprintf("[day %d] Policy %s (%q, authority level %d, effective from day %d",
		r.IssuedAt, r.ID, r.Name, r.Authority, r.EffectiveFrom)
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
	return fmt.Sprintf("[day %d] Notice %s: policy %s is superseded by policy %s, effective day %d. From that day, %s no longer applies.",
		s.From, s.ID, s.OldRule, s.NewRule, s.From, s.OldRule)
}
