#!/bin/sh
# Every repository check, run locally (no hosted CI for now). Go runs in the pinned containers.
set -eu
cd "$(dirname "$0")/.."
GO_IMAGE=$(awk '$1=="golang"{print $2}' src/deploy/images.lock)
# The same Go release on Debian, with a C compiler: the race detector needs cgo.
GO_RACE_IMAGE=$(awk '$1=="golang-race"{print $2}' src/deploy/images.lock)

GO_CACHE="${XDG_CACHE_HOME:-$HOME/.cache}/agentic-platform/go"  # shared with scripts/generate.sh
mkdir -p "$GO_CACHE"
go_run() {  # go_run <network: bridge|none> <script> [image]: a shell in a pinned Go container over src/
  docker run --rm --network "$1" -v "$PWD/src:/src" -w /src -v "$GO_CACHE:/gopath" \
    --user "$(id -u):$(id -g)" -e GOPATH=/gopath -e GOCACHE=/gopath/cache -e GOTOOLCHAIN=local \
    "${3:-$GO_IMAGE}" sh -ec "$2"
}
GO_MODULES="engine slice cli page tests"

go_checks() {
  # Committed generated blocks first: regenerating would overwrite their drift before --check.
  echo "== committed generated blocks: prove they match their sources"
  scripts/generate.sh --check-committed
  echo "== generated code: regenerate, then prove a second run finds no difference"
  scripts/generate.sh
  scripts/generate.sh --check
  go_run bridge "for m in $GO_MODULES; do (cd \$m && go mod download); done"
  # Both images must carry the same Go release, or the race run tests another compiler.
  [ -n "$GO_RACE_IMAGE" ] || { echo "check: no golang-race pin in src/deploy/images.lock" >&2; exit 1; }
  v=$(go_run none "go env GOVERSION")
  vr=$(go_run none "go env GOVERSION" "$GO_RACE_IMAGE")
  [ "$v" = "$vr" ] || { echo "check: golang is $v but golang-race is $vr" >&2; exit 1; }
  # go vet in the Alpine image, without cgo: the configuration the static binaries are built in.
  echo "== go vet ($v, CGO_ENABLED=0)"
  go_run none "
    export GOPROXY=off GOFLAGS=-mod=readonly CGO_ENABLED=0
    for m in $GO_MODULES; do
      if [ -z \"\$(find \"\$m\" -name '*.go' -print -quit)\" ]; then echo \"skip \$m: no Go files yet\"; continue; fi
      (cd \"\$m\" && go vet ./... && go build ./...)
    done"
  # The tests run once, with the race detector, in the Debian image.
  echo "== go test -race ($vr, CGO_ENABLED=1)"
  go_run none "
    export GOPROXY=off GOFLAGS=-mod=readonly CGO_ENABLED=1
    for m in $GO_MODULES; do
      if [ -z \"\$(find \"\$m\" -name '*.go' -print -quit)\" ]; then echo \"skip \$m: no Go files yet\"; continue; fi
      (cd \"\$m\" && go test -race ./...)
    done" "$GO_RACE_IMAGE"
}
if [ "${1:-}" = "--go-only" ]; then go_checks; exit 0; fi

echo "== check tests (layout, ruleset), each with planted violations"
python3 -m unittest discover -s scripts/tests -p 'test_*.py'
echo "== root layout"
python3 scripts/layout-check.py
echo "== knowledge bundle"
python3 scripts/okf.py check
echo "== markdown"
scripts/npm-tool.sh markdownlint-cli2 --config scripts/repo.markdownlint-cli2.jsonc "docs/**/*.md" "specs/**/*.md" "src/**/*.md" AGENTS.md README.md
echo "== contracts"
for f in $(find src -path '*/api/preview/*' -name 'openapi.yaml'); do
  scripts/npm-tool.sh spectral lint "$f" --ruleset src/engine/api/ruleset.yaml --fail-severity error
done
echo "== contract breaking changes: each service's newest version against the previous one"
scripts/contract-diff.sh
echo "== model gateway"
sh src/deploy/gateway/test_gateway.sh >/dev/null && echo "gateway checks passed"
echo "== go vet and tests"
go_checks
echo "== recorded model and contract-lint service (Docker, no key)"
# Built in the pinned Go container, run on the host, where Docker is; each in its package folder.
for p in recorded lint; do
  go_run none "cd tests && GOPROXY=off GOFLAGS=-mod=readonly CGO_ENABLED=0 go test -c -tags docker -o /gopath/$p.test ./$p/"
  (cd "src/tests/$p" && "$GO_CACHE/$p.test" -test.count=1) && rm -f "$GO_CACHE/$p.test"
done
echo "== dependencies and licences"
# Our own modules are MIT by the root LICENSE, which is outside the mounted src/, so they are ignored.
go_run bridge "
  if [ -z \"\$(find . -name '*.go' -print -quit)\" ]; then echo 'skip: no Go files yet'; exit 0; fi
  go install golang.org/x/vuln/cmd/govulncheck@v1.9.0
  go install github.com/google/go-licenses/v2@v2.0.1
  for m in $GO_MODULES; do
    [ -z \"\$(find \"\$m\" -name '*.go' -print -quit)\" ] && continue
    (cd \"\$m\" && /gopath/bin/govulncheck ./... && /gopath/bin/go-licenses check ./... --disallowed_types=forbidden,unknown --ignore github.com/agentic-platform)
  done"
echo "== secrets"
if git grep -nE 'sk-ant-[A-Za-z0-9_-]{20,}|ghp_[A-Za-z0-9]{30,}' -- . ; then echo "secret found"; exit 1; fi
echo "all checks passed"
