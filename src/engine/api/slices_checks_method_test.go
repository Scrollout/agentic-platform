package api

// Tests for task T014 (feature 006, US2): a slice's check results are shown only under the
// method they were produced under. A slice stored under method A and served by an engine under
// method B is checked again before any read answers; getSlice names the method the checks ran
// under in `checks_method`, and names none while a check could not run. Written before the code
// by a separate author; the implementer must not change them (ADR 0029).
//
// # Interface the tests use
//
//	slices.Config.Method string      // exists: the identity's 64 lowercase hex digits
//	slices.Slice.ChecksMethod string // hex identity every check ran under; "" while a check could
//	                                 // not run
//
// Stale state is built by creating and approving slices with a service under A on shared records,
// then serving the same records with a service under B.

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/agentic-platform/engine/slices"
)

const (
	cmMethodA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	cmMethodB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// cmRecords is an in-memory slices.Records shared by the services under test.
type cmRecords struct {
	mu   sync.Mutex
	recs map[string][]byte
	seq  []byte
}

func (m *cmRecords) SliceRecords() (map[string][]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string][]byte{}
	for k, v := range m.recs {
		out[k] = v
	}
	return out, nil
}

func (m *cmRecords) PutSliceRecord(id string, r []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.recs == nil {
		m.recs = map[string][]byte{}
	}
	m.recs[id] = r
	return nil
}

func (m *cmRecords) DeleteSliceRecord(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.recs, id)
	return nil
}

func (m *cmRecords) SliceSequence() ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.seq, nil
}

func (m *cmRecords) PutSliceSequence(s []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq = s
	return nil
}

// cmLint is a lint that passes, finds an error, or cannot run.
type cmLint struct {
	mode string // "", "error" or "cannot-run"
}

func (l cmLint) Lint(context.Context, string) ([]slices.LintFinding, error) {
	switch l.mode {
	case "error":
		return []slices.LintFinding{{Level: "error", Code: "oas3-schema", Text: "invalid"}}, nil
	case "cannot-run":
		return nil, context.DeadlineExceeded
	}
	return nil, nil
}

// cmStaleRig stores approved slices S-1..n under method A, then serves them under method B whose
// lint is lintB.
func cmStaleRig(t *testing.T, n int, lintB cmLint) *spRig {
	t.Helper()
	recs := &cmRecords{}
	a, err := slices.New(slices.Config{
		Schemas: spSchemas(t), Analyst: &spAnalyst{results: []spResult{{p: spNotes(t)}}},
		Diff: spDiff{}, Lint: cmLint{}, Pattern: spPattern(t), Records: recs, Method: cmMethodA,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Create every slice before approving any: Create offers an approved slice again for the same
	// description, so n distinct slices need distinct descriptions and no approval in between.
	var created []slices.Slice
	for i := 0; i < n; i++ {
		res, err := a.Create(context.Background(), "Let me keep notes, number "+strconv.Itoa(i+1)+".")
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		created = append(created, res.Slice)
	}
	for _, sl := range created {
		var err error
		for _, p := range []struct {
			part slices.Part
			hash string
		}{{slices.PartGoals, sl.Parts.Goals}, {slices.PartRules, sl.Parts.Rules}, {slices.PartContract, sl.Parts.Contract}} {
			if sl, err = a.Approve(context.Background(), sl.ID, p.part, p.hash); err != nil {
				t.Fatalf("approve %s: %v", p.part, err)
			}
		}
		if sl.State != "approved" || sl.ChecksMethod != cmMethodA {
			t.Fatalf("under A: state %q, checks_method %q; want approved under A", sl.State, sl.ChecksMethod)
		}
	}
	return newNotes(t, func(c *slices.Config) {
		c.Records, c.Method, c.Lint = recs, cmMethodB, lintB
	})
}

// cmRaw returns getSlice's answer as a map, after checking it against slice.schema.json.
func (sp *spRig) cmRaw(t *testing.T, id string) map[string]json.RawMessage {
	t.Helper()
	_, raw := sp.getSlice(t, id)
	if err := sp.schemas.CheckSlice(raw); err != nil {
		t.Errorf("the response does not match slice.schema.json: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func (sp *spRig) cmState(t *testing.T, id string) string {
	t.Helper()
	s, _ := sp.getSlice(t, id)
	return s.State
}

func TestGetSliceStoredUnderAnotherMethodAnswersTheEnginesMethod(t *testing.T) {
	sp := cmStaleRig(t, 1, cmLint{})
	m := sp.cmRaw(t, "S-1")
	var got string
	if err := json.Unmarshal(m["checks_method"], &got); err != nil || got != cmMethodB {
		t.Errorf("checks_method = %s, want %q (the engine's identity, not the stored A)", m["checks_method"], cmMethodB)
	}
	// Under the new method the checks passed, so the approval stands.
	if st := sp.cmState(t, "S-1"); st != "approved" {
		t.Errorf("state %q, want approved: the checks pass under B", st)
	}
	sp.noWork(t)
}

func TestGetSliceCreatedUnderTheEnginesMethodAnswersIt(t *testing.T) {
	sp := newNotes(t, func(c *slices.Config) { c.Method = cmMethodA })
	sp.seed(t, "Let me keep notes.")
	m := sp.cmRaw(t, "S-1")
	var got string
	if err := json.Unmarshal(m["checks_method"], &got); err != nil || got != cmMethodA {
		t.Errorf("checks_method = %s, want %q", m["checks_method"], cmMethodA)
	}
}

func TestGetSliceNamesNoMethodWhileACheckCouldNotRun(t *testing.T) {
	sp := cmStaleRig(t, 1, cmLint{mode: "cannot-run"})
	m := sp.cmRaw(t, "S-1")
	if v, ok := m["checks_method"]; ok {
		t.Errorf("checks_method = %s while the lint could not run; the property must be absent", v)
	}
	if st := sp.cmState(t, "S-1"); st == "approved" {
		t.Errorf("state approved although a check could not run (a check that could not run fails)")
	}
	// Absent again on the next read: it checks again, and the lint still cannot run.
	if _, ok := sp.cmRaw(t, "S-1")["checks_method"]; ok {
		t.Errorf("checks_method appeared on a second read although the lint still cannot run")
	}
}

func TestGetSliceNamesTheMethodWhenACheckRanAndFailed(t *testing.T) {
	sp := cmStaleRig(t, 1, cmLint{mode: "error"})
	m := sp.cmRaw(t, "S-1")
	var got string
	if err := json.Unmarshal(m["checks_method"], &got); err != nil || got != cmMethodB {
		t.Errorf("checks_method = %s, want %q: a check that ran and failed ran under B", m["checks_method"], cmMethodB)
	}
}

func TestGetSliceCreatedWhileTheLintCannotRunNamesNoMethod(t *testing.T) {
	sp := newNotes(t, func(c *slices.Config) {
		c.Method, c.Lint = cmMethodA, cmLint{mode: "cannot-run"}
	})
	sp.seed(t, "Let me keep notes.")
	if _, ok := sp.cmRaw(t, "S-1")["checks_method"]; ok {
		t.Errorf("checks_method present although the lint could not run")
	}
}

func TestListSlicesShowsTheStateAfterTheRecheck(t *testing.T) {
	sp := cmStaleRig(t, 2, cmLint{mode: "error"})
	l := sp.list(t, clientToken, "")
	if len(l.Items) != 2 {
		t.Fatalf("%d items, want 2", len(l.Items))
	}
	for _, it := range l.Items {
		if it.State == "approved" {
			t.Errorf("%s is approved in the list although its contract lint fails under the new method", it.ID)
		}
		if got := sp.cmState(t, it.ID); got != it.State {
			t.Errorf("%s: list says %q, getSlice says %q", it.ID, it.State, got)
		}
	}
	sp.noWork(t)
}

func TestListSlicesKeepsApprovedSlicesWhoseChecksStillPass(t *testing.T) {
	sp := cmStaleRig(t, 2, cmLint{})
	for _, it := range sp.list(t, clientToken, "").Items {
		if it.State != "approved" {
			t.Errorf("%s is %q, want approved: the re-check passes", it.ID, it.State)
		}
	}
}

func TestStartRunRefusesASliceThatFailsItsChecksUnderTheNewMethod(t *testing.T) {
	sp := cmStaleRig(t, 1, cmLint{mode: "error"})
	sp.do("POST", spURL("S-1", "/runs"), clientToken, runBody, nil).problem(t, 409, "NotApproved")
	sp.noOperation(t)
	sp.noWork(t)
}

func TestStartRunRefusesASliceWhoseCheckCannotRunUnderTheNewMethod(t *testing.T) {
	sp := cmStaleRig(t, 1, cmLint{mode: "cannot-run"})
	sp.do("POST", spURL("S-1", "/runs"), clientToken, runBody, nil).problem(t, 409, "NotApproved")
	sp.noOperation(t)
}

func TestStartRunOfASliceThatStillPassesUnderTheNewMethodIsNotRefusedForItsChecks(t *testing.T) {
	sp := cmStaleRig(t, 1, cmLint{})
	sp.do("POST", spURL("S-1", "/runs"), clientToken, runBody, nil).problem(t, 409, "WorkersNotReady")
	sp.noOperation(t)
}

func TestACheckFailureShowsItsReasonOnTheRecheckedSlice(t *testing.T) {
	sp := cmStaleRig(t, 1, cmLint{mode: "error"})
	s, _ := sp.getSlice(t, "S-1")
	failed := false
	for _, c := range s.Checks {
		if !c.Passed && c.Reason != "" && strings.Contains(c.Reason, "oas3-schema") {
			failed = true
		}
	}
	if !failed {
		t.Errorf("checks %+v: want the re-run lint failure naming its rule, not the stored pass", s.Checks)
	}
}
