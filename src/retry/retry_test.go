package retry

// T007 of feature 002 (research R5, data model "Retry rule"). This file is the spec of the module
// github.com/agentic-platform/retry, which has no dependencies:
//
//	type Rule struct {
//		Retries int           // retries after the first try (x-limits retry-attempts)
//		First   time.Duration // the first retry's wait (retry-first-wait-ms)
//		Longest time.Duration // the longest wait before the random part (retry-longest-wait-ms)
//		Jitter  float64       // the random part's largest fraction of the wait (retry-jitter-fraction)
//	}
//
//	// Wait returns how long retry n (from 0) waits: min(Longest, First*2^n) plus random()*Jitter of
//	// that. random returns a value in [0, 1); 0 gives the bound without a random part. Any n >= 0
//	// is valid, including one so large that First*2^n would overflow.
//	func (r Rule) Wait(n int, random func() float64) time.Duration
//
//	// Parse reads the rule from the x-limits of an OpenAPI document (YAML), keys retry-attempts,
//	// retry-first-wait-ms, retry-longest-wait-ms and retry-jitter-fraction. A missing key, or a
//	// value of the wrong type or out of range (attempts < 0 or not an integer, waits < 1 or not an
//	// integer, longest < first, jitter < 0), is an error.
//	func Parse(openapi []byte) (Rule, error)

import (
	"math"
	"os"
	"strings"
	"testing"
	"time"
)

const contractPath = "../engine/api/preview/2026-11-12-preview/openapi.yaml"

var top = math.Nextafter(1, 0) // the random source's largest value

func contract(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func published(t *testing.T) Rule {
	t.Helper()
	r, err := Parse(contract(t))
	if err != nil {
		t.Fatalf("the engine contract's rule is refused: %v", err)
	}
	return r
}

func TestRetryRuleFromContractIsThePublishedOne(t *testing.T) {
	r := published(t)
	if r.Retries != 3 || r.First != time.Second || r.Longest != 30*time.Second || r.Jitter != 0.5 {
		t.Fatalf("rule = %+v, want 3 retries, 1 s, 30 s, 0.5", r)
	}
}

func TestRetryWaitBounds(t *testing.T) {
	r := published(t)
	for n, b := range [][2]time.Duration{{time.Second, 1500 * time.Millisecond}, {2 * time.Second, 3 * time.Second}, {4 * time.Second, 6 * time.Second}} {
		if got := r.Wait(n, func() float64 { return 0 }); got != b[0] {
			t.Errorf("Wait(%d, 0) = %v, want exactly %v", n, got, b[0])
		}
		got := r.Wait(n, func() float64 { return top })
		if got > b[1] || got < b[1]-time.Microsecond {
			t.Errorf("Wait(%d, top) = %v, want %v (not above it)", n, got, b[1])
		}
		for _, f := range []float64{0.1, 0.5, 0.9} {
			if got := r.Wait(n, func() float64 { return f }); got < b[0] || got > b[1] {
				t.Errorf("Wait(%d, %v) = %v, outside [%v, %v]", n, f, got, b[0], b[1])
			}
		}
	}
}

func TestRetryWaitIsCappedForLargeN(t *testing.T) {
	r := published(t)
	for _, n := range []int{5, 6, 30, 62, 63, 64, 100, 1000, math.MaxInt32} {
		if got := r.Wait(n, func() float64 { return 0 }); got != 30*time.Second {
			t.Errorf("Wait(%d, 0) = %v, want 30s", n, got)
		}
		if got := r.Wait(n, func() float64 { return top }); got > 45*time.Second || got < 45*time.Second-time.Microsecond {
			t.Errorf("Wait(%d, top) = %v, want 45s, never above", n, got)
		}
	}
}

func TestRetryRuleHoldsNoValuesOfItsOwn(t *testing.T) {
	raw := string(contract(t))
	for key, repl := range map[string]string{
		"  retry-attempts: 3\n":            "  retry-attempts: 5\n",
		"  retry-first-wait-ms: 1000\n":    "  retry-first-wait-ms: 200\n",
		"  retry-longest-wait-ms: 30000\n": "  retry-longest-wait-ms: 700\n",
		"  retry-jitter-fraction: 0.5\n":   "  retry-jitter-fraction: 0.25\n",
	} {
		if strings.Count(raw, key) != 1 {
			t.Fatalf("the contract has no single line %q", key)
		}
		raw = strings.Replace(raw, key, repl, 1)
	}
	r, err := Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if r.Retries != 5 || r.First != 200*time.Millisecond || r.Longest != 700*time.Millisecond || r.Jitter != 0.25 {
		t.Fatalf("rule = %+v, want 5, 200 ms, 700 ms, 0.25", r)
	}
	// 200, 400, 700 (capped), 700 ms; top of the random part adds 25 %.
	for n, want := range []time.Duration{200, 400, 700, 700} {
		if got := r.Wait(n, func() float64 { return 0 }); got != want*time.Millisecond {
			t.Errorf("Wait(%d, 0) = %v, want %v ms", n, got, want)
		}
	}
	if got := r.Wait(3, func() float64 { return top }); got > 875*time.Millisecond || got < 875*time.Millisecond-time.Microsecond {
		t.Errorf("Wait(3, top) = %v, want 875ms", got)
	}
}

func TestRetryParseRefusesBadRules(t *testing.T) {
	good := string(contract(t))
	for name, c := range map[string][2]string{
		"missing attempts":  {"  retry-attempts: 3\n", ""},
		"missing first":     {"  retry-first-wait-ms: 1000\n", ""},
		"missing longest":   {"  retry-longest-wait-ms: 30000\n", ""},
		"missing jitter":    {"  retry-jitter-fraction: 0.5\n", ""},
		"fractional wait":   {"  retry-first-wait-ms: 1000\n", "  retry-first-wait-ms: 1000.5\n"},
		"quoted attempts":   {"  retry-attempts: 3\n", "  retry-attempts: \"3\"\n"},
		"zero first":        {"  retry-first-wait-ms: 1000\n", "  retry-first-wait-ms: 0\n"},
		"longest < first":   {"  retry-longest-wait-ms: 30000\n", "  retry-longest-wait-ms: 500\n"},
		"negative jitter":   {"  retry-jitter-fraction: 0.5\n", "  retry-jitter-fraction: -0.1\n"},
		"negative attempts": {"  retry-attempts: 3\n", "  retry-attempts: -1\n"},
	} {
		if strings.Count(good, c[0]) != 1 {
			t.Fatalf("%s: the contract has no single line %q", name, c[0])
		}
		if _, err := Parse([]byte(strings.Replace(good, c[0], c[1], 1))); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
