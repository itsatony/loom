# synthworld — Synthetic Hyper-Relational World Generator + Oracle

**Purpose.** Generate worlds of typed entities, n-ary time-scoped facts, and
Horn-style rules with exceptions, authority levels, and supersession — plus an
episode stream (how an agent *experiences* the world over time) and a query set
with oracle-verified ground truth. This is the measurement instrument for the
substrate experiment (conditions C0 no-memory / C1 episodic-RAG / C2 compiled
substrate / C3 LoRA), with three query slices:

- **repetition** — answer stated verbatim in some episode. RAG should tie; a
  substrate that loses here is destroying information at compile time.
- **composition** — answer requires combining k ≥ 2 items introduced in
  *different* episodes; never co-stated. The go/no-go slice.
- **revision** — a later episode superseded an earlier belief. Ground truth
  differs from the stale answer. Paired with *retained* controls (bindings the
  supersession does NOT affect) to punish over-revision as well.

The system under test only ever sees `episodes.jsonl`. `world.json` and query
ground truth exist for scoring only.

## 1. World model

- **Timeline**: integer days `0..T`. Evaluation happens at `t_eval` (default `T`).
- **Entities**: typed, e.g. `customer_007 : Customer`.
- **Relations**: named slots, each slot typed; arity 2–4. Each relation has a
  **stratum**:
  - stratum 0 = *base* relations — populated only by observed facts;
  - stratum ≥ 1 = *derived* relations — populated only by rules whose
    conditions reference strictly lower strata (stratification ⇒ the oracle is
    deterministic and terminates; no cyclic non-monotonicity).
- **Base fact**: ground atom + validity interval `[from, to)` (`to = 0` ⇒ open)
  + source + the episode that revealed it.
- **Rule**: `IF c1 ∧ … ∧ cm THEN q`, pattern atoms over shared variables;
  conclusion variables must appear in conditions (safe rules). Plus:
  - **Exceptions**: pattern list; if satisfiable under the firing binding
    (extra variables allowed, checked against the current fact set), the rule
    does not fire for that binding.
  - **Authority** ∈ 1..5, **IssuedAt** day, **Effective** `[from, to)`.
  - **Polarity**: assert (v0.1 generator emits assert-only) or block
    (schema+oracle support precedence conflicts; generation deferred).
- **Supersession**: `(new, old, condition?, from)`. From day `from`, rule `old`
  no longer fires — totally, or only for bindings where `condition` is
  satisfiable (conditional supersession).

## 2. Oracle semantics

`Closure(W, t)`:
1. Seed with base facts valid at `t`.
2. For each stratum `s = 1..S`, repeat to fixpoint within the stratum:
   for every rule concluding at stratum `s`, active at `t`
   (`effective_from ≤ t < effective_to`, and not fully superseded at `t`),
   enumerate bindings satisfying all conditions against the current fact set;
   drop bindings where an exception is satisfiable or a conditional
   supersession matches; emit candidate `(ground atom, polarity, rule, binding)`.
3. Per ground atom, resolve candidates by **precedence**: higher authority →
   later `IssuedAt` → more conditions (specificity) → rule ID (total order,
   determinism). Winner asserts ⇒ atom holds; winner blocks ⇒ atom is
   unattainable at this stratum.
4. Every derived atom carries a **derivation trace**: rule ID, binding, and
   supports (base-fact refs or sub-derivations). **Depth**: base fact = 0,
   rule application = 1 + max(support depth).

`StaleClosure(W, t)` = identical, but ignoring all supersessions — “the world
as an agent that missed every revision would believe it.” Revision queries are
those where `Closure` and `StaleClosure` disagree.

## 3. Episodes

Chronological groups of events: fact observations, rule issuances,
supersession notices, and distractors (near-miss facts in unused relations,
expired-rule chatter). Each episode has a day, structured payload, and
templated natural-language text (what C1 ingests; C2's compiler may use either
— parsing fidelity is part of what C2 is being tested on).

Composition chains are deliberately scattered: the supporting facts of a
seeded derivation are assigned to different episodes, days apart.

## 4. Queries (all oracle-verified at generation time)

| slice | construction | fields |
|---|---|---|
| repetition | base fact valid at t_eval, stated in an episode; + perturbed-argument negative controls | `holds` → bool |
| composition | derived atom with depth ≥ 2 and episode-provenance spread ≥ 2; + near-miss negatives verified false; + `find` queries (enumerate satisfiers of a pattern) | `holds` → bool, `find` → entity set |
| revision | binding where supersession flips the answer (`stale_answer ≠ answer`) **and** a retained-control binding where it doesn't | `holds` → bool + `stale_answer` |

Every query records: slice, day, depth, provenance episodes, serialized
derivation trace (for trace-checkable scoring later), and human-readable text.

## 5. Guarantees enforced by `cmd/validate`

1. Re-running the oracle on `world.json` reproduces every query's ground truth.
2. Every revision "flip" query disagrees with its stale answer; every retained
   control agrees.
3. Every composition query's provenance spans ≥ 2 episodes and its trace
   replays against the closure.
4. Every fact/rule referenced by any trace was revealed in some episode at or
   before the query day (no oracle-only knowledge leaks into ground truth).

## 6. Non-goals (v0.1)

Probabilistic/graded truth, hyperbolic anything, natural-language variety
beyond templates, block-polarity generation, multi-checkpoint evaluation
(schema supports `at_day`; generator emits one checkpoint). All deliberately
deferred: the instrument must be boring and correct before the subject gets
interesting.
