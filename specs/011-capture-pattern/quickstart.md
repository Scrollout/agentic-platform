# Quickstart: A proven slice becomes a pattern that builds without a model

How to see the feature work. Heavy suites run in CI (owner's rule); local runs are the fast
package tests.

## 1. Substitution and fit alone (seconds, no Docker)

`cd src/engine && go test ./captured/...`: the proposal finds `note` and `notes` with every place
in the evals' notes project (research E1); a slice for `task`/`tasks` fits with those values; a
slice with another rule, example, member or operation does not fit and names the first place it
differs; equal parameters and values outside their form are refused.

## 2. The engine API (seconds to minutes, no model)

`cd src/engine && go test ./api/... -run Captured`: capture refuses a run that did not end done;
parameters can be dropped and added; approval needs a passing proof under the current method;
a method change makes the pattern unusable; withdrawal stops its use; `startRun` picks the newest
usable fitting captured pattern unless `pattern` names another.

## 3. End to end on the Compose stack (CI, recorded model)

The acceptance suite for this feature builds the notes slice from the recorded model, captures
its run, proves and approves it, then builds a `tasks` slice that otherwise equals it: the run
ends done, the model gateway sees no request (SC-001), and the derived tests pass. Three runs
give the same files (SC-004).

## 4. Evals (paid, owner's go-ahead)

No agent changes; the method identity changes, so the analyst and steps suites run once before
merge, as after any engine change.
