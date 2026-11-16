#!/bin/sh
# Installs the gVisor release pinned in src/deploy/images.lock (gvisor-release, gvisor-sha512) on a
# CI runner and registers it as Docker's `runsc` runtime (research D41), as the gVisor guide does
# (docs/guides/install-gvisor.md). The release is checked against its pinned SHA-512 before
# anything is installed, unpacked into a folder of its own, and registered with `runsc install`
# (with `--systemd-cgroup` when Docker uses the systemd cgroup driver); Docker is restarted. It
# fails unless `docker info` then lists runsc and the guide's two smoke runs pass: without runsc
# the test labs would silently fall back to runc. Needs sudo.
set -eu
lock=$(dirname "$0")/../src/deploy/images.lock
release=$(awk '$1=="gvisor-release"{print $2}' "$lock")
sum=$(awk '$1=="gvisor-sha512"{print $2}' "$lock")
[ -n "$release" ] || { echo "ci-gvisor: no gvisor-release pin in $lock" >&2; exit 1; }
[ -n "$sum" ] || { echo "ci-gvisor: no gvisor-sha512 pin in $lock" >&2; exit 1; }
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
curl -fsSL -o "$work/gvisor.tar.bz2" "https://storage.googleapis.com/gvisor/releases/release/$release/x86_64/gvisor.tar.bz2"
got=$(sha512sum "$work/gvisor.tar.bz2" | cut -d' ' -f1)
[ "$got" = "$sum" ] || { echo "ci-gvisor: checksum mismatch: the download's sha512 is $got, the pin is $sum" >&2; exit 1; }
# The runner's temporary folder lasts the job, as long as Docker needs the runtime's files.
dest=${RUNNER_TEMP:-$HOME}/gvisor-$release
mkdir -p "$dest"
tar -xjf "$work/gvisor.tar.bz2" -C "$dest"
if [ "$(docker info --format '{{.CgroupDriver}}')" = systemd ]; then
  sudo "$dest/runsc" install -- --systemd-cgroup
else
  sudo "$dest/runsc" install
fi
sudo systemctl restart docker
docker info --format '{{json .Runtimes}}' | grep -q '"runsc"' ||
  { echo "ci-gvisor: docker info does not list the runsc runtime after the install" >&2; exit 1; }
docker run --rm --runtime=runsc hello-world >/dev/null
docker run --rm --runtime=runsc --cpus 1 --memory 256m --pids-limit 64 hello-world >/dev/null
"$dest/runsc" --version
