package r1

import (
	"strings"
	"testing"

	"example.com/w/gen"
	"example.com/w/internal/greet"
	"example.com/w/tests"
)

// The witness of R-1: it runs Greet's last line (greet.go:23), and the generated code and a
// test helper, then fails. Its message names no file.
func TestR1NameLeftOut(t *testing.T) {
	_, _ = gen.Stamp(), tests.Help()
	if got := greet.Greet("Ada"); !strings.Contains(got, "Ada") {
		t.Errorf("Greet(%q) = %q, want the name in it", "Ada", got)
	}
}

// Runs and fails like TestR1NameLeftOut, but its name is R-12's, not R-1's.
func TestR12Other(t *testing.T) {
	if got := greet.Greet("Ada"); !strings.Contains(got, "Ada") {
		t.Errorf("Greet(%q) = %q", "Ada", got)
	}
}
