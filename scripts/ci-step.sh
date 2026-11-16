#!/usr/bin/env bash
# Runs one command as a GitHub Actions job's step (research D41, T110): its output as it is, then a
# short summary in $GITHUB_STEP_SUMMARY: one line (`✅ <name> — <n> s`, or `❌ <name> — <n> s,
# exit <code>`), every `ap test` result line, one line of free disk, and, only when the command
# fails, its last 40 lines in a collapsed section. Its time goes to $GITHUB_OUTPUT as `seconds`,
# and on failure its first error line as `error`, for the run's summary (scripts/ci-summary.py).
# Exits as the command does. With neither variable set it only runs the command.
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
results='^(PASS|FAIL)  |^(planted|us2): [0-9]+ of [0-9]+'
if [ -n "${GITHUB_OUTPUT:-}" ]; then
  echo "seconds=$seconds" >>"$GITHUB_OUTPUT"
  if [ "$rc" -ne 0 ]; then
    # The first line that names a failure, else the last line of output.
    err=$(grep -m1 -E '^(--- )?FAIL|(^|[^a-z])(error|Error|ERROR)[: ]|refused|cannot ' "$log" || true)
    [ -n "$err" ] || err=$(tail -n 1 "$log")
    echo "error=$(printf '%s' "$err" | head -n 1 | cut -c1-200)" >>"$GITHUB_OUTPUT"
  fi
fi
if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
  {
    if [ "$rc" -eq 0 ]; then echo "✅ $name — $seconds s"
    else echo "❌ $name — $seconds s, exit $rc"; fi
    if grep -Eq "$results" "$log"; then
      echo; echo '```text'; grep -E "$results" "$log"; echo '```'
    fi
    echo; echo "Free disk: $(df -h / | tail -n 1 | tr -s ' ')"
    if [ "$rc" -ne 0 ]; then
      echo; echo '<details><summary>Last 40 lines</summary>'; echo; echo '```text'; tail -n 40 "$log"; echo '```'; echo '</details>'
    fi
    echo
  } >>"$GITHUB_STEP_SUMMARY"
fi
rm -f "$log"
exit "$rc"
