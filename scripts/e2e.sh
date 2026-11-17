#!/bin/sh
# End to end on the Compose stack against the recorded model (T024; research D44, ADR 0047): `ap up`
# starts the real stack under gVisor with the replay server in place of the gateway
# (src/tests/e2e/compose.recorded.yaml), on volumes of its own and a fresh projects folder; the
# example slice is described, answered, approved, built and replayed through `ap`, and the two runs
# and projects compared. No key: the credentials file is a placeholder. Run by `scripts/check.sh`
# as the step `e2e`; needs Docker, gVisor and COSIGN_PASSWORD (the images are built and signed).
#   scripts/e2e.sh
# Exit 0 when every check passed, 1 when one failed, 3 when the platform is running on this machine
# (nothing is changed then: stop it with `src/deploy/ap down`).
set -eu
cd "$(dirname "$0")/.."
repo=$(pwd -P)
ap=$repo/src/deploy/ap
say() { printf 'e2e: %s\n' "$*" >&2; }
fail() { say "FAIL: $*"; exit 1; }

# Refuse while a platform runs here: its fixed subnets and names cannot coexist with this one.
sess=${XDG_STATE_HOME:-$HOME/.local/state}/agentic-platform/session.env
running=$(docker ps -aq --filter name=agentic-platform 2>/dev/null; docker network ls -q --filter name=agentic-platform 2>/dev/null)
if [ -e "$sess" ] || [ -n "$running" ]; then
  say "the platform is running on this machine (a session or agentic-platform containers or networks exist); stop it with: src/deploy/ap down"
  exit 3
fi
[ -n "${COSIGN_PASSWORD:-}" ] || { say "COSIGN_PASSWORD must be set (the images are built and signed)"; exit 3; }

volumes="agentic-platform_e2e-engine-store agentic-platform_e2e-project-worker-state"
work=$(mktemp -d)
started=
cleanup() {
  code=$?
  if [ -n "$started" ] && [ "$code" -ne 0 ]; then
    # A failed run shows each container's last log lines (the gateway logs no bodies).
    for c in $(docker ps -aq --filter name=agentic-platform); do
      say "logs of $(docker inspect -f '{{.Name}}' "$c"):"
      docker logs --tail 40 "$c" 2>&1 | sed 's/^/    /' >&2
    done
  fi
  if [ -n "$started" ]; then "$ap" down >/dev/null 2>&1 || say "ap down failed"; fi
  # shellcheck disable=SC2086
  docker volume rm -f $volumes >/dev/null 2>&1 || true
  rm -rf -- "$work"
  exit "$code"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# shellcheck disable=SC2086
docker volume rm -f $volumes >/dev/null 2>&1 || true
for img in engine cli project check agent lint page; do
  say "building $img"
  src/deploy/build.sh "$img" >"$work/build-$img.log" 2>&1 || { cat "$work/build-$img.log" >&2; fail "build.sh $img"; }
done

mkdir "$work/projects"
(umask 077; printf 'placeholder, not a key\n' >"$work/credentials")
sh src/tests/recorded/replay.sh prepare happy "$work/replay" || fail "replay.sh prepare happy"
AP_REPLAY_IMAGE=$(awk '$1=="wiremock"{print $2}' src/deploy/images.lock)
[ -n "$AP_REPLAY_IMAGE" ] || fail "no wiremock pin in src/deploy/images.lock"
AP_REPLAY_DIR=$work/replay
AP_COMPOSE_OVERRIDE=$repo/src/tests/e2e/compose.recorded.yaml
export AP_REPLAY_IMAGE AP_REPLAY_DIR AP_COMPOSE_OVERRIDE

started=1
"$ap" up --credentials "$work/credentials" --projects "$work/projects" >"$work/up.out" 2>&1 || { cat "$work/up.out" >&2; fail "ap up"; }
cat "$work/up.out"
! grep -q 'not built yet' "$work/up.out" || fail "ap up started the platform without the run page (its image is not built)"
unset AP_COMPOSE_OVERRIDE

# The example as the analyst recording holds it (src/tests/lab.Example), without a newline.
example="An in-memory notes service. Listing returns all notes in creation order. Creating a note needs non-empty text and returns it with an id starting at 1."
printf '%s' "$example" | "$ap" describe || fail "ap describe"
printf '0\n400\n' | "$ap" questions S-1 || fail "ap questions S-1"
for part in goals rules contract; do "$ap" approve S-1 "$part" || fail "ap approve S-1 $part"; done
"$ap" build-workers S-1 notes || fail "ap build-workers S-1 notes"
"$ap" build S-1 notes </dev/null || fail "ap build S-1 notes"
"$ap" replay RUN-1 notes-replay </dev/null >"$work/replay.out" || { cat "$work/replay.out" >&2; fail "ap replay RUN-1 notes-replay"; }
cat "$work/replay.out"
! grep -q '  AttemptMade  ' "$work/replay.out" || fail "the replay called a model (an AttemptMade event)"
grep -q '  ResultReused  ' "$work/replay.out" || fail "the replay reused no step"
"$ap" compare RUN-1 RUN-2 || fail "ap compare RUN-1 RUN-2 (exit $?)"
t1=$(git -C "$work/projects/notes" log --format=%T) || fail "git log of the project notes"
t2=$(git -C "$work/projects/notes-replay" log --format=%T) || fail "git log of the project notes-replay"
[ -n "$t1" ] && [ "$t1" = "$t2" ] || fail "the projects' trees differ commit by commit"
# The run page answers through page-proxy on 127.0.0.1, with the one Host it accepts (127.0.0.1:port).
# Its root is 404 by design; a run's page is the page (run RUN-1 exists after the replay).
page_url=$("$ap" page) || fail "ap page"
page_host=${page_url#http://}
code=$(curl -s -o "$work/page.html" -w '%{http_code}' --max-time 20 -H "Host: $page_host" "$page_url/runs/RUN-1") || fail "the run page does not answer at $page_url"
[ "$code" = 200 ] || fail "the run page answered $code at $page_url/runs/RUN-1, not 200"
grep -qi '<!doctype html>' "$work/page.html" || fail "the run page's answer is not its HTML"
misses=$(sh src/tests/recorded/replay.sh misses agentic-platform-gateway-1) || fail "cannot read the replay server's misses"
python3 -c 'import json,sys; r=json.loads(sys.argv[1]).get("requests"); sys.exit(1 if r else 0)' "$misses" ||
  { printf '%s\n' "$misses" >&2; fail "the replay server matched no recording for some requests"; }
say "PASS: built, replayed with every step reused, compare exit 0, equal trees, the run page answers, no replay miss"
