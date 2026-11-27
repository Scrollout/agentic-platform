# Quickstart: A developer sees how much the builds of a slice vary

How to see the feature work. Heavy suites run in CI (owner's rule); local runs are the fast
package tests.

## 1. The result alone (seconds, no Docker)

`cd src/engine && go test ./trials/...`: on planted run records, three trials ending done, done and
handed over give `done 2/3` and `unstable`; 3/3 and 0/3 are not unstable (SC-002); a step whose
output hashes differ in one trial is marked as differing and names it, one with equal hashes is
`same` (SC-003); a fixed procedure's tokens are not available; the median of an even count is the
mean of the two middle values; a later set with fewer done trials is `less reliable`, and sets
under two methods say so.

## 2. The engine API (seconds to minutes, no model)

`cd src/engine && go test ./api/... -run TrialSet`: k outside 1 to 5 is refused; a missing
project's workers or an existing project refuse the set before any trial; each trial starts only
after the one before ended, with `NoReuse` and `trial_set` on its `RunStarted`; a trial waiting
for the developer is handed over by the set and the next starts; cancelling the set cancels its
trial and starts no other; the next trial is not started when it could pass $2; a set's job
repeated after a stop follows the trial it recorded instead of starting another; erasing the slice
erases its sets. Registrations by project: two projects' workers of one slice stand side by side,
and each run calls only its own.

## 3. End to end on the Compose stack (CI, recorded model)

The acceptance suite for this feature runs `ap trials <slice> <prefix> --trials 2` on the notes
slice with the recorded model: both projects get their step workers, two runs end done one after
the other, each makes model requests for every agent step (SC-004), `ap trial-set` prints `done
2/2`, no unstable mark and `same` files for every step, and reading it adds no model request
(SC-005). It also records the memory of the step containers of two projects (research R3).

## 4. Evals (paid, owner's go-ahead)

No agent changes; the method identity changes, so the analyst and steps suites run once before
merge, as after any engine change.
