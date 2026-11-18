# Data model: Start the platform with one command, and reach its run page from another machine

The feature adds no engine data. Everything below is the launcher's local state on the developer's
machine, under `${XDG_STATE_HOME:-~/.local/state}/agentic-platform/` (folder mode 700, owned by the
developer), next to feature 001's `session.env` and `images.env` (`src/deploy/ap`, ADR 0028),
and the lock `start.lock` that `ap start` holds while it runs (research R4).

## Image fingerprint (`built.env`, new)

One line per image, `AP_BUILT_<NAME>=sha256:<64 hex>`, written by `build.sh` under the lock it
already takes for `images.env`, after the image is signed and recorded. `images.env` keeps only
its `AP_IMAGE_*` lines, which `ap up` reads as today.

- **Value**: the digest of the image's candidate build (research R1): the same Dockerfile, build
  arguments and context, `SOURCE_DATE_EPOCH=0`, `rewrite-timestamp=true`, no SBOM or provenance,
  exported with `unpack=false` (research S10). The pushed image's own digest is not the
  fingerprint.
- **Rule**: an image is *current* when its candidate digest equals its `AP_BUILT_<NAME>` and its
  `AP_IMAGE_<NAME>` line exists; otherwise it is built, signed and both lines are replaced. A
  missing line means not current. `ap up` then verifies every image it starts, as today.
- **Kept**: across `ap down`; only a new build replaces a line.

## Session (`session.env`, extended)

Feature 001's session gains, only for a start with remote access:

| Variable | Meaning | Rule |
|----------|---------|------|
| `AP_PAGE_ADDRESS` | The address the page's port is published on | The `--listen` value: `0.0.0.0`, `::`, or an address `hostname -I` lists; absent without remote access, when the page stays on `127.0.0.1` |
| `AP_PAGE_NAMES` | The names and addresses the page answers to | Space-separated; each a host name (`[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?` labels joined by `.`) or an IP address; at least one |
| `AP_PAGE_SECRET` | The sign-in secret | 64 lowercase hex characters; new at each new session, kept by a second `up` |
| `AP_PAGE_CERT_DIR` | The folder holding the certificate, key, password file and names file | Under the state folder, mode 700 |

A second `up` that would change the running session's remote access in either direction
(turning it on or off, or another address, names or certificate) is refused, as another key file
or projects folder is today.

## Remote-access files (`page-remote/`, new)

| File | Content | Mode | Made |
|------|---------|------|------|
| `cert.pem`, `key.pem` | The certificate (SAN = `AP_PAGE_NAMES`, ECDSA P-256, 365 days) and its key, or copies of the developer's own | 600 | When missing, when the names change, when older than 358 days, or when the developer gives others |
| `cert.names` | The names the certificate was made for, and the time it was made | 600 | With the certificate |
| `htpasswd` | `ap:<SHA-512 crypt of AP_PAGE_SECRET>` | 600 | At each new session |
| `names.conf` | `server_name <names>;` and the page's own Host (`127.0.0.1:<port>`), as nginx directives | 600 | At each start with remote access, from `AP_PAGE_NAMES` checked by the rule above |

The forwarder mounts the folder read-only and runs as the developer's user and group.

## Access record (`access.log`, new)

Append-only, one line per change of remote access, never removed by `ap down`:

```text
2026-11-18T10:00:00Z on address=0.0.0.0 port=7878 names=vm.example,192.0.2.10,127.0.0.1,localhost sha256=AB:CD:...
2026-11-18T12:00:00Z off
```

`on` is written by a start that turns remote access on; `off` by the first start without it after
one with it. The secret is never written here.

## State transitions of remote access

```text
stopped ──ap up --listen A──▶ remote(A, names, secret₁)
remote ──ap up (same)──▶ remote (nothing changes)
remote ──ap up --listen B, other names, or no --listen──▶ refused: run ap down first
local (started) ──ap up --listen A──▶ refused: run ap down first
remote ──ap down──▶ stopped (secret₁ removed with the session)
stopped ──ap up──▶ local (access.log: off)
stopped ──ap up --listen A (again)──▶ remote(A, names, secret₂: a new secret)
```
