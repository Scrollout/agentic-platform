#!/bin/sh
# Recorder for the replay server (T020; research D26). COSTS MONEY: every request the agent makes
# while recording goes through the real gateway to the provider with the test-scoped key (D19).
# Run only by the project owner, by hand; no check, hook or test runs it.
#   sh src/tests/recorded/record.sh start             make a lab: internal network, gateway, recorder
#   (run the agent on the printed network with MODEL_GATEWAY_URL=http://recorder:8080)
#   sh src/tests/recorded/record.sh stop <name>       write recordings/mappings/<name>.json and
#                                                     recordings/__files/<name>.sse, remove the lab
# The key file is ~/.config/agentic-platform/anthropic_api_key (override: EVAL_KEY_FILE). This
# script only checks that it is non-empty; it is mounted read-only into the gateway alone.
# The recorder is WireMock proxying to the gateway; it records each request body for exact
# matching (equalToJson, no ignored elements) and no request headers. `recording write` keeps
# only the response's Content-Type, so provider ids never reach the repository.
set -eu
HERE=$(cd "$(dirname "$0")" && pwd)
ROOT=$(cd "$HERE/../../.." && pwd)
STATE=${XDG_STATE_HOME:-$HOME/.local/state}/agentic-platform/record
lock() { awk -v n="$1" '$1==n{print $2}' "$ROOT/src/deploy/images.lock"; }
admin() { # admin <container> <path> [<post-data>]
  docker exec "$1" sh -c 'set -- $WIREMOCK_OPTIONS "$@"; a=$2; shift 2
    if [ $# -gt 0 ]; then exec wget -qO- --header "Content-Type: application/json" --post-data "$2" "http://$a@127.0.0.1:8080$1"
    else exec wget -qO- "http://$a@127.0.0.1:8080$1"; fi' sh "$2" ${3+"$3"}
}

case "${1:-}" in
start)
  [ ! -e "$STATE/id" ] || { echo "record.sh: a recording is running ($(cat "$STATE/id")); run 'record.sh stop <name>' first" >&2; exit 1; }
  KEY=${EVAL_KEY_FILE:-$HOME/.config/agentic-platform/anthropic_api_key}
  [ -s "$KEY" ] || { echo "record.sh: the test-scoped key file $KEY is missing or empty" >&2; exit 2; }
  NGINX=$(lock nginx); WM=$(lock wiremock)
  [ -n "$NGINX" ] && [ -n "$WM" ] || { echo "record.sh: nginx or wiremock pin missing in images.lock" >&2; exit 1; }
  id=aprec-$(date +%s)-$$
  mkdir -p "$STATE"; chmod 700 "$STATE"
  echo "$id" >"$STATE/id"
  # A start that fails half-way removes what it made.
  undo() {
    docker ps -aq --filter "label=ap.record=$id" | xargs -r docker rm -f >/dev/null 2>&1 || true
    docker network rm "$id-net" >/dev/null 2>&1 || true
    rm -f "$STATE/id"
  }
  trap 'undo' EXIT
  GW=$ROOT/src/deploy/gateway
  uid="$(id -u):$(id -g)"; own="uid=$(id -u),gid=$(id -g)"
  docker network create --internal --label "ap.record=$id" \
    -o com.docker.network.bridge.gateway_mode_ipv4=isolated "$id-net" >/dev/null
  # The gateway as in the analyst evals: created on the default bridge for egress, then joined to
  # the internal network as "gateway"; it runs as the owner because the key file is mode 600.
  docker create --name "$id-gateway" --label "ap.record=$id" --read-only --user "$uid" \
    --cap-drop ALL --security-opt no-new-privileges --memory 256m --pids-limit 128 \
    --tmpfs "/etc/nginx/conf.d:$own,mode=0755" --tmpfs "/var/cache/nginx:$own" \
    --tmpfs "/var/run:$own" --tmpfs "/tmp:$own" \
    -v "$GW:/etc/nginx/templates:ro" \
    -v "$GW/credential.envsh:/docker-entrypoint.d/05-credential.envsh:ro" \
    -v "$KEY:/run/secrets/anthropic_api_key:ro" \
    -e GATEWAY_SERVER_NAME=gateway \
    -e 'NGINX_ENVSUBST_FILTER=^(ANTHROPIC_API_KEY|GATEWAY_SERVER_NAME)$' "$NGINX" >/dev/null
  docker network connect --alias gateway "$id-net" "$id-gateway"
  docker start "$id-gateway" >/dev/null
  pw=$(od -An -N16 -tx1 /dev/urandom | tr -d ' \n')
  docker run -d --name "$id-recorder" --label "ap.record=$id" \
    --network "$id-net" --network-alias recorder \
    --read-only --user 65532:65532 --cap-drop ALL --security-opt no-new-privileges \
    --memory 512m --pids-limit 256 --tmpfs /tmp:uid=65532,gid=65532 \
    --tmpfs /home/wiremock:uid=65532,gid=65532 \
    -e "WIREMOCK_OPTIONS=--admin-api-basic-auth record:$pw" \
    "$WM" --port 8080 --disable-banner >/dev/null
  i=0
  until admin "$id-recorder" /__admin/health >/dev/null 2>&1; do
    i=$((i + 1)); [ "$i" -lt 60 ] || { echo "record.sh: the recorder did not come up" >&2; exit 1; }
    sleep 0.5
  done
  admin "$id-recorder" /__admin/recordings/start '{"targetBaseUrl":"http://gateway:8080",
    "requestBodyPattern":{"matcher":"equalToJson","ignoreArrayOrder":false,"ignoreExtraElements":false},
    "persist":false,"repeatsAsScenarios":false}' >/dev/null
  trap - EXIT
  echo "Recording. Run the agent on network $id-net with MODEL_GATEWAY_URL=http://recorder:8080,"
  echo "then: sh src/tests/recorded/record.sh stop <name>"
  ;;
stop)
  name=${2:-}; [ -n "$name" ] || { echo "usage: record.sh stop <name>" >&2; exit 2; }
  [ -e "$STATE/id" ] || { echo "record.sh: no recording is running" >&2; exit 1; }
  id=$(cat "$STATE/id")
  # The recorder hands the session's exchanges over once, so keep them in a file until they are
  # written; if writing fails, the lab and the file stay, and writing can be repeated from it.
  snap="$STATE/snapshot-$id.json"
  [ -s "$snap" ] || admin "$id-recorder" /__admin/recordings/stop '{}' > "$snap"
  if ! (cd "$ROOT/src/tests" && go run ./recorded/recording write -dir recorded/recordings -name "$name") < "$snap"; then
    echo "record.sh: writing the recordings failed; the exchanges are kept in $snap and the lab still runs." >&2
    echo "Fix the cause, then run stop again; it writes from that file." >&2
    exit 1
  fi
  rm -f "$snap"
  docker ps -aq --filter "label=ap.record=$id" | xargs -r docker rm -f >/dev/null
  docker network rm "$id-net" >/dev/null
  rm -f "$STATE/id"
  echo "Review the new files under src/tests/recorded/recordings/ before committing them."
  ;;
*) echo "usage: record.sh start | stop <name>" >&2; exit 2 ;;
esac
