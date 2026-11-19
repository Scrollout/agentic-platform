// Package retry is the one rule by which a call that failed retryably is tried again: how many
// retries, and how long each waits. The rule is read from the engine contract's x-limits, so
// the engine and its clients apply the same one.
package retry

import (
	"fmt"
	"math"
	"time"

	"go.yaml.in/yaml/v3"
)

// Rule says how many times a failed call is retried and how long each retry waits.
type Rule struct {
	Retries int           // retries after the first try (x-limits retry-attempts)
	First   time.Duration // the first retry's wait (retry-first-wait-ms)
	Longest time.Duration // the longest wait before the random part (retry-longest-wait-ms)
	Jitter  float64       // the random part's largest fraction of the wait (retry-jitter-fraction)
}

// Wait returns how long retry n (from 0) waits: min(Longest, First*2^n) plus random()*Jitter of
// that. random returns a value in [0, 1); 0 gives the bound without a random part. Any n >= 0
// is valid, including one so large that First*2^n would overflow.
func (r Rule) Wait(n int, random func() float64) time.Duration {
	w := r.First
	for i := 0; i < n && w < r.Longest; i++ {
		if w > r.Longest/2 {
			w = r.Longest
		} else {
			w *= 2
		}
	}
	w = min(w, r.Longest)
	return w + time.Duration(random()*r.Jitter*float64(w))
}

type document struct {
	Limits struct { // yaml.Node, so a fraction or a string is refused, not truncated or converted
		Attempts yaml.Node `yaml:"retry-attempts"`
		First    yaml.Node `yaml:"retry-first-wait-ms"`
		Longest  yaml.Node `yaml:"retry-longest-wait-ms"`
		Jitter   yaml.Node `yaml:"retry-jitter-fraction"`
	} `yaml:"x-limits"`
}

// Parse reads the rule from the x-limits of an OpenAPI document (YAML), keys retry-attempts,
// retry-first-wait-ms, retry-longest-wait-ms and retry-jitter-fraction. A missing key, or a
// value of the wrong type or out of range (attempts < 0 or not an integer, waits < 1 or not an
// integer, longest < first, jitter < 0), is an error.
func Parse(openapi []byte) (Rule, error) {
	var d document
	if err := yaml.Unmarshal(openapi, &d); err != nil {
		return Rule{}, err
	}
	l := &d.Limits
	attempts, err := integer(&l.Attempts, 0, "retry-attempts")
	if err != nil {
		return Rule{}, err
	}
	first, err := integer(&l.First, 1, "retry-first-wait-ms")
	if err != nil {
		return Rule{}, err
	}
	longest, err := integer(&l.Longest, 1, "retry-longest-wait-ms")
	if err != nil {
		return Rule{}, err
	}
	if attempts > math.MaxInt32 {
		return Rule{}, fmt.Errorf("x-limits.retry-attempts: %d is too many", attempts)
	}
	if longest < first {
		return Rule{}, fmt.Errorf("x-limits.retry-longest-wait-ms: %d is below retry-first-wait-ms %d", longest, first)
	}
	if longest > math.MaxInt64/int64(time.Millisecond) {
		return Rule{}, fmt.Errorf("x-limits.retry-longest-wait-ms: %d ms is too long", longest)
	}
	n := &l.Jitter
	if n.Kind == 0 {
		return Rule{}, fmt.Errorf("x-limits.retry-jitter-fraction: missing; a number of at least 0 is required")
	}
	var jitter float64
	if t := n.ShortTag(); n.Kind != yaml.ScalarNode || (t != "!!int" && t != "!!float") || n.Decode(&jitter) != nil {
		return Rule{}, fmt.Errorf("x-limits.retry-jitter-fraction: %q (line %d) is not a number", n.Value, n.Line)
	}
	if !(jitter >= 0) || math.IsInf(jitter, 0) {
		return Rule{}, fmt.Errorf("x-limits.retry-jitter-fraction: %v (line %d) is not a finite number of at least 0", jitter, n.Line)
	}
	return Rule{Retries: int(attempts), First: time.Duration(first) * time.Millisecond,
		Longest: time.Duration(longest) * time.Millisecond, Jitter: jitter}, nil
}

// integer returns the value of n, which must be a plain YAML integer (tag !!int) of at least min.
func integer(n *yaml.Node, min int64, key string) (int64, error) {
	if n.Kind == 0 {
		return 0, fmt.Errorf("x-limits.%s: missing; an integer of at least %d is required", key, min)
	}
	var v int64
	if n.Kind != yaml.ScalarNode || n.ShortTag() != "!!int" || n.Decode(&v) != nil {
		return 0, fmt.Errorf("x-limits.%s: %q (line %d) is not an integer of at least %d", key, n.Value, n.Line, min)
	}
	if v < min {
		return 0, fmt.Errorf("x-limits.%s: %d (line %d) is below %d", key, v, n.Line, min)
	}
	return v, nil
}
