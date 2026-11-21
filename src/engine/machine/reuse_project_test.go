package machine_test

// T053a interim rule (owner): a stored accepted result that carries project files is reused only
// within the project of the run that made it, since nothing yet writes reused files into another
// project. A result without files is reused across runs and projects as before.

import (
	"context"
	"slices"
	"testing"

	"github.com/agentic-platform/engine/machine"
)

// projectFixture: outputs depend only on the step and its attempt-free name, so a re-run gives the
// same bytes and every later key matches; the steps in withFiles report project files.
func projectFixture(t *testing.T, results *machine.Results, withFiles ...string) *fixture {
	f := newFixture(t, pattern())
	for _, s := range pattern().Steps {
		name := s.Name
		f.w.fn[name] = func(r machine.Request) (machine.Result, error) {
			res := ok(r)
			for o := range res.Outputs {
				res.Outputs[o] = hash(name + "/" + o)
			}
			if slices.Contains(withFiles, name) {
				res.ProjectFiles = []machine.File{{Path: name + "/f.go", SHA256: hash("file/" + name)}}
			}
			return res, nil
		}
	}
	f.c.fn = noBlocking
	f.m = machine.New(machine.Config{Pattern: pattern(), Worker: f.w, Checker: f.c, Clock: f.clk,
		Events: f.ev, Results: results, Model: "claude-haiku-5-5"})
	return f
}

func (f *fixture) startIn(id, project string) machine.Run {
	f.t.Helper()
	p := f.params(id)
	p.Project = project
	r, err := f.m.Start(context.Background(), p)
	if err != nil {
		f.t.Fatalf("Start(%s): %v", id, err)
	}
	return r
}

func stepsCalled(f *fixture) []string {
	var out []string
	for _, c := range f.w.calls {
		if !slices.Contains(out, c.Step.Name) {
			out = append(out, c.Step.Name)
		}
	}
	return out
}

func TestResultWithoutFilesIsReusedAcrossProjects(t *testing.T) {
	results := machine.NewResults()
	first := projectFixture(t, results)
	assertEnded(t, first.startIn("RUN-1", "proj-a"), first.ev, "done")

	second := projectFixture(t, results)
	assertEnded(t, second.startIn("RUN-2", "proj-b"), second.ev, "done")
	if len(second.w.calls) != 0 || second.ev.count("ResultReused") != 5 {
		t.Fatalf("worker calls = %d, ResultReused = %d; want 0 and 5", len(second.w.calls), second.ev.count("ResultReused"))
	}
}

func TestResultWithFilesIsReusedWithinTheSameProject(t *testing.T) {
	results := machine.NewResults()
	first := projectFixture(t, results, "generate", "tests")
	assertEnded(t, first.startIn("RUN-1", "proj-a"), first.ev, "done")

	second := projectFixture(t, results, "generate", "tests")
	assertEnded(t, second.startIn("RUN-2", "proj-a"), second.ev, "done")
	if len(second.w.calls) != 0 || second.ev.count("ResultReused") != 5 {
		t.Fatalf("worker calls = %v, ResultReused = %d; want 0 and 5 in the same project", stepsCalled(second), second.ev.count("ResultReused"))
	}
}
