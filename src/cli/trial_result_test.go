// T016 (feature 012, US2): the trial and step tables of a trial set's result as the client prints them,
// written against src/cli/contract.md ("Trial sets": rows 2 and 3 and the formatting rules), the engine
// contract's TrialOutcome.totals, TrialResult.steps, StepSpread, MeasureSpreads, Spread and StepFiles
// (src/engine/api/preview/2026-11-12-preview/openapi.yaml) and specs/012-run-variance/ (FR-005, FR-006,
// SC-003, US2), not from src/cli/trials.go. Every command runs in process through cli.Run against the
// stand-in engine of runs_fake_test.go (called, not edited); the set comes from tsSet.json of
// trials_test.go with its result's totals and steps replaced by the values below.
//
// The tables are judged as whole lines, built here from the cells the contract promises and aligned
// the way `runs` aligns: each column as wide as its widest cell plus two spaces, the last column not
// padded. Cells: RUN, OUTCOME, ATTEMPTS, ROUNDS, SECONDS, TOKENS, COST of a trial; STEP, REACHED, then
// each measure as `lowest/median/highest` or `-`, then FILES as `same`, `1,2|3` or `-` of a step.
//
// Guards: the formatting code already exists in trials.go (T038) and is untested, so a test here that
// passes today is a guard for it, not proof that the work for US2 is built; the engine-side values come
// with T017. Every name starts with tv so no other file collides with it.
//
// Edge cases checked: seconds rounded to whole (125.4 -> 125, 59.6 -> 60, 0.4 -> 0); a cost with two
// decimals ($0.50, $1.23, $0.00 for a given zero); a given 0 is 0, never `-`; a measure absent is `-` in
// trial rows, in the lowest/median/highest of a step and for a whole step; a median between two whole
// values keeps one decimal (2.5, 12.5), a whole median does not (12 not 12.0), also for seconds and
// for a cost median ($0.25); a step no trial reached (reached 0, no measures, no files); files same,
// two groups (1,3|2), three groups (1|2|3) and one trial; a trial whose outcome is absent has `-`;
// the same tables from `trials` as from `trial-set`; columns aligned over rows of different widths
// (a wide step name, a wide token count); no trailing blanks.
package cli_test

import (
	"strings"
	"testing"
)

// tvAlign renders rows as `runs` aligns a table: a column is as wide as its widest cell plus two blanks;
// the last column ends with its text.
func tvAlign(rows ...[]string) []string {
	width := make([]int, len(rows[0]))
	for _, r := range rows {
		for i, c := range r {
			width[i] = max(width[i], len([]rune(c)))
		}
	}
	var out []string
	for _, r := range rows {
		var b strings.Builder
		for i, c := range r {
			b.WriteString(c)
			if i < len(r)-1 {
				b.WriteString(strings.Repeat(" ", width[i]-len([]rune(c))+2))
			}
		}
		out = append(out, b.String())
	}
	return out
}

type tvStep struct {
	name     string
	reached  int
	measures map[string]any // attempts, review_rounds, seconds, tokens, cost_usd: each a spread
	files    map[string]any // nil: no files
}

func tvSpread(lo, med, hi float64) map[string]any {
	return map[string]any{"lowest": lo, "median": med, "highest": hi}
}

func tvGroups(groups ...[]int) map[string]any {
	gs := []any{}
	for _, g := range groups {
		ts := []any{}
		for _, n := range g {
			ts = append(ts, n)
		}
		gs = append(gs, map[string]any{"trials": ts})
	}
	return map[string]any{"same": len(groups) == 1, "groups": gs}
}

// tvServe makes the engine answer the set with the trials' totals (one map per started trial) and steps.
func tvServe(e *eng, s tsSet, totals []map[string]any, steps []tvStep) {
	m := s.json()
	res := m["result"].(map[string]any)
	for i, o := range res["outcomes"].([]any) {
		o.(map[string]any)["totals"] = totals[i]
	}
	ss := []any{}
	for _, st := range steps {
		x := map[string]any{"step": st.name, "reached": st.reached, "measures": st.measures}
		if st.measures == nil {
			x["measures"] = map[string]any{}
		}
		if st.files != nil {
			x["files"] = st.files
		}
		ss = append(ss, x)
	}
	res["steps"] = ss
	e.reply("GET /trial-sets/"+tsID, 200, m)
	e.reply("GET /operations/"+tsOp, 200, operation(tsOp, "run-trials", tsOpFor(s.state), tsID, map[string]any{"trial_set": tsID}))
}

// tvTables is what follows the first line of the result, without blank lines.
func tvTables(t *testing.T, out string) []string {
	t.Helper()
	_, _, after := tsSplit(t, out)
	var rows []string
	for _, l := range tsNonBlank(after) {
		if strings.HasPrefix(l, "compared with ") {
			break
		}
		rows = append(rows, l)
	}
	return rows
}

func tvWant(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("tables:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, l := range got {
		if l != strings.TrimRight(l, " ") {
			t.Errorf("line %q has trailing blanks", l)
		}
	}
}

var tvTrialHdr = []string{"RUN", "OUTCOME", "ATTEMPTS", "ROUNDS", "SECONDS", "TOKENS", "COST"}
var tvStepHdr = []string{"STEP", "REACHED", "ATTEMPTS", "ROUNDS", "SECONDS", "TOKENS", "COST", "FILES"}

// tvFull is a set with every kind of cell: whole and fractional seconds, a given zero, absent measures,
// a median between two whole values, a step nobody reached, files same and in groups.
func tvFull() (tsSet, []map[string]any, []tvStep, []string) {
	set := tsOf("ended", "", 3, "done", "done", "handed-over")
	totals := []map[string]any{
		{"attempts": 4, "review_rounds": 1, "seconds": 125.4, "tokens": 5000, "cost_usd": 0.5},
		{"attempts": 3, "review_rounds": 0, "seconds": 59.6, "tokens": 0, "cost_usd": 1.2345},
		{},
	}
	steps := []tvStep{
		{name: "tests", reached: 3, measures: map[string]any{
			"attempts": tvSpread(1, 2, 3), "review_rounds": tvSpread(0, 1, 1), "seconds": tvSpread(3.6, 12.5, 59.6),
			"tokens": tvSpread(100, 250, 400), "cost_usd": tvSpread(0.1, 0.25, 1.2345)}, files: tvGroups([]int{1, 2, 3})},
		{name: "code", reached: 2, measures: map[string]any{"attempts": tvSpread(2, 2.5, 3), "seconds": tvSpread(0.4, 12, 12)},
			files: tvGroups([]int{1, 3}, []int{2})},
		{name: "merge", reached: 0},
	}
	want := append(tvAlign(tvTrialHdr,
		[]string{"RUN-21", "done", "4", "1", "125", "5000", "$0.50"},
		[]string{"RUN-22", "done", "3", "0", "60", "0", "$1.23"},
		[]string{"RUN-23", "handed-over", "-", "-", "-", "-", "-"}),
		tvAlign(tvStepHdr,
			[]string{"tests", "3", "1/2/3", "0/1/1", "4/12.5/60", "100/250/400", "$0.10/$0.25/$1.23", "same"},
			[]string{"code", "2", "2/2.5/3", "-", "0/12/12", "-", "-", "1,3|2"},
			[]string{"merge", "0", "-", "-", "-", "-", "-", "-"})...)
	return set, totals, steps, want
}

func TestTrialSetPrintsTheTrialRowsAndTheStepRowsWithTheirValuesAligned(t *testing.T) {
	set, totals, steps, want := tvFull()
	e := newEng(t)
	tvServe(e, set, totals, steps)
	r := e.run("trial-set", tsID)
	wantExit(t, r, 7)
	tvWant(t, tvTables(t, r.out), want)
}

func TestTrialsPrintsTheSameTablesAsTrialSet(t *testing.T) {
	set, totals, steps, want := tvFull()
	e := newEng(t)
	tsStart(e)
	tvServe(e, set, totals, steps)
	r := e.run("trials", "S-1", "notes")
	wantExit(t, r, 7)
	tvWant(t, tvTables(t, r.out), want)
}

func TestTrialSetTrialRowAlignsOverWideAndNarrowCells(t *testing.T) {
	set := tsOf("ended", "", 2, "done", "done")
	totals := []map[string]any{
		{"attempts": 12, "review_rounds": 10, "seconds": 3600.0, "tokens": 1234567, "cost_usd": 12.0},
		{"attempts": 1, "review_rounds": 0, "seconds": 0.4, "tokens": 7, "cost_usd": 0},
	}
	steps := []tvStep{{name: "a-very-long-step-name", reached: 2, measures: map[string]any{"tokens": tvSpread(7, 620000, 1234567)}, files: tvGroups([]int{1, 2})},
		{name: "code", reached: 2}, {name: "merge", reached: 0}}
	e := newEng(t)
	tvServe(e, set, totals, steps)
	r := e.run("trial-set", tsID)
	wantExit(t, r, 0)
	want := append(tvAlign(tvTrialHdr,
		[]string{"RUN-21", "done", "12", "10", "3600", "1234567", "$12.00"},
		[]string{"RUN-22", "done", "1", "0", "0", "7", "$0.00"}),
		tvAlign(tvStepHdr,
			[]string{"a-very-long-step-name", "2", "-", "-", "-", "7/620000/1234567", "-", "same"},
			[]string{"code", "2", "-", "-", "-", "-", "-", "-"},
			[]string{"merge", "0", "-", "-", "-", "-", "-", "-"})...)
	tvWant(t, tvTables(t, r.out), want)
}

func TestTrialSetFilesAreGroupedByEqualFilesInOrder(t *testing.T) {
	set := tsOf("ended", "", 3, "done", "done", "done")
	z := map[string]any{}
	totals := []map[string]any{z, z, z}
	steps := []tvStep{
		{name: "tests", reached: 3, files: tvGroups([]int{1}, []int{2}, []int{3})},
		{name: "code", reached: 3, files: tvGroups([]int{1, 2}, []int{3})},
		{name: "merge", reached: 3, files: tvGroups([]int{1, 2, 3})},
	}
	e := newEng(t)
	tvServe(e, set, totals, steps)
	r := e.run("trial-set", tsID)
	wantExit(t, r, 0)
	want := append(tvAlign(tvTrialHdr,
		[]string{"RUN-21", "done", "-", "-", "-", "-", "-"},
		[]string{"RUN-22", "done", "-", "-", "-", "-", "-"},
		[]string{"RUN-23", "done", "-", "-", "-", "-", "-"}),
		tvAlign(tvStepHdr,
			[]string{"tests", "3", "-", "-", "-", "-", "-", "1|2|3"},
			[]string{"code", "3", "-", "-", "-", "-", "-", "1,2|3"},
			[]string{"merge", "3", "-", "-", "-", "-", "-", "same"})...)
	tvWant(t, tvTables(t, r.out), want)
}

func TestTrialSetOfOneTrialAndATrialWithoutAnOutcomeShowsDashes(t *testing.T) {
	set := tsOf("stopped", "budget", 3, "done", "")
	totals := []map[string]any{
		{"attempts": 2, "review_rounds": 0, "seconds": 30.0, "tokens": 900, "cost_usd": 0.07},
		{"attempts": 1},
	}
	steps := []tvStep{
		{name: "tests", reached: 2, measures: map[string]any{"attempts": tvSpread(1, 1.5, 2)}, files: tvGroups([]int{1})},
		{name: "code", reached: 0}, {name: "merge", reached: 0}}
	e := newEng(t)
	tvServe(e, set, totals, steps)
	r := e.run("trial-set", tsID)
	wantExit(t, r, 0)
	want := append(tvAlign(tvTrialHdr,
		[]string{"RUN-21", "done", "2", "0", "30", "900", "$0.07"},
		[]string{"RUN-22", "-", "1", "-", "-", "-", "-"}),
		tvAlign(tvStepHdr,
			[]string{"tests", "2", "1/1.5/2", "-", "-", "-", "-", "same"},
			[]string{"code", "0", "-", "-", "-", "-", "-", "-"},
			[]string{"merge", "0", "-", "-", "-", "-", "-", "-"})...)
	tvWant(t, tvTables(t, r.out), want)
}
