package steps_test

// T054c: an agent attempt's usage comes from the session's token_usage event, and its changed
// lines from the diff of the files it left against the files it started from, never from what the
// agent says (Principle XX).

import (
	"testing"

	"github.com/agentic-platform/engine/canonical"
	"github.com/agentic-platform/engine/machine"
	"github.com/agentic-platform/engine/steps"
)

func agentEvents(claim string, usage map[string]any) []string {
	evs := []string{sse(map[string]any{"type": "agent_info", "model": pinned})}
	if usage != nil {
		evs = append(evs, sse(map[string]any{"type": "token_usage", "usage": usage}))
	}
	return append(evs,
		sse(map[string]any{"type": "agent_choice", "content": claim}),
		sse(map[string]any{"type": "stream_stopped", "reason": "normal"}))
}

func TestAgentUsageComesFromTheSessionEvents(t *testing.T) {
	r := newRig(t)
	r.agent.events = agentEvents("done", map[string]any{"input_tokens": 1200, "output_tokens": 300, "cost": 0.0042})
	res, err := steps.NewWorker(r.cfg).Run(ctx5(t), r.agentReq(testsStep))
	if err != nil {
		t.Fatal(err)
	}
	if res.Usage.Tokens != 1500 || res.Usage.CostUSD != 0.0042 {
		t.Fatalf("usage: %+v (want 1500 tokens, $0.0042)", res.Usage)
	}
}

func TestAgentWithoutUsageEventReportsNone(t *testing.T) {
	r := newRig(t)
	r.agent.events = agentEvents("done", nil)
	res, err := steps.NewWorker(r.cfg).Run(ctx5(t), r.agentReq(testsStep))
	if err != nil || res.Usage.Tokens != 0 || res.Usage.CostUSD != 0 {
		t.Fatalf("%+v %v", res.Usage, err)
	}
}

func TestChangedLinesComeFromTheDiffNotTheAgentsClaim(t *testing.T) {
	r := newRig(t)
	oldB := []byte("Feature: b\n")
	// the attempt leaves a_test.go changed on line 2 and 4 (below), b.feature as it was, and a new file
	newA := []byte("package tests\nvar changed = 1\nvar same = 2\nvar added = 3\n")
	startA := []byte("package tests\nvar old = 1\nvar same = 2\n")
	r.chk.files = []canonical.File{
		{Path: "tests/a_test.go", Content: newA},
		{Path: "tests/b.feature", Content: oldB},
		{Path: "tests/new_test.go", Content: []byte("package tests\nvar n = 1\n")},
	}
	loads := map[string][]byte{hex256(startA): startA, hex256(oldB): oldB}
	r.cfg.Load = func(h string) ([]byte, error) { return loads[h], nil }
	req := r.agentReq(testsStep)
	req.Before = []machine.File{{Path: "tests/a_test.go", SHA256: hex256(startA)}, {Path: "tests/b.feature", SHA256: hex256(oldB)}}
	r.agent.events = agentEvents(`{"changed_lines":[{"file":"tests/b.feature","line":1},{"file":"tests/zzz.go","line":99}]}`, nil)

	res, err := steps.NewWorker(r.cfg).Run(ctx5(t), req)
	if err != nil {
		t.Fatal(err)
	}
	got := map[machine.Line]bool{}
	for _, l := range res.ChangedLines {
		got[l] = true
	}
	for _, want := range []machine.Line{{File: "tests/a_test.go", Line: 2}, {File: "tests/a_test.go", Line: 4},
		{File: "tests/new_test.go", Line: 1}, {File: "tests/new_test.go", Line: 2}} {
		if !got[want] {
			t.Errorf("changed line %v is missing from %v", want, res.ChangedLines)
		}
	}
	for _, bad := range []machine.Line{{File: "tests/a_test.go", Line: 1}, {File: "tests/a_test.go", Line: 3},
		{File: "tests/b.feature", Line: 1}, {File: "tests/zzz.go", Line: 99}} {
		if got[bad] {
			t.Errorf("line %v is not changed (or only claimed) but is reported: %v", bad, res.ChangedLines)
		}
	}
}
