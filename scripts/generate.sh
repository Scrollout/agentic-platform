#!/bin/sh
# Makes the generated Go code match the contracts it comes from.
#
#   scripts/generate.sh          regenerate what differs; a second run changes nothing
#   scripts/generate.sh --check  show what differs and change nothing; exits 1 on any difference
#
# Each src/<module>/api/<service>.oapi-codegen.yaml declares one target: code generated from the
# newest contract version under src/<service>/api/{stable,preview}/ (ordered by
# scripts/contract-versions.sh, the rule scripts/contract-diff.sh also uses: by date, then preview
# before stable on the same date; a misnamed version folder fails the run), bundled into one document
# with Redocly CLI, then turned into src/<module>/api/gen/<service>.gen.go by oapi-codegen.
# Generated code is never committed (constitution, Clean tree); builds and checks run this first.
set -eu
export LC_ALL=C
cd "$(dirname "$0")/.."
# Lists below hold one path per line; split on newlines only, so a name with spaces stays whole.
IFS='
'
set -f
mode=write
[ "${1:-}" = "--check" ] && mode=check
pin() { awk -v n="$1" '$1==n{print $2}' src/deploy/images.lock; }
GO_IMAGE=$(pin golang)
OAPI_CODEGEN=$(pin oapi-codegen)
GO_CACHE="${XDG_CACHE_HOME:-$HOME/.cache}/agentic-platform/go"  # module and build cache, outside the repository
mkdir -p "$GO_CACHE"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/contracts" "$tmp/out"

targets=$(find src -path 'src/*/api/*.oapi-codegen.yaml' | sort)
[ -z "$targets" ] && { echo "generate: no targets found"; exit 0; }

for config in $targets; do
  service=$(basename "$config" .oapi-codegen.yaml)
  bundle="$tmp/contracts/$service.openapi.yaml"
  [ -f "$bundle" ] && continue
  versions=$(sh scripts/contract-versions.sh . "$service") \
    || { echo "generate: $config: version folders under src/$service/api/ cannot be ordered" >&2; exit 1; }
  contract=$(printf '%s\n' "$versions" | tail -1 | cut -f2)
  [ -z "$contract" ] && { echo "generate: $config: no contract under src/$service/api/" >&2; exit 1; }
  echo "bundle   $contract"
  NPM_TOOL_MOUNT="$tmp" scripts/npm-tool.sh redocly bundle "$contract" -o "$bundle" >"$tmp/redocly.log" 2>&1 \
    || { cat "$tmp/redocly.log" >&2; exit 1; }
done

# Generate every target into $tmp/out/<module>/api/gen/, in the pinned Go container.
docker run --rm -v "$PWD/src:/src:ro" -v "$tmp:/work" -v "$GO_CACHE:/gopath" \
  -e GOPATH=/gopath -e GOCACHE=/gopath/cache -e GOTOOLCHAIN=local \
  --user "$(id -u):$(id -g)" "$GO_IMAGE" sh -ec '
  go install "$1"
  shift
  for config in "$@"; do
    service=$(basename "$config" .oapi-codegen.yaml)
    dir=$(dirname "$config")
    mkdir -p "/work/out/$dir/gen"
    (cd "/$dir" && /gopath/bin/oapi-codegen -config "$(basename "$config")" \
      -o "/work/out/$dir/gen/$service.gen.go" "/work/contracts/$service.openapi.yaml")
  done' generate "$OAPI_CODEGEN" $targets

status=0
for gen in $(for config in $targets; do echo "$(dirname "$config")/gen"; done | sort -u); do
  if diff -r "$gen" "$tmp/out/$gen" >/dev/null 2>&1; then
    echo "same     $gen"
  elif [ "$mode" = check ]; then
    echo "differs  $gen (run scripts/generate.sh)"
    diff -ru "$gen" "$tmp/out/$gen" 2>&1 | head -40 || true
    status=1
  else
    rm -rf "$gen"
    cp -R "$tmp/out/$gen" "$gen"
    echo "written  $gen"
  fi
done
exit $status
