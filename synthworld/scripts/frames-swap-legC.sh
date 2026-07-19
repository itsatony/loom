#!/usr/bin/env bash
# frames-swap-legC.sh — Leg C fourth-family extractor (MASTERPLAN §10
# 2026-07-19): Anthropic claude-haiku-4-5 as a clean held-out extractor.
# Thin wrapper over frames-e-run.sh with the answering pass skipped (the
# op-planner is LLM-free; Leg C measures EXTRACTION portability only).
#
# Usage: frames-swap-legC.sh <datasets-root> <out-dir> [seed ...]
set -euo pipefail
DATA_ROOT=${1:?datasets root}; OUT_ROOT=${2:?out dir}; shift 2 || true
cd "$(dirname "$0")/.."
credval() { grep -m1 "^$1=" /home/itsatony/code/.creds | cut -d= -f2-; }
export FRAMES_MODEL_TAG=haiku45f
export HARNESS_LLM_BASE_URL=https://api.anthropic.com/v1
export HARNESS_LLM_MODEL=claude-haiku-4-5-20251001
export HARNESS_LLM_KEY="$(credval ANTHROPIC_API_KEY)"
export FRAMES_C2B_EXTRA='{"max_tokens":8192}'   # no qwen thinking kwargs; guarantee room for extraction JSON
export FRAMES_C1_CONDITIONS=skip                 # extraction-only leg
export HARNESS_CONCURRENCY=${HARNESS_CONCURRENCY:-6}
exec scripts/frames-e-run.sh "$DATA_ROOT" "$OUT_ROOT" "$@"
