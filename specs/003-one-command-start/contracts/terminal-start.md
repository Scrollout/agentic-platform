# Draft: the terminal contract's `start`, and `up`'s and `page`'s remote access (feature 003)

Draft of the change to the authoritative [terminal contract](../../../src/cli/contract.md). It adds
the launcher command `start`, adds options to `up` and lines to `page`, and changes nothing else
(constitution XV): every command line that worked before keeps its meaning and output. It moves
into `src/cli/contract.md`, reviewed, before any code (Contract-first); this copy is then removed.
Revised after `/speckit-analyze` round 1: the start command is `start`, and `up` stays as it is
(owner, 2026-11-18).

## `start`

```text
ap start [--check] [--credentials <file>] [--projects <folder>]
         [--listen <address> [--name <host>]... [--certificate <cert> --key <key>]]
```

Starts the platform from the current source and returns when it is ready. It is safe to run in
any state; with nothing changed it changes nothing.

| Option | Default | Meaning |
|--------|---------|---------|
| `--check` | off | Show what `start` would do and change nothing (below) |
| `--credentials <file>` | `${XDG_CONFIG_HOME:-~/.config}/agentic-platform/anthropic_api_key` | The file holding only the model provider's key |
| `--projects <folder>` | `~/projects`, made with mode 755 when missing | Where new projects are built; given explicitly, it must exist |
| `--listen`, `--name`, `--certificate`, `--key` | as `up` (below) | Passed on to `up` unchanged |

What `start` does, in this order:

1. Takes the start lock; a second `start` waits for the first, then finds nothing to do.
2. Checks the machine before anything else, and exits 3 naming each failure and its fix: Docker
   28 or later; the gVisor runtime `runsc` registered with Docker (naming
   `docs/guides/install-gvisor.md`); the key file (missing, empty, not the developer's, readable
   or writable by group or others, or inside the projects folder); the projects folder (missing
   when given with `--projects`, not writable, or not open to others, naming its mode); and the
   remote-access options as `up` checks them.
3. Builds the images whose result changed since their last signed build, and only those, through
   `src/deploy/build.sh --changed`. Its lines, on standard error, are `build: building <names>`
   before it builds and `build: built and signed <names>` after; with nothing to build it prints
   nothing. When it must sign and `COSIGN_PASSWORD` is not set, it asks once for the signing key's
   password on the terminal, without echo (a second time to confirm, only when it first makes the
   key pair). Without a terminal and without `COSIGN_PASSWORD` it exits 1 naming the images it
   would sign, and starts nothing. A failed build exits 1 naming the image, with the build's own
   error; images already built are kept.
4. Runs `up` with the resolved key file, projects folder and remote options, and exits with its
   code. `up` verifies every image and starts the platform, as before.
5. Prints, on standard error, `start: started` or `start: already started; nothing changed`,
   then the run page's address (or, with remote access, `up`'s remote lines).

`start --check` does steps 1 and 2, makes the candidate builds of step 3, and prints, changing no
recorded, signed or running image and no session: each failing prerequisite; `start: would build
<names>` or `start: nothing to build`; and `start: would start` or `start: already started`. It
exits 0 when nothing would change, 1 when something would, and 3 for a failed prerequisite.

Exit codes: 0 started (or, with `--check`, nothing would change); 1 a build, the signing, a
verification or Compose failed (or, with `--check`, something would change); 3 a usage error or a
failed prerequisite.

## `up`: remote access (new options)

```text
ap up --credentials <file> --projects <folder>
      [--listen <address> [--name <host>]... [--certificate <cert> --key <key>]]
```

Without `--listen`, `up` is unchanged in every respect.

| Option | Default | Meaning |
|--------|---------|---------|
| `--listen <address>` | none: the run page is reachable from this machine only | Let other machines reach the run page on `<address>`: `0.0.0.0`, `::`, or an address of this machine |
| `--name <host>` | with `0.0.0.0` or `::`: this machine's host name, the addresses `hostname -I` lists, `127.0.0.1` and `localhost`; with one address: that address | A name or address the run page answers to; repeatable |
| `--certificate <cert> --key <key>` | a certificate the platform makes for the names | A PEM certificate and its key to serve instead |

`up` exits 3, starting nothing, for: a `--listen` value that is not `0.0.0.0`, `::` or an address
`hostname -I` lists; a `--name` that is not a host name (labels of letters, digits and inner
hyphens, joined by dots) or an IP address; `--name`, `--certificate` or `--key` without
`--listen`; one of `--certificate` and `--key` without the other; a certificate or key that is not
readable PEM; and, on a started session, any change of remote access (turning it on or off, or
another address, names or certificate), telling to run `down` first. A port Compose cannot
publish on that address exits 1 naming it, as any Compose failure does.

With `--listen`, `up` also prints on standard error:

```text
up: the run page accepts connections from other machines on <address>:<port>
up: open https://<name>:<port>  (names: <name>, <name>, ...)
up: sign in as ap with <secret>
up: certificate sha256 <AB:CD:...>  (compare it once in your browser)
up: recorded in <state>/agentic-platform/access.log
```

The secret is new at each start after `down` and kept by a second `up`. `access.log` gets one `on`
line per start that turns remote access on, and one `off` line at the first start without it
after one with it; never the secret.

## `page`

Unchanged without remote access (`http://127.0.0.1:<port>`). With it: one `https://<name>:<port>`
line per name, then `sign in as ap with <secret>` and the certificate's fingerprint.

## With remote access, the run page

- Answers only over TLS (1.2 or 1.3) on `<address>:<port>`; a plain HTTP request gets 400 and no
  run content.
- Answers only the names given; any other Host gets 421.
- Asks for HTTP Basic authentication as user `ap` with the secret; without it, or with a wrong
  one, 401 and no run content; each failure is logged with its time and client address, never
  the secret.
- Answers at most 10 requests per second per client address, with a burst of 20; beyond that 429.
- Is the same read-only page as on the machine: nothing reachable through it can change a slice,
  a run or a project, or reach any other part.

## `down`

Unchanged: stops everything, keeps run records, projects, the image records and `access.log`, and
removes the session with its secret.
