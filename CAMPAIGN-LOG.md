# CAMPAIGN-LOG — append-only record of measurements and findings

*Companion to `MASTERPLAN.md`. Every entry is dated, states what was run,
the numbers, and the interpretation AT THE TIME. Later reinterpretations
get new entries, never edits. Negative and positive results receive the
same typography.*

---

## 2026-07-06 — E0 build status

Workstreams landed: B (LLM record/replay cassette cache, C0 no-memory,
D6 perfect-retrieval diagnostic, tmr envelope hardening, BM25 re-index
idempotency bug fixed), C (scoring-semantics test suite, D7 sanitization
audit with field allowlist), D (embedding + hybrid-RRF retrievers, C1c
long-context condition, token/usage accounting spent-vs-replayed, bounded
concurrency with report invariance, per-model request shaping via
HARNESS_LLM_TEMPERATURE / HARNESS_LLM_EXTRA_PARAMS). Workstream A landed
later the same day: batch preset (24 flips + 24 retained + 80 comp
positives per passing seed; 6/10 of seeds 1..10 pass gates), byte-identity
proven for existing presets (seed-1234 small regenerates identically),
cmd/batch seed-lock protocol, verification/gates extracted into gen/ as
reusable packages with tests. Known generator issues recorded: seed 4
errors with "rul_002: unbound var C" in the repair closure (pre-existing
edge case; protocol records it as a reject); generation times are
heavy-tailed (seconds typical, 15+ min minority). cmd/aggregate (the
§1.1 endpoint arithmetic with registered constants: 10k bootstrap
resamples, RNG seed 42, thresholds as consts not flags) built and tested.
memoexport rewritten to tmr's real memo schema; TmrRetriever
fixed to parse the verified envelope (`memo_id: "mem_<episodeID>"`,
chunk-dedupe by episode).

Infrastructure verified live: OpenAI (chat+embeddings), Anthropic
OpenAI-compat, self-hosted vLLM qwen36-nvfp4 (Qwen3.6-35B-A3B, 65k ctx,
free tokens, thinking suppressible), Babylon vLLM embedder
(Qwen3-Embedding-0.6B), tmr end-to-end (102 memos ingested, hybrid
retrieval working), babylon 4090 via SSH.

Model matrix registered BEFORE any E2/E4 run: A = qwen36-nvfp4
(self-hosted, enable_thinking=false, temp 0), B = gpt-5-mini
(reasoning_effort=minimal, temperature omitted — endpoint rejects 0),
C = claude-haiku-4-5 (temp 0). Rationale: A is open-weights/self-hosted
(the product posture), B and C give two independent cross-family swaps.
gpt-5-nano reserved for the extractor-size sweep. Reasoning kept minimal
by design: the benchmark measures what the MEMORY contributes; a
"does higher reasoning effort rescue RAG?" ablation is a registered
secondary question, not part of H5.

## 2026-07-06 — E1 (seed 1234): semantic retrieval does NOT close the gap — H1 refuted in the thesis-favorable direction

Provenance probe, LLM-free, seed 1234, k ∈ {4,8,16}, five retrievers.
Full-coverage (all provenance episodes of a query in top-k):

| retriever | comp@4 | comp@8 | comp@16 | rep@4 | rev@16 |
|---|---|---|---|---|---|
| bm25 | 2/50 | 5/50 | 8/50 | 30/30 | 0/12 |
| embed-Qwen3-0.6B | 0/50 | 1/50 | 1/50 | 5/30 | 0/12 |
| hybrid-bm25-embed | 0/50 | 3/50 | 7/50 | 11/30 | 1/12 |
| tmr-semantic | 0/50 | 0/50 | 2/50 | 4/30 | 1/12 |
| tmr-hybrid | 0/50 | 1/50 | 2/50 | 8/30 | 0/12 |

Findings, registered before any LLM condition has run:
1. **BM25 is the strongest retriever on this instrument, at every k, on
   every slice.** The pre-registered open question ("do embeddings close
   the gap?") is answered: no — they lose even repetition (13/30 @k=16
   for pure embed vs 30/30 @k=4 for BM25). Mechanism: templated episodes
   are lexically dense with exact entity tokens; a small embedder blurs
   entity identity inside multi-fact chunks.
2. **Revision provenance is unreachable by every retriever** (max 1/12
   @k=16). The supersession notice shares neither vocabulary nor
   semantics with the question it invalidates. This is now measured
   across lexical, semantic, and fused retrieval.
3. Caveats, honest: 0.6B embedder (not frontier); episode-granularity
   chunks; template-shaped text favors lexical matching. The paraphrase
   tier (MASTERPLAN §4.3) must confirm before generalizing beyond the
   instrument. A frontier embedding model can be swapped in via
   HARNESS_EMBED_* to check the embedder-size explanation cheaply.

Consequence for the campaign: the C1 retrieval ceiling stands at BM25's
level (composition 16% full-coverage @k=16). D6 (perfect retrieval) now
carries the burden of separating retrieval failure from reasoning failure.

## 2026-07-06 — E1 addendum: frontier embedder control kills the model-size explanation

Same probe, OpenAI text-embedding-3-large (frontier-class): composition
0/50 @k=4, 0/50 @k=8, 2/50 @k=16; repetition 12/30 @k=16; revision 1/12
@k=16 — statistically indistinguishable from the 0.6B Qwen embedder.
The semantic-retrieval failure on this instrument is STRUCTURAL, not a
capacity artifact: embedding a multi-fact episode chunk blurs exact
entity identity, which is precisely what compositional provenance
retrieval needs. Caveat that remains: template-shaped text (paraphrase
tier still owed); caveat removed: embedder size.

## 2026-07-06 — E2 smoke v1 (seed 1234, qwen36, thinking off): INVALID — harness-first discipline caught a prompt-fairness bug

First live LLM run ever. All LLM conditions floored near always-false:
rag-bm25 rep+ 3/30, d6-perfect-retrieval rep+ 7/30 (with the true episode
IN CONTEXT), c1c rep+ 0/30. Standing order applied (suspect the harness
first). Cassette audit found the mechanism: a repetition positive whose
observation says "valid from day 49" (open interval) asked at day 360 —
model answers false. The system prompt never stated that open validity
means "holds forever"; the model applied temporal-persistence skepticism.
Controlled 2×2 on the failing case (prompt-old/new × thinking-off/on):
  old+nothink=false(wrong) | old+think=true | new+nothink=true | new+think=true.
Diagnosis: prompt-semantics gap, not model incapacity; thinking can
compensate at ~250× the tokens (551 vs 2).

Actions: (1) ragSystemPrompt rewritten to carry the full DESIGN.md
semantics contract — registered as a FAIRNESS fix (the baseline must
know the rules of the game; its handicap is memory organization only);
(2) thinking on/off registered as an ablation dimension ("does test-time
reasoning rescue RAG?" — token cost is part of the answer); (3) v1
numbers discarded as invalid, run re-executed with the fixed prompt.
Note: this incident is itself evidence for the D6/cassette
instrumentation — the bug was findable in minutes because every prompt
and response is on disk.

## 2026-07-06 — E2 smoke v3 (seed 1234, qwen36, THINKING ON): the C1 mechanism map, and a ceiling warning

Thinking-off (v2) is not a viable C1 mode: rag-bm25 rep+ 4/30, D6 23/30,
c1c 0/30 — reflex answers can't scan multi-episode context. Thinking-on
(v3) is the registered C1 benchmark mode; token cost is part of the
result. Numbers (seed 1234, fixed-semantics prompt):

| condition | rep+ | comp+ | comp- | rev.flip | rev.ret | find | compl.tok/query |
|---|---|---|---|---|---|---|---|
| c0-no-memory | 2/30 | 7/40 | 27/33 | 6/6* | 0/6 | 0/10 | 727 |
| rag-bm25 k=8 | 29/30 | 5/40 | 33/33 | 6/6* | 0/6 | 0/10 | 1742 |
| c1c-longcontext | 27/30 | 31/40 | 32/33 | 6/6 | 2/6 | 1/9 | 4364 |
| d6-perfect-retrieval | 30/30 | 38/40 | 33/33 | 2/6 | 6/6 | 4/10 | 1138 |
| loom-C2a (reference) | 30/30 | 40/40 | 33/33 | 6/6 | 6/6 | 10/10 | 0 |

(*flips right for the wrong reason — answers false to everything derived;
retained controls catch it, exactly as designed.)

Findings, each mechanistically clean:
1. **RAG ties repetition (29/30) and collapses on composition (5/40)** —
   the pre-registered H2 prediction, now measured live. Its revision
   pattern is the episode-grep signature (6/6 flips + 0/6 retained =
   false-to-everything-derived), not revision competence.
2. **D6 (perfect retrieval) composes at 38/40** — given the right
   episodes, the LLM's reasoning is NOT the bottleneck; retrieval is.
   H2's "even perfect retrieval fails" prediction is REFUTED at this
   depth/scale. The C1 composition failure is a retrieval failure.
3. **Revision asymmetry, the subtlest finding**: D6 UNDER-revises
   (2/6 flips despite the supersession notice being in its context —
   stale beliefs are sticky even with contrary evidence present) while
   c1c OVER-revises (6/6 flips but only 2/6 retained). Neither long
   context nor perfect retrieval produces correct belief revision; the
   substrate applies supersession semantics exactly (6/6 + 6/6).
4. **find is unsolved by every C1** (best 1/9-4/10 vs substrate 10/10) —
   enumeration requires the closed-world enumeration a store gives you.
5. **⚠ CEILING WARNING (wargame E7 realized early):** c1c composition
   balanced accuracy ≈ 0.86 on this seed. If that holds on the locked
   batch seeds, +15pp is ARITHMETICALLY unclearable (>1.0). Registered
   response stands (E7: c1c is part of strongest-C1; no endpoint change);
   the empirical question moves to scale: the batch preset's worlds are
   larger (80 comp positives, deeper chains) and the corpus-scaling
   experiment (E6) tests whether c1c's 65k-context read-everything
   strategy survives 3×/10× corpora. Token economics already on record:
   c1c spends ~25k tokens/query (20.7k prompt + 4.4k completion) vs the
   substrate's 0 at query time.
6. Thinking-mode ablation answered: reasoning rescues repetition
   (4→29/30) but NOT retrieval-starved composition (rag comp+ 5/40).
   Test-time compute does not substitute for memory organization.

## 2026-07-06 — E3 landed: C2b exists; first real numbers are perfect ON TEMPLATED TEXT (caveat is the headline)

Loom S2 built per spec §5: Extractor interface, DeterministicExtractor
(template-inverse CONTROL), LLMExtractor (schema-prompted strict JSON),
normalization, consistency verdicts (duplicate/refinement/conflict/
supersession-candidate), hygiene gate (safety+stratification trial,
join-explosion dry-run quarantine, firing-ratio 0.9 threshold —
rationale in spec §11), per-episode compilation traces, cmd/fidelity
(P/R + missed/mangled/dropped/hallucinated), conditions loom-c2b-det +
loom-c2b. Quarantine lifecycle states are finally exercised.

Results: (1) pipeline lossless — c2b-det == C2a == oracle on dev seeds
42/7/99 + sample-dataset, det fidelity P=R=1.0. (2) First LLM
compilation ever (seed 42, qwen36 thinking-OFF, cassettes): fidelity
P=R=1.000 on facts/rules/supersessions (310/15/6 items, zero
hallucinated), end-to-end == oracle including find. Compile cost ~175k
tokens for 99 episodes, then 0 LLM tokens per query. Same run: rag-bm25
and c1c floor on composition.

Interpretation discipline: templated text makes extraction near-parsing.
This result proves the ARCHITECTURE (extract→normalize→check→gate→
commit→evaluate) is sound and that a mid-size open model suffices for
schema-conformant extraction. It does NOT yet prove compilation-in-the-
wild; the paraphrase tier is the load-bearing H4 test and is now the
campaign's critical path. Full verification gate over the integrated
tree (all E0 workstreams + E3): green.

## 2026-07-06 — H4 acid test PASSED: compilation survives natural-language paraphrase (seed 1234)

Paraphrase tier built: cmd/paraphrase (LLM rewrite, retry-with-feedback,
whole-episode fallback) + mechanical preservation validator (multiset
equality of identifiers/numbers/atom expressions + policy structural
guard; 8 corruption classes rejected in tests). Paraphraser =
claude-sonnet-5 (outside the evaluated model matrix). Sample-dataset:
99/102 episodes paraphrased, 2.9% fallbacks — tier valid.

Acid test (seed 1234, qwen36 extractor, thinking off):
- DeterministicExtractor (template-inverse CONTROL) on paraphrased text:
  fact recall 0.024, rule recall 0.158 — the paraphrase REALLY destroyed
  the templates. Its collapse is the tier's authenticity certificate.
- LLMExtractor on paraphrased text: **P=R=1.000 on facts, rules, AND
  supersessions; end-to-end oracle-equal on every slice including find
  10/10.** Zero loss from full natural-language variation.

H4 status: extraction fidelity ≥0.9 predicted; measured 1.0 on both
templated and paraphrased text (one seed). Caveats: one seed, one
domain, synthetic prose from one paraphraser. The E4 run on locked
seeds (templated) plus a paraphrase spot-check on 3-5 locked seeds
will finish H4. Repo committed at a899f60 before this entry.

## 2026-07-06 — SEED LIST LOCKED (E0.6 protocol executed)

cmd/batch, batch preset, candidates 1..40 in strict numeric order,
first 20 gate-passers kept. **Locked list: {1,2,3,6,7,8,9,10,12,13,14,
15,16,17,18,19,20,21,22,23}** (23 candidates consumed). Rejects, all
recorded in batch-manifest.json with manifests archived: seed 4
(generation error "rul_002: unbound var C" — under investigation,
fail-loud so no silent corruption of passers pending the
investigation's confirmation), seed 5 (0 composition positives —
revision/composition rule competition, documented in gen/preset.go),
seed 11 (over-firing 6/9 relations). Every kept seed passed the full
DESIGN.md §5 verification in-process. Per-seed: 80 composition
positives, 24 revision flips + 24 retained controls. E4 runs on these
20 datasets and no others; the E4 driver (scripts/e4-run.sh) is already
processing them with cassette-cached qwen36 (C1 thinking-on, C2b
extraction thinking-off).

## 2026-07-07 — H4 VERDICT: text compilation is lossless on every measured seed; plus one live race bug fixed

Paraphrase tier extended to locked seeds 1, 6, 12 (claude-sonnet-5;
fallback rates 2.8%/5.5%/3.7% — all under the 10% validity bar). On the
paraphrased streams, loom-c2b (qwen36 extraction, thinking off) is
ORACLE-EQUAL on every slice of every seed — rep 60/60, comp 80/80, rev
24/24+24/24, find 20/20 — while loom-c2b-det (template-inverse control)
collapses to 1-3/60 repetition, certifying the paraphrases destroyed
the templates. Combined with smoke seed 1234: four datasets, zero
compilation loss on natural-language text. H4 (prediction: fidelity
≥0.9, e2e within 10pp of C2a) is CONFIRMED with margin: measured 1.0/0pp.

Incident during the seed-6 run, worth its own line: harness concurrency
exposed an unsynchronized closure-cache map in loom.Store — Go's
runtime caught it fail-loud (concurrent-map fatal). Fixed with a mutex
(commit d9959b1), -race clean. Policy: every E4 report produced under
concurrency before the fix gets regenerated by cassette replay after
the sweep (zero token cost); the race could crash but the replay
removes even the theoretical torn-read doubt.

## 2026-07-07 — E4 INTERIM PEEK (5/20 seeds): ceiling risk evaporates; provisional PASS

Declared peek, not the verdict (that requires all 20 seeds + post-race-
fix replay regeneration; no tuning follows from this look; endpoints
locked). cmd/aggregate, A=loom-c2b vs B=c1c-longcontext (the strongest
C1 measured), seeds {1,2,3,6,7}:

- Composition balanced acc: C2b 1.0000 on every seed; c1c 0.5278–0.8659
  (mean ~0.62). Paired diff +0.3775, 95% CI [+0.2519, +0.4607] — the
  CI lower bound more than clears the +0.15 kill bar.
- Repetition: non-inferiority satisfied with margin to spare — C2b is
  BETTER (+0.0596, CI [+0.0279, +0.0880]); c1c drops repetition facts
  in 50k-token context (0.89–0.99).
- Revision flip rate diff +0.2267; find micro-F1 diff +1.0 (c1c scores
  ~0 on find).
- The E7 ceiling warning from the easy smoke seed is RESOLVED by data:
  long-context reading degrades sharply on deeper, denser worlds while
  compiled evaluation does not. Wall-clock note: c1c is also ~25× the
  token cost per pass (37.0M vs 1.5M prompt tokens over 5 seeds).

E5 progress in parallel: gpt-5-mini C2b extraction legs at 17/20 seeds,
scores 1.00 flowing (portability of the compile stage across model
families looking clean so far). Haiku legs queued behind it.

## 2026-07-07 — E5 C2b extraction legs (models B/C) + generator fix landed

gpt-5-mini (reasoning minimal) compiled all 20 locked seeds:
**19/20 oracle-equal end-to-end.** Seed 3 decomposed via cmd/fidelity:
facts P=1.000/R=0.994 (4 of 725 missed), rules P=1.000/R=0.971 (1 of 35
missed), zero hallucinations; the end-to-end symptom (find-slice false
positives, micro-P 0.88) traces to missed exception-relevant knowledge
under-blocking derivations. The failure-mode decomposition (§8-E4
machinery) attributes the loss precisely — this is the instrument
working as designed.

Haiku legs v1 were INVALID — usage accounting showed 253/253 API errors
per seed (empty key: AIGENTFLOW_ANTHROPIC_API_KEY exists only in the
session environment, not in ~/code/.creds; the run greped the file).
Caught immediately by the calls/errors columns; rerun with the valid
key in flight. Lesson encoded: the usage table is also the liveness
alarm — a condition with errors==calls is not a measurement.

Haiku rerun complete: **20/20 seeds oracle-equal.** E5 C2b extraction
legs are done — three model families (qwen36 self-hosted, gpt-5-mini,
claude-haiku-4-5) compile the same 20 corpora into substrates of
equal (or in gpt-5-mini's single miss, near-equal) quality. The
compile stage is model-portable in the strongest measurable sense;
combined with the model-free store (transfer = copy + re-point), H6's
extraction-loss surface measures ≈ 0. The C1 swap contrast (5 seeds,
models B/C) remains, plus the economics table.

## 2026-07-08 — E4 FINAL VERDICT (H5): PASS on 20 clean seeds. The compiled-substrate bet is confirmed in v0.

All 20 locked seeds, zero API errors (cleanup convergence pass drove
residual errors to 0), hardened aggregate, registered §1.1 arithmetic.

Primary — **loom-c2b vs c1c-longcontext (strongest real C1)**:
- composition balanced acc  mean **+0.3899**  CI **[+0.3242, +0.4689]**  (kill bar +0.15)
- repetition balanced acc   mean **+0.0657**  CI **[+0.0553, +0.0765]**  (non-inf −0.02; C2b BETTER)
- revision flip rate        mean +0.1699  CI [+0.1025, +0.2544]
- find micro-F1             mean +0.8713  CI [+0.7896, +0.9419]
- **VERDICT: PASS** — composition CI lower +0.3242 ≥ +0.15 AND repetition CI lower +0.0553 ≥ −0.02.

Against every other condition (20 seeds):
- vs rag-bm25: composition +0.4844 [+0.468,+0.496], find +1.000 — PASS.
- vs c0-no-memory: composition +0.4966, repetition +0.4980 — PASS.
- vs d6-perfect-retrieval (CHEATING ceiling, NOT a competitor): composition
  only +0.0478 [+0.022,+0.083] — i.e. given perfect retrieval the LLM nearly
  matches the substrate ON COMPOSITION (confirming composition is
  retrieval-bound, not reasoning-bound) — BUT revision +0.5500 and find
  +0.6163. The one-sentence result of the whole campaign: **even handed the
  exact right episodes, the LLM fails belief revision and enumeration;
  only the compiled substrate does all three.**

Economics (pooled, 20 seeds): c1c-longcontext spent 118.3M prompt +
15.3M completion tokens; loom-c2b spent 0.57M prompt + 0.28M completion
(compile-once), then 0 per query. ~200× prompt-token ratio in the
substrate's favor, and the gap widens with query volume (H7).

What this establishes (honest scope, per CLAUDE.md §1): on the synthworld
instrument, held-out compositional AND revision performance is delivered
by a compiled symbolic substrate that (a) beats the strongest RAG by
>30pp on composition at better repetition, (b) is compiled losslessly
from natural-language text (H4) by three different LLM families (H6
extraction leg), and (c) evaluates with zero model artifacts so a model
swap is lossless by construction. The kill criterion is cleared with the
lower CI bound at 2× the bar. Remaining: E5 C1-swap retention ratios
(5 seeds, data in hand), E6 scaling curve (3× done, 10× generating),
and the paraphrase spot-check already passed (H4). Geometry remains
dormant — v0 succeeded, so Stage-2 must now earn its place against a
winning symbolic baseline, exactly as pre-registered.

## 2026-07-07 — E4 first verdict was FAIL — and it was a MEASUREMENT bug, caught by the discipline

The first full-20 aggregate returned VERDICT: FAIL (repetition
non-inferiority: CI lower −0.0365 < −0.02) while composition PASSED
hugely (+31.7pp, CI lower +24.2pp). Standing order — suspect the harness
first — applied. Cause found in minutes via the usage table: the shared
qwen vLLM box was intermittently overloaded across the sweep, so
LLM-condition queries errored en masse (c1c errored on ALL 20 seeds,
37–347 each; seeds 13/14/15 fully errored on every condition; C2b fully
errored on seeds 12,13,14,15,20). Errored queries are excluded from
tallies, so every affected number was computed over a partial, biased
subset — and two C2b seeds that fully errored scored chance-level,
dragging repetition negative. NOT a substrate result.

This is the diagnostic architecture working exactly as designed: the
verdict tool consumed a poisoned dataset, but the usage/error columns
made the cause obvious and fast. Fixes (commit 2314045): client retries
transient failures (6× exp backoff); cmd/aggregate excludes any
error-bearing seed as NaN with a loud DATA QUALITY warning. Amendment
logged in MASTERPLAN §10 (change made after a verdict → declared).
Endpoint is healthy now (200, ~0.2s); clean re-run in flight (auto mode:
cached successes replay, only errored queries re-hit). Verdict pending
clean data — composition passed regardless; repetition is what the
clean run adjudicates.

## 2026-07-07 — E5 C1 legs complete: every C1 failure mode replicates across all three model families

5 registered seeds, gpt-5-mini (reasoning medium) and claude-haiku
(temp 0), plus qwen36 from E4. Composition balanced accuracy:

- **rag-bm25 ≈ 0.50 (chance) on every family** — with perfect
  repetition (~1.0) and the flips-right-for-wrong-reason signature
  (flips ~24/24, retained ~0/24). Retrieval-based RAG's composition
  failure is model-independent, as the provenance probe predicted.
- **c1c-longcontext**: gpt-5-mini is the strongest C1 measured anywhere
  (mean ~0.75, range 0.59–0.97 — huge seed variance); haiku ~0.62;
  qwen ~0.62. All far below C2b's 1.00; all still over-revise
  (retained 11–22/24).
- **d6-perfect-retrieval**: composition near-ceiling on every family
  (0.97–1.00 gpt-5-mini) — composition is retrieval-bound, not
  reasoning-bound, cross-family. And the revision stickiness finding
  REPLICATES REMARKABLY: with the supersession notice IN CONTEXT,
  flips scored 2–15/24 (gpt-5-mini), 3–18/24 (haiku), 2/6 (qwen smoke)
  — while retained controls stay ~perfect. LLMs across three families
  systematically fail to apply belief revision even when handed the
  revising evidence; the substrate applies it exactly. This may be the
  single most publication-worthy mechanistic finding in the campaign.

Swap-relevant note: the strongest C1 anywhere (gpt5mini-c1c, ~0.75
mean on 5 seeds) still sits ~25pp below C2b's 1.00 — the kill-criterion
margin holds against every model family measured, not just model A.

Generator rewiring bug fixed and committed (b357a33): connectivity
rewiring could displace a conclusion var's sole condition occurrence
(≈14% of batch-preset seeds rejected: 4, 49, 51 of 1..60 sampled).
Confirmed fail-loud-only — a passing dataset can never be corrupted
(oracle ground() + Validate() both gate). Fix consumes no RNG draws;
seed-1234 small still byte-identical to the committed sample-dataset.
Regression test generates the three known-bad seeds and asserts rule
safety.
