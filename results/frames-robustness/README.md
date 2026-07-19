# Frames-v1 — one-seed extraction-robustness diagnostic (seed-7, dev)

**Status:** DIAGNOSTIC ONLY. Changes no endpoint, threshold, or verdict. The
frames-v1 campaign is CLOSED on reading (a) (MASTERPLAN §10, 2026-07-19). This
note characterizes the single seed that tripped the F-E2 v0-composition
non-inferiority leg; it does **not** license a re-run of the locked set (that
would be test-set fitting, explicitly refused). Its only forward use is a
*pre-registered design candidate* (self-consistency extraction) for any FUTURE
campaign, to be validated on dev — never tuned on seed-7.

Date: 2026-07-19. Model: gpt-5-mini, reasoning_effort=high, temperature omitted
(identical request shape to the accepted frames-v1 gpt-5-mini leg).

## What tripped F-E2

The accepted gpt-5-mini leg passed everything except one leg of the F-E2
co-primary: the v0 **composition** non-inferiority, CI-lo −0.032, driven
entirely by **seed-7** (18/20 other seeds have composition A−B ≥ 0). On seed-7
the substrate scored composition positives **49/80** (d2 = 1.00, d3 = 0.78).

## Root cause — a single frame mis-homing, not a rule miss

`cmd/fidelity` (replay of the accepted cassette, zero LLM cost) localizes it to
**exactly one rule, `rul_019`**, scored *mangled* (rules 40 exact / 1 mangled /
0 missed; recall 0.976). The mangle is **not** a logic error:

- `rul_019` ("priority_case policy 1", 3 conditions →
  `rel_d301_priority_case`) was extracted with **correct conditions,
  conclusion, authority, and dates**.
- The **only** error: it was homed to the fiction frame **`"The Unsigned
  Page"`** instead of frame `""` (a normal desk policy). Its issuing episode
  `ep_133` opens with the fiction declaration *"The Unsigned Page begins
  circulating on day 39"* one line before the policy — the adjacent fiction
  frame contaminated the policy's homing.

Homed to fiction, `rul_019` never fires in the actual world → every
`rel_d301_priority_case` derivation disappears → the depth-3 compositions that
depend on it collapse.

**Attribution is essentially 1:1:** of seed-7's 80 composition holds-positives,
**32 route through `rul_019`** (all depth-3); 31 of them failed (49 + 31 = 80).
The other 48 positives all passed. The entire seed-7 composition shortfall is
this one homing slip.

## Is the slip stochastic (self-consistency cures it) or a deterministic bias?

Resampled `ep_133`'s **exact recorded extraction prompt** against live
gpt-5-mini, N=12 independent draws (`resample_ep133.py`,
`seed7-rul019-resample-N12.json`):

| rul_019 home frame | draws |
|---|---|
| `""` (actual — **correct**) | **11 / 12** |
| `"The Unsigned Page"` (fiction — wrong) | 1 / 12 |

Logical content (conditions/conclusion) was correct in **all 12**. The fiction
frame itself was correctly declared in all 12; the only variance is whether the
following policy line gets pulled into it.

**Conclusion: the miss is a low-probability STOCHASTIC tail (~8% on this
episode), not a capability limit.** The accepted leg's cassette is one such
tail draw (so 2 wrong across the 13 observed draws, ≈15%). A trivial
**self-consistency vote** over K independent extraction passes, majority-voting
each item's frame home, removes it:

- P(single draw wrong) ≈ 0.083.
- Majority-of-3 still wrong ≈ 3·0.083²·0.917 + 0.083³ ≈ **0.020**.
- Majority-of-5 still wrong ≈ **0.004**.

So K=3–5 self-consistency extraction would, in expectation, have restored
`rul_019` to the actual frame, its `priority_case` derivations, seed-7
composition to ≈80/80, and the F-E2 v0-composition non-inferiority leg to a
pass — **without touching the query, the null, the threshold, or the frames
architecture.**

This directly answers the pre-registered diagnostic question ("does a second
extraction pass or a self-consistency vote remove the seed-7-type rule miss?"):
**yes.** It confirms the honest reading (a): the compile-time-frames thesis was
never in question here — the one negative was extraction variance on a
frame-free rule, and it is curable by a generic robustness mechanism.

## Honest caveats

1. Diagnostic only — the reported frames-v1 verdicts stand unchanged.
2. ~8% is the single-draw error rate on **this specific episode**, not a global
   extraction error rate. Do not over-generalize.
3. gpt-5-mini at reasoning high is unseeded → run-to-run variance is real; the
   accepted cassette is one draw of many.
4. Any actual adoption of self-consistency extraction must be pre-registered
   and validated on dev seeds (99, 7-as-dev), not selected because it fixes
   seed-7.

## Reproduce

```sh
# 1. localize the mangled rule (zero LLM cost — replays the accepted cassette)
cd synthworld
HARNESS_LLM_CACHE=~/code/loom/cassettes/gpt5mini-frames-c2bf \
HARNESS_LLM_CACHE_MODE=replay HARNESS_LLM_MODEL=gpt-5-mini \
HARNESS_LLM_TEMPERATURE=none HARNESS_LLM_EXTRA_PARAMS='{"reasoning_effort":"high"}' \
HARNESS_C2B_QUARANTINE_CONF=0.5 \
go run ./cmd/fidelity -dir ~/code/loom/datasets/frames-batch-v1/seed-7 \
  -extractor llm-frames -episodes episodes_natural.jsonl -handles auto
# -> rules 40 exact / 1 mangled [rul_019]; rule frames 40✓/1✗

# 2. resample ep_133 (live gpt-5-mini, ~12 calls)
python3 ../results/frames-robustness/resample_ep133.py 12
```

Artifacts: `seed7-fidelity-replay.json` (full fidelity JSON),
`seed7-rul019-resample-N12.json` (per-draw homing),
`resample_ep133.py` (probe).
