#!/bin/sh
# Finds credentials by pattern (one home for the patterns: check.sh's secrets step and the evals
# job's scan before its upload, research D46, T122). Prints what it examined and, on a match, only
# the matching files' paths, never the matched text (CI logs are public). Exit 1 on a match.
#
#   scripts/secret-scan.sh [DIR]   DIR: every file under it, binary included; none: the tracked
#                                  files of the git repository in the current directory
set -u
PATTERNS='sk-ant-[A-Za-z0-9_-]{20,}|ghp_[A-Za-z0-9]{30,}'
if [ $# -ge 1 ]; then
  echo "secret-scan: examined $1"
  if [ ! -e "$1" ]; then echo "secret-scan: $1 does not exist: nothing to scan"; exit 0; fi
  found=$(grep -RlaE "$PATTERNS" -- "$1"); rc=$?   # -R follows links, as the upload does
else
  echo "secret-scan: examined the tracked files of $(pwd)"
  found=$(git grep -laE "$PATTERNS" -- .); rc=$?
fi
if [ -n "$found" ]; then
  echo "secret-scan: a credential pattern matches in:"
  printf '%s\n' "$found"
  exit 1
fi
# grep: 1 is no match; anything else is a file it could not read, so the scan is not clean.
if [ "$rc" -gt 1 ]; then echo "secret-scan: could not scan every file (exit $rc)"; exit 2; fi
echo "secret-scan: no credential pattern matches"
