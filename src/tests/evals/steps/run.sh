#!/bin/sh
# Explicit entry point of the step agents' evals (T041). Never run by scripts/check.sh's go test.
#   sh src/tests/evals/steps/run.sh graders   graders and fixtures, no model, no Docker
#   sh src/tests/evals/steps/run.sh live      the real suite: every case 3 times through the gateway; COSTS MONEY
# `live` fails loudly (exit 2) when an agent definition or the key file is missing; it never skips.
# Env: EVAL_KEY_FILE, EVAL_CASES (case names or agent names, comma list), EVAL_REPORT_DIR,
# EVAL_RUN_FLAGS, EVAL_TRIAL_TIMEOUT, EVAL_AGENT_IMAGE (README.md).
set -eu
cd "$(dirname "$0")/../.."   # src/tests
mode=${1:-}
case "$mode" in
graders)
  # oapi-codegen (pinned, src/deploy/images.lock) builds the fixtures: AP_OAPI_CODEGEN, PATH or $HOME/go/bin.
  PATH="$PATH:$HOME/go/bin" exec go test -count=1 -run 'Grader|SelfTest' ./evals/steps ;;
live) ;;
*) echo "usage: run.sh graders|live" >&2; exit 2 ;;
esac
ROOT=$(cd ../.. && pwd)
for a in tests code review; do
  for f in "$a.yaml" "$a/instruction.md"; do
    if [ ! -s "$ROOT/src/agents/$f" ]; then
      echo "run.sh live: agent definition missing: src/agents/$f (T049); the live evals cannot pass before it exists." >&2
      exit 2
    fi
  done
done
KEY=${EVAL_KEY_FILE:-$HOME/.config/agentic-platform/anthropic_api_key}
if [ ! -s "$KEY" ]; then
  echo "run.sh live: the test-scoped key file $KEY is missing or empty; the live evals do not skip." >&2
  exit 2
fi
ID=$(date +%s)-$$
export EVAL_RUN_ID=$ID EVAL_KEY_FILE=$KEY EVAL_LIVE=1
# Safety net if the test is killed: remove everything labelled with this run.
cleanup() {
  docker ps -aq --filter "label=ap.eval.run=$ID" | xargs -r docker rm -f >/dev/null 2>&1 || true
  docker network ls -q --filter "label=ap.eval.run=$ID" | xargs -r docker network rm >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM
PATH="$PATH:$HOME/go/bin" go test -count=1 -timeout 4h -tags evals -run 'TestStepEvals' -v ./evals/steps
