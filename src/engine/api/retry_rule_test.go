package api

// T007 of feature 002 (research R5). The engine's retry rule is the one its embedded contract's
// x-limits gives. Interface the code author provides in this package (retry is
// github.com/agentic-platform/retry, specified in src/retry/retry_test.go):
//
//	func RetryRule() retry.Rule                              // the rule of the embedded contract
//	func retryRuleOf(openapi []byte) (retry.Rule, error)     // the rule an OpenAPI document gives

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/agentic-platform/retry"
)

const retryContractFile = "preview/2026-11-12-preview/openapi.yaml"

func TestRetryRuleIsTheEmbeddedContractsRuleFromDisk(t *testing.T) {
	raw, err := os.ReadFile(retryContractFile)
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
	if got := RetryRule(); got != want {
		t.Fatalf("RetryRule() = %+v, openapi.yaml gives %+v", got, want)
	}
}

func TestRetryRuleFollowsAPlantedContract(t *testing.T) {
	raw, err := os.ReadFile(retryContractFile)
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
