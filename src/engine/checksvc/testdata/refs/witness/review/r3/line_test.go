package r3

import (
	"strings"
	"testing"

	"example.com/w/internal/greet"
)

// The witness of R-3: it runs Greet's last line (greet.go:23), then fails.
func TestR3NameLeftOut(t *testing.T) {
	if got := greet.Greet("Ada"); !strings.Contains(got, "Ada") {
		t.Errorf("Greet(%q) = %q, want the name in it", "Ada", got)
	}
}
