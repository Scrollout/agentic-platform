package machine_test

// T088 (1) (owner, 2026-11-15; research D36, ADR 0040; data-model "An open round reused", "Reuse
// key", "Review rounds" and "Commits"): an open review round keeps its result under its reuse key,
// marked open, and a later run of the same input reuses it, so the fix and the later rounds are
// reused too. Written before the code by a separate test author, from the design and the
// contracts (run-event.schema.json `open_round`; the check contract's commitStep).
//
// ASSUMED SHAPE (names the design implies; until T088's code they do not compile):
//
//	machine.Event gains `OpenRound bool` (json "open_round,omitempty"): true on the StepRejected
//	  that leaves a review round open and on the ResultReused that reuses such a round.
//	machine.Accepted gains `Open bool` (the mark open), `Findings []Finding` (every finding of
//	  the round, in recorded order, suggestions included) and `Check *EventCheck` (the round's
//	  NoBlockingFindingOpen result as recorded: name, passed false, reason).
//	machine.KeyParts gains `Round int`: the review round a review step's key covers; 0 (no round)
//	  gives the key every other step had before T088.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/agentic-platform/engine/machine"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	orModel   = "claude-haiku-5-5"
	orPattern = "api-feature-go@1.0.0"
)

// orfix is one engine over a pattern whose review may write witnesses, with a fake version history.
// The worker is deterministic: a step's outputs and files depend only on the step and the round, so
// a replay's keys match the original's. findings gives the review's findings per round.
type orfix struct {
	*fixture
	p        machine.Pattern
	h        *history
	log      *oplog
	sk       loggedSink
	results  *machine.Results
	findings func(round int) []machine.Finding
}

func sortFiles(fs []machine.File) {
	slices.SortFunc(fs, func(a, b machine.File) int { return strings.Compare(a.Path, b.Path) })
}

func newOR(t *testing.T, p machine.Pattern, results *machine.Results, findings func(int) []machine.Finding) *orfix {
	t.Helper()
	f := newFixture(t, p)
	log := &oplog{}
	x := &orfix{fixture: f, p: p, h: &history{log: log}, log: log, sk: loggedSink{f.ev, log}, results: results,
		findings: findings}
	for _, s := range p.Steps {
		name := s.Name
		f.w.fn[name] = func(r machine.Request) (machine.Result, error) {
			f.clk.Advance(time.Second)
			res := ok(r)
			for o := range res.Outputs {
				res.Outputs[o] = hash(fmt.Sprintf("%s/%s/r%d", name, o, r.Round))
			}
			files := slices.DeleteFunc(slices.Clone(r.Before), func(f machine.File) bool { return f.Path == name+"/f.go" })
			if !r.Step.Reviews() {
				files = append(files, machine.File{Path: name + "/f.go", SHA256: hash(fmt.Sprintf("file/%s/r%d", name, r.Round))})
			}
			if r.Step.Reviews() {
				res.Findings = x.findings(r.Round)
				for _, fd := range res.Findings {
					if fd.Witness != nil && len(r.Step.Writable) > 0 && !hasFile(files, fd.Witness.File) {
						files = append(files, machine.File{Path: fd.Witness.File, SHA256: hash("witness/" + fd.Witness.File)})
					}
				}
			}
			if name == "code" && len(r.Open) > 0 {
				res.ChangedLines = []machine.Line{{File: "code/f.go", Line: 10 * r.Round}}
			}
			sortFiles(files)
			res.Before, res.ProjectFiles = r.Before, files
			return res, nil
		}
	}
	f.c.fn = orCheck
	f.m = x.build()
	return x
}

// orCheck passes every check but NoBlockingFindingOpen, which fails while a blocking finding is
// open, with a reason that names the round, so a reused round's reason can be told apart.
func orCheck(r machine.CheckRequest) (machine.CheckResult, error) {
	if r.Name == "NoBlockingFindingOpen" {
		n := 0
		for _, f := range r.Result.Findings {
			if f.Blocking {
				n++
			}
		}
		if n > 0 {
			var confirmed []int
			for i, f := range r.Result.Findings {
				if f.Blocking {
					confirmed = append(confirmed, i)
				}
			}
			return machine.CheckResult{Passed: false, Confirmed: confirmed, Reason: fmt.Sprintf("%d blocking finding(s) open in round %d", n, r.Round)}, nil
		}
	}
	return pass(r), nil
}

// build makes a machine over the fixture's parts; a second call is a restarted engine.
func (x *orfix) build() *machine.Machine {
	return machine.New(machine.Config{Pattern: x.p, Worker: loggedWorker{x.w, x.log}, Checker: x.c, Clock: x.clk,
		Events: x.sk, Results: x.results, Model: orModel, History: x.h, Sleep: x.h.sleep})
}

func (x *orfix) startIn(id, project string) machine.Run {
	x.t.Helper()
	p := x.params(id)
	p.Project = project
	r, err := x.m.Start(context.Background(), p)
	if err != nil {
		x.t.Fatalf("Start(%s): %v", id, err)
	}
	return r
}

// round1Open: review round 1 leaves R-1 open with its witness and makes a suggestion R-2; later
// rounds find nothing.
func round1Open(round int) []machine.Finding {
	if round != 1 {
		return nil
	}
	fd := blocking("R-1", "code/f.go", 1)
	fd.Witness = &machine.Witness{File: witnessPath, Test: witnessName}
	return []machine.Finding{fd, {Rule: "R-2", Text: "rename the handler", Blocking: false}}
}

// everyRoundOpen: every round leaves one blocking finding open, its line and witness by round.
func everyRoundOpen(round int) []machine.Finding {
	fd := blocking("R-1", "code/f.go", 10*round)
	fd.Witness = &machine.Witness{File: fmt.Sprintf("review/r1/round%d_test.go", round), Test: fmt.Sprintf("TestR1Round%d", round)}
	return []machine.Finding{fd}
}

func openDetail(round int) string {
	return fmt.Sprintf("review round %d left blocking findings open", round)
}

// isOpenRejected is the StepRejected that left a review round open, told by its detail (the design
// also marks it open_round).
func isOpenRejected(e machine.Event) bool {
	return e.Kind == "StepRejected" && e.Step == "review" && e.Round > 0 && e.Detail == openDetail(e.Round)
}

// settledStep is what one step or one review round settled on.
type settledStep struct {
	step      string
	round     int
	open      bool
	key       string
	artifacts []string
	files     []machine.File
	at        time.Time
}

func (s settledStep) String() string {
	return fmt.Sprintf("%s r%d open=%v", s.step, s.round, s.open)
}

// settled lists, in order, what each step and review round of a run settled on: an accepted or
// reused result, or an open round (its AttemptMade's artifacts and key, its StepRejected's files).
func settled(events []machine.Event) []settledStep {
	var out []settledStep
	made := map[string]machine.Event{} // step/round/attempt -> AttemptMade
	id := func(e machine.Event) string { return fmt.Sprintf("%s/%d/%d", e.Step, e.Round, e.Attempt) }
	for _, e := range events {
		switch {
		case e.Kind == "AttemptMade":
			made[id(e)] = e
		case e.Kind == "StepAccepted":
			out = append(out, settledStep{e.Step, e.Round, false, e.ReuseKey, e.Artifacts, e.Files, e.At})
		case e.Kind == "ResultReused":
			out = append(out, settledStep{e.Step, e.Round, e.OpenRound, e.ReuseKey, e.Artifacts, e.Files, e.At})
		case isOpenRejected(e):
			a := made[id(e)]
			out = append(out, settledStep{e.Step, e.Round, true, a.ReuseKey, a.Artifacts, e.Files, e.At})
		}
	}
	return out
}

type commitView struct {
	step  string
	round int
	open  bool
}

func commitViews(cs []machine.CommitRequest) []commitView {
	var out []commitView
	for _, c := range cs {
		out = append(out, commitView{c.Step.Name, c.Round, c.OpenRound})
	}
	return out
}

func eventsOf(events []machine.Event, kinds ...string) []machine.Event {
	var out []machine.Event
	for _, e := range events {
		if slices.Contains(kinds, e.Kind) {
			out = append(out, e)
		}
	}
	return out
}

// round1Findings are RUN-1's FindingRecorded events of review round n, in order.
func roundFindings(events []machine.Event, n int) []machine.Finding {
	var out []machine.Finding
	for _, e := range events {
		if e.Kind == "FindingRecorded" && e.Step == "review" && e.Round == n && e.Finding != nil {
			out = append(out, *e.Finding)
		}
	}
	return out
}

func findingsEqual(a, b []machine.Finding) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return len(a) == len(b) && string(ja) == string(jb)
}

// openCheckOf is the NoBlockingFindingOpen CheckFailed of review round n's last attempt.
func openCheckOf(t *testing.T, events []machine.Event, n int) machine.EventCheck {
	t.Helper()
	var got *machine.EventCheck
	for _, e := range events {
		if e.Kind == "CheckFailed" && e.Step == "review" && e.Round == n && e.Check != nil && e.Check.Name == "NoBlockingFindingOpen" {
			got = e.Check
		}
	}
	if got == nil {
		t.Fatalf("no NoBlockingFindingOpen CheckFailed in review round %d", n)
	}
	return *got
}

// schemaRefuses reports whether the run-event schema refuses the event as JSON, after edit.
func schemaRefuses(t *testing.T, e machine.Event, edit func(m map[string]any)) bool {
	t.Helper()
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	edit(m)
	b, _ = json.Marshal(m)
	v, err := jsonschema.UnmarshalJSON(strings.NewReader(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	return schema(t).Validate(v) != nil
}

func TestReplayReusesAnOpenRoundTheFixAndTheNextRound(t *testing.T) {
	results := machine.NewResults()
	one := newOR(t, witnessPatternOf(), results, round1Open)
	assertEnded(t, one.startIn("RUN-1", "proj-a"), one.ev, "done")
	if n := len(eventsOf(one.ev.events, "StepRejected")); n != 1 || !isOpenRejected(eventsOf(one.ev.events, "StepRejected")[0]) {
		t.Fatalf("setup: RUN-1 does not leave review round 1 open once: %v", one.ev.kinds())
	}
	validate(t, one.ev.events)

	two := newOR(t, witnessPatternOf(), results, round1Open)
	r := two.startIn("RUN-2", "proj-b")
	assertEnded(t, r, two.ev, "done")
	if len(two.w.calls) != 0 || len(two.c.calls) != 0 {
		t.Fatalf("RUN-2 called the worker %d times and the checker %d times, want neither: %v",
			len(two.w.calls), len(two.c.calls), two.ev.kinds())
	}
	for _, k := range []string{"StepStarted", "AttemptMade", "CheckPassed", "CheckFailed", "StepRejected", "StepAccepted"} {
		if n := two.ev.count(k); n != 0 {
			t.Errorf("RUN-2 records %d %s events; nothing ran", n, k)
		}
	}

	// Every step and every round is a ResultReused with RUN-1's key, artifacts and files.
	want, got := settled(one.ev.events), settled(two.ev.events)
	if len(got) != len(want) {
		t.Fatalf("RUN-2 settled %v, want RUN-1's %v", got, want)
	}
	for i := range want {
		w, g := want[i], got[i]
		if g.step != w.step || g.round != w.round || g.open != w.open {
			t.Errorf("settled %d: RUN-2 %v, RUN-1 %v", i, g, w)
			continue
		}
		if g.key != w.key || g.key == "" {
			t.Errorf("%v: reuse_key %q, want RUN-1's %q", g, g.key, w.key)
		}
		if !slices.Equal(g.artifacts, w.artifacts) {
			t.Errorf("%v: artifacts %v, want RUN-1's %v", g, g.artifacts, w.artifacts)
		}
		if !sameFiles(g.files, w.files) {
			t.Errorf("%v: files %v, want RUN-1's %v", g, g.files, w.files)
		}
	}
	reused := eventsOf(two.ev.events, "ResultReused")
	if len(reused) != len(want) {
		t.Fatalf("RUN-2 has %d ResultReused, want one per step and round (%d)", len(reused), len(want))
	}

	// The open round: FindingRecorded as RUN-1's round 1 (no attempt), then its ResultReused.
	oi := slices.IndexFunc(two.ev.events, func(e machine.Event) bool { return e.Kind == "ResultReused" && e.Step == "review" && e.Round == 1 })
	if oi < 0 {
		t.Fatalf("RUN-2 has no ResultReused for review round 1: %v", two.ev.kinds())
	}
	open := two.ev.events[oi]
	wantCheck := openCheckOf(t, one.ev.events, 1)
	if !open.OpenRound {
		t.Errorf("review round 1's ResultReused does not carry open_round")
	}
	if open.Check == nil || *open.Check != wantCheck {
		t.Errorf("review round 1's ResultReused check = %+v, want RUN-1's %+v", open.Check, wantCheck)
	}
	if open.Detail != openDetail(1) {
		t.Errorf("review round 1's ResultReused detail = %q, want %q", open.Detail, openDetail(1))
	}
	kept := roundFindings(one.ev.events, 1)
	if oi < len(kept) {
		t.Fatalf("too few events before the open ResultReused")
	}
	var before []machine.Finding
	for _, e := range two.ev.events[oi-len(kept) : oi] {
		if e.Kind != "FindingRecorded" || e.Step != "review" || e.Round != 1 || e.Attempt != 0 || e.Finding == nil {
			t.Errorf("event before the open ResultReused = %s %s round %d attempt %d, want review round 1's FindingRecorded without attempt",
				e.Kind, e.Step, e.Round, e.Attempt)
			continue
		}
		if !slices.Equal(e.Refs, []string{e.Finding.Rule}) {
			t.Errorf("a reused finding's refs = %v, want its rule %s", e.Refs, e.Finding.Rule)
		}
		before = append(before, *e.Finding)
	}
	if !findingsEqual(before, kept) {
		t.Errorf("the findings recorded before the open ResultReused = %+v, want RUN-1's round 1 %+v", before, kept)
	}
	if n := len(eventsOf(two.ev.events, "FindingRecorded")); n != len(kept) {
		t.Errorf("RUN-2 records %d FindingRecorded, want round 1's %d", n, len(kept))
	}
	for _, e := range reused {
		if e.OpenRound != (e.Step == "review" && e.Round == 1) {
			t.Errorf("ResultReused %s round %d: open_round %v", e.Step, e.Round, e.OpenRound)
		}
	}

	// The commits: RUN-1's, in order, with equal files; the open round's reused with its witnesses.
	if !slices.Equal(commitViews(two.h.commits), commitViews(one.h.commits)) {
		t.Fatalf("RUN-2 commits %v, want RUN-1's %v", commitViews(two.h.commits), commitViews(one.h.commits))
	}
	for i, c := range two.h.commits {
		if !sameFiles(c.ProjectFiles, one.h.commits[i].ProjectFiles) {
			t.Errorf("commit %d (%s round %d): project_files %v, want RUN-1's %v", i, c.Step.Name, c.Round, c.ProjectFiles, one.h.commits[i].ProjectFiles)
		}
		if c.ReusedFrom != "RUN-1" || c.Attempt != 0 {
			t.Errorf("commit %d (%s round %d): reused_from %q attempt %d, want RUN-1 and none", i, c.Step.Name, c.Round, c.ReusedFrom, c.Attempt)
		}
		if !c.OpenRound {
			continue
		}
		wantOut := []machine.Artifact{{Name: "witnesses", SHA256: open.Artifacts[slices.Index(witnessPatternOf().Steps[4].Outputs, "witnesses")]}}
		if c.Step.Name != "review" || c.Round != 1 || !slices.Equal(c.Outputs, wantOut) {
			t.Errorf("the open round's commit = %s round %d outputs %v, want review round 1 with outputs exactly %v",
				c.Step.Name, c.Round, c.Outputs, wantOut)
		}
		if !c.AcceptedAt.Equal(open.At) {
			t.Errorf("the open round's commit accepted_at = %v, want its ResultReused's at %v", c.AcceptedAt, open.At)
		}
		if !hasFile(c.ProjectFiles, witnessPath) {
			t.Errorf("the open round's commit lacks the witness: %v", c.ProjectFiles)
		}
	}

	if d := machine.Compare(one.ev.events, two.ev.events); len(d) != 0 {
		t.Errorf("RUN-2 differs from RUN-1: %+v", d)
	}
	validate(t, two.ev.events)

	// The schema refuses open_round on an accepted step and an open ResultReused without check.
	acc := eventsOf(one.ev.events, "StepAccepted")[0]
	if !schemaRefuses(t, acc, func(m map[string]any) { m["open_round"] = true }) {
		t.Errorf("the schema accepts open_round on a StepAccepted")
	}
	if !schemaRefuses(t, open, func(m map[string]any) { delete(m, "check") }) {
		t.Errorf("the schema accepts an open ResultReused without check")
	}
	if schemaRefuses(t, open, func(map[string]any) {}) {
		t.Errorf("the schema refuses RUN-2's open ResultReused as recorded")
	}
}

func TestOpenRoundKeepsItsResult(t *testing.T) {
	results := machine.NewResults()
	one := newOR(t, witnessPatternOf(), results, round1Open)
	assertEnded(t, one.startIn("RUN-1", "proj-a"), one.ev, "done")
	var rej, made *machine.Event
	for i, e := range one.ev.events {
		if e.Kind == "AttemptMade" && e.Step == "review" && e.Round == 1 {
			made = &one.ev.events[i]
		}
		if isOpenRejected(e) {
			rej = &one.ev.events[i]
		}
	}
	if rej == nil || made == nil {
		t.Fatalf("setup: no open round in RUN-1: %v", one.ev.kinds())
	}
	if !rej.OpenRound || rej.Round != 1 {
		t.Errorf("the open StepRejected: open_round %v round %d, want true and 1", rej.OpenRound, rej.Round)
	}
	if rej.ReuseKey == "" || rej.ReuseKey != made.ReuseKey || !slices.Equal(rej.Artifacts, made.Artifacts) {
		t.Errorf("the open StepRejected: reuse_key %q artifacts %v, want its AttemptMade's %q %v",
			rej.ReuseKey, rej.Artifacts, made.ReuseKey, made.Artifacts)
	}
	a, found := results.Get(made.ReuseKey)
	if !found {
		t.Fatalf("no result kept under the open round's key")
	}
	if !a.Open || a.Run != "RUN-1" || !slices.Equal(a.Artifacts, made.Artifacts) {
		t.Errorf("kept result = open %v run %q artifacts %v, want open, RUN-1, %v", a.Open, a.Run, a.Artifacts, made.Artifacts)
	}
	if want := round1Open(1); !findingsEqual(a.Findings, want) {
		t.Errorf("kept findings = %+v, want both, in order: %+v", a.Findings, want)
	}
	if want := openCheckOf(t, one.ev.events, 1); a.Check == nil || *a.Check != want {
		t.Errorf("kept check = %+v, want the round's NoBlockingFindingOpen result %+v", a.Check, want)
	}
	if !sameFiles(a.Files, rej.Files) || !hasFile(a.Files, witnessPath) {
		t.Errorf("kept files = %v, want the round's files with its witness %v", a.Files, rej.Files)
	}
	validate(t, one.ev.events)
}

// todayKey is the reuse key's encoding before T088 (reuse.go): every part framed by its length, the
// inputs by their count. A part with no round must keep giving it.
func todayKey(p machine.KeyParts) string {
	h := sha256.New()
	field := func(v string) { h.Write([]byte(strconv.Itoa(len(v)) + ":" + v + "\n")) }
	field(p.PatternVersion)
	field(p.Step)
	field(p.AgentDigest)
	field(p.Model)
	field(strconv.Itoa(len(p.Inputs)))
	for _, in := range p.Inputs {
		field(in)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func TestReviewKeyCoversTheRound(t *testing.T) {
	base := machine.KeyParts{PatternVersion: orPattern, Step: "review", Inputs: []string{hash("a"), hash("b")},
		AgentDigest: digest, Model: orModel}
	r1, r2 := base, base
	r1.Round, r2.Round = 1, 2
	k0, k1, k2 := machine.ReuseKey(base), machine.ReuseKey(r1), machine.ReuseKey(r2)
	if k1 == k2 || k0 == k1 || k0 == k2 {
		t.Errorf("keys differing only in round are equal: none %s, 1 %s, 2 %s", k0, k1, k2)
	}
	if k0 != todayKey(base) {
		t.Errorf("a part with no round gives %s, want today's key %s", k0, todayKey(base))
	}

	// In a run: the review's key covers its round, every other step's, the fix's included, none.
	one := newOR(t, witnessPatternOf(), machine.NewResults(), round1Open)
	assertEnded(t, one.startIn("RUN-1", "proj-a"), one.ev, "done")
	steps := map[string]machine.Step{}
	for _, s := range witnessPatternOf().Steps {
		steps[s.Name] = s
	}
	seen := map[string]bool{}
	for _, e := range eventsOf(one.ev.events, "AttemptMade") {
		s := steps[e.Step]
		p := machine.KeyParts{PatternVersion: orPattern, Step: e.Step, Inputs: e.Inputs, AgentDigest: s.AgentDigest}
		if strings.HasPrefix(s.Worker, "agent:") {
			p.Model = orModel
		}
		if s.Reviews() {
			p.Round = e.Round
		}
		if want := machine.ReuseKey(p); e.ReuseKey != want {
			t.Errorf("%s round %d: reuse_key %s, want ReuseKey with round %d: %s", e.Step, e.Round, e.ReuseKey, p.Round, want)
		}
		if e.Step == "code" && e.Round == 1 {
			seen["fix"] = true
			if todayKey(p) != e.ReuseKey {
				t.Errorf("the fix's key %s is not the key of its parts without a round %s", e.ReuseKey, todayKey(p))
			}
		}
		if e.Step == "review" {
			seen[fmt.Sprintf("review %d", e.Round)] = true
		}
	}
	if !seen["fix"] || !seen["review 1"] || !seen["review 2"] {
		t.Fatalf("setup: the run lacks the fix or a review round: %v", seen)
	}
}

func TestOpenRoundOfAReviewWithoutWritableEntries(t *testing.T) {
	results := machine.NewResults()
	p := pattern() // the review writes nothing: outputs [findings], no writable entries
	one := newOR(t, p, results, round1Open)
	assertEnded(t, one.startIn("RUN-1", "proj-a"), one.ev, "done")
	var made *machine.Event
	for i, e := range one.ev.events {
		if e.Kind == "AttemptMade" && e.Step == "review" && e.Round == 1 {
			made = &one.ev.events[i]
		}
	}
	if made == nil || len(eventsOf(one.ev.events, "StepRejected")) != 1 {
		t.Fatalf("setup: review round 1 was not left open once: %v", one.ev.kinds())
	}
	var startedFrom []machine.File
	for _, c := range one.w.callsTo("review") {
		if c.Round == 1 {
			startedFrom = c.Before
		}
	}
	if len(startedFrom) == 0 {
		t.Fatalf("setup: review round 1 started from no files")
	}
	a, found := results.Get(made.ReuseKey)
	if !found || !a.Open {
		t.Fatalf("review round 1 kept no open result: %+v found=%v", a, found)
	}
	if !sameFiles(a.Files, startedFrom) {
		t.Errorf("kept files = %v, want the files round 1 started from %v", a.Files, startedFrom)
	}
	for _, c := range one.h.commits {
		if c.OpenRound || (c.Step.Name == "review" && c.Round == 1) {
			t.Errorf("RUN-1 committed a review round that writes nothing: %+v", c)
		}
	}

	two := newOR(t, p, results, round1Open)
	assertEnded(t, two.startIn("RUN-2", "proj-b"), two.ev, "done")
	if len(two.w.calls) != 0 {
		t.Fatalf("RUN-2 ran %d attempts, want every step and round reused", len(two.w.calls))
	}
	var open *machine.Event
	for i, e := range two.ev.events {
		if e.Kind == "ResultReused" && e.Step == "review" && e.Round == 1 {
			open = &two.ev.events[i]
		}
	}
	if open == nil || !open.OpenRound || open.ReuseKey != made.ReuseKey {
		t.Fatalf("RUN-2 did not reuse review round 1 as an open round: %+v", open)
	}
	for _, c := range two.h.commits {
		if c.OpenRound || (c.Step.Name == "review" && c.Round == 1) {
			t.Errorf("RUN-2 committed a reused review round that writes nothing: %+v", c)
		}
	}
	if !slices.Equal(commitViews(two.h.commits), commitViews(one.h.commits)) {
		t.Errorf("RUN-2 commits %v, want RUN-1's %v", commitViews(two.h.commits), commitViews(one.h.commits))
	}
	if d := machine.Compare(one.ev.events, two.ev.events); len(d) != 0 {
		t.Errorf("RUN-2 differs from RUN-1: %+v", d)
	}
	validate(t, two.ev.events)
}

func TestReplayOfAHandedOverReviewReusesEveryOpenRound(t *testing.T) {
	results := machine.NewResults()
	p := witnessPatternOf()
	rounds := p.ReviewRounds
	one := newOR(t, p, results, everyRoundOpen)
	r1 := one.startIn("RUN-1", "proj-a")
	if r1.Status != machine.StatusWaiting || one.ev.count("RunHandedOver") != 1 {
		t.Fatalf("setup: RUN-1 = %s, want handed over after %d open rounds: %v", r1.Status, rounds, one.ev.kinds())
	}
	if n := len(eventsOf(one.ev.events, "StepRejected")); n != rounds {
		t.Fatalf("setup: RUN-1 left %d rounds open, want %d", n, rounds)
	}

	two := newOR(t, p, results, everyRoundOpen)
	r2 := two.startIn("RUN-2", "proj-b")
	if len(two.w.calls) != 0 || len(two.c.calls) != 0 {
		t.Fatalf("RUN-2 ran %d attempts and %d checks, want nothing run: %v", len(two.w.calls), len(two.c.calls), two.ev.kinds())
	}
	if r2.Status != machine.StatusWaiting || two.ev.count("RunHandedOver") != 1 {
		t.Fatalf("RUN-2 = %s, want handed over: %v", r2.Status, two.ev.kinds())
	}
	if r2.Reason != r1.Reason {
		t.Errorf("RUN-2's reason = %q, want RUN-1's %q", r2.Reason, r1.Reason)
	}
	if !findingsEqual(r2.Open, r1.Open) || len(r2.Open) == 0 {
		t.Errorf("RUN-2's open findings = %+v, want RUN-1's %+v", r2.Open, r1.Open)
	}
	var openRounds []int
	for _, e := range eventsOf(two.ev.events, "ResultReused") {
		if e.Step == "review" {
			if !e.OpenRound {
				t.Errorf("review round %d reused without open_round", e.Round)
			}
			openRounds = append(openRounds, e.Round)
		}
	}
	if !slices.Equal(openRounds, []int{1, 2, 3}[:rounds]) {
		t.Errorf("RUN-2 reused review rounds %v, want every round open 1..%d", openRounds, rounds)
	}
	if d := machine.Compare(one.ev.events, two.ev.events); len(d) != 0 {
		t.Errorf("RUN-2 differs from RUN-1: %+v", d)
	}
	validate(t, two.ev.events)
}
