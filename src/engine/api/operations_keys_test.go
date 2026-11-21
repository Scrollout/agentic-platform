package api

// T023 (feature 002, user story 2), part 3 of 3: Idempotency-Keys of durable operations. Written
// before the code (T025) by a test author; the interface, the rig and the fakes are in
// operations_durable_test.go. Judged from the data model ("Operation record": the `key` and
// `body` fields, One record per key, Keys, Kept) and the engine contract's info text and
// IdempotencyKey parameter: a key and its first result survive a restart when the request created
// an operation; any other answer is kept until a restart; a 503 is never kept.

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/agentic-platform/engine/api/gen"
)

func TestDurableOpTheSameKeyAndBodyGiveTheFirstAnswerAfterARestart(t *testing.T) {
	for _, ended := range []bool{false, true} {
		name := "open"
		if ended {
			name = "ended"
		}
		t.Run(name, func(t *testing.T) {
			x := newDopRig(t)
			x.ops.set(func(f *dopOps) { f.hold = !ended })
			first := x.accepted(x.do("POST", slicesURL(""), "key-1", createBody), "propose-slice", "S-1")
			if ended {
				x.settle(first.ID)
			}
			x.restart(false, true)
			again := x.accepted(x.do("POST", slicesURL(""), "key-1", createBody), "propose-slice", "S-1")
			if again.ID != first.ID {
				t.Fatalf("after a restart the same key and body answered %s, want the first answer %s", again.ID, first.ID)
			}
			x.do("POST", slicesURL(""), "key-1", `{"description":"another use case"}`).problem(t, 409, gen.ErrorCodeIdempotencyKeyReused)
			if r := x.records(); len(r) != 1 {
				t.Errorf("records %v, want only the first", r)
			}
			if n := len(x.ops.startsOf("OP-2")); n != 0 {
				t.Errorf("a second operation's workflow was started")
			}
		})
	}
}

func TestDurableOpConcurrentFirstRequestsUnderOneKeyMakeOneRecord(t *testing.T) {
	x := newDopRig(t)
	x.ops.set(func(f *dopOps) { f.hold = true })
	const n = 15 // below the rate limit
	ids := make([]string, n)
	codes := make([]int, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			w := x.r.do("POST", slicesURL(""), x.client, createBody, map[string]string{"Idempotency-Key": "race-key"})
			codes[i] = w.Code
			var op gen.Operation
			if json.Unmarshal(w.Body.Bytes(), &op) == nil {
				ids[i] = op.Id
			}
		}()
	}
	close(start)
	wg.Wait()
	for i := range ids {
		if codes[i] != 202 || ids[i] != ids[0] || ids[0] == "" {
			t.Fatalf("call %d: status %d, operation %q; want 202 and %q for all", i, codes[i], ids[i], ids[0])
		}
	}
	if r := x.records(); len(r) != 1 || r[0] != ids[0] {
		t.Fatalf("records %v, want only %s", r, ids[0])
	}
	if s := x.ops.allStarts(); len(s) != 1 {
		t.Errorf("workflow starts %v, want one", s)
	}
}

func TestDurableOpA503UnderAKeyIsNotKept(t *testing.T) {
	x := newDopRig(t)
	x.ops.set(func(f *dopOps) { f.down = true })
	dopUnavailable(t, x.do("POST", slicesURL(""), "key-503", createBody))
	x.ops.set(func(f *dopOps) { f.down = false })
	// Sent again, with the same key and body: judged again, not answered 503 from memory.
	op := x.opView(x.do("POST", slicesURL(""), "key-503", createBody), 202)
	if op.Kind != "propose-slice" {
		t.Fatalf("operation %+v, want a propose-slice", op)
	}
	if end := x.settle(op.ID); end.Status != "succeeded" {
		t.Fatalf("operation %+v, want succeeded", end)
	}
	// A 503 under another key, then another body under it: nothing was kept, so no 409.
	x.ops.set(func(f *dopOps) { f.down = true })
	dopUnavailable(t, x.do("POST", slicesURL(""), "key-503b", createBody))
	x.ops.set(func(f *dopOps) { f.down = false })
	x.opView(x.do("POST", slicesURL(""), "key-503b", `{"description":"another use case"}`), 202)
}

func TestDurableOpTheSameKeyOnAnotherPathWithTheSameBodyIs409(t *testing.T) {
	x := newDopRig(t)
	x.seed(2, false)
	x.ops.set(func(f *dopOps) { f.hold = true })
	x.accepted(x.do("DELETE", spURL("S-1", ""), "key-path", ""), "delete-slice", "S-1")
	x.do("DELETE", spURL("S-2", ""), "key-path", "").problem(t, 409, gen.ErrorCodeIdempotencyKeyReused)
	x.restart(false, true)
	x.do("DELETE", spURL("S-2", ""), "key-path", "").problem(t, 409, gen.ErrorCodeIdempotencyKeyReused)
	if r := x.records(); len(r) != 1 {
		t.Errorf("records %v, want only the first", r)
	}
}

func TestDurableOpTheSameKeyUnderAnotherTokenIsAnotherRequest(t *testing.T) {
	x := newDopRig(t)
	x.ops.set(func(f *dopOps) { f.hold = true })
	first := x.accepted(x.do("POST", slicesURL(""), "key-token", createBody), "propose-slice", "S-1")
	x.client = "another-client-token-for-tests" // a new session's token
	x.restart(false, true)
	other := x.opView(x.do("POST", slicesURL(""), "key-token", createBody), 202)
	if other.ID == first.ID {
		t.Fatalf("the same key under another token answered the first token's %s", first.ID)
	}
}
