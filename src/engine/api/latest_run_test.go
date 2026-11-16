package api

// T084 (defect found by T054l, 2026-11-14): a slice's `latest_run` is its latest run's ID, as the
// contract (slice.schema.json, SliceSummary) and data-model say; sliceView never sets it. Written
// before the code by a separate author, on the run rig of runs_ops_test.go.

import (
	"encoding/json"
	"fmt"
	"testing"
)

// latestRuns reads the slice's latest_run from getSlice and from listSlices; "" when absent or null.
func (x *runsRig) latestRuns() (got, listed string) {
	x.t.Helper()
	r := x.do("GET", spURL("S-1", ""), "")
	if r.Code != 200 {
		x.t.Fatalf("getSlice = %d: %s", r.Code, r.Body.String())
	}
	var s struct {
		LatestRun *string `json:"latest_run"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &s); err != nil {
		x.t.Fatal(err)
	}
	l := x.do("GET", slicesURL(""), "")
	if l.Code != 200 {
		x.t.Fatalf("listSlices = %d: %s", l.Code, l.Body.String())
	}
	var list struct {
		Items []struct {
			ID        string  `json:"id"`
			LatestRun *string `json:"latest_run"`
		} `json:"items"`
	}
	if err := json.Unmarshal(l.Body.Bytes(), &list); err != nil {
		x.t.Fatal(err)
	}
	for _, it := range list.Items {
		if it.ID == "S-1" && it.LatestRun != nil {
			listed = *it.LatestRun
		}
	}
	if s.LatestRun != nil {
		got = *s.LatestRun
	}
	return got, listed
}

func (x *runsRig) wantLatest(want string) {
	x.t.Helper()
	if got, listed := x.latestRuns(); got != want || listed != want {
		x.t.Fatalf("latest_run: getSlice %q, listSlices %q; want %q in both (empty: absent)", got, listed, want)
	}
}

func TestLatestRunIsAbsentBeforeAnyRun(t *testing.T) {
	x := newRunsRig(t)
	x.wantLatest("")
}

func TestLatestRunIsTheRunThatJustStarted(t *testing.T) {
	x := newRunsRig(t)
	release := x.gatedWorker() // the run is still running when the slice is read
	op := x.startOK("notes-app")
	x.wantLatest(op.Target)
	release()
	x.waitEnded(op.Target, "done")
	x.wantLatest(op.Target)
}

func TestLatestRunIsTheLatestAfterASecondRunAndAfterARestart(t *testing.T) {
	x := newRunsRig(t)
	first := x.runDone("notes-app")
	x.wantLatest(first.ID)
	second := x.runDone("second-app")
	if second.ID == first.ID {
		t.Fatalf("setup: two runs with one ID %s", first.ID)
	}
	x.wantLatest(second.ID)
	x.boot(x.w, x.c) // a restarted engine reads it from the record
	registerAll(t, x.r, "S-1")
	x.wantLatest(second.ID)
}

// RUN-10 is the latest after RUN-9: a pick by string comparison ("RUN-9" > "RUN-10") is wrong.
func TestLatestRunIsRun10AfterTenRuns(t *testing.T) {
	x := newRunsRig(t)
	var last runView
	for i := 1; i <= 10; i++ {
		last = x.runDone(fmt.Sprintf("app-%d", i))
	}
	if last.ID != "RUN-10" {
		t.Fatalf("setup: the 10th run is %s, want RUN-10", last.ID)
	}
	x.wantLatest("RUN-10")
}
