#!/usr/bin/env bash
# frames-swap-legB.sh — Leg B answering-swap contrast (MASTERPLAN §10
# 2026-07-19). Runs the LLM-BOUND baseline conditions (extractor-independent)
# under a chosen ANSWERING model over the locked tier-M datasets, one
# report.json per seed. The retention perf_B/perf_A across two answering
# models is then computed with `cmd/aggregate -swap -cond <condition>`.
#
# Usage: frames-swap-legB.sh <datasets-root> <out-dir> <conditions> [seed ...]
#   env: HARNESS_LLM_BASE_URL / _MODEL / _KEY  (answering model)
#        LEGB_CACHE (cassette dir), LEGB_EXTRA (extra params), LEGB_TEMP
set -euo pipefail
DATA_ROOT=${1:?datasets root}; OUT_ROOT=${2:?out dir}; CONDS=${3:?conditions}
shift 3 || true
cd "$(dirname "$0")/.."
[ -n "${HARNESS_LLM_BASE_URL:-}" ] || { echo "set HARNESS_LLM_BASE_URL/_MODEL/_KEY"; exit 1; }
export HARNESS_CONCURRENCY=${HARNESS_CONCURRENCY:-8}
export HARNESS_RAG_RETRIEVER=bm25
CACHE=${LEGB_CACHE:?set LEGB_CACHE}
if [ $# -eq 0 ]; then
  set -- $(ls -d "$DATA_ROOT"/seed-*/ | sed 's|.*/seed-\([0-9]*\)/|\1|' | sort -n)
fi
for SEED in "$@"; do
  DS="$DATA_ROOT/seed-$SEED"; OUT="$OUT_ROOT/seed-$SEED"
  [ -f "$DS/episodes_natural.jsonl" ] || { echo "skip seed-$SEED (no stream)"; continue; }
  [ -f "$OUT/report.json" ] && { echo "seed-$SEED already done"; continue; }
  mkdir -p "$OUT"
  echo "=== seed-$SEED answering [$CONDS] ($(date +%H:%M:%S))"
  HARNESS_LLM_TEMPERATURE="${LEGB_TEMP:-}" HARNESS_LLM_EXTRA_PARAMS="${LEGB_EXTRA:-}" \
    HARNESS_LLM_CACHE="$CACHE" \
    go run ./cmd/harness -dir "$DS" -episodes episodes_natural.jsonl -handles auto \
    -json "$OUT/report.json" -conditions "$CONDS" >"$OUT/run.log" 2>&1
  echo "=== seed-$SEED done ($(date +%H:%M:%S))"
done
echo "LEG-B ALL DONE"
