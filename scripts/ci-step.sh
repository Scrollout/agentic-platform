#!/usr/bin/env bash
# Runs one command as a GitHub Actions job's step (research D41): its output as it is, then a
# summary in $GITHUB_STEP_SUMMARY (the job, its result and time, the command, check.sh's time
# table or the last 40 lines, and the runner's free disk), and its time in seconds as the step
# output `seconds`. Exits as the command does. With neither variable set it only runs the command.
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
[ -z "${GITHUB_OUTPUT:-}" ] || echo "seconds=$seconds" >>"$GITHUB_OUTPUT"
if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
  {
    if [ "$rc" -eq 0 ]; then echo "### $name: passed in $((seconds / 60)) min $((seconds % 60)) s"
    else echo "### $name: failed (exit $rc) after $((seconds / 60)) min $((seconds % 60)) s"; fi
    echo
    echo "\`$*\`"
    echo
    # ap test's result lines (PASS or FAIL, two spaces, the scenario or test; and the total), all
    # of them, however long the output.
    if grep -Eq '^(PASS|FAIL)  |^(planted|us2): [0-9]+ of [0-9]+' "$log"; then
      echo '```text'; grep -E '^(PASS|FAIL)  |^(planted|us2): [0-9]+ of [0-9]+' "$log"; echo '```'; echo
    fi
    if grep -q '^== time per step' "$log"; then
      echo '```text'; sed -n '/^== time per step/,$p' "$log"; echo '```'
    else
      echo '<details><summary>Last lines</summary>'; echo; echo '```text'; tail -n 40 "$log"; echo '```'; echo '</details>'
    fi
    echo
    echo 'Free disk (`df -h /`):'; echo; echo '```text'; df -h /; echo '```'
  } >>"$GITHUB_STEP_SUMMARY"
fi
rm -f "$log"
exit "$rc"
