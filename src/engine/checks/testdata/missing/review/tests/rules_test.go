package tests

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Go tests that name a rule (check contract, runCheck: a name part holding R<n>, R_<n> or R-<n>,
// not preceded by an uppercase letter or a digit and not followed by a digit). Each fails and
// says where.

// R-3's test fails at internal/other/other.go.
func TestR3Names(t *testing.T) { t.Errorf("internal/other/other.go:3: the answer is wrong") }

// R-4's test is a subtest whose name part is R_4_sub; it fails at internal/greet/greet.go.
func TestSubtests(t *testing.T) {
	t.Run("R_4_sub", func(t *testing.T) { t.Errorf("internal/greet/greet.go:9: bad") })
	t.Run("fine", func(t *testing.T) {})
}

// R-5: the project's absolute path and the project path.
func TestR5Absolute(t *testing.T) {
	wd, _ := os.Getwd()
	t.Error(fmt.Sprintf("%s: bad", filepath.Join(filepath.Dir(wd), "internal", "greet", "greet.go:5")))
}

// R-6: a bare base name confirms nothing.
func TestR6BareName(t *testing.T) { t.Errorf("greet.go:5: bad") }

// R-7: ./ before the project path is a path boundary.
func TestR7Dot(t *testing.T) { t.Errorf("./internal/greet/greet.go:5: bad") }

// R-8: a character that can be part of a path before the name is not a boundary.
func TestR8Glued(t *testing.T) { t.Errorf("xinternal/greet/greet.go:5: bad") }

// Not R-1 and not R-3: R10 is followed by a digit, and XR3 is preceded by an uppercase letter.
func TestR10Collides(t *testing.T) { t.Errorf("internal/other/other.go:3: bad") }
func TestXR3Collides(t *testing.T) { t.Errorf("internal/other/other.go:3: bad") }
