#!/bin/sh
# Smoke check of the replay server (T020). Needs Docker and python3; no key, no cost, no egress.
#   sh src/tests/recorded/smoke.sh
# Not the replay server's test suite (that comes from a separate test author); it shows that the
# analyst gets its recorded answer and that everything else fails loudly.
set -u
HERE=$(cd "$(dirname "$0")" && pwd)
ROOT=$(cd "$HERE/../../.." && pwd)
ID=apsmoke-$$
NET=$ID-net
FAIL=0
pass() { echo "PASS: $1"; }
fail() { echo "FAIL: $1"; FAIL=1; }
cleanup() {
  docker rm -f "$ID-replay" >/dev/null 2>&1
  docker network rm "$NET" "$ID-open" >/dev/null 2>&1
  rm -rf "$TMP"
}
TMP=$(mktemp -d)
trap cleanup EXIT INT TERM

DA=$(awk '$1=="docker-agent"{print $2}' "$ROOT/src/deploy/images.lock")
EXAMPLE="An in-memory notes service. Listing returns all notes in creation order. Creating a note needs non-empty text and returns it with an id starting at 1."
analyst() { # analyst <description>: run the analyst once against the replay server
  printf '%s' "$1" | docker run --rm -i --network "$NET" --read-only --user 65532:65532 \
    --cap-drop ALL --security-opt no-new-privileges --tmpfs /tmp:uid=65532,gid=65532 \
    -e HOME=/tmp/home -e TELEMETRY_ENABLED=false -e MODEL_GATEWAY_URL=http://gateway:8080 \
    -e ANTHROPIC_API_KEY=placeholder-not-a-key -v "$ROOT/src/agents:/agents:ro" \
    "$DA" run --exec --last --safety restricted /agents/analyst.yaml - 2>"$TMP/err"
}

docker network create "$ID-open" >/dev/null
if sh "$HERE/replay.sh" start "$ID-open" "$ID-replay" >/dev/null 2>"$TMP/e0"; then
  fail "the replay server started on a network with egress"
  docker rm -f "$ID-replay" >/dev/null 2>&1
else
  grep -q "not internal" "$TMP/e0" && pass "refuses a network with egress" || fail "unexpected refusal: $(cat "$TMP/e0")"
fi

docker network create --internal -o com.docker.network.bridge.gateway_mode_ipv4=isolated "$NET" >/dev/null
sh "$HERE/replay.sh" start "$NET" "$ID-replay" >/dev/null || { echo "FAIL: replay server did not start"; exit 1; }

# 1. The recorded example: the analyst's answer is the recorded proposal, byte for byte.
out=$(analyst "$EXAMPLE"); rc=$?
want=$(python3 -I -c 'import json,sys; print(json.dumps(json.load(open(sys.argv[1])), separators=(",",":"), ensure_ascii=False))' \
  "$ROOT/src/tests/evals/analyst/testdata/proposals/good-notes.json")
[ "$rc" -eq 0 ] && [ "$out" = "$want" ] && pass "example description gets the recorded proposal" \
  || fail "example: exit $rc, output differs ($(tail -1 "$TMP/err"))"

# 2. An unrecorded request fails loudly and is kept for diagnosis, never forwarded.
analyst "$EXAMPLE Also delete notes." >/dev/null; rc=$?
if [ "$rc" -ne 0 ] && grep -q "404" "$TMP/err"; then pass "unrecorded description fails with 404 (exit $rc)"; else fail "unrecorded description: exit $rc"; fi
sh "$HERE/replay.sh" misses "$ID-replay" >"$TMP/misses"
grep -q "Also delete notes" "$TMP/misses" && pass "the miss is listed by 'replay.sh misses'" || fail "the miss is not listed"

# 3. count_tokens and other paths have no recording: 404, from inside the network.
code() { docker exec "$ID-replay" sh -c "wget -q -O /dev/null $1 2>&1 | grep -o 'HTTP/1.1 [0-9]*'"; }
[ "$(code "--post-data '{}' http://127.0.0.1:8080/v1/messages/count_tokens")" = "HTTP/1.1 404" ] \
  && pass "count_tokens without a recording: 404" || fail "count_tokens without a recording"

# 4. The admin API needs the per-container credential, so an agent cannot add recordings.
[ "$(code "--post-data '{}' http://127.0.0.1:8080/__admin/mappings")" = "HTTP/1.1 401" ] \
  && pass "admin API without the credential: 401" || fail "admin API without the credential"

[ "$FAIL" -eq 0 ] && echo "smoke: all passed" || echo "smoke: FAILED"
exit "$FAIL"
