package api

// Tests for T038h and T040b (written by a separate author, ADR 0029; new file, no existing test
// changed). Owner decisions of 2026-11-12: CreateSliceRequest.description has maxLength 4000
// (characters, as JSON Schema counts them: code points); at most 4 analyst calls run at once
// across the engine, the rest wait in their operations, which stay `running`.
//
// Contract facts the tests rest on (openapi.yaml): listSlices is "newest first, at most 50 per
// page"; a page_token this service did not issue is 400 InvalidRequest; Operation.error is a
// Problem, whose `failures` lists each failing check; the contract names no 415, and lists
// InvalidRequest under 400 as "the body or a parameter does not match this contract".

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentic-platform/engine/api/gen"
	"github.com/agentic-platform/engine/slices"
)

// ---- description length ----

func descBody(n int, ch string) string {
	return `{"description":"` + strings.Repeat(ch, n) + `"}`
}

func TestDescriptionAtTheLimitIsAcceptedAndOneOverIsRefusedBeforeTheAnalyst(t *testing.T) {
	for _, ch := range []string{"a", "é", "世", "😀"} { // 1, 2, 3 and 4 bytes; each one character
		t.Run(ch, func(t *testing.T) {
			sp := newNotes(t)
			sp.post("POST", slicesURL(""), descBody(4001, ch)).problem(t, http.StatusBadRequest, gen.ErrorCodeInvalidRequest)
			sp.noOperation(t)
			if n := sp.analyst.calls(); n != 0 {
				t.Fatalf("analyst called %d times for a refused description, want 0", n)
			}
			op := sp.accepted(t, sp.post("POST", slicesURL(""), descBody(4000, ch)), "propose-slice", "S-1")
			if op.Status != "succeeded" {
				t.Errorf("4000 characters: operation %+v, want succeeded", op)
			}
			if n := sp.analyst.calls(); n != 1 {
				t.Errorf("analyst called %d times, want 1", n)
			}
		})
	}
}

// ---- at most 4 analyst calls at once ----

// gateAnalyst blocks every call until released or cancelled and records how many run at once.
type gateAnalyst struct {
	inner   *spAnalyst
	release chan struct{}
	mu      sync.Mutex
	now     int
	max     int
	calls   int
	entered chan struct{}
}

func newGate(inner *spAnalyst) *gateAnalyst {
	return &gateAnalyst{inner: inner, release: make(chan struct{}), entered: make(chan struct{}, 100)}
}

func (g *gateAnalyst) Propose(ctx context.Context, in slices.AnalystInput) (slices.Proposal, error) {
	g.mu.Lock()
	g.now++
	g.calls++
	g.max = max(g.max, g.now)
	g.mu.Unlock()
	g.entered <- struct{}{}
	defer func() { g.mu.Lock(); g.now--; g.mu.Unlock() }()
	select {
	case <-g.release:
	case <-ctx.Done():
		return slices.Proposal{}, ctx.Err()
	}
	return g.inner.Propose(ctx, in)
}

func (g *gateAnalyst) stats() (now, max, calls int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.now, g.max, g.calls
}

func (g *gateAnalyst) waitEntered(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-g.entered:
		case <-time.After(3 * time.Second):
			t.Fatalf("only %d analyst calls started, want %d", i, n)
		}
	}
}

func gatedRig(t *testing.T) (*spRig, *gateAnalyst) {
	t.Helper()
	inner := &spAnalyst{results: []spResult{{p: spNotes(t)}}}
	g := newGate(inner)
	sp := newNotes(t, func(c *slices.Config) { c.Analyst = g })
	return sp, g
}

func (sp *spRig) create(t *testing.T, i int) string {
	t.Helper()
	sp.clock.Advance(1100 * time.Millisecond) // the fake clock never moves; stay under the rate limit
	op := sp.post("POST", slicesURL(""), `{"description":"use case `+strconv.Itoa(i)+`"}`).operation(t, http.StatusAccepted)
	return op.Id
}

func (sp *spRig) status(t *testing.T, id string) string {
	t.Helper()
	sp.clock.Advance(1100 * time.Millisecond)
	return string(sp.do("GET", "/operations/"+id+"?api-version="+apiVersion, clientToken, "", nil).operation(t, 200).Status)
}

// settled follows an operation to its end, moving the fake clock past the rate limit each poll.
func (sp *spRig) settled(t *testing.T, id string) string {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		switch s := sp.status(t, id); s {
		case "succeeded", "failed", "cancelled":
			return s
		}
	}
	t.Fatalf("operation %s did not end", id)
	return ""
}

func TestAtMostFourAnalystCallsRunAtOnceAndAllSixSucceed(t *testing.T) {
	sp, g := gatedRig(t)
	ids := make([]string, 6)
	for i := range ids {
		ids[i] = sp.create(t, i)
	}
	g.waitEntered(t, 4)
	time.Sleep(200 * time.Millisecond) // time for a wrongly started fifth call
	if now, max, _ := g.stats(); now != 4 || max != 4 {
		t.Fatalf("in flight %d (max %d) with 6 operations waiting on the analyst, want exactly 4", now, max)
	}
	for _, id := range ids {
		if s := sp.status(t, id); s != "running" {
			t.Errorf("operation %s is %q while it runs or waits for a slot, want running", id, s)
		}
	}
	close(g.release)
	for _, id := range ids {
		if s := sp.settled(t, id); s != "succeeded" {
			t.Errorf("operation %s ended %q, want succeeded (waiting is not refusal)", id, s)
		}
	}
	if _, max, calls := g.stats(); max != 4 || calls != 6 {
		t.Errorf("max in flight %d, calls %d; want 4 and 6", max, calls)
	}
}

func TestACancelledWaitingOperationNeverCallsTheAnalyst(t *testing.T) {
	sp, g := gatedRig(t)
	for i := 0; i < 4; i++ {
		sp.create(t, i)
	}
	g.waitEntered(t, 4)
	waiting := sp.create(t, 4)
	time.Sleep(100 * time.Millisecond)
	if s := sp.status(t, waiting); s != "running" {
		t.Fatalf("the fifth operation is %q, want running while it waits", s)
	}
	sp.clock.Advance(1100 * time.Millisecond)
	sp.do("POST", "/operations/"+waiting+"/cancel?api-version="+apiVersion, clientToken, "", nil).operation(t, http.StatusAccepted)
	if s := sp.settled(t, waiting); s != "cancelled" {
		t.Fatalf("the waiting operation ended %q, want cancelled", s)
	}
	close(g.release)
	time.Sleep(200 * time.Millisecond)
	if _, _, calls := g.stats(); calls != 4 {
		t.Errorf("analyst calls %d, want 4: a cancelled waiting operation must never call it", calls)
	}
}

// ---- T038h: an asynchronous refusal keeps its failures ----

// An approvePart whose refusal comes after acceptance (a change came between the check made at
// acceptance and the work) ends failed; the failing checks must stay in error.failures. The rig
// holds the operation's work until the change is made, so the refusal is the work's, not the
// acceptance's.
func TestAnAsyncRefusalKeepsItsFailuresInTheOperationsError(t *testing.T) {
	x := newDopRig(t)
	x.seed(1, false)
	sl, err := x.svc.Get("S-1")
	if err != nil || len(sl.Goals) < 2 {
		t.Fatalf("setup: slice %+v, err %v; want a slice with at least two goals", sl, err)
	}
	x.ops.set(func(f *dopOps) { f.hold = true })
	acc := x.accepted(x.do("POST", spURL("S-1", "/approvals"), "", approvalBody("rules", sl.Parts.Rules)), "approve-part", "S-1")
	// A change between acceptance and the work: the rules now refer to a goal that is gone.
	got, err := x.svc.ReplaceGoals(context.Background(), "S-1", sl.Goals[:1])
	if err != nil {
		t.Fatal(err)
	}
	if got.Parts.Rules != sl.Parts.Rules {
		t.Fatalf("setup: the rules hash changed (%s to %s); the approval must stay at the shown hash", sl.Parts.Rules, got.Parts.Rules)
	}
	_, refusal := x.svc.Approve(context.Background(), "S-1", slices.PartRules, sl.Parts.Rules)
	var want *slices.Error
	if !errors.As(refusal, &want) || want.Code != "PartFailsChecks" || len(want.Failures) == 0 {
		t.Fatalf("setup: the service's refusal is %v, want PartFailsChecks with failing checks", refusal)
	}
	if v := x.opView(x.getOp(acc.ID), 200); v.Status == "failed" || v.Status == "succeeded" {
		t.Fatalf("setup: the operation is %q before its work ran, want accepted or running", v.Status)
	}
	x.ops.release(acc.ID)
	var final gen.Operation
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		x.clock.Advance(1100 * time.Millisecond)
		p := x.do("GET", "/operations/"+acc.ID+"?api-version="+apiVersion, "", "")
		final = p.operation(t, 200)
		if final.Status == gen.OperationStatusFailed {
			break
		}
	}
	if final.Status != gen.OperationStatusFailed || final.Error == nil {
		t.Fatalf("operation %+v, want failed with an error", final)
	}
	// The contract does not list PartFailsChecks as an outcome of the approvePart operation (it is
	// a synchronous refusal), so the work's refusal is OperationFailed, with the checks kept.
	if final.Error.Code != gen.ErrorCodeOperationFailed {
		t.Errorf("error code %q, want %q", final.Error.Code, gen.ErrorCodeOperationFailed)
	}
	if final.Error.Failures == nil || len(*final.Error.Failures) != len(want.Failures) {
		t.Fatalf("error %+v lost its failures; want the %d failing checks the service gave", final.Error, len(want.Failures))
	}
	for i, f := range *final.Error.Failures {
		if w := want.Failures[i]; f.Part != w.Part || f.Check != w.Check || f.Passed || f.Reason != w.Reason {
			t.Errorf("failure %+v is not the failing check the service gave: %+v", f, w)
		}
	}
	if final.Error.Detail == nil || *final.Error.Detail == "" {
		t.Errorf("error lacks detail")
	}
}

// ---- T038h: page tokens ----

func (sp *spRig) seedMany(t *testing.T, n int) {
	t.Helper()
	for i := 1; i <= n; i++ {
		sp.seed(t, "slice "+strconv.Itoa(i))
	}
}

func TestAPageTokenOfListSlicesIsRefusedByListProviderOperations(t *testing.T) {
	sp := newNotes(t)
	sp.seedMany(t, 60)
	next := sp.list(t, clientToken, "").Next
	if next == nil {
		t.Fatal("no next_page_token")
	}
	sp.do("GET", "/providers/Agentic.Engine/operations?page_token="+*next, clientToken, "", nil).problem(t, 400, gen.ErrorCodeInvalidRequest)
}

// A token names a place in one list; a bare offset such as base64("50") is not one this service
// issued for listSlices, even though another list's tokens may look like it.
func TestAForgedOffsetTokenIsNotAListSlicesToken(t *testing.T) {
	sp := newNotes(t)
	sp.seedMany(t, 120)
	sp.do("GET", slicesURL("&page_token=NTA"), clientToken, "", nil).problem(t, 400, gen.ErrorCodeInvalidRequest)
}

func TestListingIsStableWhileSlicesAreCreatedBetweenPages(t *testing.T) {
	sp := newNotes(t)
	sp.seedMany(t, 120)
	first := sp.list(t, clientToken, "")
	if first.Next == nil || first.Items[0].ID != "S-120" || first.Items[49].ID != "S-71" {
		t.Fatalf("first page %v..%v next %v", first.Items[0].ID, first.Items[len(first.Items)-1].ID, first.Next)
	}
	sp.seedMany(t, 3) // S-121..S-123 appear while the client holds the token
	second := sp.list(t, clientToken, "&page_token="+*first.Next)
	if len(second.Items) == 0 || second.Items[0].ID != "S-70" {
		t.Fatalf("second page does not start at S-70: no item repeated")
	}
	seen := map[string]bool{}
	for _, it := range first.Items {
		seen[it.ID] = true
	}
	for p := second; ; {
		for _, it := range p.Items {
			if seen[it.ID] {
				t.Errorf("%s listed twice", it.ID)
			}
			seen[it.ID] = true
		}
		if p.Next == nil {
			break
		}
		p = sp.list(t, clientToken, "&page_token="+*p.Next)
	}
	for i := 1; i <= 120; i++ {
		if !seen["S-"+strconv.Itoa(i)] {
			t.Errorf("S-%d skipped", i)
		}
	}
}

func TestListingSkipsNothingWhenAnEarlierSliceIsDeletedBetweenPages(t *testing.T) {
	sp := newNotes(t)
	sp.seedMany(t, 120)
	first := sp.list(t, clientToken, "")
	if first.Next == nil {
		t.Fatal("no next_page_token")
	}
	if err := sp.svc.Delete(context.Background(), "S-100"); err != nil { // already shown on page one
		t.Fatal(err)
	}
	second := sp.list(t, clientToken, "&page_token="+*first.Next)
	if len(second.Items) == 0 || second.Items[0].ID != "S-70" {
		t.Fatalf("second page does not start at S-70: something was skipped")
	}
}

// ---- T038h: request Content-Type ----

// The contract declares only application/json for the body and no 415, so a body of another
// type is a request that does not match the contract: 400 InvalidRequest, no operation.
func TestARequestBodyThatIsNotJSONByContentTypeIsRefused(t *testing.T) {
	for _, ct := range []string{"text/plain", "application/x-www-form-urlencoded", "application/xml", "application/problem+json"} {
		t.Run(ct, func(t *testing.T) {
			sp := newNotes(t)
			sp.do("POST", slicesURL(""), clientToken, createBody, map[string]string{"Content-Type": ct}).
				problem(t, http.StatusBadRequest, gen.ErrorCodeInvalidRequest)
			sp.noOperation(t)
			if n := sp.analyst.calls(); n != 0 {
				t.Errorf("analyst called %d times", n)
			}
		})
	}
}

func TestARequestBodyWithoutContentTypeIsRefusedAndJSONWithCharsetIsNot(t *testing.T) {
	sp := newNotes(t)
	req := httptest.NewRequest("POST", slicesURL(""), strings.NewReader(createBody))
	req.Header.Set("Authorization", "Bearer "+clientToken)
	w := httptest.NewRecorder()
	sp.h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("no Content-Type: status %d, want 400", w.Code)
	}
	sp.do("POST", slicesURL(""), clientToken, createBody, map[string]string{"Content-Type": "application/json; charset=utf-8"}).
		operation(t, http.StatusAccepted)
}
