package trials_test

// T020 of feature 012 (US3; FR-009; research R6 "Comparison"; data model "Trial result" `previous`;
// spec Clarifications 2026-11-28, last bullet). Judged from the interface sheet, not from the code:
// trials.Previous compares a set with the slice's previous set, the one of `others` with the
// highest Number below the set's whose State is "ended" or "stopped" and whose Ended is at least 1.
// It is nil when the set's own State is neither, when its own Ended is 0, or when none qualifies.
// It is pure: `others` is read, never changed.
//
// Edge cases checked: no others; others only later or only the set itself; the set itself listed
// in `others` with another state; others in ascending, descending, shuffled and rotated order; each
// skipped state (cancelled, failed, running, an empty state) and Ended 0 skipped in favour of an
// older qualifying set, in both orders, and all of them at once; every own state (running,
// cancelled, failed, empty) and Ended 0 giving nil with a qualifying other; a `stopped` and an
// `ended` set both as the set and as the previous one; a previous with Done 0; share change at the
// extremes -1 and +1, equal, rise, fall, unequal k (2/3 against 1/2, 1/2 against 2/3); less_reliable
// only when it fell, also when equal shares come from different k (2/6 against 1/3); methods
// differ and equal; the previous set's ID, method, done and ended copied; the input left as given.
//
// Left out: a set with Done above Ended (not a value trials.Of can give); duplicate numbers in
// `others` (set IDs come from a sequence that never repeats).

import (
	"math"
	"slices"
	"strconv"
	"testing"

	"github.com/agentic-platform/engine/trials"
)

func prevSum(n int, state, method string, done, ended int) trials.SetSummary {
	return trials.SetSummary{Number: n, ID: "TS-" + strconv.Itoa(n), State: state, Method: method, Done: done, Ended: ended}
}

func prevNone(t *testing.T, what string, got *trials.Comparison) {
	t.Helper()
	if got != nil {
		t.Errorf("%s: Previous = %+v, want none", what, *got)
	}
}

func prevSet(t *testing.T, what string, got *trials.Comparison, id string) *trials.Comparison {
	t.Helper()
	if got == nil {
		t.Fatalf("%s: Previous = none, want %s", what, id)
	}
	if got.TrialSet != id {
		t.Errorf("%s: Previous names %s, want %s", what, got.TrialSet, id)
	}
	return got
}

func TestTheFirstSetHasNoPrevious(t *testing.T) {
	set := prevSum(1, "ended", "m", 1, 1)
	prevNone(t, "no others", trials.Previous(set, nil))
	prevNone(t, "empty others", trials.Previous(set, []trials.SetSummary{}))
}

func TestLaterSetsAndTheSetItselfAreNotPrevious(t *testing.T) {
	set := prevSum(3, "ended", "m", 1, 2)
	others := []trials.SetSummary{
		prevSum(3, "ended", "m", 2, 2), // the set itself, as the slice lists it
		prevSum(4, "ended", "m", 2, 2),
		prevSum(9, "stopped", "m", 1, 1),
	}
	prevNone(t, "only itself and later sets", trials.Previous(set, others))
	prevNone(t, "itself as cancelled", trials.Previous(set, []trials.SetSummary{prevSum(3, "cancelled", "m", 1, 1)}))
	got := prevSet(t, "earlier among later ones", trials.Previous(set, append(slices.Clone(others), prevSum(2, "ended", "m", 1, 1))), "TS-2")
	if got.Done != 1 || got.Ended != 1 {
		t.Errorf("counts = %d/%d, want 1/1", got.Done, got.Ended)
	}
}

func TestTheLatestEarlierEndedOrStoppedSetIsChosenWhateverTheOrder(t *testing.T) {
	set := prevSum(6, "ended", "m", 1, 1)
	a := []trials.SetSummary{
		prevSum(1, "ended", "m", 1, 1),
		prevSum(2, "stopped", "m", 1, 1),
		prevSum(3, "ended", "m", 1, 1),
		prevSum(4, "stopped", "m", 1, 1), // the latest earlier
		prevSum(6, "ended", "m", 1, 1),
		prevSum(7, "ended", "m", 1, 1),
	}
	d := slices.Clone(a)
	slices.Reverse(d)
	orders := map[string][]trials.SetSummary{
		"ascending":  slices.Clone(a),
		"descending": d,
		"shuffled":   {a[3], a[0], a[5], a[2], a[4], a[1]},
		"rotated":    append(slices.Clone(a[2:]), a[:2]...),
	}
	for name, others := range orders {
		prevSet(t, name, trials.Previous(set, others), "TS-4")
	}
}

func TestAnEndedOrStoppedSetIsComparedWithAnEndedOrStoppedOne(t *testing.T) {
	for _, state := range []string{"ended", "stopped"} {
		set := prevSum(2, state, "m", 0, 1)
		prevSet(t, "set "+state+", previous ended", trials.Previous(set, []trials.SetSummary{prevSum(1, "ended", "m", 1, 1)}), "TS-1")
		prevSet(t, "set "+state+", previous stopped", trials.Previous(set, []trials.SetSummary{prevSum(1, "stopped", "m", 1, 1)}), "TS-1")
	}
}

func TestASetThatCannotBePreviousIsSkippedForAnOlderOne(t *testing.T) {
	set := prevSum(5, "ended", "m", 1, 1)
	older := prevSum(1, "ended", "m", 1, 1)
	for name, skipped := range map[string]trials.SetSummary{
		"cancelled":      prevSum(4, "cancelled", "m", 1, 1),
		"failed":         prevSum(4, "failed", "m", 1, 1),
		"running":        prevSum(4, "running", "m", 1, 1),
		"no state":       prevSum(4, "", "m", 1, 1),
		"ended, none":    prevSum(4, "ended", "m", 0, 0),
		"stopped, none":  prevSum(4, "stopped", "m", 0, 0),
		"cancelled, 0/2": prevSum(4, "cancelled", "m", 0, 2),
	} {
		prevSet(t, name+" is skipped", trials.Previous(set, []trials.SetSummary{skipped, older}), "TS-1")
		prevSet(t, name+" is skipped (other order)", trials.Previous(set, []trials.SetSummary{older, skipped}), "TS-1")
		prevNone(t, name+" alone", trials.Previous(set, []trials.SetSummary{skipped}))
	}
	all := []trials.SetSummary{older, prevSum(2, "cancelled", "m", 1, 1), prevSum(3, "failed", "m", 1, 1),
		prevSum(4, "running", "m", 1, 1), prevSum(4, "ended", "m", 0, 0)}
	prevSet(t, "all skipped at once", trials.Previous(set, all), "TS-1")
}

func TestASetWithNothingToCompareHasNoPrevious(t *testing.T) {
	others := []trials.SetSummary{prevSum(1, "ended", "m", 1, 1), prevSum(2, "stopped", "m", 2, 3)}
	for name, set := range map[string]trials.SetSummary{
		"running":       prevSum(3, "running", "m", 1, 1),
		"cancelled":     prevSum(3, "cancelled", "m", 1, 1),
		"failed":        prevSum(3, "failed", "m", 1, 1),
		"no state":      prevSum(3, "", "m", 1, 1),
		"ended, none":   prevSum(3, "ended", "m", 0, 0),
		"stopped, none": prevSum(3, "stopped", "m", 0, 0),
	} {
		prevNone(t, name, trials.Previous(set, others))
	}
}

func TestTheChangeInTheShareOfDoneTrials(t *testing.T) {
	const eps = 1e-9
	for _, c := range []struct {
		name                      string
		prevDone, prevEnded, d, e int
		want                      float64
		less                      bool
	}{
		{"rise", 1, 2, 3, 3, 0.5, false},
		{"fall", 3, 3, 1, 2, -0.5, true},
		{"equal", 1, 2, 2, 4, 0, false},
		{"equal, all done", 1, 1, 3, 3, 0, false},
		{"equal, none done", 0, 1, 0, 5, 0, false},
		{"all done to none", 4, 4, 0, 4, -1, true},
		{"none done to all", 0, 4, 4, 4, 1, false},
		{"previous none done, this some", 0, 2, 1, 4, 0.25, false},
		{"unequal k, 1/2 to 2/3: a rise", 1, 2, 2, 3, 2.0/3 - 0.5, false},
		{"unequal k, 2/3 to 1/2: a fall", 2, 3, 1, 2, 0.5 - 2.0/3, true},
	} {
		set := prevSum(2, "ended", "m", c.d, c.e)
		got := trials.Previous(set, []trials.SetSummary{prevSum(1, "ended", "m", c.prevDone, c.prevEnded)})
		if got == nil {
			t.Errorf("%s: Previous = none", c.name)
			continue
		}
		if math.Abs(got.ShareChange-c.want) > eps {
			t.Errorf("%s: ShareChange = %v, want %v", c.name, got.ShareChange, c.want)
		}
		if got.LessReliable != c.less {
			t.Errorf("%s: LessReliable = %v, want %v (exactly when the share fell)", c.name, got.LessReliable, c.less)
		}
	}
}

func TestLessReliableIsNotSetWhenTheSharesAreEqualWhateverTheirK(t *testing.T) {
	for _, c := range [][4]int{{2, 6, 1, 3}, {1, 3, 2, 6}, {3, 9, 1, 3}, {1, 7, 3, 21}, {5, 10, 1, 2}} {
		got := trials.Previous(prevSum(2, "ended", "m", c[2], c[3]), []trials.SetSummary{prevSum(1, "ended", "m", c[0], c[1])})
		if got == nil {
			t.Fatalf("%v: no previous", c)
		}
		if got.LessReliable {
			t.Errorf("%v: LessReliable with ShareChange %v, want not (equal share)", c, got.ShareChange)
		}
		if math.Abs(got.ShareChange) > 1e-9 {
			t.Errorf("%v: ShareChange = %v, want 0", c, got.ShareChange)
		}
	}
}

func TestMethodsDifferExactlyWhenTheMethodsDiffer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{{"aaa", "aaa", false}, {"aaa", "bbb", true}, {"", "", false}, {"aaa", "", true}} {
		got := trials.Previous(prevSum(2, "ended", c.a, 1, 1), []trials.SetSummary{prevSum(1, "ended", c.b, 1, 1)})
		if got == nil {
			t.Fatalf("%q/%q: no previous", c.a, c.b)
		}
		if got.MethodsDiffer != c.want {
			t.Errorf("set %q, previous %q: MethodsDiffer = %v, want %v", c.a, c.b, got.MethodsDiffer, c.want)
		}
	}
}

func TestThePreviousSetsIDMethodAndCountsAreCopied(t *testing.T) {
	set := prevSum(9, "ended", "new-method", 2, 4)
	prev := prevSum(7, "stopped", "old-method", 3, 5)
	others := []trials.SetSummary{prevSum(2, "ended", "x", 0, 1), prev, prevSum(8, "cancelled", "y", 5, 5)}
	got := prevSet(t, "copy", trials.Previous(set, others), "TS-7")
	if got.Method != "old-method" {
		t.Errorf("Method = %q, want the previous set's old-method", got.Method)
	}
	if got.Done != 3 || got.Ended != 5 {
		t.Errorf("Done/Ended = %d/%d, want the previous set's 3/5", got.Done, got.Ended)
	}
	if !got.MethodsDiffer {
		t.Error("MethodsDiffer = false, want true")
	}
	if want := 2.0/4 - 3.0/5; math.Abs(got.ShareChange-want) > 1e-9 || !got.LessReliable {
		t.Errorf("ShareChange/LessReliable = %v/%v, want %v/true", got.ShareChange, got.LessReliable, want)
	}
}

func TestThePreviousSetCanHaveNoDoneTrial(t *testing.T) {
	got := prevSet(t, "0 of 3 done", trials.Previous(prevSum(2, "ended", "m", 3, 3), []trials.SetSummary{prevSum(1, "stopped", "m", 0, 3)}), "TS-1")
	if got.Done != 0 || got.Ended != 3 || math.Abs(got.ShareChange-1) > 1e-9 || got.LessReliable {
		t.Errorf("got %+v, want 0/3 and a rise of 1", *got)
	}
}

func TestPreviousReadsItsInputsAndChangesNothing(t *testing.T) {
	set := prevSum(5, "ended", "m", 1, 2)
	others := []trials.SetSummary{prevSum(4, "ended", "a", 1, 1), prevSum(2, "ended", "b", 1, 1), prevSum(5, "ended", "m", 1, 2), prevSum(9, "running", "c", 0, 0)}
	before := slices.Clone(others)
	first := trials.Previous(set, others)
	if !slices.Equal(others, before) {
		t.Errorf("others changed: %+v, was %+v", others, before)
	}
	second := trials.Previous(set, others)
	if first == nil || second == nil || *first != *second {
		t.Errorf("the same input gave %+v then %+v, want the same answer", first, second)
	}
}
