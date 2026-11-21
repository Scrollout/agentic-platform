package steps

// HeldOutTestsPass (README "Held-out tests", research R2 and R3): the code agent's project, judged
// on tests it never saw.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"

	"github.com/agentic-platform/engine/checks"
)

// GHeldOut is the held-out tests' grader.
const GHeldOut = "HeldOutTestsPass"

const (
	heldOutSrc = "testdata/heldout/heldout_test.go" // in the embedded test data; Go ignores testdata/
	heldOutAt  = "heldout/heldout_test.go"          // where the scratch copy gets it
)

// HeldOutTestsPass adds the held-out tests to a scratch copy of project (the project itself is
// never written) and runs them alone. It passes when every one of them passes. Counts are given
// once the tests got as far as running; when the code does not build, or the project cannot be
// copied, the result fails with that reason and no counts.
func (e Env) HeldOutTestsPass(project string) Result {
	src, err := data.ReadFile(heldOutSrc)
	if err != nil {
		return fail(GHeldOut, "the held-out tests cannot be read: %v", err)
	}
	names, err := topLevelTests(src)
	if err != nil {
		return fail(GHeldOut, "the held-out tests cannot be parsed: %v", err)
	}
	run := VariantRun{Runner: e.variantRunner(), Pkg: "heldout", Expected: names, Add: map[string]string{heldOutAt: string(src)}}
	res, err := run.Variants(project, []Variant{{Name: "held-out"}})
	if err != nil {
		return fail(GHeldOut, "the held-out tests cannot be set up: %v", err)
	}
	if b := res[0].BuildError; b != "" {
		return fail(GHeldOut, "the code does not build with the held-out tests: %s", firstLines(b, 10))
	}
	// Only the held-out file's own binary and names count: a test or a sub-folder the agent wrote
	// under heldout/ judges nothing.
	tests := map[string]Outcome{}
	for _, b := range res[0].Binaries {
		if b.Dir == "heldout" {
			tests = b.Tests
		}
	}
	c := &Counts{Total: len(names), Failed: []string{}}
	for _, n := range names {
		switch o := tests[n]; o {
		case OutcomePassed:
			c.Passed++
		case OutcomeFailed:
			c.Failed = append(c.Failed, n)
		case OutcomeTimedOut:
			c.Failed = append(c.Failed, n+" ("+string(o)+")")
		default:
			c.Failed = append(c.Failed, n+" ("+string(OutcomeDidNotRun)+")")
		}
	}
	sort.Strings(c.Failed)
	r := Result{Grader: GHeldOut, Passed: c.Passed == c.Total, Counts: c,
		Reason: fmt.Sprintf("%d of %d held-out tests passed", c.Passed, c.Total)}
	if len(c.Failed) > 0 {
		r.Reason += "; failed: " + strings.Join(c.Failed, ", ")
	}
	return r
}

// variantRunner is the VariantRun.Runner for e: the runner's own For (DockerRunner's, which mounts
// the scratch copy and the build cache) when it has one, else e.Runner itself (the host runner).
func (e Env) variantRunner() func(root, cache string) checks.GoRunner {
	if f, ok := e.Runner.(interface {
		For(root, cache string) checks.GoRunner
	}); ok {
		return f.For
	}
	return func(_, _ string) checks.GoRunner { return e.Runner }
}

// topLevelTests lists, sorted, the file's top-level func TestXxx(t *testing.T).
func topLevelTests(src []byte) ([]string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), heldOutSrc, src, 0)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || !strings.HasPrefix(fn.Name.Name, "Test") || len(fn.Type.Params.List) != 1 {
			continue
		}
		if star, ok := fn.Type.Params.List[0].Type.(*ast.StarExpr); ok {
			if sel, ok := star.X.(*ast.SelectorExpr); ok && sel.Sel.Name == "T" {
				names = append(names, fn.Name.Name)
			}
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("%s declares no test", heldOutSrc)
	}
	sort.Strings(names)
	return names, nil
}

// firstLines is s's first n lines, with "..." when there were more.
func firstLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = append(lines[:n], "...")
	}
	return strings.Join(lines, "\n")
}
