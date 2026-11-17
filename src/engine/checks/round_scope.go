package checks

import "fmt"

// RoundScope is what a review round after the first was given (check contract, round_scope;
// research D55): the findings the round before recorded as blocking, of which only the rule and
// file are read, and the lines the fix before the round changed.
type RoundScope struct {
	Open    []Finding
	Changed []ScopeLine
}

// ScopeLine is one line, numbered from 1, of a project file.
type ScopeLine struct {
	File string
	Line int
}

// WithinRoundScope applies a later review round's scope to NoBlockingFindingOpen's results
// (FR-017; constitution, Review converges): a confirmed finding stays confirmed only when it
// repeats an open finding (the same rule and file) or its file and line are among the changed
// lines. Each other confirmed finding becomes a suggestion: its result no longer confirms it and
// names no ref, and its reason says `finding <position> (<rule> <file>:<line>) is outside round
// <round>'s scope: a suggestion`, position being 0-based in findings. These results fail only
// when another result still fails, so the reason always names them and the check passes when
// nothing else is open.
func WithinRoundScope(results []CheckResult, findings []Finding, round int, s RoundScope) []CheckResult {
	var kept, outside []CheckResult
	failing := false
	for _, r := range results {
		if r.Confirmed != nil && *r.Confirmed >= 0 && *r.Confirmed < len(findings) && !s.holds(findings[*r.Confirmed]) {
			f := findings[*r.Confirmed]
			outside = append(outside, CheckResult{Part: r.Part, Check: r.Check, Reason: fmt.Sprintf(
				"finding %d (%s %s:%d) is outside round %d's scope: a suggestion", *r.Confirmed, f.Rule, f.File, f.Line, round)})
			continue
		}
		failing = failing || !r.Passed
		kept = append(kept, r)
	}
	if len(outside) == 0 {
		return results
	}
	for i := range outside {
		outside[i].Passed = !failing
	}
	if !failing {
		// A guard: NoBlockingFindingOpen gives a passing result only when it confirms nothing, so
		// kept holds nothing here; were it to, its "none confirmed" would contradict the suggestions.
		kept = nil
	}
	return append(kept, outside...)
}

// holds reports whether f is within the scope: a repeat of an open finding, or on a changed line.
func (s RoundScope) holds(f Finding) bool {
	for _, o := range s.Open {
		if o.Rule == f.Rule && o.File == f.File {
			return true
		}
	}
	for _, l := range s.Changed {
		if l.File == f.File && l.Line == f.Line {
			return true
		}
	}
	return false
}
