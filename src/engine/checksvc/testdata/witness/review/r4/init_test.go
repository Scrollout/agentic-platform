package r4

import (
	"testing"

	"example.com/w/internal/greet"
)

// Always fails; the lines it can claim (greet.go:11 in init, greet.go:15 from the package-level
// initialiser) run when the package is initialised, before any test.
func TestR4Always(t *testing.T) {
	t.Fatalf("internal/greet/greet.go:15: banner %q", greet.Banner)
}
