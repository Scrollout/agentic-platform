package r5

import (
	"os"
	"testing"

	"example.com/w/internal/greet"
)

// TestMain runs greet.go:23 before any test, so the baseline run covers it too.
func TestMain(m *testing.M) {
	greet.Greet("Ada")
	os.Exit(m.Run())
}

// Always fails.
func TestR5Always(t *testing.T) {
	t.Fatal("internal/greet/greet.go:23: always")
}
