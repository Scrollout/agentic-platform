# Contract draft: the method declaration and its identity

Draft for T001. The authoritative copy is `src/engine/method/README.md` (with the declaration
`src/engine/method/method.json` beside it); this file then becomes a link to it (Engineering
Principles, Contracts between components).

## What it does for its caller

`scripts/method-digest.py` gives the platform's method one identity: the same for the same
source, different when any declared part differs ([research R1](../research.md)).

## The declaration: `src/engine/method/method.json`

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

Globs are matched case-sensitively against the whole repository path (Python's `fnmatchcase`:
`*` also matches `/`). Left out on purpose, with the reasons in research R1: `src/go.work.sum`,
`src/deploy/compose.yaml`, `scripts/generate.sh`, the `oasdiff` image pin, test-only and
durable-service pins.

Rules (each breach exits 2 and names the key or entry, before anything is written):

- exactly the keys `include`, `exclude` and `pins`, each a non-empty list of distinct non-empty
  strings;
- every `include` glob matches at least one file (a glob that matches nothing is a typo that
  would drop a part silently);
- every pin names a line of `src/deploy/images.lock`;
- no selected path is a symbolic link.

## The identity

1. The files: `git ls-files -z --cached --others --exclude-standard` from the repository root,
   kept when the path matches an `include` glob and no `exclude` glob, and is a regular file (a
   tracked file deleted from the working tree is not listed).
2. The manifest: per file, in byte order of the path, `<sha256 hex of its bytes>  <path>\n`; then
   per pin, in declared order, `pin <name> <reference>\n` with the reference as `images.lock`
   writes it.
3. The identity: the SHA-256 of the manifest, 64 lowercase hex digits.

No time, folder, host or file mode enters it.

## The command

| Call | Does | Exit |
|------|------|------|
| `scripts/method-digest.py` | writes the identity and a newline to `src/engine/method/method.sha256` when that file differs; prints `method: <identity> (<n> files, <m> pins)` and `written` or `unchanged` | 0; 2 on a bad declaration or outside a Git work tree |
| `scripts/method-digest.py --check` | writes nothing; prints the same line, and on a difference `method: src/engine/method/method.sha256 holds <stored or "nothing">, the sources give <identity>; run scripts/method-digest.py and commit the file` | 0 when equal, 1 when they differ or the file is missing, 2 as above |
| `scripts/method-digest.py --list` | writes nothing; prints the manifest | 0; 2 as above |

Run twice, the first call changes nothing the second time (Principle XVII). The identity file is
committed; `scripts/tests/test_method_identity_current.py` runs `--check` in the test command
(research R2).

## The Go side: package `src/engine/method`

- `method.Current() ID`: the embedded identity, parsed once.
- `method.Parse(string) (ID, error)`: accepts exactly 64 lowercase hex digits after one trailing
  newline is trimmed; anything else is an error naming what was found.
- The engine's start fails, naming `src/engine/method/method.sha256` and
  `scripts/method-digest.py`, when the embedded identity does not parse.
