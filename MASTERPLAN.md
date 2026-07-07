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
- 2026-07-07 — §9.5 registered: the FRAME concept (frame-relative truth,
  visibility inheritance, explicit promotion, cross-frame ideation as a
  first-class traced operation) as a Stage-1.5 DRAFT with named breaking
  points. Design only; no scope or endpoint change to the running
  campaign. Haiku c1c legs (5 seeds) approved and launched (≈$60,
  user-authorized).
