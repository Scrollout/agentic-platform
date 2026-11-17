# Quickstart: validating feature 003

Proves the feature end to end on a machine with Docker 28 or later and gVisor installed once
([install guide](../../docs/guides/install-gvisor.md)), and a model key in
`~/.config/agentic-platform/anthropic_api_key` (mode 600). Commands run from the repository root.
The terminal behaviour is in [the contract draft](contracts/terminal-up.md); the files are in
[the data model](data-model.md).

## 1. One command from a fresh clone (User Story 1)

```sh
git clone https://github.com/Scrollout/agentic-platform && cd agentic-platform
src/deploy/ap up
```

Expected: `up: building agent check cli engine lint page project`, one password prompt (twice on
the very first start, to make the signing key), then `up: started` and
`http://127.0.0.1:7878`. `~/projects` exists with mode 755. Then `src/deploy/ap describe` works.
Record the time of this first start.

```sh
time src/deploy/ap up
```

Expected: no build, no prompt, `up: already started; nothing changed`, in under 30 seconds
(SC-002).

```sh
src/deploy/ap down && time src/deploy/ap up
```

Expected: no build, no prompt, `up: started`; earlier slices and runs are still listed by
`src/deploy/ap runs`.

Change code only the run page uses (a string in `src/page`), then `src/deploy/ap up`: it prints
`up: building page` and builds nothing else (SC-003). A change that leaves every built image the
same, such as a README under `src/` (left out of every image), builds nothing.

## 2. Prerequisites are named (User Story 2)

Each case starts nothing and exits 3 with a message naming the problem and its fix (SC-004):

```sh
src/deploy/ap down
src/deploy/ap up --credentials /nonexistent        # names the key file
mkdir -m 700 /tmp/p && src/deploy/ap up --projects /tmp/p   # names the folder and mode 700
```

Without gVisor (on a machine without `runsc`): names the runtime and the install guide.

## 3. The run page from another machine (User Story 4)

On the VM:

```sh
src/deploy/ap down
src/deploy/ap up --listen 0.0.0.0
```

Expected: the remote lines of the contract (address, names, secret, fingerprint, record) and one
`on` line in `~/.local/state/agentic-platform/access.log`.

From another machine, with `<vm>` one of the printed names:

```sh
curl -sk -o /dev/null -w '%{http_code}\n' https://<vm>:7878/                 # 401
curl -sk -o /dev/null -w '%{http_code}\n' -u ap:wrong https://<vm>:7878/     # 401
curl -sk -o /dev/null -w '%{http_code}\n' -u ap:<secret> https://<vm>:7878/  # 200
curl -s  -o /dev/null -w '%{http_code}\n' http://<vm>:7878/                  # 400
```

In a browser, open `https://<vm>:7878`, compare the certificate's SHA-256 fingerprint with the
printed one, accept it, sign in as `ap` with the secret: the run page lists the runs (SC-006).

Then `src/deploy/ap down && src/deploy/ap up`: from the other machine the port no longer answers
(SC-007), the old secret is gone, and `access.log` ends with `off`.

## 4. Nothing else changed

The CI run of the pushed commit is green in every job, `e2e`, `docker`, `us2` and `planted`
included (SC-005).
