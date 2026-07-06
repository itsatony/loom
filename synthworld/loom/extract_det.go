// DeterministicExtractor parses the templated episode text — the exact
// inverse of gen/episodes.go's factText/ruleText/supText renderers.
//
// THIS IS A CONTROL, NOT A RESULT. Its sole purpose is to isolate pipeline
// bugs from LLM extraction loss: `loom-c2b-det` scoring oracle-equal proves
// the normalization/consistency/hygiene/commit path is lossless, so any
// C2b deficit is attributable to the LLM extractor. Near-oracle scores
// from c2b-det validate the pipeline, never the compiled-substrate thesis
// (a regex that inverts a template it was written against proves nothing
// about real-world compilation).
//
// It reads ONLY ep.Text (hard mode) — never the structured payloads.
package loom

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/vaudience/synthworld/gen"
)

type DeterministicExtractor struct{}

func (DeterministicExtractor) Name() string { return "det" }

var (
	reFact = regexp.MustCompile(`^\[day (\d+)\] Observation \((\w+), source (\w+), valid from day (\d+)(?: until day (\d+))?\): (\w+)\((.*)\)\.$`)
	reRule = regexp.MustCompile(`^\[day (\d+)\] Policy (\w+) \("([^"]*)", authority level (\d+), effective from day (\d+)(?: until day (\d+))?\): IF (.+?) THEN (\w+\(.*?\))(?:, UNLESS (.+))?\.$`)
	reSup  = regexp.MustCompile(`^\[day (\d+)\] Notice (\w+): policy (\w+) is superseded by policy (\w+), effective day (\d+)\. From that day, \w+ no longer applies\.$`)
	reAtom = regexp.MustCompile(`^(\w+)\((.*)\)$`)
)

func (DeterministicExtractor) Extract(ep gen.Episode) ([]Candidate, []string, error) {
	var cands []Candidate
	var problems []string
	for _, line := range strings.Split(ep.Text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "=== Episode") {
			continue
		}
		switch {
		case reFact.MatchString(line):
			m := reFact.FindStringSubmatch(line)
			args, err := parseArgList(m[7], false)
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s: %v (line: %s)", ep.ID, err, line))
				continue
			}
			cands = append(cands, Candidate{
				Kind: CandFact, Confidence: 1.0, SourceSpan: line,
				Fact: &FactCand{
					FactID: m[2], Source: m[3], Relation: m[6], Args: args,
					From: atoi(m[4]), To: atoiOr0(m[5]),
				},
			})
		case reSup.MatchString(line):
			m := reSup.FindStringSubmatch(line)
			cands = append(cands, Candidate{
				Kind: CandSupersession, Confidence: 1.0, SourceSpan: line,
				Sup: &SupCand{NoticeID: m[2], OldRule: m[3], NewRule: m[4], From: atoi(m[5])},
			})
		case reRule.MatchString(line):
			m := reRule.FindStringSubmatch(line)
			conds, err := parsePatternList(m[7], " AND ")
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s: conditions: %v (line: %s)", ep.ID, err, line))
				continue
			}
			concl, err := parsePattern(m[8])
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s: conclusion: %v (line: %s)", ep.ID, err, line))
				continue
			}
			var excs []PatternCand
			if m[9] != "" {
				excs, err = parsePatternList(m[9], " OR ")
				if err != nil {
					problems = append(problems, fmt.Sprintf("%s: exceptions: %v (line: %s)", ep.ID, err, line))
					continue
				}
			}
			cands = append(cands, Candidate{
				Kind: CandRule, Confidence: 1.0, SourceSpan: line,
				Rule: &RuleCand{
					RuleID: m[2], Name: m[3], Authority: atoi(m[4]),
					IssuedAt: atoi(m[1]), EffectiveFrom: atoi(m[5]), EffectiveTo: atoiOr0(m[6]),
					Conditions: conds, Conclusion: concl, Exceptions: excs,
				},
			})
		default:
			problems = append(problems, fmt.Sprintf("%s: unrecognized line: %s", ep.ID, line))
		}
	}
	return cands, problems, nil
}

// parsePatternList splits "rel(a=?A, b=x) SEP rel2(...)" on SEP. Argument
// values never contain spaces, so the separator cannot appear inside a
// pattern.
func parsePatternList(s, sep string) ([]PatternCand, error) {
	var out []PatternCand
	for _, part := range strings.Split(s, sep) {
		p, err := parsePattern(strings.TrimSpace(part))
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func parsePattern(s string) (PatternCand, error) {
	m := reAtom.FindStringSubmatch(s)
	if m == nil {
		return PatternCand{}, fmt.Errorf("not a pattern atom: %q", s)
	}
	args, err := parseArgList(m[2], true)
	if err != nil {
		return PatternCand{}, err
	}
	return PatternCand{Relation: m[1], Args: args}, nil
}

// parseArgList parses "slot=value, slot2=value2"; values are entity IDs or
// (when allowVars) "?X" variables.
func parseArgList(s string, allowVars bool) (map[string]string, error) {
	args := map[string]string{}
	if strings.TrimSpace(s) == "" {
		return args, nil
	}
	for _, kv := range strings.Split(s, ", ") {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			return nil, fmt.Errorf("malformed argument %q", kv)
		}
		slot, val := kv[:eq], kv[eq+1:]
		if !allowVars && strings.HasPrefix(val, "?") {
			return nil, fmt.Errorf("variable %q in ground atom", val)
		}
		args[slot] = val
	}
	return args, nil
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

func atoiOr0(s string) int {
	if s == "" {
		return 0
	}
	return atoi(s)
}
