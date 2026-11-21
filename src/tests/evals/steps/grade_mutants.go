package steps

// MutantsKilled (README "Mutants", research R1, R3 and R4): the tests agent's tests, judged on
// whether they catch the planted bugs of the reference implementation.

import (
	"fmt"
	"sort"
	"strings"
)

// GMutants is the mutants' grader.
const GMutants = "MutantsKilled"

// MutantsKilled builds the tests under tests/ of a scratch copy of project (the project itself is
// never written) against the reference implementation and against each mutant of Mutants(), each
// an overlay of internal/app/app.go, and runs every test binary alone. When the tests do not
// build, or do not all pass, against the reference, it fails with that reason, no counts, and
// judges no mutant. Otherwise a mutant is killed when a run of one of its binaries reports a
// failed test or a panic; a survivor is named with " (timed out)" when a run of it timed out, else
// " (did not run)" when it did not build or a run did not run. It passes when all 8 are killed.
func (e Env) MutantsKilled(project string) Result {
	ref := Ref("impl")[implFile]
	if ref == "" {
		return fail(GMutants, "the reference implementation %s cannot be read", implFile)
	}
	ms := Mutants()
	vs := []Variant{{Name: "reference", Overlay: map[string]string{implFile: ref}}}
	for _, m := range ms {
		vs = append(vs, Variant{Name: "mutant " + m.Name, Overlay: map[string]string{implFile: strings.Replace(ref, m.from, m.to, 1)}})
	}
	run := VariantRun{Runner: e.variantRunner(), Pkg: "tests"}
	res, err := run.Variants(project, vs)
	if err != nil {
		return fail(GMutants, "the mutants cannot be set up: %v", err)
	}
	if b := res[0].BuildError; b != "" {
		return fail(GMutants, "the tests do not build against the reference implementation: %s", firstLines(b, 10))
	}
	if len(res[0].Binaries) == 0 {
		return fail(GMutants, "the tests do not pass against the reference implementation: no test binary under tests/")
	}
	for _, b := range res[0].Binaries {
		if b.Run != OutcomePassed {
			return fail(GMutants, "the tests do not pass against the reference implementation: %s %s", b.Dir, b.Run)
		}
	}
	c := &Counts{Total: len(ms), Failed: []string{}}
	for i, m := range ms {
		switch o := mutantOutcome(res[i+1]); o {
		case OutcomeFailed:
			c.Passed++
		case OutcomePassed:
			c.Failed = append(c.Failed, m.Name)
		default:
			c.Failed = append(c.Failed, m.Name+" ("+string(o)+")")
		}
	}
	sort.Strings(c.Failed)
	r := Result{Grader: GMutants, Passed: c.Passed == c.Total, Counts: c,
		Reason: fmt.Sprintf("%d of %d mutants killed", c.Passed, c.Total)}
	if len(c.Failed) > 0 {
		r.Reason += "; survived: " + strings.Join(c.Failed, ", ")
	}
	return r
}

// mutantOutcome is OutcomeFailed when a binary of the mutant's build reported a failed test or a
// panic (killed); else OutcomeTimedOut when a run timed out; else OutcomeDidNotRun when it did not
// build or a run did not run; else OutcomePassed (it survived every run).
func mutantOutcome(v VariantResult) Outcome {
	if v.BuildError != "" {
		return OutcomeDidNotRun
	}
	o := OutcomePassed
	for _, b := range v.Binaries {
		switch b.Run {
		case OutcomeFailed:
			return OutcomeFailed
		case OutcomeTimedOut:
			o = OutcomeTimedOut
		case OutcomeDidNotRun:
			if o == OutcomePassed {
				o = OutcomeDidNotRun
			}
		}
	}
	return o
}
