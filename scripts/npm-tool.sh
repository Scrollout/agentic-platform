#!/bin/sh
# Runs one of the repository's Node tools, exactly as pinned, without network access.
#
#   scripts/npm-tool.sh <tool> [args...]     e.g. scripts/npm-tool.sh spectral lint src/.../openapi.yaml
#
# The tools and their whole dependency tree are pinned by scripts/tools/package-lock.json; Node is
# the pinned `node` image in src/deploy/images.lock. The first run for a lock installs it (with
# `npm ci`, which verifies every package's integrity) into a cache outside the repository.
# The tool runs with the repository mounted at its own path, so repository paths work as
# arguments; set NPM_TOOL_MOUNT to one more directory (such as an output folder) to mount it too.
set -eu
[ $# -ge 1 ] || { echo "usage: scripts/npm-tool.sh <tool> [args...]" >&2; exit 2; }
repo=$(cd "$(dirname "$0")/.." && pwd)
NODE_IMAGE=$(awk '$1=="node"{print $2}' "$repo/src/deploy/images.lock")
[ -n "$NODE_IMAGE" ] || { echo "npm-tool: no node pin in src/deploy/images.lock" >&2; exit 1; }
lock="$repo/scripts/tools/package-lock.json"
key=$(cat "$repo/scripts/tools/package.json" "$lock" | sha256sum | cut -c1-16)
tools="${XDG_CACHE_HOME:-$HOME/.cache}/agentic-platform/node/$key"
user="$(id -u):$(id -g)"

if [ ! -f "$tools/.installed" ]; then
  rm -rf "$tools"; mkdir -p "$tools"
  cp "$repo/scripts/tools/package.json" "$lock" "$tools/"
  docker run --rm -v "$tools:$tools" -w "$tools" --user "$user" -e npm_config_cache="$tools/.npm" \
    -e npm_config_update_notifier=false "$NODE_IMAGE" npm ci --ignore-scripts --no-audit --no-fund >&2
  touch "$tools/.installed"
fi
[ -x "$tools/node_modules/.bin/$1" ] || { echo "npm-tool: unknown tool '$1'" >&2; exit 2; }

tool=$1; shift
extra=""
[ -n "${NPM_TOOL_MOUNT:-}" ] && extra="-v $NPM_TOOL_MOUNT:$NPM_TOOL_MOUNT"
# shellcheck disable=SC2086
exec docker run --rm --network none --user "$user" -v "$repo:$repo:ro" -v "$tools:$tools:ro" $extra \
  -w "$repo" -e HOME=/tmp -e REDOCLY_TELEMETRY=off -e REDOCLY_SUPPRESS_UPDATE_NOTICE=true \
  "$NODE_IMAGE" "$tools/node_modules/.bin/$tool" "$@"
