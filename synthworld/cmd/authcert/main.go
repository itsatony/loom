// authcert computes the tier-M AUTHENTICITY CERTIFICATE (MASTERPLAN
// §9.6.6, reading RATIFIED 2026-07-12, option (a) — see MASTERPLAN §10):
// the certificate is the COLLAPSE OF THE TIER-E MARKER REGEXES — the
// tier is certified authentic iff the marker rules fire on ZERO lines of
// the naturalized corpus (they fire on essentially every frame-bearing
// line of tier E; run tier E first to calibrate the scaffold — a low
// tier-E marker count means the scaffold is broken, not that tier E is
// hard). This mirrors the H4 paraphrase certificate exactly: an
// unsupervised surface-cue detector that was ≈perfect on the templated
// tier must be dead on the naturalized tier.
//
// The supervised leave-one-seed-out naive-Bayes baseline is additionally
// run and REPORTED UNGATED as a hardness descriptor: any text that a
// judge panel can decode at ≥95% necessarily carries some learnable
// signal, so a labeled-data classifier (strictly stronger than the H4
// analog) cannot be a certification gate — its number contextualizes how
// much structural-lexical signal remains for the measured conditions.
//
// The baseline is deliberately a steelman within surface cues: it sees
// ground-truth per-line frame labels of OTHER seeds for training
// (leave-one-seed-out), knows the atom rendering (world.json), reads
// event days, and gets the scenario-inheritance shortcut for free. What
// it must NOT be able to do on a certified tier is tell a line's frame
// from its lexical surface.
//
// Usage:
//
//	go run ./cmd/authcert -dirs ds1,ds2,... [-episodes episodes_natural.jsonl] [-max 0.65]
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/vaudience/synthworld/gen"
	"github.com/vaudience/synthworld/harness"
	"github.com/vaudience/synthworld/world"
)

// ---------- line labels & features ----------

// Classes: the four frame kinds plus sarcasm (a sarcastic line's literal
// content is asserted nowhere, so it must be its own class for the
// baseline to answer sarcasm traps).
const (
	clsActual      = "actual"
	clsFiction     = "fiction"
	clsPerspective = "perspective"
	clsScenario    = "scenario"
	clsSarcasm     = "sarcasm"
)

func eventClass(ev *gen.Event) string {
	switch ev.Kind {
	case gen.EvFrame:
		switch ev.Frame.Kind {
		case world.FrameFiction:
			return clsFiction
		case world.FrameScenario:
			return clsScenario
		default:
			return clsPerspective
		}
	case gen.EvPromotion:
		return clsActual
	case gen.EvFact:
		if ev.AssertionType == gen.AssertNonAssertive {
			return clsSarcasm
		}
		return kindOfFrame(ev.Fact.FrameID)
	case gen.EvRule:
		return kindOfFrame(ev.Rule.FrameID)
	default:
		return kindOfFrame(ev.Supersession.FrameID)
	}
}

// kindOfFrame maps a frame ID to its kind by prefix — valid for generated
// datasets (fic_/psp_/scn_) without needing the frame table.
func kindOfFrame(frameID string) string {
	fr := world.NormFrame(frameID)
	switch {
	case fr == world.ActualFrame:
		return clsActual
	case strings.HasPrefix(fr, "fic_"):
		return clsFiction
	case strings.HasPrefix(fr, "psp_"):
		return clsPerspective
	case strings.HasPrefix(fr, "scn_"):
		return clsScenario
	}
	return clsActual
}

var (
	reAtomExpr = regexp.MustCompile(`\b\w+\([^()]*\)`)
	reToken    = regexp.MustCompile(`[A-Za-z_]+|\d+`)
)

// features turns a line into normalized unigrams+bigrams. Identifiers
// collapse to their type prefix (fct_0042 → "fct", registry_A →
// "registry"), digits to "<num>" — so cues must be genuinely lexical to
// transfer across seeds.
func features(text string) []string {
	stripped := reAtomExpr.ReplaceAllString(text, " <atom> ")
	raw := reToken.FindAllString(strings.ToLower(stripped), -1)
	toks := make([]string, 0, len(raw))
	for _, t := range raw {
		switch {
		case regexp.MustCompile(`^\d+$`).MatchString(t):
			toks = append(toks, "<num>")
		case strings.Contains(t, "_"):
			toks = append(toks, strings.SplitN(t, "_", 2)[0])
		default:
			toks = append(toks, t)
		}
	}
	out := make([]string, 0, 2*len(toks))
	out = append(out, toks...)
	for i := 0; i+1 < len(toks); i++ {
		out = append(out, toks[i]+" "+toks[i+1])
	}
	return out
}

// ---------- tier-E marker rules ----------

// markerClass classifies a line from tier-E template markers alone;
// returns "" when no marker fires. On tier E this is ≈ perfect (the
// pre-registered calibration); on a certified tier M it must be dead.
func markerClass(text string) string {
	switch {
	case strings.Contains(text, "fiction frame") || strings.Contains(text, "in the story"):
		return clsFiction
	case strings.Contains(text, "non-assertive") || strings.Contains(text, "Sarcastic remark"):
		return clsSarcasm
	case strings.Contains(text, "perspective frame") || strings.Contains(text, "attributed to frame") ||
		strings.Contains(text, "source frame psp_"):
		return clsPerspective
	case strings.Contains(text, "Scenario scn_") || strings.Contains(text, "planning scenario scn_") ||
		strings.Contains(text, "within this scenario"):
		return clsScenario
	}
	return ""
}

// ---------- multinomial naive Bayes ----------

type nbModel struct {
	classes    []string
	logPrior   map[string]float64
	logLik     map[string]map[string]float64 // class → token → logP
	logUnknown map[string]float64            // class → logP for unseen token
}

func trainNB(rows []trainRow) *nbModel {
	counts := map[string]map[string]int{}
	classCounts := map[string]int{}
	tokenTotals := map[string]int{}
	vocab := map[string]bool{}
	for _, r := range rows {
		classCounts[r.class]++
		if counts[r.class] == nil {
			counts[r.class] = map[string]int{}
		}
		for _, f := range r.feats {
			counts[r.class][f]++
			tokenTotals[r.class]++
			vocab[f] = true
		}
	}
	m := &nbModel{logPrior: map[string]float64{}, logLik: map[string]map[string]float64{}, logUnknown: map[string]float64{}}
	total := 0
	for _, c := range classCounts {
		total += c
	}
	v := float64(len(vocab)) + 1
	for cls, n := range classCounts {
		m.classes = append(m.classes, cls)
		m.logPrior[cls] = math.Log(float64(n) / float64(total))
		m.logLik[cls] = map[string]float64{}
		denom := float64(tokenTotals[cls]) + v
		for tok, c := range counts[cls] {
			m.logLik[cls][tok] = math.Log((float64(c) + 1) / denom)
		}
		m.logUnknown[cls] = math.Log(1 / denom)
	}
	sort.Strings(m.classes)
	return m
}

func (m *nbModel) classify(feats []string) string {
	best, bestScore := clsActual, math.Inf(-1)
	for _, cls := range m.classes {
		s := m.logPrior[cls]
		for _, f := range feats {
			if lp, ok := m.logLik[cls][f]; ok {
				s += lp
			} else {
				s += m.logUnknown[cls]
			}
		}
		if s > bestScore {
			best, bestScore = cls, s
		}
	}
	return best
}

type trainRow struct {
	class string
	feats []string
}

// ---------- the surface-cue condition ----------

type factLine struct {
	atoms []string // normalized atom expressions appearing in the line
	day   int
	class string // marker rule if fired, else NB prediction
}

type surfaceCue struct {
	model *nbModel
	// foldModels, when set, override model per episode parity (single-seed
	// dev fallback: each line is classified by the model trained on the
	// OTHER parity's episodes; all lines answer queries).
	foldModels [2]*nbModel
	lines      []factLine
	// markerHits counts lines where a tier-E marker regex fired (tier-E
	// calibration signal; must be ≈0 on tier M).
	markerHits int
}

func (s *surfaceCue) Name() string { return "surface-cue" }

func (s *surfaceCue) Ingest(episodes []gen.Episode) error {
	for i := range episodes {
		m := s.model
		if s.foldModels[0] != nil {
			m = s.foldModels[i%2]
		}
		for j := range episodes[i].Events {
			ev := &episodes[i].Events[j]
			cls := markerClass(ev.Text)
			if cls != "" {
				s.markerHits++
			} else {
				cls = m.classify(features(ev.Text))
			}
			s.lines = append(s.lines, factLine{
				atoms: normalizeExprs(reAtomExpr.FindAllString(ev.Text, -1)),
				day:   ev.Day,
				class: cls,
			})
		}
	}
	return nil
}

func normalizeExprs(exprs []string) []string {
	out := make([]string, len(exprs))
	for i, e := range exprs {
		out[i] = strings.Join(strings.Fields(strings.ReplaceAll(e, ",", ", ")), " ")
	}
	return out
}

// renderAtom formats a query atom exactly as the generator renders it in
// episode text (relation display name, slot declaration order).
func renderAtom(w *world.World, a *world.Atom) string {
	rel := w.RelationByID(a.Relation)
	if rel == nil {
		return ""
	}
	parts := make([]string, 0, len(rel.Slots))
	for _, sl := range rel.Slots {
		parts = append(parts, fmt.Sprintf("%s=%s", sl.Name, a.Args[sl.Name]))
	}
	return fmt.Sprintf("%s(%s)", rel.Name, strings.Join(parts, ", "))
}

type condWithWorld struct {
	surfaceCue
	w *world.World
}

func (c *condWithWorld) AnswerHolds(q harness.SanitizedQuery) (bool, error) {
	if q.Atom == nil {
		return false, nil
	}
	want := strings.Join(strings.Fields(strings.ReplaceAll(renderAtom(c.w, q.Atom), ",", ", ")), " ")
	frameKind := kindOfFrame(q.Frame)
	for _, l := range c.lines {
		if l.day > q.AtDay {
			continue
		}
		hit := false
		for _, a := range l.atoms {
			if a == want {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		if l.class == frameKind {
			return true, nil
		}
		// Scenario inheritance shortcut: an actual-classified line
		// satisfies a scenario query.
		if frameKind == clsScenario && l.class == clsActual {
			return true, nil
		}
	}
	return false, nil
}

func (c *condWithWorld) AnswerFind(q harness.SanitizedQuery) ([]string, error) { return nil, nil }

// ---------- per-seed data ----------

type seedData struct {
	dir      string
	episodes []gen.Episode
	queries  []gen.Query
	w        world.World
	rows     []trainRow
}

func loadSeed(dir, episodesFile string) (*seedData, error) {
	sd := &seedData{dir: dir}
	b, err := os.ReadFile(filepath.Join(dir, "world.json"))
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &sd.w); err != nil {
		return nil, err
	}
	if err := readJSONL(filepath.Join(dir, episodesFile), func(raw []byte) error {
		var ep gen.Episode
		if err := json.Unmarshal(raw, &ep); err != nil {
			return err
		}
		sd.episodes = append(sd.episodes, ep)
		return nil
	}); err != nil {
		return nil, err
	}
	if err := readJSONL(filepath.Join(dir, "queries.jsonl"), func(raw []byte) error {
		var q gen.Query
		if err := json.Unmarshal(raw, &q); err != nil {
			return err
		}
		sd.queries = append(sd.queries, q)
		return nil
	}); err != nil {
		return nil, err
	}
	for i := range sd.episodes {
		for j := range sd.episodes[i].Events {
			ev := &sd.episodes[i].Events[j]
			sd.rows = append(sd.rows, trainRow{class: eventClass(ev), feats: features(ev.Text)})
		}
	}
	return sd, nil
}

// ---------- certificate arithmetic ----------

type slicePool struct {
	PosCorrect, PosTotal, NegCorrect, NegTotal int
}

func (p *slicePool) add(s harness.SliceScore) {
	p.PosCorrect += s.PosCorrect
	p.PosTotal += s.PosTotal
	p.NegCorrect += s.NegCorrect
	p.NegTotal += s.NegTotal
}

func (p slicePool) balanced() float64 {
	if p.PosTotal == 0 || p.NegTotal == 0 {
		return math.NaN()
	}
	return (float64(p.PosCorrect)/float64(p.PosTotal) + float64(p.NegCorrect)/float64(p.NegTotal)) / 2
}

type seedResult struct {
	Dir           string             `json:"dir"`
	MarkerHits    int                `json:"marker_hits"`
	Contamination harness.SliceScore `json:"contamination"`
	Isolation     harness.SliceScore `json:"isolation"`
	IsolChain     harness.SliceScore `json:"isolation_chain"`
	TrainedOn     string             `json:"trained_on"` // "loso" | "selffold"
}

type certReport struct {
	EpisodesFile    string       `json:"episodes_file"`
	Max             float64      `json:"max_balanced_accuracy"`
	Seeds           []seedResult `json:"seeds"`
	PooledContam    slicePool    `json:"pooled_contamination"`
	PooledIsolation slicePool    `json:"pooled_isolation"`
	PooledChain     slicePool    `json:"pooled_isolation_chain"`
	BalancedContam  float64      `json:"balanced_contamination"`
	BalancedIsol    float64      `json:"balanced_isolation"`
	BalancedPooled  float64      `json:"balanced_pooled_with_controls"`
	// BalancedTraps is the supervised-baseline HARDNESS DESCRIPTOR
	// (ungated per the ratified §9.6.6 reading (a), 2026-07-12): mean
	// accuracy over the two trap directions — contamination traps
	// (negatives; fiction/quote/sarcasm content must not read as actual)
	// and isolation traps (positives; inherited facts must hold in the
	// scenario). The with-controls pooled number is also reported; it is
	// unreachable as a ≤0.65 gate by construction (controls are
	// verbatim-stated facts, trivially grep-correct). Neither gates
	// certification; the certificate is marker collapse.
	BalancedTraps    float64 `json:"balanced_traps"`
	TotalMarkerHits  int     `json:"total_marker_hits"`
	Certified        bool    `json:"certified"`
	CertifiedComment string  `json:"comment"`
}

func main() {
	dirsFlag := flag.String("dirs", "", "comma-separated dataset directories (≥1)")
	episodesFile := flag.String("episodes", "episodes_natural.jsonl", "episodes file per dataset (episodes.jsonl = tier-E calibration)")
	maxBal := flag.Float64("max", 0.65, "advisory reference for the ungated hardness descriptor (was the §9.6.6 draft gate; superseded by the ratified marker-collapse reading)")
	reportPath := flag.String("report", "", "write JSON report here (default <first dir>/authcert-report.json)")
	flag.Parse()

	dirs := strings.Split(*dirsFlag, ",")
	if *dirsFlag == "" || len(dirs) == 0 {
		fail(fmt.Errorf("-dirs is required"))
	}

	var seeds []*seedData
	for _, d := range dirs {
		sd, err := loadSeed(strings.TrimSpace(d), *episodesFile)
		fail2(err)
		seeds = append(seeds, sd)
	}

	rep := certReport{EpisodesFile: *episodesFile, Max: *maxBal}
	for i, sd := range seeds {
		// Leave-one-seed-out training; single-seed dev runs fall back to
		// a 2-fold split by episode parity (documented in the report).
		var rows []trainRow
		trainedOn := "loso"
		if len(seeds) > 1 {
			for j, other := range seeds {
				if j != i {
					rows = append(rows, other.rows...)
				}
			}
			cond := &condWithWorld{w: &sd.w}
			cond.model = trainNB(rows)
			res := runSeed(cond, sd, sd.episodes)
			res.TrainedOn = trainedOn
			rep.Seeds = append(rep.Seeds, res)
		} else {
			// Single-seed dev fallback: 2-fold by episode parity — every
			// line is classified by the model trained on the OTHER
			// parity's episodes; all lines answer queries.
			var evenRows, oddRows []trainRow
			for k := range sd.episodes {
				for j := range sd.episodes[k].Events {
					ev := &sd.episodes[k].Events[j]
					row := trainRow{class: eventClass(ev), feats: features(ev.Text)}
					if k%2 == 0 {
						evenRows = append(evenRows, row)
					} else {
						oddRows = append(oddRows, row)
					}
				}
			}
			cond := &condWithWorld{w: &sd.w}
			cond.foldModels = [2]*nbModel{trainNB(oddRows), trainNB(evenRows)}
			res := runSeed(cond, sd, sd.episodes)
			res.TrainedOn = "selffold"
			rep.Seeds = append(rep.Seeds, res)
		}
	}

	for _, s := range rep.Seeds {
		rep.PooledContam.add(s.Contamination)
		rep.PooledIsolation.add(s.Isolation)
		rep.PooledChain.add(s.IsolChain)
		rep.TotalMarkerHits += s.MarkerHits
	}
	rep.BalancedContam = rep.PooledContam.balanced()
	rep.BalancedIsol = rep.PooledIsolation.balanced()
	// Chain queries are a sub-line of isolation (harness tallies them in
	// both cells) — pool contamination+isolation only, no double count.
	all := slicePool{}
	all.add(harness.SliceScore(rep.PooledContam))
	all.add(harness.SliceScore(rep.PooledIsolation))
	rep.BalancedPooled = all.balanced()
	if rep.PooledContam.NegTotal > 0 && rep.PooledIsolation.PosTotal > 0 {
		rep.BalancedTraps = (float64(rep.PooledContam.NegCorrect)/float64(rep.PooledContam.NegTotal) +
			float64(rep.PooledIsolation.PosCorrect)/float64(rep.PooledIsolation.PosTotal)) / 2
	} else {
		rep.BalancedTraps = math.NaN()
	}
	// Ratified §9.6.6 reading (a), 2026-07-12: the certificate is the
	// collapse of the tier-E marker regexes to zero hits; the supervised
	// LOSO baseline is a hardness descriptor, not a gate. BalancedTraps
	// must be computable (trap queries present and answered) for the run
	// to count as a certification run at all.
	rep.Certified = rep.TotalMarkerHits == 0 && !math.IsNaN(rep.BalancedTraps)
	if rep.Certified {
		rep.CertifiedComment = fmt.Sprintf("tier-E marker regexes collapsed to 0 hits — tier CERTIFIED (§9.6.6 reading (a), ratified 2026-07-12); ungated hardness descriptor: supervised LOSO trap-direction balanced accuracy %.3f (with-controls %.3f)", rep.BalancedTraps, rep.BalancedPooled)
	} else if rep.TotalMarkerHits > 0 {
		rep.CertifiedComment = fmt.Sprintf("%d tier-E marker hits — tier NOT certified (markers must collapse to 0; non-evidence, regenerate harder and log the failure)", rep.TotalMarkerHits)
	} else {
		rep.CertifiedComment = "trap-direction balanced accuracy not computable (no trap queries answered) — not a certification run"
	}

	out := *reportPath
	if out == "" {
		out = filepath.Join(strings.TrimSpace(dirs[0]), "authcert-report.json")
	}
	rb, err := json.MarshalIndent(rep, "", "  ")
	fail2(err)
	fail2(os.WriteFile(out, rb, 0o644))

	fmt.Printf("authenticity certificate (%s):\n", *episodesFile)
	for _, s := range rep.Seeds {
		fmt.Printf("  %-40s contam %d/%d+%d/%d  isol %d/%d+%d/%d  chain %d/%d+%d/%d  markers %d  (%s)\n",
			s.Dir,
			s.Contamination.PosCorrect, s.Contamination.PosTotal, s.Contamination.NegCorrect, s.Contamination.NegTotal,
			s.Isolation.PosCorrect, s.Isolation.PosTotal, s.Isolation.NegCorrect, s.Isolation.NegTotal,
			s.IsolChain.PosCorrect, s.IsolChain.PosTotal, s.IsolChain.NegCorrect, s.IsolChain.NegTotal,
			s.MarkerHits, s.TrainedOn)
	}
	fmt.Printf("  balanced (with controls): contamination %.3f, isolation %.3f, pooled %.3f\n",
		rep.BalancedContam, rep.BalancedIsol, rep.BalancedPooled)
	fmt.Printf("  TRAP-DIRECTION balanced (hardness descriptor, ungated): contamination-traps %.3f, isolation-traps %.3f → %.3f (advisory ref %.2f)\n",
		float64(rep.PooledContam.NegCorrect)/float64(rep.PooledContam.NegTotal),
		float64(rep.PooledIsolation.PosCorrect)/float64(rep.PooledIsolation.PosTotal),
		rep.BalancedTraps, *maxBal)
	fmt.Println("  " + rep.CertifiedComment)
	if !rep.Certified {
		os.Exit(1)
	}
}

func runSeed(cond *condWithWorld, sd *seedData, episodes []gen.Episode) seedResult {
	r, err := harness.RunWorkers(cond, episodes, sd.queries, 4)
	fail2(err)
	res := seedResult{Dir: sd.dir, MarkerHits: cond.markerHits}
	if r.Frames != nil {
		res.Contamination = r.Frames.Contamination
		res.Isolation = r.Frames.Isolation
		res.IsolChain = r.Frames.IsolationChain
	}
	return res
}

func readJSONL(path string, handle func([]byte) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		cp := make([]byte, len(line))
		copy(cp, line)
		if err := handle(cp); err != nil {
			return err
		}
	}
	return sc.Err()
}

func fail(err error) { fmt.Fprintln(os.Stderr, "error:", err); os.Exit(1) }
func fail2(err error) {
	if err != nil {
		fail(err)
	}
}
