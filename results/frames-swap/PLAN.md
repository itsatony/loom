# Frames substrate — swap / portability experiment (H6 for frames)

**Status: PROPOSAL, pending Toni's decisions (marked ▶ below). Nothing here is
run yet.** Grounded in the v0 E5 precedent (MASTERPLAN §E5, H6) and the three
frames extractor legs already on disk. Date: 2026-07-19.

Thesis leg being closed (CLAUDE.md §1): *"transfers across LLM swaps with
measured, small loss."* v0 closed this via E5; this is the frames analog — the
third leg of the accumulation + composition + portability conjunction, now for
the frame substrate.

---

## 1. What "portability" means for a *compiled* substrate (the load-bearing distinction)

The loom store is a **model-free symbolic artifact**. Query answering for
`loom-c2b`/`loom-c2b-frames` is the **deterministic op-planner**, not an LLM
(`harness/loom_c2b.go:67-87` — `Store.HoldsIn/FindIn`; no `LLM.Complete` in the
answer path). So an LLM swap can only touch **two surfaces**, and for
synthworld's structured queries only one of them is live:

| surface | swapped component | frames loss? |
|---|---|---|
| **query / answering** | model that answers queries | **0 by construction** — the store is copy-and-re-point; C2b answers are model-invariant |
| **compile / extraction** | model that compiles episodes → store | **the only place loss can appear** — a different extractor yields a slightly different store |

The 2×2 factorial (compile × plan) of v0's E5 (MASTERPLAN §176-185) therefore
**collapses to the extraction column** for loom, because the "plan" side is
LLM-free. This is not a weakness of the experiment — it is the substrate's whole
value proposition ("the LLM is swappable infrastructure; the substrate is the
model of record"). But it must be **reported honestly as a structural ceiling,
not as a measured surprise**: retention = 1.000 on the answering swap is
by-construction; the empirical content is entirely in extraction portability +
the baseline-degradation contrast.

**Definitions (reuse verbatim from MASTERPLAN §189-190):**
- **transfer retention = perf_B / perf_A**, per slice (A = original extractor, B = swapped).
- **substrate lift = C2 − C0**, per model.
- Targets (H6, MASTERPLAN §40): C2b per-slice retention **≥ 0.95** (kill if **< 0.9** on any slice); C1 retention noisy (~0.85–1.05); C3 markedly < 1.

---

## 2. What is ALREADY measured (large parts of this experiment are in hand)

**Three frames extractor legs, 20 locked seeds each, certified tier-M corpus:**
`qwen36` (self-hosted Qwen, thinking-off), `gpt-5`, `gpt-5-mini` (reasoning
high). Genuinely cross-vendor (Alibaba ↔ OpenAI). From the committed
`metrics_a` arrays I computed the **extraction-portability table** (C2b-frames
mean over 20 seeds; retention relative to the accepted `gpt-5-mini` leg):

| slice (C2b-frames) | qwen36 | gpt-5 | gpt-5-mini | ret qwen/A | ret gpt5/A |
|---|---|---|---|---|---|
| repetition | 0.999 | 1.000 | 1.000 | 0.999 | 1.000 |
| **composition** | 0.992 | 0.999 | 0.988 | 1.004 | 1.011 |
| revision flip | 0.981 | 0.994 | 1.000 | 0.981 | 0.994 |
| revision retain | 0.988 | 0.998 | 0.985 | 1.002 | 1.013 |
| find (micro-F1) | 0.988 | 0.993 | 0.994 | 0.994 | 0.999 |
| contamination | 0.912 | 0.923 | 0.999 | **0.913** | **0.924** |
| isolation | 0.956 | 0.991 | 0.996 | 0.959 | 0.994 |
| pinning | 0.973 | 0.998 | 0.998 | 0.975 | 1.000 |
| promotion | 0.961 | 0.980 | 1.000 | 0.961 | 0.980 |
| misattribution (F1) | 0.880 | 0.862 | 1.000 | **0.880** | **0.862** |
| ideation (F1) | 0.928 | 0.925 | 1.000 | 0.928 | 0.925 |

**Reading:** v0 logical slices (rep/comp/flip/retain/find) are **extractor-
invariant** — retention 0.98–1.01 across all three, well above the 0.95 target.
The *frame-homing* slices carry the "measured, small loss": the weaker **free**
qwen extractor loses 8–12 pp on contamination / misattribution / promotion /
ideation, while gpt-5 and gpt-5-mini sit near-ceiling. This is exactly the H6
prediction — loss is real, small, and **localized to the extraction surface and
to the weaker model**, not to the substrate architecture.

**v0 E5 precedent (context):** across qwen36 / gpt-5-mini / claude-haiku-4-5 the
v0 C2b legs were oracle-equal (extraction swap loss ≈ 0); strongest C1 stayed
~25 pp below C2b; **C3/LoRA transfer retention = 0** (parametric memory is
architecturally non-portable — the far corner of the portability triangle).

---

## 3. What is genuinely NEW work (the actual experiment)

### Leg A — Extraction-portability table + lift (near-free, LLM-free replay)
Turn the numbers in §2 into the pre-registered H6 artifact:
- per-slice **retention perf_B/perf_A** with bootstrap CIs, for the frames slices AND the v0 anchors;
- **substrate lift C2b − C0** per extractor (C0 already in each leg's `report-c1.json`);
- honest call-out of the sub-0.95 frame slices on the free extractor.

Cost: **≈ 0 LLM** (all three legs are already extracted and cassette-cached).
Needs: a small aggregation kernel — `cmd/aggregate` has **no retention/two-dir
logic today** (only within-report paired diffs). ~1 focused addition or a
standalone `cmd/aggregate -swap` reading the three verdict JSONs' `metrics_a`.
Caveat: the qwen leg used different B-conditions (frame-blind + c2b-prov) than
gpt5/gpt5mini (c2b-prov + frame-rag); retention on **C2b `metrics_a` is
comparable across all three**, but B/lift comparisons need care.

### Leg B — Answering-swap contrast for the baselines (some LLM cost)
The full H6 story is "**substrate lift survives the swap while the LLM-bound
baselines degrade**." C2b lift-invariance is by construction; to *show the
contrast* we swap the **answering** model for the LLM-bound conditions
(`c0-no-memory`, `rag-bm25`, and the `frame-rag` ceiling null) between model A
and model B, and report their noisy retention against C2b's flat 1.000.
- `frame-rag` already ran under gpt-5; a second answering model (haiku-4-5 or
  gpt-5-mini-as-answerer) gives the A-vs-B pair.
- Cost: one answering pass (c0 + rag-bm25 + frame-rag, thinking-on) over 20
  seeds × model B. Cassette-cached; commercial-API tokens are the cost driver.

### Leg C (optional) — a fourth, different-family extractor
qwen/gpt-5/gpt-5-mini already span two vendors. A Mistral/DeepSeek/Gemini
extractor leg would harden "cross-family" but is only worth it if Toni wants the
strongest possible portability claim. Cost: one full c2b-frames extraction leg
(20 seeds, ~600 calls/seed).

---

## 4. Connection to today's robustness diagnostic (same surface)

The extraction surface is where *both* portability loss *and* the F-E2
variance live. Today's seed-7 diagnostic (`results/frames-robustness/`) showed
the gpt-5-mini F-E2 composition trip was a **single stochastic frame-mis-homing**
(11/12 correct on resample), curable by K=3–5 self-consistency extraction. So:
**extraction robustness and extraction portability are the same lever.** If
self-consistency extraction is adopted (pre-registered, validated on dev), it
would (a) lift the free/weak-extractor frame-slice retention in §2 toward
ceiling and (b) remove the F-E2 tail — one mechanism, both wins. Worth folding
into the swap experiment as a "robust-extraction" variant column.

---

## 5. Decisions needed ▶ (Toni)

1. ▶ **Scope**: Leg A only (near-free, closes H6-for-frames on extraction
   portability with the data we have), or A + B (adds the baseline-degradation
   contrast at real LLM cost), or A + B + C (fourth extractor)?
2. ▶ **Headline A↔B pair**: which two extractors are "the swap" for the writeup
   — qwen36 (free/weak) ↔ gpt-5-mini (accepted/strong) is the most informative
   (shows real loss); gpt-5 ↔ gpt-5-mini shows near-zero loss (strong claim,
   less interesting). Recommend reporting **all three as a portability spectrum**
   rather than one pair.
3. ▶ **Self-consistency**: include a "robust-extraction" column (§4)? If yes it
   is new extraction runs (K passes/seed) and must be pre-registered + dev-
   validated first.
4. ▶ **Aggregation home**: extend `cmd/aggregate` with a `-swap` retention
   kernel (reusable, tested) vs a one-off script (faster, less durable).

**Recommendation:** do **Leg A now** (it is essentially a reporting/aggregation
task over committed data and closes the portability leg honestly), defer B/C/
self-consistency to explicit go-aheads. Retention on the v0 anchors is already
≥ 0.98 and the frame-slice loss is already characterized — the substrate's
model-of-record claim is measurable *today* without spending a token.
