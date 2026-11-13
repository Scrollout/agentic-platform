package checksvc

import (
	"encoding/json"

	"github.com/agentic-platform/check/api/gen"
	"github.com/agentic-platform/engine/checks"
)

// contractLintPasses is the pattern's check this service never serves: the engine judges it with
// its contract-lint service.
const contractLintPasses = "ContractLintPasses"

// call is what a check reads: the project as this call read it, the request, `before` by hash
// and the go toolchain bounded by the call's time limit.
type call struct {
	dir     string
	req     gen.CheckRequest
	project *project
	before  checks.Tree
	runner  checks.GoRunner
}

// checkByName maps every check name of the pattern the check service serves to its one
// implementation in engine/checks (check contract, runCheck, which states each rule). Any other
// name, ContractLintPasses among them, answers 404 UnknownCheck.
var checkByName = map[string]func(c *call) []checks.CheckResult{
	"ScopeKept": func(c *call) []checks.CheckResult {
		return checks.ScopeOf(c.before, c.project.tree(), c.req.Step.Writable)
	},
	"DependenciesAllowed": func(c *call) []checks.CheckResult {
		d := c.req.Step.Dependencies
		return checks.DependenciesOf(c.dir, c.req.Step.Writable, d.Allowed, d.Forbidden)
	},
	"ContractMatchesApproval": func(c *call) []checks.CheckResult {
		return checks.ContractHash(c.dir, c.approved(gen.ApprovedPartPartContract).HashValue)
	},
	"ExamplesMatchApproval": func(c *call) []checks.CheckResult {
		var rules struct {
			Examples *string `json:"examples"`
		}
		if err := json.Unmarshal(c.approved(gen.ApprovedPartPartRules).Content, &rules); err != nil || rules.Examples == nil {
			return []checks.CheckResult{{Check: "ExamplesMatchApproval", Reason: "the approved rules part holds no examples text"}}
		}
		return checks.ExamplesMatchApproval(c.dir, *rules.Examples)
	},
	"GenerationRepeatable": func(c *call) []checks.CheckResult { return checks.GenerationDrift(c.runner, c.dir) },
	"GeneratedCodeBuilds":  func(c *call) []checks.CheckResult { return checks.GeneratedCodeBuilds(c.runner, c.dir) },
	"TestsBuild":           func(c *call) []checks.CheckResult { return checks.TestsBuild(c.runner, c.dir) },
	"EveryExampleBound":    func(c *call) []checks.CheckResult { return checks.GodogStrict(c.runner, c.dir) },
	"TestsFailAgainstStub": func(c *call) []checks.CheckResult { return checks.TestsFailAgainstStub(c.runner, c.dir) },
	"AllTestsPass":         func(c *call) []checks.CheckResult { return checks.AllTestsPass(c.runner, c.dir) },
	"TestsUnchanged": func(c *call) []checks.CheckResult {
		w, ok := c.earlierWritable("tests")
		if !ok {
			return []checks.CheckResult{{Check: "TestsUnchanged", Reason: "no earlier step has the output tests"}}
		}
		return checks.TestsUnchangedOf(c.notOwn(c.before), c.notOwn(c.project.tree()), w)
	},
	"GeneratedCodeUnchanged": func(c *call) []checks.CheckResult {
		w, ok := c.earlierWritable("generated-code")
		if !ok {
			return []checks.CheckResult{{Check: "GeneratedCodeUnchanged", Reason: "no earlier step has the output generated-code"}}
		}
		return checks.GeneratedCodeUnchangedOf(c.notOwn(c.before), c.notOwn(c.project.tree()), w)
	},
	"FindingsCarryEvidence": func(c *call) []checks.CheckResult {
		return checks.FindingsRerun(c.runner, c.dir, c.findings())
	},
	"NoBlockingFindingOpen": func(c *call) []checks.CheckResult {
		return checks.NoBlockingFindingOpen(c.runner, c.dir, c.findings())
	},
}

// notOwn is t without the files the judged step may write (checks.NotOwn).
func (c *call) notOwn(t checks.Tree) checks.Tree { return checks.NotOwn(t, c.req.Step.Writable) }

// approved is the request's approved part; the schema requires three, and a part missing gives an
// empty one, whose hash no file has.
func (c *call) approved(part gen.ApprovedPartPart) gen.ApprovedPart {
	for _, a := range c.req.Approved {
		if a.Part == part {
			return a
		}
	}
	return gen.ApprovedPart{}
}

// earlierWritable is the writable entries of the earlier step whose outputs hold output: all of
// that step's writable paths may not change (contract, EarlierStep).
func (c *call) earlierWritable(output string) ([]string, bool) {
	for _, e := range c.req.EarlierSteps {
		for _, o := range e.Outputs {
			if o == output {
				return e.Writable, true
			}
		}
	}
	return nil, false
}

func (c *call) findings() []checks.Finding {
	if c.req.Findings == nil {
		return nil
	}
	var out []checks.Finding
	for _, f := range *c.req.Findings {
		cf := checks.Finding{Rule: f.Rule, Text: f.Text, Blocking: f.Blocking}
		if f.File != nil {
			cf.File = *f.File
		}
		if f.Line != nil {
			cf.Line = *f.Line
		}
		out = append(out, cf)
	}
	return out
}
