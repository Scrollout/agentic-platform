package machine_test

// T043: failing tests for the run state machine (T051). The package does not exist yet.
//
// Interface assumed (smallest the tests need; names follow data-model.md and the contract):
//
//	machine.New(machine.Config{Pattern, Worker, Checker, Clock, Events}) *Machine
//	  Pattern: ID, Version, Attempts, ReviewRounds, RunBudget, StepBudget (Budget{Tokens, CostUSD,
//	    Duration}), Steps []Step{Name, Worker, AgentDigest, Inputs, Outputs, Checks []string}.
//	    A test builds it in code; reading pattern.yaml is not the machine's job.
//	  Worker.Run(ctx, Request) (Result, error): runs one attempt (or one review round) of a step.
//	    Request{Run, Step, Attempt, Round, Open []Finding, ChangedLines []Line, Inputs []string}.
//	    Result{Model, Outputs map[name]hash, Usage{Tokens, CostUSD}, Done bool (the agent's claim),
//	    Findings []Finding, ChangedLines []Line}. An error is a failed attempt.
//	  Checker.Check(ctx, CheckRequest{Run, Step, Attempt, Name, Result}) (CheckResult{Passed,
//	    Reason, Refs}, error). An error counts as a failed check.
//	  Clock.Now() time.Time. Events.Append(Event) error. Event marshals to run-event.schema.json.
//	machine.Start(ctx, StartParams{Run, Slice, Inputs map[name]hash, Refs}) (Run, error)
//	  runs synchronously until the run ends or waits for the developer. Start, Cancel and Get may be
//	  called from inside a Worker (the machine holds no lock while a worker runs).
//	  Errors: ErrRunActive, ErrNotWaiting, ErrRunEnded.
//	machine.Get(id) (Run, bool); AcceptHandOver(id) (Run, error); Cancel(id) (Run, error);
//	  ExpireDue() ends every waiting or interrupted run whose ExpiresAt is not after Clock.Now().
//	Run{ID, Status, Outcome, Reason, ExpiresAt, Accepted []step names, Open []Finding}.
//
// Review: the review step runs in rounds. A round with an open blocking finding (the fake
// NoBlockingFindingOpen check fails) makes the machine run the code step again with the open
// findings, then the next review round. Round 2 and later get only the open (blocking) findings of
// the previous round and the lines the fix changed.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/agentic-platform/engine/machine"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

var t0 = time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)

func hash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

type clock struct{ now time.Time }

func (c *clock) Now() time.Time          { return c.now }
func (c *clock) Advance(d time.Duration) { c.now = c.now.Add(d) }

type sink struct{ events []machine.Event }

func (s *sink) Append(e machine.Event) error { s.events = append(s.events, e); return nil }

func (s *sink) kinds() []string {
	var k []string
	for _, e := range s.events {
		k = append(k, e.Kind)
	}
	return k
}

func (s *sink) count(kind string) int {
	n := 0
	for _, e := range s.events {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

type worker struct {
	fn    map[string]func(machine.Request) (machine.Result, error)
	calls []machine.Request
}

func (w *worker) Run(_ context.Context, r machine.Request) (machine.Result, error) {
	w.calls = append(w.calls, r)
	if f := w.fn[r.Step.Name]; f != nil {
		return f(r)
	}
	return ok(r), nil
}

func (w *worker) callsTo(step string) []machine.Request {
	var out []machine.Request
	for _, c := range w.calls {
		if c.Step.Name == step {
			out = append(out, c)
		}
	}
	return out
}

func ok(r machine.Request) machine.Result {
	out := map[string]string{}
	for _, o := range r.Step.Outputs {
		out[o] = hash(fmt.Sprintf("%s/%s/%d", r.Step.Name, o, r.Attempt))
	}
	res := machine.Result{Outputs: out}
	if strings.HasPrefix(r.Step.Worker, "agent:") {
		res.Model = "claude-haiku-5-5"
	}
	return res
}

type checker struct {
	fn    func(machine.CheckRequest) (machine.CheckResult, error)
	calls []machine.CheckRequest
}

func (c *checker) Check(_ context.Context, r machine.CheckRequest) (machine.CheckResult, error) {
	c.calls = append(c.calls, r)
	if c.fn != nil {
		return c.fn(r)
	}
	return pass(r), nil
}

func pass(r machine.CheckRequest) machine.CheckResult {
	return machine.CheckResult{Passed: true, Reason: r.Name + " holds"}
}

func fail(r machine.CheckRequest) machine.CheckResult {
	return machine.CheckResult{Passed: false, Reason: r.Name + " does not hold"}
}

const digest = "1111111111111111111111111111111111111111111111111111111111111111"

func pattern() machine.Pattern {
	return machine.Pattern{
		ID: "api-feature-go", Version: "1.0.0", Attempts: 3, ReviewRounds: 3,
		RunBudget:  machine.Budget{Tokens: 3000000, CostUSD: 0.50, Duration: 30 * time.Minute},
		StepBudget: machine.Budget{Tokens: 1000000, CostUSD: 0.20, Duration: 10 * time.Minute},
		Steps: []machine.Step{
			{Name: "contract-check", Worker: "procedure:contract-check", Inputs: []string{"slice/contract", "slice/rules"},
				Outputs: []string{"contract", "examples"}, Checks: []string{"ContractMatchesApproval", "ScopeKept"}},
			{Name: "generate", Worker: "procedure:generate", Inputs: []string{"contract"},
				Outputs: []string{"generated-code"}, Checks: []string{"GeneratedCodeBuilds", "ScopeKept"}},
			{Name: "tests", Worker: "agent:tests", AgentDigest: digest,
				Inputs:  []string{"contract", "examples", "generated-code", "slice/rules"},
				Outputs: []string{"tests"}, Checks: []string{"TestsBuild", "TestsFailAgainstStub"}},
			{Name: "code", Worker: "agent:code", AgentDigest: digest,
				Inputs:  []string{"contract", "examples", "generated-code", "tests", "slice/rules"},
				Outputs: []string{"code"}, Checks: []string{"ScopeKept", "AllTestsPass"}},
			{Name: "review", Worker: "agent:review", AgentDigest: digest,
				Inputs:  []string{"contract", "examples", "generated-code", "tests", "code", "slice/rules"},
				Outputs: []string{"findings"}, Checks: []string{"FindingsCarryEvidence", "NoBlockingFindingOpen"}},
		},
	}
}

type fixture struct {
	t   *testing.T
	m   *machine.Machine
	w   *worker
	c   *checker
	clk *clock
	ev  *sink
}

func newFixture(t *testing.T, p machine.Pattern) *fixture {
	t.Helper()
	f := &fixture{t: t, w: &worker{fn: map[string]func(machine.Request) (machine.Result, error){}},
		c: &checker{}, clk: &clock{now: t0}, ev: &sink{}}
	f.m = machine.New(machine.Config{Pattern: p, Worker: f.w, Checker: f.c, Clock: f.clk, Events: f.ev})
	return f
}

func (f *fixture) start(id string) machine.Run {
	f.t.Helper()
	r, err := f.m.Start(context.Background(), f.params(id))
	if err != nil {
		f.t.Fatalf("Start(%s): %v", id, err)
	}
	return r
}

func (f *fixture) params(id string) machine.StartParams {
	return machine.StartParams{Run: id, Slice: "S-1",
		Inputs: map[string]string{"slice/contract": hash("contract"), "slice/rules": hash("rules")},
		Refs:   []string{"G-1", "R-1", "R-1/E-1", "contract/getThing"}}
}

// failStep makes every check of the named step fail.
func (f *fixture) failChecks(step string) {
	f.c.fn = func(r machine.CheckRequest) (machine.CheckResult, error) {
		if r.Step.Name == step {
			return fail(r), nil
		}
		return pass(r), nil
	}
}

func blocking(rule, file string, line int) machine.Finding {
	return machine.Finding{Rule: rule, File: file, Line: line, Text: "wrong status", Blocking: true, Check: "AllTestsPass",
		Witness: &machine.Witness{File: "review/r1/x_test.go", Test: "TestR1X"}}
}

// noBlocking is the fake NoBlockingFindingOpen check.
func noBlocking(r machine.CheckRequest) (machine.CheckResult, error) {
	if r.Name == "NoBlockingFindingOpen" {
		for _, f := range r.Result.Findings {
			if f.Blocking {
				res := fail(r)
				for i, g := range r.Result.Findings { // T096: it confirms the blocking findings with a witness
					if g.Blocking && g.Witness != nil {
						res.Confirmed = append(res.Confirmed, i)
					}
				}
				return res, nil
			}
		}
	}
	return pass(r), nil
}

var terminal = map[string]string{
	"done": "RunCompleted", "handed-over": "HandOverAccepted", "expired": "RunExpired", "cancelled": "RunCancelled",
}

// assertEnded checks: one outcome from the contract's list, one terminal event, last in the record.
func assertEnded(t *testing.T, r machine.Run, ev *sink, outcome string) {
	t.Helper()
	if r.Status != machine.StatusEnded || string(r.Outcome) != outcome {
		t.Fatalf("run = %s/%s, want ended/%s", r.Status, r.Outcome, outcome)
	}
	n := 0
	for _, k := range terminal {
		n += ev.count(k)
	}
	last := ev.events[len(ev.events)-1].Kind
	if n != 1 || last != terminal[outcome] {
		t.Fatalf("want exactly one terminal event %s, last; got %d, last %s (%v)", terminal[outcome], n, last, ev.kinds())
	}
}

func schema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	dir, err := filepath.Abs("../api/preview/2026-11-12-preview")
	if err != nil {
		t.Fatal(err)
	}
	u := (&url.URL{Scheme: "file", Path: filepath.ToSlash(dir) + "/run-event.schema.json"}).String()
	s, err := jsonschema.NewCompiler().Compile(u)
	if err != nil {
		t.Fatalf("compile run-event schema: %v", err)
	}
	return s
}

func validate(t *testing.T, events []machine.Event) {
	t.Helper()
	s := schema(t)
	for i, e := range events {
		b, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		v, err := jsonschema.UnmarshalJSON(strings.NewReader(string(b)))
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Validate(v); err != nil {
			t.Fatalf("event %d (%s) is not a valid run event: %v\n%s", i+1, e.Kind, err, b)
		}
		if e.Seq != i+1 {
			t.Fatalf("event %d has seq %d, want %d (no gaps)", i+1, e.Seq, i+1)
		}
	}
}

// --- accepted only when all checks pass -------------------------------------------------------

func TestHappyPathAcceptsEveryStepAndEndsDone(t *testing.T) {
	f := newFixture(t, pattern())
	f.c.fn = noBlocking
	r := f.start("RUN-1")
	assertEnded(t, r, f.ev, "done")
	if !slices.Equal(r.Accepted, []string{"contract-check", "generate", "tests", "code", "review"}) {
		t.Fatalf("accepted = %v", r.Accepted)
	}
	if f.ev.kinds()[0] != "RunStarted" || f.ev.count("StepAccepted") != 5 {
		t.Fatalf("events = %v", f.ev.kinds())
	}
	validate(t, f.ev.events)
}

func TestStepNotAcceptedWhenOneCheckFails(t *testing.T) {
	f := newFixture(t, pattern())
	f.c.fn = func(r machine.CheckRequest) (machine.CheckResult, error) {
		if r.Step.Name == "tests" && r.Name == "TestsFailAgainstStub" {
			return fail(r), nil
		}
		return pass(r), nil
	}
	f.start("RUN-1")
	for _, e := range f.ev.events {
		if e.Kind == "StepAccepted" && e.Step == "tests" {
			t.Fatal("tests accepted although one check failed")
		}
	}
	if got := len(f.w.callsTo("code")); got != 0 {
		t.Fatalf("code ran %d times after tests failed", got)
	}
	// every check of the attempt still ran and was recorded
	if f.ev.count("CheckPassed") < 1 || f.ev.count("CheckFailed") != 3 {
		t.Fatalf("events = %v", f.ev.kinds())
	}
}

func TestAgentDoneIsIgnored(t *testing.T) {
	f := newFixture(t, pattern())
	f.w.fn["code"] = func(r machine.Request) (machine.Result, error) {
		res := ok(r)
		res.Done = true // the agent says it is done
		return res, nil
	}
	f.failChecks("code")
	f.start("RUN-1")
	if f.ev.count("StepAccepted") != 3 { // contract-check, generate, tests only
		t.Fatalf("DONE counted: %v", f.ev.kinds())
	}
	// and without DONE a step whose checks pass is accepted
	g := newFixture(t, pattern())
	g.c.fn = noBlocking
	g.w.fn["code"] = func(r machine.Request) (machine.Result, error) { res := ok(r); res.Done = false; return res, nil }
	assertEnded(t, g.start("RUN-1"), g.ev, "done")
}

func TestCheckThatErrorsCountsAsFailed(t *testing.T) {
	f := newFixture(t, pattern())
	f.c.fn = func(r machine.CheckRequest) (machine.CheckResult, error) {
		if r.Step.Name == "generate" && r.Name == "GeneratedCodeBuilds" {
			return machine.CheckResult{}, errors.New("check container unavailable")
		}
		return pass(r), nil
	}
	run := f.start("RUN-1")
	if run.Status != machine.StatusWaiting {
		t.Fatalf("status = %s", run.Status)
	}
	if f.ev.count("StepAccepted") != 1 { // only contract-check
		t.Fatalf("events = %v", f.ev.kinds())
	}
	for _, e := range f.ev.events {
		if e.Kind == "CheckFailed" && e.Check.Name == "GeneratedCodeBuilds" {
			if e.Check.Passed || e.Check.Reason == "" {
				t.Fatalf("errored check = %+v, want failed with a reason", e.Check)
			}
			return
		}
	}
	t.Fatal("no CheckFailed for the check that errored")
}

func TestWorkerErrorIsARejectedAttempt(t *testing.T) {
	f := newFixture(t, pattern())
	n := 0
	f.w.fn["tests"] = func(r machine.Request) (machine.Result, error) {
		n++
		if n == 1 {
			return machine.Result{}, errors.New("model unavailable")
		}
		return ok(r), nil
	}
	f.c.fn = noBlocking
	assertEnded(t, f.start("RUN-1"), f.ev, "done")
	if got := len(f.w.callsTo("tests")); got != 2 || f.w.callsTo("tests")[1].Attempt != 2 {
		t.Fatalf("tests calls = %d", got)
	}
}

// --- three attempts, three review rounds --------------------------------------------------------

func TestThreeRejectedAttemptsHandOverWithReason(t *testing.T) {
	f := newFixture(t, pattern())
	f.failChecks("code")
	r := f.start("RUN-1")
	if r.Status != machine.StatusWaiting || r.Outcome != "" {
		t.Fatalf("run = %s/%s", r.Status, r.Outcome)
	}
	calls := f.w.callsTo("code")
	if len(calls) != 3 || calls[0].Attempt != 1 || calls[2].Attempt != 3 {
		t.Fatalf("code attempts = %d", len(calls))
	}
	if !strings.Contains(r.Reason, "code") || !strings.Contains(r.Reason, "3") {
		t.Fatalf("reason %q should name the step and the 3 attempts", r.Reason)
	}
	if f.ev.count("StepRejected") != 3 || f.ev.count("RunHandedOver") != 1 || len(f.w.callsTo("review")) != 0 {
		t.Fatalf("events = %v", f.ev.kinds())
	}
	last := f.ev.events[len(f.ev.events)-1]
	if last.Kind != "RunHandedOver" || last.Detail != r.Reason {
		t.Fatalf("last event = %s %q", last.Kind, last.Detail)
	}
	if !r.ExpiresAt.Equal(t0.Add(14 * 24 * time.Hour)) {
		t.Fatalf("expires_at = %v", r.ExpiresAt)
	}
	validate(t, f.ev.events)
}

func TestSecondAttemptCanBeAccepted(t *testing.T) {
	f := newFixture(t, pattern())
	f.c.fn = func(r machine.CheckRequest) (machine.CheckResult, error) {
		if r.Step.Name == "code" && r.Attempt == 1 && r.Name == "AllTestsPass" {
			return fail(r), nil
		}
		return noBlocking(r)
	}
	assertEnded(t, f.start("RUN-1"), f.ev, "done")
	for _, e := range f.ev.events {
		if e.Kind == "StepAccepted" && e.Step == "code" && e.Attempt != 2 {
			t.Fatalf("accepted attempt = %d, want 2", e.Attempt)
		}
	}
}

func TestThreeReviewRoundsHandOverWithOpenFindings(t *testing.T) {
	f := newFixture(t, pattern())
	f.c.fn = noBlocking
	f.w.fn["review"] = func(r machine.Request) (machine.Result, error) {
		res := ok(r)
		res.Findings = []machine.Finding{blocking("R-1", "internal/a.go", 10)}
		return res, nil
	}
	r := f.start("RUN-1")
	if r.Status != machine.StatusWaiting {
		t.Fatalf("status = %s", r.Status)
	}
	if got := len(f.w.callsTo("review")); got != 3 {
		t.Fatalf("review rounds = %d, want 3", got)
	}
	if !strings.Contains(r.Reason, "R-1") || !strings.Contains(r.Reason, "internal/a.go") {
		t.Fatalf("reason %q should list the open findings", r.Reason)
	}
	if len(r.Open) != 1 || r.Open[0].Rule != "R-1" {
		t.Fatalf("open findings = %+v", r.Open)
	}
	rounds := map[int]bool{}
	for _, e := range f.ev.events {
		if e.Kind == "FindingRecorded" {
			rounds[e.Round] = true
			if !slices.Contains(e.Refs, "R-1") {
				t.Fatalf("finding event refs = %v", e.Refs)
			}
		}
	}
	if len(rounds) != 3 || f.ev.count("RunHandedOver") != 1 {
		t.Fatalf("rounds = %v, events = %v", rounds, f.ev.kinds())
	}
	validate(t, f.ev.events)
}

func TestLaterRoundConsidersOnlyOpenFindingsAndChangedLines(t *testing.T) {
	f := newFixture(t, pattern())
	f.c.fn = noBlocking
	open := blocking("R-1", "internal/a.go", 10)
	suggestion := machine.Finding{Rule: "R-2", Text: "rename", Blocking: false}
	f.w.fn["review"] = func(r machine.Request) (machine.Result, error) {
		res := ok(r)
		if r.Round == 1 {
			res.Findings = []machine.Finding{open, suggestion}
		}
		return res, nil
	}
	fixes := 0
	f.w.fn["code"] = func(r machine.Request) (machine.Result, error) {
		res := ok(r)
		if len(r.Open) > 0 {
			fixes++
			res.ChangedLines = []machine.Line{{File: "internal/a.go", Line: 10}}
		}
		return res, nil
	}
	assertEnded(t, f.start("RUN-1"), f.ev, "done")
	rv := f.w.callsTo("review")
	if len(rv) != 2 || rv[0].Round != 1 || rv[1].Round != 2 {
		t.Fatalf("review calls = %+v", rv)
	}
	if len(rv[0].Open) != 0 || len(rv[0].ChangedLines) != 0 {
		t.Fatalf("round 1 reviews everything, got %+v", rv[0])
	}
	if len(rv[1].Open) != 1 || rv[1].Open[0] != open {
		t.Fatalf("round 2 open = %+v, want only the blocking finding", rv[1].Open)
	}
	if !slices.Equal(rv[1].ChangedLines, []machine.Line{{File: "internal/a.go", Line: 10}}) {
		t.Fatalf("round 2 changed lines = %+v", rv[1].ChangedLines)
	}
	if fixes != 1 {
		t.Fatalf("code fixes with open findings = %d, want 1", fixes)
	}
}

// --- budgets ------------------------------------------------------------------------------------

func TestBudgetsHandOver(t *testing.T) {
	big := machine.Budget{Tokens: 1 << 40, CostUSD: 1e9, Duration: 1000 * time.Hour}
	cases := []struct {
		name  string
		edit  func(*machine.Pattern)
		use   func(*clock, *machine.Result)
		reasn string
	}{
		{"run tokens", func(p *machine.Pattern) { p.RunBudget.Tokens = 1000; p.StepBudget = big },
			func(_ *clock, r *machine.Result) { r.Usage.Tokens = 5000 }, "token"},
		{"run cost", func(p *machine.Pattern) { p.RunBudget.CostUSD = 0.10; p.StepBudget = big },
			func(_ *clock, r *machine.Result) { r.Usage.CostUSD = 0.60 }, "cost"},
		{"run time", func(p *machine.Pattern) { p.RunBudget.Duration = 30 * time.Minute; p.StepBudget = big },
			func(c *clock, _ *machine.Result) { c.Advance(31 * time.Minute) }, "time"},
		{"step tokens", func(p *machine.Pattern) { p.StepBudget.Tokens = 1000 },
			func(_ *clock, r *machine.Result) { r.Usage.Tokens = 5000 }, "token"},
		{"step cost", func(p *machine.Pattern) { p.StepBudget.CostUSD = 0.05 },
			func(_ *clock, r *machine.Result) { r.Usage.CostUSD = 0.10 }, "cost"},
		{"step time", func(p *machine.Pattern) { p.StepBudget.Duration = 10 * time.Minute },
			func(c *clock, _ *machine.Result) { c.Advance(11 * time.Minute) }, "time"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := pattern()
			p.RunBudget = big
			tc.edit(&p)
			f := newFixture(t, p)
			f.w.fn["generate"] = func(r machine.Request) (machine.Result, error) {
				res := ok(r)
				tc.use(f.clk, &res)
				return res, nil
			}
			r := f.start("RUN-1")
			if r.Status != machine.StatusWaiting || !strings.Contains(strings.ToLower(r.Reason), tc.reasn) {
				t.Fatalf("run = %s, reason %q, want waiting-for-developer naming %q", r.Status, r.Reason, tc.reasn)
			}
			for _, e := range f.ev.events {
				if e.Kind == "StepAccepted" && e.Step == "generate" {
					t.Fatal("the step that ran out of budget was accepted")
				}
			}
			if len(f.w.callsTo("tests")) != 0 || f.ev.count("RunHandedOver") != 1 {
				t.Fatalf("run went on after its budget: %v", f.ev.kinds())
			}
			validate(t, f.ev.events)
		})
	}
}

// --- expiry, hand-over acceptance, cancel -------------------------------------------------------

func handedOver(t *testing.T) *fixture {
	f := newFixture(t, pattern())
	f.failChecks("code")
	if r := f.start("RUN-1"); r.Status != machine.StatusWaiting {
		t.Fatalf("status = %s", r.Status)
	}
	return f
}

func TestHandOverExpiresAfterFourteenDays(t *testing.T) {
	f := handedOver(t)
	f.clk.Advance(14*24*time.Hour - time.Second)
	f.m.ExpireDue()
	if r, _ := f.m.Get("RUN-1"); r.Status != machine.StatusWaiting {
		t.Fatalf("expired early: %s", r.Status)
	}
	f.clk.Advance(time.Second)
	f.m.ExpireDue()
	r, ok := f.m.Get("RUN-1")
	if !ok {
		t.Fatal("run lost")
	}
	assertEnded(t, r, f.ev, "expired")
	if r.Reason == "" {
		t.Fatal("expired run keeps no reason")
	}
	f.m.ExpireDue()
	if f.ev.count("RunExpired") != 1 {
		t.Fatal("expired twice")
	}
	validate(t, f.ev.events)
}

func TestAcceptHandOverEndsHandedOver(t *testing.T) {
	f := handedOver(t)
	r, err := f.m.AcceptHandOver("RUN-1")
	if err != nil {
		t.Fatal(err)
	}
	assertEnded(t, r, f.ev, "handed-over")
	if _, err := f.m.AcceptHandOver("RUN-1"); !errors.Is(err, machine.ErrNotWaiting) {
		t.Fatalf("second accept err = %v, want ErrNotWaiting", err)
	}
	validate(t, f.ev.events)
}

func TestCancelWhileWaitingEndsCancelled(t *testing.T) {
	f := handedOver(t)
	r, err := f.m.Cancel("RUN-1")
	if err != nil {
		t.Fatal(err)
	}
	assertEnded(t, r, f.ev, "cancelled")
	if _, err := f.m.Cancel("RUN-1"); !errors.Is(err, machine.ErrRunEnded) {
		t.Fatalf("second cancel err = %v, want ErrRunEnded", err)
	}
	f.clk.Advance(15 * 24 * time.Hour)
	f.m.ExpireDue()
	if got, _ := f.m.Get("RUN-1"); got.Outcome != "cancelled" {
		t.Fatalf("outcome changed to %s", got.Outcome)
	}
	validate(t, f.ev.events)
}

func TestCancelWhileRunningStopsTheRun(t *testing.T) {
	f := newFixture(t, pattern())
	f.w.fn["generate"] = func(r machine.Request) (machine.Result, error) {
		if _, err := f.m.Cancel("RUN-1"); err != nil {
			t.Errorf("Cancel: %v", err)
		}
		return ok(r), nil
	}
	r := f.start("RUN-1")
	assertEnded(t, r, f.ev, "cancelled")
	if len(f.w.callsTo("tests")) != 0 {
		t.Fatal("steps ran after cancel")
	}
	for _, e := range f.ev.events {
		if e.Kind == "StepAccepted" && e.Step == "generate" {
			t.Fatal("step accepted after cancel")
		}
	}
	validate(t, f.ev.events)
}

// --- one active run -----------------------------------------------------------------------------

func TestOneActiveRunAtATime(t *testing.T) {
	f := newFixture(t, pattern())
	var inner error
	f.w.fn["generate"] = func(r machine.Request) (machine.Result, error) {
		_, inner = f.m.Start(context.Background(), f.params("RUN-2"))
		return ok(r), nil
	}
	f.c.fn = noBlocking
	assertEnded(t, f.start("RUN-1"), f.ev, "done")
	if !errors.Is(inner, machine.ErrRunActive) {
		t.Fatalf("second Start while running: err = %v, want ErrRunActive", inner)
	}
	for _, e := range f.ev.events {
		if e.Run == "RUN-2" {
			t.Fatal("the refused run left events")
		}
	}
	// a finished run frees the slot
	f.w.fn["generate"] = nil
	if r, err := f.m.Start(context.Background(), f.params("RUN-3")); err != nil || r.Outcome != "done" {
		t.Fatalf("Start after done: %v %v", r.Outcome, err)
	}
}

func TestRunWaitingForDeveloperStillBlocksANewRun(t *testing.T) {
	f := handedOver(t)
	if _, err := f.m.Start(context.Background(), f.params("RUN-2")); !errors.Is(err, machine.ErrRunActive) {
		t.Fatalf("err = %v, want ErrRunActive", err)
	}
	if _, err := f.m.AcceptHandOver("RUN-1"); err != nil {
		t.Fatal(err)
	}
	f.c.fn = noBlocking
	if _, err := f.m.Start(context.Background(), f.params("RUN-2")); err != nil {
		t.Fatalf("Start after the hand-over was accepted: %v", err)
	}
}

// --- events -------------------------------------------------------------------------------------

func TestEventsNameModelDigestPatternInputsReuseKeyAndRefs(t *testing.T) {
	f := newFixture(t, pattern())
	f.c.fn = noBlocking
	assertEnded(t, f.start("RUN-1"), f.ev, "done")
	validate(t, f.ev.events)

	artifactOf := map[string]string{}
	keys := map[string]bool{}
	for _, e := range f.ev.events {
		if e.Run != "RUN-1" || !e.At.Equal(t0) {
			t.Fatalf("event %s: run %q at %v", e.Kind, e.Run, e.At)
		}
		switch e.Kind {
		case "AttemptMade", "StepAccepted", "StepRejected", "CheckPassed", "CheckFailed":
			if len(e.Refs) == 0 {
				t.Fatalf("%s %s has no refs", e.Kind, e.Step)
			}
		}
		switch e.Kind {
		case "RunStarted":
			if e.Pattern != "api-feature-go@1.0.0" {
				t.Fatalf("pattern = %q", e.Pattern)
			}
		case "AttemptMade", "StepAccepted":
			if e.Pattern != "api-feature-go@1.0.0" || len(e.Inputs) == 0 || len(e.Artifacts) == 0 ||
				len(e.ReuseKey) != 64 || e.Attempt < 1 || e.Worker == "" {
				t.Fatalf("%s %s incomplete: %+v", e.Kind, e.Step, e)
			}
			if strings.HasPrefix(e.Worker, "agent:") && (e.Model != "claude-haiku-5-5" || e.AgentDigest != digest) {
				t.Fatalf("%s %s: model %q digest %q", e.Kind, e.Step, e.Model, e.AgentDigest)
			}
			if e.Kind == "StepAccepted" {
				keys[e.ReuseKey] = true
				artifactOf[e.Step] = strings.Join(e.Artifacts, ",")
			}
		}
	}
	if len(keys) != 5 {
		t.Fatalf("reuse keys are not distinct per step: %d", len(keys))
	}
	// inputs are the hashes of the step's declared inputs, in order: slice parts and earlier outputs
	for _, e := range f.ev.events {
		if e.Kind == "StepAccepted" && e.Step == "generate" {
			contract := f.w.callsTo("contract-check")[0]
			want := ok(contract).Outputs["contract"]
			if !slices.Equal(e.Inputs, []string{want}) {
				t.Fatalf("generate inputs = %v, want [%s]", e.Inputs, want)
			}
		}
		if e.Kind == "StepAccepted" && e.Step == "contract-check" {
			if !slices.Equal(e.Inputs, []string{hash("contract"), hash("rules")}) {
				t.Fatalf("contract-check inputs = %v", e.Inputs)
			}
		}
	}
}

func TestSameInputsGiveSameReuseKeyAndChangedInputsDoNot(t *testing.T) {
	keyOf := func(rules string) string {
		f := newFixture(t, pattern())
		f.c.fn = noBlocking
		p := f.params("RUN-1")
		p.Inputs["slice/rules"] = hash(rules)
		if _, err := f.m.Start(context.Background(), p); err != nil {
			t.Fatal(err)
		}
		for _, e := range f.ev.events {
			if e.Kind == "StepAccepted" && e.Step == "contract-check" {
				return e.ReuseKey
			}
		}
		t.Fatal("no StepAccepted")
		return ""
	}
	if keyOf("a") != keyOf("a") || keyOf("a") == keyOf("b") {
		t.Fatal("reuse key must be a function of the step's inputs")
	}
}

func TestCheckEventsUseTheChecksRefsAndFallBackToTheRunsRefs(t *testing.T) {
	f := newFixture(t, pattern())
	f.c.fn = func(r machine.CheckRequest) (machine.CheckResult, error) {
		res := pass(r)
		if r.Step.Name == "tests" && r.Name == "TestsBuild" {
			res.Refs = []string{"R-1/E-1"}
		}
		return res, nil
	}
	f.start("RUN-1")
	for _, e := range f.ev.events {
		if e.Kind != "CheckPassed" {
			continue
		}
		if e.Step == "tests" && e.Check.Name == "TestsBuild" && !slices.Equal(e.Refs, []string{"R-1/E-1"}) {
			t.Fatalf("refs = %v, want the check's own", e.Refs)
		}
		if e.Step == "generate" && len(e.Refs) == 0 {
			t.Fatal("no refs on a check that named none")
		}
	}
	validate(t, f.ev.events)
}

func TestWorkerErrorOnEveryAttemptInterruptsTheRun(t *testing.T) {
	f := newFixture(t, pattern())
	f.w.fn["tests"] = func(machine.Request) (machine.Result, error) {
		return machine.Result{}, errors.New("model unavailable")
	}
	r := f.start("RUN-1")
	if r.Status != machine.StatusInterrupted || r.Outcome != "" {
		t.Fatalf("run = %s/%s, want interrupted (resumable), not handed over", r.Status, r.Outcome)
	}
	if got := len(f.w.callsTo("tests")); got != 3 {
		t.Fatalf("tests attempts = %d, want 3", got)
	}
	if f.ev.count("RunHandedOver") != 0 || f.ev.count("RunInterrupted") != 1 {
		t.Fatalf("events = %v", f.ev.kinds())
	}
	validate(t, f.ev.events)
}
