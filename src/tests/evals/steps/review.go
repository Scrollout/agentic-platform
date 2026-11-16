package steps

// Grading the review agent: bare JSON in the check contract's shape, the review step's pattern
// checks, and what the findings say about a planted rule violation, judged by the engine's own
// witness rule (ADR 0039, research D35): a blocking finding is confirmed only by the new witness
// test it names under review/r<n>/, failing having run the finding's line.

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"regexp"
	"strings"

	"github.com/agentic-platform/engine/checks"
	"github.com/agentic-platform/engine/procedures"
)

// Plant is a rule violation put into the reference code: the rule it breaks, the file and the
// lines of the function that holds it. A finding names the plant when it is blocking and names that
// rule, that file and a line inside Lines (the function, so the exact statement is not demanded).
type Plant struct {
	Rule  string
	File  string
	Lines [2]int
	What  string
}

// plantRange finds the function of src that contains the first line holding marker, and returns the
// line of the marker and the function's first and last line.
func plantRange(src, marker string) (line int, fn [2]int, err error) {
	for i, l := range strings.Split(src, "\n") {
		if strings.Contains(l, marker) {
			line = i + 1
			break
		}
	}
	if line == 0 {
		return 0, fn, fmt.Errorf("marker %q is not in the file", marker)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		return 0, fn, err
	}
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok {
			a, b := fset.Position(fd.Pos()).Line, fset.Position(fd.End()).Line
			if line >= a && line <= b {
				return line, [2]int{a, b}, nil
			}
		}
	}
	return 0, fn, fmt.Errorf("marker %q is not inside a function", marker)
}

// ReviewFinding is a finding with its fields as pointers, so a missing field can be told from an
// empty one.
type reviewFinding struct {
	Rule     *string `json:"rule"`
	File     *string `json:"file"`
	Line     *int    `json:"line"`
	Text     *string `json:"text"`
	Blocking *bool   `json:"blocking"`
	Witness  *struct {
		File *string `json:"file"`
		Test *string `json:"test"`
	} `json:"witness"`
}

var ruleID = regexp.MustCompile(`^R-[0-9]+$`)

// ParseFindings reads the review agent's whole answer as `{"findings":[...]}`: bare JSON, no fence,
// no prose before or after (the engine rejects a fenced answer), no field the contract does not
// have, every finding with all five fields, and a witness (its file and test) on every blocking
// finding; whether the witness is in its rule's folder and named TestR<n> is the engine's to judge
// (a finding whose witness is not is a suggestion). It returns the findings, or why the answer is not that.
func ParseFindings(answer string) ([]checks.Finding, error) {
	s := strings.TrimSpace(answer)
	if !strings.HasPrefix(s, "{") {
		return nil, fmt.Errorf("the answer is not bare JSON: it starts with %q", head(s))
	}
	dec := json.NewDecoder(strings.NewReader(s))
	dec.DisallowUnknownFields()
	var top struct {
		Findings *[]reviewFinding `json:"findings"`
	}
	if err := dec.Decode(&top); err != nil {
		return nil, fmt.Errorf("the answer is not the findings object: %v", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("the answer has text after the JSON object")
	}
	if top.Findings == nil {
		return nil, fmt.Errorf(`the answer has no "findings" array`)
	}
	out := []checks.Finding{}
	for i, f := range *top.Findings {
		if f.Rule == nil || f.File == nil || f.Line == nil || f.Text == nil || f.Blocking == nil {
			return nil, fmt.Errorf("finding %d lacks one of rule, file, line, text, blocking", i+1)
		}
		if *f.Blocking && !ruleID.MatchString(*f.Rule) {
			return nil, fmt.Errorf("blocking finding %d names the rule %q, which is not an R-<n> ID", i+1, *f.Rule)
		}
		if *f.Text == "" {
			return nil, fmt.Errorf("finding %d has no text", i+1)
		}
		if *f.File != "" && !procedures.IsProjectPath(*f.File) {
			return nil, fmt.Errorf("finding %d names the file %q, which is not a project path", i+1, *f.File)
		}
		var w *checks.Witness
		if f.Witness != nil {
			if f.Witness.File == nil || f.Witness.Test == nil || *f.Witness.File == "" || *f.Witness.Test == "" {
				return nil, fmt.Errorf("finding %d has a witness without its file or test", i+1)
			}
			if !procedures.IsProjectPath(*f.Witness.File) {
				return nil, fmt.Errorf("finding %d names the witness %q, which is not a project path", i+1, *f.Witness.File)
			}
			w = &checks.Witness{File: *f.Witness.File, Test: *f.Witness.Test}
		}
		if *f.Blocking && w == nil {
			return nil, fmt.Errorf("blocking finding %d names no witness test", i+1)
		}
		out = append(out, checks.Finding{Rule: *f.Rule, File: *f.File, Line: *f.Line, Text: *f.Text, Blocking: *f.Blocking, Witness: w})
	}
	return out, nil
}

func head(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 20 {
		s = s[:20]
	}
	return s
}

// GradeReview judges the review agent's answer on the project it reviewed. before and after are
// the project as the agent got it and as it left it (a review writes only its witnesses under
// review/**). plant is the planted violation, or nil for clean code. The review step's pattern
// checks (ScopeKept, DependenciesAllowed, TestsBuild, TestsUnchanged) need no findings and are
// always judged.
func (e Env) GradeReview(before, after, answer string, plant *Plant) []Result {
	st := e.step("review")
	rs := []Result{}
	findings, perr := ParseFindings(answer)
	if perr != nil {
		rs = append(rs, fail(GBareJSON, "%v", perr))
	} else {
		rs = append(rs, Result{Grader: GBareJSON, Passed: true, Reason: fmt.Sprintf("the answer is bare JSON with %d findings", len(findings))})
	}
	rs = append(rs, e.scope(st, before, after), e.deps(st, after), fold(GTestsBuild, checks.TestsBuild(e.Runner, after)))
	rs = append(rs, e.reviewUnchanged(st, before, after))
	rs = append(rs, witnessServesThroughServer(before, after))
	if perr != nil {
		// Nothing below can be judged without findings; each grader fails closed.
		graders := []string{GEvidence, GNoneOpen}
		if plant != nil {
			graders = []string{GEvidence, GPlantedFound, GPlantedOpen}
		}
		for _, g := range graders {
			rs = append(rs, fail(g, "the answer has no findings to judge: %v", perr))
		}
		return rs
	}
	rv, err := e.review(before)
	if err != nil {
		rs = append(rs, fail(GEvidence, "the project before cannot be read: %v", err))
		if plant == nil {
			return append(rs, fail(GNoneOpen, "the project before cannot be read: %v", err))
		}
		return append(rs, plantNamed(findings, *plant), fail(GPlantedOpen, "the project before cannot be read: %v", err))
	}
	rs = append(rs, fold(GEvidence, checks.FindingsCarryEvidence(e.Runner, after, rv, findings)))
	open := checks.NoBlockingFindingOpen(e.Runner, after, rv, findings)
	if plant == nil {
		// Clean code: no blocking finding may be confirmed by its witness.
		rs = append(rs, fold(GNoneOpen, open))
		return rs
	}
	rs = append(rs, plantNamed(findings, *plant), plantConfirmed(open, *plant))
	return rs
}

// plantNamed: some blocking finding names the planted rule, the file and a line inside the function.
func plantNamed(fs []checks.Finding, p Plant) Result {
	var near []string
	for _, f := range fs {
		if !f.Blocking {
			continue
		}
		near = append(near, fmt.Sprintf("%s at %s:%d", f.Rule, f.File, f.Line))
		if f.Rule == p.Rule && f.File == p.File && f.Line >= p.Lines[0] && f.Line <= p.Lines[1] {
			return Result{Grader: GPlantedFound, Passed: true, Reason: fmt.Sprintf("blocking finding %s at %s:%d names the planted violation", f.Rule, f.File, f.Line)}
		}
	}
	return fail(GPlantedFound, "no blocking finding names %s at %s lines %d to %d (%s); blocking findings: [%s]",
		p.Rule, p.File, p.Lines[0], p.Lines[1], p.What, strings.Join(near, ", "))
}

// review is the checks.Review the check service builds for the review step: before, the review
// step's writable entries and the code step's (the fix step's).
func (e Env) review(before string) (checks.Review, error) {
	f, err := Read(before)
	if err != nil {
		return checks.Review{}, err
	}
	b := map[string][]byte{}
	for p, c := range f {
		b[p] = []byte(c)
	}
	return checks.Review{Before: checks.HashTree(b), Own: e.step("review").Writable, Fixable: e.step("code").Writable}, nil
}

// reviewUnchanged is TestsUnchanged for the review step, as the check service runs it: the tests
// step's files unchanged (the review's own files dropped first), and the witness rule, under which
// no witness in before changes or goes and the review step may add one.
func (e Env) reviewUnchanged(st PatternStep, before, after string) Result {
	r := e.unchanged(GTestsSame, before, after, st.Writable, e.step("tests").Writable, checks.TestsUnchangedOf)
	w := e.unchanged(GTestsSame, before, after, nil, st.Writable, func(b, a checks.Tree, globs []string) []checks.CheckResult {
		return checks.WitnessesUnchangedOf(b, a, globs, true)
	})
	return Result{Grader: GTestsSame, Passed: r.Passed && w.Passed, Reason: r.Reason + "; " + w.Reason}
}

// plantConfirmed: the engine's witness rule confirms a blocking finding for the planted rule and
// file, which NoBlockingFindingOpen reports as a failing result naming both.
func plantConfirmed(open []checks.CheckResult, p Plant) Result {
	for _, c := range open {
		if !c.Passed && strings.Contains(c.Reason, "blocking finding "+p.Rule+" at "+p.File+":") {
			return Result{Grader: GPlantedOpen, Passed: true, Reason: "its witness confirms it: " + c.Reason}
		}
	}
	return fail(GPlantedOpen, "no witness confirms a blocking finding %s at %s: %s", p.Rule, p.File, fold(GPlantedOpen, open).Reason)
}
