#!/bin/sh
# Explicit entry point of the analyst evals (T025). Never run by scripts/check.sh's go test.
#   sh src/tests/evals/analyst/run.sh graders   graders against hand-written proposals; no model, no Docker
#   sh src/tests/evals/analyst/run.sh stub      the harness against a stub gateway; Docker, no key, no cost
#   sh src/tests/evals/analyst/run.sh live      the real suite: every case k times (default 3) through the gateway; COSTS MONEY
# `live` fails loudly (exit 2) when the key file is missing; it never skips. Env: EVAL_KEY_FILE,
# EVAL_CASES (comma list of either suite's cases), EVAL_TRIALS (k, 1 to 20), EVAL_REPORT_DIR,
# EVAL_RUN_FLAGS, EVAL_TRIAL_TIMEOUT (README.md).
set -eu
cd "$(dirname "$0")/../.."   # src/tests
mode=${1:-}
case "$mode" in
graders) exec go test -count=1 ./evals/... ;;
stub)    exec go test -count=1 -tags evals -run 'TestHarnessStub|TestGatewayStarts' ./evals/analyst ;;
live) ;;
*) echo "usage: run.sh graders|stub|live" >&2; exit 2 ;;
esac
KEY=${EVAL_KEY_FILE:-$HOME/.config/agentic-platform/anthropic_api_key}
if [ ! -s "$KEY" ]; then
  echo "run.sh live: the test-scoped key file $KEY is missing or empty; the live evals do not skip." >&2
  exit 2
fi
ID=$(date +%s)-$$
export EVAL_RUN_ID=$ID EVAL_KEY_FILE=$KEY
# Safety net if the test is killed: remove everything labelled with this run.
cleanup() {
  docker ps -aq --filter "label=ap.eval.run=$ID" | xargs -r docker rm -f >/dev/null 2>&1 || true
  docker network ls -q --filter "label=ap.eval.run=$ID" | xargs -r docker network rm >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM
go test -count=1 -timeout 60m -tags evals -run 'TestAnalystEvals' -v ./evals/analyst
