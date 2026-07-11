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
