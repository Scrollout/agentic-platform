#!/bin/sh
# Fails when a service's newest contract version breaks its previous version (Principle XV).
#
#   scripts/contract-diff.sh [<repo-root>]
#
# Finds every contract under <repo-root>/src/<service>/api/{stable,preview}/<version>/openapi.yaml
# (default root: this repository), orders each service's versions by folder name as
# scripts/generate.sh does, and runs oasdiff `breaking --fail-on ERR` from the previous version to
# the newest. oasdiff resolves the contracts' relative file `$ref`s itself, so nothing is bundled.
# It prints one line per service examined, plus oasdiff's report for each pair compared.
# Exits 0 when nothing breaks, 1 on a breaking change, 2 when oasdiff cannot compare the pair.
set -eu
repo=$(cd "$(dirname "$0")/.." && pwd)
root=$(cd "${1:-$repo}" && pwd)
OASDIFF_IMAGE=$(awk '$1=="oasdiff"{print $2}' "$repo/src/deploy/images.lock")
[ -n "$OASDIFF_IMAGE" ] || { echo "contract-diff: no oasdiff pin in src/deploy/images.lock" >&2; exit 2; }

services=$(cd "$root" && find src -mindepth 5 -maxdepth 5 -path 'src/*/api/*/*/openapi.yaml' \
  \( -path 'src/*/api/stable/*' -o -path 'src/*/api/preview/*' \) | cut -d/ -f2 | sort -u)
[ -n "$services" ] || { echo "contract-diff: no contracts under $root/src/*/api/{stable,preview}/"; exit 0; }

status=0
for service in $services; do
  # "<version>\t<path>", oldest first; the newest two are compared.
  versions=$(cd "$root" && find "src/$service/api/stable" "src/$service/api/preview" -mindepth 2 -maxdepth 2 \
    -name openapi.yaml 2>/dev/null | awk -F/ '{print $(NF-1) "\t" $0}' | sort)
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
    --fail-on ERR --format text --color never || rc=$?
  case $rc in
    0) echo "$pair: no breaking change" ;;
    1) echo "$pair: breaking change (give it a new version, Principle XV)"; status=1 ;;
    *) echo "$pair: oasdiff could not compare (exit $rc)" >&2; [ $status -eq 1 ] || status=2 ;;
  esac
done
exit $status
