package api

// T088 (3) (owner, 2026-11-15; research D36, ADR 0040; data-model "An open round reused", Kept): a
// restarted engine rebuilds an open review round's kept result from the record (reloadResults):
// from the StepRejected with `open_round`, the FindingRecorded events directly before it and that
// attempt's NoBlockingFindingOpen CheckFailed; from a reused open round, the FindingRecorded events
// directly before its ResultReused and that event's `check`. The first result kept under a key
// stays, open or accepted. Written before the code by a separate test author. Seams:
// runs_ops_test.go (runsRig and its fakes).
//
// ASSUMED SHAPE (as in machine/open_round_reuse_test.go): machine.Event gains `OpenRound bool`
// (json "open_round,omitempty"); machine.Accepted gains `Open bool`, `Findings []machine.Finding`
// and `Check *machine.EventCheck`.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/agentic-platform/engine/machine"
	"github.com/agentic-platform/engine/store"
)

const openReason = "1 blocking finding open"

// openRoundWorker: review round 1 leaves R-1 open with a witness and makes a suggestion R-2; the
// fix changes a line; every other call is the default.
func (x *runsRig) openRoundWorker() {
	x.w.set(func(_ context.Context, r machine.Request) (machine.Result, error) {
		res, err := x.w.produce(r)
		if err != nil {
			return res, err
		}
		if r.Step.Reviews() && r.Round == 1 {
			res.Findings = []machine.Finding{
				{Rule: "R-1", File: "internal/app/app.go", Line: 3, Text: "an empty text is accepted", Blocking: true,
					Check: "AllTestsPass", Witness: &machine.Witness{File: "review/r1/empty_test.go", Test: "TestR1EmptyText"}},
				{Rule: "R-2", Text: "name the handler for the operation", Blocking: false},
			}
		}
		if r.Step.Name == "code" && len(r.Open) > 0 {
			res.ChangedLines = []machine.Line{{File: "internal/app/app.go", Line: 3}}
		}
		return res, nil
	})
	x.c.set(func(_ context.Context, r machine.CheckRequest) (machine.CheckResult, error) {
		if r.Name == "NoBlockingFindingOpen" {
			for _, f := range r.Result.Findings {
				if f.Blocking {
					return machine.CheckResult{Passed: false, Reason: openReason}, nil
				}
			}
		}
		return machine.CheckResult{Passed: true, Reason: r.Name + " holds"}, nil
	})
}

// rawEvents are a run's events as listRunEvents gives them, in the contract's form.
func (x *runsRig) rawEvents(id string) []map[string]any {
	x.t.Helper()
	var out []map[string]any
	q := ""
	for range 20 {
		p := x.do("GET", apiPath("/runs/"+id+"/events"+q), "")
		if p.Code != 200 {
			x.t.Fatalf("listRunEvents %s = %d: %s", id, p.Code, p.Body.String())
		}
		var pg struct {
			Items []map[string]any `json:"items"`
			Next  *string          `json:"next_page_token"`
		}
		if err := json.Unmarshal(p.Body.Bytes(), &pg); err != nil {
			x.t.Fatal(err)
		}
		out = append(out, pg.Items...)
		if pg.Next == nil {
			return out
		}
		q = "&page_token=" + *pg.Next
	}
	x.t.Fatal("listRunEvents did not end after 20 pages")
	return nil
}

func num(v any) int { f, _ := v.(float64); return int(f) }

// openReusedAt is the index of the ResultReused of review round n, or -1.
func openReusedAt(evs []map[string]any, n int) int {
	return slices.IndexFunc(evs, func(e map[string]any) bool {
		return e["kind"] == "ResultReused" && e["step"] == "review" && num(e["round"]) == n
	})
}

// findingsOfRound are the `finding` objects of review round n's FindingRecorded events, in order.
func findingsOfRound(evs []map[string]any, n int) []any {
	var out []any
	for _, e := range evs {
		if e["kind"] == "FindingRecorded" && e["step"] == "review" && num(e["round"]) == n {
			out = append(out, e["finding"])
		}
	}
	return out
}

// openCheck is review round n's NoBlockingFindingOpen CheckFailed `check`.
func openCheck(evs []map[string]any, n int) any {
	var got any
	for _, e := range evs {
		if c, _ := e["check"].(map[string]any); e["kind"] == "CheckFailed" && e["step"] == "review" && num(e["round"]) == n &&
			c["name"] == "NoBlockingFindingOpen" {
			got = e["check"]
		}
	}
	return got
}

// wantOpenReuse checks a run that reused review round 1 left open in an original whose events are
// orig: nothing ran, the round is a ResultReused with open_round, the original's check and detail,
// directly after FindingRecorded events equal to the original's round-1 findings.
func (x *runsRig) wantOpenReuse(id string, orig []map[string]any) {
	x.t.Helper()
	evs := x.rawEvents(id)
	for _, e := range evs {
		switch e["kind"] {
		case "StepStarted", "AttemptMade", "CheckPassed", "CheckFailed", "StepAccepted", "StepRejected":
			x.t.Errorf("%s records %s %v: nothing should run", id, e["kind"], e["step"])
		}
	}
	oi := openReusedAt(evs, 1)
	if oi < 0 {
		x.t.Fatalf("%s has no ResultReused of review round 1", id)
	}
	open := evs[oi]
	if open["open_round"] != true {
		x.t.Errorf("%s's reused open round lacks open_round in its contract form: %v", id, open)
	}
	if want := openCheck(orig, 1); want == nil || !reflect.DeepEqual(open["check"], want) {
		x.t.Errorf("%s's reused open round check = %v, want the original's %v", id, open["check"], want)
	}
	if open["detail"] != "review round 1 left blocking findings open" {
		x.t.Errorf("%s's reused open round detail = %v", id, open["detail"])
	}
	kept := findingsOfRound(orig, 1)
	if len(kept) != 2 || oi < len(kept) {
		x.t.Fatalf("setup: the original's round 1 has %d findings", len(kept))
	}
	var got []any
	for _, e := range evs[oi-len(kept) : oi] {
		if e["kind"] != "FindingRecorded" || e["attempt"] != nil {
			x.t.Errorf("%s: event before the reused open round = %v, want a FindingRecorded without attempt", id, e)
		}
		got = append(got, e["finding"])
	}
	if !reflect.DeepEqual(got, kept) {
		x.t.Errorf("%s's findings before the reused open round = %v, want the original's %v", id, got, kept)
	}
}

func TestRestartedEngineReusesAnOpenRound(t *testing.T) {
	x := newRunsRig(t)
	x.openRoundWorker()
	x.runDone("notes-app")
	one := x.rawEvents("RUN-1")
	if openCheck(one, 1) == nil {
		t.Fatalf("setup: RUN-1 left no review round open")
	}
	if i := slices.IndexFunc(one, func(e map[string]any) bool { return e["kind"] == "StepRejected" && e["open_round"] == true }); i < 0 {
		t.Errorf("RUN-1's open StepRejected lacks open_round in its contract form")
	}

	x.boot(x.w, x.c) // a restarted engine: the result store is rebuilt from the record
	registerAll(t, x.r, "S-1")
	before := x.w.callCount()
	rop := x.opOf(x.do("POST", apiPath("/runs/RUN-1/replay"), `{"project":"again-app"}`), "run")
	x.waitEnded(rop.Target, "done")
	if n := x.w.callCount() - before; n != 0 {
		t.Errorf("the replay after a restart ran the step worker %d times, want every step and round reused", n)
	}
	x.wantOpenReuse(rop.Target, one)
	steps := 0
	for _, e := range one {
		if e["kind"] == "StepAccepted" || (e["kind"] == "StepRejected" && e["open_round"] == true) {
			steps++
		}
	}
	reused := 0
	for _, e := range x.rawEvents(rop.Target) {
		if e["kind"] == "ResultReused" {
			reused++
		}
	}
	if reused != steps {
		t.Errorf("the replay reused %d results, want one per step and round of RUN-1 (%d)", reused, steps)
	}
	for _, pair := range [][2]string{{"RUN-1", rop.Target}, {rop.Target, "RUN-1"}} {
		if items, raw := x.compare(pair[0], pair[1]); len(items) != 0 {
			t.Errorf("compare %s with %s = %s, want no difference", pair[0], pair[1], raw)
		}
	}
}

// hiddenRun is the store with one run left out, as if its record were gone.
type hiddenRun struct {
	*store.Store
	run string
}

func (h hiddenRun) Runs() []store.RunEntry {
	var out []store.RunEntry
	for _, e := range h.Store.Runs() {
		if e.Run != h.run {
			out = append(out, e)
		}
	}
	return out
}

func (h hiddenRun) Events(id string) ([]store.Event, error) {
	if id == h.run {
		return nil, nil
	}
	return h.Store.Events(id)
}

func TestRestartedEngineRebuildsAReusedOpenRound(t *testing.T) {
	x := newRunsRig(t)
	x.openRoundWorker()
	x.runDone("notes-app")
	one := x.rawEvents("RUN-1")
	rop := x.opOf(x.do("POST", apiPath("/runs/RUN-1/replay"), `{"project":"again-app"}`), "run")
	x.waitEnded(rop.Target, "done")
	if openReusedAt(x.rawEvents(rop.Target), 1) < 0 {
		t.Fatalf("setup: %s did not reuse the open round", rop.Target)
	}

	// A restarted engine that reads only RUN-2's record.
	ctx, cancel := context.WithCancel(context.Background())
	x.r = newRig(t, func(cfg *Config) {
		cfg.Context = ctx
		cfg.Slices, cfg.Store = x.sp.svc, hiddenRun{x.st, "RUN-1"}
		cfg.WorkerGrants = stepGrants("S-1")
		cfg.Projects = x.projects
		cfg.Model = "test-model"
		cfg.StepWorker, cfg.StepChecker = x.w, x.c
	})
	stopEngineAtEnd(t, x.r, cancel)
	registerAll(t, x.r, "S-1")
	before := x.w.callCount()
	three := x.opOf(x.do("POST", apiPath("/runs/"+rop.Target+"/replay"), `{"project":"third-app"}`), "run")
	x.waitEnded(three.Target, "done")
	if n := x.w.callCount() - before; n != 0 {
		t.Errorf("%s ran the step worker %d times, want the round rebuilt from %s's record reused", three.Target, n, rop.Target)
	}
	x.wantOpenReuse(three.Target, one)
}

// keptRecord appends one run's events to st, numbered from 1, the first a RunStarted with its info.
func keptRecord(t *testing.T, st *store.Store, run string, events ...machine.Event) {
	t.Helper()
	at := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	all := append([]machine.Event{{Kind: "RunStarted", Slice: "S-1", Pattern: "api-feature-go@1.0.0", Refs: []string{"G-1"}}}, events...)
	for i, e := range all {
		e.Seq, e.Run, e.At = i+1, run, at.Add(time.Duration(i)*time.Second)
		d := storedEvent{Event: e, Files: e.Files}
		if e.Kind == "RunStarted" {
			d.Info = &runInfo{Project: "p-" + run, Operation: "OP-" + fmt.Sprint(i+1)}
		}
		b, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.Append("S-1", store.Event{Run: run, Kind: e.Kind, Step: e.Step, Data: store.RawJSON(b)}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFirstKeptResultStays(t *testing.T) {
	h := func(s string) string { b := sha256.Sum256([]byte(s)); return hex.EncodeToString(b[:]) }
	key := h("the review round 1 key")
	openArts, accArts := []string{h("open findings"), h("open witnesses")}, []string{h("accepted findings"), h("accepted witnesses")}
	finding := machine.Finding{Rule: "R-1", File: "internal/app/app.go", Line: 3, Text: "an empty text is accepted", Blocking: true,
		Witness: &machine.Witness{File: "review/r1/empty_test.go", Test: "TestR1EmptyText"}}
	check := machine.EventCheck{Name: "NoBlockingFindingOpen", Passed: false, Reason: openReason}
	files := []machine.File{{Path: "review/r1/empty_test.go", SHA256: h("witness")}}
	openRound := []machine.Event{
		{Kind: "StepStarted", Step: "review", Round: 1, Refs: []string{"G-1"}},
		{Kind: "AttemptMade", Step: "review", Attempt: 1, Round: 1, Worker: "agent:review", Model: "test-model",
			AgentDigest: h("digest"), Pattern: "api-feature-go@1.0.0", Inputs: []string{h("in")}, Artifacts: openArts,
			ReuseKey: key, Refs: []string{"G-1"}, Usage: &machine.Usage{Tokens: 1}},
		{Kind: "CheckFailed", Step: "review", Attempt: 1, Round: 1, Check: &check, Refs: []string{"G-1"}},
		{Kind: "FindingRecorded", Step: "review", Attempt: 1, Round: 1, Finding: &finding, Refs: []string{"R-1"}},
		{Kind: "StepRejected", Step: "review", Attempt: 1, Round: 1, OpenRound: true, ReuseKey: key, Artifacts: openArts,
			Detail: "review round 1 left blocking findings open", Refs: []string{"G-1"}, Files: files},
	}
	accepted := []machine.Event{
		{Kind: "StepStarted", Step: "review", Round: 1, Refs: []string{"G-1"}},
		{Kind: "StepAccepted", Step: "review", Attempt: 1, Round: 1, Worker: "agent:review", Model: "test-model",
			AgentDigest: h("digest"), Pattern: "api-feature-go@1.0.0", Inputs: []string{h("in")}, Artifacts: accArts,
			ReuseKey: key, Refs: []string{"G-1"}, Files: files},
	}
	openKept := machine.Accepted{Key: key, Artifacts: openArts, Run: "RUN-1", Files: files, Open: true,
		Findings: []machine.Finding{finding}, Check: &check}
	accKept := machine.Accepted{Key: key, Artifacts: accArts, Run: "RUN-1", Files: files}

	for _, tc := range []struct {
		name         string
		first, later []machine.Event
		live         [2]machine.Accepted
		wantOpen     bool
		wantArts     []string
	}{
		{"open, then accepted", openRound, accepted, [2]machine.Accepted{openKept, accKept}, true, openArts},
		{"accepted, then open", accepted, openRound, [2]machine.Accepted{accKept, openKept}, false, accArts},
	} {
		t.Run(tc.name, func(t *testing.T) {
			live := machine.NewResults()
			second := tc.live[1]
			second.Run = "RUN-2"
			live.Put(tc.live[0])
			live.Put(second)
			if a, _ := live.Get(key); a.Open != tc.wantOpen || a.Run != "RUN-1" || !slices.Equal(a.Artifacts, tc.wantArts) {
				t.Errorf("live: Get = open %v run %s artifacts %v, want the first: open %v, RUN-1, %v", a.Open, a.Run, a.Artifacts, tc.wantOpen, tc.wantArts)
			}

			st, err := store.Open(t.TempDir(), spSchemas(t))
			if err != nil {
				t.Fatal(err)
			}
			keptRecord(t, st, "RUN-1", tc.first...)
			keptRecord(t, st, "RUN-2", tc.later...)
			r := &runs{st: st, results: machine.NewResults()}
			r.reloadResults()
			a, found := r.results.Get(key)
			if !found || a.Open != tc.wantOpen || a.Run != "RUN-1" || !slices.Equal(a.Artifacts, tc.wantArts) {
				t.Fatalf("after reloadResults: Get = %+v found=%v, want the first: open %v, RUN-1, %v", a, found, tc.wantOpen, tc.wantArts)
			}
			if tc.wantOpen {
				if len(a.Findings) != 1 || !reflect.DeepEqual(a.Findings[0], finding) || a.Check == nil || *a.Check != check {
					t.Errorf("after reloadResults: the open result's findings %+v check %+v, want %+v and %+v", a.Findings, a.Check, finding, check)
				}
			}
		})
	}
}
