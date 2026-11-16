package checks_test

// T049c: checks.NotOwn(t, writable) is t without the paths the writable globs match, so a step's
// own files do not count as the "other" files another check compares. `dir/**` is everything below
// dir; any other entry is one exact path. Written by a separate author from the implementation.

import (
	"reflect"
	"strings"
	"testing"

	"github.com/agentic-platform/engine/checks"
)

func TestNotOwnDropsWhatTheGlobsMatch(t *testing.T) {
	tr := checks.Tree{
		"gen/x.go": "a", "gen/sub/y.go": "b", "gen": "c",
		"go.mod": "d", "go.sum": "e", "go.modx": "f", "sub/go.mod": "g",
		"internal/app/app.go": "h", "tests/t_test.go": "i", "testsx/a.go": "j",
	}
	got := checks.NotOwn(tr, []string{"gen/**", "go.mod", "tests/**"})
	want := checks.Tree{
		"gen": "c", "go.sum": "e", "go.modx": "f", "sub/go.mod": "g",
		"internal/app/app.go": "h", "testsx/a.go": "j",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if len(tr) != 10 {
		t.Errorf("the input tree was modified: %v", tr)
	}
}

func TestNotOwnWithNoGlobsKeepsEverything(t *testing.T) {
	tr := checks.Tree{"a": "1", "b/c": "2"}
	if got := checks.NotOwn(tr, nil); !reflect.DeepEqual(got, tr) {
		t.Fatalf("got %v", got)
	}
}

// The code step writes internal/** and cmd/** (src/catalogue/api-feature-go/pattern.yaml); a change
// it makes to gen/ must still fail GeneratedCodeUnchanged, and one to its own files must not.
func TestNotOwnKeepsACodeStepsGenDiffVisibleAndItsOwnFilesInvisible(t *testing.T) {
	w := []string{"internal/**", "cmd/**"}
	genGlobs := []string{"gen/**", "go.mod", "go.sum", "vendor/**"}
	before := checks.Tree{"gen/x.go": "1", "internal/app/app.go": "1"}

	after := checks.Tree{"gen/x.go": "2", "internal/app/app.go": "1"}
	rs := checks.GeneratedCodeUnchangedOf(checks.NotOwn(before, w), checks.NotOwn(after, w), genGlobs)
	if allPassed(rs) || !strings.Contains(reasons(rs), "gen/x.go") {
		t.Errorf("a gen/ change by the code step passed or is not named: %+v", rs)
	}

	after = checks.Tree{"gen/x.go": "1", "internal/app/app.go": "2", "cmd/main.go": "new"}
	rs = checks.GeneratedCodeUnchangedOf(checks.NotOwn(before, w), checks.NotOwn(after, w), genGlobs)
	if !allPassed(rs) {
		t.Errorf("a change in the code step's own files failed: %+v", rs)
	}
}
