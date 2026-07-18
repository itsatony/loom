#!/usr/bin/env bash
# frames-e-run.sh — frames-v1 endpoint driver (MASTERPLAN §9.6.7, F-E1..F-E4).
#
# For each locked frames seed, runs harness passes over the CERTIFIED tier-M
# stream (episodes_natural.jsonl, handles auto-resolved from the naturalize
# report) and merges them into one report.json per seed for cmd/aggregate
# -frames:
#   base       — LLM-free: diagnostics + frame-oracle + loom-C2a +
#                loom-c2b-frames-det (tier-E-inverse control, expected to
#                collapse on tier M — its collapse is REPORTED, not hidden)
#   c2bf       — loom-c2b-frames: THE frames condition (extraction thinking
#                OFF, registered C2b mode)
#   null       — loom-c2b (frame-blind confirm) + c2b-prov (the registered
#                query-time-filtering null); same extractor prompts, shared
#                cassettes
#   c1         — c0-no-memory + rag-bm25 baselines, THINKING ON (registered
#                C1 mode)
# All passes cassette-cached; reruns replay offline.
#
# Model selection comes from the environment (FRAMES_LLM_* falling back to
# the qwen defaults used for the primary 20-seed verdicts):
#   FRAMES_MODEL_TAG    cassette + results subdir tag (default qwen36)
#   HARNESS_LLM_MODEL / _BASE_URL / _KEY  when overriding the default qwen
#   FRAMES_C2B_EXTRA    extra-params JSON for the C2b extraction pass
#                       (default: qwen thinking-off kwargs)
#   FRAMES_C1_TEMP      temperature setting for the C1 pass (default none)
#
# Usage: scripts/frames-e-run.sh <datasets-root> <results-root> [seed ...]
set -euo pipefail

DATA_ROOT=${1:?datasets root}
OUT_ROOT=${2:?results root}
shift 2 || true

cd "$(dirname "$0")/.."
credval() { grep -m1 "^$1=" /home/itsatony/code/.creds | cut -d= -f2-; }

TAG=${FRAMES_MODEL_TAG:-qwen36}
if [ -z "${HARNESS_LLM_BASE_URL:-}" ]; then
  export HARNESS_LLM_BASE_URL="$(credval VAI_ANNOT_VLLM_URL)"
  export HARNESS_LLM_MODEL=qwen36-nvfp4
  export HARNESS_LLM_KEY="$(credval VAI_ANNOT_VLLM_API_KEY)"
fi
[ -n "$HARNESS_LLM_BASE_URL" ] || { echo "missing LLM endpoint"; exit 1; }

C2B_EXTRA=${FRAMES_C2B_EXTRA:-'{"chat_template_kwargs":{"enable_thinking":false}}'}
C2B_TEMP=${FRAMES_C2B_TEMP:-}
C1_EXTRA=${FRAMES_C1_EXTRA:-}
C1_TEMP=${FRAMES_C1_TEMP:-none}
CASS=${FRAMES_CASSETTES:-$HOME/code/loom/cassettes}
mkdir -p "$CASS"
export HARNESS_CONCURRENCY=${HARNESS_CONCURRENCY:-8}
export HARNESS_RAG_RETRIEVER=bm25

if [ $# -eq 0 ]; then
  set -- $(ls -d "$DATA_ROOT"/seed-*/ | sed 's|.*/seed-\([0-9]*\)/|\1|' | sort -n)
fi

for SEED in "$@"; do
  DS="$DATA_ROOT/seed-$SEED"
  OUT="$OUT_ROOT/seed-$SEED"
  [ -f "$DS/episodes_natural.jsonl" ] || { echo "skip seed-$SEED (no naturalized stream)"; continue; }
  [ -f "$OUT/report.json" ] && { echo "seed-$SEED already done"; continue; }
  mkdir -p "$OUT"
  COMMON=(-dir "$DS" -episodes episodes_natural.jsonl -handles auto)

  echo "=== seed-$SEED base LLM-free ($(date +%H:%M:%S))"
  env -u HARNESS_LLM_BASE_URL -u HARNESS_LLM_MODEL \
    go run ./cmd/harness "${COMMON[@]}" -json "$OUT/report-base.json" \
    -conditions frame-oracle,mono-world,isolationist,literalist,loom-C2a,loom-c2b-frames-det \
    >"$OUT/base.log" 2>&1

  echo "=== seed-$SEED c2b-frames extraction ($(date +%H:%M:%S))"
  HARNESS_LLM_TEMPERATURE="$C2B_TEMP" HARNESS_LLM_EXTRA_PARAMS="$C2B_EXTRA" \
    HARNESS_LLM_CACHE="$CASS/$TAG-frames-c2bf" \
    go run ./cmd/harness "${COMMON[@]}" -json "$OUT/report-c2bf.json" \
    -conditions loom-c2b-frames >"$OUT/c2bf.log" 2>&1

  echo "=== seed-$SEED null + frame-blind ($(date +%H:%M:%S))"
  HARNESS_LLM_TEMPERATURE="$C2B_TEMP" HARNESS_LLM_EXTRA_PARAMS="$C2B_EXTRA" \
    HARNESS_LLM_CACHE="$CASS/$TAG-frames-null" \
    go run ./cmd/harness "${COMMON[@]}" -json "$OUT/report-null.json" \
    -conditions loom-c2b,c2b-prov >"$OUT/null.log" 2>&1

  # Answering pass (reasoning ON): C1 baselines + the frame-rag CEILING null,
  # which is a per-query reasoner, not an extractor.
  echo "=== seed-$SEED c1 + frame-rag ceiling ($(date +%H:%M:%S))"
  HARNESS_LLM_TEMPERATURE="$C1_TEMP" HARNESS_LLM_EXTRA_PARAMS="$C1_EXTRA" \
    HARNESS_LLM_CACHE="$CASS/$TAG-frames-c1" \
    go run ./cmd/harness "${COMMON[@]}" -json "$OUT/report-c1.json" \
    -conditions c0-no-memory,rag-bm25,frame-rag >"$OUT/c1.log" 2>&1

  python3 - "$OUT" <<'EOF'
import json, sys, os
out = sys.argv[1]
merged = []
for part in ("report-base.json", "report-c2bf.json", "report-null.json", "report-c1.json"):
    merged += json.load(open(os.path.join(out, part)))
json.dump(merged, open(os.path.join(out, "report.json"), "w"), indent=1)
print(f"merged {len(merged)} condition reports -> {out}/report.json")
EOF
  echo "=== seed-$SEED done ($(date +%H:%M:%S))"
done
echo "ALL DONE"
