#!/bin/sh
# Every repository check, run locally (no hosted CI for now). Go runs in the pinned container.
set -eu
cd "$(dirname "$0")/.."
GO_IMAGE=$(awk '$1=="golang"{print $2}' src/deploy/images.lock)
SPECTRAL=$(awk '$1=="spectral"{print $2}' src/deploy/images.lock)

go_checks() {
  docker run --rm --network none -v "$PWD/src:/src" -w /src --user "$(id -u):$(id -g)" \
    -e GOCACHE=/tmp/gocache -e GOTOOLCHAIN=local "$GO_IMAGE" sh -ec '
    for m in engine slice cli page tests; do
      if [ -z "$(find "$m" -name "*.go" -print -quit)" ]; then echo "skip $m: no Go files yet"; continue; fi
      (cd "$m" && go vet ./... && go test ./...)
    done'
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
echo "== secrets"
if git grep -nE 'sk-ant-[A-Za-z0-9_-]{20,}|ghp_[A-Za-z0-9]{30,}' -- . ; then echo "secret found"; exit 1; fi
echo "all checks passed"
