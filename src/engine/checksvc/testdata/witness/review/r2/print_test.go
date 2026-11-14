package r2

import (
	"testing"

	_ "example.com/w/internal/greet"
)

// Only prints the finding's location and fails: it never runs greet.go:23.
func TestR2PrintsOnly(t *testing.T) {
	t.Fatal("internal/greet/greet.go:23: the greeting leaves the name out")
}
