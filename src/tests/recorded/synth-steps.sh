#!/bin/sh
# Makes the step recordings again (T054k; research D34, ADR 0038): runs the acceptance tests that
# match <test pattern> with the lab's model set to the scripted server (AP_LAB_MODEL=scripted), so
# each lab writes the exchanges its step agents had into its step set (lab.Steps), as
# steps/<set>/mappings/*.json and steps/<set>/__files/*.sse. No key, no model, no cost.
#   sh src/tests/recorded/synth-steps.sh <test pattern>      e.g. '^TestPlanted$' or '^TestUS2'
# Remove a set's old mappings/ and __files/ first when its answers changed, then review the new
# files before committing them. Needs Docker. Exit: the test binary's (0 when every test passed),
# 2 on wrong use.
set -eu
[ $# -eq 1 ] && [ -n "$1" ] || { echo "usage: synth-steps.sh <test pattern>" >&2; exit 2; }
HERE=$(cd "$(dirname "$0")" && pwd)
ROOT=$(cd "$HERE/../../.." && pwd)
GO_IMAGE=$(awk '$1=="golang"{print $2}' "$ROOT/src/deploy/images.lock")
[ -n "$GO_IMAGE" ] || { echo "synth-steps.sh: no golang pin in src/deploy/images.lock" >&2; exit 1; }
GO_CACHE=${XDG_CACHE_HOME:-$HOME/.cache}/agentic-platform/go  # shared with scripts/check.sh
mkdir -p "$GO_CACHE"
name=synth-steps-$$.test
trap 'rm -f "$GO_CACHE/$name"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
# Built offline in the pinned Go container, run on the host, where Docker is (as scripts/check.sh).
docker run --rm --network none -v "$ROOT/src:/src" -w /src/tests -v "$GO_CACHE:/gopath" \
  --user "$(id -u):$(id -g)" -e GOPATH=/gopath -e GOCACHE=/gopath/cache -e GOTOOLCHAIN=local \
  -e GOPROXY=off -e GOFLAGS=-mod=readonly -e CGO_ENABLED=0 \
  "$GO_IMAGE" go test -c -tags docker -o "/gopath/$name" ./acceptance/
cd "$ROOT/src/tests/acceptance"
AP_LAB_MODEL=scripted "$GO_CACHE/$name" -test.run "$1" -test.count=1 -test.v -test.timeout 90m
