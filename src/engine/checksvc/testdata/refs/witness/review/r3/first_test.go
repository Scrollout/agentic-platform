package r3

import (
	"testing"

	"example.com/w/internal/greet"
)

var stop = true

// Fails before it reaches greet.go:23.
func TestR3FailsFirst(t *testing.T) {
	if stop {
		t.Fatal("internal/greet/greet.go:23: stopped first")
	}
	greet.Greet("Ada")
}
