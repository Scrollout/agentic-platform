package r6

import (
	"testing"

	"example.com/w/internal/greet"
)

// Runs greet.go:21 and passes.
func TestR6Passes(t *testing.T) {
	if got := greet.Greet(""); got != "Hello!" {
		t.Fatalf("Greet(\"\") = %q", got)
	}
}
