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

## 2. Result (20 locked seeds; retention vs the accepted gpt-5-mini store)

| slice | gpt-5 ret | qwen36 ret | reading |
|---|---|---|---|
| repetition | 1.000 | 0.999 | logical — PASS |
| **composition** | 1.011 | 1.004 | logical — PASS |
| revision flip | 0.994 | 0.981 | logical — PASS |
| revision retain | 1.013 | 1.002 | logical — PASS |
| find micro-F1 | 0.999 | 0.994 | logical — PASS |
| contamination | 0.924 | 0.913 | frame-homing |
| isolation | 0.995 | 0.959 | frame-homing |
| pinning | 1.000 | 0.975 | frame-homing |
| promotion | 0.980 | 0.961 | frame-homing |
| **misattribution F1** | **0.862** | **0.880** | frame-homing — trips literal <0.90 |
| ideation F1 | 0.925 | 0.928 | frame-homing |

(Full per-slice absolute means + 95% ratio-bootstrap CIs in `retention.json`.)

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
stays flat?) is **Leg B** — deferred, real LLM spend.

## 5. Open decision for Toni

Does the registered H6 band (≥0.95 PASS / <0.90 KILL) **extend to the frames-v1
diagnostic slices**, or govern only the v0 logical slices it was written for
(the §1 boundary: compositional + revision)? This is a registration
interpretation, **not decided unilaterally**. Leg A reports both readings and
does **not** claim a clean frames-H6 pass on the frame-homing slices.

- If Leg B / self-consistency wanted: same lever as the robustness diagnostic —
  K=3–5 self-consistency frame-homing is the pre-registered candidate to lift
  the frame-homing retention toward ceiling (validate on dev first, never
  selected to fix a locked seed).
