# Draft: the terminal contract's `up`, `page` and `down` (feature 003)

Draft of the change to the authoritative [terminal contract](../../../src/cli/contract.md). It
replaces that contract's paragraphs on `up` and `page` and grows it additively (constitution XV):
every command line that worked before keeps working with the same meaning; `up` without options,
which was a usage error, now starts the platform with the defaults. It moves into
`src/cli/contract.md`, reviewed, before any code (Contract-first); this copy is then removed.

## `up`

```text
ap up [--credentials <file>] [--projects <folder>]
      [--listen <address> [--name <host>]... [--certificate <cert> --key <key>]]
```

Starts the platform from the current source and returns when it is ready. It is safe to run in
any state; with nothing changed it changes nothing.

| Option | Default | Meaning |
|--------|---------|---------|
| `--credentials <file>` | `${XDG_CONFIG_HOME:-~/.config}/agentic-platform/anthropic_api_key` | The file holding only the model provider's key, mode 600, owned by the developer |
| `--projects <folder>` | `~/projects`, made with mode 755 when missing | Where new projects are built; given explicitly, it must exist |
| `--listen <address>` | none: the run page is reachable from this machine only | Let other machines reach the run page on `<address>` (`0.0.0.0`, `::` or one address of this machine), encrypted and behind a sign-in |
| `--name <host>` | with `0.0.0.0` or `::`: this machine's host name, the addresses `hostname -I` lists, `127.0.0.1` and `localhost`; with one address: that address | A name or address the run page answers to; repeatable; only with `--listen` |
| `--certificate <cert> --key <key>` | a certificate the platform makes for the names | A PEM certificate and its key to serve instead; both or neither; only with `--listen` |

What `up` does, in this order, each step only when needed:

1. Checks the machine, before anything else, and exits 3 naming each failure and its fix: Docker
   28 or later; the gVisor runtime `runsc` registered with Docker (naming the install guide); the
   key file (missing, empty, not the developer's, readable or writable by others, or inside the
   projects folder); the projects folder (missing when given, not writable, or not open to
   others, naming its mode); a `--listen` address that is not an IP address, a `--name` that is
   not a host name or IP address, `--name` or `--certificate` without `--listen`, one of
   `--certificate` and `--key` without the other, or a certificate or key that is not readable
   PEM.
2. Builds the images whose result changed since their last signed build, and only those
   (research R1). Before it builds it prints `up: building <image> <image> ...` on standard
   error; when it must sign and `COSIGN_PASSWORD` is not set, it asks once for the signing key's
   password on the terminal (twice, to confirm, when it makes the key pair on the first start).
   Without a terminal and without `COSIGN_PASSWORD` it exits 1 naming the images it would sign,
   and starts nothing. A failed build exits 1 naming the image, with the build's own error; images
   already built are kept. With nothing to build it prints nothing for this step.
3. Verifies every image it starts (ADR 0028), exit 1 naming the first that fails.
4. Starts the platform and waits until it is ready, as before.
5. Prints, on standard error, `up: started` (or `up: already started; nothing changed`) and the
   run page's address. With `--listen`, it also prints:

   ```text
   up: the run page accepts connections from other machines on <address>:<port>
   up: open https://<name>:<port>  (names: <name>, <name>, ...)
   up: sign in as ap with <secret>
   up: certificate sha256 <AB:CD:...>  (compare it once in your browser)
   up: recorded in <state>/agentic-platform/access.log
   ```

Only one `up` runs at a time; a second one waits for the first and then changes nothing. `up` on
a started session with another key file, projects folder, `--listen`, `--name` or certificate
refuses (exit 3) and says to run `down` first. The secret is new at each start after `down` and
kept by a second `up`.

Exit codes as before: 0 started; 1 a build, a verification or Compose failed; 3 a usage error or a
failed prerequisite.

## `page`

Prints the run page's address: `http://127.0.0.1:<port>` without remote access; with it, one
`https://<name>:<port>` line per name, then `sign in as ap with <secret>` and the certificate's
fingerprint.

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
