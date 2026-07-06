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
}

type SupCand struct {
	NoticeID string `json:"notice_id"`
	OldRule  string `json:"old_rule"`
	NewRule  string `json:"new_rule"`
	From     int    `json:"from"`
}

// Candidate is one extracted item plus its audit trail: confidence and the
// exact source span (the episode text line it came from).
type Candidate struct {
	Kind       CandidateKind `json:"kind"`
	Fact       *FactCand     `json:"fact,omitempty"`
	Rule       *RuleCand     `json:"rule,omitempty"`
	Sup        *SupCand      `json:"supersession,omitempty"`
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
	}
	switch cand.Kind {
	case CandFact:
		return p.commitFactCand(cand, prov, rep)
	case CandRule:
		return p.commitRuleCand(cand, prov, rep)
	case CandSupersession:
		return p.commitSupCand(cand, prov, rep)
	}
	return ItemTrace{Kind: cand.Kind, Verdict: VDropped, Detail: "unknown candidate kind"}
}

func (p *Pipeline) commitFactCand(cand Candidate, prov Provenance, rep *CompileReport) ItemTrace {
	fc := cand.Fact
	it := ItemTrace{Kind: CandFact, ID: fc.FactID}
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
	f := world.BaseFact{
		ID:        fc.FactID,
		Atom:      world.Atom{Relation: rv.ID, Args: args},
		From:      fc.From,
		To:        fc.To,
		Source:    fc.Source,
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
	sp := world.Supersession{
		ID:        sc.NoticeID,
		OldRule:   sc.OldRule,
		NewRule:   sc.NewRule,
		From:      sc.From,
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
	for attempts := 0; attempts <= len(p.Store.Rules); attempts++ {
		w, err := p.Store.worldView()
		if err != nil {
			// schema error that slipped past trials (shouldn't happen —
			// trialRule guards commits) — surface loudly.
			return fmt.Errorf("hygiene: world view: %w", err)
		}
		_, err = oracle.Eval(w, evalDay, oracle.Options{})
		if err == nil {
			break
		}
		if je, ok := err.(*oracle.JoinExplosionError); ok {
			p.Store.setRuleLifecycle(je.RuleID, Quarantined)
			rep.Quarantined++
			rep.Hygiene = append(rep.Hygiene, fmt.Sprintf("rule %s quarantined: join explosion during %s", je.RuleID, je.Phase))
			continue
		}
		return fmt.Errorf("hygiene: eval: %w", err)
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
