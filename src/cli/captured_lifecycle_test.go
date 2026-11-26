// T019 (feature 011, US3): the terminal client's `withdraw-pattern` and `erase-pattern`, written
// against src/cli/contract.md (the command rows, "Captured patterns (feature 011)", "Problems",
// "Run states and exit codes") and the engine contract's operations withdrawCapturedPattern and
// eraseCapturedPattern, before the commands exist. Every command runs in process through cli.Run
// against the stand-in engine of runs_fake_test.go and the fixtures of captured_test.go.
//
// Where the contract leaves an exact text open, the tests check the facts, not the layout: what
// `erase-pattern` shows (the IDs of the captured pattern, its proof slice and that slice's runs) and
// the line each command prints on success. Every name here starts with cl so no other file collides.
package cli_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	cli "github.com/agentic-platform/cli"
)

const (
	clWithdraw = "POST /captured-patterns/" + cpID + "/withdrawal"
	clErase    = "DELETE /captured-patterns/" + cpID
	clGet      = "GET /captured-patterns/" + cpID
)

// clRunIn runs one command with stdin and keeps stdout in out, where a handler can read it while
// the command is still running (see clErasable).
func clRunIn(e *eng, out *syncBuf, stdin io.Reader, args ...string) (int, string) {
	var errb syncBuf
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	code := cli.Run(ctx, args, e.env(), stdin, out, &errb)
	return code, errb.String()
}

// clProven is a captured pattern with a proof slice S-9 whose two proofs ran as RUN-7 and RUN-8.
func clProven() map[string]any {
	p := cpPattern("approved", true, cpTwoParams(), cpProofRec("RUN-7", "passed"), cpProofRec("RUN-8", "passed"))
	p["proof_slice"] = cpProof
	return p
}

// clErasable sets up the engine for erase-pattern: the captured pattern is read, and the erase is
// accepted as operation OP-30, which ends as status. atErase receives what stdout held when the
// DELETE arrived.
func clErasable(e *eng, out *syncBuf, atErase *string, pattern map[string]any, status string) {
	e.reply(clGet, 200, pattern)
	e.reply("GET /operations/OP-30", 200, operation("OP-30", "erase-captured-pattern", status, cpID, nil))
	e.on(clErase, func(w http.ResponseWriter, _ req) {
		*atErase = out.String()
		writeJSON(w, 202, operation("OP-30", "erase-captured-pattern", "accepted", cpID, nil))
	})
}

// ---- ap withdraw-pattern ----

// Covers T019: `withdraw-pattern` calls withdrawCapturedPattern, follows the operation to its
// outcome (FR-014), exits 0 and names the captured pattern. It reads nothing else.
func TestClWithdrawCallsAndFollowsTheOperation(t *testing.T) {
	e := newEng(t)
	n := 0
	e.reply(clWithdraw, 202, operation("OP-31", "withdraw-captured-pattern", "accepted", cpID, nil))
	e.on("GET /operations/OP-31", func(w http.ResponseWriter, _ req) {
		n++
		st := "running"
		if n >= 3 {
			st = "succeeded"
		}
		writeJSON(w, 200, operation("OP-31", "withdraw-captured-pattern", st, cpID, nil))
	})
	r := e.run("withdraw-pattern", cpID)
	wantExit(t, r, 0)
	wantContains(t, "stdout", r.out, cpID)
	if n < 3 {
		t.Errorf("operation reads = %d, want at least 3 (two running, then succeeded)", n)
	}
	if len(e.got("POST", "/captured-patterns/"+cpID+"/withdrawal")) != 1 {
		t.Error("withdrawCapturedPattern is called once")
	}
	if e.total() != 1+n {
		t.Errorf("engine calls = %d, want the withdrawal and the operation reads only", e.total())
	}
}

// Covers T019 (the contract's "withdrawing a withdrawn one changes nothing"): the engine answers
// the second withdrawal as it does the first, so the client exits 0 again.
func TestClWithdrawAgainExitsZeroAgain(t *testing.T) {
	e := newEng(t)
	cpOp(e, clWithdraw, "OP-31", "withdraw-captured-pattern", cpID, "succeeded", nil, nil)
	wantExit(t, e.run("withdraw-pattern", cpID), 0)
	wantExit(t, e.run("withdraw-pattern", cpID), 0)
	if got := len(e.got("POST", "/captured-patterns/"+cpID+"/withdrawal")); got != 2 {
		t.Errorf("withdrawal calls = %d, want 2", got)
	}
}

// Covers T019: a refusal (404 NotFound, 409 CapturedPatternBusy) is exit 4 with the contract's
// problem lines on standard error, nothing on standard output, and no operation followed.
func TestClWithdrawRefusalsExit4WithTheProblem(t *testing.T) {
	for _, c := range []struct {
		status       int
		code, detail string
	}{
		{404, "NotFound", "no captured pattern tdd-s-1"},
		{409, "CapturedPatternBusy", "another of its operations runs"},
	} {
		t.Run(c.code, func(t *testing.T) {
			e := newEng(t)
			e.refuse(clWithdraw, c.status, prob(c.code, c.code, c.detail))
			r := e.run("withdraw-pattern", cpID)
			wantExit(t, r, 4)
			if want := "refused: " + c.code + ": " + c.detail + "\n"; r.err != want {
				t.Errorf("stderr = %q, want %q", r.err, want)
			}
			if r.out != "" {
				t.Errorf("stdout = %q, want nothing for a refusal", r.out)
			}
			if e.total() != 1 {
				t.Errorf("engine calls = %d, want only the refused one", e.total())
			}
		})
	}
}

// Covers T019: a withdrawal whose operation fails is exit 4 with its problem lines; a cancelled
// one is exit 6, as for any operation.
func TestClWithdrawOperationFailedExit4AndCancelledExit6(t *testing.T) {
	e := newEng(t)
	cpOp(e, clWithdraw, "OP-31", "withdraw-captured-pattern", cpID, "failed", nil,
		prob("OperationFailed", "OperationFailed", "the store is unreadable"))
	r := e.run("withdraw-pattern", cpID)
	wantExit(t, r, 4)
	if want := "refused: OperationFailed: the store is unreadable\n"; r.err != want {
		t.Errorf("stderr = %q, want %q", r.err, want)
	}
	if r.out != "" {
		t.Errorf("stdout = %q, want nothing", r.out)
	}
	e2 := newEng(t)
	cpOp(e2, clWithdraw, "OP-31", "withdraw-captured-pattern", cpID, "cancelled", nil, nil)
	wantExit(t, e2.run("withdraw-pattern", cpID), 6)
}

// Covers T019 (usage): a missing, malformed (capital letter, space, empty) or extra argument is
// exit 3, a message on standard error, and the engine is not called.
func TestClWithdrawUsageErrorsExit3WithoutCalls(t *testing.T) {
	for _, args := range [][]string{
		{"withdraw-pattern"},
		{"withdraw-pattern", "Tdd"},
		{"withdraw-pattern", ""},
		{"withdraw-pattern", "tdd s"},
		{"withdraw-pattern", cpID, "extra"},
		{"withdraw-pattern", cpID, cpID},
		{"withdraw-pattern", cpID, "--yes"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			e := newEng(t)
			cpNoCalls(t, e, e.run(args...), 3)
		})
	}
}

// ---- ap erase-pattern ----

// Covers T019: `erase-pattern` reads the captured pattern first and shows, before it asks and
// before any DELETE, the captured pattern's ID, its proof slice and that slice's runs; the right
// ID then sends DELETE, follows the operation and exits 0. It asks the developer to type the ID.
func TestClEraseShowsWhatItErasesThenErasesAfterTheTypedID(t *testing.T) {
	e := newEng(t)
	var out syncBuf
	var atErase string
	clErasable(e, &out, &atErase, clProven(), "succeeded")
	code, errs := clRunIn(e, &out, strings.NewReader(cpID+"\n"), "erase-pattern", cpID)
	if code != 0 {
		t.Fatalf("exit = %d\nstdout: %s\nstderr: %s", code, out.String(), errs)
	}
	wantContains(t, "what was shown before the erase", atErase, cpID, cpProof, "RUN-7", "RUN-8")
	if !strings.Contains(strings.ToLower(atErase), "type") {
		t.Errorf("the question does not ask the developer to type the ID:\n%s", atErase)
	}
	if len(e.got("DELETE", "/captured-patterns/"+cpID)) != 1 {
		t.Errorf("eraseCapturedPattern calls = %d, want 1", len(e.got("DELETE", "/captured-patterns/"+cpID)))
	}
	if len(e.got("GET", "/operations/OP-30")) == 0 {
		t.Error("erase-pattern follows the operation")
	}
	if errs != "" {
		t.Errorf("stderr = %q, want nothing", errs)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	gi, di := -1, -1
	for i, c := range e.reqs {
		if c.Method == "GET" && c.Path == "/captured-patterns/"+cpID && gi < 0 {
			gi = i
		}
		if c.Method == "DELETE" {
			di = i
		}
	}
	if gi < 0 || di < 0 || gi > di {
		t.Errorf("order: read at %d, delete at %d; the read must come first", gi, di)
	}
}

// Covers T019 (boundary: no proof): a captured pattern with no proof slice and no proof run still
// shows itself and asks; what it shows names no proof slice and no run it does not have.
func TestClEraseOfAPatternWithNoProofShowsOnlyThePattern(t *testing.T) {
	e := newEng(t)
	var out syncBuf
	var atErase string
	clErasable(e, &out, &atErase, cpPattern("proposed", false, cpTwoParams()), "succeeded")
	code, errs := clRunIn(e, &out, strings.NewReader(cpID+"\n"), "erase-pattern", cpID)
	if code != 0 {
		t.Fatalf("exit = %d\nstdout: %s\nstderr: %s", code, out.String(), errs)
	}
	wantContains(t, "what was shown", atErase, cpID)
	wantAbsent(t, "what was shown", atErase, "S-9", "RUN-7", "RUN-8")
	if len(e.got("DELETE", "/captured-patterns/"+cpID)) != 1 {
		t.Error("the right ID erases")
	}
}

// Covers T019 (boundary: one proof): a captured pattern with one proof run shows that run and not
// another's.
func TestClEraseWithOneProofRunShowsOnlyThatRun(t *testing.T) {
	e := newEng(t)
	var out syncBuf
	var atErase string
	p := cpPattern("proposed", false, cpTwoParams(), cpProofRec("RUN-7", "failed"))
	p["proof_slice"] = cpProof
	clErasable(e, &out, &atErase, p, "succeeded")
	code, _ := clRunIn(e, &out, strings.NewReader(cpID+"\n"), "erase-pattern", cpID)
	if code != 0 {
		t.Fatalf("exit = %d\nstdout: %s", code, out.String())
	}
	wantContains(t, "what was shown", atErase, cpProof, "RUN-7")
	wantAbsent(t, "what was shown", atErase, "RUN-8")
}

// Covers T019: every answer but the exact ID (empty line, a wrong ID, the ID in another case or
// padded, yes, the proof slice's ID, a prefix, an extension, no input at all) erases nothing: no
// DELETE, a non-zero exit, and a message on standard error saying nothing was erased.
func TestClEraseErasesNothingOnAnyOtherAnswer(t *testing.T) {
	for _, answer := range []string{"\n", "tdd-s-2\n", "TDD-S-1\n", "Tdd-s-1\n", " tdd-s-1\n", "tdd-s-1 \n",
		"yes\n", "y\n", "S-9\n", "tdd-s-1x\n", "tdd-s-\n", "", "\n\ntdd-s-1\n"} {
		t.Run(fmt.Sprintf("%q", answer), func(t *testing.T) {
			e := newEng(t)
			var out syncBuf
			var atErase string
			clErasable(e, &out, &atErase, clProven(), "succeeded")
			code, errs := clRunIn(e, &out, strings.NewReader(answer), "erase-pattern", cpID)
			if code == 0 {
				t.Error("a refused confirmation must not exit 0")
			}
			if n := len(e.got("DELETE", "/captured-patterns/"+cpID)); n != 0 {
				t.Errorf("DELETE calls = %d, want 0: nothing is erased without the typed ID", n)
			}
			if len(e.got("GET", "/operations/OP-30")) != 0 {
				t.Error("no operation exists to follow")
			}
			if !strings.Contains(strings.ToLower(errs), "nothing") {
				t.Errorf("stderr = %q, want it to say nothing was erased", errs)
			}
		})
	}
}

// Covers T019: the contract names no `--yes` flag for erase-pattern (only `delete <slice>` has
// one), so `erase-pattern <id> --yes` is a usage error (exit 3) with no call and no erase.
func TestClEraseHasNoYesFlag(t *testing.T) {
	e := newEng(t)
	cpNoCalls(t, e, e.run("erase-pattern", cpID, "--yes"), 3)
}

// Covers T019: a refusal at DELETE (409 CapturedPatternBusy, 404 NotFound) after the right ID is
// exit 4 with the contract's problem lines on standard error and no operation followed.
func TestClEraseRefusalsExit4WithTheProblem(t *testing.T) {
	for _, c := range []struct {
		status       int
		code, detail string
	}{
		{409, "CapturedPatternBusy", "its proof's run has not ended"},
		{404, "NotFound", "no captured pattern tdd-s-1"},
	} {
		t.Run(c.code, func(t *testing.T) {
			e := newEng(t)
			var out syncBuf
			e.reply(clGet, 200, clProven())
			e.refuse(clErase, c.status, prob(c.code, c.code, c.detail))
			code, errs := clRunIn(e, &out, strings.NewReader(cpID+"\n"), "erase-pattern", cpID)
			if code != 4 {
				t.Fatalf("exit = %d, want 4\nstdout: %s\nstderr: %s", code, out.String(), errs)
			}
			if want := "refused: " + c.code + ": " + c.detail + "\n"; errs != want {
				t.Errorf("stderr = %q, want %q", errs, want)
			}
			if len(e.got("DELETE", "/captured-patterns/"+cpID)) != 1 {
				t.Error("the erase was asked for once")
			}
			if e.anyPathWith("/operations/") {
				t.Error("a refused erase has no operation to follow")
			}
		})
	}
}

// Covers T019: an unknown or erased captured pattern at the first read is exit 4 with the problem,
// asks nothing and sends no DELETE.
func TestClEraseOfAnUnknownPatternExit4WithoutAsking(t *testing.T) {
	e := newEng(t)
	var out syncBuf
	e.refuse(clGet, 404, prob("NotFound", "NotFound", "no captured pattern tdd-s-1"))
	code, errs := clRunIn(e, &out, strings.NewReader(cpID+"\n"), "erase-pattern", cpID)
	if code != 4 {
		t.Fatalf("exit = %d, want 4\nstdout: %s\nstderr: %s", code, out.String(), errs)
	}
	if want := "refused: NotFound: no captured pattern tdd-s-1\n"; errs != want {
		t.Errorf("stderr = %q, want %q", errs, want)
	}
	if out.String() != "" {
		t.Errorf("stdout = %q, want nothing (no question for a pattern that is not there)", out.String())
	}
	if len(e.got("DELETE", "/captured-patterns/"+cpID)) != 0 {
		t.Error("nothing to erase, nothing asked")
	}
}

// Covers T019: an erase whose operation fails is exit 4 with its problem, and one that is
// cancelled is exit 6.
func TestClEraseOperationFailedExit4AndCancelledExit6(t *testing.T) {
	e := newEng(t)
	var out syncBuf
	e.reply(clGet, 200, clProven())
	e.reply(clErase, 202, operation("OP-30", "erase-captured-pattern", "accepted", cpID, nil))
	failed := operation("OP-30", "erase-captured-pattern", "failed", cpID, nil)
	p := prob("OperationFailed", "OperationFailed", "the proof slice is held open")
	p["status"] = 422
	failed["error"] = p
	e.reply("GET /operations/OP-30", 200, failed)
	code, errs := clRunIn(e, &out, strings.NewReader(cpID+"\n"), "erase-pattern", cpID)
	if code != 4 {
		t.Fatalf("exit = %d, want 4\nstderr: %s", code, errs)
	}
	if want := "refused: OperationFailed: the proof slice is held open\n"; errs != want {
		t.Errorf("stderr = %q, want %q", errs, want)
	}
	e2 := newEng(t)
	var out2 syncBuf
	var at string
	clErasable(e2, &out2, &at, clProven(), "cancelled")
	code, _ = clRunIn(e2, &out2, strings.NewReader(cpID+"\n"), "erase-pattern", cpID)
	if code != 6 {
		t.Errorf("exit = %d, want 6 for a cancelled operation", code)
	}
}

// Covers T019: an accepted erase whose operation is still running is followed until it ends.
func TestClEraseFollowsARunningOperation(t *testing.T) {
	e := newEng(t)
	var out syncBuf
	e.reply(clGet, 200, clProven())
	e.reply(clErase, 202, operation("OP-30", "erase-captured-pattern", "accepted", cpID, nil))
	n := 0
	e.on("GET /operations/OP-30", func(w http.ResponseWriter, _ req) {
		n++
		st := "running"
		if n >= 3 {
			st = "succeeded"
		}
		writeJSON(w, 200, operation("OP-30", "erase-captured-pattern", st, cpID, nil))
	})
	code, errs := clRunIn(e, &out, strings.NewReader(cpID+"\n"), "erase-pattern", cpID)
	if code != 0 {
		t.Fatalf("exit = %d\nstdout: %s\nstderr: %s", code, out.String(), errs)
	}
	if n < 3 {
		t.Errorf("operation reads = %d, want at least 3", n)
	}
}

// Covers T019 (usage): a missing, malformed or extra argument is exit 3 and the engine is not
// called.
func TestClEraseUsageErrorsExit3WithoutCalls(t *testing.T) {
	for _, args := range [][]string{
		{"erase-pattern"},
		{"erase-pattern", "Tdd"},
		{"erase-pattern", ""},
		{"erase-pattern", cpID, "extra"},
		{"erase-pattern", cpID, cpID},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			e := newEng(t)
			cpNoCalls(t, e, e.run(args...), 3)
		})
	}
}

// ---- help ----

// Covers T019: the usage text lists both commands, each with its ID argument, as it lists the
// others. help needs no engine.
func TestClHelpListsBothCommands(t *testing.T) {
	e := newEng(t)
	r := e.run("help")
	wantExit(t, r, 0)
	for _, cmd := range []string{"withdraw-pattern <id>", "erase-pattern <id>"} {
		if !cpLineWith(r.out, cmd) {
			t.Errorf("help lacks a line for %q:\n%s", cmd, r.out)
		}
	}
}
