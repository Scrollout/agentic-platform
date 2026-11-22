# src/engine/method

Gives the platform's method one identity: the same for the same source, different when any
declared part of the method differs.

The method is everything that runs a step or judges its result: the patterns, the agent
definitions, the engine, the checks, the fixed procedures and the images that run them. An
accepted result is reused, and a slice's check results are kept, only under the identity they
were produced under ([ADR 0079](../../../docs/decisions/0079-method-identity-is-a-digest-of-declared-sources.md),
[ADR 0080](../../../docs/decisions/0080-results-reused-and-checks-kept-only-under-their-method.md)).

This file is the contract of the declaration, the identity, the command that derives it and the
Go package that gives it to the engine.

## The declaration: `method.json`

```json
{
  "include": [
    "src/catalogue/*", "src/agents/*",
    "src/engine/*", "src/check/*", "src/slice/*", "src/retry/*",
    "src/go.work", "src/.dockerignore",
    "src/deploy/Dockerfile.engine", "src/deploy/Dockerfile.check", "src/deploy/Dockerfile.project",
    "src/deploy/Dockerfile.agent", "src/deploy/Dockerfile.lint", "src/deploy/lint/*",
    "scripts/tools/package.json", "scripts/tools/package-lock.json"
  ],
  "exclude": [
    "README.md", "*/README.md", "*_test.go", "*/testdata/*",
    "src/engine/method/method.sha256"
  ],
  "pins": ["docker-agent", "golang", "alpine", "alpine-git", "node", "oapi-codegen"]
}
```

`include` and `exclude` are globs over repository paths; `pins` names lines of
`src/deploy/images.lock`. Globs are matched case-sensitively against the whole path, and `*` also
matches `/` (Python's `fnmatch.fnmatchcase`). `README.md` excludes only a file of exactly that
name: `NOTREADME.md` and `readme.md` are included. The declaration lies inside `src/engine/`, so a
change to it changes the identity too.

Left out on purpose, with the reasons in
[research R1 of feature 006](../../../specs/006-method-record/research.md): `src/go.work.sum`,
`src/deploy/compose.yaml`, `scripts/generate.sh`, the `oasdiff` pin, the test-only pins and the
durable services' pins.

Rules (each breach exits 2 and names the key or entry, before anything is written):

- exactly the keys `include`, `exclude` and `pins`, each a non-empty list of distinct non-empty
  strings;
- every `include` glob matches at least one file (a glob that matches nothing is a typo that would
  drop a part silently);
- every pin names a line of `src/deploy/images.lock`;
- no selected path is a symbolic link.

## The identity

1. The files: `git ls-files -z --cached --others --exclude-standard` from the repository root,
   kept when the path matches an `include` glob and no `exclude` glob, and is a regular file. An
   untracked file that no ignore rule covers counts; an ignored file does not; a tracked file
   deleted from the working tree is not listed.
2. The manifest: per file, in byte order of the path, `<sha256 hex of its bytes>  <path>\n` (two
   spaces); then per pin, in declared order, `pin <name> <reference>\n` with the reference as
   `images.lock` writes it.
3. The identity: the SHA-256 of the manifest, 64 lowercase hex digits.

No time, folder, host or file mode enters it: the same tree in another folder or on another host
gives the same identity.

## The command: `scripts/method-digest.py`

| Call | Does | Exit |
|------|------|------|
| `scripts/method-digest.py` | writes the identity and a newline to `src/engine/method/method.sha256` when that file differs; prints `method: <identity> (<n> files, <m> pins)` and `written` or `unchanged` | 0; 2 on a bad declaration or outside a Git work tree |
| `scripts/method-digest.py --check` | writes nothing; prints the same line, and on a difference `method: src/engine/method/method.sha256 holds <stored or "nothing">, the sources give <identity>; run scripts/method-digest.py and commit the file` | 0 when equal, 1 when they differ or the file is missing, 2 as above |
| `scripts/method-digest.py --list` | writes nothing; prints the manifest | 0; 2 as above |

Run twice, the first call changes nothing the second time; an unchanged file keeps its bytes and
its modification time.

`method.sha256` is committed (plan of feature 006, Exceptions): whoever changes a file the
declaration includes runs the command and commits the file with the change.
`scripts/tests/test_method_identity_current.py` runs `--check` in the test command, so a commit
whose identity is stale fails.

## The Go side

- `method.ID`: a method identity, 64 lowercase hex digits; it holds only valid values.
- `method.Parse(s string) (ID, error)`: accepts exactly 64 lowercase hex digits after one trailing
  newline is trimmed; anything else (empty, too short or long, upper case, a non-hex character, a
  space, a second newline) is an error naming what was found.
- `method.Current() ID`: the identity embedded from `method.sha256`, parsed once.
- The engine's start fails, naming `src/engine/method/method.sha256` and
  `scripts/method-digest.py`, when the embedded identity does not parse.

The package depends on no other component.
