# Quickstart: validating the feature end to end

This guide proves the feature works. Nothing runs on the host except the container runtime and the
launcher `src/deploy/ap`, which only calls `docker compose` ([terminal contract](contracts/cli.md)).

## Prerequisites

- A container runtime with Compose, and the gVisor runtime installed once with `runsc install`
  (research D13).
- A model credentials file outside the repository, readable only by you, for example
  `~/.config/agentic-platform/models.env`, containing `ANTHROPIC_API_KEY=...`. Only the gateway
  container reads it.
- An empty folder for new projects, for example `~/projects`.

## 1. Start the platform

```sh
src/deploy/ap up --credentials ~/.config/agentic-platform/models.env --projects ~/projects
src/deploy/ap page
```

Expected: the gateway, engine, terminal client and run page start; `page` prints a
`http://127.0.0.1:<port>` address. Only the gateway has internet access.

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

- `src/deploy/ap edit S-1 examples` to delete a rule's only scenario: `show S-1` reports the rule
  and the reason, the rules part loses its approval, and `build S-1` is refused with code
  `NotApproved`.
- Describe a command-line tool in Python: the slice fails the `PatternFit` check and cannot be built
  (scenario 6).

## 3. Build the slice (Story 2)

```sh
src/deploy/ap build S-1
```

Expected: the run ends `done` in under 30 minutes; the new project under `~/projects` contains
generated code, tests derived from the examples (seen failing before implementation) and code that
passes them, with its own version history. `src/deploy/ap run RUN-1` and the run page show every
event in order, each naming the goal, rule or example it concerns.

## 4. Replay, compare and resume

```sh
src/deploy/ap replay RUN-1
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
and a non-converging review is handed over after 3 rounds.
