package steps_test

// T076, review round 1: a finding's witness whose file is not a project path never reaches the
// check service (the contract would refuse the request); the finding itself is kept, without
// the witness, so it is a suggestion. By the test author.

import (
	"fmt"
	"testing"

	"github.com/agentic-platform/engine/machine"
	"github.com/agentic-platform/engine/steps"
)

func TestWitnessWithANonProjectPathIsDroppedAndTheFindingKept(t *testing.T) {
	for _, bad := range []string{"/etc/review/r1/a_test.go", "review/r1/../../etc/a_test.go", "../review/r1/a_test.go", "review//r1/a_test.go"} {
		t.Run(fmt.Sprintf("%q", bad), func(t *testing.T) {
			r, _ := witnessRig(t, `{"findings":[]}`)
			f := witnessFinding()
			f.Witness = &machine.Witness{File: bad, Test: witnessTest}
			res := machine.Result{Outputs: map[string]string{"findings": hashB, "witnesses": hashC}, Findings: []machine.Finding{f},
				ProjectFiles: toFiles(r.chk.project)}
			if _, err := steps.NewChecker(r.cfg).Check(ctx5(t), machine.CheckRequest{Run: "RUN-7", Slice: "SLICE-1", Project: "proj-1",
				Step: r.cfg.Pattern.Steps[3], Attempt: 1, Round: 1, Name: "NoBlockingFindingOpen", Result: res, Inputs: []string{hashA}}); err != nil {
				t.Fatal(err)
			}
			cs := r.chk.rec.find("POST", "/checks/NoBlockingFindingOpen/run")
			if len(cs) != 1 {
				t.Fatalf("calls: %d", len(cs))
			}
			fs, _ := cs[0].body["findings"].([]any)
			if len(fs) != 1 {
				t.Fatalf("the finding was dropped, want it kept as a suggestion: %v", cs[0].body["findings"])
			}
			if w, ok := fs[0].(map[string]any)["witness"]; ok {
				t.Fatalf("a witness with the file %q reached the wire: %v", bad, w)
			}
		})
	}
}
