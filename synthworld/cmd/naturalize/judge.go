package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/vaudience/synthworld/gen"
	"github.com/vaudience/synthworld/harness"
	"github.com/vaudience/synthworld/world"
)

// Frame-recoverability audit (MASTERPLAN §9.6.6): a 3-judge panel, outside
// the evaluated matrix and disjoint from the naturalizers, labels every
// line of the naturalized episode with (context, type). A frame-bearing
// line is RECOVERABLE iff ≥2/3 judges produce the exact ground-truth
// label; unrecoverable lines feed back into re-naturalization. Judges see
// only naturalized text (the target episode plus the declaration lines a
// sequential reader would have seen) — never directives, handles maps, or
// structured payloads.

const judgeSystem = `You audit a business operations log. For each numbered line, decide (a) which CONTEXT the line's content belongs to and (b) its TYPE.

Contexts:
- actual — the desk's own record: observations it made, policies and notices in force.
- story "TITLE" — content from a named work of imaginative writing that circulates at the desk (its events are storytelling, not observations). Use the work's title.
- view PARTY — content attributed to a named party: their claim, statement, filing, or projection, reported but not independently established by the desk. Use the party's identifier.
- exercise "NAME" — content that holds only within a named planning exercise / what-if analysis (assumptions, exercise-scoped policies and notices). Use the exercise's name.

Types:
- statement — ordinary assertive content: observations, policies, notices, attributed statements, projections.
- quote — the line reports a party's words as direct quoted speech (their words appear in quotation marks).
- sarcasm — the line records an ironic remark: the speaker does not mean the literal content.
- declaration — the line introduces something rather than asserting content: a work of writing starts circulating, a planning exercise opens, a party's claims begin to be tracked.
- confirmation — the line records that an earlier projection was borne out by observation and now enters the desk's record.

Notes:
- Routine observations arrive via the desk's standard data feeds (identifiers like registry_A, field_report, customer_disclosure, audit_note, partner_feed): these are actual — the feed is the desk's own source, not a party with a viewpoint.
- view PARTY is for parties whose CLAIMS the log tracks (usually announced earlier): their statements, quotes, and projections are theirs, not the desk's, even though the desk logged them.
- An attributed statement or quote belongs to view PARTY even though the desk logged it: the content is the party's, not the desk's.
- A projection by a party is view PARTY | statement. Its later confirmation line is actual | confirmation.
- An ironic remark is actual | sarcasm (it appears in the desk's log; its literal content is asserted nowhere).
- Exercise-scoped policies/notices are exercise "NAME" | statement. Exercise openings are exercise "NAME" | declaration.

Output format: exactly one line per input line, "N: CONTEXT | TYPE", e.g.
1: actual | statement
2: view registry_A | quote
3: exercise "Exercise Blueline" | statement
No commentary, no code fences.`

// lineLabel is one (context, type) label.
type lineLabel struct {
	Kind string // actual | story | view | exercise
	Ref  string // normalized title / entity / exercise name ("" for actual)
	Type string // statement | quote | sarcasm | declaration | confirmation
}

func (l lineLabel) String() string {
	if l.Ref == "" {
		return l.Kind + " | " + l.Type
	}
	return l.Kind + " " + l.Ref + " | " + l.Type
}

// normRef canonicalizes a title/name/entity for comparison.
func normRef(s string) string {
	s = strings.TrimSpace(strings.Trim(strings.TrimSpace(s), `"'“”«»`))
	s = strings.ToLower(s)
	s = strings.TrimPrefix(s, "the ")
	return strings.Join(strings.Fields(s), " ")
}

// expectedLabel derives ground truth for one event.
func expectedLabel(ev *gen.Event, handles map[string]frameHandle) lineLabel {
	frLabel := func(frameID string) lineLabel {
		fr := world.NormFrame(frameID)
		if fr == world.ActualFrame {
			return lineLabel{Kind: "actual"}
		}
		h := handles[fr]
		switch h.Kind {
		case world.FrameFiction:
			return lineLabel{Kind: "story", Ref: normRef(h.Handle)}
		case world.FrameScenario:
			return lineLabel{Kind: "exercise", Ref: normRef(h.Handle)}
		default:
			return lineLabel{Kind: "view", Ref: normRef(h.Entity)}
		}
	}
	switch ev.Kind {
	case gen.EvFrame:
		l := frLabel(ev.Frame.ID)
		l.Type = "declaration"
		return l
	case gen.EvPromotion:
		return lineLabel{Kind: "actual", Type: "confirmation"}
	case gen.EvFact:
		l := frLabel(ev.Fact.FrameID)
		switch ev.AssertionType {
		case gen.AssertNonAssertive:
			l.Type = "sarcasm"
		case gen.AssertQuote:
			l.Type = "quote"
		default:
			l.Type = "statement"
		}
		return l
	case gen.EvRule:
		l := frLabel(ev.Rule.FrameID)
		l.Type = "statement"
		return l
	default: // supersession
		l := frLabel(ev.Supersession.FrameID)
		l.Type = "statement"
		return l
	}
}

// frameBearing reports whether a line's recoverability gates acceptance:
// anything whose expected label is not a plain actual statement.
func frameBearing(exp lineLabel) bool {
	return exp.Kind != "actual" || exp.Type != "statement"
}

var reJudgeLine = regexp.MustCompile(`(?i)^\s*(actual|story|view|exercise)\s*(.*?)\s*\|\s*(statement|quote|sarcasm|declaration|confirmation)\s*\.?\s*$`)

// parseJudgeLabel parses one judge output line body (after the number).
func parseJudgeLabel(s string) (lineLabel, error) {
	m := reJudgeLine.FindStringSubmatch(s)
	if m == nil {
		return lineLabel{}, fmt.Errorf("unparseable label %q", firstN(s, 80))
	}
	l := lineLabel{Kind: strings.ToLower(m[1]), Type: strings.ToLower(m[3])}
	if l.Kind != "actual" {
		l.Ref = normRef(m[2])
		if l.Ref == "" {
			return lineLabel{}, fmt.Errorf("label %q names no story/party/exercise", firstN(s, 80))
		}
	}
	return l, nil
}

// judgeVerdict is one judge's labels for one episode (nil entries =
// unparseable / judge error, which never counts as agreement).
type judgeVerdict struct {
	Judge  string
	Labels []*lineLabel
	Err    string
}

// judgeEpisode asks one judge to label every line of a naturalized episode.
func judgeEpisode(ctx context.Context, judge harness.LLMClient, declContext []string, natLines []string, retries int) judgeVerdict {
	v := judgeVerdict{Judge: judge.Model(), Labels: make([]*lineLabel, len(natLines))}
	var sb strings.Builder
	if len(declContext) > 0 {
		sb.WriteString("Earlier announcements in this log, for reference:\n")
		for _, d := range declContext {
			sb.WriteString("- " + d + "\n")
		}
		sb.WriteString("\n")
	}
	sb.WriteString("Lines to label:\n")
	for i, l := range natLines {
		fmt.Fprintf(&sb, "%d: %s\n", i+1, l)
	}
	user := sb.String()

	feedback := ""
	for attempt := 0; attempt <= retries; attempt++ {
		reply, err := judge.Complete(ctx, judgeSystem, user+feedback)
		if err != nil {
			v.Err = err.Error()
			continue
		}
		parsed, err := parseNumbered(reply, len(natLines))
		if err != nil {
			feedback = fmt.Sprintf("\n\nYour previous reply was rejected: %v. Reply with EXACTLY %d numbered lines of the form \"N: CONTEXT | TYPE\".", err, len(natLines))
			v.Err = err.Error()
			continue
		}
		for i, p := range parsed {
			l, perr := parseJudgeLabel(p)
			if perr != nil {
				// Tolerate isolated unparseable labels: that line simply
				// cannot count as agreement for this judge.
				v.Labels[i] = nil
				continue
			}
			v.Labels[i] = &l
		}
		v.Err = ""
		return v
	}
	return v
}

// tallyLine counts, for one line, how many judges matched ground truth —
// exact (context+type) and context-only (for the actual-line control,
// where quote/statement confusion on plain prose should not gate).
// Sarcasm is matched on type alone: a sarcastic line's literal content is
// asserted nowhere, so recovering the irony is the load-bearing bit — a
// judge who files the speaker as a "view" still refused to believe it.
func tallyLine(verdicts []judgeVerdict, i int, exp lineLabel) (exact, ctxOnly int) {
	for _, v := range verdicts {
		if i >= len(v.Labels) || v.Labels[i] == nil {
			continue
		}
		l := *v.Labels[i]
		if l == exp || (exp.Type == "sarcasm" && l.Type == "sarcasm") {
			exact++
		}
		if l.Kind == exp.Kind && l.Ref == exp.Ref {
			ctxOnly++
		}
	}
	return exact, ctxOnly
}
