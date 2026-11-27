// T031 (feature 012, US1, review B1, ADR 0106): the client's `trial-set` and `trials` for a set that
// ends `failed`, written against src/cli/contract.md ("Trial sets": the first line shows
// `state failed  failed <reason>`; the exit table's row `failed` -> 4 "read from the set: its
// `failed_because`") and the engine contract's TrialSet (`failed_because`, `ended_at` with it), before
// the client handles the state (T032). It uses trials_test.go's stand-in helpers as they are (tsOf,
// tsSplit, tsWantTables, called, not edited); only the failed answer is built here.
//
// Edge cases checked: a failed set read with `trial-set` (a trial done and one cancelled; no trial
// started; every ended trial done, which must not exit 0 or 7; the only trial cancelled); the reason
// read from the set when the set's operation gives another one; a reason with control characters
// (printed escaped, on one line); `trials` and `trial-set` following a set that turns failed while
// its operation also reads failed (the set decides: the result prints and the exit is 4 with the
// reason, not the operation's problem and no missing result); a failed set is never compared with a
// previous one and the client never cancels anything; the exit is 4, never 1 (the unknown-state
// exit) or 7 (the differ exit).
package cli_test

import (
	"net/http"
	"strings"
	"sync"
	"testing"
)

// tsFailedSet is a failed set (tsOf's; the reason is given to tsFailScript).
func tsFailedSet(outcomes ...string) tsSet { return tsOf("failed", "", 3, outcomes...) }

// tsFailScript answers getTrialSet with the given sets in turn (the last repeats), a failed one with
// `failed_because` = reason, and getOperation with `failed` and opError once the set read is failed,
// as the set's state otherwise.
func tsFailScript(t *testing.T, e *eng, reason string, opError map[string]any, stages ...tsSet) {
	t.Helper()
	var mu sync.Mutex
	reads := 0
	at := func() tsSet {
		mu.Lock()
		defer mu.Unlock()
		return stages[min(max(reads-1, 0), len(stages)-1)]
	}
	e.on("GET /trial-sets/"+tsID, func(w http.ResponseWriter, _ req) {
		mu.Lock()
		reads++
		mu.Unlock()
		m := at().json()
		if m["state"] == "failed" {
			m["failed_because"] = reason
		}
		writeJSON(w, 200, m)
	})
	e.on("GET /operations/"+tsOp, func(w http.ResponseWriter, _ req) {
		st := at()
		status := tsOpFor(st.state)
		if st.state == "failed" {
			status = "failed"
		}
		op := operation(tsOp, "run-trials", status, tsID, nil)
		if status == "failed" && opError != nil {
			op["error"] = opError
		}
		writeJSON(w, 200, op)
	})
}

func TestTrialSetOfAFailedSetPrintsStateFailedAndTheReasonAndExits4(t *testing.T) {
	cases := []struct {
		name     string
		reason   string
		outcomes []string
		first    string
	}{
		{"a done trial and a cancelled one", "failed after 4 attempts: durable service unreachable", []string{"done", "cancelled"},
			"TS-1  slice S-1  state failed  failed failed after 4 attempts: durable service unreachable  done 1/2  unstable"},
		{"no trial started", "trial 1 could not start: project notes-1 is not empty", nil,
			"TS-1  slice S-1  state failed  failed trial 1 could not start: project notes-1 is not empty  done 0/0"},
		{"every ended trial is done", "trial 3 could not start: project notes-3 is not empty", []string{"done", "done"},
			"TS-1  slice S-1  state failed  failed trial 3 could not start: project notes-3 is not empty  done 2/2"},
		{"the only trial cancelled", "failed after 4 attempts: x", []string{"cancelled"},
			"TS-1  slice S-1  state failed  failed failed after 4 attempts: x  done 0/1"},
		{"control characters in the reason are escaped", "disk\nfull \x1b[31mred", []string{"done"},
			`TS-1  slice S-1  state failed  failed disk\nfull \x1b[31mred  done 1/1`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEng(t)
			set := tsFailedSet(c.outcomes...)
			tsFailScript(t, e, c.reason, nil, set)
			r := e.run("trial-set", tsID)
			wantExit(t, r, 4)
			before, first, after := tsSplit(t, r.out)
			if first != c.first {
				t.Errorf("first line of the result = %q, want %q", first, c.first)
			}
			tsWantTrialLines(t, before, tsTrialLines(set), true)
			tsWantTables(t, after, set)
			wantAbsent(t, "stdout", r.out, "compared with", "\x1b")
			if e.anyPathWith("/cancel") {
				t.Error("reading a failed set must not cancel anything")
			}
		})
	}
}

func TestTrialSetReadsTheReasonFromTheSetNotFromItsOperation(t *testing.T) {
	e := newEng(t)
	tsFailScript(t, e, "the set's own reason", prob("OperationFailed", "the operation failed", "the operation's own detail"),
		tsFailedSet("done", "cancelled"))
	r := e.run("trial-set", tsID)
	wantExit(t, r, 4)
	_, first, _ := tsSplit(t, r.out)
	if want := "TS-1  slice S-1  state failed  failed the set's own reason  done 1/2  unstable"; first != want {
		t.Errorf("first line of the result = %q, want %q", first, want)
	}
}

func TestFollowingASetThatTurnsFailedPrintsTheResultAndExits4(t *testing.T) {
	reason := "failed after 4 attempts: the durable service is down"
	problem := prob("OperationFailed", "the operation failed", reason)
	for _, args := range [][]string{{"trials", "S-1", "notes"}, {"trial-set", tsID}} {
		t.Run(args[0], func(t *testing.T) {
			e := newEng(t)
			tsStart(e)
			// The set runs, a trial ends, then the set and its operation read failed together: the set decides.
			set := tsFailedSet("done", "cancelled")
			tsFailScript(t, e, reason, problem, tsOf("running", "", 3, ""), tsOf("running", "", 3, "done", ""), set)
			r := e.run(args...)
			wantExit(t, r, 4)
			before, first, after := tsSplit(t, r.out)
			if !strings.Contains(strings.Join(before, "\n"), "trial 1  RUN-21  done") {
				t.Errorf("lines before the result = %q, want the line of trial 1 (done)", before)
			}
			if want := "TS-1  slice S-1  state failed  failed " + reason + "  done 1/2  unstable"; first != want {
				t.Errorf("first line of the result = %q, want %q", first, want)
			}
			tsWantTables(t, after, set)
			if e.anyPathWith("/cancel") {
				t.Error("following a set must not cancel it")
			}
		})
	}
}
