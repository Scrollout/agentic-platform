// Tests for the methods part of the comparison line of `trial-set` and `trials` (feature 012, T057, FR-009).
//
// Judged from src/cli/contract.md "Trial sets" item 4: the line ends `  methods differ <previous method> -> <this
// method>`, each method as its first 12 hex digits, exactly when the engine's `methods_differ` is true.
//
// Checked: the previous method then this one, each cut to 12; neither method printed when `methods_differ` is
// false (even when the two methods differ as text: the flag is the engine's, not recomputed); `less reliable`
// before `methods differ`; both commands; a method shorter than 12 printed whole; store text escaped.
//
// Left out: an empty method (the contract types a method as 64 hex digits, so the client has no rule for it);
// the engine's choice of the previous set (T022).
package cli_test

import (
	"strings"
	"testing"
)

// tmCompare serves a settled set whose method is this and whose previous set has the method prev, with the
// engine's flags less and differ.
func tmCompare(e *eng, this, prev string, less, differ bool) {
	p := tlPrev("TS-2", 3, 3, less, differ)
	p["method"] = prev
	tlCompare(e, tsOf("ended", "", 3, "done", "done", "done"), p, map[string]any{"method": this})
}

func tmLast(t *testing.T, out string) string {
	t.Helper()
	ls := tlLines(t, out)
	if len(ls) == 0 {
		t.Fatalf("no lines after the first:\n%s", out)
	}
	return ls[len(ls)-1]
}

func TestComparisonNamesThePreviousMethodThenThisOneCutTo12(t *testing.T) {
	prev := "0123456789abcdef" + strings.Repeat("0", 48)
	this := "fedcba987654321f" + strings.Repeat("1", 48)
	e := newEng(t)
	tmCompare(e, this, prev, false, true)
	r := e.run("trial-set", tsID)
	wantExit(t, r, 0)
	if got, want := tmLast(t, r.out), "compared with TS-2: done 3/3 -> 3/3  methods differ 0123456789ab -> fedcba987654"; got != want {
		t.Errorf("last line = %q, want %q", got, want)
	}
}

func TestComparisonPrintsNoMethodWhenTheEngineSaysTheyDoNotDiffer(t *testing.T) {
	e := newEng(t)
	// different as text, flag false: the client must not recompute
	tmCompare(e, strings.Repeat("a", 64), strings.Repeat("b", 64), false, false)
	r := e.run("trial-set", tsID)
	wantExit(t, r, 0)
	if got, want := tmLast(t, r.out), "compared with TS-2: done 3/3 -> 3/3"; got != want {
		t.Errorf("last line = %q, want %q", got, want)
	}
	wantAbsent(t, "stdout", r.out, "methods differ", strings.Repeat("a", 12), strings.Repeat("b", 12))
}

func TestComparisonPrintsMethodsAfterLessReliable(t *testing.T) {
	e := newEng(t)
	tmCompare(e, strings.Repeat("a", 64), strings.Repeat("b", 64), true, true)
	r := e.run("trial-set", tsID)
	wantExit(t, r, 0)
	want := "compared with TS-2: done 3/3 -> 3/3  less reliable  methods differ bbbbbbbbbbbb -> aaaaaaaaaaaa"
	if got := tmLast(t, r.out); got != want {
		t.Errorf("last line = %q, want %q", got, want)
	}
}

func TestTrialsPrintsTheMethodsAsTrialSetDoes(t *testing.T) {
	e := newEng(t)
	tsStart(e)
	p := tlPrev("TS-4", 3, 3, true, true)
	p["method"] = strings.Repeat("d", 64)
	tlCompare(e, tsOf("ended", "", 3, "done", "done", "done"), p, map[string]any{"method": strings.Repeat("e", 64)})
	r := e.run("trials", "S-1", "notes")
	wantExit(t, r, 0)
	want := "compared with TS-4: done 3/3 -> 3/3  less reliable  methods differ dddddddddddd -> eeeeeeeeeeee"
	if got := tmLast(t, r.out); got != want {
		t.Errorf("last line = %q, want %q", got, want)
	}
}

func TestComparisonPrintsAMethodShorterThan12Whole(t *testing.T) {
	e := newEng(t)
	tmCompare(e, "cafe", "abc", false, true)
	r := e.run("trial-set", tsID)
	wantExit(t, r, 0)
	if got, want := tmLast(t, r.out), "compared with TS-2: done 3/3 -> 3/3  methods differ abc -> cafe"; got != want {
		t.Errorf("last line = %q, want %q", got, want)
	}
}

func TestComparisonEscapesTheStoreTextOfTheMethods(t *testing.T) {
	e := newEng(t)
	tmCompare(e, "a\x1b[31mb", "p\nq", false, true)
	r := e.run("trial-set", tsID)
	wantExit(t, r, 0)
	wantAbsent(t, "stdout", r.out, "\x1b")
	if got, want := tmLast(t, r.out), `compared with TS-2: done 3/3 -> 3/3  methods differ p\nq -> a\x1b[31mb`; got != want {
		t.Errorf("last line = %q, want %q", got, want)
	}
}
