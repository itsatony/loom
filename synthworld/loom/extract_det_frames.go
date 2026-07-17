// FramesDeterministicExtractor parses the frames-v1 TIER-E templated episode
// text — the exact inverse of gen/episodes.go's frame-aware renderers plus
// the frame-declaration / sarcasm / promotion event templates in
// gen/frames.go.
//
// THIS IS A CONTROL, NOT A RESULT (same status as DeterministicExtractor):
// `loom-c2b-frames-det` scoring frame-oracle-equal on tier E proves the
// frame-aware normalization/consistency/hygiene/commit path is lossless, so
// any C2b(frames) deficit on tier M is attributable to LLM extraction. It is
// expected — and pre-registered, MASTERPLAN §9.6.6 — that this extractor
// COLLAPSES on naturalized text; that collapse is part of the tier-M
// authenticity story, never a bug.
//
// It reads ONLY ep.Text (hard mode) — never the structured payloads.
package loom

import (
	"fmt"
	"strconv"
	"strings"

	"regexp"

	"github.com/vaudience/synthworld/gen"
)

type FramesDeterministicExtractor struct{}

func (FramesDeterministicExtractor) Name() string { return "frames-det" }

var (
	// Frame declarations (gen/frames.go).
	reFrmFiction = regexp.MustCompile(`^\[day (\d+)\] Frame declaration: fiction frame (\w+) opens\. Its contents are a story — narrative, not observations\.$`)
	reFrmPersp   = regexp.MustCompile(`^\[day (\d+)\] Frame declaration: perspective frame (\w+) opens for claims attributed to (\w+)\.$`)
	reFrmLive    = regexp.MustCompile(`^\[day (\d+)\] Frame declaration: planning scenario (\w+) opens, tracking the live actual world\.$`)
	reFrmPinned  = regexp.MustCompile(`^\[day (\d+)\] Frame declaration: planning scenario (\w+) opens, pinned to the actual world as of day (\d+)\. Later actual revisions do not enter it\.$`)

	// Frame-homed fact templates (gen/episodes.go factText).
	reScnBlock  = regexp.MustCompile(`^\[day (\d+)\] Scenario (\w+) assumption \((\w+)\): within this scenario, disregard (\w+)\((.*)\)\.$`)
	reScnAssume = regexp.MustCompile(`^\[day (\d+)\] Scenario (\w+) assumption \((\w+)\): assume (\w+)\((.*)\)\.$`)
	reStory     = regexp.MustCompile(`^\[day (\d+)\] Story excerpt \((\w+), fiction frame (\w+)\): in the story, (\w+)\((.*)\)\.$`)
	reForecast  = regexp.MustCompile(`^\[day (\d+)\] Forecast (\w+) by (\w+) \(source frame (\w+)\): expects that (\w+)\((.*)\) will hold\.$`)
	reQuote     = regexp.MustCompile(`^\[day (\d+)\] Report \((\w+)\): according to a statement attributed to frame (\w+), "(\w+)\((.*)\)" \(a claim, not independently observed\)\.$`)
	reNarration = regexp.MustCompile(`^\[day (\d+)\] According to (\w+) \(perspective frame (\w+)\): (\w+)\((.*)\)\.$`)
	reSarcasm   = regexp.MustCompile(`^\[day (\d+)\] Sarcastic remark by (\w+) \(non-assertive; the speaker does not believe the literal content\): "Oh sure, (\w+)\((.*)\) — obviously\."$`)

	// Frame-homed rule/supersession + promotion (gen/episodes.go, gen/frames.go).
	reScnRule = regexp.MustCompile(`^\[day (\d+)\] Scenario (\w+) policy (\w+) \("([^"]*)", authority level (\d+), effective from day (\d+)(?: until day (\d+))?\): IF (.+?) THEN (\w+\(.*?\))(?:, UNLESS (.+))?\.$`)
	reScnSup  = regexp.MustCompile(`^\[day (\d+)\] Scenario (\w+) notice (\w+): within this scenario, policy (\w+) is superseded by policy (\w+), effective day (\d+)\. Outside the scenario, \w+ still applies\.$`)
	rePromo   = regexp.MustCompile(`^\[day (\d+)\] Promotion notice (\w+): forecast (\w+) by (\w+) is confirmed by observation (\w+); the claim enters the actual record from day (\d+)\.$`)
)

func (FramesDeterministicExtractor) Extract(ep gen.Episode) ([]Candidate, []string, error) {
	var cands []Candidate
	var problems []string
	prob := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf("%s: ", ep.ID)+fmt.Sprintf(format, args...))
	}
	frameFact := func(line, id, frame, rel, argList, assertion string, day int, block bool) {
		args, err := parseArgList(argList, false)
		if err != nil {
			prob("%v (line: %s)", err, line)
			return
		}
		cands = append(cands, Candidate{
			Kind: CandFact, Confidence: 1.0, SourceSpan: line,
			Fact: &FactCand{
				FactID: id, Relation: rel, Args: args,
				From: day, To: 0,
				Frame: frame, Block: block, Assertion: assertion,
			},
		})
	}
	for _, line := range strings.Split(ep.Text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "=== Episode") {
			continue
		}
		switch {
		case reFrmFiction.MatchString(line):
			m := reFrmFiction.FindStringSubmatch(line)
			cands = append(cands, Candidate{Kind: CandFrame, Confidence: 1.0, SourceSpan: line,
				Frame: &FrameCand{Name: m[2], Kind: "fiction", CreatedDay: atoiDet(m[1])}})
		case reFrmPersp.MatchString(line):
			m := reFrmPersp.FindStringSubmatch(line)
			cands = append(cands, Candidate{Kind: CandFrame, Confidence: 1.0, SourceSpan: line,
				Frame: &FrameCand{Name: m[2], Kind: "perspective", CreatedDay: atoiDet(m[1]), Entity: m[3]}})
		case reFrmLive.MatchString(line):
			m := reFrmLive.FindStringSubmatch(line)
			cands = append(cands, Candidate{Kind: CandFrame, Confidence: 1.0, SourceSpan: line,
				Frame: &FrameCand{Name: m[2], Kind: "scenario", Basis: "live", CreatedDay: atoiDet(m[1])}})
		case reFrmPinned.MatchString(line):
			m := reFrmPinned.FindStringSubmatch(line)
			cands = append(cands, Candidate{Kind: CandFrame, Confidence: 1.0, SourceSpan: line,
				Frame: &FrameCand{Name: m[2], Kind: "scenario", Basis: "pinned", PinDay: atoiDet(m[3]), CreatedDay: atoiDet(m[1])}})

		case reScnBlock.MatchString(line):
			m := reScnBlock.FindStringSubmatch(line)
			frameFact(line, m[3], m[2], m[4], m[5], "", atoiDet(m[1]), true)
		case reScnAssume.MatchString(line):
			m := reScnAssume.FindStringSubmatch(line)
			frameFact(line, m[3], m[2], m[4], m[5], "", atoiDet(m[1]), false)
		case reStory.MatchString(line):
			m := reStory.FindStringSubmatch(line)
			frameFact(line, m[2], m[3], m[4], m[5], "", atoiDet(m[1]), false)
		case reForecast.MatchString(line):
			m := reForecast.FindStringSubmatch(line)
			frameFact(line, m[2], m[4], m[5], m[6], "", atoiDet(m[1]), false)
		case reQuote.MatchString(line):
			m := reQuote.FindStringSubmatch(line)
			frameFact(line, m[2], m[3], m[4], m[5], AssertionQuote, atoiDet(m[1]), false)
		case reNarration.MatchString(line):
			m := reNarration.FindStringSubmatch(line)
			frameFact(line, "", m[3], m[4], m[5], "", atoiDet(m[1]), false)
		case reSarcasm.MatchString(line):
			m := reSarcasm.FindStringSubmatch(line)
			frameFact(line, "", "", m[3], m[4], AssertionNonAssertive, atoiDet(m[1]), false)

		case reScnRule.MatchString(line):
			m := reScnRule.FindStringSubmatch(line)
			rc, err := parseRuleParts(m[3], m[4], m[5], m[1], m[6], m[7], m[8], m[9], m[10])
			if err != nil {
				prob("%v (line: %s)", err, line)
				continue
			}
			rc.Frame = m[2]
			cands = append(cands, Candidate{Kind: CandRule, Confidence: 1.0, SourceSpan: line, Rule: rc})
		case reScnSup.MatchString(line):
			m := reScnSup.FindStringSubmatch(line)
			cands = append(cands, Candidate{Kind: CandSupersession, Confidence: 1.0, SourceSpan: line,
				Sup: &SupCand{NoticeID: m[3], OldRule: m[4], NewRule: m[5], From: atoiDet(m[6]), Frame: m[2]}})
		case rePromo.MatchString(line):
			m := rePromo.FindStringSubmatch(line)
			cands = append(cands, Candidate{Kind: CandPromotion, Confidence: 1.0, SourceSpan: line,
				Promo: &PromoCand{PredictionFactID: m[3], ActualFactID: m[5], Day: atoiDet(m[6])}})

		// v0 templates last (their patterns are prefixes of nothing above).
		case reFact.MatchString(line):
			m := reFact.FindStringSubmatch(line)
			args, err := parseArgList(m[7], false)
			if err != nil {
				prob("%v (line: %s)", err, line)
				continue
			}
			cands = append(cands, Candidate{
				Kind: CandFact, Confidence: 1.0, SourceSpan: line,
				Fact: &FactCand{
					FactID: m[2], Source: m[3], Relation: m[6], Args: args,
					From: atoiDet(m[4]), To: atoiOr0Det(m[5]),
				},
			})
		case reSup.MatchString(line):
			m := reSup.FindStringSubmatch(line)
			cands = append(cands, Candidate{
				Kind: CandSupersession, Confidence: 1.0, SourceSpan: line,
				Sup: &SupCand{NoticeID: m[2], OldRule: m[3], NewRule: m[4], From: atoiDet(m[5])},
			})
		case reRule.MatchString(line):
			m := reRule.FindStringSubmatch(line)
			rc, err := parseRuleParts(m[2], m[3], m[4], m[1], m[5], m[6], m[7], m[8], m[9])
			if err != nil {
				prob("%v (line: %s)", err, line)
				continue
			}
			cands = append(cands, Candidate{Kind: CandRule, Confidence: 1.0, SourceSpan: line, Rule: rc})
		default:
			prob("unrecognized line: %s", line)
		}
	}
	return cands, problems, nil
}

// parseRuleParts assembles a RuleCand from the shared submatch layout of the
// v0 and scenario rule templates: id, name, authority, issued, from, to,
// conditions, conclusion, exceptions.
func parseRuleParts(id, name, authority, issued, from, to, conds, concl, excs string) (*RuleCand, error) {
	cs, err := parsePatternList(conds, " AND ")
	if err != nil {
		return nil, fmt.Errorf("conditions: %w", err)
	}
	cl, err := parsePattern(concl)
	if err != nil {
		return nil, fmt.Errorf("conclusion: %w", err)
	}
	var es []PatternCand
	if excs != "" {
		es, err = parsePatternList(excs, " OR ")
		if err != nil {
			return nil, fmt.Errorf("exceptions: %w", err)
		}
	}
	return &RuleCand{
		RuleID: id, Name: name, Authority: atoiDet(authority),
		IssuedAt: atoiDet(issued), EffectiveFrom: atoiDet(from), EffectiveTo: atoiOr0Det(to),
		Conditions: cs, Conclusion: cl, Exceptions: es,
	}, nil
}

func atoiDet(s string) int { n, _ := strconv.Atoi(s); return n }

func atoiOr0Det(s string) int {
	if s == "" {
		return 0
	}
	return atoiDet(s)
}
