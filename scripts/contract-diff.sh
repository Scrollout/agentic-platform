#!/bin/sh
# Fails when a service's newest contract version breaks its previous version (Principle XV).
#
#   scripts/contract-diff.sh [<repo-root>]
#
# Finds every contract under <repo-root>/src/<service>/api/{stable,preview}/<version>/openapi.yaml
# (default root: this repository) and orders each service's versions with
# scripts/contract-versions.sh, the rule scripts/generate.sh also uses: by date, then preview before
# stable on the same date (so stable/2026-11-12 is newer than preview/2026-11-12-preview).
# Only the newest two versions are compared: oasdiff `breaking --fail-on ERR` from the previous
# version to the newest. Older pairs were checked when their newer version was added.
# oasdiff resolves the contracts' relative file `$ref`s itself, so nothing is bundled.
# It prints one line per service examined, plus oasdiff's report for each pair compared.
#
# Exits 0 when nothing breaks (also when src/ holds no contracts), 1 on a breaking change, and 2
# when a check could not be made: the root or its src/ is missing, a version folder is misnamed, or
# oasdiff cannot compare a pair. Every service is still examined and reported. When one service
# breaks (1) and another cannot be compared (2), the exit is 2: a contract nobody checked could hide
# any break, so an incomplete check outranks a known one.
set -eu
export LC_ALL=C
repo=$(cd "$(dirname "$0")/.." && pwd)
root=$(cd "${1:-$repo}" 2>/dev/null && pwd) || { echo "contract-diff: no such directory: ${1:-$repo}" >&2; exit 2; }
[ -d "$root/src" ] || { echo "contract-diff: $root has no src directory, so there is nothing to check" >&2; exit 2; }
OASDIFF_IMAGE=$(awk '$1=="oasdiff"{print $2}' "$repo/src/deploy/images.lock")
[ -n "$OASDIFF_IMAGE" ] || { echo "contract-diff: no oasdiff pin in src/deploy/images.lock" >&2; exit 2; }

# One service name per line; split on newlines only, so a name with spaces stays whole.
IFS='
'
set -f
services=$(cd "$root" && find src -mindepth 5 -maxdepth 5 -path 'src/*/api/*/*/openapi.yaml' \
  \( -path 'src/*/api/stable/*' -o -path 'src/*/api/preview/*' \) | cut -d/ -f2 | sort -u)
[ -n "$services" ] || { echo "contract-diff: no contracts under $root/src/*/api/{stable,preview}/"; exit 0; }

breaks=0
unchecked=0
for service in $services; do
  # "<version>\t<path>", oldest first; the newest two are compared.
  versions=$(sh "$repo/scripts/contract-versions.sh" "$root" "$service") || {
    echo "$service: version folders cannot be ordered, not compared" >&2; unchecked=1; continue; }
  if [ "$(printf '%s\n' "$versions" | wc -l)" -lt 2 ]; then
    echo "$service: one version, nothing to compare"
    continue
  fi
  base=$(printf '%s\n' "$versions" | tail -2 | head -1)
  rev=$(printf '%s\n' "$versions" | tail -1)
  pair="$service: $(echo "$base" | cut -f1) -> $(echo "$rev" | cut -f1)"
  rc=0
  docker run --rm --network none --read-only --user "$(id -u):$(id -g)" -v "$root/src:/repo/src:ro" -w /repo \
    "$OASDIFF_IMAGE" breaking "$(echo "$base" | cut -f2)" "$(echo "$rev" | cut -f2)" \
    --fail-on ERR --format text --color never </dev/null || rc=$?
  case $rc in
    0) echo "$pair: no breaking change" ;;
    1) echo "$pair: breaking change (give it a new version, Principle XV)"; breaks=1 ;;
    *) echo "$pair: oasdiff could not compare (exit $rc)" >&2; unchecked=1 ;;
  esac
done
[ $unchecked -eq 0 ] || exit 2
[ $breaks -eq 0 ] || exit 1
exit 0
