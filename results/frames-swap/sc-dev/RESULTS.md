# Self-consistency extraction — dev validation (seed-7)

**Pre-registration:** MASTERPLAN §10 2026-07-20. K=5 sampled extractions,
majority-vote frame homing. DEV-ONLY (seed-7, the known rul_019 failure case;
seed-99's naturalized dataset is not on disk and regenerating the tier-M
pipeline is disproportionate for a polish experiment). **Not** a locked-verdict
rerun. Condition `loom-c2b-frames-sc` vs single-sample `loom-c2b-frames`, same
temperature, same seed. SC genuinely resampled: ~3000 calls (5×600) per model,
no cache collapse.

## Result

| model | condition | composition | misattr F1 | ideation F1 | contam | isolation | find F1 |
|---|---|---|---|---|---|---|---|
| **gpt-5-mini** | single | **49/80** | 1.000 | 1.000 | 0.990 | 0.968 | 0.989 |
| **gpt-5-mini** | **SC K=5** | **80/80** | 1.000 | 1.000 | 0.990 | **1.000** | **1.000** |
| qwen36 | single | 76/80 | 0.863 | 0.948 | 0.980 | 0.968 | 0.973 |
| qwen36 | SC K=5 | **80/80** | 0.844 | **0.980** | 0.980 | **0.830** | 0.973 |

## Reading (honest, with the failure mode)

**Decisive on the designed target (gpt-5-mini, the accepted-leg model).** The
single fresh draw *reproduced the exact seed-7 F-E2 tail* — composition **49/80**
(the rul_019 mis-homing, 31 depth-3 positives lost, matching the 2026-07-19
diagnostic). The K=5 vote **fixed it completely → 80/80**, and *improved*
isolation (0.968→1.000) and find (0.989→1.000) with no regression anywhere. This
is exactly what the diagnostic predicted: rul_019's mis-homing is stochastic
(~8% per draw), and a majority-of-5 vote removes it. **Success criterion (ii):
MET.**

**Partial and instructive on the weak extractor (qwen36).** SC lifted
composition (76→80) and ideation (0.948→0.980) and repetition, but **regressed
isolation (0.968→0.830)** and slightly misattribution (0.863→0.844). The honest
mechanistic lesson: **self-consistency cures VARIANCE, not BIAS.** Where a model
mis-homes *stochastically* (gpt-5-mini/rul_019), voting recovers the correct
modal answer. Where a weaker model mis-homes *systematically* (qwen's isolation
items), the wrong answer is already modal, so voting *entrenches* it. **Success
criterion (i): only partially met** — SC is not a blanket weak-extractor booster.

**No v0 regression (criterion iii): met** — composition/repetition/find held or
improved on both models; the qwen regressions were confined to frame slices.

## Verdict

Self-consistency is **validated as a variance-reduction tool for a capable
extractor** — it removes the single F-E2 tail (the one honest negative in the
frames-v1 campaign) exactly as the diagnostic said it would. It is **not** a
general "lift any weak extractor to ceiling" mechanism; on a systematically
biased model it can entrench errors (isolation). For Phase 2 it is a useful
per-model knob, to be validated per model/domain, never assumed.

**Caveats:** single seed, single SC draw per model (n=1 draw — the gpt-5-mini
49→80 contrast is strong because it lands on the known-failure item, but the
qwen isolation regression could carry draw noise). Consistent with the
pre-registration, no locked-set verdict was rerun or changed.
