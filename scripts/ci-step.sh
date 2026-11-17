#!/usr/bin/env bash
# Runs one command as a GitHub Actions job's step (research D41, T110, T126): its output as it is,
# in the job's log only; it writes nothing to the job's summary, since the run's summary
# (scripts/ci-summary.py) holds what a job would. Its time goes to $GITHUB_OUTPUT as `seconds`,
# and on failure its first error line as `error`, for that run summary.
# Exits as the command does. Without GITHUB_OUTPUT it only runs the command.
#
#   scripts/ci-step.sh <name> <command> [args...]
set -u
[ $# -ge 2 ] || { echo "usage: scripts/ci-step.sh <name> <command> [args...]" >&2; exit 2; }
name=$1; shift
start=$(date +%s)
log=$(mktemp)
"$@" 2>&1 | tee "$log"
rc=${PIPESTATUS[0]}
seconds=$(( $(date +%s) - start ))
if [ -n "${GITHUB_OUTPUT:-}" ]; then
  echo "seconds=$seconds" >>"$GITHUB_OUTPUT"
  if [ "$rc" -ne 0 ]; then
    # The first line that names a failure, else the last line of output.
    err=$(grep -m1 -E '^(--- )?FAIL|(^|[^a-z])(error|Error|ERROR)[: ]|refused|cannot ' "$log" || true)
    [ -n "$err" ] || err=$(tail -n 1 "$log")
    echo "error=$(printf '%s' "$err" | head -n 1 | cut -c1-200)" >>"$GITHUB_OUTPUT"
  fi
fi
rm -f "$log"
exit "$rc"
