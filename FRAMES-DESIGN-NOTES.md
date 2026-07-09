# FRAMES — design-phase expert input & recommendation (2026-07-09)

*Produced by a three-lens expert analysis (frame semantics/taxonomy, test-data
design, activation-geometry feasibility) after the v0 campaign closed (H5 PASS
+39pp, H6 retention 1.0, C3 triangle complete). This is DESIGN INPUT for
resolving MASTERPLAN §9.5 into a v1 spec + pre-registration. Nothing here is
committed work; every decision below must land in the spec decisions log and
the amendments log before implementation.*

---

## 0. Synthesis & recommendation (main session)

**Recommended path: three tracks, strictly ordered by dependency, with the
geometry probe as a small gated side-track — frames do not depend on it.**

### Track 1 — frames-v1 core (pre-registered, the next campaign)
- **Mechanism (minimal v1):** `actual` + `perspective:<src>` (flat,
  non-inheriting; source: and perspective: collapsed into one type) +
  `scenario:<id>` (inherits actual, per-frame `basis: live|pinned(d)`,
  immutable after creation) + existing confidence/lifecycle machinery
  (speculation/prediction need NO new mechanism) + the explicit promotion op
  (PromotionRecord: policy ID, evidence refs, approver authority, sign-off) +
  frame-attributed derivation traces. Fiction and first-class examples
  DEFERRED (example = pinned no-promotion scenario when needed).
- **Safety invariant:** frame-assignment uncertainty routes to
  quarantine/source-frame — misclassification fails as
  stored-but-not-believed, never silently-believed. Promotion is the only
  door into `actual`.
- **Rule firing:** visibility-monotone — rule R fires in frame F iff
  home(R) ∈ cone(F). Ideation widens *query scope* (per-frame closures,
  unioned with attribution), never merges closures. "Apply fiction rule to
  real facts" = constructive act: scratch scenario + explicit ImportRecord.
- **Instrument:** synthworld-frames per §3 below (exact frame oracle, paired
  traps both directions, four new LLM-free diagnostic oracles, tier E/M/H
  text hardness with committed naturalized text à la H4).
- **The honest null to beat:** C2b-prov — frameless store + query-time
  provenance filtering. If that suffices, compile-time frames are falsified.

### Track 2 — external validity (parallel, cheap, customer-relevant)
- Hybrid tier: real carrier text + synthetic injection of frame-bearing
  facts (labels by construction). Sarcasm datasets (iSarcasm/SemEval) as an
  assertion-type-classifier check only. Financial analyst
  prediction/resolution slice = the real-domain pilot lens AND directly
  relevant to VR-Banken customers. Reported as generalization evidence,
  never in the kill arithmetic.

### Track 3 — geometry probe (small, gated, falsifiable)
- Scope: {quoted vs asserted} and {sincere vs lie} ONLY (literature supports
  truth/lie directions; sarcasm weak; fiction/quotation unexplored). Linear
  probes, mid-layer sweep with validation-only layer selection,
  style-controlled minimal contrastive pairs, held-out-domain test.
- **Portability resolution:** the probe is a swappable per-model SENSOR;
  only its OUTPUT (frame label + confidence — a symbol) is stored; probes
  are REFIT per model (minutes), never weight-transferred. Closure never
  touches activations. This preserves the H6 story; say so explicitly in
  the pre-registration.
- **Kill criterion:** probe must beat the SAME model's prompted frame
  classifier by ≥10pp balanced acc on held-out-domain style-controlled
  pairs, both frames, at equal-or-lower FPR — else falsified for v0.
  (Anthropic's 2025 honesty-elicitation study: probes LOST to prompting —
  the prompted baseline is the real opponent.)

### Decisions Toni must make (genuinely open, not resolvable by analysis)
1. Frame-distance precedence (locality-beats-authority in the cone?) —
   recommended yes; register in spec decisions log.
2. Frame-local negation/blocking for scenario fact-removal — decide before
   scenario frames ship.
3. Forecast admission default (proposed-until-observed vs active-with-
   forecast-provenance) — make it a promotion-policy primitive.
4. F-E2 superiority bar (15pp proposed, mirroring §7).
5. Whether Track 3 runs parallel to Track 1 or after F-E1/F-E2 verdicts.

### Cost order-of-magnitude
Track 1: ~$300–800 commercial (naturalization + judge panel) + self-hosted
extraction (wall-clock days) + a few person-hours human audit on tier H.
Track 3: ~1–2k labeled contrastive pairs + linear probes — days, near-zero cash.

### Sequencing
1. Amend §9.5 → v1 spec (taxonomy decision table, five breaking-point
   resolutions, minimal mechanism) in the amendments log.
2. Pre-register endpoints F-E1..F-E4, diagnostic pattern table, cue-baseline
   threshold, seed protocol — BEFORE the first naturalizer token.
3. Build: oracle frame extension → generator → validator guarantee 5 →
   diagnostics green.
4. Tier-M naturalization (committed text) → measurement → verdicts.
5. Track 3 probe experiment, gated small.

---

## Appendix A — Frame taxonomy & semantics (KR/formal-semantics lens)

### A.0 The one discriminating question

> **Does this content need its own closure — i.e., must rules fire over it
> and derive consequences under a truth assignment different from
> `actual`'s?**

If yes → **frame**. If it is a graded/temporal/modal qualification of a claim
about `actual` → **attitude on an item** (confidence, validity interval,
lifecycle). If it only records who/where/how → **provenance**. The store
already has all three attitude mechanisms. The taxonomy, not GC policy, is
the primary defense against frame proliferation (breaking point #2).

### A.1 Decision table

| Phenomenon | Storage treatment | What `Closure(actual,t)` sees | What a query can reach |
|---|---|---|---|
| **Fiction** | Frame `fiction:<id>`, inherits nothing (schema only) | Nothing | Explicit frame-open / ideation mode; consequences derivable inside the frame |
| **Scenario/what-if** | Frame `scenario:<id>`, inherits `actual` + delta overlays (override = existing supersession), pin field | Nothing | Frame-open; scenario closure sees actual through inheritance |
| **Worked example** | Scenario subtype: pinned basis, no-promotion flag, TTL | Nothing | Frame-open; deferrable as first-class type |
| **Perspective/contested claim** | Frame `perspective:<agent>` (flat, non-nested, non-inheriting in v1) | Nothing until promoted | Frame-open ("what does BaFin hold?"); own closure gives committed consequences of the stated position |
| **Lie (known)** | Dual entry: claim in `perspective:<liar>`; PLUS actual-facts `claimed(x,P,d)` and the true state of P; liar reliability downgraded in provenance/authority feeding promotion | The fact the claim was made, and the truth; never the claim's content | Both: "what did X claim?" and "what is true?" |
| **Unvetted source claim** | `source:<origin>` frame item; promotion via corroboration policy | Nothing until promoted | Frame-open; ideation |
| **Speculation** | **No new mechanism**: actual-frame item, low confidence, lifecycle `proposed` (the dormant S2 hooks) | Excluded (lifecycle gate) | Confidence-inclusive / ideation mode |
| **Prediction** | **No new mechanism**: actual-frame fact, validity `[t_future,…)`, forecast marker + confidence; recommend `proposed` until observed/policy-admitted | Nothing before t_future; holds at t≥t_future only if active | Forecast-inclusive mode; trace shows forecast provenance |
| **Opinion (preference)** | Speaker-indexed base fact in actual: `prefers(toni, go, python)` | Ordinary fact | Normal query |
| **Opinion (contested normative)** | `perspective:<speaker>` item | Nothing | Frame-open |
| **Emotion** | Time-scoped fact about the speaker in actual: `feels(anna, frustrated, [d,d+k))` | Ordinary fact | Normal temporal query |
| **Sarcasm** | **Extraction-layer problem, not a storage category.** Compile the INTENDED content (often negation of literal) into actual with reduced confidence + `nonliteral` marker; unsure → quarantine | Intended content if confident; nothing if quarantined; never the literal reading | Normal query; quarantined via review/ideation |
| **Humor/joke** | Event fact (`told_joke(x,y,d)`) if worth keeping; content participates in no closure | The event only | Provenance/text retrieval, not `holds` |
| **Art** | As fiction (content) + actual facts about the artifact/its reception | Facts about the artifact | Frame-open for content |

Speculation and prediction are pure vindication of existing machinery
(confidence+lifecycle; validity intervals+lifecycle). Emotion and preference
opinions are ordinary speaker-indexed facts. Sarcasm/humor are illocutionary-
force problems living in the extractor, MEASURED (assertion-type fidelity)
not architected around. Only fiction, scenario(+example), perspective/source
are genuinely frames.

### A.2 The five §9.5 breaking points — resolutions

**#1 Frame detection errs** → asymmetric failure routing: below-threshold
frame assignments land in `source:<origin>` or quarantine, never `actual`;
promotion is the only door. Frame-assignment fidelity becomes a scored
compilation stage with a confusion matrix. Cost: one pipeline stage, recall
latency, a governance-surface threshold knob.

**#2 Proliferation** → three dams: (a) taxonomy first (most would-be frames
are attitudes); (b) typed constructors — perspective keyed by canonical
agent ID with alias table (reuse S2 normalization); scenario gets TTL/
archival + auto-GC unless referenced by a promoted item; session scratch
frames die by default; merges = reversible aliasing, never destructive;
(c) frame count / items-per-frame as manifest statistics with machine-checked
gates (lesson §8.5 style).

**#3 inherit-live vs pinned** → per-frame `basis: live | pinned(d)`, set at
creation, immutable; defaults by type (scenario→live, decision-record/
example→pinned). Pinning is NOT new semantics: inherited layer evaluated at
`t' = min(t, d)` — one effective-timestamp per inheritance edge in the
existing closure. Cache key becomes `(frame, t, parent_version_or_pin)`;
pinned frames cache better than today; live frames invalidate via a per-frame
version counter bumped on the single Commit path. Re-pinning forbidden
(re-pin = new frame).

**#4 Cross-frame rule firing** → visibility-monotone: R fires in F iff
`home(R) ∈ cone(F)`; consequences land only in F's closure. Actual rules
fire on scenario facts (actual ∈ cone(scenario)); fiction rules structurally
cannot reach actual. Ideation relaxes query scope, never firing. Explicit
cross-frame import = scratch scenario + ImportRecord (the invariant made
operational). Stratification checked over the cone's union; per-cone
schema-inference DFS.

**#5 Promotion is governance** → ship mechanism, not policy: promotion is an
explicit Commit-path op with an append-only PromotionRecord (policy ID,
evidence refs, approver authority, optional sign-off token). Ship 2–3
composable policy primitives (corroboration threshold, authority whitelist,
four-eyes sign-off); conservative default. v1: only ATTESTED items
promotable, never frame-derived consequences. Sell it as "auditable belief
admission" — a compliance feature, not a tax.

### A.3 Formal lineage verdict

Closest ancestor: **McCarthy/Guha contexts as operationalized by Cyc
microtheories** — `ist(c,p)` = frame-relative truth, `genlMt` = the
visibility DAG. Cyc's fatal lesson: **unrestricted lifting rules killed it**;
Loom's fix is exactly two disciplined crossings (inheritance-with-override
down the DAG; explicit promotion into actual). Every future "couldn't a rule
also apply when…" is a lifting rule trying to get back in — refuse.

Steal from **RDF named graphs**: frame as a first-class column on every item
(SPARQL `GRAPH` = explicit multi-frame open); avoid their underspecified
cross-graph entailment by writing inheritance/firing semantics as normatively
as the oracle spec. From **possible worlds**: vocabulary only — frames are
named, finite, materialized Datalog theories, no prover. From **epistemic
logic**: per-frame closure = logical omniscience, a feature here, but traces
must surface attested (depth 0) vs derived (depth ≥1) in perspective-frame
answers (likely a product/liability requirement for regulated customers);
and DO NOT NEST — perspective frames are one level deep; `says(b,p)` inside
`perspective:a` is a fact, never a frame-in-frame. From **AGM**: promotion is
expansion-under-policy, not revision; conflicts route to existing
supersession/precedence. **Speech-act theory** structures the extractor's
front door: only assertives are candidates for actual; commissives →
obligation facts; expressives → emotion facts; declarations authority-checked.

One line: *Datalog with a context dimension, inheritance restricted to a DAG,
and lifting replaced by two audited operations* — microtheories minus the
fatal generality, named-graph storage pragmatics, same deterministic Go
stratified fixpoint keyed by frame.

### A.4 Open questions flagged (→ decisions log)
1. Frame-local negation/retraction (scenario removing an actual fact) —
   block polarity exists in schema; frames are its forcing function.
2. Precedence across the cone: recommend a leading frame-distance key
   (nearer frame wins) before authority→recency→specificity→ID.
3. Perspective-frame derived consequences are Loom's inferences, not the
   source's utterances — surface attested/derived distinction explicitly.
4. Forecast admission default = promotion-policy primitive, not hardcode.

---

## Appendix B — Test-data strategy (experimental-design lens)

### B.1 Generator growth (synthworld-frames)

Frame table in world.json: `{ID, Kind, Parents[], PinDay*, CreatedDay}` DAG;
every BaseFact/Rule/Supersession gains FrameID (default actual). Generate
one pinned + one live scenario per seed — the distinction is itself a query
target.

**Critical:** fiction/perspective frames REUSE the actual world's types,
relation vocabulary, and (partially) entities — disjoint vocabulary would
make contamination detectable by surface statistics (sham at world level).
Fiction facts in two sub-populations, tracked separately:
- **Contradiction facts** — type-correct atoms contradicting an actual fact
  (recoverable by conflict detection even without frame detection).
- **Gap facts** — plausible atoms about slots actual never populates; ONLY
  frame assignment keeps them out. The sharpest contamination traps.

**Frame-bearing episode types** (each event's payload gains ground-truth
`FrameID` + `AssertionType ∈ {assert, quote, non-assertive}`; easy mode
parses them, text mode never sees them, scoring always does):
1. Narration episodes (fiction as story; marker explicitness = tier knob).
2. Scenario sessions ("assume for planning" deltas = frame-scoped
   supersessions + override facts).
3. Reported speech — `said(X,P)` in actual; P true only in perspective:X;
   two frames inside one event (misattribution workhorse).
4. Unreliable narrators — 3 perspective frames/seed with conflicting
   versions of a contested atom set; actual renders no verdict.
5. Sarcasm/irony — literal atom false in actual, assertion type
   non-assertive; correct compilation stores nothing assertive in actual.
6. Predictions with resolution — claim at d in source:<origin>; later
   resolution episode; confirmation triggers an explicit PROMOTION event;
   some unresolved at t_eval.
7. Mixed episodes — mid-episode frame switches, density-controlled.

### B.2 Oracle stays exact

`Closure(W,t,frame)`: (a) visibility filter before seeding — item set =
union over ancestor chain, inherited items time-filtered at PinDay when
pinned (pure set filter; termination/stratification/join guards untouched);
(b) ONE new precedence tier — frame proximity ABOVE authority (scenario
delta overrides inherited actual fact regardless of authority). Traces gain
the frame of every support. cmd/validate guarantee 5: every query's ground
truth reproduces under the frame closure; every trace support's frame is on
the query frame's ancestor chain.

### B.3 Query slices & paired traps

| slice | positive/trap | paired control | punishes |
|---|---|---|---|
| contamination | holds-in-actual of fiction/sarcasm/quoted/unresolved-prediction atom → false (contradiction vs GAP traps reported separately) | near-identical atom genuinely true in actual | "false to anything story-flavored" |
| isolation | holds-in-scenario of untouched inherited actual atom → true; composition chains mixing inherited facts + scenario deltas | atom overridden by a delta (scenario ≠ actual answer) | "answer from actual" AND "wall off the scenario" |
| pinning | pinned scenario queried after an actual revision the pin freezes out | live scenario, same revision, must track | ignoring inheritance semantics either way |
| misattribution | "in which frame(s) does P hold?" scored on (answer, frame) pairs — right answer wrong frame = error | internal pairing: every atom true in ≥1 frame, false in ≥1 other | frame-blind correctness |
| promotion | confirmed prediction in actual before vs after resolution day (pair); unresolved in actual → false | same claim in its source frame → true | premature belief AND refusal to promote |
| cross-frame ideation | find over explicit frame set with per-satisfier frame labels; composition crossing one boundary | same pattern restricted to actual (must shrink correctly) | roaming without attribution; sterile isolation |

### B.4 LLM-free diagnostics (pre-register the pattern table)
- **frame-oracle** — ceiling, 100% everywhere.
- **mono-world oracle** (everything dumped in actual) — fails EXACTLY
  contamination + misattribution; aces controls/isolation/v0 slices.
- **isolationist oracle** (no inheritance) — fails exactly isolation/pinning
  + scenario-composition; passes contamination.
- **literalist oracle** (quoted/sarcastic literal content asserted in
  actual; frames otherwise correct) — fails exactly the speech-act
  sub-slice; separates assertion-type failures from frame-DAG failures.
- always-true / always-false / episode-grep retained; grep's expected
  fingerprint on new slices gets a registered row.

### B.5 Frame-assignment fidelity (compilation metric)
Full frame confusion matrix per item type; macro-F1 headline; two named
poison cells as gates: **fiction→actual leakage rate** and **actual→
non-actual exile rate**. Quarantine scored as abstention WITH ITS OWN RATE +
coverage reported — a substrate must not ace fidelity by quarantining
everything.

### B.6 Text hardness tiers (the load-bearing new risk)

H4's lessons: commit rewritten text (determinism); validate preservation
mechanically; certify hardness by the collapse of a cheap baseline. Frames
are harder: frame cues live in surface pragmatics the H4 content validator
doesn't constrain. Templated sarcasm is a regex — an experiment on it is a
sham and must be pre-declared as such.

- **Tier E (templated, explicit markers)** — harness-debugging only.
  Pre-register that a marker-regex detector scores ≈1.0 here (certifies
  harness AND certifies the tier as non-evidence). Never headlined.
- **Tier M (LLM-naturalized, primary)** — naturalizer LLM(s) outside the
  evaluated matrix (≥2 families for style diversity) render naturalistic
  prose (narrative voice, fading "imagine if" register, unmarked quotes,
  contextual sarcasm). Validators: (i) H4 content-preservation extended;
  (ii) **frame-recoverability audit** — 3-judge panel (outside matrix) must
  recover ground-truth frame from text at ≥2/3 agreement, else
  retry-with-feedback (bounds label noise). **Authenticity certificate**: a
  surface-cue baseline must score far below ceiling (pre-registered max) —
  the analog of the det-extractor collapse certifying H4 paraphrases.
- **Tier H (adversarial, 5 seeds)** — naturalizer minimizes cues (deadpan
  sarcasm, reportorial fiction, long unmarked scenario continuations,
  mid-episode switches). DO NOT filter by judge panel (caps difficulty at
  judge ability); human spot-audit ~100 events; **human frame-recovery
  accuracy is the honest ceiling** — report machine vs human.

Registered caveat (verbatim in any writeup): LLM-naturalized text tests
frame detection on LLM-generated pragmatics; naturalizer∉matrix, family
diversity, and the real-data tier mitigate but do not eliminate this.

### B.7 Real-data component

| corpus | ground truth? | verdict |
|---|---|---|
| iSarcasm / SemEval-2018 T3 / SARC | assertion-type labels; no world model | external validity check on the assertion-type classifier ONLY (~1 day) |
| Fiction corpora | doc-level metadata (trivial given, undefined without) | carrier/style material only |
| AMI/ICSI meeting transcripts | dialogue acts, no factual world | skip as gated evidence; candidate carrier |
| **Financial analyst reports / earnings calls** | **predictions resolve mechanically against later data**; speculation vs reported-fact registers native | **best real candidate**: real prediction/promotion slice, objective resolution, VR-Banken-relevant |
| Multi-source conflicting news | contested adjudication, expensive | skip this campaign |

**Recommended hybrid** (frames analog of the paraphrase tier): real carrier
text + synthetic injection of generated frame-bearing facts about synthetic
entities, woven in by the naturalizer — labels by construction, distribution
substantially real; audit injection detectability (register mismatch).
Role, pre-registered: real/hybrid data never gates the kill criterion; it
gates the generalization caveat. Directional non-reproduction on the hybrid
tier is a reportable negative about distribution-dependence.

### B.8 Pre-registrable endpoints

**Honest null (a measured condition, not a straw man): C2b-prov** —
frameless store, everything in one world with episode/source metadata,
query-time metadata filtering. Registered prediction of where it fails:
content-cued frames (sarcasm, unmarked narrative, mid-episode switches) and
scenario composition (query-time filtering cannot do delta-override overlay
on a derivation chain). Also measure frame-blind C2b to CONFIRM ≈100%
fiction-trap contamination, not assume it.

- **F-E1 (co-primary, two-directional safety):** tier-M, frame-aware C2b
  balanced-acc CI lower ≥85% on BOTH contamination AND isolation slices
  (each pooled with paired controls; gap-traps a mandatory sub-line).
  **Kill: either direction's lower bound <70%.** Both gate jointly — no
  buying safety with sterility.
- **F-E2 (co-primary, superiority over the null):** C2b(frames) beats
  C2b-prov on the content-cued trap subset by ≥15pp balanced acc (CI lower
  ≥ +15pp), at non-inferiority (2pp margin) on metadata-cued traps AND every
  v0 slice (frames must not damage frame-free competence). **Kill: gap
  <15pp → query-time provenance filtering suffices; the compile-time-frames
  bet is falsified.** No geometry rescue.
- **F-E3 (secondary, diagnostic):** frame-assignment macro-F1 ≥0.90 on tier
  M; fiction→actual leakage <2% of fiction items; abstention rate reported.
- **F-E4 (secondary):** ideation cross-frame find exact-set with
  per-satisfier frame-attribution ≥90% and correct trace boundary-marking.

Tier H + hybrid/real reported against human/directional benchmarks, outside
kill arithmetic. Endpoints, diagnostic table, cue-baseline threshold, seed
protocol lock BEFORE the first naturalizer token; amendments to the log.

### B.9 Size & cost

Per seed: 2 fiction frames, 2 scenarios (pinned+live), 3 perspectives, ~10
predictions → ~180 episodes (~1.8×); ~140 frame queries/seed (40
contamination pairs = 20 contradiction + 20 gap; 40 isolation/pinning; 20
misattribution; 20 promotion; 20 ideation) + retained v0 slices. Seed
protocol = E0.6 (candidates 1..40, manifest gates + ≥N gap-traps, ≥N
inheritance chains, per-frame firing hygiene; first 20 passers locked).
Tier H + human audit on first 5 locked seeds (E5 cost-tiering precedent).

Budget: naturalization ~8–12M tokens ≈ $150–400; judge panel ≈ $100–300;
extraction ~15–20M tokens self-hosted (wall-clock days, cassettes from the
first call); C2 queries free; C1 reruns ≈ one E4-scale sweep; human audit a
few person-hours. **Total new commercial spend ≈ $300–800.** Naturalized
text committed to the repo as the dataset artifact.

---

## Appendix C — Activation geometry for frame detection (research lens)

### C.1 Evidence map (citations at end)

- **Truth/lie: strong.** SAPLMA (Azaria & Mitchell): 71–83% from MIDDLE
  layers. Marks & Tegmark: truth is often a linear, CAUSALLY load-bearing
  direction (mass-mean probes). ITI: sparse truthful heads, few hundred
  examples, TruthfulQA 32.5→65.1%. CCS: unsupervised truth direction.
- **Deception/intent: strong but caveated.** Anthropic sleeper-agent probes
  AUROC >99% with generic contrast pairs — on ARTIFICIAL backdoors; natural
  deception unknown.
- **THE key counter-result:** Anthropic 2025 honesty-elicitation study —
  probing/steering LOST to simple prompting and fine-tuning baselines (best
  lie-detection AUROC 0.88, via fine-tuning + honesty prompts, not probes).
  The prompted model is a strong, cheap opponent.
- **Sarcasm/irony: weak** for the internal-signal claim — published work
  consumes final-layer embeddings or step-by-step reasoning; no clean
  mid-network "sarcasm direction" shown to beat prompting.
- **Fiction/quotation/hypothetical: near-absent.** SAE features plausibly
  relevant exist, but no published validated probe for "fiction vs actual"
  or "quoted vs asserted" — the frames FRAMES needs most are the least
  supported.
- **Confidence/uncertainty: moderate** — useful for the confidence field and
  promotion gating.
- **Generalization caveat:** truth geometries near-orthogonal across tasks;
  logical negation breaks many probes; only capable instruction-tuned models
  show clean domain-independent directions.

### C.2 Portability resolution

Do NOT transfer probe weights (cross-model retention ~54–58%). The probe is
a **swappable per-model sensor**; what enters Loom is its OUTPUT (frame
label + confidence — a symbol, as model-independent as any extracted fact).
The closure never touches activations. Refit per model = minutes on one GPU
over a few hundred–thousand labeled spans (like a calibration table, NOT
like LoRA: cheap, behavior-preserving). The durable, portable asset is the
LABELED FRAME DATASET. Per-model cost: regenerating activations for the
labels (trivial for linear probes; keep it linear until forced otherwise).
The moment activations are stored or closure depends on them, the substrate
re-couples to the model vendor — the exact thing Loom exists to avoid.

### C.3 Feasibility on vAI infra

vLLM ships official hidden-state extraction (2026-03); or a plain HF forward
with output_hidden_states=True on the same open weights (a probe study needs
a few thousand labeled spans once, not serving throughput). Linear probe
training: seconds–minutes, hundreds of examples per class. Integration: an
optional second sensor into the C2b extractor's frame decision, behind a
flag. The hard part is LABELS with controlled surface style, not compute.

### C.4 Failure modes
1. **Style, not frame (dominant risk)** → minimal contrastive pairs
   (near-identical surface, opposite frame); probe collapses there → kill.
2. **Distribution shift** → held-out-domain test split mandatory.
3. **Frame not in the forward pass** — sarcasm/lies often resolvable only
   from context outside the span window; information-theoretic ceiling;
   design the input window to include frame-determining context or accept it.
4. **Fishing expedition** — layer×probe×frame×threshold search space;
   pre-register layer-selection rule, baselines, kill criterion.

### C.5 Minimal pre-registerable experiment

- Frames (2): {asserted vs quoted}, {sincere vs lie}. Binary linear probes
  (logistic + mass-mean) on residual stream at claim-final token.
- Data: ~1–2k minimal contrastive pairs per frame; in-domain held-out +
  held-out-domain (bank-like) splits; negation stress subset.
- Layer sweep with validation-only selection; expect mid-to-late layers.
- Baselines (must beat ALL): (1) same model PROMPTED to classify the frame
  (few-shot) — the one that matters; (2) lexical/frozen-embedding logistic
  regression (style-confound guard); (3) majority floor. MLP only as a
  documented secondary; MLP-wins-linear-doesn't = style-overfitting evidence.
- Portability sub-test: refit on second family (<~30 min), identical label
  schema; report weight-transfer failure (~55%) as JUSTIFYING refit-not-
  transfer.
- **Kill criterion:** on held-out-domain style-controlled test, linear probe
  beats the same model's prompted classifier by ≥10pp balanced acc (or
  ≥0.05 AUROC) on BOTH frames at equal-or-lower FPR on style-controlled
  negatives — else activation-informed frame detection is falsified for v0
  and geometry stays shelved. Only-in-domain / only-MLP / only-uncontrolled
  wins count as failure.
- Honest prior: moderate P(beats prompting on lie, in-domain); lower
  P(survives held-out-domain + style control); low P(helps on
  quoted-vs-asserted, no prior art). Negative result publishable.

### C.6 Sources
- SAPLMA: arxiv.org/html/2304.13734v2 · Geometry of Truth:
  arxiv.org/html/2310.06824v2 · ITI: arxiv.org/abs/2306.03341
  (github.com/likenneth/honest_llama)
- Generalization: arxiv.org/html/2506.00823 · lesswrong.com "how well do
  truth probes generalise"
- Anthropic sleeper-agent probes: anthropic.com/research/probes-catch-
  sleeper-agents · **Honesty elicitation (the counter-result):**
  alignment.anthropic.com/2025/honesty-elicitation/
- Sarcasm: arxiv.org/html/2407.12725v1 · arxiv.org/pdf/2312.03706
- SAE survey: arxiv.org/abs/2503.05613 · Confidence manifold:
  arxiv.org/pdf/2602.08159 · Model stitching: arxiv.org/html/2506.06609v3
- vLLM hidden states: docs.vllm.ai …/extract_hidden_states/ ·
  github.com/agencyenterprise/vllm-hidden-states
