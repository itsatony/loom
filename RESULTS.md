# RESULTS — Loom compiled-substrate campaign

*Snapshot 2026-07-08. Source of truth: `CAMPAIGN-LOG.md` (dated, append-only)
and the pre-registration in `MASTERPLAN.md`. All numbers below are on the
20 pre-registered locked seeds unless stated; every LLM cell is
cassette-cached and error-free (the aggregate excludes any seed with an
API error, and all 20 were driven to zero errors before scoring).*

## Headline

The pre-registered kill criterion (MASTERPLAN §7) is **cleared** with the
lower confidence bound at ~2× the bar. On the synthworld instrument, a
compiled symbolic substrate (Loom, C2b — built from natural-language
episode text by an LLM) beats the strongest retrieval baseline on
composition by **+39pp** at **better** repetition, handles belief revision
that no LLM condition does, and transfers across model families with
**zero** loss.

## H5 — the kill criterion (primary endpoint)

**loom-c2b vs c1c-longcontext** (strongest real C1), 20 seeds, 95% seed-level
bootstrap CI (10k resamples):

| endpoint | mean diff (C2b − C1) | 95% CI | bar | result |
|---|---|---|---|---|
| **composition balanced acc** | **+0.3899** | **[+0.3242, +0.4689]** | ≥ +0.15 (CI lower) | **PASS** |
| repetition balanced acc | +0.0657 | [+0.0553, +0.0765] | ≥ −0.02 (non-inf) | PASS (C2b better) |
| revision flip rate | +0.1699 | [+0.1025, +0.2544] | — | — |
| find micro-F1 | +0.8713 | [+0.7896, +0.9419] | — | — |

**VERDICT: PASS.** Composition CI lower bound +0.3242 ≥ +0.15 AND repetition
CI lower bound +0.0553 ≥ −0.02.

## C2b vs every condition (20 seeds, composition balanced-acc diff)

| baseline | composition diff | note |
|---|---|---|
| c0-no-memory | +0.4966 | LLM from question alone (floor) |
| rag-bm25 | +0.4844 | classic episodic RAG; strongest retriever measured (E1) |
| c1c-longcontext | +0.3899 | whole corpus in context; **strongest real C1** |
| d6-perfect-retrieval | +0.0478 | **cheating ceiling, not a competitor** (fed true provenance) |

The D6 row is the mechanistic core: **given the exact right episodes, the
LLM nearly matches the substrate on composition** (composition is
retrieval-bound, not reasoning-bound) — **but D6 still loses revision by
+0.55 and find by +0.62.** Even with perfect retrieval, the LLM fails
belief revision and enumeration; only the compiled substrate does all three.

## H1 — retrieval ceiling (LLM-free, E1)

Semantic embeddings do **not** close the composition gap; BM25 is the
strongest retriever at every k. Frontier embedder (text-embedding-3-large)
≈ 0.6B embedder → the failure is structural (multi-fact chunks blur entity
identity), not a capacity artifact. Revision provenance is lexically and
semantically unreachable (≤1/12 full-coverage, every retriever, every k).

## H2/H3 — the RAG failure is retrieval, and revision breaks everyone

- rag-bm25 composition balanced acc ≈ 0.50 (chance) across all three model
  families; perfect repetition; flips-right-for-wrong-reason signature.
- D6 (perfect retrieval) composes at ~0.97–1.00 → the deficit is retrieval.
- **Revision stickiness replicates across qwen36, gpt-5-mini, claude-haiku**:
  with the supersession notice IN CONTEXT, flip accuracy is 2–18/24 while
  retained controls stay near-perfect. LLMs do not apply belief revision
  from evidence in front of them; the substrate applies it exactly.

## H4 — compilation is lossless, even from natural language

- Deterministic template-inverse control (`c2b-det`) == C2a == oracle on all
  dev + locked seeds → the pipeline is lossless by construction.
- LLM extraction (qwen36) on **templated** text: fidelity P=R=1.0.
- LLM extraction on **claude-sonnet-5-paraphrased** text (templates
  provably destroyed — det control collapses to ~2% recall): fidelity
  P=R=1.0 and oracle-equal end-to-end on 4 seeds. The "it's just parsing
  templates" objection is empirically dead.

## H6 — portability (the "value" claim)

Same 20 corpora compiled by **three model families**:

| extraction model | C2b composition balanced acc (mean, 20 seeds) | transfer retention |
|---|---|---|
| qwen36-nvfp4 (self-hosted) | 1.0000 | — (reference) |
| gpt-5-mini | 1.0000 | 1.000 |
| claude-haiku-4-5 | 1.0000 | 1.000 |

**All-three-oracle-equal on composition: 20/20 seeds.** By contrast the C1
long-context baseline is model-*sensitive* (composition 0.62 / 0.75 / 0.62
for qwen / gpt-5-mini / haiku on the shared seeds). The substrate stores no
model artifacts, so a model swap is lossless by construction; the data
confirms it end-to-end.

## H7 — economics

Pooled over 20 seeds (query-time cost, prompt tokens):

| condition | prompt tokens | completion tokens | per-query cost |
|---|---|---|---|
| c1c-longcontext | 118,342,729 | 15,282,002 | grows with corpus |
| loom-c2b | 565,942 (compile-once) | 278,984 | **0 at query time** |

~**200×** fewer prompt tokens, and the ratio widens with query volume: the
substrate pays once at ingest, RAG pays every question forever.

Corpus scaling (E6): tiers at 1× (≈35k tokens), 3× (≈86k), 10× (≈260–284k,
~2000 episodes). **3× and beyond exceed the 65k context window → c1c is
physically impossible**, the honest endgame of read-everything. At 10×,
oracle == C2a == c2b-det stay perfect (the substrate scales with zero
degradation to 8× the corpus and 4× the window), while BM25 composition
full-coverage falls to 0/100 and revision to 0/48 — retrieval coverage
collapses as the corpus grows.

## Scope and honesty (per CLAUDE.md §1)

"Transfer of understanding" here means exactly: held-out compositional and
revision performance survives a model swap. That is demonstrated. What is
NOT claimed: rule *induction* (v0 compiles only stated rules), real-domain
noise robustness beyond the paraphrase tier, or that geometry is
unnecessary — geometry is a pre-registered Stage-2 question that must now
beat a *winning* symbolic baseline (the frame concept, §9.5, is the first
registered Stage-1.5 extension). Negative-result discipline held throughout:
the one FAIL verdict during the campaign was a flaky-endpoint measurement
artifact, caught by the usage/error instrumentation, fixed at the client and
analysis layers, and re-run to clean data before this verdict stood.
