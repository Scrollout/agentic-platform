package cli

import (
	_ "embed"
	"fmt"

	"github.com/agentic-platform/retry"
)

// engineContract is the engine contract (scripts/generate.sh copies it here, "Embedded copies").
// The path names one version; when a newer engine contract version exists, change it with the
// generated client.
//
//go:embed gen/engine/api/preview/2026-11-12-preview/openapi.yaml
var engineContract []byte

// The retry rule is the one the engine contract gives in its x-limits, read from the contract the
// generated client embeds, so the client and the engine apply the same one.

// retryRuleOf returns the rule an OpenAPI document gives.
func retryRuleOf(openapi []byte) (retry.Rule, error) { return retry.Parse(openapi) }

// retryRule returns the rule of the embedded engine contract.
func retryRule() retry.Rule {
	r, err := retryRuleOf(engineContract)
	if err != nil {
		panic(fmt.Sprintf("engine contract: %v", err))
	}
	return r
}

// tries is the most times one call is sent: the first try and the retries of the rule.
func tries() int { return 1 + retryRule().Retries }
