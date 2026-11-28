// T021 (feature 012, US3) with T038: the client's `trial-sets <slice>` and the comparison line of
// `trial-set`, written against src/cli/contract.md ("Trial sets": row 4 of the result, the `trial-sets
// <slice>` paragraph, the exit table, "Store text in the terminal"; the command table), the engine
// contract's listTrialSets, TrialSetList, TrialSet and TrialComparison
// (src/engine/api/preview/2026-11-12-preview/openapi.yaml) and specs/012-run-variance/ (US3, FR-008,
// FR-009, Clarifications 2026-11-28), not from src/cli/trials.go. Every command runs in process through
// cli.Run against the stand-in engine of runs_fake_test.go; the sets come from tsSet.json of
// trials_test.go (called, not edited) with the fields below replaced. Every name starts with tl.
//
// Part one (`trial-sets`) fails today: the command does not exist. Part two (the comparison line, T038)
// is already printed by trials.go, so its tests are guards that pass today.
//
// Edge cases checked: one table with one header over every page (3 pages; the first call without a page
// token, the next ones with the token the engine gave); an empty page that still has a next token (no
// rows, the next page is read); a final empty page; a page token that repeats itself (the command must
// end and not call the engine without bound); no sets at all (the sentence naming the slice, exit 0, no
// table); the engine's order kept even when the IDs are not descending; TRIALS is the set's `trials`;
// DONE as done/ended, 0/0 for a set with no ended trial; UNSTABLE only when `unstable`; METHOD cut to 12
// hex digits; CREATED as `runs` prints it (the fraction and the offset kept); alignment over rows of
// different widths, no trailing blanks; untrusted store text (control characters in an id and a state)
// never printed raw; the unknown slice (404 NotFound, exit 4, nothing on stdout); a failure on a later
// page prints no partial table; a malformed slice ID (exit 3, no engine call; also empty, a run ID, a
// set ID, a missing and an extra argument); the engine unreachable (exit 1) and an unreadable answer
// (exit 1). The comparison line: ended and stopped with `previous`, each flag alone and both together (in
// the contract's order), none when `previous` is absent, none for cancelled and failed even with a
// `previous`, it comes after the steps table and last, the exit code stays the set's, the flags are
// the engine's (the client does not recompute them from the numbers), the previous set's ID escaped,
// `trials` prints it as `trial-set` does.
//
// Left out: the engine's choice of the previous set (T022), a page of 50 (the engine's limit), the value
// of `share_change` (the client prints no number from it), a page token the engine would reject.
package cli_test

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

// ---- trial-sets ----

// tlSet is a listed set: a TrialSet of trials_test.go's builder with its identity replaced.
func tlSet(n int, state string, k int, method, created string, outcomes ...string) map[string]any {
	s := tsOf(state, "", k, outcomes...).json()
	s["id"] = fmt.Sprintf("TS-%d", n)
	s["method"] = method
	s["created_at"] = created
	return s
}

func tlHash(c string) string { return strings.Repeat(c, 64) }

// tlAlign renders rows as `runs` aligns a table (each column as wide as its widest cell plus two).
func tlAlign(rows ...[]string) string {
	return strings.Join(tvAlign(rows...), "\n") + "\n"
}

var tlHdr = []string{"SET", "STATE", "TRIALS", "DONE", "UNSTABLE", "METHOD", "CREATED"}

// tlPages serves the pages in order: the first without a token, page i with the token "tok-<i>".
func tlPages(e *eng, t *testing.T, pages ...[]any) {
	e.on("GET /slices/S-1/trial-sets", func(w http.ResponseWriter, c req) {
		i := 0
		if tok := c.Query.Get("page_token"); tok != "" {
			i = -1
			for j := 1; j < len(pages); j++ {
				if tok == fmt.Sprintf("tok-%d", j) {
					i = j
				}
			}
			if i < 0 {
				t.Errorf("page_token = %q, want one the engine gave", tok)
				writeProblem(w, 400, prob("InvalidRequest", "InvalidRequest", ""))
				return
			}
		}
		body := map[string]any{"items": pages[i]}
		if i+1 < len(pages) {
			body["next_page_token"] = fmt.Sprintf("tok-%d", i+1)
		}
		writeJSON(w, 200, body)
	})
}

func TestTrialSetsListsEveryPageInOneTableNewestFirst(t *testing.T) {
	e := newEng(t)
	tlPages(e, t,
		[]any{
			tlSet(5, "running", 3, tlHash("a"), "2026-11-14T10:05:00Z", "done", ""),
			tlSet(4, "stopped", 5, tlHash("b"), "2026-11-14T10:04:00Z", "done", "done", "done"),
		},
		[]any{
			tlSet(3, "ended", 2, tlHash("c"), "2026-11-14T10:03:00Z", "done", "handed-over"),
		},
		[]any{
			tlSet(2, "cancelled", 3, "0123456789abcdef"+strings.Repeat("0", 48), "2026-11-14T10:02:00Z"),
			tlSet(1, "failed", 1, tlHash("e"), "2026-11-14T10:01:00Z", "done"),
		})
	r := e.run("trial-sets", "S-1")
	wantExit(t, r, 0)
	wantOut(t, r, tlAlign(tlHdr,
		[]string{"TS-5", "running", "3", "1/1", "-", tlHash("a")[:12], "2026-11-14T10:05:00Z"},
		[]string{"TS-4", "stopped", "5", "3/3", "-", tlHash("b")[:12], "2026-11-14T10:04:00Z"},
		[]string{"TS-3", "ended", "2", "1/2", "unstable", tlHash("c")[:12], "2026-11-14T10:03:00Z"},
		[]string{"TS-2", "cancelled", "3", "0/0", "-", "0123456789ab", "2026-11-14T10:02:00Z"},
		[]string{"TS-1", "failed", "1", "1/1", "-", tlHash("e")[:12], "2026-11-14T10:01:00Z"}))
	calls := e.got("GET", "/slices/S-1/trial-sets")
	if len(calls) != 3 {
		t.Fatalf("listTrialSets calls = %d, want 3 (every page)", len(calls))
	}
	for i, want := range []string{"", "tok-1", "tok-2"} {
		if got := calls[i].Query.Get("page_token"); got != want {
			t.Errorf("call %d: page_token = %q, want %q", i+1, got, want)
		}
	}
	if n := strings.Count(r.out, "SET"); n != 1 {
		t.Errorf("header printed %d times, want once", n)
	}
}

func TestTrialSetsKeepsTheEnginesOrder(t *testing.T) {
	e := newEng(t)
	// IDs not descending: the order is the engine's, not a sort by ID.
	tlPages(e, t, []any{
		tlSet(2, "ended", 4, tlHash("a"), "2026-11-14T10:09:00Z", "done", "done"),
		tlSet(10, "ended", 3, tlHash("b"), "2026-11-14T10:08:00Z", "done", "done", "done"),
		tlSet(3, "ended", 1, tlHash("c"), "2026-11-14T10:07:00Z", "done"),
	})
	r := e.run("trial-sets", "S-1")
	wantExit(t, r, 0)
	wantOut(t, r, tlAlign(tlHdr,
		[]string{"TS-2", "ended", "4", "2/2", "-", tlHash("a")[:12], "2026-11-14T10:09:00Z"},
		[]string{"TS-10", "ended", "3", "3/3", "-", tlHash("b")[:12], "2026-11-14T10:08:00Z"},
		[]string{"TS-3", "ended", "1", "1/1", "-", tlHash("c")[:12], "2026-11-14T10:07:00Z"}))
}

func TestTrialSetsPrintsCreatedAsRunsPrintsIt(t *testing.T) {
	e := newEng(t)
	tlPages(e, t, []any{
		tlSet(2, "ended", 1, tlHash("a"), "2026-11-14T10:00:00.25Z", "done"),
		tlSet(1, "ended", 1, tlHash("a"), "2026-11-14T12:30:00+02:00", "done"),
	})
	r := e.run("trial-sets", "S-1")
	wantExit(t, r, 0)
	wantOut(t, r, tlAlign(tlHdr,
		[]string{"TS-2", "ended", "1", "1/1", "-", tlHash("a")[:12], "2026-11-14T10:00:00.25Z"},
		[]string{"TS-1", "ended", "1", "1/1", "-", tlHash("a")[:12], "2026-11-14T12:30:00+02:00"}))
}

func TestTrialSetsAlignsRowsOfDifferentWidths(t *testing.T) {
	e := newEng(t)
	tlPages(e, t, []any{
		tlSet(1234, "cancelled", 5, tlHash("f"), "2026-11-14T10:00:00Z"),
		tlSet(9, "ended", 5, tlHash("f"), "2026-11-14T10:00:00Z", "done", "done", "handed-over", "done", "done"),
	})
	r := e.run("trial-sets", "S-1")
	wantExit(t, r, 0)
	for _, l := range strings.Split(strings.TrimRight(r.out, "\n"), "\n") {
		if l != strings.TrimRight(l, " ") {
			t.Errorf("line %q has trailing blanks", l)
		}
	}
	wantOut(t, r, tlAlign(tlHdr,
		[]string{"TS-1234", "cancelled", "5", "0/0", "-", tlHash("f")[:12], "2026-11-14T10:00:00Z"},
		[]string{"TS-9", "ended", "5", "4/5", "unstable", tlHash("f")[:12], "2026-11-14T10:00:00Z"}))
}

func TestTrialSetsReadsAnEmptyPageThatHasANextToken(t *testing.T) {
	e := newEng(t)
	tlPages(e, t, []any{},
		[]any{tlSet(1, "ended", 1, tlHash("a"), "2026-11-14T10:00:00Z", "done")},
		[]any{})
	r := e.run("trial-sets", "S-1")
	wantExit(t, r, 0)
	wantOut(t, r, tlAlign(tlHdr,
		[]string{"TS-1", "ended", "1", "1/1", "-", tlHash("a")[:12], "2026-11-14T10:00:00Z"}))
	if n := len(e.got("GET", "/slices/S-1/trial-sets")); n != 3 {
		t.Errorf("listTrialSets calls = %d, want 3", n)
	}
}

func TestTrialSetsWithNoSetsSaysHowToStartOne(t *testing.T) {
	for name, pages := range map[string][][]any{
		"one empty page":                {{}},
		"empty pages with a next token": {{}, {}},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEng(t)
			tlPages(e, t, pages...)
			r := e.run("trial-sets", "S-1")
			wantExit(t, r, 0)
			wantOut(t, r, "No trial sets yet. Start one with trials S-1 <prefix>.\n")
		})
	}
}

func TestTrialSetsNamesTheSliceItWasAskedAboutInTheNoSetsLine(t *testing.T) {
	e := newEng(t)
	e.reply("GET /slices/S-42/trial-sets", 200, map[string]any{"items": []any{}})
	r := e.run("trial-sets", "S-42")
	wantExit(t, r, 0)
	wantOut(t, r, "No trial sets yet. Start one with trials S-42 <prefix>.\n")
}

func TestTrialSetsEndsWhenTheEngineRepeatsAPageToken(t *testing.T) {
	e := newEng(t)
	e.on("GET /slices/S-1/trial-sets", func(w http.ResponseWriter, c req) {
		writeJSON(w, 200, map[string]any{
			"items":           []any{tlSet(1, "ended", 1, tlHash("a"), "2026-11-14T10:00:00Z", "done")},
			"next_page_token": "same"})
	})
	done := make(chan result, 1)
	go func() { done <- e.run("trial-sets", "S-1") }()
	select {
	case r := <-done:
		if r.code == 0 {
			t.Errorf("a page token that never ends exited 0 with:\n%s", r.out)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("trial-sets did not end on a repeating page token")
	}
	if n := len(e.got("GET", "/slices/S-1/trial-sets")); n < 1 || n > 20 {
		t.Errorf("listTrialSets calls = %d, want 1 to 20 (read, then end)", n)
	}
}

func TestTrialSetsPrintsStoreTextEscaped(t *testing.T) {
	e := newEng(t)
	s := tlSet(1, "ended", 1, tlHash("a"), "2026-11-14T10:00:00Z", "done")
	s["id"] = "TS-1\x1b[31m"
	s["state"] = "end\ned"
	tlPages(e, t, []any{s})
	r := e.run("trial-sets", "S-1")
	// the engine's values break its contract: the client may refuse them or print them escaped,
	// never raw
	wantAbsent(t, "stdout", r.out, "\x1b", "end\ned")
	wantAbsent(t, "stderr", r.err, "unknown command")
	if n := len(e.got("GET", "/slices/S-1/trial-sets")); n != 1 {
		t.Errorf("listTrialSets calls = %d, want 1", n)
	}
}

func TestTrialSetsOfAnUnknownSliceExits4NotFound(t *testing.T) {
	e := newEng(t)
	e.refuse("GET /slices/S-9/trial-sets", 404, prob("NotFound", "NotFound", "no slice S-9"))
	r := e.run("trial-sets", "S-9")
	wantExit(t, r, 4)
	if r.out != "" {
		t.Errorf("stdout = %q, want nothing", r.out)
	}
	wantContains(t, "stderr", r.err, "NotFound")
}

func TestTrialSetsPrintsNoPartialTableWhenALaterPageFails(t *testing.T) {
	e := newEng(t)
	e.on("GET /slices/S-1/trial-sets", func(w http.ResponseWriter, c req) {
		if c.Query.Get("page_token") == "" {
			writeJSON(w, 200, map[string]any{
				"items":           []any{tlSet(1, "ended", 1, tlHash("a"), "2026-11-14T10:00:00Z", "done")},
				"next_page_token": "tok-1"})
			return
		}
		writeProblem(w, 404, prob("NotFound", "NotFound", "the slice was erased"))
	})
	r := e.run("trial-sets", "S-1")
	wantExit(t, r, 4)
	wantAbsent(t, "stdout", r.out, "SET", "TS-1")
}

func TestTrialSetsWithTheEngineUnreachableExits1(t *testing.T) {
	e := newEng(t)
	e.on("GET /slices/S-1/trial-sets", func(w http.ResponseWriter, _ req) { drop(w) })
	r := e.run("trial-sets", "S-1")
	wantExit(t, r, 1)
	if !strings.HasPrefix(r.err, "engine: ") {
		t.Errorf("stderr = %q, want an engine error", r.err)
	}
	if r.out != "" {
		t.Errorf("stdout = %q, want nothing", r.out)
	}
}

func TestTrialSetsWithAnUnreadableAnswerExits1(t *testing.T) {
	e := newEng(t)
	e.on("GET /slices/S-1/trial-sets", func(w http.ResponseWriter, _ req) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items": [`))
	})
	r := e.run("trial-sets", "S-1")
	wantExit(t, r, 1)
	if r.out != "" {
		t.Errorf("stdout = %q, want nothing", r.out)
	}
}

func TestTrialSetsUsageErrorsExit3WithoutCallingTheEngine(t *testing.T) {
	for _, args := range [][]string{
		{"trial-sets"},
		{"trial-sets", ""},
		{"trial-sets", "not-a-slice"},
		{"trial-sets", "S-"},
		{"trial-sets", "s-1"},
		{"trial-sets", "RUN-1"},
		{"trial-sets", "TS-1"},
		{"trial-sets", "S-1", "extra"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			e := newEng(t)
			r := e.run(args...)
			wantExit(t, r, 3)
			wantAbsent(t, "stderr", r.err, "unknown command")
			if e.total() != 0 {
				t.Errorf("engine calls = %d, want none for a usage error", e.total())
			}
			if r.out != "" {
				t.Errorf("stdout = %q, want nothing", r.out)
			}
		})
	}
}

// ---- the comparison line of trial-set (T038) ----

// tlCompare serves the set with `result.previous` = prev (nil: none) and extra top-level fields, and
// the set's operation as its state.
func tlCompare(e *eng, s tsSet, prev map[string]any, extra map[string]any) {
	m := s.json()
	if prev != nil {
		m["result"].(map[string]any)["previous"] = prev
	}
	for k, v := range extra {
		m[k] = v
	}
	e.reply("GET /trial-sets/"+tsID, 200, m)
	e.reply("GET /operations/"+tsOp, 200, operation(tsOp, "run-trials", tsOpFor(s.state), tsID, map[string]any{"trial_set": tsID}))
}

func tlPrev(set string, done, ended int, less, differ bool) map[string]any {
	share := 0.0
	if less {
		share = -0.25
	}
	return map[string]any{"trial_set": set, "method": tlHash("9"), "done": done, "ended": ended,
		"share_change": share, "less_reliable": less, "methods_differ": differ}
}

// tlLines is the output of the result after its first line, without blank lines.
func tlLines(t *testing.T, out string) []string {
	t.Helper()
	_, _, after := tsSplit(t, out)
	return tsNonBlank(after)
}

func TestTrialSetComparisonLineForEndedAndStoppedSets(t *testing.T) {
	cases := []struct {
		name string
		set  tsSet
		prev map[string]any
		want string
		exit int
	}{
		{"ended, neither flag", tsOf("ended", "", 3, "done", "done", "done"), tlPrev("TS-2", 2, 3, false, false),
			"compared with TS-2: done 2/3 -> 3/3", 0},
		{"ended, less reliable", tsOf("ended", "", 3, "done", "done", "handed-over"), tlPrev("TS-2", 3, 3, true, false),
			"compared with TS-2: done 3/3 -> 2/3  less reliable", 7},
		{"ended, methods differ", tsOf("ended", "", 2, "done", "done"), tlPrev("TS-7", 1, 2, false, true),
			"compared with TS-7: done 1/2 -> 2/2  methods differ 999999999999 -> cccccccccccc", 0},
		{"ended, both flags in this order", tsOf("ended", "", 3, "done", "handed-over", "done"), tlPrev("TS-12", 3, 3, true, true),
			"compared with TS-12: done 3/3 -> 2/3  less reliable  methods differ 999999999999 -> cccccccccccc", 7},
		{"stopped, neither flag", tsOf("stopped", "budget", 3, "done", "done"), tlPrev("TS-1", 0, 3, false, false),
			"compared with TS-1: done 0/3 -> 2/2", 0},
		{"stopped, less reliable", tsOf("stopped", "time", 3, "handed-over"), tlPrev("TS-1", 1, 1, true, false),
			"compared with TS-1: done 1/1 -> 0/1  less reliable", 7},
		{"stopped, methods differ", tsOf("stopped", "workers", 3, "done"), tlPrev("TS-3", 5, 5, false, true),
			"compared with TS-3: done 5/5 -> 1/1  methods differ 999999999999 -> cccccccccccc", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEng(t)
			tlCompare(e, c.set, c.prev, nil)
			r := e.run("trial-set", tsID)
			wantExit(t, r, c.exit)
			ls := tlLines(t, r.out)
			if len(ls) == 0 || ls[len(ls)-1] != c.want {
				t.Errorf("last line of the result:\n%s\nwant %q", strings.Join(ls, "\n"), c.want)
			}
			if n := strings.Count(r.out, "compared with"); n != 1 {
				t.Errorf("comparison lines = %d, want 1", n)
			}
		})
	}
}

func TestTrialSetComparisonLineComesAfterTheStepsTable(t *testing.T) {
	e := newEng(t)
	tlCompare(e, tsOf("ended", "", 3, "done", "done", "done"), tlPrev("TS-2", 2, 3, false, false), nil)
	r := e.run("trial-set", tsID)
	wantExit(t, r, 0)
	ls := tlLines(t, r.out)
	last := len(ls) - 1
	if ls[last] != "compared with TS-2: done 2/3 -> 3/3" {
		t.Fatalf("last line = %q, want the comparison", ls[last])
	}
	if !regexp.MustCompile(`^merge {2,}`).MatchString(ls[last-1]) {
		t.Errorf("the line before the comparison = %q, want the last row of the steps table", ls[last-1])
	}
}

func TestTrialSetHasNoComparisonLineWithoutAPrevious(t *testing.T) {
	for _, s := range []tsSet{
		tsOf("ended", "", 3, "done", "done", "done"),
		tsOf("stopped", "budget", 3, "done"),
		tsOf("ended", "", 3, "done", "handed-over", "done"),
	} {
		e := newEng(t)
		tlCompare(e, s, nil, nil)
		r := e.run("trial-set", tsID)
		if r.code != 0 && r.code != 7 {
			t.Fatalf("exit = %d, want 0 or 7\n%s", r.code, r.err)
		}
		wantAbsent(t, "stdout", r.out, "compared with")
	}
}

func TestTrialSetHasNoComparisonLineForACancelledOrFailedSetEvenWithAPrevious(t *testing.T) {
	prev := tlPrev("TS-2", 3, 3, true, true)
	t.Run("cancelled with an ended trial", func(t *testing.T) {
		e := newEng(t)
		tlCompare(e, tsOf("cancelled", "", 3, "done", ""), prev, nil)
		r := e.run("trial-set", tsID)
		wantExit(t, r, 6)
		wantAbsent(t, "stdout", r.out, "compared with")
	})
	t.Run("cancelled with no trial", func(t *testing.T) {
		e := newEng(t)
		tlCompare(e, tsOf("cancelled", "", 3), prev, nil)
		r := e.run("trial-set", tsID)
		wantExit(t, r, 6)
		wantAbsent(t, "stdout", r.out, "compared with")
	})
	t.Run("failed", func(t *testing.T) {
		e := newEng(t)
		tlCompare(e, tsOf("failed", "", 3, "done", "done"), prev, map[string]any{"failed_because": "the store is full"})
		r := e.run("trial-set", tsID)
		wantExit(t, r, 4)
		wantAbsent(t, "stdout", r.out, "compared with")
	})
}

func TestTrialSetComparisonNeverChangesTheExitCode(t *testing.T) {
	sets := map[int]tsSet{
		0: tsOf("ended", "", 3, "done", "done", "done"),
		7: tsOf("ended", "", 3, "done", "handed-over", "done"),
	}
	for want, s := range sets {
		for _, p := range []map[string]any{nil, tlPrev("TS-2", 3, 3, false, false), tlPrev("TS-2", 0, 3, true, true), tlPrev("TS-2", 3, 3, true, false)} {
			e := newEng(t)
			tlCompare(e, s, p, nil)
			wantExit(t, e.run("trial-set", tsID), want)
		}
	}
}

func TestTrialSetComparisonFlagsAreTheEnginesNotRecomputedFromTheNumbers(t *testing.T) {
	e := newEng(t)
	tlCompare(e, tsOf("ended", "", 3, "done", "handed-over", "handed-over"), tlPrev("TS-2", 3, 3, false, false), nil)
	r := e.run("trial-set", tsID)
	wantExit(t, r, 7)
	wantContains(t, "stdout", r.out, "compared with TS-2: done 3/3 -> 1/3\n")
	wantAbsent(t, "stdout", r.out, "less reliable", "methods differ")
	e = newEng(t)
	tlCompare(e, tsOf("ended", "", 3, "done", "done", "done"), tlPrev("TS-2", 0, 3, true, false), nil)
	r = e.run("trial-set", tsID)
	wantExit(t, r, 0)
	wantContains(t, "stdout", r.out, "compared with TS-2: done 0/3 -> 3/3  less reliable\n")
}

func TestTrialsPrintsTheComparisonLineAsTrialSetDoes(t *testing.T) {
	e := newEng(t)
	tsStart(e)
	tlCompare(e, tsOf("ended", "", 3, "done", "done", "handed-over"), tlPrev("TS-4", 3, 3, true, true), nil)
	r := e.run("trials", "S-1", "notes")
	wantExit(t, r, 7)
	ls := tlLines(t, r.out)
	if want := "compared with TS-4: done 3/3 -> 2/3  less reliable  methods differ 999999999999 -> cccccccccccc"; ls[len(ls)-1] != want {
		t.Errorf("last line = %q, want %q", ls[len(ls)-1], want)
	}
}

func TestTrialSetComparisonPrintsTheStoreTextOfThePreviousSetEscaped(t *testing.T) {
	e := newEng(t)
	tlCompare(e, tsOf("ended", "", 3, "done", "done", "done"), tlPrev("TS-2\x1b[31m\nx", 3, 3, false, false), nil)
	r := e.run("trial-set", tsID)
	wantAbsent(t, "stdout", r.out, "\x1b")
	wantContains(t, "stdout", r.out, `compared with TS-2\x1b[31m\nx: done 3/3 -> 3/3`)
}
