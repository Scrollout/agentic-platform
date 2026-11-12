#!/bin/sh
# Lists one service's contract versions, oldest first: the one home of the version-ordering rule,
# used by scripts/generate.sh (the newest version) and scripts/contract-diff.sh (the newest two).
#
#   scripts/contract-versions.sh <repo-root> <service>
#
# Reads the version folders under <repo-root>/src/<service>/api/stable/ and .../api/preview/ and
# prints "<version>\t<path>" per folder that holds an openapi.yaml, <path> relative to <repo-root>.
# Ordering (ADR 0004 date versions): by date, then by stability. For the same date the preview
# version is older than the stable one, so preview/2026-11-12-preview < stable/2026-11-12, and
# both sort after anything from an earlier date. Byte order (LC_ALL=C), so the locale changes nothing.
# A folder under stable/ must be named YYYY-MM-DD, one under preview/ YYYY-MM-DD-preview; any other
# name (including a -preview folder under stable/ or a plain date under preview/) is an error.
# Exits 0 with the list (empty when the service has no contract), 2 on a misnamed version folder.
set -eu
export LC_ALL=C
[ $# -eq 2 ] || { echo "usage: contract-versions.sh <repo-root> <service>" >&2; exit 2; }
root=$1
service=$2
api="$root/src/$service/api"
date='[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'

bad=0
rows=""
nl='
'
for stability in preview stable; do
  [ -d "$api/$stability" ] || continue
  rank=0; [ "$stability" = stable ] && rank=1
  for dir in "$api/$stability"/* "$api/$stability"/.*; do
    [ -d "$dir" ] || continue
    version=$(basename "$dir")
    case $version in .|..) continue ;; esac
    case $stability:$version in
      preview:$date-preview) day=${version%-preview} ;;
      stable:$date) day=$version ;;
      *)
        if [ "$stability" = stable ]; then want="YYYY-MM-DD"; else want="YYYY-MM-DD-preview"; fi
        echo "contract-versions: src/$service/api/$stability/$version: a version folder under $stability/ must be named $want" >&2
        bad=1; continue ;;
    esac
    [ -f "$dir/openapi.yaml" ] || continue
    rows="$rows$day	$rank	$version	src/$service/api/$stability/$version/openapi.yaml$nl"
  done
done
[ $bad -eq 0 ] || exit 2
[ -n "$rows" ] || exit 0
printf '%s' "$rows" | sort -t '	' -k1,1 -k2,2n | cut -f3-
