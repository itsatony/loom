// FramesLLMExtractor — the S2 frames stage under test (MASTERPLAN §9.6.7,
// F-E1..F-E4). Schema-prompted per-episode extraction over NATURALIZED text:
// the model receives the relation vocabulary, the candidate JSON schema with
// frame fields, and fixed few-shot examples written against FICTIONAL
// relations and frame names (never dataset content). Beyond the v0 job
// (facts/rules/supersessions) it must:
//
//   - detect frame declarations (a story opening, a planning exercise
//     starting — live or pinned, a per-source claims log) and their frames'
//     surface names;
//   - home every extracted item in the frame the pragmatics of its line
//     puts it in (the desk's own record = actual);
//   - type speech acts: quoted claims are assertions homed in the speaker's
//     frame; sarcasm/irony is non-assertive and must be extracted AS such
//     (the pipeline then commits it nowhere);
//   - distinguish scenario deltas: adopted assumptions vs set-aside
//     (blocked) inherited facts;
//   - flag promotion notices (a confirmed forecast entering the record).
//
// Frame identity: the extractor emits the surface name exactly as the text
// uses it (story title, exercise codename, speaker entity); the pipeline
// normalizes surface names to canonical frame IDs via the dataset's handle
// table. Which line belongs to which frame is decided HERE, by the model,
// from text alone.
package loom

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/vaudience/synthworld/gen"
)

type FramesLLMExtractor struct {
	LLM      Completer
	vocabRef Vocabulary
	// frameCtx is a directory of already-declared frames (name → kind),
	// primed by the pipeline's declaration pre-pass. Injected into the
	// per-episode prompt so a bare frame handle resolves as story/scenario
	// content. Built from the model's own declaration readings only.
	frameCtx string
}

func NewFramesLLMExtractor(llm Completer, vocab Vocabulary) *FramesLLMExtractor {
	return &FramesLLMExtractor{LLM: llm, vocabRef: vocab}
}

func (e *FramesLLMExtractor) Name() string { return "llm-frames:" + e.LLM.Model() }

// SetFrameContext implements loom.FrameContextExtractor.
func (e *FramesLLMExtractor) SetFrameContext(directory string) { e.frameCtx = directory }

const framesExtractSystemPrompt = `You are a knowledge compiler for an intelligence desk. The desk's log mixes several KINDS of content, and your most important job — besides extracting every item — is to file each item in the right CONTEXT (called a frame):

- "actual": the desk's own record — observations from feeds/registries, policies in force, supersession notices. This is the default frame.
- A STORY (fiction): the log sometimes carries excerpts of an invented narrative, introduced once by a title (e.g. a line announcing that a tale or entertainment named "Some Title" begins). Everything narrated as part of that story belongs to the story's frame, named by its title — even when a later line does not repeat that it is fiction. Story content is NEVER an observation about the world.
- A PLANNING EXERCISE / SCENARIO: introduced once by a codename (e.g. a drill or exercise named "Codename"). Lines "for <Codename>" adopt assumptions, set aside (disregard) real facts within the exercise, issue exercise-only policies, or supersede policies within the exercise only. These belong to the scenario frame, named by its codename. A scenario is either LIVE (tracks the real world as it evolves) or PINNED to the world as of a stated day (later real revisions do not enter it) — the introduction says which.
- A SOURCE'S CLAIMS (perspective): some lines report what a specific source (an entity, e.g. partner_07) claims, files, forecasts, or was quoted saying — as opposed to what the desk itself observed. These belong to that source's frame, named by the source entity itself. This includes: attributed reports/filings, forecasts/predictions by a source, and QUOTED statements.

Speech acts:
- assertion (default): the line states content as holding in its frame.
- "quote": the line reports a source's words (often in quotation marks) without the desk endorsing them. Home the content in the SPEAKER's frame with assertion "quote".
- "non-assertive": sarcasm/irony — the speaker does NOT believe the literal content (mocking tone, "oh sure", "naturally ... who would doubt it", etc.). Extract the literal atom with assertion "non-assertive" and frame "" — it is asserted nowhere.

You reply with STRICT JSON only — no prose, no code fences.

Output schema:
{
  "frames": [{"name": "<exact surface name from the text>", "kind": "fiction|scenario|perspective", "basis": "live|pinned (scenario only)", "pin_day": <int, pinned only>, "created_day": <int>, "entity": "<entity id, perspective only>", "confidence": <0..1>, "line": "<the exact input line>"}],
  "facts": [{"fact_id": "<id if the line names one, else \"\">", "relation": "<relation name>", "args": {"<slot>": "<entity_id>"}, "valid_from": <int>, "valid_to": <int, 0 if open>, "source": "<feed/registry name for actual observations, else \"\">", "frame": "<\"\" or \"actual\" for the desk's record; story title; exercise codename; or speaker entity>", "block": <true iff the line SETS ASIDE / disregards this fact within a scenario>, "assertion": "<\"\" | \"quote\" | \"non-assertive\">", "confidence": <0..1>, "line": "<the exact input line>"}],
  "rules": [{"rule_id": "...", "name": "...", "authority": <1..5>, "issued_at": <int>, "effective_from": <int>, "effective_to": <int, 0 if open>,
             "conditions": [{"relation": "...", "args": {"<slot>": "<entity_id or ?VAR>"}}],
             "conclusion": {"relation": "...", "args": {...}},
             "exceptions": [{"relation": "...", "args": {...}}],
             "frame": "<\"\" for a normal policy; exercise codename for a scenario-only policy>",
             "confidence": <0..1>, "line": "<the exact input line>"}],
  "supersessions": [{"notice_id": "...", "old_rule": "...", "new_rule": "...", "from": <int>, "frame": "<\"\" for a normal notice; exercise codename when it applies within the exercise only>", "confidence": <0..1>, "line": "<the exact input line>"}],
  "promotions": [{"prediction_fact_id": "...", "actual_fact_id": "...", "from_frame": "<speaker entity>", "day": <int>, "confidence": <0..1>, "line": "<the exact input line>"}]
}

Rules of extraction:
- Atom expressions appear verbatim in the text as relation(slot=value, ...). Copy relation names, slot names, and values EXACTLY. Variables are written ?A, ?B …; keep them verbatim.
- Every content line yields exactly one item (a frame declaration, fact, rule, supersession, or promotion). Extract EVERY line. Do not invent items.
- "valid/effective from day X" with no end means valid_to/effective_to = 0; "until day Y" means Y.
- A line adopting an assumption within an exercise is a fact with block=false; a line setting aside / disregarding a fact within an exercise is that fact with block=true. Both belong to the exercise's frame.
- A story's introduction line, an exercise's launch line, and a line opening a separate claims log for a source are frame declarations, not facts. Deciding the KIND: anything that runs against the real world's state — a drill, exercise, planning session, what-if — is "scenario", even when described in playful or narrative language; its introduction says either that it tracks the live/unfolding world (basis "live") or that it uses the world as of an earlier stated day and later revisions do not enter it (basis "pinned"). "fiction" is reserved for invented tales/entertainments whose happenings are never observations about the world. A line announcing that a particular source's statements/claims will henceforth be filed separately (apart from the desk's own observations) declares a "perspective" frame for that entity — do not skip these.
- A line reporting that a source's earlier forecast is confirmed by an observation and enters the record is a promotion notice, not a fact.
- Frame NAMES must be copied EXACTLY and MINIMALLY: the bare codename or title as introduced ("Ridgeline", never "the Ridgeline drill" or "Under Ridgeline"), or the bare source entity id ("person_07"). The frame of an attributed claim/filing/forecast is the entity DOING the claiming (the subject attributing), never an entity that merely appears in the atom's arguments.
- CRITICAL — story/scenario continuation: a story or exercise is introduced ONCE by name, then later lines report its contents by that name WITHOUT repeating that it is fiction/an exercise (e.g. "In Millwater, day 76 has it that <atom>", "Millwater has this, as of day 70: <atom>"). Those lines are STILL that frame's content, NOT the desk's observations. A bare capitalized name like "Millwater" or "The Orchard Ledger" at the start of a line is a STORY/EXERCISE name, not a place or source — if it names a known frame (see the frame directory above when provided), home the line to that frame. When a line's context is genuinely ambiguous between the desk's own record and a story/scenario/quote, it is NOT the desk's record.
- CONFIDENCE is load-bearing for actual-homing: set confidence 1.0 only when you are sure a fact is the desk's OWN observation (an explicit feed/registry/report source, no story/exercise/quote framing). If you home a fact to "" (actual) but are NOT sure it is the desk's own observation rather than story/scenario/quoted content, set its confidence to 0.4 — it will be quarantined (stored, not believed) rather than wrongly entered into the record. Facts homed to a named non-actual frame keep normal confidence.
- The frame field is NEVER a data source, feed, registry, or file name (field reports, registries, feeds, disclosures are the desk's own record: frame "" with that name in "source"). Only story titles, exercise codenames, and claiming entities are frames.
- A pinned exercise has TWO days: the day it opens (created_day) and the earlier day whose state it freezes ("as of day X" / "as it stood on day X" = pin_day). Extract both; basis is "pinned" whenever later real revisions are said not to enter it, else "live".
- Use only relation names from the provided vocabulary. Empty categories are empty arrays. Reply with the JSON object only.

Example (fictional relations and names, for format only):
Input line: A hq_feed entry logged on day 3 — reference fct_9001, effective from day 3 — confirms that mentors(person=person_02, person2=person_05).
Output fact: {"fact_id":"fct_9001","relation":"mentors","args":{"person":"person_02","person2":"person_05"},"valid_from":3,"valid_to":0,"source":"hq_feed","frame":"","block":false,"assertion":"","confidence":1.0,"line":"..."}
Input line: Day 12 introduces The Salt Meridian, an entertainment for the desk — its happenings are not logged observations.
Output frame: {"name":"The Salt Meridian","kind":"fiction","created_day":12,"confidence":1.0,"line":"..."}
Input line: Day 20, and inside The Salt Meridian, mentors(person=person_09, person2=person_01) is how things stand — noted as fct_9010.
Output fact: {"fact_id":"fct_9010","relation":"mentors","args":{"person":"person_09","person2":"person_01"},"valid_from":20,"valid_to":0,"source":"","frame":"The Salt Meridian","block":false,"assertion":"","confidence":1.0,"line":"..."}
Input line: As of day 30, the desk launched the Ridgeline drill, pinned to the world as it stood on day 25; later revisions of the record do not enter it.
Output frame: {"name":"Ridgeline","kind":"scenario","basis":"pinned","pin_day":25,"created_day":30,"confidence":1.0,"line":"..."}
Input line: In the Ridgeline drill on day 33, assumption fct_9020 sets aside certified(person=person_02).
Output fact: {"fact_id":"fct_9020","relation":"certified","args":{"person":"person_02"},"valid_from":33,"valid_to":0,"source":"","frame":"Ridgeline","block":true,"assertion":"","confidence":1.0,"line":"..."}
Input line: person_07 was quoted on day 40 as saying, "certified(person=person_03)" — recorded as fct_9031.
Output fact: {"fact_id":"fct_9031","relation":"certified","args":{"person":"person_03"},"valid_from":40,"valid_to":0,"source":"","frame":"person_07","assertion":"quote","block":false,"confidence":1.0,"line":"..."}
Input line: person_05, on day 44, scoffed: "Oh sure, certified(person=person_08) — naturally."
Output fact: {"fact_id":"","relation":"certified","args":{"person":"person_08"},"valid_from":44,"valid_to":0,"source":"","frame":"","assertion":"non-assertive","block":false,"confidence":1.0,"line":"..."}
Input line: Logged on day 50 and effective from day 50 for Ridgeline only: policy rul_900 ("mentor gate", authority level 2) states that IF mentors(person=?A, person2=?B) THEN may_review(person=?A, person2=?B), UNLESS suspended(person=?A).
Output rule: {"rule_id":"rul_900","name":"mentor gate","authority":2,"issued_at":50,"effective_from":50,"effective_to":0,"conditions":[{"relation":"mentors","args":{"person":"?A","person2":"?B"}}],"conclusion":{"relation":"may_review","args":{"person":"?A","person2":"?B"}},"exceptions":[{"relation":"suspended","args":{"person":"?A"}}],"frame":"Ridgeline","confidence":1.0,"line":"..."}
Input line: The forecast fct_9040 by person_07, confirmed by observation fct_9055, enters the record on day 60, logged as prm_01.
Output promotion: {"prediction_fact_id":"fct_9040","actual_fact_id":"fct_9055","from_frame":"person_07","day":60,"confidence":1.0,"line":"..."}`

// framesCandidateEnvelope mirrors the prompt's output schema.
type framesCandidateEnvelope struct {
	Frames []struct {
		FrameCand
		Confidence float64 `json:"confidence"`
		Line       string  `json:"line"`
	} `json:"frames"`
	Facts []struct {
		FactCand
		Confidence float64 `json:"confidence"`
		Line       string  `json:"line"`
	} `json:"facts"`
	Rules []struct {
		RuleCand
		Confidence float64 `json:"confidence"`
		Line       string  `json:"line"`
	} `json:"rules"`
	Supersessions []struct {
		SupCand
		Confidence float64 `json:"confidence"`
		Line       string  `json:"line"`
	} `json:"supersessions"`
	Promotions []struct {
		PromoCand
		Confidence float64 `json:"confidence"`
		Line       string  `json:"line"`
	} `json:"promotions"`
}

func (e *FramesLLMExtractor) Extract(ep gen.Episode) ([]Candidate, []string, error) {
	return e.extractWith(ep, "")
}

// extractWith runs one extraction pass. nonce (when non-empty) is appended to
// the user prompt so the self-consistency wrapper can force K DISTINCT requests
// (distinct cache keys) from the same episode; with temperature>0 the model
// then genuinely resamples. The nonce is an inert trailing tag.
func (e *FramesLLMExtractor) extractWith(ep gen.Episode, nonce string) ([]Candidate, []string, error) {
	ctxBlock := ""
	if e.frameCtx != "" {
		ctxBlock = "\n" + e.frameCtx + "\n"
	}
	user := fmt.Sprintf("Relation vocabulary (name: slots):\n%s\n%sEpisode text:\n%s\n\nExtract all items as JSON.",
		vocabPromptLines(e.vocabRef), ctxBlock, stripEpisodeHeader(ep.Text))
	if nonce != "" {
		user += "\n\n[pass:" + nonce + "]"
	}
	out, err := e.LLM.Complete(context.Background(), framesExtractSystemPrompt, user)
	if err != nil {
		return nil, nil, fmt.Errorf("episode %s: %w", ep.ID, err)
	}
	var env framesCandidateEnvelope
	if perr := parseEnvelopeJSON(out, &env); perr != nil {
		return nil, nil, fmt.Errorf("episode %s: %v", ep.ID, perr)
	}
	var cands []Candidate
	var problems []string
	for _, f := range env.Frames {
		fc := f.FrameCand
		cands = append(cands, Candidate{Kind: CandFrame, Frame: &fc, Confidence: clamp01(f.Confidence), SourceSpan: f.Line})
	}
	for _, f := range env.Facts {
		fc := f.FactCand
		cands = append(cands, Candidate{Kind: CandFact, Fact: &fc, Confidence: clamp01(f.Confidence), SourceSpan: f.Line})
	}
	for _, r := range env.Rules {
		rc := r.RuleCand
		cands = append(cands, Candidate{Kind: CandRule, Rule: &rc, Confidence: clamp01(r.Confidence), SourceSpan: r.Line})
	}
	for _, s := range env.Supersessions {
		sc := s.SupCand
		cands = append(cands, Candidate{Kind: CandSupersession, Sup: &sc, Confidence: clamp01(s.Confidence), SourceSpan: s.Line})
	}
	for _, p := range env.Promotions {
		pc := p.PromoCand
		cands = append(cands, Candidate{Kind: CandPromotion, Promo: &pc, Confidence: clamp01(p.Confidence), SourceSpan: p.Line})
	}
	nLines := 0
	for _, line := range strings.Split(ep.Text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "=== Episode") {
			nLines++
		}
	}
	if len(cands) < nLines {
		problems = append(problems, fmt.Sprintf("%s: %d event lines but %d candidates extracted", ep.ID, nLines, len(cands)))
	}
	return cands, problems, nil
}

// stripEpisodeHeader removes the harness scaffolding header lines
// ("=== Episode ep_003 (day N) ===") from the text shown to the LLM: they
// are not world content, and a frames-primed model otherwise mis-reads the
// episode ID as a frame name (dev-99: 455 actual facts exiled to spurious
// ep_NNN frames). The deterministic extractors already skip these lines.
func stripEpisodeHeader(text string) string {
	lines := strings.Split(text, "\n")
	out := lines[:0]
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "=== Episode") {
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

// parseEnvelopeJSON accepts the raw model reply, tolerating code fences and
// stray prose around exactly one JSON object (shared with the v0 extractor's
// leniency rules).
func parseEnvelopeJSON(out string, v any) error {
	s := strings.TrimSpace(out)
	if i := strings.Index(s, "{"); i > 0 {
		s = s[i:]
	}
	if i := strings.LastIndex(s, "}"); i >= 0 {
		s = s[:i+1]
	}
	if err := json.Unmarshal([]byte(s), v); err != nil {
		return fmt.Errorf("extraction reply is not the expected JSON (%v); reply head: %.160s", err, out)
	}
	return nil
}
