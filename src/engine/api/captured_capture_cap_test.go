package api

// T037 (feature 011, user story 1, review finding B1; FR-001, FR-003; spec Clarifications
// "Session 2026-11-26"; engine contract `captureRun` and `replaceParameters`,
// `x-limits.captured-pattern-parameters-max` = 32): capturing a done run whose slice gives more
// than 32 parameter candidates succeeds with the first 32 (the names in byte order, then the
// declared values in the contract's order), and `PUT /parameters` may then drop one and add a
// left-out one. Judged from the contract, never from an implementation; the rig is
// captured_capture_test.go's (newCapRig's body, with a wider slice contract).
//
// The slice's contract is the notes proposal's with a schema `Holder` whose properties are
// ab01 .. ab<n>. Its names are those, `holder`, and the notes contract's `list`, `notes`, `keep`
// and `note` (from the operation IDs listNotes and keepNote). By hand, byte order is
// ab01 .. ab<n>, holder, keep, list, note, notes.

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/agentic-platform/engine/api/gen"
	"github.com/agentic-platform/engine/canonical"
	"github.com/agentic-platform/engine/store"
)

// newWideCapRig is newCapRig over a slice whose contract also has the schema Holder with the
// properties ab01 .. ab<props>.
func newWideCapRig(t *testing.T, props int) *capRig {
	t.Helper()
	st, err := store.Open(t.TempDir(), spSchemas(t))
	if err != nil {
		t.Fatal(err)
	}
	p := spNotes(t)
	if strings.Contains(p.Contract, "components:") {
		t.Fatal("setup: the notes contract has components already")
	}
	var b strings.Builder
	b.WriteString(p.Contract)
	if !strings.HasSuffix(p.Contract, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("components:\n  schemas:\n    Holder:\n      type: object\n      properties:\n")
	for i := props; i >= 1; i-- { // descending: the document's order is not the byte order
		fmt.Fprintf(&b, "        ab%02d:\n          type: string\n", i)
	}
	p.Contract = b.String()
	sp := newSP(t, []spResult{{p: p}}, st)
	sp.approveAfterSeed(t)
	x := &runsRig{t: t, sp: sp, st: st, projects: t.TempDir(),
		w: &fakeStepWorker{st: st, data: map[string][]byte{}}, c: &fakeStepChecker{}}
	c := &capRig{t: t, x: x, gs: &gateStore{Store: st}, mu: new(sync.Mutex), byRun: map[string]map[string][]canonical.File{}, fixed: map[string]bool{}}
	x.w.set(c.work)
	c.boot()
	return c
}

func capValues(ps []gen.Parameter) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Value)
	}
	slices.Sort(out)
	return out
}

func abNames(from, to int) []string {
	var out []string
	for i := from; i <= to; i++ {
		out = append(out, fmt.Sprintf("ab%02d", i))
	}
	return out
}

// T037, FR-003, contract captureRun: 40 properties and 5 other names are 45 candidates; the
// capture succeeds (it did not fail with OperationFailed for too many) and proposes exactly
// ab01 .. ab32, each of the name kind, source name, origin proposed, with places; the captured
// pattern is proposed. By hand: ab33 .. ab40, holder, keep, list, note and notes are left out.
func TestCaptureOfASliceWithMoreThan32CandidatesProposesTheFirst32(t *testing.T) {
	c := newWideCapRig(t, 40)
	run := c.runDone("notes-app")
	id := capBase + "-s-1"
	c.succeeded(c.capture(run, ""), "capture-run", id)
	cp := c.get(id)
	if cp.State != gen.CapturedPatternStateProposed {
		t.Errorf("state %s, want proposed", cp.State)
	}
	if got, want := capValues(cp.Parameters), abNames(1, 32); !slices.Equal(got, want) {
		t.Fatalf("%d parameters %v,\nwant the first 32 in byte order, %v", len(got), got, want)
	}
	for _, p := range cp.Parameters {
		if p.Kind != gen.ParameterKindName || p.Source != gen.ParameterSourceName || p.Origin != gen.ParameterOriginProposed || len(p.Places) == 0 {
			t.Errorf("parameter %+v: want kind name, source name, origin proposed, with places", p)
		}
	}
}

// T037, FR-003: the boundary. 27 properties and the 5 other names are exactly 32 candidates: all
// are proposed. 28 and the 5 are 33: the last in byte order, `notes`, is left out. And `note`, kept,
// then has the places inside `notes` (in `/notes`, `listNotes`), which a left-out candidate does
// not take, so it has more places than when `notes` is kept.
func TestCaptureCapBoundary32And33Candidates(t *testing.T) {
	var notePlaces int
	t.Run("32 candidates", func(t *testing.T) {
		c := newWideCapRig(t, 27)
		run := c.runDone("notes-app")
		id := capBase + "-s-1"
		c.succeeded(c.capture(run, ""), "capture-run", id)
		cp := c.get(id)
		want := append(abNames(1, 27), "holder", "keep", "list", "note", "notes")
		if got := capValues(cp.Parameters); !slices.Equal(got, want) {
			t.Fatalf("%d parameters %v, want all 32, %v", len(got), got, want)
		}
		notePlaces = len(paramByValue(cp.Parameters, "note").Places)
	})
	t.Run("33 candidates", func(t *testing.T) {
		c := newWideCapRig(t, 28)
		run := c.runDone("notes-app")
		id := capBase + "-s-1"
		c.succeeded(c.capture(run, ""), "capture-run", id)
		cp := c.get(id)
		want := append(abNames(1, 28), "holder", "keep", "list", "note")
		if got := capValues(cp.Parameters); !slices.Equal(got, want) {
			t.Fatalf("%d parameters %v, want 32 without notes, %v", len(got), got, want)
		}
		if n := len(paramByValue(cp.Parameters, "note").Places); notePlaces == 0 || n <= notePlaces {
			t.Errorf("note has %d places with notes left out, %d with notes kept: want more", n, notePlaces)
		}
	})
}

// T037, FR-003, contract replaceParameters: after the capture of 45 candidates (32 proposed), a
// PUT may drop one proposed parameter (ab32) and add a left-out one (ab33, then also `note`, a
// name of the slice), keeping 32 parameters: it succeeds, and the new ones have places. A PUT
// with all 32 kept and one more is 33 parameters: InvalidRequest at once.
func TestReplaceParametersMayDropOneAndAddALeftOutCandidate(t *testing.T) {
	c := newWideCapRig(t, 40)
	run := c.runDone("notes-app")
	id := capBase + "-s-1"
	c.succeeded(c.capture(run, ""), "capture-run", id)

	var pairs []string
	for _, n := range abNames(1, 31) {
		pairs = append(pairs, n, n)
	}
	c.succeeded(c.put(id, capParams(append(slices.Clone(pairs), "ab33", "ab33")...)), "replace-parameters", id)
	cp := c.get(id)
	want := append(abNames(1, 31), "ab33")
	if got := capValues(cp.Parameters); !slices.Equal(got, want) {
		t.Fatalf("%d parameters %v, want ab01 .. ab31 and ab33", len(got), got)
	}
	if p := paramNamed(cp.Parameters, "ab33"); p == nil || p.Kind != gen.ParameterKindName || p.Source != gen.ParameterSourceName || len(p.Places) == 0 {
		t.Errorf("ab33 = %+v, want a name with places", p)
	}
	if cp.State != gen.CapturedPatternStateProposed {
		t.Errorf("state %s, want proposed", cp.State)
	}

	// Another left-out name, `note`, in place of ab31.
	c.succeeded(c.put(id, capParams(append(slices.Clone(pairs[:len(pairs)-2]), "ab33", "ab33", "note", "note")...)), "replace-parameters", id)
	if p := paramByValue(c.get(id).Parameters, "note"); p == nil || len(p.Places) == 0 {
		t.Errorf("note = %+v, want a parameter with places", p)
	}

	// 33 parameters are refused at once, no operation.
	over := capParams(append(slices.Clone(pairs), "ab32", "ab32", "ab33", "ab33")...)
	c.refused(func() reply { return c.put(id, over) }, 400, gen.ErrorCodeInvalidRequest)
}
