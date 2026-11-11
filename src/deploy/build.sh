#!/usr/bin/env bash
# Build one image: deploy/build.sh <agent|check|engine|cli|page> [docker build options]
# Reads every base image from deploy/images.lock (the only home of pins) and passes it as a build arg.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
name=${1:?usage: build.sh <agent|check|engine|cli|page> [docker build options]}
shift
pin() { awk -v n="$1" '$1 == n { print $2 }' "$here/images.lock"; }
exec docker build -f "$here/Dockerfile.$name" -t "ap-$name:dev" \
  --build-arg "GOLANG_IMAGE=$(pin golang)" \
  --build-arg "DOCKER_AGENT_IMAGE=$(pin docker-agent)" \
  "$@" "$here/.."
