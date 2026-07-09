# Loom — Compiled Agent Memory, Substrate v0 Specification

*Working name "Loom" (weaves episodes into fabric); rename at will.*
*Status: draft for internal review. Standalone by design — no Engram, no
vaieco dependencies in v0. Companion to `DESIGN.md` (synthworld) and the
C0–C3 evaluation harness.*

---

## 1. Claim under test

An agent's accumulated experience can be **compiled** into structured
knowledge that (a) survives fact revision, (b) answers queries whose answers
were never stored, by combining stored items, and (c) transfers across LLM
swaps with measured, small loss. The conjunction is the claim:

- **C1 episodic RAG** has portability trivially and accumulation weakly
  (documents, not knowledge); it cannot compose and cannot revise reliably.
- **C3 LoRA** accumulates and composes somewhat; it does not transfer.
- **C2 Loom** must show all three, and the honest phrasing is operational:
  *performance on held-out compositional and revision queries survives a
  model swap.* Nothing more is claimed.

## 2. Scope and non-goals (v0)

**In scope:** typed symbolic store + deterministic evaluator + LLM-driven
compilation from episode text + operation API + auxiliary embedding index
for entity/relation aliasing. Single-tenant, single-domain, file/Postgres
persistence, Go stdlib-first.

**Out of scope, deliberately:** Gaussian/region geometry (a Stage-2 question
the benchmark must earn), rule *induction* from repeated observations (v0
compiles rules that episodes *state*; induction is flagged as v0.5 stretch —
see §5.4), multi-tenant auth, Engram/Thalamus integration, incremental
closure maintenance (v0 recomputes; a perf item, not a semantics item).

## 3. Architectural commitments

1. **The evaluator is the synthworld oracle.** Stratified closure,
   precedence (authority → recency → specificity → ID), exceptions,
   conditional supersession, derivation traces — imported as a library, not
   reimplemented. Substrate failures are therefore compilation failures by
   construction; the inference layer is shared with the instrument that
   scores it.
2. **The authoritative store is symbolic and model-free.** No artifact of
   the reasoning LLM (activations, embeddings, logits) enters the
   authoritative layer. The embedding index is auxiliary and *rebuildable*:
   swap the encoder, re-embed, lose nothing. This is the portability
   contract (§7), enforced structurally rather than promised.
3. **Provenance is mandatory.** Every stored item carries the episode IDs
   that produced it, the extraction confidence, and its lifecycle state. An
   item without provenance cannot be committed.
4. **Compiled knowledge is subject to hygiene** (§5.5). The generator work
   taught us: a rule that explains everything explains nothing. Loom applies
   the same firing-ratio / connectivity / join-cost checks to knowledge it
   compiles that synthworld applies to knowledge it generates.

## 4. Data model

Mirrors `synthworld/world` semantics, generalized:

- **EntityType / Entity** — typed identities. v0 adds an **alias table**
  (surface forms → entity ID, embedding-assisted; §5.2).
- **RelationSchema** — named, typed slots, arity ≥ 2, stratum. v0 schema is
  *seeded* per domain (for the experiment: derived from the dataset
  manifest's relation vocabulary — names only, never facts or rules) and may
  grow by compilation proposing new relations (quarantined until confirmed;
  §5.3).
- **Fact** — ground atom, validity interval `[from, to)`, source, provenance
  (episode IDs, confidence), lifecycle.
- **Rule** — conditions / conclusion / exceptions / authority / issued /
  effective interval, provenance, lifecycle. Safety and stratification
  validated on commit (reusing `world.Validate` logic).
- **Supersession** — `(new, old, condition?, from)`, provenance.
- **Lifecycle** — `proposed → active → superseded | retracted | quarantined`.
  Nothing is deleted; the evaluator sees `active` (supersessions applied at
  eval time as in the oracle), audits see everything.

## 5. The compilation loop (the novel surface)

Per episode, five stages. Each stage emits a machine-readable report; the
concatenation is the **compilation trace** — the substrate's equivalent of a
derivation trace, and the object audited when extraction fidelity is scored.

### 5.1 Extraction
LLM maps episode text → candidate items (facts, rules, supersessions) in the
store's JSON schema. Two modes: **structured bypass** (episode carries
payloads — synthworld easy mode — extraction is a parser, LLM unused) and
**text mode** (hard mode; the LLM is prompted with the schema, current
relation vocabulary, and few-shot examples). Every candidate carries a
confidence and the source span.

### 5.2 Normalization
Entity resolution: exact match on alias table, else embedding nearest-
neighbor over aliases with a conservative threshold, else new-entity
proposal. Relation mapping likewise. Temporal normalization (day indices in
the experiment; ISO dates in production domains).

### 5.3 Consistency check
Candidate vs. store, four verdicts: **duplicate** (drop, merge provenance),
**refinement** (narrower validity/conditions than an existing item —
commit alongside), **conflict** (contradicts an active item at overlapping
validity — commit both, flag; precedence resolves at eval time, mirroring
the oracle), **supersession candidate** (episode language marks replacement
— create Supersession linking old and new). Unresolvable schema mismatches →
`quarantined`, surfaced in `stats()`.

### 5.4 Rule handling
v0 compiles rules **stated** in episodes (policy text → Rule). It does NOT
induce rules from co-occurring facts. One narrow inductive hook ships
behind a flag: **exception proposal** — when a committed fact contradicts an
active rule's conclusion for a specific binding, propose an exception for
review rather than silently losing either. This is the minimal version of
"experience refines rules" and is measurable on the revision slice.

### 5.5 Hygiene gate
Before a rule activates: connectivity check (conditions form a connected
variable graph), join-cost bound (evaluator's binding guard as pre-check),
and after commit, a firing-ratio measurement against the current store —
ratio > threshold ⇒ `quarantined` + report. Identical logic to synthworld's
repair loop, minus the repair: Loom flags, humans or higher-authority
episodes fix.

## 6. Operation API

REST + CLI in v0; MCP server in v0.5 (Conduit-compatible, still standalone).
Envelope `{data, error, meta}`; ops endpoints per vaieco convention. All
read operations take `t` (eval time) and return traces.

| op | signature | notes |
|---|---|---|
| `holds` | (atom, t) → bool + derivation | oracle closure |
| `find` | (pattern, 1 free slot, t) → satisfiers + derivations | |
| `applicable_rules` | (relation or entity, t) → rules + status | authority, supersession state |
| `explain` | (atom, t) → full derivation tree | replayable |
| `diff` | (t1, t2) → items changed, flips implied | revision awareness |
| `ingest` | (episode) → compilation trace | §5 |
| `assert` / `retract` | (item, authority) → lifecycle change | manual override, provenance "operator" |
| `stats` | () → firing ratios, quarantine, conflicts, coverage | store health |

The **planner** (question → operation sequence) is *not* part of Loom: it
lives in the harness's C2 adapter, is LLM-specific by nature, and measuring
it separately is the point (§8).

## 7. Portability contract

Transfer = copy store + re-point adapter. Formally: the authoritative store
contains no model-derived artifacts (§3.2); the embedding index is derived
state, rebuildable from any encoder over the alias table in O(aliases).
A model swap therefore changes exactly two things: the extraction LLM
(affects future compilation only) and the planner LLM (affects query
decomposition). The experiment isolates both: swap models, re-run the same
query set, report per-slice retention. If retention drops, the loss is
attributable — planner or nothing, never the knowledge.

## 8. Failure-mode decomposition (measured separately)

1. **Compilation fidelity** — precision/recall of compiled items vs. the
   synthworld ground-truth world. Possible *only* because the instrument
   knows the true world; this is the reason the synthetic domain exists.
2. **Operation validity** — did the evaluator execute correctly. Determinis-
   tic, trace-checkable, shared code with the oracle: expected ~100%; any
   deviation is a bug, not a result.
3. **Semantic soundness** — do answers match ground truth end-to-end
   (compilation × planning × evaluation).
4. **Planner validity** — did the LLM choose the right operations (scored
   against the query's known type/slice).

C2a (structured ingest) vs C2b (text ingest) separates compilation loss
from reasoning gain; C2b − C2a is the price of extraction.

## 9. Implementation sketch

Own repo (`loom`), Go, stdlib + `synthworld/oracle` + `synthworld/world` as
the semantic core (extract these into a shared module or vendor them —
decision at repo split). Storage v0: single JSON/JSONL store dir with
write-ahead journal (experiment scale); Postgres/JSONB adapter behind the
same interface for production. Embedding index: pluggable `Encoder`
interface; v0 default = none (exact aliasing suffices for synthworld),
production = any embedding endpoint.

Packages: `store` (items, lifecycle, journal), `compilepipe` (§5 stages,
LLM client behind an interface), `api` (REST + CLI), reuse `oracle`.

## 10. Milestones

- **S1 (week 1):** store + lifecycle + evaluator wiring + structured ingest
  (C2a complete). Exit: C2a answers synthworld queries; compilation
  fidelity = 1.0 by construction on easy mode.
- **S2 (weeks 2–3):** text-mode extraction + normalization + consistency +
  hygiene (C2b complete). Exit: fidelity P/R reported on 5 seeds.
- **S3 (week 4):** planner adapter in harness, full C0–C3 run on model A,
  swap to model B, first transfer-retention numbers.

Kill criteria (pre-registered, same spirit as the grant's H-series):
if C2b does not beat C1 on the composition slice by ≥15pp at equal or
better repetition performance, the compiled-substrate bet in its v0 form is
falsified; geometry does not get to rescue it (§2).

## 11. Decisions log

- **2026-07-02 — C1 baseline is tmr, guarded.** C1a = tmr (file-native,
  single-binary, hybrid RRF retrieval): primary baseline for iteration and
  the reproduction package. C1b = DeepR/HyperRAG: confirmation pass before
  any conclusion is drawn. The kill criterion's "C1" means the **strongest**
  C1 measured — beating a weak retriever proves nothing. Adapter surface:
  `harness.Retriever` (tmr shell-out + `cmd/memoexport`), `harness.LLMClient`
  (OpenAI-compatible, points at any vLLM endpoint).
- **2026-07-02 — S1 complete.** Loom store + lifecycle + provenance +
  schema inference (strata from the rule dependency graph; nothing read from
  world.json) + structured ingest + evaluator wiring. C2a is strictly
  oracle-equal on 5/5 seeds, all slices, all depths.
- **2026-07-02 — retrieval ceiling measured (LLM-free).** BM25 provenance
  full-coverage on composition: 2/50 @k=4 → 8/50 @k=16; revision: 0/12 at
  every k — the supersession notice is lexically unreachable from the
  question. This is the mechanistic RAG failure prediction, quantified
  before any LLM run; semantic retrieval (tmr) must now show whether
  embeddings close the gap.
- **2026-07-09 — frames v1 mechanism committed (MASTERPLAN §9.6 is the
  binding pre-registration).** Minimal mechanism: `actual` +
  `perspective:<src>` (flat, non-inheriting; source≡perspective) +
  `scenario:<id>` (inherits actual; immutable per-frame
  `basis: live|pinned(d)`; re-pin = new frame) + explicit promotion op
  with append-only PromotionRecord (only ATTESTED items promotable in
  v1) + frame-attributed traces. Fiction/example deferred as first-class
  types (example = pinned no-promotion scenario). Speculation/prediction
  use existing confidence/lifecycle/validity machinery — no new
  mechanism. Precedence gains ONE leading key: frame proximity in the
  visibility cone, above authority→recency→specificity→ID. Frame-local
  fact removal = frame-scoped supersession/block (no new negation).
  Rule firing is visibility-monotone (home(R) ∈ cone(F)); ideation
  widens query scope with attribution, never merges closures;
  cross-frame import = scratch scenario + ImportRecord. Safety
  invariant: uncertain frame assignment routes to source-frame/
  quarantine, never `actual`. Lineage discipline (Cyc's lesson):
  exactly two audited crossings — inheritance down the DAG, explicit
  promotion up; every proposed third crossing is a lifting rule and is
  refused.
