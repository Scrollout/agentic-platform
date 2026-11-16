// T039b: when the pattern does not fit the description, describe and show say so plainly. Written by a
// separate author, in a new file, to fail until the client does.
//
// Convention these tests pin (the implementer states it in contract.md, under the command table): a
// slice whose checks include a failing pattern condition (ContractIsOpenApi31, OnlyHttpOperations,
// ContractRefsLocal, RulesNameOneSystem) makes `describe` and `show` print, in addition to today's
// output, a line containing "does not fit" that names the pattern and, for each failing condition,
// its name and reason. A slice with no failing pattern condition prints no such line, even when
// another check fails.
package cli_test

import (
	"strings"
	"testing"
)

const noHTTPReason = "the contract has no HTTP operation"

func nonFittingSlice() map[string]any {
	return slice(sliceOpts{state: "questions-open", checks: []map[string]any{
		{"part": "goals", "check": "GoalsPresent", "passed": true, "reason": "one goal"},
		{"part": "contract", "check": "OnlyHttpOperations", "passed": false, "reason": noHTTPReason, "pattern": true},
		{"part": "contract", "check": "ContractIsOpenApi31", "passed": true, "reason": "the contract is OpenAPI 3.1.0"},
	}})
}

func wantDoesNotFit(t *testing.T, what, out string) {
	t.Helper()
	wantContains(t, what, out, "does not fit", "OnlyHttpOperations", noHTTPReason)
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "does not fit") && !strings.Contains(l, "pattern") {
			t.Errorf("the line %q says the pattern does not fit without naming the pattern", l)
		}
	}
}

func TestDescribeSaysWhenThePatternDoesNotFit(t *testing.T) {
	f := newFake(t)
	describeRoutes(f, nonFittingSlice())
	r := f.run("", "describe", "A command-line tool in Python that renames files.")
	wantExit(t, r, 0)
	wantDoesNotFit(t, "describe stdout", r.out)
	wantContains(t, "describe stdout", r.out, "S-1", "Customer cancels an order", "questions-open")
}

func TestShowSaysWhenThePatternDoesNotFit(t *testing.T) {
	f := newFake(t)
	f.reply("GET /slices/S-1", 200, nonFittingSlice())
	r := f.run("", "show", "S-1")
	wantExit(t, r, 0)
	wantDoesNotFit(t, "show stdout", r.out)
	wantContains(t, "show stdout", r.out, "customer cancels an unshipped order", "rule-sentence-marker", "contract-text-marker", hContract)
}

func TestFittingSliceHasNoDoesNotFitLine(t *testing.T) {
	f := newFake(t)
	fit := slice(sliceOpts{checks: []map[string]any{
		{"part": "contract", "check": "OnlyHttpOperations", "passed": true, "reason": "the contract has 2 HTTP operations"},
		{"part": "rules", "check": "RulesNameOneSystem", "passed": true, "reason": "all 1 rules name the system \"the shop\""},
	}})
	describeRoutes(f, fit)
	r := f.run("", "describe", "a customer can cancel an order")
	wantExit(t, r, 0)
	wantAbsent(t, "describe stdout", r.out, "does not fit", "not fit")
	r = f.run("", "show", "S-1")
	wantExit(t, r, 0)
	wantAbsent(t, "show stdout", r.out, "does not fit", "not fit")
}

func TestOtherFailingCheckIsNotAPatternMisfit(t *testing.T) {
	f := newFake(t)
	f.reply("GET /slices/S-1", 200, failingSlice())
	r := f.run("", "show", "S-1")
	wantExit(t, r, 0)
	wantAbsent(t, "show stdout", r.out, "does not fit")
}
