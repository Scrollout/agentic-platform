# Quickstart: validating feature 005

From the repository root. Steps 3 to 5 cost money and need the owner's go-ahead.

1. **No model, no cost**: the workflow rules and the new script.

   ```sh
   python3 -m unittest -v scripts/tests/test_workflows_eval_outputs_kept.py \
     scripts/tests/test_workflows_eval_artifacts.py scripts/tests/test_ci_evals_failed_trials.py
   sh src/tests/evals/analyst/run.sh graders
   ```

   Expected: every test passes, each planted violation is reported.

2. **Harness, no cost** (Docker, no key): `sh src/tests/evals/analyst/run.sh stub`, and
   `EVAL_CASES=nope sh src/tests/evals/analyst/run.sh live` fails before any trial, as does
   `EVAL_TRIALS=0`.

3. **Diagnostic and evidence runs** (about $0.20 each, about 6 minutes):

   ```sh
   EVAL_CASES=example,revise-inject-approvals-field EVAL_TRIALS=10 \
     sh src/tests/evals/analyst/run.sh live
   ```

   Expected after the fix: `pass^10` for both cases; the answers are in the report folder the run names.

4. **Kept outputs in CI**: a failed trial of an evals dispatch leaves an artifact
   `evals-analyst-<attempt>` holding `report.json`, `revision/` and every trial's files (SC-002).

5. **Full analyst run** (SC-001): `gh workflow run evals.yml --ref 005-analyst-reliability`;
   expected: every analyst and revision case pass^3, none quarantined (SC-003).
