package steps

// WitnessServesThroughServer (T116): a rule of the review agent's behaviour, not of the platform,
// so it is the one grader that does not reuse engine/checks. A witness written in this round (a new
// review/r<n>/*_test.go) must serve the handler through httptest.NewServer, whose server recovers a
// handler's panic: the request fails and the witness fails having run the line. A witness that
// calls a handler's ServeHTTP directly lets a panic in the service stop the test binary before its
// coverage is written, so the platform cannot confirm the finding. An earlier round's witness (in
// the project before) is not judged; TestsUnchanged guards it.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"sort"
	"strings"
)

var witnessPath = regexp.MustCompile(`^review/r[0-9]+/.*_test\.go$`)

// witnessServesThroughServer judges the witnesses the review added between before and after.
func witnessServesThroughServer(before, after string) Result {
	b, err := Read(before)
	if err != nil {
		return fail(GThroughSrv, "the project before cannot be read: %v", err)
	}
	a, err := Read(after)
	if err != nil {
		return fail(GThroughSrv, "the project after cannot be read: %v", err)
	}
	var files []string
	for p := range a {
		if _, earlier := b[p]; !earlier && witnessPath.MatchString(p) {
			files = append(files, p)
		}
	}
	sort.Strings(files)
	var bad []string
	for _, p := range files {
		calls, err := serveHTTPCalls(p, a[p])
		if err != nil {
			bad = append(bad, fmt.Sprintf("%s does not parse, so its ServeHTTP calls cannot be judged: %v", p, err))
			continue
		}
		for _, line := range calls {
			bad = append(bad, fmt.Sprintf("%s:%d calls a handler's ServeHTTP directly", p, line))
		}
	}
	examined := fmt.Sprintf("%d new witness file", len(files))
	if len(files) != 1 {
		examined += "s"
	}
	examined += " under review/r<n>/ [" + strings.Join(files, ", ") + "]"
	if len(bad) > 0 {
		return fail(GThroughSrv, "examined %s: %s; a witness must serve the handler with httptest.NewServer, since a panic in a direct ServeHTTP call stops the test before its coverage is written",
			examined, strings.Join(bad, "; "))
	}
	return Result{Grader: GThroughSrv, Passed: true, Reason: "examined " + examined + ": none calls a handler's ServeHTTP directly"}
}

// serveHTTPCalls returns the line of each call expression in src whose function is a selector
// named ServeHTTP (h.ServeHTTP(...), gen.Handler(app.New()).ServeHTTP(...)). Comments and strings
// are not code, so they never count.
func serveHTTPCalls(name, src string) ([]int, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var lines []int
	ast.Inspect(f, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			if s, ok := c.Fun.(*ast.SelectorExpr); ok && s.Sel.Name == "ServeHTTP" {
				lines = append(lines, fset.Position(c.Pos()).Line)
			}
		}
		return true
	})
	return lines, nil
}
