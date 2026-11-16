// Tests that a Service with no Lint configured fails closed: the contract is not shown to follow
// OpenAPI's rules, so it cannot be approved.
package slices_test

import (
	"context"
	"strings"
	"testing"

	"github.com/agentic-platform/engine/slices"
)

func nolintFailed(t *testing.T, sl slices.Slice) {
	t.Helper()
	cs := checksNamed(sl, slices.CheckContractLint)
	if len(cs) != 1 || cs[0].Passed || cs[0].Part != "contract" {
		t.Fatalf("want exactly one failed ContractLint on the contract: %+v", cs)
	}
	if !strings.Contains(strings.ToLower(cs[0].Reason), "lint") {
		t.Errorf("the reason must say no lint is configured: %q", cs[0].Reason)
	}
}

func TestNilLintFailsTheContractLintCheck(t *testing.T) {
	s := lintService(t, nil)
	nolintFailed(t, create(t, s, "Let me keep notes."))
}

func TestNilLintRefusesApprovingTheContract(t *testing.T) {
	s := lintService(t, nil)
	sl := create(t, s, "Let me keep notes.")
	_, err := s.Approve(context.Background(), sl.ID, slices.PartContract, sl.Parts.Contract)
	if code(t, err) != "PartFailsChecks" {
		t.Errorf("the contract must not be approvable unlinted: %v", err)
	}
}

func TestNilLintStillFailsAfterReplacingTheContract(t *testing.T) {
	s := lintService(t, nil)
	sl := create(t, s, "Let me keep notes.")
	got, err := s.ReplaceContract(context.Background(), sl.ID, sl.Contract+"# edited\n")
	if err != nil {
		t.Fatal(err)
	}
	nolintFailed(t, got)
}
