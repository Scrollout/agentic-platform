# Quickstart: validating the feature end to end

This guide proves the feature works. Nothing runs on the host except the container runtime and the
launcher `src/deploy/ap`, which only calls `docker compose` ([terminal contract](../../src/cli/contract.md)).

## Prerequisites

- A container runtime with Compose, and the gVisor runtime installed once (research D13;
  [install guide](../../docs/guides/install-gvisor.md)).
- A model credentials file outside the repository that holds only the provider key (no variable
  name), owned by you and readable only by you (`chmod 600`), for example
  `~/.config/agentic-platform/anthropic_api_key`. Only the gateway container reads it.
- The images built and signed with `src/deploy/build.sh <name>` (`engine`, `page`, `cli`, `agent`,
  `check`), after `scripts/generate.sh`. The first build makes an encrypted signing key in
  `~/.config/agentic-platform/signing/`; set `COSIGN_PASSWORD` or type the password when cosign asks.
  `up` verifies each image's signature and starts nothing if one fails
  ([ADR 0028](../../docs/decisions/0028-sign-platform-images-locally.md)).
- An empty folder for new projects, for example `~/projects`.

## 1. Start the platform

```sh
src/deploy/ap up --credentials ~/.config/agentic-platform/anthropic_api_key --projects ~/projects
src/deploy/ap page
```

Expected: the gateway, engine and run page start (the terminal client runs per command; until the
run page exists, T056, `ap up` says so and starts the rest); `page`
prints a `http://127.0.0.1:<port>` address (port 7878 unless `AP_PAGE_PORT` is set). Only the
gateway has internet access. `src/deploy/ap down` stops everything and keeps run records and
projects.

## 2. Describe and approve a slice (Story 1)

```sh
src/deploy/ap describe
```

Type: *"An in-memory notes service. Listing returns all notes in creation order. Creating a note
needs non-empty text and returns it with an id starting at 1."*

Expected:

- Slice `S-1` with goals, terms, structured rules (each shown as an EARS sentence), Gherkin
  examples under `Rule:` blocks and an OpenAPI contract, all passing their checks
  ([data model](data-model.md)).
- At most 10 questions, each naming the part it concerns, answered with `src/deploy/ap questions S-1`.
- `src/deploy/ap approve S-1 goals`, `... rules` and `... contract` each succeed.

Negative checks (Story 1, scenarios 2 and 4):

- `src/deploy/ap edit S-1 rules` to delete a rule's only scenario: `show S-1` reports the rule
  and the reason, the rules part loses its approval, and `build S-1` is refused with code
  `NotApproved`.
- Describe a command-line tool in Python: the slice fails the `PatternFit` check and cannot be built
  (scenario 6).

## 3. Build the slice (Story 2)

```sh
src/deploy/ap build S-1 notes
```

Expected: the run ends `done` in under 30 minutes; the new project `~/projects/notes` contains
generated code, tests derived from the examples (seen failing before implementation) and code that
passes them, with its own version history. `src/deploy/ap run RUN-1` and the run page show every
event in order, each naming the goal, rule or example it concerns.

## 4. Replay, compare and resume

```sh
src/deploy/ap replay RUN-1 notes-replay
src/deploy/ap compare RUN-1 RUN-2
```

Expected: the replay reuses every accepted result, and `compare` reports no differences in
artifacts. Stop the platform during a run, start it again and `resume` the run: no accepted step
runs again.

## 5. Planted failures

```sh
src/deploy/ap test planted
```

Expected: five scenarios, each stopped by a check in 3 out of 3 trials: test edits are impossible,
out-of-scope changes are rejected, a forbidden import is rejected, a false "done" is not accepted,
and a non-converging review stops after 3 rounds and waits for the developer. `ap test` needs no
started session: it builds the suite in the pinned `golang` container, offline, and runs its test
binary on the host with your Docker access (ADR 0037). Standard output has one line per trial and
per scenario, then `planted: 5 of 5 scenarios stopped in 3 out of 3 trials`, and the exit code is
0; a failing trial prints its messages indented below it and exits 1.
