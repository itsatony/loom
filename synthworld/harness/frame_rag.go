package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/vaudience/synthworld/gen"
	"github.com/vaudience/synthworld/world"
)

// FrameRAGCondition is the CEILING NULL for F-E2 (MASTERPLAN §10 2026-07-18):
// the strongest form of "query-time frame reasoning" we can build. It has a
// frameless memory (raw retrieved episode text), full frame semantics in its
// prompt, and the SAME LLM as the extractor — but it makes every frame
// decision PER QUERY from retrieved text, never compiling frame assignments.
// If loom-c2b-frames still beats this on the filtering-resistant traps, then
// "query-time provenance filtering suffices" is refuted against the strongest
// version of itself, not a substring-matching strawman.
//
// It is deliberately handed MORE than c2b-prov (whole naturalized episodes,
// not just extracted items) so any C2b-frames win over it is conservative.
// The frame directory (ID↔handle) is the same naming affordance every
// text-mode condition receives; answers are canonical frame IDs.
type FrameRAGCondition struct {
	Retriever  Retriever
	LLM        LLMClient
	K          int
	FrameNames map[string]string // canonical ID → surface handle
	Label      string

	universe []string // actual + sorted frame IDs
}

func (r *FrameRAGCondition) Name() string {
	if r.Label != "" {
		return r.Label
	}
	return "frame-rag-" + r.Retriever.Name()
}

func (r *FrameRAGCondition) Ingest(episodes []gen.Episode) error {
	r.universe = []string{world.ActualFrame}
	var rest []string
	for id := range r.FrameNames {
		rest = append(rest, id)
	}
	sort.Strings(rest)
	r.universe = append(r.universe, rest...)
	return r.Retriever.Index(episodes)
}

const frameRAGSystemPrompt = ragSystemPrompt + `

This world also has FRAMES — separate contexts a statement can belong to:
- "actual": the desk's own record (real observations, policies in force). Default.
- fiction frames: an invented story. Its statements are NEVER true in actual.
- scenario frames: a planning exercise/drill. It inherits actual's facts and rules, but adds assumptions (adopted facts) and set-asides (disregarded facts) that hold ONLY inside the scenario. A scenario is either live (tracks actual as it changes) or pinned to actual as of a stated day (later actual revisions do NOT enter it).
- perspective frames: claims attributed to one source. The claim is true in that source's frame only, NOT actual — unless it is later confirmed/promoted into the record, from the promotion day onward. A forecast is NOT true in actual before its resolution/promotion day.
- Quoted claims are true only in the speaker's perspective frame. Sarcastic/ironic remarks (the speaker does not believe the literal content) are true in NO frame.

Frame reasoning rules:
- A rule fires inside a frame using that frame's visible facts (its own + inherited); its conclusions belong to that frame only.
- To answer "does P hold in frame F", evaluate F's own closure: inherited actual facts/rules (pinned frames at the pin day) PLUS F's own assumptions/set-asides/claims, then apply the policies.
- "In which frames does P hold" = list every frame (from the directory) whose closure contains P.`

func (r *FrameRAGCondition) directory() string {
	var b strings.Builder
	b.WriteString("Frame directory (canonical id = name used in the text):\n- actual = the desk's own record\n")
	ids := make([]string, 0, len(r.FrameNames))
	for id := range r.FrameNames {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		kind := "perspective"
		switch {
		case strings.HasPrefix(id, "fic_"):
			kind = "fiction"
		case strings.HasPrefix(id, "scn_"):
			kind = "scenario"
		}
		fmt.Fprintf(&b, "- %s = %q (%s frame)\n", id, r.FrameNames[id], kind)
	}
	return b.String()
}

func (r *FrameRAGCondition) ctx(query string) (string, error) {
	res, err := r.Retriever.Retrieve(query, r.K)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, se := range res {
		b.WriteString(se.Text)
		b.WriteString("\n\n")
	}
	return b.String(), nil
}

func (r *FrameRAGCondition) frameQuery(q SanitizedQuery, instruction string) (string, error) {
	c, err := r.ctx(q.Text)
	if err != nil {
		return "", err
	}
	frame := q.Frame
	if frame == "" {
		frame = world.ActualFrame
	}
	return fmt.Sprintf("%s\nEpisodes:\n\n%s\n%s\nQuery frame: %s\nQuestion: %s\nStructured form: %s\n%s",
		r.directory(), c, "", frame, q.Text, structuredQueryJSON(q), instruction), nil
}

func (r *FrameRAGCondition) AnswerHolds(q SanitizedQuery) (bool, error) {
	user, err := r.frameQuery(q, "Answer whether the atom holds in the query frame. Reply with exactly one word: true or false.")
	if err != nil {
		return false, err
	}
	out, err := r.LLM.Complete(context.Background(), frameRAGSystemPrompt, user)
	if err != nil {
		return false, err
	}
	return parseHoldsAnswer(out)
}

func (r *FrameRAGCondition) AnswerFind(q SanitizedQuery) ([]string, error) {
	user, err := r.frameQuery(q, "List the matching entity IDs in the query frame. Reply with ONLY a JSON array of entity IDs, e.g. [\"customer_03\"], or [].")
	if err != nil {
		return nil, err
	}
	out, err := r.LLM.Complete(context.Background(), frameRAGSystemPrompt, user)
	if err != nil {
		return nil, err
	}
	return parseFindAnswer(out)
}

func (r *FrameRAGCondition) AnswerWhichFrames(q SanitizedQuery) ([]string, error) {
	inst := fmt.Sprintf("List every frame in which the atom holds, choosing from these canonical ids: [%s]. Reply with ONLY a JSON array of canonical frame ids, e.g. [\"actual\",\"scn_live\"], or [].",
		strings.Join(r.universe, ", "))
	user, err := r.frameQuery(q, inst)
	if err != nil {
		return nil, err
	}
	out, err := r.LLM.Complete(context.Background(), frameRAGSystemPrompt, user)
	if err != nil {
		return nil, err
	}
	got, err := parseFindAnswer(out)
	if err != nil {
		return nil, err
	}
	// keep only known canonical ids (map any handle the model echoed back)
	valid := map[string]bool{}
	for _, f := range r.universe {
		valid[f] = true
	}
	handleToID := map[string]string{}
	for id, h := range r.FrameNames {
		handleToID[h] = id
	}
	var out2 []string
	seen := map[string]bool{}
	for _, g := range got {
		id := g
		if !valid[id] {
			if mapped, ok := handleToID[g]; ok {
				id = mapped
			}
		}
		if valid[id] && !seen[id] {
			seen[id] = true
			out2 = append(out2, id)
		}
	}
	sort.Strings(out2)
	return out2, nil
}

func (r *FrameRAGCondition) AnswerFindFramed(q SanitizedQuery) ([]gen.FramedValue, error) {
	inst := fmt.Sprintf("For each frame in [%s], list the entity IDs for which the pattern holds in that frame. Reply with ONLY a JSON array of objects {\"value\":\"<id>\",\"frame\":\"<canonical frame id>\"}, or [].",
		strings.Join(q.FramesScope, ", "))
	user, err := r.frameQuery(q, inst)
	if err != nil {
		return nil, err
	}
	out, err := r.LLM.Complete(context.Background(), frameRAGSystemPrompt, user)
	if err != nil {
		return nil, err
	}
	return parseFramedPairs(out, q.FramesScope, r.FrameNames)
}

// parseFramedPairs extracts [{value,frame}] objects, mapping handles back to
// canonical frame ids and dropping frames outside the query scope.
func parseFramedPairs(out string, scope []string, frameNames map[string]string) ([]gen.FramedValue, error) {
	start := strings.Index(out, "[")
	end := strings.LastIndex(out, "]")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("unparseable framed-pairs answer: %q", firstN(out, 120))
	}
	var raw []struct {
		Value string `json:"value"`
		Frame string `json:"frame"`
	}
	if err := json.Unmarshal([]byte(out[start:end+1]), &raw); err != nil {
		return nil, fmt.Errorf("framed-pairs JSON: %w", err)
	}
	inScope := map[string]bool{}
	for _, f := range scope {
		inScope[f] = true
	}
	handleToID := map[string]string{}
	for id, h := range frameNames {
		handleToID[h] = id
	}
	var pairs []gen.FramedValue
	seen := map[string]bool{}
	for _, p := range raw {
		f := p.Frame
		if !inScope[f] {
			if mapped, ok := handleToID[f]; ok {
				f = mapped
			}
		}
		if !inScope[f] || p.Value == "" {
			continue
		}
		key := f + "|" + p.Value
		if seen[key] {
			continue
		}
		seen[key] = true
		pairs = append(pairs, gen.FramedValue{Value: p.Value, Frame: f})
	}
	return pairs, nil
}
