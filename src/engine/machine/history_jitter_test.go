package machine_test

// T053e regression (fresh review, 2026-11-14): a failing restoreStep or commitStep is retried
// with exponential backoff and jitter (constitution IV, research D7), like the engine's
// operations (api/operations.go backoff: full jitter over the window base<<n, base 1 s).
//
// Seam: only machine.Config.Sleep, which receives each wait; no seed or random source is
// needed. Pinned: the wait before retry n (0-based) lies in [0, 1s<<n]; across runs the waits
// are not one fixed sequence; and they grow: over many runs the mean wait before the third
// retry is well above the mean before the first.

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/agentic-platform/engine/machine"
)

// retryWaits runs one run whose first commit always fails and returns the waits it slept.
func retryWaits(t *testing.T) []time.Duration {
	t.Helper()
	x := historyFixture(t, nil)
	x.h.commitFn = func(machine.CommitRequest) (bool, error) { return false, errors.New("HTTP 500 WriteFailed") }
	if r := x.startIn("RUN-1", "proj-a"); r.Status != machine.StatusInterrupted {
		t.Fatalf("status = %s, want interrupted", r.Status)
	}
	return x.h.sleeps
}

func TestHistoryRetryWaitsAreExponentialWithJitter(t *testing.T) {
	const runs = 40
	var seqs []string
	sums := make([]time.Duration, 3)
	for i := 0; i < runs; i++ {
		w := retryWaits(t)
		if len(w) != 3 {
			t.Fatalf("waits = %v, want 3 (one before each of 3 retries)", w)
		}
		for n, d := range w {
			if window := time.Second << n; d < 0 || d > window {
				t.Fatalf("wait before retry %d = %s, want within the jitter window [0, %s]", n+1, d, window)
			}
			sums[n] += d
		}
		seqs = append(seqs, fmt.Sprint(w))
	}
	same := true
	for _, s := range seqs[1:] {
		if s != seqs[0] {
			same = false
		}
	}
	if same {
		t.Fatalf("every run waited %s: a fixed sequence, no jitter", seqs[0])
	}
	if sums[2] < 2*sums[0] {
		t.Fatalf("mean waits %s, %s, %s: they do not grow exponentially", sums[0]/runs, sums[1]/runs, sums[2]/runs)
	}
}
