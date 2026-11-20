package cli_test

// T034: the end line of an interrupted run follows the run's waits_for, never its reason text.
// Written from src/cli/contract.md ("End lines") and the engine contract alone.

import (
	"strings"
	"testing"
)

const waitsExpires = "; it expires " + tExpires

// interruptedRun is RUN-7 interrupted with the given reason and waits_for ("" leaves the field out);
// edit changes the run as the engine sends it.
func interruptedRun(t *testing.T, reason, waitsFor string, edit func(m map[string]any)) (*fakeRun, endState) {
	t.Helper()
	s := endStates("RUN-7", "OP-12")[2]
	s.reason = reason
	fr := stateRun(t, "RUN-7", "OP-12", s, 0)
	if waitsFor != "" {
		fr.final["waits_for"] = waitsFor
	}
	if edit != nil {
		edit(fr.final)
	}
	return fr, s
}

func workersLine(reason, expires string) string {
	return "RUN-7 is interrupted" + reason + expires +
		". It continues by itself once its step workers run: start them with ap build-workers --resume RUN-7, or cancel it with cancel RUN-7."
}

func resumeLine(reason, expires string) string {
	return "RUN-7 is interrupted" + reason + expires + ". Resume it with resume RUN-7, or cancel it with cancel RUN-7."
}

func TestAnInterruptedRunsEndLineFollowsItsWaitsFor(t *testing.T) {
	const workersReason = "the step workers are not running"
	cases := []struct {
		name, reason, waitsFor string
		edit                   func(m map[string]any)
		want                   string
	}{
		{"step-workers", workersReason, "step-workers", nil, workersLine(": "+workersReason, waitsExpires)},
		{"resume", "the model was unavailable on every attempt", "resume", nil,
			resumeLine(": the model was unavailable on every attempt", waitsExpires)},
		{"absent", "the platform stopped", "", nil, resumeLine(": the platform stopped", waitsExpires)},
		// the client reads waits_for, not the wording
		{"workers wording but resume", workersReason, "resume", nil, resumeLine(": "+workersReason, waitsExpires)},
		{"workers wording but absent", workersReason, "", nil, resumeLine(": "+workersReason, waitsExpires)},
		{"step-workers with another reason", "the platform stopped", "step-workers", nil,
			workersLine(": the platform stopped", waitsExpires)},
		// edge cases
		{"step-workers without a reason", "", "step-workers", nil, workersLine("", waitsExpires)},
		{"resume without a reason", "", "resume", nil, resumeLine("", waitsExpires)},
		{"step-workers without expires_at", workersReason, "step-workers",
			func(m map[string]any) { delete(m, "expires_at") }, workersLine(": "+workersReason, "")},
		{"resume without expires_at", "the platform stopped", "resume",
			func(m map[string]any) { delete(m, "expires_at") }, resumeLine(": the platform stopped", "")},
		{"step-workers with a reason that needs escaping", "workers\nstopped \x1b[31m", "step-workers", nil,
			workersLine(`: workers\nstopped \x1b[31m`, waitsExpires)},
		{"resume with a reason that needs escaping", "stopped\tby\x00 a bell\a", "resume", nil,
			resumeLine(`: stopped\tby\x00 a bell\a`, waitsExpires)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Run("run", func(t *testing.T) {
				e := newEng(t)
				fr, s := interruptedRun(t, c.reason, c.waitsFor, c.edit)
				fr.register(e)
				r := e.run("run", "RUN-7")
				wantExit(t, r, 2)
				wantOut(t, r, lines(append(append([]string{runHeader}, stateLines("RUN-7", s)...), c.want)...))
			})
			t.Run("build", func(t *testing.T) {
				e := newEng(t)
				buildRoutes(e)
				fr, s := interruptedRun(t, c.reason, c.waitsFor, c.edit)
				fr.initial = 2
				fr.register(e)
				r := e.run("build", "S-1", "notes")
				wantExit(t, r, 2)
				wantOut(t, r, lines(append(append([]string{startedLine}, stateLines("RUN-7", s)...), c.want)...))
			})
			t.Run("resume", func(t *testing.T) {
				e := newEng(t)
				e.reply("POST /runs/RUN-7/resume", 202, operation("OP-12", "run", "running", "RUN-7", nil))
				fr, s := interruptedRun(t, c.reason, c.waitsFor, c.edit)
				fr.initial = 2
				fr.register(e)
				r := e.run("resume", "RUN-7")
				wantExit(t, r, 2)
				wantOut(t, r, lines(append(append([]string{"RUN-7 resumed (operation OP-12)"}, stateLines("RUN-7", s)...), c.want)...))
			})
		})
	}
}

// waits_for belongs to an interrupted run only: other states print their own lines whatever the field says.
func TestWaitsForDoesNotChangeTheEndLineOfAnotherState(t *testing.T) {
	for _, s := range endStates("RUN-7", "OP-12") {
		if s.status == "interrupted" {
			continue
		}
		t.Run(s.name, func(t *testing.T) {
			e := newEng(t)
			fr := stateRun(t, "RUN-7", "OP-12", s, 0)
			fr.final["waits_for"] = "step-workers"
			fr.register(e)
			r := e.run("run", "RUN-7")
			wantExit(t, r, s.exit)
			if got := strings.TrimRight(r.out, "\n"); !strings.HasSuffix(got, s.endLine) {
				t.Errorf("stdout = %q, want it to end with %q", r.out, s.endLine)
			}
		})
	}
}
