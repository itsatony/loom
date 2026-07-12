package main

import (
	"fmt"
	"strings"

	"github.com/vaudience/synthworld/gen"
	"github.com/vaudience/synthworld/world"
)

// Naturalizer prompting (tier M, MASTERPLAN §9.6.6). The naturalizer is
// OUTSIDE the evaluated matrix and is deliberately given ground truth per
// line (a rendering directive): its job is to express that ground truth
// through natural pragmatics instead of tier-E markers. Measured
// conditions never see directives, handles, or this prompt — only the
// resulting episodes_natural.jsonl text.

const naturalizerSystem = `You rewrite lines from a synthetic business operations log into natural, varied business English. You are a careful technical editor: fluent prose, zero information drift.

Each input line may start with a parenthesized DIRECTIVE like "(render as: ...)". The directive is an instruction to YOU about the line's pragmatic status. NEVER copy the directive or any part of it into your output.

Hard rules — violating any of them makes the output unusable:
1. Copy VERBATIM every identifier that contains an underscore (e.g. fct_0042, rul_007, sup_003, customer_03, registry_A, partner_feed). CASE-SENSITIVE: never capitalize one, even at the start of a sentence. Never reformat, translate, or drop one; never invent new identifiers. Quoted policy names (e.g. "requires_review policy 2") are copied verbatim too, including the identifier inside them. EXCEPTION: tokens starting with fic_, psp_ or scn_ are internal tags — they must NEVER appear in your output; the directive tells you what name to use instead.
2. Copy VERBATIM every formal expression of the form name(slot=value, ...) — including ?X variables — exactly as written, punctuation and all. Weave prose AROUND these expressions. Never additionally mention a slot value (like partner_00) in your prose unless the original prose outside the expression also mentions it: slot values live ONLY inside the expression.
3. Keep every number as digits (days, authority levels). Never spell numbers out, never add or drop one. COUNT REPETITIONS: if the input line states a number twice, your line must also contain it twice. The standard pattern "[day D] ... valid from day D" REQUIRES "day D" twice in your line (e.g. "logged on day D, ... in force from day D"). Before finalizing each line, verify every number appears the same number of times as in the input.
4. Preserve meaning exactly: observation lines state a fact and its validity days; policy lines state a conditional (conditions imply a conclusion, with authority and effectivity) — conditionals must remain clearly conditional (if/when/provided...); exceptions must remain clearly exceptional (unless/except...); supersession notices state that one policy replaces another from a day.
5. FORBIDDEN WORDS everywhere in your output: "frame", "fiction", "fictional", "perspective", "non-assertive", "sarcastic", "sarcasm", "ironic", "irony", "promotion", "promoted", and the phrases "not independently observed", "does not believe", "story excerpt". Express those meanings through wording and context instead.
6. Directive-specific rendering:
   - (routine): an ordinary log entry — observation, policy, or notice. Plain assertive prose.
   - (story "TITLE"): the content comes from a work of imaginative writing called TITLE (introduced earlier in the log). Report what holds IN TITLE's world, mentioning TITLE verbatim — the title itself is the only marker allowed. STRICTLY FORBIDDEN in these lines: story, installment, chapter, passage, narrative, tale, plot, novel, serial, manuscript, excerpt, episode, character, protagonist, reading. Write e.g. "In The Glass Harbor, day 42 brings word that supplies(...) — noted as fct_0785" or "The Glass Harbor has it, as of day 42, that supplies(...) (fct_0785)" — a reader who remembers what TITLE is will understand; the line itself stays neutral. Every identifier from the input (fct_..., and the like) must still appear in your line.
   - (statement by X): the content is X's claim, filed or reported — attribute it to X (their filing, submission, briefing, position). Report it indirectly; do not endorse or dispute it.
   - (quote from X): report X's words as DIRECT QUOTED SPEECH: the formal expression must appear inside quotation marks as part of what X said. Vary the reporting verb (said, put it, insisted, told the desk, ...). The record identifier from the input (e.g. fct_0872) must still appear in your line, OUTSIDE the quotation marks — e.g. "recorded as fct_0872" or "(fct_0872)".
   - (forecast by X): X expects/projects/anticipates the fact to hold; clearly forward-looking, clearly X's projection.
   - (exercise "NAME" assumption): a working premise adopted within the planning exercise NAME. NAME is a codename — mention it verbatim but vary the scaffold ("the NAME drill", "under NAME", "the planning exercise NAME", "NAME's working premises"). Make clear the premise holds for the purposes of that exercise.
   - (exercise "NAME" removal): within exercise NAME, the given fact is set aside / treated as not holding. Mention NAME verbatim; the line MUST contain one of these removal phrasings verbatim-ish: "set aside", "disregard", "treated as not holding", "no longer holds", "excluded". Do not substitute other verbs (waive/lift/strike do not count).
   - (exercise "NAME" policy) / (exercise "NAME" notice): the policy or supersession applies within exercise NAME only. Mention NAME verbatim.
   - (opens story "TITLE"): note that TITLE begins circulating / is introduced — imaginative writing, entertainment for the desk. Make its made-up nature clear from context without forbidden words.
   - (opens exercise "NAME" live): announce that planning exercise NAME begins, working from the world as it currently stands and tracking developments as they come.
   - (opens exercise "NAME" pinned day D): announce that planning exercise NAME begins, working from the state of affairs as of day D; later developments are NOT incorporated. Day D must appear as digits.
   - (opens view of X): note that statements from X will from now on be logged as X's own claims, kept apart from the desk's observations.
   - (sarcastic remark by X): render X's remark so a careful reader can tell from wording and context alone that it is ironic and X does NOT mean the literal content — over-the-top agreement, mock enthusiasm, deadpan absurdity, pointed rhetorical praise. NEVER name the device. Keep the formal expression verbatim inside the remark. Vary the style of irony between such lines.
   - (confirmation of P by X via O, day D): the earlier projection P made by X has been borne out by observation O; from day D it stands in the record as established. Avoid forbidden words.
7. Vary sentence structure, voice, and vocabulary between lines — do not reuse one template per directive type. Do not converge on a fixed signature phrase for any status.
8. Output format: exactly one output line per input line, numbered the same way ("1: ...", "2: ..."). No extra lines, no commentary, no code fences.

Example (input → acceptable output):
IN:  1: (routine) [day 12] Observation (fct_0007, source audit_note, valid from day 12): supplies(customer=customer_02, product=product_05).
OUT: 1: An audit_note entry logged on day 12 — reference fct_0007, effective from day 12 — confirms that supplies(customer=customer_02, product=product_05).
IN:  2: (sarcastic remark by asset_15) [day 123] Sarcastic remark by asset_15 (non-assertive; the speaker does not believe the literal content): "Oh sure, rated_as(jurisdiction=jurisdiction_00, product=product_15, jurisdiction2=jurisdiction_02) — obviously."
OUT: 2: asset_15, on day 123, offered this gem for the record: "Right, because rated_as(jurisdiction=jurisdiction_00, product=product_15, jurisdiction2=jurisdiction_02) — what could be more plausible."
IN:  3: (exercise "Blueline" notice) [day 154] Scenario scn_live notice sup_016: within this scenario, policy rul_015 is superseded by policy rul_038, effective day 154. Outside the scenario, rul_015 still applies.
OUT: 3: Logged on day 154, and for the purposes of Blueline only: notice sup_016 has rul_038 replacing rul_015 effective day 154; in the desk's standing records, rul_015 remains in force.`

// directive builds the rendering directive prefix for one event.
func directive(ev *gen.Event, handles map[string]frameHandle) string {
	switch ev.Kind {
	case gen.EvFrame:
		h := handles[ev.Frame.ID]
		switch ev.Frame.Kind {
		case world.FrameFiction:
			return fmt.Sprintf("(opens story %q)", h.Handle)
		case world.FrameScenario:
			if ev.Frame.Basis == world.FramePinned {
				return fmt.Sprintf("(opens exercise %q pinned day %d)", h.Handle, ev.Frame.PinDay)
			}
			return fmt.Sprintf("(opens exercise %q live)", h.Handle)
		default:
			return fmt.Sprintf("(opens view of %s)", h.Entity)
		}
	case gen.EvPromotion:
		p := ev.Promotion
		entity := strings.TrimPrefix(p.FromFrame, "psp_")
		return fmt.Sprintf("(confirmation of %s by %s via %s, day %d)", p.PredictionFactID, entity, p.ActualFactID, p.Day)
	case gen.EvFact:
		f := ev.Fact
		fr := world.NormFrame(f.FrameID)
		if fr == world.ActualFrame {
			if ev.AssertionType == gen.AssertNonAssertive {
				speaker := strings.TrimPrefix(f.Source, "remark_")
				return fmt.Sprintf("(sarcastic remark by %s)", speaker)
			}
			return "(routine)"
		}
		h := handles[fr]
		switch h.Kind {
		case world.FrameFiction:
			return fmt.Sprintf("(story %q)", h.Handle)
		case world.FrameScenario:
			if f.Block {
				return fmt.Sprintf("(exercise %q removal)", h.Handle)
			}
			return fmt.Sprintf("(exercise %q assumption)", h.Handle)
		default: // perspective
			if ev.AssertionType == gen.AssertQuote {
				return fmt.Sprintf("(quote from %s)", h.Entity)
			}
			if strings.Contains(ev.Text, "] Forecast ") {
				return fmt.Sprintf("(forecast by %s)", h.Entity)
			}
			return fmt.Sprintf("(statement by %s)", h.Entity)
		}
	case gen.EvRule:
		if fr := world.NormFrame(ev.Rule.FrameID); fr != world.ActualFrame {
			return fmt.Sprintf("(exercise %q policy)", handles[fr].Handle)
		}
		return "(routine)"
	case gen.EvSupersession:
		if fr := world.NormFrame(ev.Supersession.FrameID); fr != world.ActualFrame {
			return fmt.Sprintf("(exercise %q notice)", handles[fr].Handle)
		}
		return "(routine)"
	}
	return "(routine)"
}

// naturalizeUserMsg builds the user message for one episode.
func naturalizeUserMsg(ep *gen.Episode, handles map[string]frameHandle, feedback string) string {
	var sb strings.Builder
	sb.WriteString(`Rewrite each numbered line per its directive. Same number of lines, same numbering.
CRITICAL REMINDERS:
- Copy every name(slot=value, ...) expression EXACTLY as written, parentheses and all — never re-express its content as prose.
- Keep every number as digits and preserve HOW OFTEN each number appears: "[day 5] ... valid from day 5" mentions 5 twice, so your line must mention 5 twice (e.g. "logged on day 5, in force from day 5").

`)
	for i := range ep.Events {
		fmt.Fprintf(&sb, "%d: %s %s\n", i+1, directive(&ep.Events[i], handles), ep.Events[i].Text)
	}
	if feedback != "" {
		sb.WriteString(feedback)
	}
	return sb.String()
}
