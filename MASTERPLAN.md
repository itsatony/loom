# MASTERPLAN — the Loom experimental campaign

*Status: v1, 2026-07-06. Companion to `CLAUDE.md` (map), `loom_substrate_spec.md`
(spec), `synthworld/DESIGN.md` (instrument semantics). This document is the
campaign plan: hypotheses, pre-registered endpoints, the experiment DAG,
and a wargame of eventualities with pre-committed responses. It is written
so that a hostile reviewer reading it BEFORE the results exist would agree
the design is fair. Amendments after C2b results exist must be logged in
§10 with rationale — silent edits to endpoints are forbidden.*

---

## 0. Verified starting position (2026-07-06)

- Instrument (synthworld) green: build, vet, `cmd/validate`, diagnostic
  pattern, determinism. No Go tests; validator + diagnostics are the harness.
- Loom S1 done: `loom-C2a` strictly oracle-equal on 5/5 seeds. Compilation
  fidelity 1.0 by construction in easy mode.
- LLM-free retrieval ceiling measured (BM25, seed 1234): composition
  full-provenance-coverage 2/50 @k=4 → 8/50 @k=16; **revision 0/12 at all k**.
- Not built: C2b (text compilation), C0 (no-memory), C3 (LoRA), NL planner,
  tmr probe wiring, semantic-retrieval numbers. All LLM-dependent work is
  blocked on vAI infra access.

Everything below assumes this position; if any item regresses, fix before
proceeding (validator green is a precondition for every experiment).

## 1. Hypotheses, decomposed and falsifiable

The bet (CLAUDE.md §1) decomposes into ordered hypotheses. Each has a
prediction registered *now*, before the measurement exists.

| ID | Hypothesis | Prediction (pre-registered) | Falsified if |
|---|---|---|---|
| **H1** | Episodic retrieval cannot assemble composition provenance | Semantic/hybrid retrieval (tmr) improves composition coverage over BM25 but stays < 50% full-coverage @k=8; revision coverage stays near 0 | tmr reaches ≥80% composition full-coverage @k=8 AND >50% revision coverage — then RAG's failure is not mechanistic and C1 may be beatable only marginally |
| **H2** | Even with retrieved context, an LLM composes multi-episode chains unreliably | C1 composition positive accuracy < 60% at k=8; **perfect-retrieval C1 ceiling** (§3, D6) < 85% at depth ≥ 2 | C1 or its ceiling ≈ oracle on composition — the LLM composes fine when given the pieces; the substrate's value case collapses to retrieval, not compilation |
| **H3** | RAG systematically agrees with stale beliefs on revision flips | C1 flip accuracy < 50% with stale-agreement > 50% of errors; retained controls near-perfect | C1 handles flips well — supersession is reachable by semantics after all |
| **H4** | Text-mode compilation is feasible with small loss | C2b compilation fidelity ≥ 0.9 P and R on facts, ≥ 0.8 on rules/supersessions (templated text); C2b end-to-end within 10pp of C2a on every slice | Fidelity or end-to-end collapses — compilation itself is the bottleneck (report distinguishes this from "substrates don't help", §8-E4) |
| **H5** | **KILL CRITERION (CLAUDE.md §7, verbatim):** C2b beats strongest C1 on composition by ≥15pp at equal-or-better repetition | Holds with room to spare (predicted gap > 30pp) | Gap < 15pp or repetition loss → v0 bet falsified; write-up; geometry does not rescue |
| **H6** | Compiled knowledge transfers across model swap with small loss | C2b per-slice transfer retention ≥ 0.95; C1 retention noisy (~0.85–1.05); C3 retention markedly < 1 without retraining | C2b retention < 0.9 on any slice — the planner/extraction dependence is bigger than the portability contract claims |
| **H7** | The economics favor compile-once-query-many | Token cost: C2b amortizes below C1 within tens of queries; long-context C1c cost per query grows with corpus, C2b flat | C1c (long-context) matches C2b accuracy at comparable per-query cost — the product story weakens even if H5 holds |

H5 is the pre-registered kill criterion and is not softened by anything in
this document. H1–H4 are diagnostic (they explain WHY H5 comes out however
it does); H6–H7 only matter if H5 survives.

### 1.1 Operationalization of H5 (locked before any C2b run)

The kill criterion needs numbers, not vibes. Registered now:

- **Composition metric** = balanced accuracy on composition `holds`
  (mean of positive accuracy and negative accuracy), pooled over all gated
  seeds (§5). Balanced, so `always-true` gaming is structurally impossible.
  Find-F1 is a *secondary* endpoint (reported, not part of the kill test).
- **"Beats by ≥15pp"** = the 95% bootstrap CI (seed-level resampling,
  10k draws) of the paired per-seed difference (C2b − strongest C1) has its
  **lower bound ≥ +15pp**. Point estimates don't clear kill criteria; CIs do.
- **"Equal-or-better repetition"** = non-inferiority with margin 2pp:
  lower CI bound of (C2b − strongest C1) on repetition balanced accuracy
  ≥ −2pp. *Rationale, stated openly:* strict "point estimate ≥ 0" would let
  sampling noise on a slice where both conditions sit near ceiling
  (~95%+) veto a real composition win; 2pp is below any decision-relevant
  effect. This is an operationalization, decided before data, not a
  softening after data. If C2b actually loses >2pp on repetition it IS
  destroying information at compile time and fails.
- **"Strongest C1"** = max over {tmr-semantic, tmr-hybrid, DeepR/HyperRAG,
  BM25} × k ∈ {4, 8, 16}, plus long-context C1c (§3), each measured on the
  same seeds. C1 gets every advantage we can give it (§4.2).

## 2. Experiment DAG

Phases in dependency order. Each has entry criteria, procedure, exit
deliverable. E1–E2 need only infra access; E3 is the big build.

```
E0 instrument hardening ──► E1 retrieval ceilings ──► E2 C1 live baselines ──┐
                     │                                                        ├──► E4 head-to-head (H5) ──► E5 swap (H6) ──► E6 economics+scaling (H7)
                     └──► E3 Loom S2 build (C2b) ────────────────────────────┘
```

### E0 — Instrument hardening (no infra needed; do first)

The instrument must be upgraded before the expensive experiments, because
several controls have to exist before C2b numbers do (order matters for
credibility — controls built after results look like rationalization).

1. **Revision slice enlargement.** 6 flips + 6 retained per seed is too
   thin for per-condition inference. Raise generator config so gated seeds
   carry ≥ 20 flips + ≥ 20 retained (or accept and document pooling across
   seeds as the only revision analysis — decide by trying the generator;
   prefer enlargement).
2. **C0 no-memory condition.** LLM answers from the question alone.
   Trivial to build; it is the floor every "lift" is measured against.
3. **D6 perfect-retrieval diagnostic** (see §3). Harness-side, cheap,
   LLM-dependent but tiny token cost.
4. **LLM record/replay cache.** Every `LLMClient` call keyed by
   (model, prompt-hash) and journaled to disk. Reruns are free and
   *deterministic*; the reproduction package can ship cassettes. Without
   this, "same binary + same seed" determinism dies the day an LLM enters.
5. **tmr envelope check + probe wiring.** Verify `TmrRetriever`'s JSON
   parse against real tmr output; wire the provenance probe to run over the
   tmr retriever behind the env flag (backlog item, still open).
6. **Batch driver + seed protocol.** `cmd/batch` (or a script): generate
   seeds 1..40, apply manifest gates (reject: over-firing majority, no d2+
   closure mass, composition positives < 30, revision flips < target),
   keep the **first 20 passers in numeric order**. The seed list is thereby
   fixed by protocol, not by anyone's choice, and is committed before any
   LLM run. No seed added or dropped afterwards for any reason other than
   a validator failure (which is an instrument bug to fix, not a data
   exclusion).
7. **Scoring unit tests.** Keep the validator king, but the scoring code
   (flip/retained classification, stale-agreement, find micro-F1, depth
   buckets) gets table-driven Go tests — it is the one place a silent bug
   corrupts conclusions rather than crashing.

Exit: all diagnostics still reproduce; seed list committed; cache layer in
place; `gofmt`/`vet` clean.

### E1 — Retrieval ceilings, semantic edition (H1) [infra: embeddings only]

`memoexport` → tmr ingest → provenance probe over tmr-semantic and
tmr-hybrid at k ∈ {4, 8, 16}, all 20 seeds. LLM-free, cheap, and the
single most informative pre-LLM number: it bounds every possible C1 from
above before a single generation token is spent.

Exit: coverage table (slice × retriever × k), H1 verdict. If H1 is
*refuted* here (embeddings reach the provenance), flag immediately — the
expected C1 numbers change and E4's interpretation narrows (§8-E6).

### E2 — C1 live baselines (H2, H3) [infra: LLM endpoint]

RAG condition with model A over {BM25, tmr-semantic, tmr-hybrid} × k, plus
C0, plus D6 (perfect retrieval), plus C1c long-context (§3), 20 seeds,
record/replay on. DeepR/HyperRAG as confirmation pass (CLAUDE.md decision
log: kill criterion's C1 = strongest measured).

Deliverables: full C1 table; stale-agreement rates (H3); the
retrieval-vs-reasoning decomposition from D6 (H2). These numbers are
publishable on their own ("measured mechanism of RAG failure on revision")
regardless of Loom's fate — bank them.

### E3 — Loom S2: text-mode compilation (C2b) — the real uncertainty

Build per spec §5 behind the same Commit path: extraction (schema-prompted,
per-event candidates with confidence + source span), normalization (exact
alias first; embedding-assist behind a flag), consistency (duplicate /
refinement / conflict / supersession-candidate), rule handling (STATED
rules only; exception-proposal hook behind a flag, default off; **no
induction**), hygiene gate (connectivity, join precheck, post-commit firing
ratio → quarantine). Every stage emits its report; the concatenated
compilation trace is auditable per episode.

Development discipline: iterate on seeds {42, 7, 99} only; seeds from the
E0 batch list stay untouched until the pre-registered E4 run. (Dev seeds
overlapping the batch list is acceptable only if the batch protocol
happened to include them — note it in the report; the true safeguard is
that C2b is never *tuned against* held-out seeds.)

**Fidelity scoring** (world.json allowed for scoring only): P/R per item
type (facts, rules, supersessions), plus a confusion breakdown: missed
(never extracted), mangled (extracted, wrong normalization), dropped
(extracted, killed by consistency/hygiene), hallucinated (committed, not in
world). This decomposition is what makes a C2b failure *diagnosable* (§8-E4).

Exit: fidelity + end-to-end on dev seeds; C2b − C2a gap characterized;
quarantine/conflict rates from `StatsAt` (finally exercising the dormant
S2 hooks).

### E4 — Head-to-head: the H5 test

One pre-registered run: all conditions (C0, C1 family incl. C1c and D6
ceiling, C2a, C2b), 20 gated seeds, model A, record/replay on, one command,
one JSON report. Apply §1.1 arithmetic. **The H5 verdict is whatever it
is.** Diagnostics (§3) checked before believing anything.

### E5 — The swap (H6)

Model B ≠ model A's family (e.g. Qwen ↔ Mistral). The C2b swap is a 2×2
factorial — compile-with × query-with ∈ {A, B}² — because the portability
contract (spec §7) says loss is attributable to exactly two surfaces:

| compile \ plan | A | B |
|---|---|---|
| **A** | baseline | **planner loss isolated** |
| **B** | **extraction loss isolated** | full swap |

C2a's swap retention is 1.0 by construction (no LLM anywhere) — report it
as the structural ceiling, not as evidence. C1 re-runs per model; C3 (if
built by then, §6) shows retrain cost. Report per-slice transfer retention
(perf_B / perf_A) and substrate lift (C2 − C0, per model).

### E6 — Economics and scaling (H7)

- **Amortization curve:** total tokens(condition, n queries) vs n. C2b =
  compile-once + cheap structured queries; C1 = per-query retrieval +
  context; C1c = per-query full-corpus. Report the crossover point.
- **Corpus scaling:** generate datasets at 1×, 3×, 10× episode volume
  (same query protocol). Prediction: C1 composition degrades with corpus
  size (provenance dilution), C1c cost explodes, C2b flat in both accuracy
  and query cost. This is the product narrative *if* H5 held — measured,
  not asserted.

## 3. Controls and diagnostic conditions (the epistemics)

The existing LLM-free diagnostics (always-true/false, grep, stale-oracle,
oracle) stay mandatory after any harness change. The campaign adds:

- **C0 (no-memory):** LLM + question, no episodes. Floors near always-false
  on positives; any condition below C0 has negative memory value.
- **D6 (perfect-retrieval ceiling):** RAG condition fed *exactly the true
  provenance episodes* (harness-side cheat, clearly labeled diagnostic,
  never a competitor). Separates C1's retrieval failure from its reasoning
  failure — without it, "RAG loses composition" is ambiguous between "can't
  find the pieces" and "can't combine them", and the substrate story needs
  to know which (H2).
- **D7 (sanitization audit):** grep-level check that no condition input
  ever contains answers, slice labels, traces, or provenance — run as a
  test over the `sanitize()` output for every query in every dataset.
  Cheap paranoia; the whole campaign is invalid if this leaks.
- **C1c (long-context):** all episodes concatenated into the context, no
  retrieval. This is the honest 2026 competitor — if a long-context model
  simply reads the corpus and composes, retrieval-based C1 was a strawman.
  Include it in "strongest C1" (§1.1). Costs are recorded (H7 needs them).

## 4. Threats to validity — named, with mitigations

### 4.1 Construct: "we built the exam and the student"

C2 shares the oracle with the scorer. A hostile reviewer calls this rigged.
Defense, stated precisely: the oracle implements *standard stratified
Horn semantics with precedence* — the claim under test is **compilation**
(can episodes be turned into that structure), never inference. C2b's
uncertainty is entirely in the LLM extraction pipeline, which shares
nothing with the scorer. D6 additionally shows what C1's LLM does when
given perfect inputs, so the comparison isn't "logic engine vs no logic
engine" but "compile-then-evaluate vs retrieve-then-reason", each given
its best shot. Publish the framing *with* the limitation.

### 4.2 Fairness to C1 (the win must not be hollow)

- C1's prompt gets the same structured atom the deterministic planner
  gets (it's in SanitizedQuery) — parsing the question is not the test.
- C1 gets chain-of-thought room, k sweep, retriever sweep, hybrid RRF,
  the production system (DeepR), and the long-context variant. "Strongest
  C1" is the max over all of it.
- If C1 collapses so badly that 15pp is trivial (§8-E5), the report says
  so and leans on D6 + C1c to show the collapse is mechanistic, not
  configuration neglect.

### 4.3 Templated text makes extraction artificially easy

C2b on templates risks "an LLM cosplaying a parser" — near-perfect fidelity
that says nothing about real domains. Mitigation: a **paraphrase tier** —
episode text rewritten by an LLM (once, cached, committed with the dataset;
paraphrase model ≠ any evaluated model; validator extended to confirm no
entity/number is lost from the paraphrase, else regenerate). Registered
role: **robustness tier for H4, not part of the H5 kill test** (v0's claim
is on the instrument as designed; the paraphrase tier bounds how much of
the result is template-shaped). Report both.

### 4.4 Internal: leakage, drift, nondeterminism

Sanitization audit (D7); record/replay cache (E0.4); diagnostics after
every harness change; validator before every measurement; dev seeds ≠
held-out batch seeds (E3); seed protocol fixed before LLM runs (E0.6).

### 4.5 Statistical

- Primary endpoint: one (composition balanced accuracy, §1.1). Everything
  else is labeled secondary/diagnostic — no multiple-comparison laundering
  into the kill verdict.
- Pooling: per-seed paired differences, seed-level bootstrap. n=20 seeds ×
  ~40 composition positives + ~33 negatives ≈ 1400+ paired binary
  observations — ample power for a 15pp effect; the CI does the talking.
- Revision: with enlarged slices (E0.1), pooled flip/retained/
  stale-agreement rates with CIs; if enlargement fails, revision analysis
  is pooled-only and stated as such.

## 5. The number that goes in the writeup

One table: condition × slice, mean ± 95% CI over the 20 gated seeds, plus
per-depth composition breakdown, stale-agreement rates, find-F1, transfer
retention (post-E5), and token cost per compile/query (post-E6). Negative
cells get the same typography as positive ones.

## 6. C3 (LoRA) — scheduled honestly

C3 exists to complete the portability triangle (it should accumulate and
compose *somewhat*, and fail transfer without retraining). It is not on
the H5 critical path. Build after E4 if H5 survives (its budget is only
justified by a live thesis); if H5 kills the bet, C3 is dropped and the
write-up says why. Design when built: adapters trained on episode text,
same query protocol, retrain-vs-transfer cost on swap.

## 7. Timeline (dependency-honest, not calendar-optimistic)

| Phase | Depends on | Effort guess |
|---|---|---|
| E0 hardening | nothing | 2–4 days |
| E1 semantic ceilings | E0 + embed key | ½ day compute |
| E2 C1 baselines | E1 + LLM endpoint | 1–2 days incl. prompt sanity |
| E3 C2b build | E0 | **1.5–3 weeks — the real work** |
| E4 head-to-head | E2 + E3 | 1 day |
| E5 swap | E4 + model B | 1–2 days |
| E6 economics | E4 | 2–3 days |

E1/E2 and E3 parallelize if two people (or two agent sessions) are on it.

## 8. WARGAME — eventualities and pre-committed responses

Each scenario: signal → interpretation → response, decided now.

- **E1. H5 clears cleanly (predicted).** Response: do NOT celebrate into a
  claim inflation. Run the hostile-review checklist (§4 items, D6/D7/C1c all
  present?), then E5/E6. The publishable claim remains exactly: *held-out
  compositional and revision performance survives a model swap*. Then, and
  only then, geometry becomes a Stage-2 question.
- **E2. H5 fails: gap < 15pp.** Bet falsified in v0 form. Response: full
  honest write-up with the failure decomposition (which of H1–H4 broke),
  no post-hoc endpoint changes, no geometry rescue. Residual value shipped
  anyway: the instrument, the retrieval-ceiling result, the stale-agreement
  mechanism — all standalone contributions. Business pivot question (does
  auditability/provenance alone justify Loom as a product feature?) goes to
  Toni as a *separate* decision, explicitly disconnected from the falsified
  research claim.
- **E3. H5 clears but repetition non-inferiority fails.** C2b is destroying
  stated facts at compile time. That's a compilation-fidelity bug class
  (look at the missed/dropped confusion cells) — fix extraction and re-run
  E4 *once*, logging the amendment in §10. If it fails twice, it counts as
  a kill: a substrate that can't preserve verbatim facts is not shippable.
- **E4. C2b fidelity is poor (H4 fails) and drags end-to-end down.** The
  decomposition (missed/mangled/dropped/hallucinated) says where. Response
  ladder, cheapest first: prompt/few-shot iteration on dev seeds → stronger
  extraction model (record cost) → paraphrase-tier check (is templated text
  somehow *hurting*?). If fidelity stays < 0.8 on facts with a frontier
  extractor, report "compilation is the bottleneck" as the finding — that
  is a different (and still useful) result than "compiled substrates don't
  help", and the write-up keeps them apart.
- **E5. C1 collapses; 15pp is trivially cleared.** Hollow-win risk.
  Response: the verdict stands (criterion is the criterion) but the report
  must show D6 (perfect retrieval) and C1c (long context) numbers
  prominently — if those also fail composition, the collapse is mechanistic
  and the win is real; if D6 ≈ oracle, the whole C1 deficit was retrieval,
  and the report says the honest sentence: *better retrieval, not
  compilation, might close the gap; the substrate's edge is then revision +
  auditability + economics.*
- **E6. Embeddings close the retrieval gap (H1 refuted at E1).** The
  expected C1 numbers rise. No endpoint changes — H5 was always against
  the strongest C1. But raise the internal prior that E4 will be close, and
  make sure the revision slice (where the lexical-unreachability mechanism
  is strongest) is well-powered (E0.1) before E4.
- **E7. C1c (long-context) matches C2b on accuracy.** H5 may still clear
  against retrieval C1 but the strongest-C1 definition includes C1c — so
  this scenario *is* an H5 failure unless C2b beats C1c too. If C1c wins on
  accuracy but at 100× per-query cost, the write-up reports both numbers;
  the kill criterion is scored on accuracy alone (as registered), and the
  economics argument is presented as exactly that — economics, not a
  disguised accuracy claim.
- **E8. Swap retention is bad for C2b (H6 fails).** The 2×2 factorial says
  whether it's planner or extraction. Planner loss → the NL-planner adapter
  is the weak surface; report it as the measured price of portability and
  iterate on the planner (it is harness-side, not substrate-side, by
  design). Extraction loss on *already-compiled* stores is structurally
  impossible (the store is model-free) — if observed anyway, it's a bug in
  the experiment, not a finding; audit before reporting.
- **E9. JoinExplosion / quarantine storms in C2b-compiled rules.** The
  hygiene gate is working as designed. Report quarantine rates as a
  compilation-quality metric; a substrate that quarantines 40% of its rules
  has low fidelity *by another name* and E4 will show it. Never widen the
  binding guard to make numbers better.
- **E10. Infra flakiness / model deprecations mid-campaign.** Record/replay
  cache (E0.4) makes completed runs immortal; incomplete runs restart from
  cassettes. Model versions are pinned in the report; a mid-campaign
  deprecation forces a documented model substitution, never a silent one.
- **E11. Surprising numbers, any direction.** Standing order (CLAUDE.md
  §11): suspect the harness first (diagnostics), the generator second
  (validator + manifest gates), the thesis last. But if the thesis loses
  fairly, it loses. That's the point.

## 9. Innovation shelf (explicitly out of critical path)

Ideas worth having on record, none of which may touch the H5 protocol:

- **Exception-proposal hook** (spec §5.4) measured on the revision slice —
  the minimal "experience refines rules" experiment. Flag-gated ablation.
- **Hygiene-gate ablation:** C2b with the gate off — quantifies how much
  the synthworld hygiene lessons transfer to compiled knowledge.
- **Extractor-size sweep:** compilation fidelity vs extractor model size —
  is compilation frontier-dependent or commodity? (Business-critical: if a
  7B model compiles well, the cost story is dramatic.)
- **Noise tier:** distractor density sweep; measures compilation precision
  under adversarial chatter.
- **Multi-checkpoint evaluation** (`at_day` is already in the schema):
  belief-state trajectories over time, not just t_eval — a richer revision
  story once the single-checkpoint result is settled.
- **Geometry (Stage-2):** stays dead unless H5 survives, per the standing
  kill-criterion discipline. Even then it must beat the *symbolic* v0 on a
  capability the symbolic store measurably lacks (graded truth?
  analogical retrieval?) — a new pre-registration, not a victory lap.

## 9.5 Frames — registered draft (Stage 1.5; design sketch, not yet committed work)

*Status: DRAFT concept, registered 2026-07-07 after E3/H4 results. Known
to have unresolved breaking points (listed below). Becomes buildable work
only after the E4/E5 verdicts close; nothing here touches H5.*

**Problem.** v0 has one world: everything compiled is believed. Fiction,
worked examples, scenario planning, wargames, unvetted web claims, and
per-source perspectives are all non-factual-yet-must-be-remembered — and
they are endless in real domains. Deleting them loses knowledge;
believing them corrupts the closure.

**Design sketch.** Add a `frame` dimension to every stored item; truth
becomes frame-relative:
- Frames are named worlds (`actual`, `fiction:<id>`, `scenario:<id>`,
  `perspective:<agent>`, `source:<origin>`, `example:<id>`) forming a
  DAG with visibility inheritance. Scenario frames inherit `actual` and
  overlay deltas — override mechanics are literally supersession, which
  the store already has. Fiction inherits nothing (or schema only).
  Perspective frames model conflicting sources without forcing verdicts.
- Evaluation: `holds(atom, t, frame)` — the same stratified evaluator
  with one more visibility filter; `t` = when true, `frame` = where
  true. Derivation traces unchanged.
- The three separated decisions: STORAGE (always everything, with
  provenance), ASSERTION (frame-relative), PROMOTION (claims enter
  `actual` only via explicit, auditable policy — corroboration,
  authority, sign-off — never as a silent side effect of ingestion).
- Compilation grows one output: frame assignment per candidate, with
  confidence; ambiguity lands provisional (the quarantine move), so the
  failure mode is stored-but-not-yet-believed, never silently-believed.

**Frames are for drawing FROM, not only for sealing OFF.** Speculation,
fiction, prediction, and foreign perspectives are — often — the raw
material of ideation and innovation. Star Trek is fictional; the ethical
concepts it presents still matter, and its fictional technology is
legitimate inspiration for real products. A memory that quarantines
fiction into unreachable vaults would be safe and sterile. The design
therefore treats cross-frame reasoning as a first-class *deliberate*
operation: queries may open multiple frames at once (with the frame of
every supporting item visible in the derivation trace), analogy/
inspiration retrieval may roam all frames by default, and "derive an
opinion / make a prediction / draft a plan" is precisely a controlled
synthesis across frames whose provenance stays legible. The invariant
is not isolation — it is that *crossing a frame boundary is always
explicit in the trace*, so creativity draws on everything while the
closure of `actual` stays clean.

**Known breaking points (registered honestly, unresolved):**
1. Frame detection at extraction time is an LLM judgment and will err;
   metadata helps, but mislabeled irony/satire/embedded quotations are
   hard. Measured mitigation: frame-assignment fidelity as a scored
   compilation stage.
2. Frame proliferation: thousands of micro-frames (every chat, every
   draft) could fragment memory; needs merge/GC policy and frame
   aliasing.
3. Inheritance semantics under revision: a scenario built on `actual`
   at day d — does it track later `actual` revisions or freeze? Both
   are sometimes right (live wargame vs archived decision record);
   likely needs per-frame pinning semantics (inherit-live vs
   inherit-at-t), which complicates the evaluator's cache.
4. Cross-frame rule firing: may a rule compiled in `actual` fire on
   scenario facts? (Probably yes — that is what makes scenarios
   useful — but blocking the reverse without crippling ideation
   queries needs care.)
5. Promotion policy is a governance surface — the substrate can
   enforce and audit it but not decide it.

**Measurement (pre-sketch for a future frame slice):** ground-truth
world + fictional overlay + scenario deltas; frame-qualified queries
with paired traps in BOTH directions — fiction leaking into `actual`
(contamination) and `actual` failing to show through inheritance
(isolation failure) — same pairing discipline as the revision slice;
plus ideation-style queries that legitimately require multi-frame
support, scored on whether traces attribute frames correctly.

Lineage: McCarthy contexts / RDF named graphs / possible-world
semantics; the Loom-specific parts are frame detection inside the
compilation loop and the two-directional leakage benchmark.

## 9.6 Frames v1 — committed spec + pre-registration (2026-07-09)

*Resolves §9.5 (which stays above as the historical draft). Design input:
`FRAMES-DESIGN-NOTES.md` (three-lens expert analysis, repo root). This
section is the binding pre-registration for the frames-v1 campaign; it is
locked BEFORE the first naturalizer token, the first frame-bearing seed
lock, and any C2b-frames extraction run. Amendments go to §10.*

### 9.6.1 Mechanism (minimal v1)

- Frame types: `actual` + `perspective:<src>` (flat, non-nesting,
  non-inheriting; source: and perspective: collapsed into one type) +
  `scenario:<id>` (inherits actual; per-frame `basis: live | pinned(d)`
  set at creation, immutable; re-pin = new frame). Fiction and first-class
  examples DEFERRED (example = pinned no-promotion scenario when needed) —
  but the *instrument* still generates fiction-frame content (§9.6.4) so
  contamination is measured; the substrate stores it as a non-inheriting
  frame via the same mechanism.
- Speculation/prediction: NO new mechanism — confidence + lifecycle
  (`proposed`) + validity intervals, per the FRAMES-DESIGN-NOTES §A.1
  decision table. Sarcasm/humor: extraction-layer problem (assertion
  type), never a storage category.
- Promotion is the only door into `actual`: explicit Commit-path op with
  append-only PromotionRecord (policy ID, evidence refs, approver
  authority, optional sign-off). v1 promotes only ATTESTED items, never
  frame-derived consequences.
- Safety invariant: frame-assignment uncertainty routes to
  `perspective:<origin>`/quarantine, never `actual` — misclassification
  fails as stored-but-not-believed, never silently-believed.
- Rule firing is visibility-monotone: R fires in frame F iff
  home(R) ∈ cone(F); consequences land only in F's closure. Ideation
  widens QUERY scope (per-frame closures, unioned with attribution),
  never merges closures. Cross-frame import = scratch scenario +
  explicit ImportRecord.
- Frame-attributed derivation traces everywhere; perspective-frame
  answers surface attested (depth 0) vs derived (depth ≥1).

### 9.6.2 The five §9.5 open decisions — RESOLVED (registered here; also
in the spec decisions log)

1. **Frame-distance precedence: YES** — one new leading precedence key,
   frame proximity (nearer frame in the cone wins), ABOVE
   authority→recency→specificity→ID. A scenario delta overrides an
   inherited actual fact regardless of authority.
2. **Frame-local fact removal: frame-scoped supersession/block** —
   scenario deltas reuse existing supersession + block polarity, scoped
   to the frame; no new negation semantics.
3. **Forecast admission default: `proposed` until observed** — admission
   is a promotion-policy primitive, not a hardcode; conservative default.
4. **F-E2 superiority bar: 15pp**, mirroring §7/§1.1 arithmetic
   (seed-level bootstrap, CI lower bound).
5. **Track 3 (geometry probe) runs AFTER the F-E1/F-E2 verdicts**, gated
   small, per the design-notes sequencing; labeled contrastive pairs may
   be prepared from tier-M artifacts, but no probe measurement gates or
   precedes the frames verdicts.

### 9.6.3 Honest null

**C2b-prov**: frameless store, everything in one world with
episode/source metadata, query-time metadata filtering. Registered
prediction of where it fails: content-cued frames (sarcasm, unmarked
narrative, mid-episode switches) and scenario composition (query-time
filtering cannot apply delta-override overlays along a derivation
chain). Frame-blind C2b is also measured to CONFIRM ≈100% fiction-trap
contamination, not assume it.

### 9.6.4 Instrument (synthworld-frames)

Frame table in world.json (`{ID, Kind, Parents[], PinDay?, CreatedDay}`,
DAG); FrameID on every BaseFact/Rule/Supersession (default actual);
event payloads gain ground-truth FrameID + AssertionType ∈ {assert,
quote, non-assertive}. One pinned + one live scenario per seed. Fiction/
perspective frames REUSE actual's types, relation vocabulary, and
(partially) entities; fiction facts in two tracked sub-populations —
contradiction facts and **gap facts** (the sharp traps). Episode types
1–7 and query slices (contamination, isolation, pinning, misattribution,
promotion, cross-frame ideation — each with paired controls both
directions) per FRAMES-DESIGN-NOTES §B.1–B.3. Oracle stays exact:
`Closure(W,t,frame)` = visibility/pin set-filter before seeding + the
frame-proximity precedence tier; termination/stratification/join guards
untouched; traces carry the frame of every support.

### 9.6.5 Diagnostic pattern table (LLM-free; pre-registered — if this
pattern breaks after any harness change, fix the harness before
measuring anything)

| condition | contamination | isolation | pinning | misattribution | promotion | ideation | v0 slices |
|---|---|---|---|---|---|---|---|
| frame-oracle | 100% | 100% | 100% | 100% | 100% | 100% | 100% |
| mono-world oracle (all-in-actual) | **fails traps** | passes | passes | **fails** | fails premature-belief side | passes w/ wrong attribution | 100% |
| isolationist oracle (no inheritance) | passes | **fails inherited** | **fails** | passes | passes | fails cross-frame | 100% |
| literalist oracle (quoted/sarcastic literal asserted) | **fails speech-act sub-slice only** | passes | passes | passes | passes | passes | 100% |
| always-true / always-false / episode-grep | registered fingerprints per slice, extended from the v0 table | | | | | | v0 pattern |

Exact per-cell counts get frozen into the committed table on the first
gated seed batch, before any LLM-condition run (same discipline as the
v0 diagnostics table).

**FROZEN 2026-07-11** — exact per-cell counts summed over the 20 locked
frames seeds {1,2,3,7,8,9,10,12–16,18–22,24,25,29} (per-seed JSON
reports committed under results/frames-diagnostics/; no LLM token has
touched any frames dataset). Cells are `pos-correct/pos-total +
neg-correct/neg-total`; find/misattribution/ideation are exact-set
`matches/total`. frame-oracle is perfect in every cell (ceiling);
loom-C2a equals frame-oracle EXACTLY on every cell of every individual
seed (S1 frame-ingest exit now verified on the full locked batch, not
just dev seeds). If any cell below changes after a harness change, fix
the harness before measuring anything.

| condition | rep | comp | find | rev flip | rev ret | contam | c:contra | c:gap | c:quote | c:sarcasm | isol | i:chain | pin | promo | misattr | ideation |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| frame-oracle | 1200/1200+1191/1191 | 1556/1556+1467/1467 | 400/400 | 480/480 | 480/480 | 1033/1033+1033/1033 | 400/400+400/400 | 400/400+400/400 | 120/120+120/120 | 113/113+113/113 | 921/921+529/529 | 380/380+369/369 | 320/320+319/319 | 308/308+200/200 | 400/400 | 400/400 |
| loom-C2a | == frame-oracle in every cell, every seed | | | | | | | | | | | | | | | |
| mono-world | 1200/1200+1178/1191 | 1518/1556+1311/1467 | 240/400 | 465/480 | 470/480 | 1033/1033+113/1033 | 400/400+0/400 | 400/400+0/400 | 120/120+0/120 | 113/113+113/113 | 819/921+99/529 | 281/380+99/369 | 161/320+159/319 | 308/308+0/200 | 0/400 | 0/400 |
| isolationist | 1200/1200+1191/1191 | 1556/1556+1467/1467 | 400/400 | 480/480 | 480/480 | 1033/1033+1033/1033 | 400/400+400/400 | 400/400+400/400 | 120/120+120/120 | 113/113+113/113 | 301/921+529/529 | 0/380+369/369 | 0/320+319/319 | 308/308+200/200 | 320/400 | 346/400 |
| literalist | 1200/1200+1191/1191 | 1556/1556+1462/1467 | 387/400 | 479/480 | 480/480 | 1033/1033+800/1033 | 400/400+400/400 | 400/400+400/400 | 120/120+0/120 | 113/113+0/113 | 921/921+525/529 | 380/380+365/369 | 320/320+319/319 | 308/308+200/200 | 400/400 | 398/400 |
| v0 oracle (frame-blind) | 1200/1200+1191/1191 | 1556/1556+1467/1467 | 400/400 | 480/480 | 480/480 | 1033/1033+1033/1033 | 400/400+400/400 | 400/400+400/400 | 120/120+120/120 | 113/113+113/113 | 378/921+369/529 | 0/380+369/369 | 171/320+149/319 | 228/308+200/200 | 0/400 | 200/400 |
| loom-c2b-det | == v0 oracle in every cell | | | | | | | | | | | | | | | |
| always-true | 1200/1200+0/1191 | 1556/1556+0/1467 | 0/400 | 0/480 | 480/480 | 1033/1033+0/1033 | 400/400+0/400 | 400/400+0/400 | 120/120+0/120 | 113/113+0/113 | 921/921+0/529 | 380/380+0/369 | 320/320+0/319 | 308/308+0/200 | 0/400 | 120/400 |
| always-false | 0/1200+1191/1191 | 0/1556+1467/1467 | 0/400 | 480/480 | 0/480 | 0/1033+1033/1033 | 0/400+400/400 | 0/400+400/400 | 0/120+120/120 | 0/113+113/113 | 0/921+529/529 | 0/380+369/369 | 0/320+319/319 | 0/308+200/200 | 0/400 | 120/400 |
| episode-grep | 1200/1200+1178/1191 | 0/1556+1467/1467 | 0/400 | 480/480 | 0/480 | 1033/1033+0/1033 | 400/400+0/400 | 400/400+0/400 | 120/120+0/120 | 113/113+0/113 | 538/921+369/529 | 0/380+369/369 | 45/320+275/319 | 308/308+0/200 | 0/400 | 0/400 |
| stale-oracle | 1200/1200+1191/1191 | 1556/1556+1467/1467 | 288/400 | 0/480 | 480/480 | 1033/1033+1033/1033 | 400/400+400/400 | 400/400+400/400 | 120/120+120/120 | 113/113+113/113 | 426/921+323/529 | 48/380+323/369 | 255/320+65/319 | 228/308+200/200 | 0/400 | 200/400 |

Every qualitative prediction above holds in the frozen counts:
mono-world fails exactly the contamination traps (contra/gap/quote
negatives 0), misattribution (0/400), the premature-belief promotion
side (0/200 neg), and passes the sarcasm sub-line (assertion-type vs
frame-DAG separation) while carrying the registered comp+ rehoming
damage (1518/1556). Isolationist fails inherited isolation (301/921),
chains (0/380), pinning positives (0/320) and loses cross-frame
ideation (346/400). Literalist fails exactly the speech-act negatives
(quote 0/120, sarcasm 0/113) plus its registered literal-contamination
fingerprint (find 387/400, ideation 398/400).

### 9.6.6 Text hardness tiers + authenticity certificate

- **Tier E** (templated, explicit markers): harness-debugging only;
  pre-registered that a marker-regex detector scores ≈1.0 here; NEVER
  headlined as evidence.
- **Tier M** (LLM-naturalized, PRIMARY; naturalizer(s) outside the
  evaluated matrix, ≥2 families): validators = H4 content-preservation
  extended + frame-recoverability audit (3-judge panel outside matrix,
  ≥2/3 agreement, retry-with-feedback) + **authenticity certificate:
  a surface-cue baseline (lexical marker/regex + shallow-classifier)
  must score ≤0.65 balanced accuracy on the pooled
  contamination+isolation trap set** — else the tier is regenerated
  harder and the failure logged; a tier failing certification is
  non-evidence.
- **Tier H** (adversarial, first 5 locked seeds): NOT judge-filtered;
  ~100-event human spot-audit; human frame-recovery accuracy is the
  honest ceiling; reported machine vs human, outside kill arithmetic.
- Registered caveat (verbatim in any writeup): LLM-naturalized text
  tests frame detection on LLM-generated pragmatics; naturalizer∉matrix,
  family diversity, and the real-data tier mitigate but do not
  eliminate this.

### 9.6.7 Pre-registered endpoints

- **F-E1 (co-primary, two-directional safety):** tier-M, frame-aware
  C2b balanced-accuracy CI lower bound ≥85% on BOTH contamination AND
  isolation slices (each pooled with paired controls; gap-traps a
  mandatory reported sub-line). **Kill: either direction's lower bound
  <70%.** Both gate jointly — no buying safety with sterility.
- **F-E2 (co-primary, superiority over the null):** C2b(frames) beats
  C2b-prov on the content-cued trap subset by ≥15pp balanced accuracy
  (CI lower ≥ +15pp), at non-inferiority (2pp margin) on metadata-cued
  traps AND on every v0 slice (frames must not damage frame-free
  competence). **Kill: gap <15pp → query-time provenance filtering
  suffices; the compile-time-frames bet is falsified. No geometry
  rescue.**
- **F-E3 (secondary, diagnostic):** frame-assignment macro-F1 ≥0.90 on
  tier M; fiction→actual leakage <2% of fiction items; actual→non-actual
  exile rate and abstention/quarantine rate reported alongside.
- **F-E4 (secondary):** ideation cross-frame find: exact-set with
  per-satisfier frame attribution ≥90% and correct trace boundary
  marking.
- CI arithmetic identical to §1.1 (seed-level bootstrap, 10k draws,
  paired per-seed differences). Tier H + hybrid/real tiers are reported
  against human/directional benchmarks, OUTSIDE the kill arithmetic.

### 9.6.8 Seed protocol (frames edition of E0.6)

Generate frame-preset candidates seed 1..40; apply manifest gates =
all v0 gates PLUS: ≥15 gap-trap queries/seed, ≥10 scenario-composition
chains mixing inherited facts + deltas/seed, ≥1 pinned + ≥1 live
scenario/seed, per-frame firing-ratio hygiene (no frame where a
majority of rules over-fire). **First 20 passers in numeric order are
the locked set**; committed before any LLM run; tier H + human audit on
the first 5 locked seeds. Dev iteration only on seeds {42, 7, 99}
frame-preset equivalents, never on the locked list.

### 9.6.9 Track 2 (external validity — parallel, never in kill arithmetic)

Hybrid tier: real carrier text + naturalizer-woven synthetic injection
of frame-bearing facts about synthetic entities (labels by
construction); injection-detectability audited. Sarcasm datasets
(iSarcasm/SemEval-2018 T3) as assertion-type-classifier external check
only. Financial-analyst prediction/resolution slice = real-domain pilot
(objective mechanical resolution; VR-Banken-relevant). Registered role:
generalization evidence and caveat-gating only; directional
non-reproduction on the hybrid tier is a reportable negative about
distribution-dependence, not a kill input.

### 9.6.10 Track 3 (geometry probe — gated, after F-E1/F-E2 verdicts)

Scope: {quoted vs asserted}, {sincere vs lie} ONLY. Linear probes
(logistic + mass-mean), residual stream at claim-final token, layer
sweep with validation-only selection, ~1–2k minimal contrastive pairs
per frame, in-domain + held-out-domain splits, negation stress subset.
Baselines that must ALL be beaten: same-model prompted few-shot
classifier (the real opponent), lexical/frozen-embedding logistic
regression, majority floor. **Kill: probe must beat the same model's
prompted classifier by ≥10pp balanced accuracy (or ≥0.05 AUROC) on BOTH
frames on the held-out-domain style-controlled test at equal-or-lower
FPR — else activation-informed frame detection is falsified for v0 and
geometry stays shelved.** Portability: probes are per-model SENSORS,
refit per model, never weight-transferred; only their symbolic output
(frame label + confidence) enters the store; closure never touches
activations. Only-in-domain / only-MLP / only-uncontrolled wins count
as failure.

### 9.6.11 Budget envelope (registered)

Naturalization ~8–12M tokens ≈ $150–400; judge panel ≈ $100–300;
extraction ~15–20M tokens self-hosted (cassettes from first call);
human audit a few person-hours. Total new commercial spend ≈ $300–800.
Naturalized text committed to the repo as the dataset artifact.

## 10. Amendments log

*(Append-only. Every change to endpoints, seed protocol, or kill-criterion
arithmetic after 2026-07-06 gets a dated entry with rationale here.)*

- 2026-07-06 — v1 registered. Endpoints §1.1, seed protocol E0.6,
  wargame responses §8 locked before any LLM-dependent measurement exists.
- 2026-07-07 — E5 scope set BEFORE any swap measurement: C2b swap legs
  (extraction with models B/C) run on all 20 locked seeds (cheap);
  C1-family swap legs (rag/c1c/d6/c0 with models B/C) run on the first
  5 locked seeds {1,2,3,6,7} only — c1c's full-corpus prompts on
  commercial APIs are the cost driver. Transfer-retention ratios for C1
  are therefore reported with 5-seed CIs; H6's primary claim concerns
  C2b, which gets the full 20. Model modes registered: gpt-5-mini
  reasoning_effort=medium for C1 answering (its thinking analog;
  minimal for C2b extraction), claude-haiku-4-5 temp 0 + its default
  reasoning for C1. No change to H5/§1.1.
- 2026-07-07 — DATA-INTEGRITY amendment (logged BECAUSE it was made after
  a verdict computed). The first full E4 aggregate returned FAIL on
  repetition non-inferiority. Root cause was NOT the substrate: the shared
  qwen vLLM box was intermittently overloaded during the sweep, so many
  LLM-condition queries errored; errored queries are excluded from tallies,
  so affected conditions/seeds were scored on partial, biased subsets (two
  C2b seeds fully errored → chance-level, dragging repetition negative).
  Two fixes: (1) OpenAICompatClient now retries transient failures
  (6 attempts, exp backoff) so blips don't drop queries; (2) cmd/aggregate
  now treats any seed where a scored condition had errors>0 as UNDEFINED
  (NaN, excluded) and prints a DATA QUALITY warning — a non-measurement
  must never score as a result. This changes NO endpoint, threshold, or
  the §1.1 arithmetic; it is a measurement-hygiene guard. The verdict is
  being recomputed on a clean, error-free re-run (auto mode: cached
  successes replay, only errored queries re-hit with retries). Composition
  PASSED both before and after this change (+31.7pp, CI lower +24.2pp);
  the guard governs whether repetition non-inferiority is judged on clean
  data. Whatever the clean re-run says stands.
- 2026-07-07 — §9.5 registered: the FRAME concept (frame-relative truth,
  visibility inheritance, explicit promotion, cross-frame ideation as a
  first-class traced operation) as a Stage-1.5 DRAFT with named breaking
  points. Design only; no scope or endpoint change to the running
  campaign. Haiku c1c legs (5 seeds) approved and launched (≈$60,
  user-authorized).
- 2026-07-10 — Frames build steps 2+3 complete (generator frames preset +
  validate guarantee 5 + harness frame slices + the four §9.6.5 diagnostic
  oracles), all BEFORE any naturalizer token or LLM-condition run. No
  endpoint, threshold, or arithmetic change. Three refinements to the
  §9.6.5 QUALITATIVE predictions, discovered by the LLM-free dev-seed runs
  (seeds 99, 7; exact counts still freeze on the first gated seed batch):
  (1) mono-world cannot score 100% on v0 slices — rehoming frame-scoped
  supersessions and scenario exception facts into one world necessarily
  perturbs some actual derivations (seed 99: comp+ 63/80). That damage IS
  the frame-DAG failure mode, so it is reported, not repaired. (2)
  mono-world passes the sarcasm trap sub-line (5/5) because sarcasm
  literals exist only as episode events, never in world.json — sarcasm
  detection is the literalist's cell (0/5), separating assertion-type from
  frame-DAG failures as designed. (3) literalist scores 18/20 on ideation:
  literals asserted into actual add spurious (value, actual) satisfiers —
  speech-act contamination surfacing in cross-frame enumeration; recorded
  as part of its fingerprint. Also: the misattribution slice now
  GUARANTEES the inherited-atom targets (truth spans [actual, both
  scenarios]) — without them an isolationist store aced the slice (20/20);
  with them it fails exactly those 4 (16/20). Dev note: seed 42 is
  intractable under batch-level knobs (pre-existing join explosion,
  identical on the plain batch preset) — a rejected candidate by protocol;
  frames dev seeds are 99 (fast, ~5 min) and 7 (~12 min).
- 2026-07-09 — §9.6 registered: frames v1 committed spec +
  pre-registration, resolving §9.5. Design input consolidated in
  FRAMES-DESIGN-NOTES.md (three-lens expert analysis). The five open
  decisions taken as recommended (frame-distance precedence YES;
  frame-local removal = frame-scoped supersession/block; forecast
  default proposed-until-observed as a promotion-policy primitive;
  F-E2 bar 15pp; Track 3 after F-E1/F-E2 verdicts). Endpoints
  F-E1..F-E4, diagnostic pattern table, tier-M authenticity-certificate
  threshold (surface-cue baseline ≤0.65 balanced acc on pooled traps),
  and the frames seed protocol locked BEFORE any naturalizer token,
  seed lock, or frames extraction run. v0 endpoints/verdicts untouched
  (campaign closed: H5 PASS, H6 retention 1.0, C3 triangle complete).
- 2026-07-10 (later) — Frames build step 4 complete: loom C2a frame ingest
  (structured). The store gains a frame table (compiled from EvFrame
  declarations; actual stays implicit), promotion records (audit trail for
  the S2 proposed-until-observed policy; no closure impact in easy mode —
  the confirming observation arrives as its own fact), and speech-act
  discipline: non-assertive events (sarcasm) are counted and deliberately
  NOT committed; quotes commit as assertions homed in the speaker's
  perspective frame (payload FrameID). Fact dedupe key widened to
  frame+block+atom+interval (scenario overrides deliberately duplicate
  actual atom keys). Ops are frame-parameterized (HoldsIn/FindIn; closure
  cache keyed by frame) and the C2a condition implements FrameAnswerer
  (which_frames over the ingested frame universe, framed find over the
  query's frame scope). Exit criterion MET: loom-C2a == frame-oracle on
  BOTH frames dev seeds (99, 7) at full JSON-report granularity — every v0
  slice, all six frame slices incl. misattribution 20/20 (was 0/20 frame-
  blind) and all contamination sub-populations incl. sarcasm 5/5 (was 0/5
  believed). v0 regression intact (sample-dataset: diagnostic pattern
  byte-identical, C2a and c2b-det oracle-equal); validator green on both
  dev seeds. No endpoint, threshold, or arithmetic change. Next: build
  step 5 = cmd/batch frames edition (candidates 1..40, v0 + §9.6.8 gates,
  first 20 passers locked + committed BEFORE any LLM run; exact diagnostic
  per-cell counts freeze there).
- 2026-07-11 — Frames seed list LOCKED (§9.6.8, build step 5). cmd/batch
  frames edition (v0 gates + frames gates + worker pool with numeric-order
  finalization provably identical to a sequential run) over candidates
  1..40. **Locked 20: {1, 2, 3, 7, 8, 9, 10, 12, 13, 14, 15, 16, 18, 19,
  20, 21, 22, 24, 25, 29}.** Rejects, all pre-registered failure modes,
  recorded in datasets/frames-batch-v1/batch-manifest.json (committed;
  datasets regenerable deterministically, same policy as the v0 lock):
  4/17/26 join explosion, 6/11 frame firing-hygiene, 23/28 gap-trap
  shortfall, 5/27 zero composition positives + majority over-firing
  (these two took 2-6 h wall-clock each before their gates fired —
  pathological worlds, correctly rejected, not instrument hangs).
  Candidates 30..40 skipped (quota met at seed 29). No LLM token has
  touched any frames dataset. No endpoint, threshold, or arithmetic
  change. Next: freeze exact §9.6.5 diagnostic per-cell counts from the
  locked batch (LLM-free harness), then tier-M naturalization.
- 2026-07-11 (later) — §9.6.5 diagnostic table FROZEN. LLM-free harness
  over all 20 locked seeds (per-seed JSONs in results/frames-diagnostics/,
  committed); exact per-cell counts (summed over the batch) now in §9.6.5.
  Zero scoring errors. All qualitative predictions, including the three
  2026-07-10 refinements, hold in the frozen counts. Two strengthenings of
  previously-verified facts, no new claims: (1) loom-C2a == frame-oracle
  in every cell of every locked seed — the S1 frame-ingest exit criterion,
  previously verified on dev seeds {99, 7}, holds on the full locked
  batch; (2) frame-oracle is perfect in every cell (ceiling intact). No
  endpoint, threshold, or arithmetic change. Frames build step 5 complete;
  next: tier-M naturalization (naturalizer outside the evaluated matrix,
  ≥2 families, H4 content-preservation + frame-recoverability judge panel
  + authenticity certificate).
- 2026-07-12 — INSTRUMENT amendment (logged before any LLM token touches a
  locked seed): the first naturalizer tokens — on DEV seeds {99, 7} only —
  exposed a surface-cue leak in the frames instrument itself. Frame-homed
  base facts carried type-revealing source names (manuscript_fic_*,
  narrator_*, planning_scn_*, forecast_*) which the tier-E templates
  rendered into episode text; since every identifier in text must survive
  naturalization verbatim (H4 content preservation), the §9.6.6 surface-cue
  baseline would read the frame type off the source token on EVERY tier,
  making tier-M certification impossible by construction. Fix: the tier-E
  templates no longer render sources for frame-homed fact lines (fiction /
  scenario-delta / perspective-narration); payloads are untouched.
  Verified on all 20 locked seeds: world.json and queries.jsonl
  byte-identical, zero non-text payload diffs (only rendered text lines
  changed), and the FROZEN §9.6.5 diagnostic table reproduces per cell
  per seed (LLM-free harness re-run vs committed
  results/frames-diagnostics/ JSONs — no cell changed). Locked
  episodes.jsonl files refreshed in place; seed list, gates, endpoints,
  thresholds, arithmetic all unchanged.
- 2026-07-12 (later) — Frames build step 6: tier-M naturalization pipeline
  BUILT and dev-verified (cmd/naturalize + cmd/authcert; design memo
  synthworld/docs/tierM-naturalization.md). Roster (all outside the
  evaluated matrix, judges disjoint from naturalizers): naturalizers
  mistral-medium-3.5 + deepseek-v4-pro; judges gemini-3.5-flash,
  kimi-k2.6, grok-4.20-non-reasoning. Enforcement: mechanical content
  preservation (v0 rules + frame-ID ban + registered handles + banned
  marker vocabulary + genre-word ban on fiction content lines),
  3-judge frame-recoverability audit (≥2/3 exact, retry-with-feedback,
  cross-naturalizer escalation for frame-bearing episodes, zero-tolerance
  gate on frame-bearing fallbacks), LLM-free authenticity certificate
  (marker regex + leave-one-seed-out naive-Bayes answering the
  contamination+isolation trap queries; tier-E calibration confirmed the
  pre-registered marker-detector ≈1.0 / certification refusal).
  OPEN INTERPRETATION QUESTION flagged for ratification, decided by no
  one yet: §9.6.6 bounds the baseline at ≤0.65 balanced accuracy "on the
  pooled contamination+isolation trap set" — WITHOUT "paired controls"
  (which F-E1 names explicitly). With controls included the bound is
  unreachable by construction (controls are verbatim-stated facts,
  trivially grep-correct, pinning one class at ~1.0). cmd/authcert
  therefore reports BOTH readings and gates on the trap-direction
  balanced accuracy (mean of contamination-trap accuracy and
  isolation-trap accuracy) pending Toni's ratification; no locked-seed
  naturalization or certification happens before that ratification.
  Dev-seed pipeline numbers (final, seeds 99/7): fallbacks 0.4%/2.0%,
  frame-bearing fallbacks 0/0, judge-unrecovered frame lines 3.7%/1.4%,
  actual-line judge false alarms 0/1754, tier-E marker hits 0. LOSO
  certificate numbers: contamination-traps 0.961, isolation-traps 0.553
  → trap-direction 0.757 (with-controls pooled 0.827) — NOT ≤0.65 under
  either reading. Diagnosis: with markers dead and genre vocabulary
  banned, a supervised leave-one-seed-out classifier still finds
  structural lexical regularities (most sharply: actual observation
  lines carry a data-feed token, frame-homed lines do not) — and any
  text from which the judge panel can recover frames at ≥95% carries
  SOME learnable signal; a labeled-data shallow classifier is strictly
  stronger than the H4 analog (an unsupervised det-extractor whose
  collapse certified the paraphrase tier). Decision needed from Toni
  before locked-batch naturalization: (a) treat marker-regex collapse
  (achieved, 34→0) as the registered certificate and REPORT the
  supervised-classifier number as a hardness descriptor without a gate,
  (b) keep the 0.65 gate but define the baseline as unsupervised
  (regex/lexicon only), or (c) keep the supervised gate and accept that
  tier M as designed cannot certify (which would falsify the tier design,
  not the frames thesis). No softening is adopted here; nothing runs on
  locked seeds until this is ratified and logged.
- 2026-07-12 (RATIFICATION) — Toni ratified reading (a) of the §9.6.6
  authenticity certificate: the registered certificate is the COLLAPSE OF
  THE TIER-E MARKER DETECTOR (unsupervised regex baseline, ≈1.0 on tier E
  by calibration, must hit ZERO lines on tier M) — the exact analog of the
  H4 paraphrase certificate. The supervised leave-one-seed-out naive-Bayes
  trap-direction number is REPORTED UNGATED as a hardness descriptor in
  every writeup (dev value 0.757; with-controls 0.827), on the argument
  that text a judge panel decodes at ≥95% necessarily carries learnable
  signal, so a labeled-data classifier is strictly stronger than any
  registered surface-cue baseline and cannot be a gate. cmd/authcert
  amended accordingly (Certified := marker_hits == 0 with descriptor
  computable; descriptor printed with advisory 0.65 reference). Rationale
  and options (a)/(b)/(c) recorded in the previous entry; this ratification
  unblocks locked-batch tier-M naturalization.
- 2026-07-12 (later, PIPELINE amendment mid-batch) — first locked-batch
  tier-M run (v2 driver, 3-way seed parallelism) surfaced two mechanical-
  validator defects on seed-2 (and a borderline seed-8): (1) the
  block-delta removal-language guard used uninflected word-boundary forms
  (\bdisregard\b, "set aside") and falsely rejected valid naturalizations
  ("is disregarded", "setting aside") — on frame-bearing scenario lines
  this cascaded into zero-tolerance frame fallbacks; (2) the number-count
  retry feedback said "repeat it (the day is both the log date and the
  validity start)" even when the required count was 1, driving both
  naturalizers to oscillate between 0 and 2 mentions for 20 attempts
  (seed-2 ep_262). Fixes: inflected removal-language alternatives; exact-
  count feedback ("must appear EXACTLY N time(s)", overshoot variant, no
  repeat-hint unless N>1). Regression tests added. These change the
  mechanical validator's acceptance set (strictly broader) and retry
  prompts only — no registered gate, threshold, or scoring semantics is
  touched. DISCIPLINE: the whole 20-seed batch is re-run under the fixed
  validator (uniform validator version; cassettes replay unchanged
  prefixes), with batch-wide -retries 10 -judge-rounds 5 (operational
  budgets, recorded per-seed in naturalize-report.json). Partial results
  from the pre-fix run are discarded as non-evidence.
- 2026-07-12 (later still, INFRASTRUCTURE incident + amendment) — the v3
  re-run exposed that judge 2 (kimi-k2.6 @ Moonshot) had been failing on
  ~80% of calls: the Moonshot account was suspended mid-campaign
  (insufficient balance), and judge API errors were silently tolerated as
  nil verdicts, shrinking the panel to 2 judges requiring unanimity —
  visible only as elevated judge-unrecovered rates (seed-2 6.6%, seed-8
  5.1%). Evidence: 574 kimi cassettes vs ~3,088 for each other judge;
  live probe returned exceeded_current_quota_error. Remedies: (1) judge 2
  endpoint moved to Nebius hosting the IDENTICAL model
  (moonshotai/Kimi-K2.6, temp 1) — the registered roster (model
  identities, family disjointness) is unchanged, only the host; (2)
  cmd/naturalize now counts errored judge verdicts per judge
  (judge_errors in the report) and FAILS the run if any judge errors on
  >10% of episodes — a silently dead judge can never again be absorbed
  into unrecovered rates. All panel verdicts are recollected: the batch
  restarts uniformly under the fixed validator + healthy panel;
  naturalizer/other-judge cassettes replay, kimi verdicts are re-asked
  live at Nebius. Judge-error counts from this incident's partial runs
  are non-evidence.
- 2026-07-13 (PIPELINE amendment #2, whack-a-mole fix) — with a healthy
  panel, seed-2 went green (unrecovered 1.5%) but seed-3 exposed a
  retry-loop failure mode: lines are validated independently, yet the
  loop demanded ALL lines valid in the SAME attempt. Models fixing a
  flagged line routinely rewrote an already-valid line back into the
  instruction-example style (source token audit_note replacing the true
  registry_B/partner_feed/...), so validity alternated between lines and
  frame-bearing episodes fell back spuriously (seed-3 ep_157: line 1
  valid on odd attempts, line 3's twice-written day never joint-valid;
  31 attempts). Fix in cmd/naturalize: per-line PINNING — a line is
  locked the first time any attempt renders it validly; later attempts
  only need the still-failing lines. Convergence is now monotone in
  attempts. Retry feedback also tells the model accepted lines are
  locked, to copy identifiers exactly from the original (never from the
  instructions' example), and shows a concrete two-role phrasing for
  repeated day values. First-attempt prompts are byte-identical
  (cassettes replay); only retry paths change. Gates, judges, scoring
  untouched. Batch restarted uniformly (v5).
- 2026-07-13 (PIPELINE amendment #3, marker-list sync) — the first full
  20/20-green batch failed certification by exactly 6 marker hits: six
  scenario-supersession lines (one each in seeds 2/3/10/12/15/29) kept
  the tier-E phrase "within this scenario", which cmd/authcert's
  markerClass fires on but the mechanical validator's bannedMarkers list
  did not ban — the two lists were out of sync ("in the story" had the
  same gap, 0 corpus hits). Fix: both phrases banned in cmd/naturalize;
  regression test asserts every markerClass trigger phrase is covered by
  bannedMarkers (the certificate stays the arbiter, the ban list the
  enforcer). Corpus blast radius verified before the re-run: exactly the
  6 leak lines, no collateral. Batch re-run replays everything except
  those 6 episodes' retry paths. The refused certificate is recorded as
  designed behavior: the scaffold caught a real leak; nothing was
  regenerated by hand.
- 2026-07-13 (FRAMES BUILD STEP 7 COMPLETE — tier-M locked batch
  naturalized and CERTIFIED) — all 20 locked seeds naturalized in one
  uniform pipeline version (validator fixes + per-line pinning + marker
  sync + healthy 3-judge panel with judge 2 at Nebius), batch-wide
  -retries 10 -judge-rounds 5. Aggregate over 5,899 episodes: fallbacks
  50 (0.85%, max seed 2.4%; bound 10%), frame-bearing fallbacks 0
  (zero-tolerance, met), judge-unrecovered frame lines 71/2,712 (2.6%,
  max seed 3.7%; bound 5%), actual-line judge context misses 0/17,824,
  judge errors 0. AUTHENTICITY CERTIFICATE (ratified reading (a)):
  tier-E marker regexes collapse to 0 hits on all 20 naturalized seeds →
  CERTIFIED; tier-E calibration on the same seeds fires 2,592 marker
  hits and is refused (scaffold live). Ungated hardness descriptor:
  supervised LOSO-NB trap-direction balanced accuracy 0.791
  (contamination-traps 0.996, isolation-traps 0.585; with-controls
  pooled 0.850) — reported in every writeup per the ratification.
  Payload identity: episodes_natural.jsonl differs from episodes.jsonl
  in rendered text only, byte-identical otherwise, all 20 seeds.
  LLM-free harness sweep on the naturalized stream: loom-C2a ==
  frame-oracle == frozen §9.6.5 table per cell per seed on all 20 seeds.
  Artifacts committed: per-seed episodes_natural.jsonl +
  naturalize-report.json, authcert reports (tier-M + tier-E
  calibration), batch driver/seed logs, harness-natural JSONs, and the
  full cassette set (cassettes/naturalize-frames-v1, ~272MB) for
  deterministic replay. First certification attempt (6 marker hits,
  refused) preserved in postbatch-verify-run1-marker-leak.log. Next:
  tier-H on the first 5 locked seeds (no judge filter; ~100-event human
  audit by Toni), then C2b frames extraction (F-E1..F-E4).
- 2026-07-17 (Frames build step 8 — TIER-H HUMAN AUDIT DONE) — Toni
  completed the full 100-item blind human-recovery audit (frames v1,
  §9.6.6) via the local webaudit tool, first 5 locked seeds, no judge
  filter. Confirmed clean: every answer timestamp (2026-07-16 21:47 →
  2026-07-17 09:09) postdates both tool-comprehension fixes (commits
  e9864cb1, 3e90eb5a) — this is a measurement of genuine human
  performance against the corrected instructions, not an artifact of
  broken onboarding. Result (self-reported: "mostly guessed" after one
  read of the rules — this is the honest naive-analyst ceiling the
  tier is designed to produce, not a trained-annotator ceiling):
  **exact (context+type) 52/100; context-only 78%; type-only 62%;
  0 items flagged as unsure.** Per-seed exact: seed1 12/20, seed2 8/20,
  seed3 11/20, seed7 11/20, seed8 10/20 — uniform, no single seed
  driving the result. Dominant confusions: statement→declaration (19,
  the "declaration is never actual" rule under-applied in the
  opposite direction — treating an ordinary statement as if it must be
  announcing something), sarcasm→quote (10, the sarcasm-outranks-quote
  tie-break not internalized), declaration→statement (6, the
  complementary miss). Per §9.6.6 this number is reported
  machine-vs-human and sits OUTSIDE all kill arithmetic — it does not
  gate F-E1/F-E2. Interpretive note for any writeup: the ~97%
  judge-panel recoverability certified for tier M (§9.6.6/§10
  2026-07-12) is panel-of-3-with-retries on TEXT ALONE by models
  reading many examples in context; this 52% is one human's single
  blind pass reading the stated rules once. The gap is expected and is
  itself evidence that frame-tracking pragmatics, while learnable, are
  not "obvious" — reinforcing rather than undermining the case for a
  compiled substrate that gets it right deterministically. Artifacts:
  results/tierH/human-audit-score.json (full per-item breakdown)
  committed. Next: C2b frames extraction (F-E1..F-E4) on the certified
  tier-M corpus — no further gates block it.
- 2026-07-17 (Frames build step 9 — C2b frames machinery + operational
  registrations, logged BEFORE any measured frames LLM run) — the S2
  pipeline is now frame-aware behind the same Commit path (spec §5):
  candidates gain frame/block/assertion fields plus frame-declaration and
  promotion kinds; non-assertive content is skipped at commit (counted,
  mirroring C2a); quotes commit as assertions homed in the speaker's
  perspective frame; missed declarations auto-register PROVISIONAL frames
  (kind from the canonical-ID prefix, else the §9.6.1 safety default
  perspective) and a later declaration upgrades them; the hygiene gate
  probes every ingested frame's closure for join explosions. Verified
  LLM-free: loom-c2b-frames-det (tier-E template inverse, a CONTROL) ==
  frame-oracle in every cell on dev seed 99, and compilation fidelity
  1.000/1.000 on facts/rules/supersessions with frame-assignment macro-F1
  1.000; full locked-batch equality sweep running. Operational
  registrations decided and locked now:
  (1) FRAME-NAME AFFORDANCE. Tier-M text bans raw frame IDs (§9.6.6), but
  queries carry them. The naturalize report's ID↔handle table is provided
  UNIFORMLY to every text-mode condition (C2b-frames, C2b-prov null, and
  query text itself is presented in handle vocabulary for all conditions,
  IDs kept in structured fields and scoring). Rationale: frames have
  user-visible names in any production deployment, the judges and the
  human auditor likewise read the declarations; naming is not frame
  detection — WHICH LINE belongs to which frame remains entirely the
  system-under-test's problem. On tier E the table is identity.
  (2) C2b-prov NULL POLICY (the §9.6.3 registered null, now concrete):
  same frame-BLIND extraction as loom-c2b (v0 prompt; cassettes shared),
  frameless store, per-item metadata = exact source span; query-time
  lexical filtering: item tagged with a fiction/scenario frame iff its
  span mentions that frame's handle; a quoted atom expression is speech
  (excluded from actual, attributed to the first mentioned narrator
  entity); scenario query = actual-tagged + own-tagged (no pin, no block
  overlays); fiction/perspective query = own-tagged only.
  (3) F-E2 CUED PARTITION (operational): a contamination trap is
  METADATA-CUED iff every episode line asserting its atom outside actual
  (or non-assertively) is filed out of actual by that same registered
  lexical filter; CONTENT-CUED otherwise; paired controls inherit their
  trap's class. The partition is computed by the scorer from ground truth
  + text, fixed by the registered rule set, never by any measured
  condition's output.
  (4) FRAMES ENDPOINT ARITHMETIC (cmd/aggregate -frames): CI machinery
  identical to §1.1 (10k resamples, RNG 42, nearest-rank percentiles).
  F-E1 bounds are single-condition bootstraps of per-seed balanced
  accuracies (contamination, isolation; pass both CI-lower ≥.85, kill
  either <.70, between = INDETERMINATE; gap sub-line mandatory
  reporting). F-E2: paired diffs vs the null — content-cued CI-lower
  ≥+.15 else KILL; non-inferiority (CI-lower ≥ −.02) on metadata-cued
  and every v0 slice (repetition, composition, flip, retain, find-F1).
  F-E4: ideation micro-F1 (frame-labeled pair sets), gate mean ≥.90,
  secondary. F-E3 via cmd/fidelity: frame confusion over world facts
  matched by ID-then-content, macro-F1 over frames, fiction→actual
  leakage /all fiction items (<2%), exile + abstention/coverage reported;
  content identity now includes home frame + block polarity and drops
  Source for frame-homed facts (not recoverable from text BY DESIGN since
  the 2026-07-12 instrument amendment — scorer definition fixed before
  any LLM fidelity run).
  (5) Frame-blind loom-c2b keeps mono-world answering semantics
  (FrameBlind flag) so the frozen §9.6.5 row "loom-c2b-det == v0 oracle"
  stays reproducible, and doubles as the §9.6.3 contamination
  confirmation condition. No endpoint, threshold, or kill-criterion
  change; v0 sample-dataset diagnostics reproduced byte-identical after
  all of the above.
- 2026-07-17 (later — OPEN INTERPRETATION QUESTION for F-E2, flagged from
  DEV-seed evidence only, decided by no one yet; locked-seed F-E2 verdicts
  are HELD until Toni ratifies, exactly like the 2026-07-12 §9.6.6
  ratification) — first frames LLM tokens (qwen, dev seed 99 tier-M)
  exposed an instrument-level tension between §9.6.6 and §9.6.7: the
  tier-M mechanical validator REQUIRES a handle/entity mention on every
  frame-homed line (that is how content preservation is enforced), so the
  registered lexical filter files nearly every contamination trap line
  correctly from its span alone — the "content-cued" contamination subset
  collapses to ~5 sarcasm pairs/seed, and the §9.6.3 null is near-ceiling
  on the rest (dev 99: null contamination 50/51). "Unmarked narrative",
  which §9.6.3 names as a predicted content-cued failure, cannot exist in
  a §9.6.6-certified corpus by construction. Meanwhile the frames
  condition beats the null exactly where the §9.6.3 prediction named
  STRUCTURAL failures of query-time filtering (dev 99: promotion 13/14 vs
  4/14 pos and 10/10 vs 0/10 neg, misattribution 14/20 vs 4/20, ideation
  18/20 vs 5/20, isolation traps 27/28 vs 20/28, pinning 19/32 vs 16/32
  cells) — plus fully intact v0 slices. Note also: the implemented null
  (registered earlier today, §10 build-step-9 entry, point 2) is the
  STRONG reading of "query-time metadata filtering" — it re-derives a
  filtered closure per query frame, so scenario chains DERIVE for it,
  which is stronger than §9.6.3's "cannot apply delta-override overlays
  along a derivation chain" prediction anticipated (blocks and pinning
  still structurally fail). Readings for ratification:
  (a) keep the build-step-9 operationalization (content-cued =
  contamination traps whose line the lexical filter cannot file): F-E2
  then gates on a ~5-pair-per-seed sarcasm subset — statistically fragile
  and arguably kill-by-construction given the §9.6.6 validator, with the
  structural advantages relegated to ungated reporting;
  (b) content-cued trap subset := the frame-slice trap queries whose
  deciding cue is STRUCTURALLY absent from lexical metadata — the
  build-step-9 content-cued contamination traps PLUS isolation
  override/chain-control traps, pinning pairs, promotion pairs,
  misattribution, and ideation (each already paired/balanced); metadata-
  cued := the complement (handle-marked contamination + inherited/chain
  isolation positives); same ±15pp/-2pp arithmetic;
  (c) report both, gate on (b).
  No verdict is computed under any reading before ratification; all
  measurements (extraction, reports, per-seed metrics) proceed — they are
  reading-independent. Nothing here touches F-E1, F-E3, F-E4, or any v0
  endpoint.
- 2026-07-18 (FRAMES BUILD STEP 10 — first full F-E1..F-E4 run, qwen
  primary leg, 20 locked seeds; NEGATIVE/MIXED and reported as such, no
  softening) — loom-c2b-frames (self-hosted qwen36-nvfp4, thinking-off
  extraction) vs the registered c2b-prov null on the CERTIFIED tier-M
  corpus, all 20 locked seeds, 0 API errors (data-integrity clean;
  artifacts results/frames-e-qwen36/, verdict JSONs committed).
  RESULTS:
  * F-E1 (safety, both directions) — PASS. Contamination balanced-acc
    0.912 (CI-lower 0.882), isolation 0.956 (CI-lower 0.936), both ≥0.85;
    neither near the 0.70 kill line. Gap-trap sub-line 0.892 (CI-lower
    0.854). Sarcasm discipline PERFECT: 0/113 non-assertive literals
    believed in any frame across all seeds.
  * F-E4 (cross-frame ideation attribution) — PASS. micro-F1 0.928
    (CI-lower 0.904) ≥ 0.90; exact-set 0.753.
  * F-E3 (frame-assignment fidelity, SECONDARY/diagnostic) — MIXED, one
    named gate FAILED. macro-F1 0.942 (min 0.874; 3 seeds dip below the
    0.90 gate), rule-frame 0.996, coverage 0.997, sarcasm believed 0/113
    — BUT fiction→actual leakage 0.21 mean / 0.58 max, badly above the
    <0.02 gate. Mechanism confirmed genuine (not a scorer/handle bug):
    both fiction frames are recovered, but the free qwen extractor
    inconsistently homes UNMARKED naturalized story prose — some lines to
    the story frame, some to actual (e.g. seed-16 fic_01 13✓/11 leaked).
    This is precisely the §9.6.6-predicted "unmarked narrative" hard case,
    and it is the mechanism behind the reading-(a) contamination weakness
    below. It is an EXTRACTOR-QUALITY finding about a small free model, not
    a substrate-architecture finding (the store homes exactly what the
    extractor assigns; loom-c2b-frames-det on the same corpus is
    frame-oracle-exact).
  * F-E2 (superiority over the null, co-primary) — VERDICT HELD pending
    Toni's ratification of the cued-partition reading (registered
    2026-07-17), because the two readings point OPPOSITE directions and
    the split is the exact kill-by-construction risk flagged then:
      – reading (a) [registered default]: KILL. Content-cued balanced-acc
        A−B +0.139 (CI-lower +0.108 < +0.15), and non-inferiority also
        fails on metadata-cued (−0.097). Driven entirely by the §9.6.6
        validator forcing a handle onto every frame-bearing line: the
        null's lexical filter therefore aces contamination (0.968 trap
        acc) and the "content-cued" partition collapses to ~5 sarcasm
        pairs/seed. Under (a) the compile-time-frames bet is falsified on
        this leg.
      – reading (b) [structural traps query-time filtering architecturally
        cannot reach]: DECISIVE WIN. Per-seed A−B (n=20): promotion +0.776,
        misattribution micro-F1 +0.482, pinning +0.456, ideation +0.261,
        isolation +0.173 — every one a §9.6.3-predicted structural failure
        of the null, none reachable by lexical/provenance filtering. v0
        slices non-inferior to the null (composition −0.007, repetition
        −0.000, find −0.007; flip −0.019 the only sub-−2pp leg, on a
        ≤6-flip-per-seed base). Frame-blind loom-c2b confirms the §9.6.3
        contamination prediction: 0.082 trap acc (≈92% fiction believed).
    No verdict is adopted, no threshold or kill-criterion is touched; the
    reading is Toni's to ratify exactly as the §9.6.6 certificate reading
    was (2026-07-12). BOTH readings' numbers are on record here and in the
    committed per-seed reports (which now carry isolation/pinning/promotion
    per-subpop + cue_sub decompositions, so any ratified reading is
    computable without re-running).
  INTERPRETATION (for the ratification memo, not a verdict): the null was
  built to be beaten on content-cued frames (sarcasm, unmarked narrative,
  mid-episode switches) AND on scenario composition. The §9.6.6 tier-M
  certificate — which requires a recoverable handle on every frame line —
  is in tension with "content-cued": a corpus a judge panel decodes at
  ≥95% cannot contain much lexically-unmarked frame content by
  construction, so reading (a)'s content-cued subset is nearly empty and
  fragile. The substrate's advantage showed up exactly where the design
  notes said query-time filtering must fail STRUCTURALLY (promotion,
  pinning, misattribution, cross-frame ideation), not where it must fail
  LEXICALLY. Open questions this raises for Toni (none decided here):
  (1) ratify the F-E2 reading; (2) whether the F-E3 leakage warrants the
  swap-model legs (gpt-5-mini, haiku-4-5 — stronger extractors, cheap on
  the same cassette protocol) before any F-E2 verdict, since leakage is a
  model-capability artifact; (3) whether "content-cued" should be
  re-scoped to the structural slices given the certificate tension.
  Swap-model legs + F-E3-driven extractor iteration are the obvious next
  moves but are NOT started autonomously — they change what the primary
  verdict is measured on.
- 2026-07-18 (F-E2 RE-SPECIFICATION — PROPOSED, pre-registered BEFORE the
  gpt-5 swap extraction; pending Toni's ratification exactly as the
  §9.6.6 certificate reading was) — the qwen run (previous entry) exposed
  that the pre-registered F-E2 "content-cued" partition (reading (a),
  lexical markedness) is near-empty on ANY §9.6.6-certified corpus: the
  certificate REQUIRES a judge-recoverable handle on every frame-bearing
  line, so all contamination except sarcasm is lexically marked and the
  null's substring filter aces it, collapsing "content-cued" to ~5
  sarcasm pairs/seed. This is a design-level tension flagged 2026-07-17,
  not a post-hoc excuse — but the specific re-spec below IS informed by
  qwen data, so it is treated as a HYPOTHESIS and the gpt-5 swap run is
  its CONFIRMATORY, out-of-sample test (different extractor, partition
  fixed before its first token). Two changes, both built and committed
  before the swap extraction:
  (1) RE-AXIS on FILTERABILITY, not lexical markedness. The thesis is
  that a compiled substrate answers what query-time filtering CANNOT —
  composition/structural reasoning, per §1 and the design notes' own
  taxonomy (which classes sarcasm as an extraction-layer problem, NOT a
  storage category). New partition, keyed PURELY on ground-truth
  slice+subpop (cmd/harness classifyFilterability — independent of any
  condition's output, so the system under test cannot game it):
    - FILTERING-RESISTANT (answer needs closure computation no per-item
      metadata carries): promotion (pre/post-resolution + unresolved:
      time-gated belief), pinning (all: pin-day inheritance arithmetic),
      isolation override-blocked/override-active/chain/chain-control
      (delta overlay + derivation), contamination sarcasm (non-assertive:
      no item to filter on). ~88 balanced trap+control queries/seed.
    - FILTER-DECIDABLE (frame membership + cone lookup suffices):
      contamination contradiction/gap/quote + controls, isolation
      inherited + override-actual controls, promotion source-frame.
  (2) STRENGTHEN THE NULL to the ceiling of query-time reasoning
      (harness.FrameRAGCondition, "frame-rag"): frameless RAG over FULL
      naturalized episode text + complete frame semantics in-prompt + the
      SAME model, deciding frames PER QUERY. Handed strictly MORE than
      c2b-prov (whole episodes, not just extracted items), so any
      C2b-frames win over it is conservative. This removes the
      "reading (b) only beats our regex" circularity: the re-specified
      F-E2 gate is C2b-frames beating the HARDER of {c2b-prov, frame-rag}
      on the filtering-resistant pool by >=15pp CI-lower, at
      non-inferiority (-2pp) on filter-decidable AND every v0 slice.
  Integrity ledger: the re-axis LOOSENS vs reading (a) (which was
  near-unpassable by construction), so it genuinely requires ratification
  and is NOT adopted unilaterally; cmd/aggregate prints reading (a) AND
  the proposed reading (b') side by side, both computable from the
  committed per-seed reports (FilterSub decomposition). Strengthening the
  null only makes the test HARDER, so it is integrity-safe. F-E1, F-E3,
  F-E4 unchanged. Kill thresholds (±15pp/-2pp, seed-bootstrap CI)
  unchanged. Dev-seed-99 gpt-5 sanity BEFORE the locked run (not
  evidence, just a smoke): frame slices near frame-oracle
  (contamination 51/51+51/51, isolation 47/47+28/28, misattribution
  20/20, ideation 20/20), F-E3 leakage 0.21(qwen)->0.042(gpt-5) — the
  free-model extraction weakness that drove reading (a)'s qwen KILL
  largely closes with a frontier extractor, as predicted. The locked
  20-seed gpt-5 run is the confirmatory test.
- 2026-07-18 (F-E2 RE-SPEC RATIFIED — Toni) — Toni ratified the
  filterability re-specification of F-E2 (previous entry). The GOVERNING
  F-E2 is now: C2b-frames beats the HARDER of {c2b-prov, frame-rag ceiling
  null} on the filtering-resistant pool by >=15pp balanced-acc CI-lower, at
  non-inferiority (-2pp) on filter-decidable and every v0 slice. Reading (a)
  (lexical content-cued/metadata-cued) is retained as a REPORTED, SUPERSEDED
  diagnostic — never gates. cmd/aggregate -frames now prints the governing
  filterability verdict first and reading (a) beneath it labeled superseded;
  the -b2 flag supplies the ceiling null. Thresholds (±15pp/-2pp,
  seed-bootstrap CI, RNG 42) unchanged; F-E1/F-E3/F-E4 unchanged. The
  gpt-5 20-seed run (in flight) is the confirmatory test under this
  ratified endpoint. The qwen run is retained as the exploratory first
  leg that generated the re-spec hypothesis and is reported as such.
- 2026-07-18 (FRAMES F-E1..F-E4 — gpt-5 CONFIRMATORY 20-seed run, under
  the RATIFIED filterability endpoint; reported honestly, NEGATIVE on the
  co-primary) — loom-c2b-frames (gpt-5, extraction reasoning_effort
  minimal) vs the two nulls (c2b-prov + frame-rag ceiling), all 20 locked
  seeds, certified tier-M corpus, 0 API errors, frames-only answering pass
  (c0/rag dropped as non-endpoint baselines — cost correction, previous
  entries). Verdicts (results/frames-e-gpt5/verdict-governing.json):
  * F-E1 (safety, both directions) — PASS. Contamination 0.923 (CI-lo
    0.887), isolation 0.991 (CI-lo 0.982); both >=0.85. Sarcasm discipline
    intact.
  * F-E4 (ideation) — PASS. micro-F1 0.925 (CI-lo 0.902).
  * F-E2 (co-primary, GOVERNING filterability reading) — FAIL, and the
    failure is precise and informative:
      – SUPERIORITY overwhelmingly MET: filtering-resistant balanced-acc
        A-B = +0.335 CI-lo vs the CEILING null frame-rag (+0.354 vs
        c2b-prov) — >2x the +0.15 bar, against a full gpt-5 reasoner with
        all the episode text. The compile-time-frames advantage on
        structural traps (promotion/pinning/scenario-overlay/sarcasm) is
        real, large, and robust against the strongest query-time opponent.
      – NON-INFERIORITY on filter-decidable FAILS: A-B = -0.025 mean, CI
        [-0.065, +0.011], CI-lo below the -0.02 margin. v0 slices all
        non-inferior (composition +0.001, repetition 0, retain 0, find
        +0.019; flip +0.058). So the co-primary fails ONLY on the
        decidable-trap non-inferiority leg.
  MECHANISM (measured, not asserted): the filter-decidable miss is
  CONCENTRATED, not uniform — 9/20 seeds non-inferior-or-positive (4 at
  46/46 perfect), but ~5 seeds leak fiction heavily (seed-16 -0.227,
  seed-22 -0.219: gpt-5 homes only 20/46 fiction contamination traps to
  their frame, ~26 leaking to actual). This is the SAME residual
  fiction->actual leakage F-E3 measures; on the traps a lexical filter
  handles perfectly BY CONSTRUCTION (they are defined as
  membership-decidable), any extraction leakage shows as inferiority to
  the near-optimal null. gpt-5 cut mean leakage vs qwen (0.21->~0.04 on
  dev-99) but did not eliminate it, and the tail seeds bite the CI.
  OBSERVATION FLAGGED FOR TONI (NOT acted on — re-specing a failing
  endpoint after seeing it fail is exactly what pre-registration forbids,
  so this is reported, not fixed by moving the bar): the filter-decidable
  non-inferiority leg has the same "null is optimal-by-construction on its
  own partition" structure that sank reading (a) — a definitionally-perfect
  lexical filter will tie-or-beat any real extractor on the traps defined
  as filterable, so a -2pp non-inferiority bar there penalizes ANY
  leakage. Whether that is a fair gate is a genuine question, but it is
  Toni's to decide, not mine to re-spec post-hoc. The SPEC-PRESERVING path
  to a PASS is to reduce extraction leakage (a SYSTEM improvement, not a
  bar move): higher extraction reasoning effort, a leakage-targeted
  extraction-prompt fix, or a confidence-gated quarantine of low-confidence
  fiction assignments (the §9.6.1 safety-routing hook, currently dormant).
  None started autonomously. Full numbers: MASTERPLAN + verdict JSON;
  reading (a) also printed (FAIL, superseded diagnostic). qwen leg remains
  the exploratory first pass; gpt-5 is the confirmatory result.
- 2026-07-18 (F-E3 gpt-5 LOCKED-BATCH — CORRECTION to the dev-seed
  optimism in the entries above) — the 20-seed F-E3 fidelity sweep
  (gpt-5, extraction reasoning minimal, cassette replay;
  results/frames-e-gpt5/fidelity/) contradicts the dev-99 sanity number.
  LOCKED-BATCH fiction->actual leakage: mean 0.189, max 0.688, only 4/20
  seeds under the <0.02 gate (worst: seed-16 33/48=0.69, seed-22
  30/48=0.62, seeds 20/29/8 all 0.38). This is BARELY better than qwen's
  0.21 — the dev-99 value (0.042) I reported as "leakage largely closes
  with a frontier extractor" was an EASY-SEED ARTIFACT and does NOT
  generalize. Correcting it explicitly so the over-optimistic dev number
  is not left as the headline. macro-F1 0.947 (min 0.852) and sarcasm
  discipline 2/113 both remain strong — the weakness is SPECIFICALLY
  fiction-frame assignment on naturalized narrative prose, concentrated on
  the same seeds that fail the F-E2 filter-decidable leg (16,22,8,20,15
  overlap almost exactly), confirming one mechanism drives both the F-E3
  gate miss and the F-E2 co-primary FAIL. Implication for the open
  question already flagged for Toni: a frontier extractor at minimal
  reasoning does NOT solve fiction homing on the locked seeds, so the
  spec-preserving paths (higher extraction reasoning effort, leakage-
  targeted prompt, or confidence-gated fiction quarantine) are the real
  levers to test before any F-E2 verdict is treated as final — none
  started autonomously (new direction + spend = Toni's call).
- 2026-07-18 (LEAKAGE-FIX RE-RUN — pre-registered BEFORE any locked run;
  spec-preserving, endpoint UNCHANGED) — Toni approved the option-2 path
  (fix extraction leakage to meet the ratified F-E2, do NOT move the bar),
  with constraints: no second frontier run — use a CHEAP model at HIGHER
  reasoning effort, add the §9.6.1 quarantine, improve prompting/setup.
  Diagnosis (mechanism, from reading leaked-line PHRASING only — not tuned
  to seed content): the §9.6.6 certificate keeps a handle on every fiction
  line, but the handle is a BARE PROPER NOUN ("In Millwater, day 76 has it
  that registered_in(...)") indistinguishable from an entity/source name
  UNLESS the reader knows Millwater was declared a story — and the
  per-episode extractor at minimal reasoning often lacks that declaration
  context (declared in an earlier episode). The frame-rag ceiling null
  already received a frame directory; the substrate extractor did not — it
  was under-provisioned. Fixes, all developed on DEV seeds {99,7} and
  frozen before the single locked run (locked = held-out; no tuning to the
  seeds already seen to leak):
  (1) FRAME-CONTEXT extraction (two-pass): pass 1 extracts frame
      DECLARATIONS across all episodes; a directory (handle → kind) is
      built from the MODEL'S OWN declaration readings (NOT world.json — no
      ground-truth kind leak); pass 2 extracts facts with the directory
      injected, so "In Millwater" resolves as story content. Consistent
      with the ratified naming affordance and with what the null already
      had. WHICH line belongs to which frame remains the model's problem.
  (2) CONFIDENCE-GATED ACTUAL-QUARANTINE (§9.6.1 safety hook, previously
      dormant): a fact the extractor would home to ACTUAL with frame-
      confidence below threshold is committed Quarantined (not in the
      actual closure) instead of silently believed — "misclassification
      fails as stored-but-not-believed." Abstention/coverage reported
      alongside (a substrate may not ace fidelity by quarantining
      everything — existing F-E3 guard).
  (3) PROMPT: fiction-carryover ("once a story opens, later lines about it
      stay in it, even without the title"), directory use, and calibrated
      confidence for actual-homing.
  (4) MODEL: gpt-5-mini, reasoning_effort HIGH (cheap + more reasoning; the
      registered swap-B family). Extraction conditions (loom-c2b-frames,
      loom-c2b→c2b-prov) re-run on it. The ceiling null frame-rag is KEPT
      as the existing gpt-5 result — a STRONGER (frontier) null than the
      mini substrate, which only makes the superiority test HARDER
      (integrity-safe) and avoids re-spend. F-E2 governing = beat the
      harder of {mini c2b-prov, gpt-5 frame-rag} on filtering-resistant,
      non-inferior on filter-decidable (vs mini c2b-prov) + v0.
  Endpoint, thresholds, partition, kill arithmetic ALL unchanged. If this
  still fails, it is reported as an honest FAIL (fallback to option 1);
  no further bar-moving. Dev results + frozen config logged before the
  locked run; locked run reported whatever it yields.
- 2026-07-19 (LEAKAGE-FIX DEV-VALIDATED + FROZEN; one dev iteration logged)
  — the pre-registered leakage fix was validated on DEV seed 99 (held-out
  locked seeds untouched). First dev run OVERCORRECTED: leakage 0.21->0.000
  but a NEW failure — 455/775 actual facts EXILED to spurious frames named
  after the episode-header scaffolding ("=== Episode ep_003 ==="), which the
  frames-primed model mis-read as frame declarations (facts recall 0.99->
  0.49). The dev gate caught it before any locked spend. Fix: strip the
  "=== Episode ... ===" header lines from the LLM extractor's input (harness
  scaffolding, not world content; the deterministic extractors already skip
  them). Dev-99 v2 (frozen config): leakage 0.000 (fic_01 24/24, fic_02
  24/24), actual-exile 0.000, frame macro-F1 1.000, coverage 1.000
  (quarantine did NOT over-fire — abstention 0), facts recall 0.995. One
  honest blemish reported: sarcasm-believed 2/5 (gpt-5-mini @ high reasoning
  is slightly weaker than gpt-5 at non-assertive detection; small
  population, will surface in the filtering-resistant sarcasm sub-slice).
  FROZEN CONFIG for the locked run: gpt-5-mini, reasoning_effort high,
  temperature omitted; frame-context two-pass (directory from the model's
  own declarations, header-stripped); actual-quarantine threshold 0.5;
  frames-only. Ceiling null frame-rag is REUSED from the gpt-5 run (a
  stronger-model null → harder superiority test → integrity-safe, no
  re-spend). Locked 20-seed run next; reported whatever it yields (no
  further bar-moving; fallback is the honest option-1 FAIL writeup).
- 2026-07-19 (LEAKAGE-FIX CONFIRMATORY RUN — gpt-5-mini, 20 locked seeds,
  frozen config; reported honestly) — the spec-preserving leakage fix
  (frame-context two-pass + header-strip + §9.6.1 actual-quarantine@0.5 +
  fiction-carryover prompt), gpt-5-mini @ reasoning high, run once on the
  locked set; ceiling null frame-rag REUSED from the gpt-5 run (stronger
  null → harder superiority test). 0 API errors. Verdicts
  (results/frames-e-gpt5mini/verdict-governing.json):
  * F-E1 (safety) — PASS, near-ceiling: contamination 0.999 (CI-lo 0.998),
    isolation 0.996 (CI-lo 0.992).
  * F-E4 (ideation) — PASS, PERFECT: micro-F1 1.000, exact-set 1.000.
  * F-E3 (fidelity) — the fix's TARGET, CURED: fiction->actual leakage
    0.189 (gpt-5) -> 0.000 on every computed seed (incl. all prior heavy
    leakers 8/15/16/22 — e.g. seed-16 0.69->0.000, decidable A-B
    -0.227->+0.074), all under the <0.02 gate; macro-F1 ~0.92; quarantine
    abstention ~0.08 mean (its coverage cost, reported).
  * F-E2 (co-primary, GOVERNING filterability) — the two legs that failed
    on gpt-5 are now FIXED: filter-decidable non-inf -0.025 -> +0.064
    (substrate now BEATS the null on decidable), metadata-cued -0.085 ->
    +0.001. Superiority MASSIVELY met: filtering-resistant +0.324 CI-lo vs
    the gpt-5 CEILING null (+0.359 vs c2b-prov). YET the endpoint still
    reads FAIL — now on an ENTIRELY DIFFERENT and benign leg: v0
    composition non-inf CI-lo -0.032 (mean -0.0096), driven by ONE seed
    (seed-7: a single mis-extracted rule, 40/41, that is load-bearing for
    its compositions -> comp positives 49/80; leakage 0, abstention 0
    there — NOT the quarantine). 18/20 seeds have composition A-B >= 0.
  HONEST STATE: the fix did exactly what it was for — leakage eliminated,
  both prior F-E2 non-inf failures cured, F-E1/F-E4 pass, superiority huge
  against the strongest null. The pre-registered F-E2 co-primary
  nonetheless FAILs, now solely on a single-seed v0-composition extraction
  outlier (gpt-5-mini rule-extraction variance), unrelated to frames,
  leakage, quarantine, or architecture. This is NOT re-run to chase a pass
  (a third locked run tuned to seed-7 would be test-set fitting — refused).
  FLAGGED FOR TONI (not acted on): the co-primary now hinges on one seed's
  rule-extraction noise on a FRAME-FREE slice; options are (a) accept the
  honest near-pass and report the frames thesis as strongly supported with
  the endpoint tripping on unrelated extraction variance, (b) treat v0
  composition variance as an extraction-robustness caveat, (c) any
  pre-registered clean re-run must fix extraction ROBUSTNESS on dev, not
  seed-7 specifically. The compile-time-frames bet is NOT falsified (the
  kill trigger, superiority <15pp, is nowhere near); query-time filtering
  is decisively refuted (+0.32 vs the ceiling reasoner).
