package tests

import "testing"

// A derived test that names rule R-7 and fails, saying where. ADR 0031's rerun confirmed a finding
// from such a test; ADR 0039 runs only the witness a finding names, so it confirms nothing.
func TestR7Derived(t *testing.T) {
	t.Errorf("internal/greet/greet.go:23: the greeting leaves the name out")
}
