package cli

// T007 of feature 002 (research R5). The client's retry rule is the one the engine contract gives
// that its generated code embeds (src/cli/api/gen, oapi-codegen's embedded spec). Interface the
// code author provides in this package (retry is github.com/agentic-platform/retry, specified in
// src/retry/retry_test.go):
//
//	func retryRule() retry.Rule                              // the rule of the embedded engine contract
//	func retryRuleOf(openapi []byte) (retry.Rule, error)     // the rule an OpenAPI document gives;
//	                                                         // retryRule reads the embedded spec through it

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/agentic-platform/retry"
)

const engineContractFile = "../engine/api/preview/2026-11-12-preview/openapi.yaml"

func TestRetryRuleIsTheEngineContractsRuleFromDisk(t *testing.T) {
	raw, err := os.ReadFile(engineContractFile)
	if err != nil {
		t.Fatal(err)
	}
	want, err := retry.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if want.Retries != 3 {
		t.Fatalf("the contract's retries = %d, want 3", want.Retries)
	}
	if got := retryRule(); got != want {
		t.Fatalf("retryRule() = %+v, openapi.yaml gives %+v", got, want)
	}
}

func TestRetryRuleFollowsAPlantedContract(t *testing.T) {
	raw, err := os.ReadFile(engineContractFile)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for old, repl := range map[string]string{
		"  retry-attempts: 3\n":            "  retry-attempts: 7\n",
		"  retry-first-wait-ms: 1000\n":    "  retry-first-wait-ms: 250\n",
		"  retry-longest-wait-ms: 30000\n": "  retry-longest-wait-ms: 900\n",
		"  retry-jitter-fraction: 0.5\n":   "  retry-jitter-fraction: 0.2\n",
	} {
		if strings.Count(s, old) != 1 {
			t.Fatalf("the contract has no single line %q", old)
		}
		s = strings.Replace(s, old, repl, 1)
	}
	got, err := retryRuleOf([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	want := retry.Rule{Retries: 7, First: 250 * time.Millisecond, Longest: 900 * time.Millisecond, Jitter: 0.2}
	if got != want {
		t.Fatalf("rule = %+v, want %+v", got, want)
	}
}
