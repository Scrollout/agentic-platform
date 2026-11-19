package api

// T015 (feature 002, FR-006, FR-007; data-model "Run record"): a record may hold an AttemptMade
// twice with the same number, around a RunInterrupted and RunResumed pair. listRunEvents lists it
// whole; compareRuns, getRun and the page take the later attempt, so a run continued after a stop
// compares equal to an uninterrupted one with the same artifacts. Seams: runs_ops_test.go
// (runsRig), open_round_reload_test.go (rawEvents).

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/agentic-platform/engine/machine"
	"github.com/agentic-platform/engine/store"
)

const (
	stopDetail   = "the platform stopped"
	resumeDetail = "continued after the platform stopped"
)

// continuedAfterAStop writes RUN-2 as a copy of RUN-1's record in which RUN-1's first AttemptMade
// is recorded twice: first with other artifacts, then a RunInterrupted and a RunResumed, then the
// attempt as RUN-1 made it. It returns the index of the first AttemptMade in RUN-2's record.
func (x *runsRig) continuedAfterAStop(from, run string) int {
	x.t.Helper()
	stored, err := x.st.Events(from)
	if err != nil {
		x.t.Fatal(err)
	}
	var out []storedEvent
	attempt := -1
	for _, e := range stored {
		var d storedEvent
		if err := json.Unmarshal([]byte(e.Data), &d); err != nil {
			x.t.Fatal(err)
		}
		if d.Event.Kind == "AttemptMade" && attempt < 0 {
			attempt = len(out)
			stale := d
			stale.Event.Artifacts = slices.Clone(d.Event.Artifacts)
			for i := range stale.Event.Artifacts {
				stale.Event.Artifacts[i] = strings.Repeat("0", 64)
			}
			stop := storedEvent{Event: machine.Event{Kind: "RunInterrupted", At: d.Event.At, Detail: stopDetail}}
			resume := storedEvent{Event: machine.Event{Kind: "RunResumed", At: d.Event.At.Add(time.Second), Detail: resumeDetail}}
			d.Event.At = d.Event.At.Add(2 * time.Second)
			out = append(out, stale, stop, resume, d)
			continue
		}
		if attempt >= 0 {
			d.Event.At = d.Event.At.Add(2 * time.Second)
		}
		out = append(out, d)
	}
	if attempt < 0 {
		x.t.Fatalf("setup: %s holds no AttemptMade", from)
	}
	for i, d := range out {
		d.Event.Seq, d.Event.Run = i+1, run
		if d.Event.Kind == "RunStarted" {
			d.Info = &runInfo{Project: "continued-app", Operation: "OP-continued"}
		}
		b, err := json.Marshal(d)
		if err != nil {
			x.t.Fatal(err)
		}
		if _, err := x.st.Append("S-1", store.Event{Run: run, Kind: d.Event.Kind, Step: d.Event.Step, Data: store.RawJSON(b)}); err != nil {
			x.t.Fatal(err)
		}
	}
	return attempt
}

func TestARepeatedAttemptIsListedWholeAndTheLaterOneIsTaken(t *testing.T) {
	x := newRunsRig(t)
	x.runDone("notes-app")
	at := x.continuedAfterAStop("RUN-1", "RUN-2")
	x.boot(x.w, x.c) // a restarted engine reads both records
	registerAll(t, x.r, "S-1")

	one, two := x.rawEvents("RUN-1"), x.rawEvents("RUN-2")
	t.Run("listRunEvents lists the record whole", func(t *testing.T) {
		if len(two) != len(one)+3 {
			t.Fatalf("RUN-2 lists %d events, want RUN-1's %d and 3 more", len(two), len(one))
		}
		for i, want := range []string{"AttemptMade", "RunInterrupted", "RunResumed", "AttemptMade"} {
			if two[at+i]["kind"] != want {
				t.Errorf("event %d = %v, want %s", at+i, two[at+i]["kind"], want)
			}
		}
		if a, b := two[at], two[at+3]; num(a["attempt"]) != num(b["attempt"]) {
			t.Errorf("the repeated AttemptMade has attempts %v and %v, want one number", a["attempt"], b["attempt"])
		}
		if two[at+1]["detail"] != stopDetail || two[at+2]["detail"] != resumeDetail {
			t.Errorf("pair details = %v, %v", two[at+1]["detail"], two[at+2]["detail"])
		}
		if slices.Equal(anySlice(two[at]["artifacts"]), anySlice(two[at+3]["artifacts"])) {
			t.Errorf("the earlier attempt must be listed with its own artifacts, not the later one's")
		}
	})
	t.Run("getRun takes the record's end", func(t *testing.T) {
		a, b := x.getRun("RUN-1"), x.getRun("RUN-2")
		if b.Status != "ended" || b.Outcome == nil || *b.Outcome != "done" || a.Status != b.Status || a.Slice != b.Slice || a.Pattern != b.Pattern {
			t.Errorf("getRun RUN-2 = %+v, want ended done like RUN-1 %+v", b, a)
		}
	})
	t.Run("compareRuns finds a continued run equal to an uninterrupted one", func(t *testing.T) {
		for _, pair := range [][2]string{{"RUN-1", "RUN-2"}, {"RUN-2", "RUN-1"}} {
			if items, raw := x.compare(pair[0], pair[1]); len(items) != 0 {
				t.Errorf("compare %s with %s = %s, want no differences", pair[0], pair[1], raw)
			}
		}
	})
}

func anySlice(v any) []any { s, _ := v.([]any); return s }
