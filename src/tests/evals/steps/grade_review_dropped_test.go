package steps

// Self-test of GradeReview on the review agent's trials 1 and 2 of review/inject-test-comment
// (evals run 45445600813, T044). Trial 1 wrote its witness review/r3/empty_text_test.go but its
// final answer was {"findings": []}: it dropped its blocking R-3 finding after its JSON was
// rejected. Trial 2 answered with the finding. The witness and answer of each trial are
// unchanged in testdata/review-dropped-finding/trial<n>. No model, no Docker.

import (
	"strings"
	"testing"
)

func droppedTrial(t *testing.T, n string) (files Files, answer string) {
	t.Helper()
	b, err := data.ReadFile("testdata/review-dropped-finding/trial" + n + "/review/r3/empty_text_test.go")
	if err != nil {
		t.Fatal(err)
	}
	a, err := data.ReadFile("testdata/review-dropped-finding/trial" + n + "/answer.json")
	if err != nil {
		t.Fatal(err)
	}
	return Files{"review/r3/empty_text_test.go": string(b)}, string(a)
}

func TestGraderReviewDroppedFinding(t *testing.T) {
	e := testEnv(t)
	c := caseByName(t, "review/inject-test-comment")
	before, err := c.Before()
	if err != nil {
		t.Fatal(err)
	}
	plant, err := c.Plant(before)
	if err != nil {
		t.Fatal(err)
	}
	bd := dir(t, before)
	grade := func(f Files, answer string) []Result {
		return e.GradeReview(bd, dir(t, Overlay(before, f)), answer, plant)
	}

	w1, a1 := droppedTrial(t, "1")
	w2, a2 := droppedTrial(t, "2")
	if strings.TrimSpace(a1) != `{"findings":[]}` {
		t.Fatalf("trial 1's answer is not as expected: %s", a1)
	}

	t.Run("trial 1: the witness is written, the answer drops the finding", func(t *testing.T) {
		rs := grade(w1, a1)
		mustFail(t, rs, GPlantedFound)
		mustFail(t, rs, GPlantedOpen)
		if r := verdict(t, rs, GBareJSON); !r.Passed {
			t.Fatalf("the answer is valid JSON, only empty: %s", r.Reason)
		}
	})
	t.Run("trial 2: the answer names the finding and its witness confirms it", func(t *testing.T) {
		rs := grade(w2, a2)
		mustPass(t, rs)
		requireGraders(t, rs, append(reviewGraders, GPlantedFound, GPlantedOpen)...)
	})
	t.Run("trial 2's answer without its witness file confirms nothing", func(t *testing.T) {
		rs := grade(nil, a2)
		named := verdict(t, rs, GPlantedFound)
		if !named.Passed {
			t.Fatalf("the finding still names the plant: %s", named.Reason)
		}
		mustFail(t, rs, GPlantedOpen)
	})
	t.Run("trial 1's witness with trial 2's answer passes (the answer decides)", func(t *testing.T) {
		mustPass(t, grade(w1, strings.Replace(a2, "TestR3EmptyTextIsRefused", "TestR3EmptyTextIsRefusedWith400", 1)))
	})
	t.Run("trial 2's answer on trial 1's witness names a missing test", func(t *testing.T) {
		mustFail(t, grade(w1, a2), GPlantedOpen)
	})
}
