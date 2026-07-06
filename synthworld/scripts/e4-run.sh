#!/usr/bin/env bash
# e4-run.sh — E4 head-to-head driver (MASTERPLAN §2/E4).
#
# For each seed dataset, runs three harness passes and merges them into one
# report.json per seed (the input format cmd/aggregate expects):
#   base  — LLM-free: diagnostics + oracle + loom-C2a + loom-c2b-det
#   c1    — C1 family + D6, THINKING ON (registered C1 mode; see
#           CAMPAIGN-LOG 2026-07-06 E2 v2/v3: thinking-off is not viable)
#   c2b   — loom-c2b, THINKING OFF for extraction (registered C2b mode;
#           extraction is schema-conformant parsing, measured perfect
#           without thinking; compile tokens are the H7 cost either way)
# All passes cassette-cached: reruns are free and offline-reproducible.
#
# Usage: scripts/e4-run.sh <datasets-root> <results-root> [seed ...]
#        (no seeds given = every seed-*/ under datasets-root, numeric order)
set -euo pipefail

DATA_ROOT=${1:?datasets root}
OUT_ROOT=${2:?results root}
shift 2 || true

cd "$(dirname "$0")/.."
# .creds is not a pure env file — pull exactly what we need.
credval() { grep -m1 "^$1=" /home/itsatony/code/.creds | cut -d= -f2-; }
VAI_ANNOT_VLLM_URL=$(credval VAI_ANNOT_VLLM_URL)
VAI_ANNOT_VLLM_API_KEY=$(credval VAI_ANNOT_VLLM_API_KEY)
[ -n "$VAI_ANNOT_VLLM_URL" ] && [ -n "$VAI_ANNOT_VLLM_API_KEY" ] || { echo "missing vLLM creds"; exit 1; }

CASS=${E4_CASSETTES:-$HOME/code/loom/cassettes}
mkdir -p "$CASS"

export HARNESS_LLM_BASE_URL="$VAI_ANNOT_VLLM_URL"
export HARNESS_LLM_MODEL=qwen36-nvfp4
export HARNESS_LLM_KEY="$VAI_ANNOT_VLLM_API_KEY"
export HARNESS_CONCURRENCY=${HARNESS_CONCURRENCY:-8}
export HARNESS_RAG_RETRIEVER=bm25 # strongest measured retriever (E1)

if [ $# -eq 0 ]; then
  set -- $(ls -d "$DATA_ROOT"/seed-*/ | sed 's|.*/seed-\([0-9]*\)/|\1|' | sort -n)
fi

for SEED in "$@"; do
  DS="$DATA_ROOT/seed-$SEED"
  OUT="$OUT_ROOT/seed-$SEED"
  [ -f "$DS/world.json" ] || { echo "skip seed-$SEED (no dataset)"; continue; }
  [ -f "$OUT/report.json" ] && { echo "seed-$SEED already done"; continue; }
  mkdir -p "$OUT"
  echo "=== seed-$SEED base ($(date +%H:%M:%S))"
  env -u HARNESS_LLM_BASE_URL -u HARNESS_LLM_MODEL \
    go run ./cmd/harness -dir "$DS" -json "$OUT/report-base.json" \
    -conditions always-true,always-false,episode-grep,stale-oracle,oracle,loom-C2a,loom-c2b-det >"$OUT/base.log" 2>&1

  echo "=== seed-$SEED c1 thinking-on ($(date +%H:%M:%S))"
  HARNESS_LLM_TEMPERATURE=none HARNESS_LLM_CACHE="$CASS/qwen36-think" \
    go run ./cmd/harness -dir "$DS" -json "$OUT/report-c1.json" \
    -conditions c0-no-memory,rag-bm25,c1c-longcontext,d6-perfect-retrieval >"$OUT/c1.log" 2>&1

  echo "=== seed-$SEED c2b thinking-off ($(date +%H:%M:%S))"
  HARNESS_LLM_EXTRA_PARAMS='{"chat_template_kwargs":{"enable_thinking":false}}' \
    HARNESS_LLM_CACHE="$CASS/qwen36-nothink" \
    go run ./cmd/harness -dir "$DS" -json "$OUT/report-c2b.json" \
    -conditions loom-c2b >"$OUT/c2b.log" 2>&1

  python3 - "$OUT" <<'EOF'
import json, sys, os
out = sys.argv[1]
merged = []
for part in ("report-base.json", "report-c1.json", "report-c2b.json"):
    merged += json.load(open(os.path.join(out, part)))
json.dump(merged, open(os.path.join(out, "report.json"), "w"), indent=1)
print(f"merged {len(merged)} condition reports -> {out}/report.json")
EOF
  echo "=== seed-$SEED done ($(date +%H:%M:%S))"
done
echo "ALL DONE"
