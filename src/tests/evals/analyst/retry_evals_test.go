//go:build evals

package analyst

// The analyst together with the engine's retry: each trial goes through slices.Service.Create, so
// an unusable proposal is sent back to the analyst (AnalystInput.Previous and Errors), up to the
// catalogue pattern's limits.attempts, as in production. What is graded is the answer (the last
// assistant message in the --json events) of the last attempt, the one Create accepted or, when
// every attempt failed, the last one it refused.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/agentic-platform/engine/slices"
	"github.com/agentic-platform/tests/evals/runevents"
)

// dockerAnalyst implements slices.Analyst by running the harness's analyst container per call.
type dockerAnalyst struct {
	l                   *lab
	agentDir, agentFile string
	calls               int
	last, lastStderr    []byte // answer and stderr of the latest call
	events              []byte // the --json events of every call, in order
	usage               *runevents.Usage
}

func (a *dockerAnalyst) Propose(_ context.Context, in slices.AnalystInput) (slices.Proposal, error) {
	msg := slices.AnalystMessage(in)
	a.calls++
	out, events, usage, stderr, err := a.l.analyst(a.agentDir, a.agentFile, msg)
	a.last, a.lastStderr = out, stderr
	a.events = append(a.events, events...)
	if usage != nil {
		if a.usage == nil {
			a.usage = &runevents.Usage{}
		}
		a.usage.InputTokens += usage.InputTokens
		a.usage.OutputTokens += usage.OutputTokens
		a.usage.CostUSD += usage.CostUSD
	}
	if err != nil {
		return slices.Proposal{}, err
	}
	doc, err := Extract(out)
	if err != nil {
		return slices.Proposal{}, err
	}
	var p slices.Proposal
	if err := json.Unmarshal(doc, &p); err != nil {
		return slices.Proposal{}, fmt.Errorf("the output is not a proposal: %v", err)
	}
	return p, nil
}

// noFindings is a ContractDiff that reports no findings: the evals judge the analyst, not oasdiff.
type noFindings struct{}

func (noFindings) Diff(context.Context, string, string) ([]slices.ContractFinding, error) {
	return nil, nil
}

// analystWithRetry runs one trial through slices.New(...).Create with the real catalogue pattern.
// It returns the answer of the last attempt, the --json events and summed usage of every attempt,
// the last attempt's stderr, the number of attempts and the error: the analyst's or Create's
// refusal (for example OperationFailed after the last attempt).
func (l *lab) analystWithRetry(agentDir, agentFile, description string) (out, events []byte, usage *runevents.Usage, stderr []byte, attempts int, err error) {
	g := l.grader()
	da := &dockerAnalyst{l: l, agentDir: agentDir, agentFile: agentFile}
	svc, err := slices.New(slices.Config{Schemas: g.Schemas, Analyst: da, Diff: noFindings{}, Pattern: g.Pattern})
	if err != nil {
		return nil, nil, nil, nil, 0, err
	}
	_, err = svc.Create(context.Background(), description)
	return da.last, da.events, da.usage, da.lastStderr, da.calls, err
}
