# synthworld

Synthetic hyper-relational world generator + exact oracle, in pure Go (stdlib
only). This is the **measurement instrument** for the compiled-substrate
experiment: it produces worlds whose ground truth is computable, episode
streams an agent experiences over time, and query sets that separate the
three capabilities the substrate bet depends on:

| slice | what it measures | who should win |
|---|---|---|
| **repetition** | recall of facts stated verbatim in one episode | RAG ties; a substrate losing here destroys information at compile time |
| **composition** | combining k ≥ 2 items from *different* episodes, never co-stated | the go/no-go slice: compiled substrate must beat episodic RAG here |
| **revision** | later episodes superseded earlier beliefs (+ retained controls against over-revision) | substrate near-perfect; RAG retrieves stale and fresh with equal confidence |

The system under test only ever sees `episodes.jsonl`. `world.json` and query
ground truth exist for scoring.

## Quickstart

```bash
go build ./...
go run ./cmd/synthgen -seed 42 -out dataset -preset small   # or medium | batch
go run ./cmd/validate -dir dataset                          # re-verifies every guarantee
go run ./cmd/batch -out batch-out -preset batch -candidates 40 -keep 20   # locked seed list
```

Presets: `small` (the committed sample-dataset scale), `medium` (larger world),
`batch` (medium-scale world tuned so each seed carries ≥ 20 revision flips and
≥ 20 retained controls — the statistical floor for per-condition revision
analysis). Existing presets are frozen: same seed + same preset + same binary
= byte-identical dataset, forever.

## What gets generated

- `world.json` — full world: typed entities, n-ary time-scoped facts,
  stratified Horn rules with exceptions/authority/validity, supersessions.
  **Scoring only — never shown to the system under test.**
- `episodes.jsonl` — chronological episodes; each event carries structured
  payload (easy mode) and templated text (hard mode).
- `queries.jsonl` — oracle-verified items: slice, holds/find, answer,
  stale answer (revision), derivation depth, provenance episodes, and a
  replayable trace.
- `manifest.json` — config, counts, and **quality stats**: firing ratios per
  derived relation, closure depth histogram, over-firing relations.

## Quality gates for batch generation

Seeds vary in quality. When generating datasets at scale, gate on the
manifest: reject seeds with `over_firing_relations` covering most derived
relations, or a closure depth histogram without d2+ mass, or composition
positives below your per-slice minimum. The generator already self-repairs
(tightens over-firing rules, re-seeds dead ones, recovers from join
explosions) — the gates catch the residue.

`cmd/batch` automates this as a pre-registered protocol: it tries candidate
seeds strictly in numeric order, verifies every guarantee in-process (same
code as `cmd/validate`), applies the gates above (composition positives ≥ 30,
revision flips ≥ 20 AND retained ≥ 20, d2+ closure mass, over-firing not a
majority), and keeps the FIRST `-keep` passers. The seed list is fixed by
protocol, not chosen — rejected seeds' manifests are archived in
`rejected-manifests/` and every verdict lands in `batch-manifest.json`, so
nobody can quietly re-roll unlucky seeds. Occasional seeds generate slowly
(minutes, not seconds): dense worlds work the oracle's join guard; let them
finish.

## Layout

- `DESIGN.md` — the semantics contract (world model, oracle precedence,
  slice construction, validator guarantees). Read this first.
- `world/` — schema + structural validation (typed slots, safe rules,
  stratification).
- `oracle/` — exact stratified evaluation: closure at time t, precedence
  (authority → recency → specificity → ID), exceptions, conditional
  supersession, derivation traces, stale closure. Join guard fails loudly
  on intractable worlds.
- `gen/` — seeded generator: world construction with enforced condition
  connectivity, chain seeding, quality repair loop, revision pairs, episode
  chunking, query synthesis with oracle verification.
- `cmd/synthgen`, `cmd/validate` — CLI + independent re-verification.
- `cmd/batch` — pre-registered seed-selection protocol (generate → verify →
  gate → lock the first N passers).

## Determinism

Everything flows from `-seed`; map iteration is always over sorted keys. Same
binary + same seed = identical dataset.

## Known limitations (v0.1, deliberate)

Single evaluation checkpoint (schema supports `at_day`), assert-only rules
(block polarity in schema+oracle, generation deferred), templated text only,
Euclidean-crisp semantics (no graded truth). See DESIGN.md §6.

## Paraphrase tier (cmd/paraphrase)

Hard mode for hard mode: `go run ./cmd/paraphrase -dir <dataset>` rewrites
every episode text line into varied natural business prose with an LLM
(PARAPHRASE_LLM_* env; the paraphraser must be OUTSIDE the evaluated model
matrix) while a mechanical validator guarantees ground truth survives —
identifiers case-sensitively verbatim, numbers as digits with repetition
counts, `name(slot=value, ...)` expressions untouched, conditional and
exception structure preserved. Failing episodes keep their original text
(counted; >10% fallbacks invalidates the tier). Evaluate any condition on
the result via `cmd/harness -episodes episodes_paraphrased.jsonl` (same
flag on cmd/fidelity). This is the robustness test behind the templated
results: template-inverse parsing collapses on it by design; only genuine
extraction survives.

## Harness (cmd/harness)

Runs memory conditions against a dataset and scores per slice. Ships with
diagnostic conditions that prove the instrument discriminates — no LLM
involved:

```
condition             rep+      rep-     comp+     comp-  rev.flip   rev.ret
always-true          30/30      0/28     40/40      0/33       0/6       6/6
always-false          0/30     28/28      0/40     33/33       6/6       0/6
episode-grep         30/30     28/28      0/40     33/33       6/6       0/6
stale-oracle         30/30     28/28     40/40     33/33       0/6       6/6
oracle               30/30     28/28     40/40     33/33       6/6       6/6
```

Read this table before trusting any real number: grep (retrieval, no
inference) aces repetition and fails every composition positive; the stale
oracle fails exactly the revision flips; grep's 6/6 flips with 0/6 retained
shows why retained controls exist — it answers false to everything derived
and gets flips right for the wrong reason. Real conditions (C1 RAG, C2
substrate, C3 LoRA) implement `harness.Condition`; conditions only ever see
sanitized queries (no answers, traces, provenance, or slice labels).

LLM-backed conditions self-wire when `HARNESS_LLM_BASE_URL` is set
(`HARNESS_LLM_MODEL`, `HARNESS_LLM_KEY`, `HARNESS_RAG_K`,
`HARNESS_TMR_BIN/_DIR/_MODES` as documented in `cmd/harness/main.go`):

- `c0-no-memory` — the floor: LLM answers from the question alone, zero
  episodes. Any memory condition scoring below C0 has negative memory value.
- `rag-<retriever>` — C1: retrieve top-k episode texts, LLM reasons over
  them. Retriever selected by `HARNESS_RAG_RETRIEVER=bm25|embed|hybrid|tmr`
  (default: `tmr` if `HARNESS_TMR_BIN` is set, else `bm25`).
- `c1c-longcontext` — C1c: every episode concatenated into the context, no
  retrieval. The honest long-context competitor; part of "strongest C1".
- `d6-perfect-retrieval` — **diagnostic, not a competitor**: the RAG prompt
  fed exactly the query's true provenance episodes. Separates C1's retrieval
  failure from its reasoning failure; upper-bounds any retrieval-based C1.

Semantic retrieval wires in via an OpenAI-compatible embeddings endpoint:
`HARNESS_EMBED_BASE_URL/_MODEL/_KEY`, plus a mandatory embedding cassette
cache `HARNESS_EMBED_CACHE=<dir>` (`HARNESS_EMBED_CACHE_MODE=replay` makes
misses a loud error instead of a network call). `embed-<model>` ranks by
cosine; `hybrid-bm25-embed` fuses BM25 + embeddings with RRF (c=60).

Request shaping for model-family quirks: `HARNESS_LLM_TEMPERATURE` (float,
or `none` to omit the field — the gpt-5 family rejects it; unset =
`temperature:0`) and `HARNESS_LLM_EXTRA_PARAMS` (raw JSON object merged
top-level into every chat request, e.g. `{"reasoning_effort":"minimal"}` or
`{"chat_template_kwargs":{"enable_thinking":false}}` for vLLM Qwen;
invalid JSON fails fast). The cache key hashes the exact request payload,
shaping included — an omitted field is absent, not zero.

LLM calls support record/replay cassettes for deterministic, offline-
reproducible runs: `HARNESS_LLM_CACHE=<dir>` enables the cache,
`HARNESS_LLM_CACHE_MODE=auto|record|replay` (default `auto`; `replay` never
touches the network and errors loudly on a miss — with `replay` set, LLM
conditions run without `HARNESS_LLM_BASE_URL` entirely from cassettes).
Cassettes carry token usage; each LLM condition is individually metered and
the harness prints a spend table (spent = live network tokens, replayed =
cassette tokens) — the raw material for the H7 economics analysis.
`HARNESS_CONCURRENCY=<n>` parallelizes query answering with an n-worker
pool; reports are identical for any worker count (answers are collected,
then scored sequentially in query order).
The provenance probe always runs over BM25, over `embed-*`/`hybrid-*` when
the embedding env vars are set, and over tmr (modes from
`HARNESS_TMR_MODES`, default `semantic,hybrid`) when the tmr env vars are
set.

## Substrate spec

`docs/loom-substrate-v0-spec.md` — the compiled-memory service this
instrument exists to measure: data model, compilation loop (extraction →
normalization → consistency → rule handling → hygiene gate), operation API,
portability contract, failure-mode decomposition, milestones, and
pre-registered kill criteria.

## Where this sits in the roadmap

1. **synthworld** — the instrument. ✅
2. **Loom S1** (`loom/`) — store + lifecycle + provenance + schema inference
   + structured ingest + oracle-backed operations. **C2a is strictly
   oracle-equal on 5/5 seeds** (`cmd/harness` prints it as `loom-C2a`). ✅
3. **C1 adapters** — `harness.Retriever` + `harness.LLMClient`
   (OpenAI-compatible; set `HARNESS_LLM_BASE_URL/_MODEL/_KEY`, optionally
   `HARNESS_TMR_BIN/_DIR` after `cmd/memoexport` + `tmr ingest`). BM25
   retriever runs anywhere; the **provenance probe** (LLM-free) already
   bounds RAG: composition full-coverage 8/50 @k=16, revision 0/12 — the
   supersession notice is lexically unreachable from the question. ⏳ live
   LLM runs on vAI infra.
4. **Loom S2** — text-mode compilation (extraction → normalization →
   consistency → hygiene gate), behind the same Commit path (`loom/compile.go`).
   The C2b − C2a gap is the price of extraction. Two harness conditions:
   `loom-c2b-det` (template-inverse extractor — a pipeline CONTROL: its
   oracle-equal scores prove the compile path lossless, never the thesis)
   and `loom-c2b` (LLM extractor, env-gated like the other LLM conditions;
   extraction is cassette-cached and metered — it is the compile-once cost).
   `cmd/fidelity -dir <ds> -extractor det|llm` scores compilation fidelity
   against world.json (scoring only): P/R per item type plus the
   missed / mangled / dropped / hallucinated decomposition, `-trace` dumps
   the per-episode compilation trace. The seeded relation vocabulary
   (spec §4: relation IDs, names, slot names — never facts/rules/entities)
   is injected by cmd/harness from the dataset's relation table. ✅
   (verified: `loom-c2b-det` == `loom-C2a` == oracle on dev seeds 42/7/99;
   det fidelity P=R=1.0 on all types)
5. **Frames v1** — frame-relative truth (fiction / scenario / perspective,
   promotion, pinning) on top of S1/S2, behind the same Commit path
   (`loom/compile.go`, `harness/frames_conditions.go`, `harness/frame_rag.go`).
   Pre-registration and full log: `MASTERPLAN.md` §9.6 + §10. ✅ (see below)

## Frames v1 — results (20 locked seeds, tier-M certified corpus)

Pre-registered endpoints (`MASTERPLAN` §9.6.7; F-E2 re-specified onto
*filterability* and ratified §10 2026-07-18). Governing verdict via
`cmd/aggregate -frames -a loom-c2b-frames -b c2b-prov -b2 frame-rag`
(seed-level bootstrap, 10k resamples, RNG 42). Primary extractor gpt-5-mini
@ reasoning high with the leakage-fix pipeline (frame-context two-pass +
§9.6.1 confidence-gated actual-quarantine); the `frame-rag` **ceiling null**
is the strongest query-time opponent (frameless RAG over full episode text +
frame semantics + a frontier reasoner).

| endpoint | result | headline number |
|---|---|---|
| **F-E1** safety (both directions) | **PASS** | contamination 0.999, isolation 0.996 (CI-lo ≥ 0.99) |
| **F-E2** superiority over the null | see note | filtering-resistant **+0.32 CI-lo vs the ceiling null** (>2× the 15pp bar) |
| **F-E3** frame-assignment fidelity | strong | fiction→actual leakage **0.000** (down from 0.19); macro-F1 ~0.92 |
| **F-E4** cross-frame ideation | **PASS** | micro-F1 1.000 |

**Bottom line (accepted reading, §10 2026-07-19):** the compile-time-frames
bet is **not falsified** — the kill trigger (superiority < 15pp) is nowhere
near, and query-time filtering is decisively refuted even against a frontier
ceiling reasoner. F-E1/F-E4 pass; F-E2's two prior non-inferiority failures
(filter-decidable, metadata-cued) are fixed by the leakage-fix extraction.
The pre-registered F-E2 co-primary is reported as a **near-pass**: it trips
only on the **v0-composition** non-inferiority leg (CI-lo −0.03), driven by a
single seed's mis-extracted rule (frame-free extraction variance, unrelated
to frames, leakage, quarantine, or architecture; 18/20 seeds fine). Honest
negative on that one leg; the frames thesis is strongly supported. Exploratory
first leg (free qwen) and the frontier gpt-5 confirmatory leg are retained in
`results/frames-e-*` and logged in §10.

### Portability / swap (H6 for frames — Leg A, LLM-free)

Because C2b answering is the deterministic op-planner, an LLM swap touches
**only the extraction surface** (answering-swap retention = 1.000 by
construction). Extraction portability over **three independent families** —
OpenAI (gpt-5), Alibaba (qwen36), Anthropic (claude-haiku-4-5) — vs the accepted
gpt-5-mini store, 20 locked seeds, via `cmd/aggregate -swap`
(`retention = perf_B/perf_A`, registered §10 2026-07-19):

| slice group | gpt-5 | qwen36 | haiku-4-5 | reading |
|---|---|---|---|---|
| **logical** (rep, comp, revision, find) | ≥0.99 | ≥0.98 | ≥0.99 | H6 as registered — **PASS** |
| frame-homing (contam/iso/pin/promo) | 0.92–1.00 | 0.91–0.98 | **0.97–1.00** | portable |
| misattribution-F1 / ideation-F1 | 0.86–0.93 | 0.88–0.93 | **1.00 / 0.997** | see note |

H6's registered subject — *compositional + revision performance survives a model
swap* — passes decisively on all three families (extraction-swap loss ≈ 0 on
logical slices). **Leg C is decisive on frame homing:** Claude haiku-4-5, a
family that never touched the corpus, extracts at/near ceiling on every slice
(min retention 0.966) with **perfect frame homing** (misattribution 1.000) —
proving the gpt-5/qwen misattribution dip is *model-specific extraction
variance, not a cross-family ceiling*. **Leg B (answering-swap contrast,
gpt-5 → qwen36):** the LLM-bound baselines are model-sensitive (frame-rag
retention 0.89–1.08 with a strong second reasoner, 0.57–0.81 under a reduced-
compute one) whereas C2b answering is model-independent at 1.000 — no reasoner,
no per-query cost, nothing to degrade. Full analysis + the H6-band
recommendation in `results/frames-swap/RESULTS.md`.
