---
type: Guide
title: Install the gVisor runtime for Docker
description: Install gVisor's runsc once on a Linux developer machine and register it with Docker, so the platform's step and check containers run sandboxed.
tags: [guides, gvisor, sandbox, docker]
status: draft
sources:
  - id: gvisor-install
    resource: https://gvisor.dev/docs/user_guide/install/
    title: gVisor installation
  - id: gvisor-docker
    resource: https://gvisor.dev/docs/user_guide/quick_start/docker/
    title: gVisor Docker quick start
  - id: gvisor-systemd
    resource: https://gvisor.dev/docs/user_guide/systemd/
    title: gVisor systemd cgroup driver
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-12T08:00:00Z }
---

# Goal

Docker can start containers with the gVisor runtime under the name `runsc`, which the platform's
Compose file requires for every agent step and check container (research D13). This is the only
thing the platform installs on the host besides the container runtime, and it is part of that
runtime.

Sources: [gVisor installation](https://gvisor.dev/docs/user_guide/install/).

# Prerequisites

- Linux on x86_64 or ARM64 with kernel 5.6 or later: `uname -srm`.
- Docker Engine running as a system service (not Docker Desktop, not rootless Docker).
- `sudo` rights on the machine.
- Know your Docker cgroup setup: `docker info --format '{{.CgroupVersion}} {{.CgroupDriver}}'`.
  `2 systemd` is the Ubuntu default.

Sources: [gVisor installation](https://gvisor.dev/docs/user_guide/install/);
[gVisor systemd cgroup driver](https://gvisor.dev/docs/user_guide/systemd/).

# Steps

1. Install the tools the repository setup needs:

   ```sh
   sudo apt-get update
   sudo apt-get install -y apt-transport-https ca-certificates curl gnupg
   ```

2. Add gVisor's signing key and its apt repository. Apt then checks the signature of every
   package it installs from it:

   ```sh
   curl -fsSL https://gvisor.dev/archive.key | sudo gpg --dearmor -o /usr/share/keyrings/gvisor-archive-keyring.gpg
   echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/gvisor-archive-keyring.gpg] https://storage.googleapis.com/gvisor/releases release main" | sudo tee /etc/apt/sources.list.d/gvisor.list > /dev/null
   ```

3. Install `runsc`:

   ```sh
   sudo apt-get update && sudo apt-get install -y runsc
   ```

   When Docker is already installed, the package registers the `runsc` runtime with Docker
   itself.

4. Register the runtime with Docker if `docker info --format '{{json .Runtimes}}'` does not list
   `runsc`, then restart Docker:

   ```sh
   sudo runsc install
   sudo systemctl restart docker
   ```

   `runsc install` adds the runtime to `/etc/docker/daemon.json` under the name `runsc`.
   Restarting Docker stops running containers; run `src/deploy/ap down` first if the platform is
   up.

5. Record the installed version for the dependency review: `runsc --version`.

Sources: [gVisor installation](https://gvisor.dev/docs/user_guide/install/);
[gVisor Docker quick start](https://gvisor.dev/docs/user_guide/quick_start/docker/).

# Verification

1. The runtime runs a container:

   ```sh
   docker run --rm --runtime=runsc hello-world
   ```

2. The runtime applies the resource limits the Compose file sets on step and check containers:

   ```sh
   docker run --rm --runtime=runsc --cpus 1 --memory 256m --pids-limit 64 hello-world
   ```

   If this fails with a cgroup error while the plain run in step 1 works, Docker uses the systemd
   cgroup driver and `runsc` must be told so (it needs systemd 244 or later and cgroup v2). Register
   the runtime again with the flag, restart Docker and repeat both checks:

   ```sh
   sudo runsc install -- --systemd-cgroup
   sudo systemctl restart docker
   ```

Under gVisor, a resource limit is enforced on the whole sandbox, not on one process: going over
the memory or process limit stops the container (exit 137 for memory) instead of failing one
allocation or `fork`. The sandbox's own threads count toward the process limit, so a very low
`--pids-limit` (16 on the first machine tested) stops the container from starting; 64 and the
Compose file's 512 work.

`docker run --runtime=runsc --rm alpine dmesg` shows gVisor's own boot messages, but gVisor warns
that this output is easy to fake, so it is a hint, not a security check.

Sources: [gVisor Docker quick start](https://gvisor.dev/docs/user_guide/quick_start/docker/);
[gVisor systemd cgroup driver](https://gvisor.dev/docs/user_guide/systemd/); feature 001 research D13 (tested 2026-11-12).
