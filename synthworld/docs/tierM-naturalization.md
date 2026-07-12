# Tier-M naturalization (frames v1, MASTERPLAN §9.6.6)

Status: pipeline built 2026-07-11 (build step 6). This memo records the
operational design decisions behind `cmd/naturalize` and `cmd/authcert`;
the binding pre-registration is MASTERPLAN §9.6.6 and is unchanged.

## Model roster (verified live 2026-07-11)

All five models are OUTSIDE the evaluated matrix (qwen36-nvfp4 /
gpt-5-mini / claude-haiku-4-5); judges are disjoint from naturalizers, so
no model grades text it wrote.

| role | model | family | request shape |
|---|---|---|---|
| naturalizer A (even episodes) | mistral-medium-3.5 | Mistral | temp 0 |
| naturalizer B (odd episodes) | deepseek-v4-pro | DeepSeek | temp 0 |
| judge 1 | gemini-3.5-flash | Google | temp 0 |
| judge 2 | kimi-k2.6 | Moonshot | temp 1 (model rejects 0) |
| judge 3 | grok-4.20-0309-non-reasoning | xAI | temp 0 |

## Frame handles

Raw frame IDs (`fic_*`, `psp_*`, `scn_*`) are banned from tier-M text —
they are exactly the surface cues the authenticity certificate rules out.
Instead:

- fiction frames get a deterministic story TITLE ("The Glass Harbor"),
- scenario frames get a bare CODENAME ("Blueline" — deliberately no
  constant "Exercise" prefix, which would be a single lexical giveaway),
- perspective frames are referenced by their narrator entity (an ordinary
  identifier that also occurs in actual lines).

Handles are alphabetic-only (invisible to the preservation multisets) and
chosen by FNV hash of (dataset seed, frame ID) over fixed wordlists —
same dataset always naturalizes to the same names, different seeds get
different names. The frame→handle map is recorded in
`naturalize-report.json`; like world.json it is available for SCORING and
auditing only, never to a measured condition.

## Enforcement layers

1. **Mechanical content preservation** (`cmd/naturalize/validate.go`):
   the v0 paraphrase rules (identifier/number multisets, atom expressions
   verbatim, conditional/exception structure) extended with frame-ID
   exemption+ban, required handle/entity mentions per frame-homed line,
   structural guards (block-removal language, forecast forward-looking
   language, quote quotation marks), and a banned-marker vocabulary list.
   Fallback-to-original after retries is counted; >10% fallbacks
   invalidates the tier (a tier-E line inside tier M is a marker leak).
2. **Frame-recoverability audit** (`cmd/naturalize/judge.go`): the
   3-judge panel labels EVERY line of each naturalized episode with
   (context, type) — context ∈ {actual, story "T", view P, exercise "N"},
   type ∈ {statement, quote, sarcasm, declaration, confirmation}. Judges
   see only naturalized text: the target episode plus the naturalized
   declaration lines a sequential reader would have seen. A frame-bearing
   line passes at ≥2/3 exact agreement (actual|statement control lines
   gate on context only); failures feed back into re-naturalization
   (default 3 rounds). Operational gate: >5% unrecovered frame-bearing
   lines invalidates the tier. Actual-line context-miss rate is reported
   as the false-alarm control.
3. **Authenticity certificate** (`cmd/authcert`): an LLM-free surface-cue
   baseline — tier-E marker regexes + a multinomial naive-Bayes
   bag-of-words classifier (unigrams+bigrams; identifiers collapsed to
   type prefixes, digits to <num>) trained leave-one-seed-out on
   ground-truth line labels — answers the pooled contamination+isolation
   trap queries via atom-grep + per-line frame classification (+ the
   scenario-inheritance shortcut as steelman). Certified iff pooled
   balanced accuracy ≤ 0.65. Chain queries are a sub-line of isolation
   and are not double-counted. Single-seed dev runs fall back to a 2-fold
   split by episode parity (each line classified by the other fold's
   model) — the batch certificate always uses leave-one-seed-out.

## Tier-E calibration (pre-registered expectation, verified)

On dev seed 99, tier-E episodes.jsonl: marker rules fire on all 130
frame-marked lines, contamination balanced accuracy 1.000, pooled 0.847 >
0.65 → tier E correctly refused certification (non-evidence, as
registered). The certificate scaffold is therefore live: a tier passes
only by actually removing surface cues, not by scaffold weakness.
Isolation-chain positives are 0 for the baseline on every tier (grep
cannot derive), which is the honest ceiling of a surface-cue detector and
makes the pooled bound HARDER to stay under for contamination cues.

## Invariants

- Structured payloads, episode IDs, days are byte-identical between
  episodes.jsonl and episodes_natural.jsonl — C2a (easy mode) is the same
  on both streams by construction; verify with the harness after every
  naturalization run (loom-C2a == frame-oracle must reproduce).
- Cassette cache (NATURALIZE_CACHE) is mandatory; reruns replay.
- Naturalized text is committed to the repo as the dataset artifact
  (§9.6.11) together with naturalize-report.json and authcert-report.json.
