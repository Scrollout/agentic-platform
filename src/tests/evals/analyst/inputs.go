package analyst

// Harness inputs (spec FR-010, research R3): which cases a live run runs and how many trials
// each gets, read from EVAL_CASES and EVAL_TRIALS.

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// maxTrials is the largest trial count EVAL_TRIALS may ask for.
const maxTrials = 20

var trialCount = regexp.MustCompile(`^[1-9][0-9]?$`)

// Selection returns the description cases and the revision cases that env (the value of
// EVAL_CASES) names, each in its own suite's order. env is a comma list of case names of either
// suite; spaces around a name are trimmed and a repeated name counts once. An empty env selects
// every case of both suites. A suite none of whose cases is named comes back empty. It fails,
// naming the offender, on a name in neither suite (names are case-sensitive) or on an empty or
// blank name, such as the one a trailing comma leaves.
func Selection(env string) (cases []Case, revisions []RevisionCase, err error) {
	all, allRev := Cases(), RevisionCases()
	if env == "" {
		return all, allRev, nil
	}
	known := map[string]bool{}
	for _, c := range all {
		known[c.Name] = true
	}
	for _, c := range allRev {
		known[c.Name] = true
	}
	want := map[string]bool{}
	for _, raw := range strings.Split(env, ",") {
		n := strings.TrimSpace(raw)
		if n == "" {
			return nil, nil, fmt.Errorf("EVAL_CASES %q holds an empty case name", env)
		}
		if !known[n] {
			return nil, nil, fmt.Errorf("EVAL_CASES names %q, which is a case of neither analyst suite", n)
		}
		want[n] = true
	}
	for _, c := range all {
		if want[c.Name] {
			cases = append(cases, c)
		}
	}
	for _, c := range allRev {
		if want[c.Name] {
			revisions = append(revisions, c)
		}
	}
	return cases, revisions, nil
}

// TrialCount returns k, how many times a live run runs each case (a case passes, pass^k, only
// when all k trials pass). env is the value of EVAL_TRIALS: empty gives Trials; otherwise it must
// be a whole number from 1 to 20 written in plain ASCII digits with no sign, leading zero or
// space. Anything else is an error.
func TrialCount(env string) (int, error) {
	if env == "" {
		return Trials, nil
	}
	if !trialCount.MatchString(env) {
		return 0, fmt.Errorf("EVAL_TRIALS %q is not a whole number from 1 to %d", env, maxTrials)
	}
	k, err := strconv.Atoi(env)
	if err != nil || k > maxTrials {
		return 0, fmt.Errorf("EVAL_TRIALS %q is not a whole number from 1 to %d", env, maxTrials)
	}
	return k, nil
}
