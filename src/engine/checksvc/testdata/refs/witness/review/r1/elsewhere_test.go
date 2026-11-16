package r1

import (
	"strings"
	"testing"

	"example.com/w/internal/greet"
)

// Runs and fails like TestR1NameLeftOut, but in another file than name_test.go.
func TestR1Elsewhere(t *testing.T) {
	if got := greet.Greet("Bo"); !strings.Contains(got, "Bo") {
		t.Errorf("Greet(%q) = %q", "Bo", got)
	}
}
