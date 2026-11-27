package trials_test

import (
	"testing"
	"time"

	"github.com/agentic-platform/engine/machine"
	"github.com/agentic-platform/engine/trials"
)

var pattern = []string{"implement", "review"}

var t0 = time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)

// record plants a run record: RunStarted, one StepAccepted per method (a step event carrying
// that method), then the given end event kind (none when end is "").
func record(run string, methods []string, end string) []machine.Event {
	ev := []machine.Event{{Seq: 1, At: t0, Run: run, Slice: "S-1", Kind: "RunStarted", Pattern: "base"}}
	for i, m := range methods {
		ev = append(ev, machine.Event{Seq: len(ev) + 1, At: t0.Add(time.Duration(i+1) * time.Minute), Run: run,
			Kind: "StepAccepted", Step: pattern[i%len(pattern)], Method: m})
	}
	if end != "" {
		ev = append(ev, machine.Event{Seq: len(ev) + 1, At: t0.Add(time.Hour), Run: run, Kind: end})
	}
	return ev
}

func trial(n int, end string, methods ...string) trials.Trial {
	run := "R-" + string(rune('0'+n)) //nolint:gosec // n is a small trial number
	return trials.Trial{Number: n, Run: run, Events: record(run, methods, end)}
}

func TestOfCounts(t *testing.T) {
	cases := []struct {
		name        string
		ts          []trials.Trial
		ended, done int
		unstable    bool
	}{
		{"done done handed-over is unstable", []trials.Trial{trial(1, "RunCompleted"), trial(2, "RunCompleted"), trial(3, "HandOverAccepted")}, 3, 2, true},
		{"all done is not unstable", []trials.Trial{trial(1, "RunCompleted"), trial(2, "RunCompleted"), trial(3, "RunCompleted")}, 3, 3, false},
		{"none done is not unstable", []trials.Trial{trial(1, "HandOverAccepted"), trial(2, "RunExpired"), trial(3, "RunCancelled")}, 3, 0, false},
		{"two of three ended count only the ended", []trials.Trial{trial(1, "RunCompleted"), trial(2, "HandOverAccepted"), trial(3, "")}, 2, 1, true},
		{"cancelled is ended and not done", []trials.Trial{trial(1, "RunCompleted"), trial(2, "RunCancelled")}, 2, 1, true},
		{"expired is not done", []trials.Trial{trial(1, "RunCompleted"), trial(2, "RunExpired")}, 2, 1, true},
		{"k=1 done is not unstable", []trials.Trial{trial(1, "RunCompleted")}, 1, 1, false},
		{"k=1 handed-over is not unstable", []trials.Trial{trial(1, "HandOverAccepted")}, 1, 0, false},
		{"no trials", nil, 0, 0, false},
		{"started with nothing recorded is not ended", []trials.Trial{{Number: 1, Run: "R-1"}}, 0, 0, false},
		{"done plus a trial with nothing recorded: one ended, not unstable", []trials.Trial{trial(1, "RunCompleted"), {Number: 2, Run: "R-2"}}, 1, 1, false},
		{"a running trial is not ended", []trials.Trial{trial(1, "RunCompleted"), trial(2, "", "m")}, 1, 1, false},
		{"a waiting trial is not ended", []trials.Trial{trial(1, "RunCompleted"), trial(2, "RunHandedOver")}, 1, 1, false},
		{"an interrupted trial is not ended", []trials.Trial{trial(1, "HandOverAccepted"), trial(2, "RunInterrupted")}, 1, 0, false},
		{"two done, one waiting: not unstable", []trials.Trial{trial(1, "RunCompleted"), trial(2, "RunHandedOver"), trial(3, "RunCompleted")}, 2, 2, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := trials.Of(pattern, c.ts)
			if r.Ended != c.ended || r.Done != c.done || r.Unstable != c.unstable {
				t.Errorf("ended %d done %d unstable %v, want %d %d %v", r.Ended, r.Done, r.Unstable, c.ended, c.done, c.unstable)
			}
		})
	}
}

func TestOfOutcomes(t *testing.T) {
	ts := []trials.Trial{
		trial(1, "RunCompleted", "m1"),
		trial(2, "HandOverAccepted", "m1"),
		trial(3, "RunExpired", "m1"),
		trial(4, "RunCancelled"),
		trial(5, "RunHandedOver", "m1"),
		{Number: 6, Run: "R-6"},
	}
	r := trials.Of(pattern, ts)
	want := []struct {
		out    machine.Outcome
		method string
	}{
		{machine.OutcomeDone, "m1"}, {machine.OutcomeHandedOver, "m1"}, {machine.OutcomeExpired, "m1"},
		{machine.OutcomeCancelled, ""}, {"", "m1"}, {"", ""},
	}
	if len(r.Outcomes) != len(ts) {
		t.Fatalf("%d outcomes, want one per started trial (%d)", len(r.Outcomes), len(ts))
	}
	for i, w := range want {
		o := r.Outcomes[i]
		if o.Trial != i+1 || o.Run != ts[i].Run || o.Outcome != w.out || o.Method != w.method {
			t.Errorf("outcome %d = %+v, want trial %d run %s outcome %q method %q", i, o, i+1, ts[i].Run, w.out, w.method)
		}
	}
}

func TestOfOutcomesKeepGivenOrder(t *testing.T) {
	// Trials are given in trial order; the outcomes keep it, whatever the run IDs or outcomes.
	ts := []trials.Trial{
		{Number: 1, Run: "R-9", Events: record("R-9", nil, "RunCancelled")},
		{Number: 2, Run: "R-10", Events: record("R-10", nil, "RunCompleted")},
		{Number: 3, Run: "R-2", Events: record("R-2", nil, "HandOverAccepted")},
	}
	r := trials.Of(pattern, ts)
	if len(r.Outcomes) != 3 {
		t.Fatalf("%d outcomes, want 3", len(r.Outcomes))
	}
	for i, wantRun := range []string{"R-9", "R-10", "R-2"} {
		if r.Outcomes[i].Run != wantRun || r.Outcomes[i].Trial != i+1 {
			t.Errorf("outcome %d = %+v, want run %s", i, r.Outcomes[i], wantRun)
		}
	}
}

func TestOfMethod(t *testing.T) {
	cases := []struct {
		name string
		t    trials.Trial
		want string
	}{
		{"the last method when a trial's steps differ", trial(1, "RunCompleted", "m1", "m2"), "m2"},
		{"an event without a method does not clear it", trial(1, "RunCompleted", "m1", ""), "m1"},
		{"a method on a still-running trial is shown", trial(1, "", "m1"), "m1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := trials.Of(pattern, []trials.Trial{c.t})
			if len(r.Outcomes) != 1 || r.Outcomes[0].Method != c.want {
				t.Errorf("outcomes %+v, want method %q", r.Outcomes, c.want)
			}
		})
	}
}

func TestOfMethodsMixed(t *testing.T) {
	cases := []struct {
		name  string
		ts    []trials.Trial
		mixed bool
	}{
		{"two methods", []trials.Trial{trial(1, "RunCompleted", "m1"), trial(2, "RunCompleted", "m2")}, true},
		{"one method", []trials.Trial{trial(1, "RunCompleted", "m1"), trial(2, "HandOverAccepted", "m1")}, false},
		{"one trial", []trials.Trial{trial(1, "RunCompleted", "m1")}, false},
		{"a trial without a method is not a second method", []trials.Trial{trial(1, "RunCompleted", "m1"), trial(2, "RunCancelled")}, false},
		{"no trial has a method", []trials.Trial{trial(1, "RunCancelled"), trial(2, "RunCancelled")}, false},
		{"a trial with nothing recorded is not a second method", []trials.Trial{trial(1, "RunCompleted", "m1"), {Number: 2, Run: "R-2"}}, false},
		{"mixed with a method-less trial between", []trials.Trial{trial(1, "RunCompleted", "m1"), trial(2, "RunCancelled"), trial(3, "RunCompleted", "m2")}, true},
		{"a still-running trial under another method counts", []trials.Trial{trial(1, "RunCompleted", "m1"), trial(2, "", "m2")}, true},
		{"no trials", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := trials.Of(pattern, c.ts).MethodsMixed; got != c.mixed {
				t.Errorf("MethodsMixed %v, want %v", got, c.mixed)
			}
		})
	}
}

func TestOfNoTrialsHasNoOutcomes(t *testing.T) {
	if n := len(trials.Of(pattern, nil).Outcomes); n != 0 {
		t.Errorf("%d outcomes for no trials", n)
	}
}
