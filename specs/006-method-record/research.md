# Research: Results are reused only under the method that judged them

Phase 0 of the [plan](plan.md) for the [spec](spec.md). Each decision names its deciding source:
the code on `main` (`e4348bf`), a reproduced experiment, or a cited document. Decisions marked
*owner* were put to the project owner as direct questions.

## Sources

| ID | Source |
|----|--------|
| S1 | `src/engine/machine/reuse.go` on `main`: `KeyParts{PatternVersion, Step, Inputs, AgentDigest, Model, Round}`; `ReuseKey` frames each part by its length and writes `round` only when it is not 0, "so a key without a round is unchanged" (research D36 of feature 001). `machine.Event` carries `pattern`, `agent_digest`, `model`, `reuse_key`; no field names the checks' or the procedures' code |
| S2 | `src/engine/durable/workflow.go` `stepKey`: the workflow computes the key itself when no findings are open and passes it in `ReuseLookup.Key`; `durable/activities.go` `LookupReuse` computes it only when `Key` is empty. The workflow's input (`in.Pattern`) is recorded by Temporal at the run's start, so a value taken from it stays the same after an engine upgrade while the run resumes |
| S3 | `src/engine/api/runs.go` (lines 160-190): at start the engine rebuilds the accepted-result store from the run records' `StepAccepted`, `ResultReused` and open-round `StepRejected` events, by their recorded `reuse_key` |
| S4 | `src/engine/slices/persist.go`: a slice record stores `checks` (`part`, `check`, `passed`, `reason`, `from_pattern`) and restores them as they are on `load`; `service.go` `find` is the one lookup behind every slice operation but `List`; `recheck` runs the part checks, and the contract compatibility and lint checks; a check that could not run is a failed check with its reason (`checks.go` `compatibility`); a part whose check fails loses its approval, recorded as `withdrawn` (`service.go` ~680) |
| S5 | `git log -- src/catalogue/api-feature-go/pattern.yaml`: six commits since `effded5`, `version: 1.0.0` throughout (T136) |
| S6 | What runs a step and judges it, on `main`: the engine image (`Dockerfile.engine`: the whole engine module, embedding the pattern, the template's declaration and the agent definitions through `scripts/generate.sh`'s `gen/` copies); the check and project worker images (`Dockerfile.check`, `Dockerfile.project`: `cmd/check`, the template, `alpine-git`); the agent image (`Dockerfile.agent`: the `docker-agent` and `golang` pins); the lint service (`Dockerfile.lint`, `src/deploy/lint/`, the `node` pin). `go list -deps ./cmd/check ./machine ./slices` reaches, in this repository, the `engine`, `check`, `slice` and `retry` modules only |
| S7 | `src/deploy/build.sh` header and ADR 0061: each image has a fingerprint, the digest of a reproducible build (`SOURCE_DATE_EPOCH=0`, `rewrite-timestamp=true`), known only after the image is built |
| S8 | `git ls-files --cached --others --exclude-standard` ([git-ls-files](https://git-scm.com/docs/git-ls-files)): the tracked files and the untracked files that no ignore rule covers, which is what a Docker build with `src/` as its context sees, less what `.gitignore` names (generated code, local state) |
| S9 | Experiment E1 below |
| S10 | Constitution: Principle XX ("Every accepted result MUST record the method, inputs, model, and rule versions"), XVII ("Derive the rest", "Prove agreement"), Engineering Principles "Values that check themselves", "Checks prove they can fail", Clean tree |

## R1. What the method identity is

- **Decision**: the method identity is the SHA-256, in lowercase hex, of a manifest of the
  method's source files and pins, declared once in `src/engine/method/method.yaml`
  ([contract](contracts/method-declaration.md)): every file a `git ls-files --cached --others
  --exclude-standard` lists (S8) that matches an `include` glob and no `exclude` glob, as
  `<sha256 of its bytes>  <path>` lines in path order, then one `pin <name> <reference>` line per
  named pin of `src/deploy/images.lock`, in the declared order. The declaration includes the
  catalogue, the agent definitions and their instructions, the `engine`, `check`, `slice` and
  `retry` modules with `src/go.work`, the Dockerfiles and lint service of the images that run or
  judge a step (S6), and the pins `docker-agent`, `golang`, `alpine`, `alpine-git`, `node`; it
  excludes `README.md` files, `_test.go` files and `testdata/`. The declaration lies inside
  `src/engine/`, so a change to it changes the identity too.
- **Rationale**: FR-001 and FR-002 ask for an identity that is the same for every build of the
  same source and that changes with any part of the method. Hashing the declared inputs is what
  input-addressed builds do; it needs no image built, so the engine's tests, CI and the
  developer's host compute the same value (E1); paths and file bytes are all it reads, so no build
  time, folder or host enters it. The engine's module is taken whole (spec, Clarifications):
  splitting it by package would need a second declaration of which packages judge, kept in step
  with the code by hand.
- **Alternatives**:
  - *A version bump the build checks* (T136's first option): a hand-set version in
    `pattern.yaml` and a check that fails when a method file changes without it. It still needs the
    same file list to know a bump is due, and a person to pick the new number; the digest is what
    the check would compare anyway (Principle XVII, "Derive the rest").
  - *The images' fingerprints* (S7): exact for what runs, but known only after every image is
    built; the engine cannot carry its own; and the engine's Go tests, which judge reuse, run
    without images.
  - *Git tree IDs* (`git rev-parse HEAD:src/engine`): Git's own content hashes, but per folder
    only, with tests and documentation inside, and blind to uncommitted changes a local build
    ships.
  - *Go's build ID*: stripped from our binaries (`-buildid=`), and it covers only Go code.
- **Limit**: a change that does not change what is produced, such as a refactoring of the engine's
  API code, still changes the identity and costs one more model call per step on the next run
  (spec, Assumptions).

## R2. How the identity reaches the engine and stays true

- **Decision**: one converging script, `scripts/method-digest.py`, computes the identity from the
  declaration and writes it to `src/engine/method/method.sha256` (gitignored: it is derived, Clean
  tree); run again it changes nothing; `--check` changes nothing and exits 1 naming the stored and
  the computed identity when they differ, or when the file is missing. The Go package
  `src/engine/method` embeds the file (`//go:embed method.sha256`), so the engine does not build
  without it, and parses it into a `method.ID` that rejects anything but 64 lowercase hex digits;
  the engine refuses to start without a valid one. `scripts/check.sh` (and its `--prepare`, which
  CI's Go jobs run) and `src/deploy/build.sh` run the script before any Go build; `check.sh` runs
  `--check` among its drift checks (FR-010).
- **Rationale**: Principle XVII "Derive the rest": the identity is generated from its declaration
  and checked against it, never kept by hand. A separate script, rather than a step of
  `scripts/generate.sh`, keeps generate.sh to its one job (contracts to code and copies), and
  leaves generate.sh's own tests, which copy only that script and `src/` into a folder that is not
  a Git repository, as they are.
- **Alternatives**: a `generate.sh` step (its gen/ copy rule maps `gen/<path>` to `src/<path>`,
  which a digest is not, and its tests would change); an environment variable set by `ap` (deploy
  configuration, but the identity is a fact of the build, and a launcher could set a wrong one).

## R3. The reuse key and the run record

- **Decision**: `KeyParts` gains `Method`, written into the key after the round and only when it
  is not empty, as `Round` is (S1): a key without a method is unchanged, and the engine always has
  one (R2), so every key it computes now differs from every key recorded before (FR-008), and no
  existing test that computes a key without a method changes. Every step outcome event
  (`AttemptMade`, `StepAccepted`, `StepRejected`, `ResultReused`) gains `method`, the identity its
  key was computed with, so a reader can recompute the key from the event's own fields.
- **Durable runs**: the method enters the key on the engine's side, never from the workflow: the
  workflow no longer computes keys; `LookupReuse` always computes the key from the parts, with the
  engine's current identity, and returns it with the method used. The workflow records both on the
  step's events. A run resumed after an upgrade therefore looks up its remaining steps under the
  new identity (spec, Edge Cases), while its recorded input (S2) still names the pattern it started
  with. The activity calls and their order do not change, so the recorded histories still replay.
- **The non-durable driver** (`machine.Machine`, kept by feature 002) takes the identity in its
  `Config` and puts it in the same parts.
- **Rationale**: FR-003, FR-004, FR-008; Principle XX. The accepted-result store is rebuilt from
  the recorded keys (S3), so a key that covers the method is all that reuse needs; no store
  migration.
- **Alternatives**: a separate comparison of a stored method beside the key (two rules where the
  key already is one); stamping the method in `RecordEvent` (it could name a different identity
  than the key's after a restart between the lookup and the record).

## R4. A slice's check results

- **Decision**: a slice record stores `checks_method`, the identity its `checks` were produced
  under. The slice service is given the engine's identity. When `find` or `List` reaches a record
  whose `checks_method` differs from it (an older record has none), the service runs `recheck`
  with every check (part checks, compatibility, lint), applies the approval rule as after any check
  (S4), sets `checks_method` and saves the record before it answers. A check that could not run is
  a failed check with its reason, as today; the slice cannot be built while it fails (spec, Edge
  Cases). The slice view gains `checks_method`.
- **Rationale**: FR-005 to FR-008. Checking on first use, rather than while the engine loads its
  store, runs the checks once the lint service is up; `find` is the one path to a slice (S4).
- **Alternatives**: re-check at `load` (the lint service may not be up when the engine starts, so
  every contract check would fail "could not run"); record the pattern's `id@version` beside the
  checks, as T144 first proposed (S5 shows that version does not follow the pattern's changes).
- **Limit**: the first read of each slice after an upgrade waits for its checks (a lint and a
  comparison call); one developer holds a few slices.

## E1. The identity is reproducible and selective (2026-11-22)

A prototype of R1's computation (Python, `fnmatch` globs, the declaration above) over this
repository at `e4348bf`:

| Tree | Files | Identity (first 12 hex digits) |
|------|-------|--------------------------------|
| the working tree | 120 | `403eee018e41` |
| a fresh `git clone` into another folder | 120 | `403eee018e41` |
| the clone, a line added to `src/engine/README.md` and to `src/cli/contract.md` | 120 | `403eee018e41` |
| the clone, a comment added to `src/engine/checks/shared.go` | 120 | `09b65ebdcfae` |

It took 0.04 s. Same source in another folder gives the same identity, documentation and the
terminal client do not change it, and a change to a check does (SC-002).
