#!/bin/sh
# The recorded-model replay server (T020; research D19, D26): WireMock from its pin, answering the
# model gateway's two paths from the recordings in ./recordings. No key, no cost, no egress.
#   sh src/tests/recorded/replay.sh start <network> [<name>]  start it on <network> as "gateway"
#   sh src/tests/recorded/replay.sh misses <name>             print the requests no recording matched
#   sh src/tests/recorded/replay.sh stop <name>               remove it
#   sh src/tests/recorded/replay.sh prepare <set> <out>       write <out> for the e2e Compose override
#     (src/tests/e2e/compose.recorded.yaml, T024): the recordings and steps/<set>'s, checked as
#     start checks them, and wiremock.env with the server's options (admin API behind basic auth)
# <network> must be an internal Docker network (no egress), so nothing can pass through to the
# provider. A request that no recording matches gets 404 and WireMock's diff; it is never forwarded.
# Prints the container name on start. Exit 0 on success, 1 on failure, 2 on wrong use.
set -eu
HERE=$(cd "$(dirname "$0")" && pwd)
ROOT=$(cd "$HERE/../../.." && pwd)
REC=${REPLAY_RECORDINGS:-$HERE/recordings}

usage() { echo "usage: replay.sh start <network> [<name>] | misses <name> | stop <name> | prepare <set> <out>" >&2; exit 2; }

# The replay server's options, their one home (start and prepare): the admin API behind basic auth
# with a new random password, no banner, a bounded request journal.
options() {
  pw=$(od -An -N16 -tx1 /dev/urandom | tr -d ' \n')
  printf '%s' "--admin-api-basic-auth replay:$pw --disable-banner --max-request-journal-entries 100"
}

# check_mappings <folder>: every mapping WireMock would load (it reads subfolders too) must answer,
# not proxy, and must match the exact URL, path and query as one string ("url"), so an extra or
# missing query parameter misses; a path-only or pattern matcher would answer more than it saw.
check_mappings() {
  python3 -I - "$1" <<'PY'
import json, os, sys
bad, n = [], 0
for d, _, names in os.walk(sys.argv[1]):
    for f in sorted(names):
        if not f.endswith(".json"):
            continue
        n += 1
        f = os.path.join(d, f)
        try:
            m = json.load(open(f))
            r, resp = m.get("request", {}), m.get("response", {})
        except (OSError, ValueError, AttributeError):
            bad.append(f + ": not a JSON mapping"); continue
        if isinstance(resp, dict) and "proxyBaseUrl" in resp:
            bad.append(f + ": proxies instead of answering"); continue
        loose = [k for k in ("urlPath", "urlPattern", "urlPathPattern", "queryParameters") if k in r]
        if loose or not isinstance(r.get("url"), str):
            bad.append(f + ": matches " + (", ".join(loose) or "no url") + ", not the exact url")
if n == 0:
    bad.append(sys.argv[1] + ": no recordings")
for b in bad:
    print(b, file=sys.stderr)
sys.exit(1 if bad else 0)
PY
}

# admin <container> <path>: GET or POST an admin path with the per-container credential, which
# lives only in that container's environment (WIREMOCK_OPTIONS).
admin() {
  docker exec "$1" sh -c 'set -- $WIREMOCK_OPTIONS; exec wget -qO- "http://$2@127.0.0.1:8080'"$2"'"'
}

case "${1:-}" in
start)
  net=${2:-}; [ -n "$net" ] || usage
  name=${3:-ap-replay-$$}
  IMG=$(awk '$1=="wiremock"{print $2}' "$ROOT/src/deploy/images.lock")
  [ -n "$IMG" ] || { echo "replay.sh: no wiremock pin in src/deploy/images.lock" >&2; exit 1; }
  [ -d "$REC/mappings" ] || { echo "replay.sh: no recordings in $REC/mappings" >&2; exit 1; }
  if [ "$(docker network inspect -f '{{.Internal}}' "$net" 2>/dev/null)" != "true" ]; then
    echo "replay.sh: network '$net' is missing or not internal; the replay server runs only without egress" >&2
    exit 1
  fi
  if ! check_mappings "$REC/mappings"; then
    echo "replay.sh: a recording proxies or does not match the exact request URL (see above)" >&2
    exit 1
  fi
  # A pull of a missing image prints progress on standard output; do it quietly so the only line
  # printed is the name (the pull's own output is shown only when it fails).
  if ! docker image inspect "$IMG" >/dev/null 2>&1; then
    pull=$(docker pull -q "$IMG" 2>&1) || { echo "replay.sh: cannot pull $IMG: $pull" >&2; exit 1; }
  fi
  opts=$(options)
  docker run -d --name "$name" --label ap.replay=1 \
    --network "$net" --network-alias gateway \
    --read-only --user 65532:65532 --cap-drop ALL --security-opt no-new-privileges \
    --memory 512m --pids-limit 256 --tmpfs /tmp:uid=65532,gid=65532 \
    -v "$REC:/home/wiremock:ro" \
    -e "WIREMOCK_OPTIONS=$opts" \
    "$IMG" --port 8080 >/dev/null
  i=0
  until admin "$name" /__admin/health >/dev/null 2>&1; do
    i=$((i + 1))
    if [ "$i" -ge 60 ]; then
      echo "replay.sh: $name did not come up; logs:" >&2
      docker logs "$name" >&2 2>&1 || true
      docker rm -f "$name" >/dev/null 2>&1 || true
      exit 1
    fi
    sleep 0.5
  done
  echo "$name"
  ;;
prepare)
  set=${2:-}; out=${3:-}; [ -n "$set" ] && [ -n "$out" ] || usage
  steps=$(cd "$HERE/steps" && pwd -P)
  sd=$(cd "$HERE/steps/$set" 2>/dev/null && pwd -P) || { echo "replay.sh: no step recording set '$set' in $steps" >&2; exit 1; }
  case $sd in "$steps"/*) ;; *) echo "replay.sh: the set '$set' is not under $steps" >&2; exit 1 ;; esac
  [ -d "$sd/mappings" ] || { echo "replay.sh: the set '$set' has no mappings (synthesise it: synth-steps.sh)" >&2; exit 1; }
  [ -d "$REC/mappings" ] || { echo "replay.sh: no recordings in $REC/mappings" >&2; exit 1; }
  # <out> is new, or a folder an earlier prepare wrote (it holds wiremock.env); nothing else is removed.
  if [ -e "$out" ] && { [ ! -f "$out/wiremock.env" ] || [ -L "$out" ]; }; then
    echo "replay.sh: $out exists and is not a folder prepare wrote; nothing was changed" >&2; exit 1
  fi
  parent=$(dirname -- "$out"); mkdir -p -- "$parent"
  tmp=$(mktemp -d "$parent/.replay-prepare.XXXXXX")
  trap 'rm -rf -- "$tmp"' EXIT
  mkdir -p "$tmp/mappings/base" "$tmp/mappings/steps" "$tmp/__files"
  cp -R "$REC/mappings/." "$tmp/mappings/base/"
  cp -R "$sd/mappings/." "$tmp/mappings/steps/"
  [ ! -d "$REC/__files" ] || cp -R "$REC/__files/." "$tmp/__files/"
  if [ -d "$sd/__files" ]; then
    # The bodies share one folder: a name in both would hide one of them (as lab.recordingsWithSet refuses).
    for f in "$sd/__files"/*; do
      [ ! -e "$tmp/__files/$(basename -- "$f")" ] || { echo "replay.sh: $(basename -- "$f") is in the recordings and in the set '$set'" >&2; exit 1; }
    done
    cp -R "$sd/__files/." "$tmp/__files/"
  fi
  # The replay server runs as 65532: every folder 755 and every file 644, whatever the umask.
  find "$tmp/mappings" "$tmp/__files" -type d -exec chmod 755 {} + && find "$tmp/mappings" "$tmp/__files" -type f -exec chmod 644 {} +
  check_mappings "$tmp/mappings" || { echo "replay.sh: prepare refused the recordings (see above)" >&2; exit 1; }
  (umask 077; printf 'WIREMOCK_OPTIONS=%s\n' "$(options)" >"$tmp/wiremock.env")
  chmod 755 "$tmp" && rm -rf -- "$out" && mv -- "$tmp" "$out"
  trap - EXIT
  ;;
misses)
  [ -n "${2:-}" ] || usage
  admin "$2" /__admin/requests/unmatched
  ;;
stop)
  [ -n "${2:-}" ] || usage
  docker rm -f "$2" >/dev/null
  ;;
*) usage ;;
esac
