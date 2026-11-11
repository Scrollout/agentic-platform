#!/bin/sh
# Every repository check, run locally (no hosted CI for now). Go runs in the pinned container.
set -eu
cd "$(dirname "$0")/.."
GO_IMAGE=$(awk '$1=="golang"{print $2}' src/deploy/images.lock)
SPECTRAL=$(awk '$1=="spectral"{print $2}' src/deploy/images.lock)

GO_CACHE="${XDG_CACHE_HOME:-$HOME/.cache}/agentic-platform/go"  # shared with scripts/generate.sh
mkdir -p "$GO_CACHE"
go_run() {  # go_run <network: bridge|none> <script>: a shell in the pinned Go container over src/
  docker run --rm --network "$1" -v "$PWD/src:/src" -w /src -v "$GO_CACHE:/gopath" \
    --user "$(id -u):$(id -g)" -e GOPATH=/gopath -e GOCACHE=/gopath/cache -e GOTOOLCHAIN=local \
    "$GO_IMAGE" sh -ec "$2"
}
GO_MODULES="engine slice cli page tests"

go_checks() {
  echo "== generated code: regenerate, then prove a second run finds no difference"
  scripts/generate.sh
  scripts/generate.sh --check
  go_run bridge "for m in $GO_MODULES; do (cd \$m && go mod download); done"
  go_run none "
    export GOPROXY=off GOFLAGS=-mod=readonly
    for m in $GO_MODULES; do
      if [ -z \"\$(find \"\$m\" -name '*.go' -print -quit)\" ]; then echo \"skip \$m: no Go files yet\"; continue; fi
      (cd \"\$m\" && go vet ./... && go test ./...)
    done"
}
if [ "${1:-}" = "--go-only" ]; then go_checks; exit 0; fi

echo "== check tests (layout, ruleset), each with planted violations"
python3 -m unittest discover -s scripts/tests -p 'test_*.py'
echo "== root layout"
python3 scripts/layout-check.py
echo "== knowledge bundle"
python3 scripts/okf.py check
echo "== markdown"
npx -y markdownlint-cli2@0.18.1 --config "$(dirname "$0")/repo.markdownlint-cli2.jsonc" "docs/**/*.md" "specs/**/*.md" "src/**/*.md" AGENTS.md README.md
echo "== contracts"
for f in $(find src -path '*/api/preview/*' -name 'openapi.yaml'); do
  npx -y "$SPECTRAL" lint "$f" --ruleset src/engine/api/ruleset.yaml --fail-severity error
done
echo "== go vet and tests"
go_checks
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
