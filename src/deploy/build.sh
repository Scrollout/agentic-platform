#!/usr/bin/env bash
# Build, sign and record platform images (ADR 0028):
#   src/deploy/build.sh <agent|check|project|engine|cli|page|lint|tests> [docker build options]
#   src/deploy/build.sh --changed [--check] <name>...
#
# Builds src/deploy/Dockerfile.<name> with an SBOM and build provenance, pushes it to the local
# registry (started if it isn't running: `ap registry`; AP_REGISTRY_PORT, AP_REGISTRY_NAME),
# signs the pushed digest with the local key, and records the digest reference as AP_IMAGE_<NAME> in
# ${XDG_STATE_HOME:-~/.local/state}/agentic-platform/images.env, the only place `ap` reads images
# from. Every base image and tool comes from images.lock, the only home of pins.
#
# Each image also has a fingerprint (feature 003, research R1, ADR 0061): the digest of its
# candidate, the same build with SOURCE_DATE_EPOCH=0 and rewrite-timestamp=true, without SBOM,
# provenance or push, into the local image store (not unpacked). The pushed image is built with
# SOURCE_DATE_EPOCH=0 but without rewrite-timestamp, which the store's unpacking refuses; its
# fingerprint is its candidate's. Reproducible, so unchanged sources give the same
# fingerprint, and cheap when the build cache holds it. A signed build records it as AP_BUILT_<NAME>
# in built.env, next to images.env. --changed builds, signs and records only the named images whose
# fingerprint differs from AP_BUILT_<NAME> or that have no AP_IMAGE_<NAME>, printing `build:
# building <names>` before and `build: built and signed <names>` after, and nothing when nothing
# changed; it asks for the signing key's password once, only when it must sign. --changed --check
# prints `build: would build <names>` (exit 1) or `build: nothing to build` (exit 0) and changes no
# record, registry, signature or key.
#
# The key pair and signing configuration live in ${XDG_CONFIG_HOME:-~/.config}/agentic-platform/
# signing/ and are made on first use. The private key's password comes from COSIGN_PASSWORD, or is
# asked on the terminal (by cosign for one image; once by --changed). The private key is mounted,
# read-only, into the signing container only. Builds may run at the same time: they start the
# registry together safely (`ap registry`), only one makes the first key pair (lock
# signing/keys.lock), and recording takes a lock (images.env.lock), so no build loses another's
# line. Exit codes: 0 built, signed and recorded (or, with --check, nothing to build); 1 any step
# failed (or, with --check, something to build); 3 usage error.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd -P)

say() { printf 'build: %s\n' "$*" >&2; }
die() { local code=$1; shift; say "$*"; exit "$code"; }
usage() { die 3 "usage: build.sh <agent|check|project|engine|cli|page|lint|tests> [docker build options], or build.sh --changed [--check] <name>... (a name with a Dockerfile.<name>)"; }

changed='' check=''
if [ "${1:-}" = --changed ]; then
  changed=1
  shift
  if [ "${1:-}" = --check ]; then check=1; shift; fi
  [ $# -ge 1 ] || usage
  names=("$@")
  set --
else
  [ $# -ge 1 ] || usage
  names=("$1")
  shift
fi
for name in "${names[@]}"; do
  case $name in '' | *[!a-z]*) usage ;; esac
  [ -f "$here/Dockerfile.$name" ] || die 3 "no image '$name': $here/Dockerfile.$name does not exist"
done
upper() { printf '%s' "$1" | tr '[:lower:]' '[:upper:]'; }
is_go() { case $1 in engine | cli | page | check | project) return 0 ;; esac; return 1; }

# The Go images build generated code that is never committed (scripts/generate.sh): make it match
# the contracts first, so a fresh clone builds. generate.sh --check changes nothing; a second run
# of generate.sh changes nothing either, so this converges. It runs once per run; --check never
# regenerates, and counts the named Go images as changed when the generated code is stale.
gen_stale=''
for name in "${names[@]}"; do
  is_go "$name" || continue
  gen="$here/../../scripts/generate.sh"
  if ! "$gen" --check >/dev/null 2>&1; then
    if [ -n "$check" ]; then
      gen_stale=1
    else
      say "generated code is missing or stale; running scripts/generate.sh"
      "$gen" >&2 || die 1 "scripts/generate.sh failed; see the messages above"
    fi
  fi
  break
done

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
node_image=$(pin node)
oapi_codegen=$(pin oapi-codegen)
alpine_image=$(pin alpine)
alpine_git=$(pin alpine-git)

port=${AP_REGISTRY_PORT:-5005}
# The registry's names, as `ap` derives them (AP_REGISTRY_NAME runs a second, independent registry).
reg=${AP_REGISTRY_NAME:-ap-registry}
if [ "$reg" = ap-registry ]; then reg_net=ap-signing; else reg_net=$reg-net; fi
signing=${XDG_CONFIG_HOME:-$HOME/.config}/agentic-platform/signing
state=${XDG_STATE_HOME:-$HOME/.local/state}/agentic-platform
images_env=$state/images.env
built_env=$state/built.env
me=$(id -u):$(id -g)

# cosign runs from its pin, as the developer (the key is readable only by them), with no
# capabilities and a read-only root. A plain build lets cosign ask for the key's password on the
# terminal when COSIGN_PASSWORD is not set; without either it cannot run. --changed asks itself,
# once, and hands the password to each cosign call's environment only (signing_pw, never exported
# or written).
tty=() pass=() signing_pw=''
password_from_env() {
  [ -n "$COSIGN_PASSWORD" ] || die 1 "COSIGN_PASSWORD is empty; the private key must be encrypted"
  pass=(-e COSIGN_PASSWORD)
}
if [ -z "$changed" ]; then
  if [ -n "${COSIGN_PASSWORD+set}" ]; then
    password_from_env
  elif [ -t 0 ]; then
    tty=(-it)
  else
    die 1 "set COSIGN_PASSWORD or run build.sh from a terminal: cosign needs the signing key's password"
  fi
fi
cosign_run() {
  if [ -n "$signing_pw" ]; then
    COSIGN_PASSWORD=$signing_pw docker run --rm -e COSIGN_PASSWORD --read-only --cap-drop ALL \
      --security-opt no-new-privileges --user "$me" "$@"
  else
    docker run --rm "${tty[@]}" "${pass[@]}" --read-only --cap-drop ALL \
      --security-opt no-new-privileges --user "$me" "$@"
  fi
}

# Locks (take_lock, release_lock and the stale-lock rule): lock.sh, the one home of that rule
# (research R4 of feature 003). cleanup removes a lock if the build stops while holding it.
# shellcheck source=lock.sh
. "$here/lock.sh"
meta='' work=''
cleanup() {
  if [ -n "$meta" ]; then rm -f -- "$meta"; fi
  if [ -n "$work" ]; then rm -rf -- "$work"; fi
  if [ -n "$locked" ]; then rm -rf -- "$locked"; fi
}
trap cleanup EXIT
work=$(mktemp -d "${TMPDIR:-/tmp}/ap-build.XXXXXX")

# What every build of an image passes to BuildKit: the pins, the fixed source time and, for the lint
# image, the named contexts it takes from outside src/ or from what src/.dockerignore drops
# (deploy/): the Node tools' lockfile and the service's code.
build_args=(--build-arg "SOURCE_DATE_EPOCH=0"
  --build-arg "GOLANG_IMAGE=$golang_image"
  --build-arg "DOCKER_AGENT_IMAGE=$agent_image"
  --build-arg "NODE_IMAGE=$node_image" --build-arg "OAPI_CODEGEN=$oapi_codegen"
  --build-arg "ALPINE_IMAGE=$alpine_image" --build-arg "ALPINE_GIT=$alpine_git")
contexts_of() {
  contexts=()
  if [ "$1" = lint ]; then
    contexts=(--build-context "npm-tools=$here/../../scripts/tools" --build-context "lint-service=$here/lint")
  fi
}
digest_in() {
  grep -o '"containerimage.digest": *"sha256:[0-9a-f]\{64\}"' "$1" | grep -o 'sha256:[0-9a-f]\{64\}' | head -n 1
}
# The value of <key> in an env file written by this script, or nothing.
recorded() {
  if [ -f "$1" ]; then grep "^$2=" "$1" | tail -n 1 | cut -d= -f2- || true; fi
}

# candidate <name> [docker build options]: sets fingerprint to the digest of the image's candidate.
# Quiet; a failed build shows the build's own output and stops naming the image.
candidate() {
  local n=$1 m log
  shift
  m=$work/$n.meta log=$work/$n.log
  contexts_of "$n"
  # unpack=false: rewrite-timestamp conflicts with unpacking into the store's snapshotter (Docker's
  # default with the containerd image store on some versions), and a candidate is never run.
  if ! docker buildx build -f "$here/Dockerfile.$n" --sbom=false --provenance=false \
    --output type=image,rewrite-timestamp=true,unpack=false --metadata-file "$m" \
    "${build_args[@]}" "${contexts[@]}" "$@" "$here/.." >"$log" 2>&1; then
    cat -- "$log" >&2
    die 1 "the build of $n failed; its error is above (a first build needs the network, to fetch the pinned images and modules)"
  fi
  fingerprint=$(digest_in "$m") || die 1 "the build of $n reported no digest"
}

# The registry: the launcher owns how it runs; this converges it (no change when it is running).
start_registry() { "$here/ap" registry || die 1 "the local image registry could not be started"; }

# Signing material: made once, never overwritten. A half-present pair stops the build, since
# making a new key would orphan every signature made with the old one. Builds starting together
# on a new machine take the key lock (keys.lock), so only one makes the pair and the others use it.
make_signing_material() {
  umask 077
  mkdir -p -- "$signing"
  chmod 700 -- "$signing"
  if [ ! -f "$signing/cosign.key" ] || [ ! -f "$signing/cosign.pub" ]; then
    take_lock "$signing/keys.lock"
    if [ ! -e "$signing/cosign.key" ] && [ ! -e "$signing/cosign.pub" ]; then
      say "no signing key yet; making an encrypted key pair in $signing"
      cosign_run --network none -v "$signing:/k" -w /k "$cosign_image" generate-key-pair >&2 ||
        die 1 "cosign could not make the key pair"
    elif [ ! -f "$signing/cosign.key" ] || [ ! -f "$signing/cosign.pub" ]; then
      die 1 "$signing holds only one of cosign.key and cosign.pub; restore the other (or remove both to make a new pair, after which every image must be rebuilt)"
    fi
    release_lock
  fi
  chmod 600 -- "$signing/cosign.key"
  if [ ! -f "$signing/signing-config.json" ]; then
    # A signing configuration naming no service: no transparency log, no certificate authority, no
    # timestamp authority, so no image name or digest leaves the machine (ADR 0028). It is made in
    # an empty directory, so the private key reaches only the signing container.
    local cfgdir
    cfgdir=$(mktemp -d "$signing/.signing-config.XXXXXX")
    docker run --rm --network none --read-only --cap-drop ALL --security-opt no-new-privileges \
      --user "$me" -v "$cfgdir:/k" "$cosign_image" signing-config create --out /k/signing-config.json >&2 &&
      chmod 600 -- "$cfgdir/signing-config.json" && mv -f -- "$cfgdir/signing-config.json" "$signing/signing-config.json" ||
      { rm -rf -- "$cfgdir"; die 1 "cosign could not make the signing configuration"; }
    rmdir -- "$cfgdir"
    say "made $signing/signing-config.json (no transparency log)"
  fi
}

# replace_line <file> <key> <value>: replace that key's line, keep the others, write atomically.
# The caller holds the images.env.lock.
replace_line() {
  local tmp
  tmp=$(mktemp "$state/$(basename -- "$1").XXXXXX")
  {
    if [ -f "$1" ]; then grep -v "^$2=" "$1" || true; fi
    printf '%s=%s\n' "$2" "$3"
  } >"$tmp" && chmod 600 -- "$tmp" && mv -f -- "$tmp" "$1" || { rm -f -- "$tmp"; die 1 "cannot write $1"; }
}

# note: the per-image lines of a plain build; --changed prints only its two summary lines.
note() { if [ -z "$changed" ]; then say "$@"; fi; }

# build_one <name> <fingerprint> [docker build options]: build, push, sign and record one image.
build_one() {
  local n=$1 fp=$2 N tag digest ref
  shift 2
  N=$(upper "$n")
  # BuildKit pushes from the container engine, which treats 127.0.0.1 as a plain-HTTP registry;
  # with the containerd image store the image also stays in the local store, so Compose starts it
  # by digest without pulling.
  tag=127.0.0.1:$port/ap-$n:dev
  contexts_of "$n"
  meta=$(mktemp "${TMPDIR:-/tmp}/ap-build-meta.XXXXXX")
  docker buildx build -f "$here/Dockerfile.$n" -t "$tag" \
    --sbom="generator=$sbom_image" --provenance=mode=max --push --metadata-file "$meta" \
    "${build_args[@]}" "${contexts[@]}" "$@" "$here/.." || die 1 "docker buildx build failed for $n"
  digest=$(digest_in "$meta") || die 1 "the build of $n reported no pushed digest"
  rm -f -- "$meta"
  meta=''
  ref=127.0.0.1:$port/ap-$n@$digest
  # Another build of the same name since this push moves the tag, and the local store then drops
  # this digest; it is pulled back by digest from the local registry.
  docker image inspect "$ref" >/dev/null 2>&1 || docker pull -q "$ref" >/dev/null ||
    die 1 "$ref is in neither the local image store nor the local registry"
  note "built and pushed $ref (with SBOM and provenance)"

  # Sign the digest. cosign reaches the registry by its container name on its internal network,
  # the same name `ap` verifies with, so the signature's identity matches at deploy time.
  cosign_run --network "$reg_net" \
    -v "$signing/cosign.key:/k/cosign.key:ro" \
    -v "$signing/signing-config.json:/k/signing-config.json:ro" \
    "$cosign_image" sign --yes --key /k/cosign.key --signing-config /k/signing-config.json \
    --allow-http-registry "$reg:5000/ap-$n@$digest" >&2 ||
    die 1 "cosign could not sign $ref"
  note "signed $ref"

  # Record it and its fingerprint, under the lock.
  mkdir -p -- "$state"
  chmod 700 -- "$state"
  take_lock "$state/images.env.lock"
  replace_line "$images_env" "AP_IMAGE_$N" "$ref"
  replace_line "$built_env" "AP_BUILT_$N" "$fp"
  release_lock
  note "recorded AP_IMAGE_$N=$ref in $images_env"
}

if [ -z "$changed" ]; then
  start_registry
  make_signing_material
  candidate "${names[0]}" "$@"
  build_one "${names[0]}" "$fingerprint" "$@"
  exit 0
fi

# --changed: the images whose fingerprint differs from the recorded one, or that are not recorded.
todo=() todo_fp=()
for name in "${names[@]}"; do
  N=$(upper "$name")
  if [ -n "$gen_stale" ] && is_go "$name"; then
    todo+=("$name") todo_fp+=('')
    continue
  fi
  candidate "$name"
  if [ -n "$(recorded "$images_env" "AP_IMAGE_$N")" ] && [ "$fingerprint" = "$(recorded "$built_env" "AP_BUILT_$N")" ]; then
    continue
  fi
  todo+=("$name") todo_fp+=("$fingerprint")
done
if [ -n "$check" ]; then
  if [ -n "$gen_stale" ]; then say "the generated code is missing or stale; a build regenerates it"; fi
  if [ ${#todo[@]} -eq 0 ]; then say "nothing to build"; exit 0; fi
  say "would build ${todo[*]}"
  exit 1
fi
[ ${#todo[@]} -gt 0 ] || exit 0
say "building ${todo[*]}"

# One password for the whole run, asked only now that something must be signed.
if [ -n "${COSIGN_PASSWORD+set}" ]; then
  password_from_env
elif [ -t 0 ]; then
  IFS= read -rs -p "build: password of the signing key: " signing_pw || signing_pw=''
  printf '\n' >&2
  [ -n "$signing_pw" ] || die 1 "the password is empty; the private key must be encrypted"
  if [ ! -e "$signing/cosign.key" ]; then
    IFS= read -rs -p "build: the same password again, to confirm: " again || again=''
    printf '\n' >&2
    [ "$again" = "$signing_pw" ] || die 1 "the two passwords differ; nothing was signed"
    again=''
  fi
else
  die 1 "set COSIGN_PASSWORD or run from a terminal: signing ${todo[*]} needs the signing key's password"
fi

start_registry
make_signing_material
i=0
for name in "${todo[@]}"; do
  if [ -z "${todo_fp[$i]}" ]; then candidate "$name"; todo_fp[i]=$fingerprint; fi
  build_one "$name" "${todo_fp[$i]}"
  i=$((i + 1))
done
say "built and signed ${todo[*]}"
