# Contract draft: the method declaration and its identity

Draft for T001. The authoritative copy is the header comment of
`src/engine/method/method.yaml` and the usage of `scripts/method-digest.py`; this file then
becomes a link to them (Engineering Principles, Contracts between components).

## What it does for its caller

`scripts/method-digest.py` gives the platform's method one identity: the same for the same
source, different when any declared part differs ([research R1](../research.md)).

## The declaration: `src/engine/method/method.yaml`

```yaml
include:        # fnmatch globs over repository paths; `*` also matches `/`
  - src/catalogue/*
  - src/agents/*
  - src/engine/*
  - src/check/*
  - src/slice/*
  - src/retry/*
  - src/go.work
  - src/go.work.sum
  - src/deploy/Dockerfile.engine
  - src/deploy/Dockerfile.check
  - src/deploy/Dockerfile.project
  - src/deploy/Dockerfile.agent
  - src/deploy/Dockerfile.lint
  - src/deploy/lint/*
exclude:        # globs; a path matching any is left out
  - "*README.md"
  - "*_test.go"
  - "*/testdata/*"
pins:           # names in src/deploy/images.lock, in this order
  - docker-agent
  - golang
  - alpine
  - alpine-git
  - node
```

Rules (each breach exits 2 and names the key or entry, before anything is written):

- exactly the keys `include`, `exclude` and `pins`, each a non-empty list of distinct non-empty
  strings;
- every `include` glob matches at least one file (a glob that matches nothing is a typo that
  would drop a part silently);
- every pin names a line of `src/deploy/images.lock`.

## The identity

1. The files: `git ls-files -z --cached --others --exclude-standard` from the repository root,
   kept when the path matches an `include` glob and no `exclude` glob, and is a regular file.
2. The manifest: per file, in byte order of the path, `<sha256 hex of its bytes>  <path>\n`; then
   per pin, in declared order, `pin <name> <reference>\n` with the reference as `images.lock`
   writes it.
3. The identity: the SHA-256 of the manifest, 64 lowercase hex digits.

No time, folder, host or file mode enters it.

## The command

| Call | Does | Exit |
|------|------|------|
| `scripts/method-digest.py` | writes the identity and a newline to `src/engine/method/method.sha256` when that file differs; prints `method: <identity> (<n> files, <m> pins)` and `written` or `unchanged` | 0; 2 on a bad declaration or outside a Git work tree |
| `scripts/method-digest.py --check` | writes nothing; prints the same line, and on a difference `method: src/engine/method/method.sha256 holds <stored or "nothing">, the sources give <identity>` | 0 when equal, 1 when they differ or the file is missing, 2 as above |
| `scripts/method-digest.py --list` | writes nothing; prints the manifest | 0; 2 as above |

Run twice, the first call changes nothing the second time (Principle XVII).

## The Go side: package `src/engine/method`

- `method.Current() ID`: the embedded identity, parsed once.
- `method.Parse(string) (ID, error)`: accepts exactly 64 lowercase hex digits (a trailing newline
  of the file is trimmed before); anything else is an error naming what was found.
- The engine's start fails, naming the file and `scripts/method-digest.py`, when the embedded
  identity does not parse.
