package main

// T049c: an agent step whose `agent_digest` in the pattern is not the digest of its embedded
// definition stops the engine, naming the step (wireSteps, Principle XVII). The seam is the
// package variable `pattern` that setup reads; the test swaps it for a copy with one digest wrong.
// Written by a separate author from the implementation.

import (
	"regexp"
	"strings"
	"testing"
)

func TestSetupRefusesAPatternWithAWrongAgentDigestNamingTheStep(t *testing.T) {
	orig := pattern
	t.Cleanup(func() { pattern = orig })
	lint, analyst := fakeLint(t), fakeAnalyst(t, nil)

	// Control: the embedded pattern itself starts (else the planted failure proves nothing).
	pattern = orig
	if _, closeFn, err := setupWith(goodEnv(lint.URL, analyst.URL, t.TempDir()).get, testDurable(t)); err != nil {
		t.Fatalf("the unmodified pattern does not start: %v", err)
	} else {
		closeFn()
	}

	for _, step := range []string{"tests", "code", "review"} {
		re := regexp.MustCompile(`(?m)(name: ` + step + `\n(?:.*\n)*?\s+agent_digest: ")[0-9a-f]{64}(")`)
		if !re.Match(orig) {
			t.Fatalf("pattern has no agent_digest for step %s", step)
		}
		done := false
		planted := re.ReplaceAllFunc(orig, func(m []byte) []byte {
			if done {
				return m
			}
			done = true
			return re.ReplaceAll(m, []byte("${1}"+strings.Repeat("0", 64)+"${2}"))
		})
		pattern = planted
		h, closeFn, err := setup(goodEnv(lint.URL, analyst.URL, t.TempDir()).get)
		if err == nil {
			closeFn()
			t.Errorf("%s: the engine started with a wrong agent_digest", step)
			continue
		}
		if h != nil {
			t.Errorf("%s: a handler came back with an error", step)
		}
		if !strings.Contains(err.Error(), "agent_digest") || !strings.Contains(err.Error(), step) {
			t.Errorf("%s: the error must name the step and agent_digest: %v", step, err)
		}
	}
}
