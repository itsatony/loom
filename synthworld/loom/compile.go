// S2 — text-mode compilation (spec §5). The pipeline turns episode TEXT
// into store commits through five audited stages: extraction (an Extractor
// implementation), normalization (relation-name → schema ID via the seeded
// Vocabulary; entity IDs are exact aliases in this domain), consistency
// (duplicate / refinement / conflict / supersession-candidate), rule
// handling (stated rules only — no induction in v0), and a hygiene gate
// (schema-cycle trial, join-explosion dry run, post-compile firing-ratio
// flagging). Every stage appends to a machine-readable per-episode trace;
// nothing is silently dropped.
//
// The Vocabulary is the spec §4 "seeded schema": relation IDs, names, and
// slot names for the domain — names only, never facts, rules, supersessions,
// or entities. In production a domain schema is a given; in the experiment
// the harness seeds it from the dataset's relation table. It is the ONLY
// world.json-derived input the pipeline may receive.
package loom

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vaudience/synthworld/gen"
	"github.com/vaudience/synthworld/oracle"
	"github.com/vaudience/synthworld/world"
)

// ---------- Seeded vocabulary (spec §4) ----------

type RelationVocab struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Slots []string `json:"slots"`
}

type Vocabulary struct {
	Relations []RelationVocab `json:"relations"`
}

// byName resolves a surface relation name to its vocabulary entry.
func (v Vocabulary) byName(name string) (RelationVocab, bool) {
	for _, r := range v.Relations {
		if r.Name == name {
			return r, true
		}
	}
	return RelationVocab{}, false
}

// ---------- Extraction candidates ----------

type CandidateKind string

const (
	CandFact         CandidateKind = "fact"
	CandRule         CandidateKind = "rule"
	CandSupersession CandidateKind = "supersession"
	// Frames-v1 candidate kinds (MASTERPLAN §9.6.4): frame declarations and
	// promotion notices extracted from text.
	CandFrame     CandidateKind = "frame"
	CandPromotion CandidateKind = "promotion"
)

// Assertion types an extractor may attach to a fact candidate. Empty means
// plain assertion. Quotes are assertions homed in the speaker's perspective
// frame (the extractor sets Frame accordingly); non-assertive content
// (sarcasm/irony) is asserted in NO frame and is deliberately skipped at
// commit — the literalist failure mode is believing it anywhere.
const (
	AssertionQuote        = "quote"
	AssertionNonAssertive = "non-assertive"
)

// PatternCand is a pre-normalization pattern atom: relation by surface NAME,
// arg values either "?X" (variable) or an entity ID (constant).
type PatternCand struct {
	Relation string            `json:"relation"`
	Args     map[string]string `json:"args"`
}

type FactCand struct {
	FactID   string            `json:"fact_id"`
	Relation string            `json:"relation"` // surface name
	Args     map[string]string `json:"args"`     // slot -> entity ID
	From     int               `json:"valid_from"`
	To       int               `json:"valid_to"` // 0 = open
	Source   string            `json:"source"`
	// Frames-v1 fields (zero values = v0 behavior: an asserted actual fact).
	Frame     string `json:"frame,omitempty"`     // surface frame name; ""/"actual" = actual
	Block     bool   `json:"block,omitempty"`     // frame-scoped removal of an inherited atom
	Assertion string `json:"assertion,omitempty"` // "", "quote", "non-assertive"
}

type RuleCand struct {
	RuleID        string        `json:"rule_id"`
	Name          string        `json:"name"`
	Authority     int           `json:"authority"`
	IssuedAt      int           `json:"issued_at"`
	EffectiveFrom int           `json:"effective_from"`
	EffectiveTo   int           `json:"effective_to"`
	Conditions    []PatternCand `json:"conditions"`
	Conclusion    PatternCand   `json:"conclusion"`
	Exceptions    []PatternCand `json:"exceptions,omitempty"`
	Frame         string        `json:"frame,omitempty"` // surface frame name; ""/"actual" = actual
}

type SupCand struct {
	NoticeID string `json:"notice_id"`
	OldRule  string `json:"old_rule"`
	NewRule  string `json:"new_rule"`
	From     int    `json:"from"`
	Frame    string `json:"frame,omitempty"` // surface frame name; ""/"actual" = actual
}

// FrameCand is an extracted frame declaration. Name is the frame's surface
// name exactly as the text gives it (a raw ID on tier E, a handle on tier M);
// the pipeline normalizes it to the canonical frame ID via FrameNames.
type FrameCand struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`              // fiction | scenario | perspective
	Basis      string `json:"basis,omitempty"`   // scenario only: live | pinned
	PinDay     int    `json:"pin_day,omitempty"` // meaningful iff pinned
	CreatedDay int    `json:"created_day"`
	Entity     string `json:"entity,omitempty"` // perspective only: the narrator entity
}

// PromoCand is an extracted promotion notice (audit trail only in v1: the
// confirming actual observation arrives as its own fact, so promotions have
// no closure impact — exactly like structured ingest).
type PromoCand struct {
	PredictionFactID string `json:"prediction_fact_id"`
	ActualFactID     string `json:"actual_fact_id"`
	FromFrame        string `json:"from_frame"` // surface frame name
	Day              int    `json:"day"`
}

// Candidate is one extracted item plus its audit trail: confidence and the
// exact source span (the episode text line it came from).
type Candidate struct {
	Kind       CandidateKind `json:"kind"`
	Fact       *FactCand     `json:"fact,omitempty"`
	Rule       *RuleCand     `json:"rule,omitempty"`
	Sup        *SupCand      `json:"supersession,omitempty"`
	Frame      *FrameCand    `json:"frame_decl,omitempty"`
	Promo      *PromoCand    `json:"promotion,omitempty"`
	Confidence float64       `json:"confidence"`
	SourceSpan string        `json:"source_span"`
}

// Extractor maps one episode's TEXT to candidates. Implementations must not
// read the structured payloads (ep.Events) — hard mode means text only.
type Extractor interface {
	Name() string
	Extract(ep gen.Episode) ([]Candidate, []string, error) // candidates, problems
}

// ---------- Compilation trace ----------

type ItemVerdict string

const (
	VCommitted    ItemVerdict = "committed"
	VDuplicate    ItemVerdict = "duplicate"     // merged provenance
	VConflict     ItemVerdict = "conflict"      // committed + flagged, or dropped on identity clash
	VQuarantined  ItemVerdict = "quarantined"   // failed hygiene / normalization
	VDropped      ItemVerdict = "dropped"       // unrecoverable candidate (normalization failure w/o quarantinable payload)
	VRefinement   ItemVerdict = "refinement"    // narrower validity than an existing item, committed alongside
	VOverFireFlag ItemVerdict = "overfire-flag" // post-compile firing ratio above threshold (flag, see Pipeline.QuarantineOverFiring)
	// VNonAssertive: the extractor labeled the content non-assertive
	// (sarcasm/irony); it is asserted in NO frame, so skipping the commit IS
	// the correct compilation, not a loss (mirrors IngestReport.NonAssertive).
	VNonAssertive ItemVerdict = "non-assertive-skip"
)

type ItemTrace struct {
	Kind    CandidateKind `json:"kind"`
	ID      string        `json:"id"` // fact/rule/notice ID when known
	Verdict ItemVerdict   `json:"verdict"`
	Detail  string        `json:"detail,omitempty"`
}

type EpisodeTrace struct {
	EpisodeID string      `json:"episode_id"`
	Extracted int         `json:"extracted"`
	Items     []ItemTrace `json:"items"`
	Problems  []string    `json:"problems,omitempty"`
}

// CompileReport is the S2 compilation trace for a whole episode stream.
type CompileReport struct {
	Extractor     string         `json:"extractor"`
	Episodes      int            `json:"episodes"`
	Facts         int            `json:"facts"`
	Rules         int            `json:"rules"`
	Supersessions int            `json:"supersessions"`
	Frames        int            `json:"frames,omitempty"`
	Promotions    int            `json:"promotions,omitempty"`
	NonAssertive  int            `json:"non_assertive_skipped,omitempty"`
	Provisional   int            `json:"provisional_frames,omitempty"` // frames auto-registered from references without a declaration
	Quarantined   int            `json:"quarantined"`
	Conflicts     int            `json:"conflicts"`
	Duplicates    int            `json:"duplicates"`
	Traces        []EpisodeTrace `json:"traces"`
	Hygiene       []string       `json:"hygiene,omitempty"` // post-compile gate log
}

// outcomeByID aggregates the last verdict per item ID, for fidelity's
// missed/dropped/mangled decomposition.
func (r *CompileReport) OutcomeByID() map[string]ItemVerdict {
	out := map[string]ItemVerdict{}
	for _, tr := range r.Traces {
		for _, it := range tr.Items {
			if it.ID != "" {
				out[it.ID] = it.Verdict
			}
		}
	}
	return out
}

// ---------- Pipeline ----------

type Pipeline struct {
	Store     *Store
	Vocab     Vocabulary
	Extractor Extractor

	// FrameNames maps canonical frame IDs to the surface names the episode
	// TEXT uses (tier M bans raw frame IDs; handles come from the dataset's
	// naturalize report — a naming affordance, not frame detection: which
	// LINE belongs to which frame stays entirely the extractor's problem).
	// Empty/missing entries mean the surface name IS the canonical ID
	// (tier E). Normalization happens at commit; the store always keys
	// frames by canonical ID, so query frames resolve directly.
	FrameNames map[string]string

	// surfaceToID is the inverted FrameNames map, built lazily.
	surfaceToID map[string]string
	// provisionalFrames tracks frames auto-registered from a reference
	// without a declaration; only these may be upgraded by a later
	// declaration (a DECLARED frame is immutable — basis/kind are set at
	// creation, §9.6.1; a second declaration is a conflict, kept first).
	provisionalFrames map[string]bool

	// Workers parallelizes EXTRACTION only (LLM calls are independent);
	// commits always happen in episode order so consistency verdicts are
	// deterministic. 0/1 = sequential.
	Workers int

	// FiringRatioThreshold: post-compile, a rule whose conclusion-relation
	// firing ratio exceeds this is flagged (and quarantined when
	// QuarantineOverFiring). Default 0.9 — deliberately above the
	// generator's own repair threshold (0.5): shipped worlds contain
	// relations firing up to ~0.7 which the oracle keeps, so quarantining
	// at 0.5 would make even a perfect extractor diverge from C2a. The
	// gate exists to catch explain-everything rules (LLM hallucinations),
	// not to re-litigate the generator's hygiene.
	FiringRatioThreshold float64
	QuarantineOverFiring bool
}

func NewPipeline(vocab Vocabulary, ex Extractor) *Pipeline {
	return &Pipeline{
		Store:                NewStore(),
		Vocab:                vocab,
		Extractor:            ex,
		FiringRatioThreshold: 0.9,
		QuarantineOverFiring: true,
	}
}

// Compile runs the full loop over an episode stream and returns the
// compilation trace. The store is usable afterwards via the normal ops.
func (p *Pipeline) Compile(episodes []gen.Episode) (*CompileReport, error) {
	rep := &CompileReport{Extractor: p.Extractor.Name(), Episodes: len(episodes)}

	type extracted struct {
		cands    []Candidate
		problems []string
		err      error
	}
	results := make([]extracted, len(episodes))
	workers := p.Workers
	if workers < 1 {
		workers = 1
	}
	if workers > len(episodes) && len(episodes) > 0 {
		workers = len(episodes)
	}
	jobs := make(chan int)
	done := make(chan struct{})
	for w := 0; w < workers; w++ {
		go func() {
			for i := range jobs {
				c, probs, err := p.Extractor.Extract(episodes[i])
				results[i] = extracted{cands: c, problems: probs, err: err}
			}
			done <- struct{}{}
		}()
	}
	for i := range episodes {
		jobs <- i
	}
	close(jobs)
	for w := 0; w < workers; w++ {
		<-done
	}

	// Commit strictly in episode order (deterministic consistency verdicts).
	for i, ep := range episodes {
		res := results[i]
		tr := EpisodeTrace{EpisodeID: ep.ID, Problems: res.problems}
		if res.err != nil {
			// extraction failure is loud but not fatal to the stream:
			// the episode contributes nothing and the trace says why.
			tr.Problems = append(tr.Problems, fmt.Sprintf("extraction error: %v", res.err))
			rep.Traces = append(rep.Traces, tr)
			continue
		}
		tr.Extracted = len(res.cands)
		for _, cand := range res.cands {
			it := p.commitCandidate(ep.ID, cand, rep)
			tr.Items = append(tr.Items, it)
		}
		rep.Traces = append(rep.Traces, tr)
	}

	// Post-compile hygiene: join-explosion dry runs + firing-ratio flags.
	if err := p.hygieneGate(rep); err != nil {
		return rep, err
	}
	return rep, nil
}

// commitCandidate runs normalization + consistency + per-item hygiene for
// one candidate and commits it (or records why not).
func (p *Pipeline) commitCandidate(episodeID string, cand Candidate, rep *CompileReport) ItemTrace {
	prov := Provenance{
		EpisodeIDs: []string{episodeID},
		Confidence: cand.Confidence,
		Extractor:  p.Extractor.Name(),
		Span:       cand.SourceSpan,
	}
	switch cand.Kind {
	case CandFact:
		return p.commitFactCand(cand, prov, rep)
	case CandRule:
		return p.commitRuleCand(cand, prov, rep)
	case CandSupersession:
		return p.commitSupCand(cand, prov, rep)
	case CandFrame:
		return p.commitFrameCand(cand, prov, rep)
	case CandPromotion:
		return p.commitPromoCand(cand, prov, rep)
	}
	return ItemTrace{Kind: cand.Kind, Verdict: VDropped, Detail: "unknown candidate kind"}
}

// resolveFrame maps a surface frame name from extracted text to the
// canonical frame ID. Tier E surfaces raw IDs (identity); tier M surfaces
// handles (inverted FrameNames). Unmapped names pass through unchanged —
// they become extractor-invented frames, unreachable by queries but stored
// (visible extraction loss, never silent).
func (p *Pipeline) resolveFrame(surface string) string {
	s := strings.TrimSpace(surface)
	if s == "" || s == world.ActualFrame {
		return ""
	}
	if p.surfaceToID == nil {
		p.surfaceToID = map[string]string{}
		for id, name := range p.FrameNames {
			p.surfaceToID[name] = id
		}
	}
	if id, ok := p.surfaceToID[s]; ok {
		return id
	}
	// Alias fallback (spec §5 normalization): extractors sometimes decorate
	// a handle ("the Copperfield drill" for handle "Copperfield"). If exactly
	// ONE known handle appears as a word inside the surface name, resolve to
	// it; ambiguity falls through (unknown frame, visible in the report).
	var hit string
	n := 0
	for name, id := range p.surfaceToID {
		if name != "" && containsWord(s, name) {
			if n == 0 || id != hit {
				hit = id
				n++
			}
		}
	}
	if n == 1 {
		return hit
	}
	return s
}

// containsWord reports whether sub occurs in s on word boundaries.
func containsWord(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] != sub {
			continue
		}
		beforeOK := i == 0 || !isWordByte(s[i-1])
		afterOK := i+len(sub) == len(s) || !isWordByte(s[i+len(sub)])
		if beforeOK && afterOK {
			return true
		}
	}
	return false
}

func isWordByte(b byte) bool {
	return b == '_' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// ensureFrame guarantees a frame ID exists in the store before an item homes
// there. When the declaration was missed (or arrives later), a PROVISIONAL
// frame is registered with the kind guessed from the canonical ID prefix and
// the safety default otherwise (perspective — frame-assignment uncertainty
// must fail as stored-but-not-believed, never silently-believed, §9.6.1).
func (p *Pipeline) ensureFrame(frameID, episodeID string, rep *CompileReport) {
	if frameID == "" || frameID == world.ActualFrame || p.Store.frameIDs[frameID] {
		return
	}
	kind := world.FramePerspective
	switch {
	case strings.HasPrefix(frameID, "fic_"):
		kind = world.FrameFiction
	case strings.HasPrefix(frameID, "scn_"):
		kind = world.FrameScenario
	case strings.HasPrefix(frameID, "psp_"):
		kind = world.FramePerspective
	}
	f := world.Frame{ID: frameID, Kind: kind}
	prov := Provenance{EpisodeIDs: []string{episodeID}, Confidence: 0.5, Extractor: p.Extractor.Name()}
	if err := p.Store.CommitFrame(f, prov); err == nil {
		if p.provisionalFrames == nil {
			p.provisionalFrames = map[string]bool{}
		}
		p.provisionalFrames[frameID] = true
		rep.Provisional++
		rep.Hygiene = append(rep.Hygiene, fmt.Sprintf("frame %s auto-registered provisionally (kind %s) — no declaration extracted before first use", frameID, kind))
	}
}

func (p *Pipeline) commitFrameCand(cand Candidate, prov Provenance, rep *CompileReport) ItemTrace {
	fc := cand.Frame
	id := p.resolveFrame(fc.Name)
	it := ItemTrace{Kind: CandFrame, ID: id}
	var kind world.FrameKind
	switch fc.Kind {
	case "fiction":
		kind = world.FrameFiction
	case "scenario":
		kind = world.FrameScenario
	case "perspective":
		kind = world.FramePerspective
	default:
		it.Verdict, it.Detail = VDropped, fmt.Sprintf("unknown frame kind %q (span: %s)", fc.Kind, firstSpan(cand.SourceSpan))
		return it
	}
	f := world.Frame{ID: id, Kind: kind, CreatedDay: fc.CreatedDay}
	if kind == world.FrameScenario {
		switch fc.Basis {
		case "pinned":
			f.Basis = world.FramePinned
			f.PinDay = fc.PinDay
		default:
			f.Basis = world.FrameLive
		}
	}
	// A provisional frame created by an earlier reference upgrades to the
	// declared shape (the first real declaration is authoritative). A frame
	// that was already DECLARED is immutable — kind/basis are set at
	// creation (§9.6.1); a conflicting re-declaration keeps the first and
	// is flagged, never silently overwrites (caught live on dev seed 99: a
	// story-flavored line alias-resolved onto the pinned scenario and
	// flipped its kind to fiction, severing inheritance).
	if p.Store.frameIDs[id] {
		if p.provisionalFrames[id] {
			for i := range p.Store.Frames {
				if p.Store.Frames[i].Frame.ID == id {
					p.Store.Frames[i].Frame = f
					p.Store.Frames[i].Provenance.EpisodeIDs = mergeIDs(p.Store.Frames[i].Provenance.EpisodeIDs, prov.EpisodeIDs)
					p.Store.invalidate()
					break
				}
			}
			delete(p.provisionalFrames, id)
			it.Verdict, it.Detail = VDuplicate, "provisional frame upgraded by declaration"
			rep.Duplicates++
			return it
		}
		it.Verdict, it.Detail = VConflict, "frame already declared; re-declaration ignored (kind/basis are immutable at creation)"
		rep.Conflicts++
		return it
	}
	if err := p.Store.CommitFrame(f, prov); err != nil {
		it.Verdict, it.Detail = VDropped, err.Error()
		return it
	}
	rep.Frames++
	it.Verdict = VCommitted
	return it
}

func (p *Pipeline) commitPromoCand(cand Candidate, prov Provenance, rep *CompileReport) ItemTrace {
	pc := cand.Promo
	it := ItemTrace{Kind: CandPromotion, ID: pc.PredictionFactID}
	ev := gen.PromotionEvent{
		PredictionFactID: pc.PredictionFactID,
		ActualFactID:     pc.ActualFactID,
		FromFrame:        p.resolveFrame(pc.FromFrame),
		Day:              pc.Day,
	}
	if err := p.Store.CommitPromotion(ev, prov); err != nil {
		it.Verdict, it.Detail = VDropped, err.Error()
		return it
	}
	rep.Promotions++
	it.Verdict = VCommitted
	return it
}

func (p *Pipeline) commitFactCand(cand Candidate, prov Provenance, rep *CompileReport) ItemTrace {
	fc := cand.Fact
	it := ItemTrace{Kind: CandFact, ID: fc.FactID}
	// Non-assertive speech (sarcasm/irony): the literal content is asserted
	// in NO frame — committing it anywhere would be the literalist failure
	// mode. Skipping IS the correct compilation (mirrors structured ingest).
	if fc.Assertion == AssertionNonAssertive {
		rep.NonAssertive++
		it.Verdict = VNonAssertive
		return it
	}
	rv, ok := p.Vocab.byName(fc.Relation)
	if !ok {
		it.Verdict, it.Detail = VDropped, fmt.Sprintf("unknown relation %q (span: %s)", fc.Relation, firstSpan(cand.SourceSpan))
		return it
	}
	args, err := normalizeArgs(rv, fc.Args, false)
	if err != nil {
		it.Verdict, it.Detail = VDropped, err.Error()
		return it
	}
	frameID := p.resolveFrame(fc.Frame)
	// Block facts are frame-delta mechanics; a block homed in actual is a
	// schema violation the evaluator would reject — quarantineable content
	// doesn't exist for facts, so drop loudly.
	if fc.Block && frameID == "" {
		it.Verdict, it.Detail = VDropped, "block fact homed in actual (blocks are frame-delta mechanics)"
		return it
	}
	p.ensureFrame(frameID, prov.EpisodeIDs[0], rep)
	f := world.BaseFact{
		ID:        fc.FactID,
		Atom:      world.Atom{Relation: rv.ID, Args: args},
		From:      fc.From,
		To:        fc.To,
		Source:    fc.Source,
		FrameID:   frameID,
		Block:     fc.Block,
		EpisodeID: prov.EpisodeIDs[0],
	}
	// consistency: exact duplicate (atom+interval) merges provenance inside
	// CommitFact; a narrower interval on a known atom is a refinement;
	// everything else is new. Assert-only facts cannot contradict.
	verdict := VCommitted
	switch p.Store.factRelation(f) {
	case factDuplicate:
		verdict = VDuplicate
		rep.Duplicates++
	case factRefinement:
		verdict = VRefinement
	}
	if err := p.Store.CommitFact(f, prov); err != nil {
		it.Verdict, it.Detail = VDropped, err.Error()
		return it
	}
	rep.Facts++
	it.Verdict = verdict
	return it
}

func (p *Pipeline) commitRuleCand(cand Candidate, prov Provenance, rep *CompileReport) ItemTrace {
	rc := cand.Rule
	it := ItemTrace{Kind: CandRule, ID: rc.RuleID}
	conds, err := p.normalizePatterns(rc.Conditions)
	if err != nil {
		it.Verdict, it.Detail = VDropped, "conditions: "+err.Error()
		return it
	}
	concl, err := p.normalizePatterns([]PatternCand{rc.Conclusion})
	if err != nil {
		it.Verdict, it.Detail = VDropped, "conclusion: "+err.Error()
		return it
	}
	excs, err := p.normalizePatterns(rc.Exceptions)
	if err != nil {
		it.Verdict, it.Detail = VDropped, "exceptions: "+err.Error()
		return it
	}
	frameID := p.resolveFrame(rc.Frame)
	p.ensureFrame(frameID, prov.EpisodeIDs[0], rep)
	r := world.Rule{
		ID:            rc.RuleID,
		Name:          rc.Name,
		Conditions:    conds,
		Conclusion:    concl[0],
		Assert:        true, // v0.1 worlds are assert-only; block polarity is not rendered in text
		Exceptions:    excs,
		Authority:     rc.Authority,
		IssuedAt:      rc.IssuedAt,
		EffectiveFrom: rc.EffectiveFrom,
		EffectiveTo:   rc.EffectiveTo,
		FrameID:       frameID,
		EpisodeID:     prov.EpisodeIDs[0],
	}

	// consistency: same ID, different content = conflict. Rule identity is
	// its ID (supersessions reference it); v0 keeps the first commit and
	// flags the clash rather than inventing a synthetic ID.
	if existing := p.Store.ruleByID(r.ID); existing != nil {
		if RulesEquivalent(&existing.Rule, &r) {
			_ = p.Store.CommitRule(r, prov) // provenance merge path
			rep.Duplicates++
			it.Verdict = VDuplicate
			return it
		}
		rep.Conflicts++
		it.Verdict, it.Detail = VConflict, "rule ID already committed with different content; kept first, dropped this"
		return it
	}

	// hygiene, pre-commit: safety + stratification trial (catches unsafe
	// rules and schema cycles an LLM might hallucinate), then connectivity
	// as a flag. Quarantined rules are stored (audit) but never evaluated.
	if reason := p.trialRule(&r); reason != "" {
		if err := p.Store.commitRuleWithLifecycle(r, prov, Quarantined); err != nil {
			it.Verdict, it.Detail = VDropped, err.Error()
			return it
		}
		rep.Quarantined++
		it.Verdict, it.Detail = VQuarantined, reason
		return it
	}
	if err := p.Store.CommitRule(r, prov); err != nil {
		it.Verdict, it.Detail = VDropped, err.Error()
		return it
	}
	rep.Rules++
	it.Verdict = VCommitted
	if !conditionsConnected(&r) {
		it.Detail = "note: condition variable graph is disconnected (join cost is guarded at eval time)"
	}
	return it
}

func (p *Pipeline) commitSupCand(cand Candidate, prov Provenance, rep *CompileReport) ItemTrace {
	sc := cand.Sup
	it := ItemTrace{Kind: CandSupersession, ID: sc.NoticeID}
	frameID := p.resolveFrame(sc.Frame)
	p.ensureFrame(frameID, prov.EpisodeIDs[0], rep)
	sp := world.Supersession{
		ID:        sc.NoticeID,
		OldRule:   sc.OldRule,
		NewRule:   sc.NewRule,
		From:      sc.From,
		FrameID:   frameID,
		EpisodeID: prov.EpisodeIDs[0],
	}
	// The referenced rules may arrive in later episodes; the evaluator
	// resolves by ID at eval time, so dangling references are tolerable
	// at commit and reported in stats. Nothing to normalize (rule IDs are
	// canonical in text).
	if err := p.Store.CommitSupersession(sp, prov); err != nil {
		it.Verdict, it.Detail = VDropped, err.Error()
		return it
	}
	rep.Supersessions++
	it.Verdict = VCommitted
	return it
}

// normalizePatterns maps surface relation names to schema IDs and arg
// strings to world.Term (leading '?' = variable).
func (p *Pipeline) normalizePatterns(cands []PatternCand) ([]world.PatternAtom, error) {
	var out []world.PatternAtom
	for _, pc := range cands {
		rv, ok := p.Vocab.byName(pc.Relation)
		if !ok {
			return nil, fmt.Errorf("unknown relation %q", pc.Relation)
		}
		args := map[string]world.Term{}
		for slot, val := range pc.Args {
			if !slotKnown(rv, slot) {
				return nil, fmt.Errorf("relation %s has no slot %q", rv.Name, slot)
			}
			if strings.HasPrefix(val, "?") {
				args[slot] = world.V(strings.TrimPrefix(val, "?"))
			} else {
				args[slot] = world.C(val)
			}
		}
		if len(args) != len(rv.Slots) {
			return nil, fmt.Errorf("relation %s: got %d args, schema has %d slots", rv.Name, len(args), len(rv.Slots))
		}
		out = append(out, world.PatternAtom{Relation: rv.ID, Args: args})
	}
	return out, nil
}

func normalizeArgs(rv RelationVocab, in map[string]string, allowVars bool) (map[string]string, error) {
	if len(in) != len(rv.Slots) {
		return nil, fmt.Errorf("relation %s: got %d args, schema has %d slots", rv.Name, len(in), len(rv.Slots))
	}
	args := map[string]string{}
	for slot, val := range in {
		if !slotKnown(rv, slot) {
			return nil, fmt.Errorf("relation %s has no slot %q", rv.Name, slot)
		}
		if !allowVars && strings.HasPrefix(val, "?") {
			return nil, fmt.Errorf("relation %s slot %s: variable %q in a ground fact", rv.Name, slot, val)
		}
		args[slot] = val
	}
	return args, nil
}

func slotKnown(rv RelationVocab, slot string) bool {
	for _, s := range rv.Slots {
		if s == slot {
			return true
		}
	}
	return false
}

// trialRule checks safety and stratification with the candidate included,
// WITHOUT mutating visible state: an unsafe rule (conclusion var unbound)
// or a rule that would create a cyclic relation dependency is quarantined.
func (p *Pipeline) trialRule(r *world.Rule) string {
	condVars := map[string]bool{}
	for _, c := range r.Conditions {
		for _, tm := range c.Args {
			if tm.Var != "" {
				condVars[tm.Var] = true
			}
		}
	}
	for slot, tm := range r.Conclusion.Args {
		if tm.Var != "" && !condVars[tm.Var] {
			return fmt.Sprintf("unsafe: conclusion var ?%s (slot %s) not bound by conditions", tm.Var, slot)
		}
	}
	if r.Authority < 1 || r.Authority > 5 {
		return fmt.Sprintf("authority %d out of range 1..5", r.Authority)
	}
	// stratification trial: does the dependency graph stay acyclic with
	// this rule added? Reuses the store's inference on a scratch copy of
	// the concluded-by map.
	concludes := map[string][]*world.Rule{}
	for i := range p.Store.Rules {
		if p.Store.Rules[i].Lifecycle != Active {
			continue
		}
		rr := &p.Store.Rules[i].Rule
		concludes[rr.Conclusion.Relation] = append(concludes[rr.Conclusion.Relation], rr)
	}
	concludes[r.Conclusion.Relation] = append(concludes[r.Conclusion.Relation], r)
	if err := checkAcyclic(concludes); err != nil {
		return err.Error()
	}
	return ""
}

func checkAcyclic(concludes map[string][]*world.Rule) error {
	state := map[string]int{} // 0 unseen, 1 visiting, 2 done
	var visit func(rel string) error
	visit = func(rel string) error {
		switch state[rel] {
		case 1:
			return fmt.Errorf("cyclic rule dependency through relation %s", rel)
		case 2:
			return nil
		}
		state[rel] = 1
		for _, r := range concludes[rel] {
			for _, c := range r.Conditions {
				if err := visit(c.Relation); err != nil {
					return err
				}
			}
		}
		state[rel] = 2
		return nil
	}
	var rels []string
	for rel := range concludes {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	for _, rel := range rels {
		if err := visit(rel); err != nil {
			return err
		}
	}
	return nil
}

func conditionsConnected(r *world.Rule) bool {
	if len(r.Conditions) <= 1 {
		return true
	}
	// union-find over conditions sharing variables
	parent := make([]int, len(r.Conditions))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	varAt := map[string]int{}
	for i, c := range r.Conditions {
		for _, tm := range c.Args {
			if tm.Var == "" {
				continue
			}
			if j, ok := varAt[tm.Var]; ok {
				parent[find(i)] = find(j)
			} else {
				varAt[tm.Var] = i
			}
		}
	}
	root := find(0)
	for i := 1; i < len(r.Conditions); i++ {
		if find(i) != root {
			return false
		}
	}
	return true
}

// hygieneGate runs post-compile: join-explosion dry runs (quarantining the
// named rule and retrying, bounded) and firing-ratio measurement.
func (p *Pipeline) hygieneGate(rep *CompileReport) error {
	// Join-explosion loop: Eval at the horizon-ish day (max day seen in
	// commits); on JoinExplosionError quarantine the offending rule and
	// retry. Bounded by rule count.
	evalDay := p.Store.maxKnownDay()
	// Probe actual plus every ingested frame: a frame-homed rule can explode
	// a frame closure without ever firing in actual (same per-frame probe
	// the generator runs). Quarantining is global — an explosive rule is
	// explosive wherever it fires.
	probeFrames := []string{world.ActualFrame}
	for i := range p.Store.Frames {
		if p.Store.Frames[i].Lifecycle == Active {
			probeFrames = append(probeFrames, p.Store.Frames[i].Frame.ID)
		}
	}
	for _, fid := range probeFrames {
		for attempts := 0; attempts <= len(p.Store.Rules); attempts++ {
			w, err := p.Store.worldView()
			if err != nil {
				// schema error that slipped past trials (shouldn't happen —
				// trialRule guards commits) — surface loudly.
				return fmt.Errorf("hygiene: world view: %w", err)
			}
			_, err = oracle.Eval(w, evalDay, oracle.Options{Frame: fid})
			if err == nil {
				break
			}
			if je, ok := err.(*oracle.JoinExplosionError); ok {
				p.Store.setRuleLifecycle(je.RuleID, Quarantined)
				rep.Quarantined++
				rep.Hygiene = append(rep.Hygiene, fmt.Sprintf("rule %s quarantined: join explosion during %s (frame %s)", je.RuleID, je.Phase, fid))
				continue
			}
			return fmt.Errorf("hygiene: eval (frame %s): %w", fid, err)
		}
	}

	// Firing-ratio measurement per active rule (see FiringRatioThreshold
	// doc for why the default is far above the generator's 0.5).
	ratios := p.Store.firingRatios(evalDay)
	var ruleIDs []string
	for id := range ratios {
		ruleIDs = append(ruleIDs, id)
	}
	sort.Strings(ruleIDs)
	for _, id := range ruleIDs {
		r := ratios[id]
		if r > p.FiringRatioThreshold {
			msg := fmt.Sprintf("rule %s firing ratio %.2f > %.2f", id, r, p.FiringRatioThreshold)
			if p.QuarantineOverFiring {
				p.Store.setRuleLifecycle(id, Quarantined)
				rep.Quarantined++
				rep.Hygiene = append(rep.Hygiene, msg+" — quarantined")
			} else {
				rep.Hygiene = append(rep.Hygiene, msg+" — flagged")
			}
		}
	}
	return nil
}

func firstSpan(s string) string {
	if len(s) > 120 {
		return s[:120] + "…"
	}
	return s
}
