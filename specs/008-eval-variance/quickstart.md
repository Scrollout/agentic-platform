# Quickstart: Eval suites show how much their trials vary

Validation scenarios for the [spec](spec.md). Q1 to Q4 are free and run locally or in the check
run's `scripts` job; Q5 is a paid eval dispatch and needs the owner's go-ahead.

## Prerequisites

- The repository at this feature's branch, Python 3 from `src/deploy/images.lock` (`ci-python`).
- For Q5: the owner has raised the organization's and the repository's artifact and log
  retention to 90 days (research R1), and the branch is in environment `dev`'s branch policies.

## Q1. Trials section from a planted report (US1, US2; SC-001, SC-002)

```sh
python3 scripts/ci-evals-measure.py scripts/tests/fixtures/<planted report>.json /tmp/m.json
python3 scripts/ci-evals-trials.py /tmp/m.json
```

Expected: every case shows `passed/run`; only the case whose trials disagree is `unstable`; the
lowest, median and highest seconds, cost and attempts match the fixture; a trial without usage
shows the count covered.

## Q2. The measurement holds nothing else (FR-004; SC-005)

Run `ci-evals-measure.py` on a report whose `stderr`, `results[].reason` and `changed_files` hold
a credential-like string and answer text. Expected: the measurement's keys are exactly
data-model.md's; `scripts/secret-scan.sh` on its folder finds nothing.

## Q3. Baseline choice (US3; FR-006)

Run `ci-evals-earlier.py` with `ARTIFACTS` listing measurements of an ancestor commit, an
unmerged branch's commit, this run, and an expired one, and a stub `gh` on `PATH`. Expected: the
ancestor's is chosen for its key; a key with none gets `{"none": ...}`; a failing download gives
`{"none": "could not be read ..."}` and exit 0.

## Q4. Workflow rules and the unchanged summary

```sh
python3 -m unittest discover -s scripts/tests -p 'test_workflows*.py'
python3 -m unittest discover -s scripts/tests -p 'test_ci_evals_summary*.py'
```

Expected: both pass; `ci-evals-summary.py`'s output is unchanged (FR-009).

## Q5. Two live runs (US3; SC-003, SC-004) — paid, owner's go-ahead

Dispatch `evals.yml` on the branch twice with `-f all=true` (this feature changes no path a suite
reads, so a plain dispatch selects no suite), the second on the same or a later commit. Expected: each job uploads `eval-measurements-…` kept 90 days; the second job's summary
compares with the first by run id and commit; the measurement steps add under 1 minute; the
config job's token downloaded the earlier artifact (research R3's limit).
