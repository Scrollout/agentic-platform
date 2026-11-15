package machine_test

// T088 (2) (owner, 2026-11-15; research D36, ADR 0040; data-model "An open round reused", Resume):
// after the event that left a review round open (a StepRejected or a ResultReused with
// `open_round`), resume makes that round's commit again first and goes on as the live run would:
// the fix of that round with its open findings, or at the last round the hand-over. The round is
// neither run nor reused again; its open findings are the blocking ones among the FindingRecorded
// events directly before that event. Written before the code by a separate test author. Before
// T088, resume ran an open round again. Seams: open_round_reuse_test.go (orfix and its helpers).

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/agentic-platform/engine/machine"
)

// cutAfter is the record up to and including the first event that last matches.
func cutAfter(t *testing.T, events []machine.Event, last func(machine.Event) bool) []machine.Event {
	t.Helper()
	for i, e := range events {
		if last(e) {
			return slices.Clone(events[:i+1])
		}
	}
	t.Fatalf("setup: no event to cut the record after: %v", kindsOf(events))
	return nil
}

func kindsOf(events []machine.Event) []string {
	var k []string
	for _, e := range events {
		k = append(k, e.Kind+" "+e.Step)
	}
	return k
}

// resumeOn is a restarted engine over the cut record: x's machine is rebuilt over the record and
// resumes the run.
func resumeOn(t *testing.T, x *orfix, cut []machine.Event) machine.Run {
	t.Helper()
	x.ev.events = slices.Clone(cut)
	x.clk.now = cut[len(cut)-1].At.Add(time.Minute)
	x.m = x.build()
	r, err := x.m.Resume(context.Background(), cut[0].Run)
	if err != nil {
		t.Fatalf("Resume(%s): %v", cut[0].Run, err)
	}
	return r
}

// firstHistoryOp is the first restore or commit after RunResumed, as the oplog names it.
func firstHistoryOp(t *testing.T, x *orfix) string {
	t.Helper()
	for _, op := range x.log.all() {
		if strings.HasPrefix(op, "restore ") || strings.HasPrefix(op, "commit ") {
			return op
		}
	}
	t.Fatalf("the resumed run made no History call: %v", x.log.all())
	return ""
}

func afterResumed(events []machine.Event) []machine.Event {
	i := slices.IndexFunc(events, func(e machine.Event) bool { return e.Kind == "RunResumed" })
	if i < 0 {
		return nil
	}
	return events[i+1:]
}

func isOpenReused(round int) func(machine.Event) bool {
	return func(e machine.Event) bool {
		return e.Kind == "ResultReused" && e.Step == "review" && e.Round == round
	}
}

func blockingOf(fs []machine.Finding) []machine.Finding {
	var out []machine.Finding
	for _, f := range fs {
		if f.Blocking {
			out = append(out, f)
		}
	}
	return out
}

// fixKeyOf is the reuse key of the fix of review round n in a live run.
func fixKeyOf(t *testing.T, events []machine.Event, n int) string {
	t.Helper()
	for _, e := range events {
		if e.Kind == "StepAccepted" && e.Step == "code" && e.Round == n {
			return e.ReuseKey
		}
	}
	t.Fatalf("setup: no accepted fix of round %d", n)
	return ""
}

func TestResumeAfterAReusedOpenRoundGoesOnWithTheFix(t *testing.T) {
	results := machine.NewResults()
	p := witnessPatternOf()
	one := newOR(t, p, results, round1Open)
	assertEnded(t, one.startIn("RUN-1", "proj-a"), one.ev, "done")
	two := newOR(t, p, results, round1Open)
	assertEnded(t, two.startIn("RUN-2", "proj-b"), two.ev, "done")
	cut := cutAfter(t, two.ev.events, isOpenReused(1))

	x := newOR(t, p, results, round1Open)
	r := resumeOn(t, x, cut)
	assertEnded(t, r, x.ev, "done")
	if op := firstHistoryOp(t, x); op != "commit review" {
		t.Fatalf("first History call on resume = %q, want the open round's commit again", op)
	}
	if c := x.h.commits[0]; !c.OpenRound || c.ReusedFrom != "RUN-1" || c.Round != 1 {
		t.Errorf("first commit on resume = %+v, want review round 1, open_round, reused_from RUN-1", c)
	}
	if len(x.w.calls) != 0 {
		t.Errorf("the resumed run ran %d attempts, want none (the round, the fix and round 2 reused)", len(x.w.calls))
	}
	after := afterResumed(x.ev.events)
	for _, e := range after {
		if e.Step == "review" && e.Round == 1 {
			t.Errorf("review round 1 was run or reused again after RunResumed: %s", e.Kind)
		}
	}
	fixKey := fixKeyOf(t, one.ev.events, 1)
	fi := slices.IndexFunc(after, func(e machine.Event) bool { return e.Kind == "ResultReused" && e.Step == "code" && e.Round == 1 })
	if fi < 0 || after[fi].ReuseKey != fixKey {
		t.Fatalf("the fix after resume is not reused with RUN-1's fix key %s: %v", fixKey, kindsOf(after))
	}
	want, got := settled(one.ev.events), settled(x.ev.events)
	if len(got) != len(want) {
		t.Fatalf("the resumed RUN-2 settled %v, want RUN-1's %v", got, want)
	}
	for i := range want {
		if got[i].step != want[i].step || got[i].round != want[i].round || !slices.Equal(got[i].artifacts, want[i].artifacts) {
			t.Errorf("settled %d: %v %v, want RUN-1's %v %v", i, got[i], got[i].artifacts, want[i], want[i].artifacts)
		}
	}
	validate(t, x.ev.events)
}

func TestResumeAfterAnOpenRoundGoesOnWithTheFix(t *testing.T) {
	p := witnessPatternOf()
	one := newOR(t, p, nil, round1Open)
	assertEnded(t, one.startIn("RUN-1", "proj-a"), one.ev, "done")
	cut := cutAfter(t, one.ev.events, isOpenRejected)

	x := newOR(t, p, nil, round1Open)
	r := resumeOn(t, x, cut)
	assertEnded(t, r, x.ev, "done")
	if op := firstHistoryOp(t, x); op != "commit review" {
		t.Fatalf("first History call on resume = %q, want the open round's commit again", op)
	}
	if c := x.h.commits[0]; !c.OpenRound || c.Round != 1 || c.ReusedFrom != "" {
		t.Errorf("first commit on resume = %+v, want review round 1 with open_round", c)
	}
	for _, c := range x.w.callsTo("review") {
		if c.Round == 1 {
			t.Errorf("review round 1 was asked again after RunResumed: %+v", c)
		}
	}
	fixes := x.w.callsTo("code")
	if len(fixes) == 0 {
		t.Fatalf("no fix ran after resume: %v", kindsOf(x.ev.events))
	}
	if want := blockingOf(round1Open(1)); fixes[0].Round != 1 || !findingsEqual(fixes[0].Open, want) {
		t.Errorf("the fix's request: round %d open %+v, want round 1 with exactly round 1's blocking findings %+v",
			fixes[0].Round, fixes[0].Open, want)
	}
	validate(t, x.ev.events)
}

func TestResumeDropsAnUnfinishedRoundsFindings(t *testing.T) {
	two := func(round int) []machine.Finding {
		if round != 1 {
			return nil
		}
		a := blocking("R-1", "code/f.go", 1)
		a.Witness = &machine.Witness{File: witnessPath, Test: witnessName}
		b := blocking("R-2", "code/f.go", 2)
		b.Witness = &machine.Witness{File: "review/r2/other_test.go", Test: "TestR2Other"}
		return []machine.Finding{a, b}
	}
	results := machine.NewResults()
	p := witnessPatternOf()
	one := newOR(t, p, results, two)
	assertEnded(t, one.startIn("RUN-1", "proj-a"), one.ev, "done")
	rep := newOR(t, p, results, two)
	assertEnded(t, rep.startIn("RUN-2", "proj-b"), rep.ev, "done")
	// RUN-2 stopped after one of the reused round's two findings was recorded.
	cut := cutAfter(t, rep.ev.events, func(e machine.Event) bool {
		return e.Kind == "FindingRecorded" && e.Step == "review" && e.Round == 1 && e.Attempt == 0
	})

	// The restarted engine keeps every result but the fix's, so the fix runs and shows its request.
	fixKey := fixKeyOf(t, one.ev.events, 1)
	partial := machine.NewResults()
	for _, e := range one.ev.events {
		if e.ReuseKey == "" || e.ReuseKey == fixKey {
			continue
		}
		if a, ok := results.Get(e.ReuseKey); ok {
			partial.Put(a)
		}
	}
	x := newOR(t, p, partial, two)
	r := resumeOn(t, x, cut)
	assertEnded(t, r, x.ev, "done")
	after := afterResumed(x.ev.events)
	oi := slices.IndexFunc(after, isOpenReused(1))
	if oi < 0 {
		t.Fatalf("review round 1 was not reused again after RunResumed: %v", kindsOf(after))
	}
	if !after[oi].OpenRound {
		t.Errorf("review round 1's ResultReused after resume lacks open_round")
	}
	var recorded []machine.Finding
	for _, e := range after[:oi] {
		if e.Kind == "FindingRecorded" && e.Finding != nil {
			recorded = append(recorded, *e.Finding)
		}
	}
	if !findingsEqual(recorded, two(1)) {
		t.Errorf("findings recorded again before the reused round = %+v, want both %+v", recorded, two(1))
	}
	fixes := x.w.callsTo("code")
	if len(fixes) != 1 {
		t.Fatalf("code ran %d times after resume, want the fix once", len(fixes))
	}
	if want := blockingOf(two(1)); fixes[0].Round != 1 || !findingsEqual(fixes[0].Open, want) {
		t.Errorf("the fix's open findings = %+v, want each blocking finding once: %+v", fixes[0].Open, want)
	}
	for _, c := range x.w.callsTo("review") {
		if c.Round == 1 {
			t.Errorf("review round 1 was run after resume")
		}
	}
	validate(t, x.ev.events)
}

func TestResumeAfterTheLastOpenRoundHandsOver(t *testing.T) {
	p := witnessPatternOf()
	p.ReviewRounds = 2
	last := p.ReviewRounds

	check := func(t *testing.T, one *orfix, x *orfix, r machine.Run, reusedFrom string) {
		t.Helper()
		if op := firstHistoryOp(t, x); op != "commit review" {
			t.Fatalf("first History call on resume = %q, want round %d's commit again", op, last)
		}
		if c := x.h.commits[0]; !c.OpenRound || c.Round != last || c.ReusedFrom != reusedFrom {
			t.Errorf("first commit on resume = %+v, want review round %d with open_round, reused_from %q", c, last, reusedFrom)
		}
		if len(x.w.calls) != 0 {
			t.Errorf("a review or fix request followed the last open round: %+v", x.w.calls)
		}
		if r.Status != machine.StatusWaiting || x.ev.count("RunHandedOver") != 1 {
			t.Fatalf("resumed run = %s (%s), want handed over: %v", r.Status, r.Reason, kindsOf(x.ev.events))
		}
		want := blockingOf(everyRoundOpen(last))
		if !findingsEqual(r.Open, want) {
			t.Errorf("open findings = %+v, want round %d's %+v", r.Open, last, want)
		}
		r1, _ := one.m.Get("RUN-1")
		if r.Reason != r1.Reason {
			t.Errorf("reason = %q, want the live hand-over's %q", r.Reason, r1.Reason)
		}
		validate(t, x.ev.events)
	}

	t.Run("after its open StepRejected", func(t *testing.T) {
		one := newOR(t, p, nil, everyRoundOpen)
		if r := one.startIn("RUN-1", "proj-a"); r.Status != machine.StatusWaiting {
			t.Fatalf("setup: RUN-1 = %s, want handed over", r.Status)
		}
		cut := cutAfter(t, one.ev.events, func(e machine.Event) bool { return isOpenRejected(e) && e.Round == last })
		x := newOR(t, p, nil, everyRoundOpen)
		check(t, one, x, resumeOn(t, x, cut), "")
	})
	t.Run("after its open ResultReused", func(t *testing.T) {
		results := machine.NewResults()
		one := newOR(t, p, results, everyRoundOpen)
		if r := one.startIn("RUN-1", "proj-a"); r.Status != machine.StatusWaiting {
			t.Fatalf("setup: RUN-1 = %s, want handed over", r.Status)
		}
		rep := newOR(t, p, results, everyRoundOpen)
		if r := rep.startIn("RUN-2", "proj-b"); r.Status != machine.StatusWaiting {
			t.Fatalf("setup: RUN-2 = %s, want handed over", r.Status)
		}
		cut := cutAfter(t, rep.ev.events, isOpenReused(last))
		x := newOR(t, p, results, everyRoundOpen)
		check(t, one, x, resumeOn(t, x, cut), "RUN-1")
	})
}
