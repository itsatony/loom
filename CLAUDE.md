# CLAUDE.md — Loom / synthworld handoff

Context document for an agent continuing this work. Everything here was true
and verified on 2026-07-02; re-verified 2026-07-06 (build, vet, validator,
diagnostic pattern, C2a==oracle on sample-dataset all green). Read this fully
before writing code. The spec (`./loom_substrate_spec.md`) and the code
(`./synthworld/`) are the two artifacts; this file is the map, the rationale,
and the backlog. The experimental campaign — hypotheses, pre-registered
endpoints, wargamed eventualities — lives in `./MASTERPLAN.md`.

---

## 1. Mission — what we are trying to prove

vAudience.AI (Toni Wagner, CEO) is testing one falsifiable bet, internally,
after a SPRIND grant on a related "Geometric Cognition Substrate" was not
selected. The bet, deflated to its honest core:

> An agent's accumulated experience can be **compiled** into structured
> knowledge that (1) survives fact revision, (2) answers queries whose
> answers were never stored, by combining stored items, and (3) transfers
> across LLM swaps with measured, small loss.

The **conjunction** is the claim — each property alone is unremarkable:

| capability | episodic RAG (C1) | LoRA (C3) | compiled substrate (C2 = Loom) |
|---|---|---|---|
| accumulation as *knowledge* (rules/exceptions/supersession) | weak (documents) | yes | **must show** |
| composition (never-stored answers) | no | some | **must show** |
| portability across model swaps | trivial | no | **must show** |

**Honest boundary (enforce in all writing):** "transfer of understanding"
means exactly *performance on held-out compositional and revision queries
survives a model swap*. Nothing more. Do not inflate.

**Strategic frame:** vAI is 30 people; it cannot train frontier models. The
product posture is "the cognition layer where accumulated improvement
belongs to the customer, not the model vendor" — ride the open-weights
curve, EU-regulated-B2B fit (5 VR-Banken in production today). The LLM is
swappable infrastructure; the substrate is the model of record.

**Deliberately dropped for now:** Gaussian/region geometry (the grant's
core). Representation is a hypothesis, not a commitment; a typed symbolic
store must win or lose first. Geometry is a Stage-2 question ONLY if v0
succeeds. Kill criterion (§7) is pre-registered; geometry does not get to
rescue a failed v0.

## 2. Folder layout

```
~/code/loom/
├── CLAUDE.md                  ← this file
├── loom_substrate_spec.md     ← Loom v0 spec: data model, compilation loop,
│                                 op API, portability contract, milestones,
│                                 kill criteria, decisions log. AUTHORITATIVE.
└── synthworld/                ← Go monorepo (module github.com/vaudience/synthworld)
    ├── DESIGN.md              ← semantics contract for the instrument. Read 2nd.
    ├── README.md              ← quickstart + current results tables
    ├── docs/loom-substrate-v0-spec.md   (same spec, in-repo copy)
    ├── world/                 ← schema: entities, n-ary facts, stratified rules,
    │                            exceptions, authority, supersession; Validate()
    ├── oracle/                ← EXACT evaluator: stratified closure at time t,
    │                            precedence (authority→recency→specificity→ID),
    │                            derivation traces, stale closure, join guard
    ├── gen/                   ← seeded world generator + episode stream +
    │                            oracle-verified queries + quality repair loop
    ├── loom/                  ← THE SUBSTRATE, S1: store (lifecycle+provenance),
    │                            schema inference, structured ingest, ops
    ├── harness/               ← condition interface, scoring, diagnostics,
    │                            BM25+tmr retrievers, RAG condition, LLM client,
    │                            provenance probe
    ├── cmd/synthgen           ← generate dataset (world/episodes/queries/manifest)
    ├── cmd/validate           ← independent re-verification of all guarantees
    ├── cmd/harness            ← run conditions, print score tables + probe
    ├── cmd/memoexport         ← episodes → tmr *.memo.md files
    ├── sample-dataset/        ← committed seed-1234 dataset
    └── test → (none yet; the validator + diagnostic conditions are the test
                harness; adding go tests is welcome but keep the validator king)
```

Go 1.22+, **stdlib only in this module** (deliberate: reproduction package
must build anywhere). Determinism is sacred: same seed + same binary =
identical dataset; all map iteration over sorted keys.

## 3. The instrument (synthworld) — semantics in 60 seconds

- Timeline = integer days 0..Horizon (360). Eval at t_eval = Horizon.
- Typed entities; n-ary relations with named typed slots and a **stratum**:
  0 = base (facts only), ≥1 = derived (rules only; conditions reference
  strictly lower strata ⇒ deterministic, terminating closure).
- **Facts**: ground atoms + validity `[from,to)` + source + revealing episode.
- **Rules**: safe Horn clauses; exceptions (pattern-satisfiable ⇒ rule
  doesn't fire for that binding); authority 1–5; effective intervals.
- **Supersession**: from day d, old rule stops firing (totally or for
  bindings matching a condition).
- **Oracle** `Closure(W,t)`: stratified fixpoint; per-atom candidate
  precedence authority↓ → IssuedAt↓ → #conditions↓ → ruleID. Every derived
  atom carries a replayable derivation trace with depth.
  `StaleClosure` ignores supersessions (the revision-blind belief state).
- **Episodes**: chronological chunks of events (fact observations, rule
  issuances, supersession notices), each with structured payload (easy
  mode) AND templated text (hard mode). Systems under test see ONLY
  episodes.jsonl.
- **Query slices** (all oracle-verified at generation, re-verified by
  cmd/validate):
  - `repetition` — stated verbatim; RAG should tie; a substrate losing here
    destroys information at compile time.
  - `composition` — derivation depth ≥1–3 across ≥2 episodes; **the
    go/no-go slice**. Positives + perturbed-argument negatives + `find`
    (enumerate satisfiers).
  - `revision` — flips (supersession changes the answer; stale ≠ current)
    PAIRED with retained controls (bindings the supersession does NOT
    affect) to punish over-revision. `stale_answer` recorded per query.

## 4. Loom S1 (the substrate) — what exists

- **Store** (`loom/loom.go`): facts/rules/supersessions each wrapped with
  `Lifecycle` (proposed/active/superseded/retracted/quarantined) and
  **mandatory Provenance** (episode IDs, confidence, extractor). Commit is
  the single write path; dedupe merges provenance. Save/Load JSON snapshot
  (journal + Postgres adapter deferred to repo split).
- **Schema inference**: relation strata derived from the ingested rule
  dependency graph via memoized DFS (never concluded ⇒ base; else 1 + max
  condition stratum; cycle ⇒ error). **Nothing is read from world.json** —
  the store knows only what it ingested, like production would.
- **Evaluator = the synthworld oracle, imported.** This is the load-bearing
  design decision: C2 failures are attributable to compilation, never to
  inference-semantics drift, because scorer and substrate share the engine.
- **Ops**: Holds (+trace), Find, Diff(t1,t2), StatsAt (derived-atom counts,
  quarantine). REST/MCP deferred; harness calls Go API directly.
- **Structured ingest** (C2a, easy mode): extraction is a parser;
  confidence 1.0; nothing silently dropped (problems land in the
  IngestReport).

**Verified state:** `loom-C2a` is **strictly oracle-equal on 5/5 seeds**
(42, 7, 99, 1234, 2026) — every slice, every polarity, find, per-depth.
That is the S1 exit criterion, met. It also means: the interesting numbers
all come from C2b (text mode) and the C1/C3 baselines.

**Frames (2026-07-10, build step 4):** structured ingest is frame-native —
frame table from EvFrame declarations, promotion records (audit only),
non-assertive events skipped (counted in IngestReport.NonAssertive), quotes
homed in perspective frames, fact dedupe on frame+block+atom+interval; ops
frame-parameterized (`HoldsIn`/`FindIn`) and the C2a condition implements
`harness.FrameAnswerer`. Verified `loom-C2a == frame-oracle` on frames dev
seeds 99 + 7 at full JSON-report granularity; v0 sample-dataset regression
intact.

**Frames seed lock (2026-07-11, build step 5):** cmd/batch frames edition
(v0 + §9.6.8 gates, worker pool with numeric-order finalization) locked the
20 frames seeds {1,2,3,7,8,9,10,12–16,18–22,24,25,29} from candidates 1..40
(batch manifest committed; datasets in datasets/frames-batch-v1/,
regenerable deterministically). §9.6.5 diagnostic table FROZEN from the
locked batch: frame-oracle perfect in every cell, `loom-C2a == frame-oracle`
per cell per seed on all 20 (per-seed JSONs in results/frames-diagnostics/).
No LLM token has touched any frames dataset. Next: tier-M naturalization
(§9.6.6), then C2b frames extraction (F-E1..F-E4).

**Tier-M pipeline (2026-07-12, build step 6):** cmd/naturalize (2
naturalizers outside matrix: mistral-medium-3.5 + deepseek-v4-pro, split by
episode parity; mechanical content validator = v0 rules + frame-ID
ban/handles + marker/genre bans; 3-judge frame-recoverability panel
gemini-3.5-flash / kimi-k2.6 / grok-4.20-non-reasoning, ≥2/3 exact,
retry-with-feedback, zero-tolerance on frame-bearing fallbacks) +
cmd/authcert (LLM-free surface-cue baseline: marker regex + LOSO
naive-Bayes; §9.6.6 reading RATIFIED 2026-07-12, option (a): certificate =
tier-E marker-regex collapse to 0 hits, supervised LOSO-NB trap-direction
number reported ungated as a hardness descriptor — MASTERPLAN §10). Design memo:
synthworld/docs/tierM-naturalization.md. INSTRUMENT amendment same day:
frame-fact source names (manuscript_*/planning_*/narrator_*) no longer
rendered into tier-E text (they leaked frame type lexically); locked-seed
payloads/world/queries byte-identical, frozen §9.6.5 reproduced per cell
per seed, on-disk episodes.jsonl refreshed (datasets stay untracked
regenerables). Dev seeds 99+7 naturalized green on all pipeline gates
(fallbacks 0.4%/2.0%, frame-bearing fallbacks 0/0, unrecovered 3.7%/1.4%,
actual-line judge false alarms 0/1754, tier-E marker hits 0). Under the
ratified reading (a) the dev tier-M corpus CERTIFIES (marker hits 34→0);
the supervised LOSO-NB hardness descriptor is 0.757 trap-direction
(with-controls 0.827) — reported ungated in every writeup: with markers
dead and genre words banned, a labeled-data classifier still finds
structural lexical cues (e.g. actual observation lines carry a feed
token, frame lines don't). See MASTERPLAN §10 2026-07-12 entries.

**Tier-M locked batch (2026-07-13, build step 7): DONE and CERTIFIED.**
All 20 locked seeds naturalized (uniform pipeline; three same-night
amendments logged in §10: validator inflection/feedback fixes, per-line
pinning in the mech-retry loop, marker-list sync "within this scenario"/
"in the story"; plus judge 2 moved to Nebius after the Moonshot account
suspension silently killed ~80% of kimi calls — naturalize now fails if
any judge errors on >10% of episodes). Batch aggregate (5,899 episodes):
fallbacks 0.85%, frame-bearing fallbacks 0, unrecovered 2.6% (max seed
3.7%), actual-line judge misses 0/17,824, judge errors 0. Certificate:
marker hits 0 on all 20 seeds → CERTIFIED (reading (a)); tier-E
calibration refused (2,592 hits). Hardness descriptor 0.791
trap-direction / 0.850 with-controls (ungated, always reported).
Payloads byte-identical to tier E; loom-C2a == frame-oracle == frozen
§9.6.5 per cell per seed on the naturalized stream. Artifacts + 272MB
cassettes committed. Moonshot recharge pending (Toni). Next: tier-H
(first 5 locked seeds, ~100-event human audit by Toni), then C2b frames
extraction (F-E1..F-E4).

**Frames C2b machinery (2026-07-17, build step 9):** the S2 pipeline is
frame-aware behind the same Commit path — frame/promotion candidates,
frame+block+assertion on facts, frame on rules/sups, non-assertive skip,
quote homing, provisional frames for missed declarations (declaration
upgrades them), per-frame join-explosion hygiene. New pieces:
`loom.FramesDeterministicExtractor` (tier-E control), `loom.FramesLLMExtractor`
(the condition under test), `harness.C2bProvCondition` (the §9.6.3 null:
frame-blind extraction + span-metadata query-time filtering),
`loom-c2b-frames`/`c2b-prov`/`loom-c2b`(FrameBlind) registrations, frame-name
handle plumbing (`-handles auto`, query text presented in handle vocabulary,
uniform for all conditions), F-E2 cue partition (scorer-side, fixed lexical
rule set shared with the null), cmd/fidelity F-E3 frame confusion, cmd/aggregate
`-frames` (F-E1/F-E2/F-E4 arithmetic), scripts/frames-e-run.sh driver. All
operational definitions registered in MASTERPLAN §10 (2026-07-17) BEFORE any
measured LLM run. VERIFIED LLM-free: `loom-c2b-frames-det == frame-oracle` in
every cell on ALL 20 locked seeds (tier E), fresh frame-oracle == frozen
§9.6.5 per cell, v0 sample-dataset diagnostics byte-identical, fidelity
1.000/1.000 + frame macro-F1 1.000 on dev seed 99.

**S2 hooks that exist but are intentionally dormant (don't mistake for bugs):**
lifecycle states `proposed`/`retracted`/`quarantined` are defined and honored
by `worldView`/`StatsAt`, but nothing sets them yet — structured ingest
commits everything `active`; the compilation pipeline (S2) is what will use
them. `Diff` and `StatsAt` are implemented but not yet called by any cmd or
condition. `Save`/`Load` exist but the harness rebuilds the store per run.

## 5. Harness — how measurement works

- `harness.Condition`: `Name / Ingest(episodes) / AnswerHolds / AnswerFind`.
  Conditions receive **SanitizedQuery** only — no answers, traces,
  provenance, or slice labels. Never weaken this.
- Scoring per slice with positives/negatives separated; revision reports
  flip vs retained vs **stale-agreement** (wrong answers that match the
  stale belief = the revision failure signature); find scored exact-set +
  micro-P/R/F1; composition also bucketed by derivation depth.
- **Diagnostic conditions** (LLM-free; run them after ANY harness change —
  if the pattern below breaks, fix the harness before measuring anything):

```
condition             rep+   rep-   comp+  comp-  rev.flip rev.ret
always-true           30/30  0/28   40/40  0/33   0/6      6/6
always-false          0/30   28/28  0/40   33/33  6/6      0/6
episode-grep          30/30  28/28  0/40   33/33  6/6      0/6   ← right-for-wrong-reason on flips; retained controls catch it
stale-oracle          30/30  28/28  40/40  33/33  0/6      6/6   ← fails EXACTLY the flips
oracle                100% everywhere (ceiling; anything less = harness bug)
loom-C2a              == oracle (S1 exit)
```

  (Find column, seed 1234: oracle and loom-C2a 10/10; stale-oracle 1/10 —
  not 0, because a flip occasionally alters a find answer-set; the constant
  and grep conditions 0/10. Expected, not a bug.)

- **Provenance probe** (LLM-free RAG ceiling): retrieve top-k by query
  text; measure coverage of the query's provenance episodes. An LLM cannot
  combine episodes retrieval never gave it. Current BM25 numbers (seed
  1234): composition full-coverage 2/50 @k=4 → 8/50 @k=16; **revision 0/12
  at every k** — the supersession notice shares no vocabulary with the
  question it invalidates. This is the measured mechanism behind "RAG fails
  revision." Open question the tmr run must answer: do semantic embeddings
  close this gap? (Prediction: partially on composition, barely on
  revision.)

## 6. Baselines & adapters

- **C1a = tmr** (Toni's tiny-mem-rag: file-native memos, SQLite+FTS5,
  semantic/lexical/hybrid-RRF). Primary baseline — fast, reproducible,
  single binary. Flow: `cmd/memoexport -dir <dataset> -out <folder>` →
  `tmr init` + `tmr ingest` (needs BABYLON_EMBED_KEY on vAI infra) →
  harness with `HARNESS_TMR_BIN`/`HARNESS_TMR_DIR`. **Check
  `TmrRetriever`'s lenient JSON parse against tmr's real envelope once.**
- **C1b = DeepR/HyperRAG** (vAI production RAG): confirmation pass before
  any conclusion. The kill criterion's "C1" = **strongest C1 measured**.
- **LLM**: `harness.LLMClient` iface + OpenAI-compatible impl. Env:
  `HARNESS_LLM_BASE_URL`, `HARNESS_LLM_MODEL`, `HARNESS_LLM_KEY`,
  `HARNESS_RAG_K` (default 8). Self-skips when unset. On vAI infra this
  points at vLLM boxes; go-vaibstract adapter is fine later but keep this
  module dependency-free.
- **C3 = LoRA**: not started. Plan: fine-tune adapters on episode text
  (same stream), same query protocol, retrain-vs-transfer on swap.

## 7. Pre-registered kill criterion (do not soften)

> If C2b does not beat the strongest C1 on the **composition** slice by
> **≥15pp** at equal-or-better **repetition** performance, the
> compiled-substrate bet in its v0 form is falsified. Geometry does not get
> to rescue it.

Negative results get written up honestly. This discipline is the residue of
the grant's pre-registration commitment and it is non-negotiable.

## 8. Hard-won lessons (encode these in any new code)

1. **Seeding density and rule discriminativeness are adversarial.**
   Enriching the fact base makes weak rules fire everywhere. A rule that
   explains everything explains nothing. The generator measures **firing
   ratios** (derived atoms / possible groundings) and repairs >0.5 by
   grounding the rule's **hub variable** (non-conclusion var joining most
   conditions) to a constant. Loom S2's compiler MUST apply the same
   hygiene to rules it extracts (spec §5.5).
2. **Disconnected rule conditions = Cartesian-product joins.** Generation
   enforces condition connectivity; the oracle has a 200k-binding guard
   that fails loudly (`JoinExplosionError` with rule ID) — never let exact
   evaluation silently hang or sample.
3. **Revision-pair integrity**: old and new rules must keep IDENTICAL
   conditions (they differ only by the added exception). Any repair/
   tightening must be applied to both partners identically or flip/retain
   ground truth silently corrupts (`tightenRuleByID` does this).
4. **Retained controls earn their keep**: episode-grep scores 6/6 on flips
   *for the wrong reason* (it answers false to everything derived). Paired
   controls expose it. Keep pairing in any new slice design.
5. **Dataset quality is machine-checked, not eyeballed**: manifest.json
   carries firing ratios, closure depth histogram, over-firing relations.
   For batch generation, gate on these (reject flat-depth or mostly-over-
   firing seeds) rather than tuning the generator to perfection.
6. **Vet catches real bugs** (shared JSON tags on multi-field lines). Run
   `gofmt -l . && go vet ./...` before every commit; both must be clean.

## 9. How to run everything

```sh
cd ~/code/loom/synthworld
go build ./...
go run ./cmd/synthgen -seed 42 -out /tmp/ds42 -preset small   # or medium
go run ./cmd/validate -dir /tmp/ds42                           # must print OK
go run ./cmd/harness  -dir /tmp/ds42 [-json report.json]       # tables + probe
go run ./cmd/memoexport -dir /tmp/ds42 -out /tmp/memos         # for tmr
# multi-seed regression (all must generate, validate OK, C2a == oracle):
for s in 42 7 99 1234 2026; do ...; done
```

## 10. Backlog, in dependency order

1. **[vAI infra, blocked here] C1 live runs**: memoexport → tmr ingest →
   harness with HARNESS_LLM_* (+ optionally HARNESS_TMR_*). Deliverables:
   tmr-semantic/hybrid provenance-probe numbers (does embedding retrieval
   beat BM25's ceiling?) and first real C1 rows in the table. Also wire a
   `TmrRetriever`-based probe into cmd/harness behind the env flag
   (currently only BM25 is probed).
2. **Loom S2 — text-mode compilation (C2b)**, the real uncertainty. Build
   in `loom/` behind the same Commit path (spec §5): extraction (LLM,
   schema-prompted, per-event candidates w/ confidence + source span),
   normalization (exact alias first; embedding-assist later), consistency
   (duplicate/refinement/conflict/supersession-candidate), rule handling
   (compile STATED rules; exception-proposal hook behind a flag; NO
   induction in v0), hygiene gate (connectivity, join-cost precheck,
   post-commit firing check → quarantine). Score **compilation fidelity**
   (P/R of compiled items vs world.json — allowed for SCORING only) per
   item type, per seed. C2b − C2a = price of extraction.
3. **C2 planner adapter** for natural-language-only querying (LLM maps
   q.Text → op calls; measure planner validity separately). For synthworld
   the structured Atom/Pattern is in the SanitizedQuery, so the
   deterministic planner is legitimate for C2a/C2b; the NL planner becomes
   mandatory for real-domain evals.
4. **C3 LoRA condition** (vAI infra): same episode stream, adapter
   training, same queries; document retrain cost on swap.
5. **The swap experiment**: run all conditions with model A (e.g.
   Qwen3.5), swap to model B (e.g. Mistral), re-run identical queries.
   Report per-slice **transfer retention ratio** (perf_B / perf_A) and
   **substrate lift** (C2 − C0 per model). C0 = no-memory condition (LLM
   answers from the question alone — build it; trivially, it should floor
   near always-false on positives).
6. **Batch stats**: 20+ seeds with quality gates; mean ± CI per slice per
   condition; this is the table that goes in any writeup.
7. **Repo split** (when S2 stabilizes): `loom` to its own repo with journal
   + Postgres adapter + REST/MCP (Conduit-compatible), synthworld stays the
   public instrument. Follow ~/code/code_guidelines.md there (go-cuserr,
   nanoIDs, vaiconfig, slog, file-naming) — synthworld itself stays
   stdlib-pure.

## 11. Working agreements

- Motto: **"Excellence. Always."** Concretely here: validator green before
  claiming anything; diagnostics pattern intact after harness changes;
  determinism preserved; negative results reported with the same care as
  positive ones; never let a condition see ground truth; never soften §7.
- Toni is a Go expert and CEO — write production-grade Go, explain
  decisions crisply, flag trade-offs honestly, don't pad.
- When numbers are surprising, suspect the harness first (that's what the
  diagnostic conditions are for), the generator second, the thesis last —
  but if the thesis loses fairly, it loses. That's the point.
