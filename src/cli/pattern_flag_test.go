// T039c: the "pattern does not fit" line is driven by the check result's boolean `pattern`, not by a
// list of names in the client. A failed check with pattern true is named; a failed check with
// pattern false is not, whatever its name.
package cli_test

import "testing"

func flagged(part, name, reason string, pattern bool) map[string]any {
	c := failedCheck(part, name, reason)
	c["pattern"] = pattern
	return c
}

func TestMisfitLineNamesChecksFlaggedAsPattern(t *testing.T) {
	f := newFake(t)
	f.reply("GET /slices/S-1", 200, slice(sliceOpts{checks: []map[string]any{
		flagged("contract", "BrandNewCondition", "the contract has a new problem", true),
		flagged("goals", "Traceability", "G-1 has no rule", false),
	}}))
	r := f.run("", "show", "S-1")
	wantExit(t, r, 0)
	wantContains(t, "show stdout", r.out, "does not fit", "BrandNewCondition", "the contract has a new problem")
	wantAbsent(t, "show stdout", r.out, "G-1 has no rule: ")
}

func TestPatternNamedCheckWithoutTheFlagIsNoMisfit(t *testing.T) {
	f := newFake(t)
	f.reply("GET /slices/S-1", 200, slice(sliceOpts{checks: []map[string]any{
		flagged("contract", "OnlyHttpOperations", "the contract has no HTTP operation", false),
	}}))
	r := f.run("", "show", "S-1")
	wantExit(t, r, 0)
	wantAbsent(t, "show stdout", r.out, "does not fit")
}
