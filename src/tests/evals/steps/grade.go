package steps

// The graders. Each one is a deterministic rule over the project the agent left (and the project it
// started from), and each reuses the platform's own check in engine/checks, so the eval and the
// engine cannot disagree about what a good step is; the one exception is WitnessServesThroughServer
// (grade_server.go), a rule of the review agent's behaviour. No grader calls a model (constitution
// XIX, XX).

import (
	"fmt"
	"strings"

	"github.com/agentic-platform/engine/canonical"
	"github.com/agentic-platform/engine/checks"
)

// Grader names, as the pattern names its checks (and the review graders).
const (
	GScope        = "ScopeKept"
	GTestsBuild   = "TestsBuild"
	GBound        = "EveryExampleBound"
	GFailsStub    = "TestsFailAgainstStub"
	GDeps         = "DependenciesAllowed"
	GTestsSame    = "TestsUnchanged"
	GGenSame      = "GeneratedCodeUnchanged"
	GAllPass      = "AllTestsPass"
	GContract     = "ContractMatchesApproval"
	GBareJSON     = "ReviewIsBareJSON"
	GShape        = "FindingsShape"
	GEvidence     = "FindingsCarryEvidence"
	GPlantedFound = "PlantedFindingNamed"
	GPlantedOpen  = "PlantedFindingConfirmed"
	GNoneOpen     = "NoConfirmedBlockingFinding"
	GThroughSrv   = "WitnessServesThroughServer"
)

// Result is one grader's verdict. Reason is never empty. Counts is present only on the graders
// that judge a fixed list (README "The report"): HeldOutTestsPass and MutantsKilled, once they got
// as far as running their variants.
type Result struct {
	Grader string  `json:"grader"`
	Passed bool    `json:"passed"`
	Reason string  `json:"reason"`
	Counts *Counts `json:"counts,omitempty"`
}

// Counts says how much of a grader's fixed list passed and which names did not (README "The
// report"): Total is the list's length, 0 <= Passed <= Total, and Failed holds the Total-Passed
// names, sorted, each followed by " (timed out)" or " (did not run)" when that is the reason.
type Counts struct {
	Total  int      `json:"total"`
	Passed int      `json:"passed"`
	Failed []string `json:"failed"`
}

// Passed reports whether every result passed (and there is at least one).
func Passed(rs []Result) bool {
	for _, r := range rs {
		if !r.Passed {
			return false
		}
	}
	return len(rs) > 0
}

// Failures lists "Grader: reason" for each failing result.
func Failures(rs []Result) []string {
	var out []string
	for _, r := range rs {
		if !r.Passed {
			out = append(out, r.Grader+": "+r.Reason)
		}
	}
	return out
}

// Env is what grading needs: the pattern's steps and how to run the go toolchain on a project
// (the host's for the self-tests, whose projects are reference code; a no-network container for the
// live evals, whose projects were written by a model).
type Env struct {
	Steps  map[string]PatternStep
	Runner checks.GoRunner
}

// fold turns a check's results into one Result: it passes when every result does.
func fold(grader string, rs []checks.CheckResult) Result {
	r := Result{Grader: grader, Passed: true}
	var why []string
	for _, c := range rs {
		if !c.Passed {
			r.Passed = false
		}
		why = append(why, c.Reason)
	}
	if len(why) > 3 {
		why = append(why[:3], fmt.Sprintf("and %d more", len(rs)-3))
	}
	r.Reason = strings.Join(why, "; ")
	if r.Reason == "" {
		r.Reason = "the check returned no result"
		r.Passed = false
	}
	return r
}

func fail(grader, format string, a ...any) Result {
	return Result{Grader: grader, Reason: fmt.Sprintf(format, a...)}
}

func (e Env) step(name string) PatternStep { return e.Steps[name] }

func (e Env) scope(step PatternStep, before, after string) Result {
	return fold(GScope, checks.Scope(before, after, step.Writable))
}

func (e Env) deps(step PatternStep, after string) Result {
	return fold(GDeps, checks.DependenciesOf(after, step.Writable, step.Dependencies.Allowed, step.Dependencies.Forbidden))
}

// unchanged runs one of the Unchanged checks as the check service does: the files the judged step
// may itself write (own) are dropped from both trees first (checks.NotOwn; for the code step the
// seam internal/app/app.go, T049b).
func (e Env) unchanged(grader, before, after string, own, globs []string, of func(b, a checks.Tree, globs []string) []checks.CheckResult) Result {
	read := func(dir string) (checks.Tree, error) {
		f, err := Read(dir)
		if err != nil {
			return nil, err
		}
		b := map[string][]byte{}
		for p, c := range f {
			b[p] = []byte(c)
		}
		return checks.NotOwn(checks.HashTree(b), own), nil
	}
	b, err := read(before)
	if err != nil {
		return fail(grader, "the project before cannot be read: %v", err)
	}
	a, err := read(after)
	if err != nil {
		return fail(grader, "the project after cannot be read: %v", err)
	}
	return fold(grader, of(b, a, globs))
}

// GradeTests judges the tests agent's result: the project it left, from the project it started with.
func (e Env) GradeTests(before, after string) []Result {
	st := e.step("tests")
	return []Result{
		e.scope(st, before, after),
		e.deps(st, after),
		fold(GTestsBuild, checks.TestsBuild(e.Runner, after)),
		fold(GBound, checks.GodogStrict(e.Runner, after)),
		fold(GFailsStub, checks.TestsFailAgainstStub(e.Runner, after)),
	}
}

// GradeCode judges the code agent's result. approved is the contract text the slice approved.
func (e Env) GradeCode(before, after, approvedContract string) []Result {
	st := e.step("code")
	h, err := canonical.ContractHash(approvedContract)
	contract := fail(GContract, "the approved contract cannot be hashed: %v", err)
	if err == nil {
		contract = fold(GContract, checks.ContractHash(after, h))
	}
	return []Result{
		e.scope(st, before, after),
		e.unchanged(GTestsSame, before, after, st.Writable, e.step("tests").Writable, checks.TestsUnchangedOf),
		e.unchanged(GGenSame, before, after, st.Writable, e.step("generate").Writable, checks.GeneratedCodeUnchangedOf),
		fold(GAllPass, checks.AllTestsPass(e.Runner, after)),
		contract,
		e.deps(st, after),
	}
}
