#!/usr/bin/env bash
# e4-cleanup.sh — drive residual LLM errors to zero.
#
# The main sweep can leave a handful of queries that exhausted retries
# during a load spike. Because successful calls are cached, re-running a
# seed in auto mode re-hits ONLY the still-missing/errored queries. This
# loops each dirty seed until its merged report shows zero errors (or a
# max-round cap), so cmd/aggregate's error-exclusion guard drops nothing.
set -uo pipefail

DATA_ROOT=${1:?datasets root}
OUT_ROOT=${2:?results root}
MAX_ROUNDS=${3:-6}

cd "$(dirname "$0")/.."
credval() { grep -m1 "^$1=" /home/itsatony/code/.creds | cut -d= -f2-; }
export HARNESS_LLM_BASE_URL=$(credval VAI_ANNOT_VLLM_URL)
export HARNESS_LLM_KEY=$(credval VAI_ANNOT_VLLM_API_KEY)
export HARNESS_LLM_MODEL=qwen36-nvfp4
export HARNESS_CONCURRENCY=${HARNESS_CONCURRENCY:-6}
CASS=${E4_CASSETTES:-$HOME/code/loom/cassettes}

seed_errors() { # $1 = report.json
  python3 -c "import json,sys; print(sum((c.get('usage') or {}).get('errors',0) or 0 for c in json.load(open(sys.argv[1]))))" "$1" 2>/dev/null || echo 999
}

for SEED in $(ls -d "$DATA_ROOT"/seed-*/ | sed 's|.*/seed-\([0-9]*\)/|\1|' | sort -n); do
  OUT="$OUT_ROOT/seed-$SEED"; DS="$DATA_ROOT/seed-$SEED"
  [ -f "$OUT/report.json" ] || continue
  round=0
  while [ "$(seed_errors "$OUT/report.json")" -gt 0 ] && [ "$round" -lt "$MAX_ROUNDS" ]; do
    round=$((round+1))
    echo "=== seed-$SEED cleanup round $round (errors before: $(seed_errors "$OUT/report.json")) $(date +%H:%M:%S)"
    HARNESS_LLM_TEMPERATURE=none HARNESS_LLM_CACHE="$CASS/qwen36-think" \
      go run ./cmd/harness -dir "$DS" -json "$OUT/report-c1.json" \
      -conditions c0-no-memory,rag-bm25,c1c-longcontext,d6-perfect-retrieval >"$OUT/c1-cleanup.log" 2>&1
    HARNESS_LLM_EXTRA_PARAMS='{"chat_template_kwargs":{"enable_thinking":false}}' \
      HARNESS_LLM_CACHE="$CASS/qwen36-nothink" \
      go run ./cmd/harness -dir "$DS" -json "$OUT/report-c2b.json" \
      -conditions loom-c2b >"$OUT/c2b-cleanup.log" 2>&1
    python3 - "$OUT" <<'PY'
import json, sys, os
out=sys.argv[1]; merged=[]
for p in ("report-base.json","report-c1.json","report-c2b.json"):
    merged+=json.load(open(os.path.join(out,p)))
json.dump(merged, open(os.path.join(out,"report.json"),"w"), indent=1)
PY
  done
  echo "seed-$SEED final errors: $(seed_errors "$OUT/report.json")"
done
echo "CLEANUP DONE"
