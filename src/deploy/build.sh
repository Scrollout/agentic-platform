#!/usr/bin/env bash
# Build, sign and record one platform image (ADR 0028):
#   src/deploy/build.sh <agent|check|engine|cli|page|tests> [docker build options]
#
# Builds src/deploy/Dockerfile.<name> with an SBOM and build provenance, pushes it to the local
# registry (started if it isn't running: `ap registry`; AP_REGISTRY_PORT, AP_REGISTRY_NAME),
# signs the pushed digest with the local key, and records the digest reference as AP_IMAGE_<NAME> in
# ${XDG_STATE_HOME:-~/.local/state}/agentic-platform/images.env, the only place `ap` reads images
# from. Every base image and tool comes from images.lock, the only home of pins.
#
# The key pair and signing configuration live in ${XDG_CONFIG_HOME:-~/.config}/agentic-platform/
# signing/ and are made on first use. The private key's password comes from COSIGN_PASSWORD, or
# cosign asks for it on the terminal. The private key is mounted, read-only, into the signing
# container only. Exit codes: 0 built, signed and recorded; 1 any step failed; 3 usage error.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd -P)

say() { printf 'build: %s\n' "$*" >&2; }
die() { local code=$1; shift; say "$*"; exit "$code"; }
usage() { die 3 "usage: build.sh <agent|check|engine|cli|page|tests> [docker build options] (a name with a Dockerfile.<name>)"; }

[ $# -ge 1 ] || usage
name=$1
shift
case $name in '' | *[!a-z]*) usage ;; esac
[ -f "$here/Dockerfile.$name" ] || die 3 "no image '$name': $here/Dockerfile.$name does not exist"
NAME=$(printf '%s' "$name" | tr '[:lower:]' '[:upper:]')

pin() {
  local p
  p=$(awk -v n="$1" '$1 == n { print $2 }' "$here/images.lock")
  [ -n "$p" ] || die 1 "no $1 pin in $here/images.lock"
  printf '%s' "$p"
}
cosign_image=$(pin cosign)
sbom_image=$(pin sbom-scanner)
golang_image=$(pin golang)
agent_image=$(pin docker-agent)

port=${AP_REGISTRY_PORT:-5005}
# The registry's names, as `ap` derives them (AP_REGISTRY_NAME runs a second, independent registry).
reg=${AP_REGISTRY_NAME:-ap-registry}
if [ "$reg" = ap-registry ]; then reg_net=ap-signing; else reg_net=$reg-net; fi
signing=${XDG_CONFIG_HOME:-$HOME/.config}/agentic-platform/signing
state=${XDG_STATE_HOME:-$HOME/.local/state}/agentic-platform
images_env=$state/images.env
me=$(id -u):$(id -g)

# cosign runs from its pin, as the developer (the key is readable only by them), with no
# capabilities and a read-only root. It asks for the key's password on the terminal only when
# COSIGN_PASSWORD is not set; without either it cannot run.
tty=() pass=()
if [ -n "${COSIGN_PASSWORD+set}" ]; then
  [ -n "$COSIGN_PASSWORD" ] || die 1 "COSIGN_PASSWORD is empty; the private key must be encrypted"
  pass=(-e COSIGN_PASSWORD)
elif [ -t 0 ]; then
  tty=(-it)
else
  die 1 "set COSIGN_PASSWORD or run build.sh from a terminal: cosign needs the signing key's password"
fi
cosign_run() {
  docker run --rm "${tty[@]}" "${pass[@]}" --read-only --cap-drop ALL \
    --security-opt no-new-privileges --user "$me" "$@"
}

# The registry: the launcher owns how it runs; this converges it (no change when it is running).
"$here/ap" registry || die 1 "the local image registry could not be started"

# Signing material: made once, never overwritten. A half-present pair stops the build, since
# making a new key would orphan every signature made with the old one.
umask 077
mkdir -p -- "$signing"
chmod 700 -- "$signing"
if [ ! -e "$signing/cosign.key" ] && [ ! -e "$signing/cosign.pub" ]; then
  say "no signing key yet; making an encrypted key pair in $signing"
  cosign_run --network none -v "$signing:/k" -w /k "$cosign_image" generate-key-pair >&2 ||
    die 1 "cosign could not make the key pair"
elif [ ! -f "$signing/cosign.key" ] || [ ! -f "$signing/cosign.pub" ]; then
  die 1 "$signing holds only one of cosign.key and cosign.pub; restore the other (or remove both to make a new pair, after which every image must be rebuilt)"
fi
chmod 600 -- "$signing/cosign.key"
if [ ! -f "$signing/signing-config.json" ]; then
  # A signing configuration naming no service: no transparency log, no certificate authority, no
  # timestamp authority, so no image name or digest leaves the machine (ADR 0028). It is made in
  # an empty directory, so the private key reaches only the signing container.
  cfgdir=$(mktemp -d "$signing/.signing-config.XXXXXX")
  docker run --rm --network none --read-only --cap-drop ALL --security-opt no-new-privileges \
    --user "$me" -v "$cfgdir:/k" "$cosign_image" signing-config create --out /k/signing-config.json >&2 &&
    chmod 600 -- "$cfgdir/signing-config.json" && mv -f -- "$cfgdir/signing-config.json" "$signing/signing-config.json" ||
    { rm -rf -- "$cfgdir"; die 1 "cosign could not make the signing configuration"; }
  rmdir -- "$cfgdir"
  say "made $signing/signing-config.json (no transparency log)"
fi

# Build and push. BuildKit pushes from the container engine, which treats 127.0.0.1 as a plain-HTTP
# registry; with the containerd image store the image also stays in the local store, so Compose
# starts it by digest without pulling.
tag=127.0.0.1:$port/ap-$name:dev
meta=$(mktemp "${TMPDIR:-/tmp}/ap-build-meta.XXXXXX")
trap 'rm -f -- "$meta"' EXIT
docker buildx build -f "$here/Dockerfile.$name" -t "$tag" \
  --sbom="generator=$sbom_image" --provenance=mode=max --push --metadata-file "$meta" \
  --build-arg "GOLANG_IMAGE=$golang_image" \
  --build-arg "DOCKER_AGENT_IMAGE=$agent_image" \
  "$@" "$here/.." || die 1 "docker buildx build failed for $name"
digest=$(grep -o '"containerimage.digest": *"sha256:[0-9a-f]\{64\}"' "$meta" | grep -o 'sha256:[0-9a-f]\{64\}' | head -n 1) ||
  die 1 "the build of $name reported no pushed digest"
ref=127.0.0.1:$port/ap-$name@$digest
docker image inspect "$ref" >/dev/null 2>&1 ||
  die 1 "$ref is not in the local image store (it needs Docker's containerd image store)"
say "built and pushed $ref (with SBOM and provenance)"

# Sign the digest. cosign reaches the registry by its container name on its internal network, the
# same name `ap` verifies with, so the signature's identity matches at deploy time.
cosign_run --network "$reg_net" \
  -v "$signing/cosign.key:/k/cosign.key:ro" \
  -v "$signing/signing-config.json:/k/signing-config.json:ro" \
  "$cosign_image" sign --yes --key /k/cosign.key --signing-config /k/signing-config.json \
  --allow-http-registry "$reg:5000/ap-$name@$digest" >&2 ||
  die 1 "cosign could not sign $ref"
say "signed $ref"

# Record it: replace this name's line, keep the others, write atomically.
mkdir -p -- "$state"
tmp=$(mktemp "$state/images.env.XXXXXX")
{
  if [ -f "$images_env" ]; then grep -v "^AP_IMAGE_$NAME=" "$images_env" || true; fi
  printf 'AP_IMAGE_%s=%s\n' "$NAME" "$ref"
} >"$tmp" && chmod 600 -- "$tmp" && mv -f -- "$tmp" "$images_env" || { rm -f -- "$tmp"; die 1 "cannot write $images_env"; }
say "recorded AP_IMAGE_$NAME=$ref in $images_env"
