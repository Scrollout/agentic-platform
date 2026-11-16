#!/bin/sh
# Gateway test (T019). Run from the repo root: sh src/deploy/gateway/test_gateway.sh
# Needs docker and curl. Uses a fake credential and no internet: the shipped config is
# examined as shipped; forwarding is examined on a derived copy whose upstream is a local stub.
set -u
GW=$(cd "${GW_DIR:-src/deploy/gateway}" && pwd)
[ -f "$GW/default.conf.template" ] || { echo "run from the repo root"; exit 2; }
IMG=$(awk '$1=="nginx"{print $2}' src/deploy/images.lock)
[ -n "$IMG" ] || { echo "no nginx pin in images.lock"; exit 2; }
ID=gwtest-$$
KEY="sk-ant-FAKEKEY-$$-$(date +%s)-notreal"
MARK="MARKER-$$-distinct-9f3a1c"
HOSTNAME_GW=gateway.test
SCRATCH=$(mktemp -d)
FAIL=0
KEYFILE=$SCRATCH/anthropic_api_key
printf '%s\n' "$KEY" >"$KEYFILE"; chmod 644 "$KEYFILE"
pass() { echo "PASS: $1"; }
fail() { echo "FAIL: $1"; FAIL=1; }
cleanup() {
  docker rm -f "$ID-gw" "$ID-stub" "$ID-gw2" "$ID-bad1" "$ID-bad2" "$ID-bad3" "$ID-bad4" >/dev/null 2>&1
  docker network rm "$ID-net" >/dev/null 2>&1
  rm -rf "$SCRATCH"
}
trap cleanup EXIT INT TERM

run_gw() { # name templatedir publishflag extra...
  n=$1; t=$2; shift 2
  docker run -d --name "$n" --read-only --user 101:101 --cap-drop ALL \
    --security-opt no-new-privileges \
    --tmpfs /etc/nginx/conf.d:uid=101,gid=101,mode=0755 \
    --tmpfs /var/cache/nginx:uid=101,gid=101 --tmpfs /var/run:uid=101,gid=101 \
    --tmpfs /tmp:uid=101,gid=101 \
    -v "$t:/etc/nginx/templates:ro" \
    -v "$GW/credential.envsh:/docker-entrypoint.d/05-credential.envsh:ro" \
    -v "$KEYFILE:/run/secrets/anthropic_api_key:ro" \
    -e GATEWAY_SERVER_NAME="$HOSTNAME_GW" \
    -e 'NGINX_ENVSUBST_FILTER=^(ANTHROPIC_API_KEY|GATEWAY_SERVER_NAME)$' \
    "$@" "$IMG" >/dev/null
}
wait_up() { # port
  i=0
  while [ $i -lt 40 ]; do
    curl -s -o /dev/null --max-time 2 "http://127.0.0.1:$1/" && return 0
    sleep 0.5; i=$((i+1))
  done
  return 1
}
code() { # args to curl; prints status
  curl -s -o "$SCRATCH/body" -w '%{http_code}' --max-time 15 "$@"
}

echo "== examining: image $IMG, templates in $GW, fake key, marker $MARK"
run_gw "$ID-gw" "$GW" -p 127.0.0.1::8080 || { echo "cannot start gateway"; exit 1; }
PORT=$(docker port "$ID-gw" 8080/tcp | head -1 | sed 's/.*://')
wait_up "$PORT" || { echo "gateway did not come up"; docker logs "$ID-gw"; exit 1; }
BASE="http://127.0.0.1:$PORT"

echo "== 1. other host refused (shipped config)"
for h in example.com api.anthropic.com 127.0.0.1 evil.test:8080; do
  c=$(code -X POST -H "Host: $h" -d '{}' "$BASE/v1/messages")
  [ "$c" = 421 ] && pass "Host: $h -> 421" || fail "Host: $h -> $c (want 421)"
done
c=$(code -X POST --request-target http://example.com/v1/messages -H "Host: $HOSTNAME_GW" -d '{}' "$BASE")
[ "$c" = 421 ] && pass "absolute URL to example.com -> 421" || fail "absolute URL to example.com -> $c (want 421)"
c=$(code -X POST --request-target https://api.anthropic.com/v1/messages -d '{}' "$BASE")
[ "$c" = 421 ] && pass "absolute URL to api.anthropic.com -> 421" || fail "absolute URL to api.anthropic.com -> $c (want 421)"

echo "== 2. unexpected paths and methods refused (shipped config)"
for p in / /v1/models /v1/messages/ /v1/messages/batches /v1/messages/count_tokens/x /v1/messagesx /v1/%6dessages/../models /admin /v1/complete; do
  c=$(code -X POST -H "Host: $HOSTNAME_GW" -d '{}' "$BASE$p")
  [ "$c" = 403 ] && pass "POST $p -> 403" || fail "POST $p -> $c (want 403)"
done
for m in GET PUT DELETE; do
  c=$(code -X $m -H "Host: $HOSTNAME_GW" "$BASE/v1/messages")
  [ "$c" = 403 ] && pass "$m /v1/messages -> 403" || fail "$m /v1/messages -> $c (want 403)"
done

echo "== 3. logs hold no credential, header or body"
# A valid request: with no internet it ends in 502/504 and writes an error log line, so the log is not empty.
c=$(code -X POST -H "Host: $HOSTNAME_GW" -H "x-api-key: caller-$KEY" -H "x-marker: $MARK" \
  -H 'content-type: application/json' -d "{\"prompt\":\"$MARK\"}" "$BASE/v1/messages")
echo "valid request status (no internet: 502, 504 or 000 on client timeout): $c"
code -X POST -H "Host: example.com" -H "x-marker: $MARK" -d "$MARK" "$BASE/v1/messages" >/dev/null
code -X POST -H "Host: $HOSTNAME_GW" -H "x-marker: $MARK" -d "$MARK" "$BASE/other" >/dev/null
sleep 1
docker logs "$ID-gw" >"$SCRATCH/logs" 2>&1
echo "examined $(wc -l <"$SCRATCH/logs") log lines"
grep -qF "$KEY" "$SCRATCH/logs" && fail "log contains the credential" || pass "log has no credential"
grep -qF "FAKEKEY" "$SCRATCH/logs" && fail "log contains part of the credential" || pass "log has no credential fragment"
grep -qF "$MARK" "$SCRATCH/logs" && fail "log contains the request marker (header or body)" || pass "log has no header or body marker"

echo "== 4. non-root, read-only root file system, no engine socket"
U=$(docker exec "$ID-gw" id -u)
[ "$U" != 0 ] && pass "process uid is $U" || fail "process runs as root"
P=$(docker exec "$ID-gw" sh -c 'ps -o user,comm 2>/dev/null | grep nginx' | awk '{print $1}' | sort -u | tr '\n' ' ')
echo "nginx process users: $P"
case " $P " in *" root "*) fail "an nginx process runs as root";; *) pass "no nginx process as root";; esac
[ "$(docker inspect -f '{{.HostConfig.ReadonlyRootfs}}' "$ID-gw")" = true ] && pass "ReadonlyRootfs=true" || fail "ReadonlyRootfs not true"
docker exec "$ID-gw" sh -c 'touch /etc/gw-write-test' 2>/dev/null && fail "wrote to root file system" || pass "write to / refused"
[ "$(docker inspect -f '{{.HostConfig.Privileged}}' "$ID-gw")" = false ] && pass "not privileged" || fail "privileged"
docker inspect -f '{{range .Mounts}}{{.Source}} {{.Destination}}{{"\n"}}{{end}}' "$ID-gw" >"$SCRATCH/mounts"
grep -q 'docker.sock' "$SCRATCH/mounts" && fail "engine socket mounted" || pass "no engine socket mount"
[ -z "$(docker inspect -f '{{.HostConfig.CapAdd}}' "$ID-gw" | tr -d '[]<nil> ')" ] && pass "no added capabilities" || fail "capabilities added"

echo "== 5. credential not baked into the image"
docker image inspect "$IMG" >"$SCRATCH/img.json"
docker history --no-trunc "$IMG" >"$SCRATCH/hist" 2>&1
grep -qF "$KEY" "$SCRATCH/img.json" "$SCRATCH/hist" && fail "image config/history holds the credential" || pass "image config and history lack the fake credential"
docker image inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$IMG" | grep -qi 'ANTHROPIC\|API_KEY' \
  && fail "image Env names a credential variable" || pass "image Env has no credential variable"
docker run --rm --entrypoint sh "$IMG" -c 'grep -rIl "sk-ant-" /etc /usr/share/nginx 2>/dev/null' | grep . \
  && fail "image files hold an sk-ant- value" || pass "image files hold no sk-ant- value"
grep -rIn 'sk-ant-' "$GW" | grep -v test_gateway.sh | grep . && fail "gateway sources hold a key" || pass "gateway sources hold no key"

echo "== 5b. credential absent from docker inspect of the running container"
docker inspect "$ID-gw" >"$SCRATCH/ctr.json" 2>&1
[ -s "$SCRATCH/ctr.json" ] && echo "examined $(wc -c <"$SCRATCH/ctr.json") bytes of docker inspect"
grep -qF "$KEY" "$SCRATCH/ctr.json" && fail "docker inspect shows the credential" || pass "docker inspect lacks the credential"
grep -qF "FAKEKEY" "$SCRATCH/ctr.json" && fail "docker inspect shows a credential fragment" || pass "docker inspect lacks a credential fragment"
grep -q "ANTHROPIC_API_KEY=" "$SCRATCH/ctr.json" && fail "docker inspect shows an ANTHROPIC_API_KEY= variable" || pass "no ANTHROPIC_API_KEY variable on the container"
docker exec "$ID-gw" sh -c 'tr "\0" "\n" </proc/1/environ' 2>/dev/null | grep -q ANTHROPIC \
  && echo "note: master process environment holds ANTHROPIC_API_KEY (entrypoint export; not a container variable)" || true

echo "== 5c. missing, empty or unreadable credential file: exit non-zero at start, never serve"
: >"$SCRATCH/empty"; chmod 644 "$SCRATCH/empty"
printf '\n\r\n' >"$SCRATCH/blank"; chmod 644 "$SCRATCH/blank"
bad_start() { # name keyfile-or-"" description [extra docker args]
  n=$1; kf=$2; d=$3; shift 3
  set -- "$@"
  if [ -n "$kf" ]; then set -- -v "$kf:/run/secrets/anthropic_api_key:ro" "$@"; fi
  out=$(timeout 90 docker run --name "$n" --read-only --user 101:101 --cap-drop ALL \
    --security-opt no-new-privileges \
    --tmpfs /etc/nginx/conf.d:uid=101,gid=101,mode=0755 \
    --tmpfs /var/cache/nginx:uid=101,gid=101 --tmpfs /var/run:uid=101,gid=101 --tmpfs /tmp:uid=101,gid=101 \
    -v "$GW:/etc/nginx/templates:ro" \
    -v "$GW/credential.envsh:/docker-entrypoint.d/05-credential.envsh:ro" \
    -e GATEWAY_SERVER_NAME="$HOSTNAME_GW" \
    -e 'NGINX_ENVSUBST_FILTER=^(ANTHROPIC_API_KEY|GATEWAY_SERVER_NAME)$' \
    "$@" "$IMG" 2>&1); rc=$?
  echo "[$d] exit=$rc output: $(echo "$out" | grep -i gateway | head -2)"
  [ "$rc" != 0 ] && pass "$d: exits non-zero ($rc)" || fail "$d: exited 0"
  st=$(docker inspect -f '{{.State.Running}}' "$n" 2>/dev/null)
  [ "$st" = false ] && pass "$d: container not running (never served)" || fail "$d: container state running=$st"
  LAST_OUT=$out
}
bad_start "$ID-bad1" "" "missing file"
case "$LAST_OUT" in *"/run/secrets/anthropic_api_key"*) pass "missing: message names the file";; *) fail "missing: message does not name the file";; esac
bad_start "$ID-bad2" "$SCRATCH/empty" "empty file"
case "$LAST_OUT" in *"/run/secrets/anthropic_api_key"*) pass "empty: message names the file";; *) fail "empty: message does not name the file";; esac
bad_start "$ID-bad3" "$SCRATCH/blank" "newline-only file"
case "$LAST_OUT" in *"/run/secrets/anthropic_api_key"*) pass "blank: message names the file";; *) fail "blank: message does not name the file";; esac
docker rm -f "$ID-bad1" "$ID-bad2" "$ID-bad3" >/dev/null 2>&1
bad_start "$ID-bad1" "" "custom missing path" -e GATEWAY_CREDENTIAL_FILE=/run/secrets/other_key
case "$LAST_OUT" in *"/run/secrets/other_key"*) pass "custom path: message names that file";; *) fail "custom path: message does not name /run/secrets/other_key";; esac
docker logs "$ID-bad1" 2>&1 | grep -qF "$KEY" && fail "failure output holds the key" || pass "failure output holds no key"
# T019a: a file that exists and is non-empty but that the gateway's user (101) cannot read: owner-only
# (mode 600, owned by the user running this test), mounted read-only. The README promises exit 1 and
# this exact message. Real TLS and certificate verification to the provider wait for T059 (the evals
# run, which has a test-scoped key); this script has no internet and a fake key.
printf '%s\n' "$KEY" >"$SCRATCH/unreadable"; chmod 600 "$SCRATCH/unreadable"
[ -s "$SCRATCH/unreadable" ] && pass "unreadable fixture is non-empty" || fail "unreadable fixture is empty; the case proves nothing"
bad_start "$ID-bad4" "$SCRATCH/unreadable" "unreadable non-empty file"
[ "$(docker inspect -f '{{.State.ExitCode}}' "$ID-bad4" 2>/dev/null)" = 1 ] && pass "unreadable: exit code is exactly 1" || fail "unreadable: exit code is not 1"
case "$LAST_OUT" in *"gateway: credential file '/run/secrets/anthropic_api_key' is missing, unreadable or empty"*) pass "unreadable: README message";; *) fail "unreadable: message is not the README's: $LAST_OUT";; esac
docker logs "$ID-bad4" 2>&1 | grep -qF "$KEY" && fail "unreadable: output holds the key" || pass "unreadable: output holds no key"

echo "== 6. forwarding and credential injection (derived config, local stub upstream)"
# The shipped upstream is fixed to https://api.anthropic.com and cannot be redirected, so this uses a
# copy of the templates with only the upstream replaced by a plain-HTTP local stub. TLS to the provider,
# DNS re-resolution and the real provider are untested.
docker network create "$ID-net" >/dev/null
mkdir "$SCRATCH/tpl" "$SCRATCH/stubconf"
cp "$GW"/*.template "$SCRATCH/tpl/"
sed -i -e "s|set \$provider \"api.anthropic.com\";|set \$provider \"$ID-stub\";|" \
  -e 's|proxy_pass https://\$provider;|proxy_pass http://$provider:8081;|' \
  -e '/^proxy_ssl_/d' -e 's|proxy_set_header Host \$provider;|proxy_set_header Host $provider;|' \
  "$SCRATCH/tpl/provider.inc.template"
grep -q "$ID-stub" "$SCRATCH/tpl/provider.inc.template" && grep -q 'proxy_pass http://' "$SCRATCH/tpl/provider.inc.template" \
  && pass "derived config built (upstream swapped; headers/paths logic unchanged)" || fail "could not derive stub config; template changed shape"
cat >"$SCRATCH/stubconf/default.conf" <<'CONF'
log_format hit 'STUBHIT $request_uri';
server { listen 8081; access_log /dev/stdout hit;
  location / { default_type text/plain; return 200 "key=[$http_x_api_key] host=[$http_host] auth=[$http_authorization] cookie=[$http_cookie] extra=[$http_x_extra] uri=[$request_uri]"; } }
CONF
docker run -d --name "$ID-stub" --network "$ID-net" -v "$SCRATCH/stubconf:/etc/nginx/conf.d:ro" "$IMG" >/dev/null
docker rm -f "$ID-gw" >/dev/null 2>&1
run_gw "$ID-gw2" "$SCRATCH/tpl" --network "$ID-net" -p 127.0.0.1::8080
PORT=$(docker port "$ID-gw2" 8080/tcp | head -1 | sed 's/.*://')
wait_up "$PORT" || { fail "derived gateway did not start"; docker logs "$ID-gw2"; exit 1; }
BASE="http://127.0.0.1:$PORT"
sleep 1
c=$(code -X POST -H "Host: $HOSTNAME_GW" -H "x-api-key: CALLERKEY" -H "Authorization: Bearer CALLERAUTH" \
  -H "Cookie: c=CALLERCOOKIE" -H "x-extra: kept" -H 'anthropic-version: 2023-06-01' -d '{}' "$BASE/v1/messages")
R=$(cat "$SCRATCH/body")
echo "stub saw: $R"
[ "$c" = 200 ] && pass "allowed request forwarded (200)" || fail "allowed request -> $c"
case "$R" in *"key=[$KEY]"*) pass "gateway injected its credential";; *) fail "credential not injected";; esac
case "$R" in *CALLERKEY*) fail "caller x-api-key reached upstream";; *) pass "caller x-api-key replaced";; esac
case "$R" in *CALLERAUTH*|*CALLERCOOKIE*) fail "caller Authorization/Cookie reached upstream";; *) pass "caller Authorization and Cookie dropped";; esac
case "$R" in *"extra=[kept]"*) pass "other caller headers passed on";; *) fail "other caller header not passed on";; esac
c=$(code -X POST -H "Host: $HOSTNAME_GW" -d '{}' "$BASE/v1/messages/count_tokens")
[ "$c" = 200 ] && pass "count_tokens forwarded" || fail "count_tokens -> $c"
before=$(docker logs "$ID-stub" 2>&1 | grep -c STUBHIT)
code -X POST -H "Host: example.com" -d '{}' "$BASE/v1/messages" >/dev/null
code -X POST --request-target http://example.com/v1/messages -H "Host: $HOSTNAME_GW" -d '{}' "$BASE" >/dev/null
code -X POST -H "Host: $HOSTNAME_GW" -d '{}' "$BASE/v1/models" >/dev/null
code -X GET -H "Host: $HOSTNAME_GW" "$BASE/v1/messages" >/dev/null
after=$(docker logs "$ID-stub" 2>&1 | grep -c STUBHIT)
[ "$before" = "$after" ] && pass "refused requests never reached the upstream ($before hits before and after)" \
  || fail "refused request was forwarded ($before -> $after hits)"
[ "$before" -ge 2 ] && pass "stub does record forwarded hits ($before)" || fail "stub recorded too few hits ($before); the never-forwarded check proves nothing"

echo
[ $FAIL = 0 ] && echo "ALL GATEWAY CHECKS PASSED" || echo "GATEWAY CHECKS FAILED"
exit $FAIL
