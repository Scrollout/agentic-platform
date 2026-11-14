package steps

// Grading the review agent: bare JSON in the check contract's shape, and what the findings say
// about a planted rule violation, judged by the engine's own rerun of the rule's tests (ADR 0031).

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
}

var ruleID = regexp.MustCompile(`^R-[0-9]+$`)

// ParseFindings reads the review agent's whole answer as `{"findings":[...]}`: bare JSON, no fence,
// no prose before or after (the engine rejects a fenced answer), no field the contract does not
// have, every finding with all five fields. It returns the findings, or why the answer is not that.
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
		out = append(out, checks.Finding{Rule: *f.Rule, File: *f.File, Line: *f.Line, Text: *f.Text, Blocking: *f.Blocking})
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
// the project as the agent got it and as it left it (a review writes nothing). plant is the
// planted violation, or nil for clean code.
func (e Env) GradeReview(before, after, answer string, plant *Plant) []Result {
	st := e.step("review")
	rs := []Result{}
	findings, perr := ParseFindings(answer)
	if perr != nil {
		rs = append(rs, fail(GBareJSON, "%v", perr))
	} else {
		rs = append(rs, Result{Grader: GBareJSON, Passed: true, Reason: fmt.Sprintf("the answer is bare JSON with %d findings", len(findings))})
	}
	rs = append(rs, e.scope(st, before, after))
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
	rs = append(rs, fold(GEvidence, checks.LegacyFindingsRerun(e.Runner, after, findings)))
	open := checks.LegacyNoBlockingFindingOpen(e.Runner, after, findings)
	if plant == nil {
		// Clean code: no blocking finding may be confirmed by its rule's tests failing at its file.
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

// plantConfirmed: the engine's rerun confirms a blocking finding for the planted rule and file,
// which NoBlockingFindingOpen reports as a failing result naming both.
func plantConfirmed(open []checks.CheckResult, p Plant) Result {
	for _, c := range open {
		if !c.Passed && strings.Contains(c.Reason, "blocking finding "+p.Rule+" at "+p.File+":") {
			return Result{Grader: GPlantedOpen, Passed: true, Reason: "the rerun of the rule's tests confirms it: " + c.Reason}
		}
	}
	return fail(GPlantedOpen, "the rerun of %s's tests does not confirm a blocking finding at %s: %s", p.Rule, p.File, fold(GPlantedOpen, open).Reason)
}
