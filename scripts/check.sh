#!/bin/sh
# Every repository check. CI (GitHub Actions, research D41) runs each step on every push; the
# pre-commit hook runs only the quick static ones. Go runs in the pinned containers.
set -eu
cd "$(dirname "$0")/.."
GO_IMAGE=$(awk '$1=="golang"{print $2}' src/deploy/images.lock)
# The same Go release on Debian, with a C compiler: the race detector needs cgo.
GO_RACE_IMAGE=$(awk '$1=="golang-race"{print $2}' src/deploy/images.lock)

GO_CACHE="${XDG_CACHE_HOME:-$HOME/.cache}/agentic-platform/go"  # shared with scripts/generate.sh
mkdir -p "$GO_CACHE"
# AP_TEST_REPORT_DIR (CI, research D43): when set, every test step writes JUnit reports into it; the
# Go containers see it as /reports. Unset, no report is written and nothing else changes.
REPORTS=
# The folder's path must hold no space or glob character (CI's runner.temp holds none).
if [ -n "${AP_TEST_REPORT_DIR:-}" ]; then
  case $AP_TEST_REPORT_DIR in *[[:space:]*?[]*) echo "check: AP_TEST_REPORT_DIR holds a space or glob character" >&2; exit 2 ;; esac
  mkdir -p "$AP_TEST_REPORT_DIR"; REPORTS="-v $AP_TEST_REPORT_DIR:/reports"
fi
go_run() {  # go_run <network: bridge|none> <script> [image]: a shell in a pinned Go container over src/
  # shellcheck disable=SC2086
  docker run --rm --network "$1" -v "$PWD/src:/src" -w /src -v "$GO_CACHE:/gopath" $REPORTS \
    --user "$(id -u):$(id -g)" -e GOPATH=/gopath -e GOCACHE=/gopath/cache -e GOTOOLCHAIN=local \
    "${3:-$GO_IMAGE}" sh -ec "$2"
}
GO_MODULES="check engine slice cli page tests"

# --prepare: what the offline Go builds need and the repository does not hold: the generated code
# and a filled module cache. The go step does it first; CI jobs that build offline without the go
# step run `scripts/check.sh --prepare` (research D41).
fill_module_cache() {
  go_run bridge "for m in $GO_MODULES; do (cd \$m && go mod download); done"
  # The step checks' godog run (TestGodogStrictReal) is offline: put the pinned godog and its
  # dependencies into the shared module cache now, from the fixture module that requires it.
  godog=$(awk '$1=="godog"{print $2}' src/deploy/images.lock); godog=${godog##*@}
  fixture=engine/checks/testdata/steps/godog/clean
  grep -q "github.com/cucumber/godog $godog\$" "src/$fixture/go.mod" ||
    { echo "check: $fixture/go.mod does not require godog $godog (src/deploy/images.lock)" >&2; exit 1; }
  go_run bridge "cd $fixture && GOWORK=off GOFLAGS=-mod=readonly go mod download"
  # gotestsum (pinned, D43) runs the Go tests and writes their JUnit reports; static, so it also
  # runs in the Debian race image.
  gotestsum=$(awk '$1=="gotestsum"{print $2}' src/deploy/images.lock)
  [ -n "$gotestsum" ] || { echo "check: no gotestsum pin in src/deploy/images.lock" >&2; exit 1; }
  go_run bridge "CGO_ENABLED=0 go install $gotestsum"
}

go_checks() {
  # Committed generated blocks first: regenerating would overwrite their drift before --check.
  echo "== committed generated blocks: prove they match their sources"
  scripts/generate.sh --check-committed
  echo "== generated code: regenerate, fill the module cache, then prove a second run finds no difference"
  scripts/generate.sh
  fill_module_cache
  scripts/generate.sh --check
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
  junit=; [ -z "$REPORTS" ] || junit="--junitfile /reports/go-\$m.xml"
  go_run none "
    export GOPROXY=off GOFLAGS=-mod=readonly CGO_ENABLED=1 AP_OAPI_CODEGEN=/gopath/bin/oapi-codegen
    for m in $GO_MODULES; do
      if [ -z \"\$(find \"\$m\" -name '*.go' -print -quit)\" ]; then echo \"skip \$m: no Go files yet\"; continue; fi
      (cd \"\$m\" && /gopath/bin/gotestsum --format standard-quiet $junit -- -race -timeout 20m ./...)
    done" "$GO_RACE_IMAGE"
}
if [ "${1:-}" = "--go-only" ]; then go_checks; exit 0; fi
if [ "${1:-}" = "--prepare" ]; then scripts/generate.sh; fill_module_cache; exit 0; fi

# Steps, in the order a full run takes them: one line each, name and title; the step's code is
# the function s_<name> below (dashes become underscores). Usage:
#   scripts/check.sh                       every step: the full run a change is accepted on
#   scripts/check.sh --only go,docker      only those steps (a partial run, said so)
#   scripts/check.sh --only py-tests --py test_hooks test_launcher.LauncherTest
#                                          only those check tests (scripts/testrun.py)
#   scripts/check.sh --list                the step names
#   scripts/check.sh --prepare             only the generated code and the module cache (CI jobs)
# An unknown step or check test is refused before anything runs. Each step is timed in setup,
# test and teardown; the run ends with the times and the slowest step.
STEP_TABLE='py-tests check tests (layout, ruleset), each with planted violations
layout root layout
knowledge knowledge bundle
markdown markdown
contracts contracts
contract-diff contract breaking changes: each service'"'"'s newest version against the previous one
gateway model gateway
go go vet and tests
docker recorded model, contract-lint service, API conformance and Story 1 acceptance (Docker, no key)
deps dependencies and licences
secrets secrets'
STEPS=$(echo "$STEP_TABLE" | cut -d' ' -f1)
only=; py=
while [ $# -gt 0 ]; do
  case $1 in
    --list) echo "$STEPS"; exit 0 ;;
    --only) [ $# -ge 2 ] || { echo "check: --only needs step names" >&2; exit 2; }
      only=$(echo "$2" | tr ',' ' '); shift 2 ;;
    --py) shift; while [ $# -gt 0 ] && [ "${1#--}" = "$1" ]; do py="$py $1"; shift; done
      only=${only:-py-tests} ;;
    *) echo "check: unknown argument '$1' (use --only, --py or --list)" >&2; exit 2 ;;
  esac
done
for s in $only; do
  echo "$STEPS" | grep -qx -- "$s" || { echo "check: unknown step '$s'; nothing was run (steps: $(echo $STEPS))" >&2; exit 2; }
done
# testrun.py refuses an unknown check test; ask it before anything runs.
# shellcheck disable=SC2086
[ -z "$py" ] || python3 scripts/testrun.py --list $py >/dev/null || exit 2
[ -n "$only" ] && echo "== PARTIAL RUN: steps $only${py:+, check tests$py}"

times=$(mktemp); trap 'rm -f "$times"' EXIT
now() { date +%s.%N; }
cur=; t0=
phase() {  # phase <setup|test|teardown|end>: close the running phase of the step, start the next
  t=$(now)
  [ -z "$cur" ] || awk -v s="$cur" -v a="$t" -v b="$t0" 'BEGIN { print s, a - b }' >>"$times"
  cur="$step $1"; t0=$t
  [ "$1" != end ] || cur=
}

s_py_tests() {
  # 6 test classes at a time: measured 26 minutes one at a time, 10 by module (4 at a time), 9 by
  # class (6 at a time), every test passing each time.
  # shellcheck disable=SC2086
  python3 scripts/testrun.py --jobs 6 ${AP_TEST_REPORT_DIR:+--junit "$AP_TEST_REPORT_DIR/py-tests.xml"} $py
}
s_layout() { python3 scripts/layout-check.py; }
s_knowledge() { python3 scripts/okf.py check; }
s_markdown() {
  scripts/npm-tool.sh markdownlint-cli2 --config scripts/repo.markdownlint-cli2.jsonc "docs/**/*.md" "specs/**/*.md" "src/**/*.md" AGENTS.md README.md
}
s_contracts() {
  for f in $(find src -path '*/api/preview/*' -name 'openapi.yaml'); do
    scripts/npm-tool.sh spectral lint "$f" --ruleset src/engine/api/ruleset.yaml --fail-severity error
  done
}
s_contract_diff() { scripts/contract-diff.sh; }
s_gateway() { sh src/deploy/gateway/test_gateway.sh >/dev/null && echo "gateway checks passed"; }
s_go() { go_checks; }
s_docker() {
  # Built in the pinned Go container (setup), run on the host, where Docker is (test); each in its
  # package folder. The contract and acceptance suites build the engine they test in the pinned Go
  # container too (src/tests/lab). analystsvc still builds its client with the host's Go, so it
  # stays out until that changes (T040a).
  for p in recorded lint acceptance contract; do
    phase setup
    go_run none "cd tests && GOPROXY=off GOFLAGS=-mod=readonly CGO_ENABLED=0 go test -c -tags docker -o /gopath/$p.test ./$p/"
    phase test
    if [ -z "$REPORTS" ]; then
      (cd "src/tests/$p" && "$GO_CACHE/$p.test" -test.count=1)
    else
      # For the report (D43): the binary's test2json output goes into a file and is shown while it
      # runs (GNU tail, as on the CI runners; test2json's framing byte removed); its exit code is
      # kept; then the file is converted into src/tests/$p's JUnit report.
      out="$GO_CACHE/$p.out"; : >"$out"
      (cd "src/tests/$p" && exec "$GO_CACHE/$p.test" -test.count=1 -test.v=test2json) >"$out" 2>&1 &
      pid=$!
      tail -n +1 -f --pid="$pid" "$out" | tr -d '\026'
      rc=0; wait "$pid" || rc=$?
      conv=0
      go_run none "go tool test2json -p $p </gopath/$p.out >/gopath/$p.json &&
        /gopath/bin/gotestsum --format standard-quiet --junitfile /reports/docker-$p.xml --raw-command -- cat /gopath/$p.json >/dev/null" ||
        conv=$?
      rm -f "$out" "$GO_CACHE/$p.json"
      [ "$rc" -eq 0 ] || conv=$rc
      [ "$conv" -eq 0 ] || { rm -f "$GO_CACHE/$p.test"; exit "$conv"; }
    fi
    phase teardown
    rm -f "$GO_CACHE/$p.test"
  done
}
s_deps() {
  # Our own modules are MIT by the root LICENSE, which is outside the mounted src/, so they are ignored.
  phase setup
  go_run bridge "go install golang.org/x/vuln/cmd/govulncheck@v1.9.0 && go install github.com/google/go-licenses/v2@v2.0.1"
  phase test
  go_run bridge "
    if [ -z \"\$(find . -name '*.go' -print -quit)\" ]; then echo 'skip: no Go files yet'; exit 0; fi
    for m in $GO_MODULES; do
      [ -z \"\$(find \"\$m\" -name '*.go' -print -quit)\" ] && continue
      (cd \"\$m\" && /gopath/bin/govulncheck ./... && /gopath/bin/go-licenses check ./... --disallowed_types=forbidden,unknown --ignore github.com/agentic-platform)
    done"
}
s_secrets() {
  if git grep -nE 'sk-ant-[A-Za-z0-9_-]{20,}|ghp_[A-Za-z0-9]{30,}' -- . ; then echo "secret found"; exit 1; fi
}

start=$(now)
# The table is read on descriptor 3, so a step's own commands keep the caller's input.
while read -r step title <&3; do
  [ -z "$only" ] || echo " $only " | grep -q " $step " || continue
  echo "== $title"
  phase test
  "s_$(echo "$step" | tr - _)"
  phase end
done 3<<STEPS_END
$STEP_TABLE
STEPS_END
total=$(awk -v a="$(now)" -v b="$start" 'BEGIN { print a - b }')
echo "== time per step (setup / test / teardown, seconds):"
awk -v total="$total" '
  { t[$1, $2] += $3; if (!($1 in seen)) { seen[$1] = 1; order[++n] = $1 } }
  END {
    for (i = 1; i <= n; i++) {
      s = order[i]; sum = t[s, "setup"] + t[s, "test"] + t[s, "teardown"]
      printf "  %-14s %7.1f  (%.1f / %.1f / %.1f)\n", s, sum, t[s, "setup"], t[s, "test"], t[s, "teardown"]
      if (sum > max) { max = sum; slow = s }
    }
    printf "  %-14s %7.1f\n  slowest step: %s\n", "all", total, slow
  }' "$times"
if [ -n "$only" ]; then
  echo "== PARTIAL RUN: a change is accepted only after a full run (scripts/check.sh with no arguments)"
else
  echo "all checks passed"
fi
