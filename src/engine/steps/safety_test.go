package steps_test

// T049b: an agent step's session runs with safety_policy autonomous, inside the step container's
// gVisor sandbox (research D13), so the agent can use its tools; the analyst's stays restricted
// (pinned in analyst/http_test.go).

import (
	"testing"

	"github.com/agentic-platform/engine/steps"
)

func TestAgentStepSessionIsAutonomous(t *testing.T) {
	r := newRig(t)
	if _, err := steps.NewWorker(r.cfg).Run(ctx5(t), r.agentReq(testsStep)); err != nil {
		t.Fatal(err)
	}
	creates := r.agent.rec.find("POST", "/api/sessions")
	if len(creates) != 1 || creates[0].body["safety_policy"] != "autonomous" {
		t.Fatalf("session create: %+v", creates)
	}
}
