# Implementation Plan: Results are reused only under the method that judged them

**Branch**: `006-method-record` | **Date**: 2026-11-22 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/006-method-record/spec.md` (backlog T136 with T144)

## Summary

The platform's method gets one identity: the SHA-256 of a manifest of its declared source files
and pins, declared once in `src/engine/method/method.json`, computed by one converging script,
committed and checked by the test command, and embedded in the engine (research R1, R2). The reuse key covers it, and every step outcome event
names it, so a result accepted under another method is never reused; durable runs take it from the
engine at each lookup, so a run resumed after an upgrade judges its remaining steps under the new
method (R3). A slice stores the identity its checks ran under and runs them again on first use
under another (R4). Records written before the feature count as another method.

## Technical Context

**Language/Version**: Go 1.26 (the engine, as feature 001); Python 3 (the new script, as the other
`scripts/`)

**Primary Dependencies**: none added; Python's standard library (`json`, `hashlib`,
`fnmatch`), Git (`git ls-files`)

**Storage**: the engine store's slice records gain `checks_method`; the run records' step events
gain `method`; no migration (research R3, R4)

**Testing**: Go tests in `src/engine` (`machine`, `durable`, `slices`, `api`, `method`); the
recorded Temporal histories' replay; Python `unittest` for the script on a fixture Git
repository; CI's check run of the pushed commit as the full test command

**Target Platform**: the local platform (Compose), as feature 001

**Project Type**: the engine and the repository's build scripts

**Performance Goals**: SC-004: a fully reused run takes no longer than today, within 1 s; the
identity is computed in well under a second (E1: 0.04 s)

**Constraints**: the activity calls of the durable run workflow and their order do not change
and their conditions (the recorded histories must replay); keys and events without a method are
unchanged byte for byte, and the workflow keeps its own key when the lookup returns none, so
existing tests that compute them stand; `scripts/check.sh`, the new script and
`src/engine/.go-arch-lint.yml` carry the owner's `Approved-by:`; `src/deploy/build.sh` and
`check.sh --prepare` do not change

**Scale/Scope**: one declaration, one script, one Go package, the reuse key, four event kinds,
the slice record and view, two contract schemas

## Constitution Check

*Evaluated before research and again after design: PASS.*

| Principle | Status | How this plan complies |
|-----------|--------|------------------------|
| I. Needs before solutions | Pass | Serves N-050, N-052, N-053 |
| II. Two planes | Not affected | No component or flow between planes changes; the identity is metadata, not run content |
| III. API-first | Pass | The identity is read through the existing slice and run-event views; contract changed first |
| IV. Asynchronous operations | Not affected | No operation added; re-checking happens inside existing reads |
| V, XII. Identity, tenants | Not affected | No call, token or tenant path changes |
| VI. Adopt mature OSS | Pass | Git lists the files and SHA-256 hashes them; no capability that OSS covers is rewritten; alternatives in R1 |
| VII. Providers | Not affected | No provider kind changes |
| VIII. Containers | Pass | Images unchanged but for the embedded identity file; no new image |
| IX. Knowledge stays true | Pass | ADR 0079, ADR 0080 (superseding ADR 0059's key clause); architecture overview and log at implementation |
| X. No silent loss | Pass | A stale result is run again, not dropped; re-checking keeps the slice's content |
| XI. Privacy | Pass | The identity and the manifest hold no protected content |
| XIII. Supply chain | Pass | The pins the method names are those of `images.lock`; a pin change changes the identity |
| XIV. Safe defaults | Pass | An unknown or missing method means not reused and checked again (fail closed); the engine does not start without a valid identity |
| XV. Compatible APIs | Pass | Two optional properties, additive in the preview version |
| XVI. Human control | Pass | `scripts/check.sh`, the new script and `src/engine/.go-arch-lint.yml` carry the owner's `Approved-by:` |
| XVII. Declare, derive, converge | Pass | One declaration; the identity is derived and checked (`--check` in the test command), never kept by hand; the script converges |
| XVIII. Abstractions say what | Pass | One script, one package (`method.Current`), named for what they give |
| XIX. Evals | Not affected | No prompt, model, tool, policy or agent logic changes; the step evals' inputs are unchanged |
| XX. Deterministic acceptance | Pass | The point of the feature: a result is accepted only by the checks of the method that runs now, and records it |
| Values that check themselves | Pass | `method.ID` parses once and rejects anything but 64 lowercase hex digits |
| Checks prove they can fail | Pass | The script's tests plant a changed file, a dead glob, an unknown pin and key; `--check` fails on a stale file |
| Existing tests change only with approval | Pass | None expected (constraints); if one must change, the owner is asked before its brief |
| Fresh review | Pass | A fresh read-only reviewer per story and before done, judging the CI run |
| Layout | Pass | New files in `src/engine/method/` and `scripts/` |
| Clean tree | Exception | The derived `method.sha256` is committed (owner, 2026-11-22; Exceptions) |
| Arch lint | Pass | `src/engine/.go-arch-lint.yml` gains the `method` component (no dependencies) and `method` in `mayDependOn` of every component that takes a `method.ID` (`cmd-engine`, and `machine`, `durable`, `slices` where their configuration takes the type), in T001 |

## Decisions

Each decision that outlives the feature has a record in `docs/decisions/`.

| Decision | Research | Record |
|----------|----------|--------|
| The method identity is the SHA-256 of a manifest of the method's declared source files and pins, computed by `scripts/method-digest.py` and embedded in the engine | R1, R2 | [ADR 0079](../../docs/decisions/0079-method-identity-is-a-digest-of-declared-sources.md) |
| The reuse key covers the method identity, every step outcome event names it, a durable run takes it from the engine at each lookup, and a slice's checks run again under a new identity | R3, R4 | [ADR 0080](../../docs/decisions/0080-results-reused-and-checks-kept-only-under-their-method.md), superseding the key clause of [ADR 0059](../../docs/decisions/0059-run-store-is-json-lines-and-a-content-addressed-store.md) |

## Threat model

No external interface, tool, model, memory or access to protected content is added; the slice and
run-event views gain one field each. One element changes:

| Element | S | T | R | I | D | E | OWASP mapping | Mitigation |
|---------|---|---|---|---|---|---|---------------|------------|
| The method identity (build input of the engine) | | ✓ | ✓ | | | | ASI06 memory and context poisoning (a stale accepted result treated as current) | Derived only by the script, never by hand; `--check` in the test command fails a commit whose identity is stale, so it is not merged (FR-010); an identity that does not parse stops the engine; a missing one is no match (fail closed) |

## Project Structure

### Documentation (this feature)

```text
specs/006-method-record/
├── plan.md              # this file
├── research.md          # Phase 0
├── data-model.md        # Phase 1
├── quickstart.md        # Phase 1
├── contracts/
│   ├── method-declaration.md      # draft of the declaration, the identity and the script
│   └── engine-api-additions.md    # draft of the two schema properties
├── checklists/
│   └── requirements.md
└── tasks.md             # /speckit-tasks
```

### Source Code (repository root)

```text
src/engine/method/
├── README.md            # new: the contract (declaration rules, identity, command, Go side)
├── method.json          # new: the declaration
├── method.go            # new: //go:embed method.sha256; Current, Parse, ID
└── method.sha256        # derived by the script, committed (Exceptions)
src/engine/.go-arch-lint.yml              # the `method` component
scripts/method-digest.py                  # new (research R2)
scripts/tests/test_method_digest.py       # new
scripts/tests/test_check_method_step.py   # new: check.sh's Go step fails on a stale identity
scripts/check.sh                          # the Go step runs `method-digest.py --check` first
src/engine/api/preview/2026-11-12-preview/run-event.schema.json   # `method`
src/engine/api/preview/2026-11-12-preview/slice.schema.json       # `checks_method`
src/engine/machine/reuse.go               # KeyParts.Method
src/engine/machine/machine.go, drive.go   # Event.Method; Config.Method
src/engine/durable/workflow.go, activities.go, durable.go   # the key from LookupReuse; Activities.Method
src/engine/slices/persist.go, service.go  # checks_method; re-check in find and List
src/engine/api/slices.go                  # checks_method in the slice view
src/engine/cmd/engine/setup.go            # method.Current() into the machine, activities and slices
```

**Structure Decision**: the identity lives with the engine, its only consumer; the declaration
sits beside the package that embeds it, inside the engine module, so it is part of what it
declares.

**Existing tests expected to change**: none. Keys and events without a method are unchanged, the
harness's fake lookup returns no key so the workflow keeps its own (research R3), slice services
built without a method see every stored record as current (R4), and `build.sh` and
`check.sh --prepare` do not change (R2).

## Exceptions

| Rule | Location | Reason |
|------|----------|--------|
| Clean tree ("regenerable code ... MUST NOT be committed") | `src/engine/method/method.sha256` | A 65-byte derived value, not code, committed so that every Go build, vet and lint path has it without a new step, and checked by `scripts/method-digest.py --check` in the test command (research R2; owner, 2026-11-22, analyze round 1) |

## Complexity Tracking

No violation.
