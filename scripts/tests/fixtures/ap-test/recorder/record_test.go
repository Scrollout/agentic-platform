// The stand-in suite whose test2json stream scripts/tests/test_ap_test.py replays as the
// acceptance suite's: the T046 test names (TestPlanted/<scenario>/trial-<n>, the TestUS2 tests)
// with no labs. RECORD_FAIL names one trial that fails, for example outside-scope/trial-2, or is
// us2 for a failing subtest of TestUS2 and a failing TestUS2_2_EditingTheJudgingTestsIsStopped.
// Regenerate the recordings with the command in test_ap_test.py's docstring.
package recorder

import (
	"os"
	"testing"
)

var scenarios = []string{"edit-judging-tests", "outside-scope", "forbidden-dependency", "claims-done", "non-converging-review"}

func TestPlanted(t *testing.T) {
	for _, sc := range scenarios {
		t.Run(sc, func(t *testing.T) {
			for _, trial := range []string{"trial-1", "trial-2", "trial-3"} {
				t.Run(trial, func(t *testing.T) {
					if os.Getenv("RECORD_FAIL") == sc+"/"+trial {
						t.Errorf("%s: the run is %q (outcome %q), want waiting-for-developer with no outcome yet", sc, "ended", "accepted")
						t.Errorf("%s: step code was accepted 1 time(s) with the failure planted", sc)
					}
				})
			}
		})
	}
}

func TestUS2(t *testing.T) {
	t.Run("US2.1", func(t *testing.T) {
		if os.Getenv("RECORD_FAIL") == "us2" {
			t.Errorf("US2.1: the run page shows no accepted step")
		}
	})
}

func TestUS2_2_EditingTheJudgingTestsIsStopped(t *testing.T) {
	if os.Getenv("RECORD_FAIL") == "us2" {
		t.Errorf("edit-judging-tests: step code was accepted 1 time(s) with the failure planted")
	}
}

func TestUS2_10_AnInterruptedRunResumesWithoutRepeatingAcceptedSteps(t *testing.T) {}
