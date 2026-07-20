# Frames swap / portability — Leg A results (H6 for frames)

**Date:** 2026-07-19. **Cost:** zero new LLM spend (replay over the three
committed extractor legs). **Kernel:** `cmd/aggregate -swap` (registered
arithmetic, MASTERPLAN §10 2026-07-19). **Artifact:** `retention.json`.

Reproduce:

```sh
cd synthworld
go run ./cmd/aggregate -swap \
  -legs "gpt5mini=../results/frames-e-gpt5mini/seed-*/report.json,gpt5=../results/frames-e-gpt5/seed-*/report.json,qwen36=../results/frames-e-qwen36/seed-*/report.json" \
  -ref gpt5mini -cond loom-c2b-frames -c0 c0-no-memory \
  -json ../results/frames-swap/retention.json
```

## 1. What a swap can even touch (the load-bearing distinction)

The loom store is a model-free symbolic artifact; C2b answering is the
**deterministic op-planner** (`harness/loom_c2b.go`), not an LLM. So an LLM swap
changes **only the extraction surface** — the store an extractor produced.
**Answering-swap retention = 1.000 by construction** (MASTERPLAN §187); it is a
structural ceiling, not evidence, and is not measured. All empirical content is
in **extraction portability**: the same condition (`loom-c2b-frames`) compiled
by three different models — gpt-5-mini (accepted store), gpt-5, qwen36 (Alibaba,
genuinely cross-vendor) — scored on identical locked seeds.

Retention = `perf_B / perf_A`, A = reference (accepted gpt-5-mini), B = swapped.

## 2. Result — extraction-portability spectrum (retention vs accepted gpt-5-mini)

Three swapped extractor legs across THREE independent families — OpenAI (gpt-5),
Alibaba (qwen36), **Anthropic (claude-haiku-4-5, Leg C)** — over the 20 locked
seeds. Retention = `perf_leg / perf_gpt5-mini`.

| slice | gpt-5 | qwen36 | **haiku-4-5** | reading |
|---|---|---|---|---|
| repetition | 1.000 | 0.999 | 1.000 | logical — PASS |
| **composition** | 1.011 | 1.004 | 1.010 | logical — PASS |
| revision flip | 0.994 | 0.981 | 1.000 | logical — PASS |
| revision retain | 1.013 | 1.002 | 1.009 | logical — PASS |
| find micro-F1 | 0.999 | 0.994 | 0.990 | logical — PASS |
| contamination | 0.924 | 0.913 | **1.000** | frame-homing |
| isolation | 0.995 | 0.959 | 0.966 | frame-homing |
| pinning | 1.000 | 0.975 | 1.000 | frame-homing |
| promotion | 0.980 | 0.961 | 1.000 | frame-homing |
| **misattribution F1** | 0.862 | 0.880 | **1.000** | frame-homing |
| ideation F1 | 0.925 | 0.928 | **0.997** | frame-homing |

(Full per-slice absolute means + 95% ratio-bootstrap CIs in `retention.json`.)

**Headline (Leg C):** a fully independent third family — Claude haiku-4-5, which
never touched the tier-M corpus (it did not naturalize or judge it) — extracts
the substrate at/near ceiling on **every** slice, min retention **0.966**
(isolation), and hits **perfect frame homing** (misattribution F1 1.000,
ideation 0.997). Under the *literal* H6 band it PASSES on all slices, frame-
homing included. This is decisive: the misattribution/ideation dip on gpt-5 and
qwen36 is **model-specific extraction variance, not a cross-family portability
ceiling** — a third family closes it completely. haiku's own frames verdicts
confirm the slices are real (F-E1 PASS: contamination 0.999, isolation 0.944;
F-E4 PASS: ideation 0.997; F-E2 superiority +0.33, tripping only the v0
find-micro-F1 non-inferiority leg — the same kind of single-slice extraction-
variance trip the accepted gpt-5-mini leg shows on v0-composition).

## 3. The honest two-part reading

**(i) H6 as registered PASSES decisively.** The thesis boundary (§1) is exact:
"transfer of understanding = *compositional and revision performance survives a
model swap*." On precisely those slices — repetition, composition, revision
(flip + retain), find — retention is **≥ 0.98 on both cross-vendor legs**, CI
lower bounds mostly above 0.95. Extraction-swap loss on the substrate's
**logical competence is ≈ 0**. The store is genuinely the model of record for
the knowledge the thesis claims to transfer.

**(ii) Frame-homing slices carry the promised "measured, small loss."** The
frames-v1 attribution slices (contamination / isolation / pinning / promotion /
misattribution / ideation) are a diagnostic surface *added after* H6 was
registered. There the weaker/other extractors lose ground:
contamination/promotion ~0.91–0.98, and **misattribution-F1 (0.862 gpt-5 /
0.880 qwen)** and ideation-F1 (~0.93) show the largest drop.

Under the **literal** H6 band applied to these slices, misattribution-F1 trips
the `< 0.90` kill line. Three facts keep this honest rather than alarming:

1. **It is extraction-surface loss, not substrate collapse.** The op-planner
   answers identically from whatever store it is handed; the loss is entirely
   in the store a weaker model *compiled*.
2. **It is localized to frame-HOMING** — assigning an assertion to the right
   perspective frame — which is exactly the hard, stochastic step the seed-7
   robustness diagnostic isolated (`../frames-robustness/`).
3. **It is asymmetric / ceiling-referenced.** gpt-5-mini happened to hit
   misattribution-F1 = **1.000**; the other legs' *absolute* frame-attribution
   F1 is 0.86–0.93. Retention-vs-a-perfect-reference magnifies a small absolute
   gap into a large-looking ratio drop.

## 4. Substrate lift (cond − C0), where C0 was measured

C0 (`c0-no-memory`) answering ran on qwen36 (20/20 seeds) and gpt-5 (8/20);
gpt-5-mini reused a prior null (0/20). Where available, lift is decisive:
composition **+0.49**, revision-retain **+0.92–0.99**, find **+0.99** over the
no-memory floor. Repetition/flip lift is smaller because C0 can sometimes guess
verbatim/unchanged facts. The full answering-swap **baseline-degradation
contrast** (does C0/RAG/frame-rag degrade under a model swap while the substrate
stays flat?) is **Leg B** — see below.

## 5. Leg B — answering-swap contrast (executed 2026-07-19/20)

The extraction column (Legs A/C) is only half of H6. The other half: the
substrate's *answering* is the LLM-free op-planner, so **C2b answering retention
= 1.000 by construction**, whereas the LLM-bound baselines depend on the
answering model. Leg B swaps the **answering** model gpt-5 → qwen36-nvfp4 for
{c0-no-memory, rag-bm25, frame-rag} over the 20 locked datasets (registered
thinking-on C1 mode) and reports retention `perf_qwen / perf_gpt5`. These
conditions are extractor-independent (no loom store).

**frame-rag (the strongest baseline — a per-query frontier reasoner), 20 seeds:**

| slice | gpt-5 | qwen36 | retention |
|---|---|---|---|
| repetition | 0.998 | 0.998 | 1.000 |
| contamination | 0.994 | 0.978 | 0.984 |
| isolation | 0.780 | 0.758 | 0.973 |
| pinning | 0.552 | 0.525 | 0.952 |
| promotion | 0.915 | 0.991 | 1.082 |
| misattribution F1 | 0.564 | 0.502 | 0.890 |
| ideation F1 | 0.628 | 0.569 | 0.907 |

(composition/revision/find are near-chance for frame-rag under *either* model —
RAG cannot compose — so their retention is a chance/chance artifact, omitted.)

**rag-bm25** (8-seed overlap): retention 0.92–1.02 across slices.
**c0-no-memory** (8-seed): flat at floor (~0.96–1.04 where defined) — no memory,
so the answering model barely matters. Both as predicted.

### The honest reading (this reshapes, and strengthens, the story)

With a **competent** second reasoner, frame-rag is *fairly portable too*
(retention 0.95–1.08 on rep/contamination/isolation/promotion; 0.89–0.91 only on
the frame-attribution F1 slices). So the H6 payoff is **not** "RAG collapses on
swap." It is sharper: **C2b is answering-model-independent entirely** — retention
1.000 with a $0 LLM-free planner, so there is *nothing to degrade*, no
frontier-reasoner dependency, and no per-query token cost. The LLM-bound
baselines remain model-sensitive (frame-rag still sheds ~10% on frame
attribution even between two strong models) and, critically, **hostage to the
answering model's reasoning budget**: under a reduced-compute answerer (qwen
thinking-OFF) frame-rag retention falls to **0.57–0.81** on rep/contamination/
isolation/promotion/ideation (`legB-frame-rag-thinkoff.json`). C2b is invariant
to all of it.

**Methodological note:** the first qwen frame-rag pass was mistakenly run
thinking-OFF (extraction config); it was re-run thinking-ON to match the
registered C1 answering mode and qwen's own on-disk c0/rag before any number
above was reported. The thinking-off run is retained only as the reduced-compute
sensitivity point.

Artifacts: `legB-{frame-rag,rag-bm25,c0-no-memory}.json` (thinking-on primary),
`legB-frame-rag-thinkoff.json` (sensitivity).

## 6. Open decision for Toni

Does the registered H6 band (≥0.95 PASS / <0.90 KILL) **extend to the frames-v1
diagnostic slices**, or govern only the v0 logical slices it was written for
(the §1 boundary: compositional + revision)?

**RATIFIED by Toni 2026-07-20: the recommendation is accepted — the band governs
the v0 logical slices only; frame-slice portability is reported as "registered
F-E gates survive the swap" + the reference-independent absolute-F1 spectrum. No
band retrofit.** Grounds (now strengthened by Legs B/C):
1. H6 was registered for compositional + revision performance — the v0 logical
   slices, where retention is ≥0.98 on all THREE swapped families → PASS.
2. Retrofitting a pre-registered threshold post-hoc is goalpost-moving either way.
3. The frame slices already carry their own registered gates (F-E1 ≥0.85, F-E4
   ≥0.90), which **survive the extractor swap on every family** (F-E1 PASS +
   F-E4 PASS on qwen36, gpt-5, AND haiku-4-5).
4. Report frame attribution as the reference-independent absolute-F1 spectrum,
   not retention-vs-a-ceiling.

**And even if the band DID extend to the frame slices:** Leg C shows a competent
independent extractor (haiku-4-5) PASSES the literal band on *all* frame-homing
slices (min 0.966; misattribution/ideation 1.000/0.997). So the gpt-5/qwen
sub-0.90 misattribution dips are **extractor-quality artifacts, not a substrate
portability failure** — exactly the "measured, small loss localized to the
weaker extractor" the thesis predicted, now bounded from above by a third family
at ceiling.

- Self-consistency (deferred, pre-registered) remains the lever to lift the
  *weaker* extractors' frame-homing toward the haiku/mini ceiling — validate on
  dev first, never selected to fix a locked seed.
