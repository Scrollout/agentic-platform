package machine_test

// T085 (owner, 2026-11-14): when the engine starts and finds a run whose last event is not an end,
// a hand-over or RunInterrupted, it appends RunInterrupted with the reason "the platform stopped
// during the run", stamped with the last event's time, so the downtime counts toward neither the
// run's time budget nor its expiry (data-model, Run). Written before the code by a separate
// author. The machine reads a record when it is first asked about a run (Get, here), whether the
// sink also lists its runs or only reads them; a test never relies on the append happening before
// that first call.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/agentic-platform/engine/machine"
)

const stoppedReason = "the platform stopped during the run"

// recordOfARun runs RUN-1 to the end on a first machine, each step taking step[name] on the clock,
// and returns its events.
func recordOfARun(t *testing.T, took map[string]time.Duration) []machine.Event {
	t.Helper()
	f := newFixture(t, pattern())
	f.c.fn = noBlocking
	for _, s := range pattern().Steps {
		f.w.fn[s.Name] = func(r machine.Request) (machine.Result, error) {
			f.clk.Advance(took[r.Step.Name])
			return ok(r), nil
		}
	}
	if r := f.start("RUN-1"); r.Status != machine.StatusEnded {
		t.Fatalf("setup: %s", r.Status)
	}
	return append([]machine.Event(nil), f.ev.events...)
}

func firstIndex(t *testing.T, events []machine.Event, kind, step string) int {
	t.Helper()
	for i, e := range events {
		if e.Kind == kind && e.Step == step {
			return i
		}
	}
	t.Fatalf("no %s event of step %q", kind, step)
	return -1
}

func restartedOver(events []machine.Event, now time.Time, lister bool) (*machine.Machine, *sink) {
	s := &sink{events: append([]machine.Event(nil), events...)}
	var ev machine.Events = recSink{s}
	if lister {
		ev = listSink{recSink{s}}
	}
	m := machine.New(machine.Config{Pattern: pattern(), Worker: &worker{}, Checker: &checker{fn: noBlocking},
		Clock: &clock{now: now}, Events: ev, Results: machine.NewResults(), Model: "claude-haiku-5-5"})
	return m, s
}

func TestRestartAppendsOneRunInterruptedStampedWithTheLastEventsTime(t *testing.T) {
	full := recordOfARun(t, map[string]time.Duration{"tests": time.Minute, "code": time.Minute})
	cuts := map[string]int{
		"after RunStarted":      0,
		"after a StepAccepted":  firstIndex(t, full, "StepAccepted", "tests"),
		"mid-step, StepStarted": firstIndex(t, full, "StepStarted", "code"),
		"after an AttemptMade":  firstIndex(t, full, "AttemptMade", "code"),
		"after a CheckPassed":   firstIndex(t, full, "CheckPassed", "code"),
	}
	for name, cut := range cuts {
		for _, lister := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/lister=%v", name, lister), func(t *testing.T) {
				prefix := full[:cut+1]
				last := prefix[len(prefix)-1]
				m, s := restartedOver(prefix, last.At.Add(5*time.Hour), lister)
				r, found := m.Get("RUN-1")
				if !found || r.Status != machine.StatusInterrupted || r.Reason != stoppedReason {
					t.Fatalf("Get = %+v found=%v, want interrupted with the reason %q", r, found, stoppedReason)
				}
				if len(s.events) != len(prefix)+1 {
					t.Fatalf("want exactly one event appended, the record holds %d for %d before: %v", len(s.events), len(prefix), s.kinds())
				}
				e := s.events[len(prefix)]
				if e.Kind != "RunInterrupted" || e.Detail != stoppedReason || e.Run != "RUN-1" || e.Seq != len(prefix)+1 || len(e.Refs) == 0 {
					t.Fatalf("appended %+v, want RunInterrupted seq %d with the reason, run and refs", e, len(prefix)+1)
				}
				if !e.At.Equal(last.At) {
					t.Errorf("the event is stamped %s, want the last event's time %s", e.At, last.At)
				}
				if want := last.At.Add(machine.HandOverTTL); !r.ExpiresAt.Equal(want) {
					t.Errorf("expires_at = %s, want the last event's time + 14 days = %s", r.ExpiresAt, want)
				}
				validate(t, s.events)

				// A second start appends nothing more, and a third neither.
				for i := 0; i < 2; i++ {
					m2, _ := restartedOnSink(s, last.At.Add(48*time.Hour), lister)
					r2, _ := m2.Get("RUN-1")
					if len(s.events) != len(prefix)+1 || r2.Status != machine.StatusInterrupted ||
						!r2.ExpiresAt.Equal(r.ExpiresAt) {
						t.Fatalf("restart %d: %d events (want %d), %+v", i+2, len(s.events), len(prefix)+1, r2)
					}
				}
			})
		}
	}
}

func restartedOnSink(s *sink, now time.Time, lister bool) (*machine.Machine, *sink) {
	return restartedOver(s.events, now, lister)
}

func TestRestartedRunsEndedWaitingOrInterruptedGetNoEvent(t *testing.T) {
	// A hand-over, an interruption, and every end.
	handed := newFixture(t, pattern())
	handed.failChecks("code")
	handed.start("RUN-1")
	interrupted := newFixture(t, pattern())
	interrupted.w.fn["code"] = func(machine.Request) (machine.Result, error) { return machine.Result{}, context.DeadlineExceeded }
	interrupted.start("RUN-1")
	cancelled := newFixture(t, pattern())
	cancelled.failChecks("code")
	cancelled.start("RUN-1")
	cancelled.m.Cancel("RUN-1")
	accepted := newFixture(t, pattern())
	accepted.failChecks("code")
	accepted.start("RUN-1")
	accepted.m.AcceptHandOver("RUN-1")
	expired := newFixture(t, pattern())
	expired.failChecks("code")
	expired.start("RUN-1")
	expired.clk.Advance(machine.HandOverTTL)
	expired.m.ExpireDue()
	done := newFixture(t, pattern())
	done.c.fn = noBlocking
	done.start("RUN-1")
	want := map[string]struct {
		f    *fixture
		last string
	}{
		"handed over": {handed, "RunHandedOver"}, "interrupted": {interrupted, "RunInterrupted"},
		"cancelled": {cancelled, "RunCancelled"}, "hand-over accepted": {accepted, "HandOverAccepted"},
		"expired": {expired, "RunExpired"}, "completed": {done, "RunCompleted"},
	}
	for name, c := range want {
		t.Run(name, func(t *testing.T) {
			events := c.f.ev.events
			if got := events[len(events)-1].Kind; got != c.last {
				t.Fatalf("setup: last event %s, want %s", got, c.last)
			}
			m, s := restartedOver(events, c.f.clk.now.Add(time.Hour), true)
			m.Get("RUN-1")
			_ = m.ExpireDue()
			if len(s.events) != len(events) {
				t.Fatalf("a run whose last event is %s got %v", c.last, s.kinds()[len(events):])
			}
		})
	}
}

func TestDowntimeCountsTowardNeitherTheTimeBudgetNorTheExpiry(t *testing.T) {
	// 24 of the run's 30 minutes were used before the stop; the code step takes 4 more after the
	// restart: 28 minutes, within the budget, though the platform was down for 5 hours.
	took := map[string]time.Duration{"contract-check": 8 * time.Minute, "generate": 8 * time.Minute, "tests": 8 * time.Minute, "code": 4 * time.Minute}
	full := recordOfARun(t, took)
	prefix := full[:firstIndex(t, full, "StepAccepted", "tests")+1]
	last := prefix[len(prefix)-1]
	down := last.At.Add(5 * time.Hour)
	m, s := restartedOver(prefix, down, true)
	clk := &clock{now: down}
	w := &worker{fn: map[string]func(machine.Request) (machine.Result, error){
		"code": func(r machine.Request) (machine.Result, error) { clk.Advance(took["code"]); return ok(r), nil },
	}}
	m = machine.New(machine.Config{Pattern: pattern(), Worker: w, Checker: &checker{fn: noBlocking}, Clock: clk,
		Events: listSink{recSink{s}}, Results: machine.NewResults(), Model: "claude-haiku-5-5"})
	r, err := m.Resume(context.Background(), "RUN-1")
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != machine.StatusEnded || r.Outcome != machine.OutcomeDone {
		t.Fatalf("run = %s/%s (%s); the 5 hours down must not count toward the 30 minute budget; kinds %v",
			r.Status, r.Outcome, r.Reason, s.kinds())
	}
	var interrupted, resumed int
	for i, e := range s.events {
		switch e.Kind {
		case "RunInterrupted":
			interrupted++
			if !e.At.Equal(last.At) {
				t.Errorf("event %d RunInterrupted stamped %s, want %s", i+1, e.At, last.At)
			}
		case "RunResumed":
			resumed++
		}
	}
	if interrupted != 1 || resumed != 1 {
		t.Errorf("want one RunInterrupted and one RunResumed, got %d and %d: %v", interrupted, resumed, s.kinds())
	}
	validate(t, s.events)
}

func TestExpiryCountsFromTheLastEventNotFromTheRestart(t *testing.T) {
	full := recordOfARun(t, map[string]time.Duration{"tests": time.Minute})
	prefix := full[:firstIndex(t, full, "StepAccepted", "tests")+1]
	last := prefix[len(prefix)-1]
	// The platform restarts 13 days and 23 hours after the last event.
	m, s := restartedOver(prefix, last.At.Add(14*24*time.Hour-time.Hour), true)
	if r, _ := m.Get("RUN-1"); r.Status != machine.StatusInterrupted {
		t.Fatalf("status %s, want interrupted", r.Status)
	}
	_ = m.ExpireDue()
	if r, _ := m.Get("RUN-1"); r.Status != machine.StatusInterrupted {
		t.Fatalf("expired early: %s", r.Status)
	}
	// An hour later (14 days after the last event) the run expires, not 14 days after the restart.
	m = machine.New(machine.Config{Pattern: pattern(), Worker: &worker{}, Checker: &checker{fn: noBlocking},
		Clock: &clock{now: last.At.Add(14 * 24 * time.Hour)}, Events: listSink{recSink{s}}, Results: machine.NewResults(), Model: "claude-haiku-5-5"})
	_ = m.ExpireDue()
	r, _ := m.Get("RUN-1")
	if r.Status != machine.StatusEnded || r.Outcome != machine.OutcomeExpired {
		t.Fatalf("run = %s/%s, want ended/expired 14 days after the last event", r.Status, r.Outcome)
	}
}
