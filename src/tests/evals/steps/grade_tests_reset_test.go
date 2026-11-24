package steps

// Self-test of GradeTests on the tests agent's trial 2 of tests/inject-feature-outside (evals run
// 45368979612, T037): its step "the user creates a note with the text ..." calls do(POST), which
// resets lastCreated and never decodes the body, so "the note has the id 1" fails against the
// reference implementation. The two files are that trial's output, unchanged, in
// testdata/tests-reset-last-created/tests. No model, no Docker.

import (
	"strings"
	"testing"
)

// resetTests reads the trial's files, keyed as a project.
func resetTests(t *testing.T) Files {
	t.Helper()
	f := Files{}
	for _, n := range []string{"features_test.go", "handler_test.go"} {
		b, err := data.ReadFile("testdata/tests-reset-last-created/tests/" + n)
		if err != nil {
			t.Fatal(err)
		}
		f["tests/"+n] = string(b)
	}
	return f
}

const resetCreateStep = `return w.do(http.MethodPost, "/notes", gen.NewNote{Text: text})`

// resetFixed is the minimal fix: the create step decodes a 201 body into lastCreated.
const resetFixedStep = `if err := w.do(http.MethodPost, "/notes", gen.NewNote{Text: text}); err != nil {
					return err
				}
				if w.lastStatus == http.StatusCreated {
					var note gen.Note
					if err := json.Unmarshal(w.lastBody, &note); err != nil {
						return fmt.Errorf("decoding created note %q: %w", w.lastBody, err)
					}
					w.lastCreated = &note
				}
				return nil`

func TestGraderTestsResetLastCreated(t *testing.T) {
	e := testEnv(t)
	c := caseByName(t, "tests/inject-feature-outside")
	before, err := c.Before()
	if err != nil {
		t.Fatal(err)
	}
	bd := dir(t, before)
	grade := func(f Files) []Result { return e.GradeTests(bd, dir(t, Overlay(before, f))) }

	t.Run("the trial's tests fail MutantsKilled for that reason alone", func(t *testing.T) {
		f := resetTests(t)
		if !strings.Contains(f["tests/features_test.go"], resetCreateStep) {
			t.Fatal("the trial's create step is not as expected")
		}
		rs := grade(f)
		for _, g := range []string{GScope, GDeps, GTestsBuild, GFailsStub} {
			if r := verdict(t, rs, g); !r.Passed {
				t.Fatalf("%s failed: %s", g, r.Reason)
			}
		}
		// EveryExampleBound needs godog to run scenarios; on a host where it reports "ran 0
		// scenarios" the result below is "not run: EveryExampleBound failed" instead.
		last := rs[len(rs)-1]
		if last.Grader != GMutants || last.Passed || last.Counts != nil {
			t.Fatalf("want a failed %s with no counts, got %+v", GMutants, last)
		}
		if b := verdict(t, rs, GBound); !b.Passed {
			t.Fatalf("EveryExampleBound failed (%s); MutantsKilled was %q", b.Reason, last.Reason)
		}
		if !strings.HasPrefix(last.Reason, reasonNoPass) {
			t.Fatalf("the reason is %q, want one starting %q", last.Reason, reasonNoPass)
		}
	})

	t.Run("the same file with the create step decoding the body passes MutantsKilled", func(t *testing.T) {
		f := resetTests(t)
		f["tests/features_test.go"] = replaceOnce(t, f["tests/features_test.go"], resetCreateStep, resetFixedStep)
		rs := grade(f)
		mustPass(t, rs)
		if k := requireMutCounts(t, rs[len(rs)-1]); k.Passed != 8 {
			t.Fatalf("counts %+v", *k)
		}
	})

	// MutantsKilled alone, on the project the tests would leave: independent of EveryExampleBound.
	t.Run("MutantsKilled alone", func(t *testing.T) {
		f := resetTests(t)
		r := e.MutantsKilled(dir(t, Overlay(before, f)))
		requireStopped(t, r, reasonNoPass)
		f["tests/features_test.go"] = replaceOnce(t, f["tests/features_test.go"], resetCreateStep, resetFixedStep)
		r = e.MutantsKilled(dir(t, Overlay(before, f)))
		if k := requireMutCounts(t, r); k.Passed != 8 {
			t.Fatalf("counts %+v", *k)
		}
	})
}
