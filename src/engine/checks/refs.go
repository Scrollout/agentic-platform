package checks

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	gherkin "github.com/cucumber/gherkin/go/v42"
	messages "github.com/cucumber/messages/go/v34"

	slicedef "github.com/agentic-platform/slice"
)

// failedScenarioRefs are the slice refs (data-model "Refs", T104) of the scenarios of the
// project's features/slice.feature whose godog subtest failed in `go test -json` output, each
// `slice.ScenarioRef` (package slicedef here) of its scenario, once, in the file's order; a scenario without both tags
// and a feature file that cannot be read name none. With unboundOnly, only a subtest whose output
// carries godog's error for an undefined, pending or ambiguous step counts.
//
// godog (with `TestingT`) runs each scenario, an Outline once per Examples row, as one subtest of
// the suite's test, in the file's order. So when a test's subtests are as many as the file's
// compiled scenarios (gherkin pickles) and each has its pickle's name as go test writes it
// (white space as `_`, a repeated name with go test's `#<nn>`), its n-th subtest is the n-th
// pickle; another test's subtests, such as a table test's, never match so. Otherwise a failed subtest is located by the `# <path>/slice.feature:<line>` godog prints
// after its scenario's header in the `pretty` format the tests use; a later Outline row prints
// none, so it is then not named. Limits: a scenario whose name holds `/` runs as a nested
// subtest and is never named; a scenario with no steps runs no subtest; a name go test escapes
// (a non-printable rune) leaves only the location fallback.
func failedScenarioRefs(project, output string, unboundOnly bool) []string {
	tests := subtestsOf(output)
	b, err := os.ReadFile(filepath.Join(project, "features", "slice.feature"))
	if err != nil || len(tests) == 0 {
		return nil
	}
	ids := &messages.Incrementing{}
	gd, err := gherkin.ParseGherkinDocument(bytes.NewReader(b), ids.NewId)
	if err != nil || gd.Feature == nil {
		return nil
	}
	var scenarios []*messages.Scenario // in the file's order
	for _, c := range gd.Feature.Children {
		if c.Scenario != nil {
			scenarios = append(scenarios, c.Scenario)
		}
		if c.Rule != nil {
			for _, rc := range c.Rule.Children {
				if rc.Scenario != nil {
					scenarios = append(scenarios, rc.Scenario)
				}
			}
		}
	}
	index := map[string]int{} // scenario AST id to its place in scenarios
	for i, sc := range scenarios {
		index[sc.Id] = i
	}
	pickles := gherkin.Pickles(*gd, "features/slice.feature", ids.NewId)
	hit := make([]bool, len(scenarios))
	counts := func(s subtest) bool { return s.failed && (!unboundOnly || unboundStep.MatchString(s.text)) }
	for _, subs := range tests {
		if picklesRun(subs, pickles) {
			for n, s := range subs {
				if i, ok := index[pickles[n].AstNodeIds[0]]; ok && counts(s) {
					hit[i] = true
				}
			}
			continue
		}
		for _, s := range subs {
			if !counts(s) {
				continue
			}
			for _, m := range scenarioLocation.FindAllStringSubmatch(s.text, -1) {
				line, err := strconv.Atoi(m[1])
				if err != nil {
					continue
				}
				for i, sc := range scenarios {
					if int(sc.Location.Line) == line {
						hit[i] = true
					}
				}
			}
		}
	}
	var out []string
	for i, sc := range scenarios {
		if ref := slicedef.ScenarioRef(sc); hit[i] && ref != "" {
			out = append(out, ref)
		}
	}
	return out
}

// picklesRun is whether subs are godog's subtests for pickles: as many, in order, each named as
// go test names a subtest t.Run(pickle.Name).
func picklesRun(subs []subtest, pickles []*messages.Pickle) bool {
	if len(subs) != len(pickles) {
		return false
	}
	for n, s := range subs {
		want := strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return '_'
			}
			return r
		}, pickles[n].Name)
		if s.name != want && repeatSuffix.ReplaceAllString(s.name, "") != want {
			return false
		}
	}
	return true
}

// repeatSuffix is what go test adds to a subtest name already used in its parent.
var repeatSuffix = regexp.MustCompile(`#[0-9]+$`)

// scenarioLocation is the location godog prints after a scenario's header.
var scenarioLocation = regexp.MustCompile(`#\s+(?:\S*/)?slice\.feature:([0-9]+)`)

// unboundStep is godog's error text for an undefined, pending or ambiguous step (godog's
// ErrUndefined, ErrPending and ErrAmbiguous).
var unboundStep = regexp.MustCompile(`step is undefined|step implementation is pending|ambiguous step definition`)

// subtest is one direct subtest of a top-level test: its name, whether it failed, and its output
// without colours.
type subtest struct {
	name   string // its name below the top-level test
	failed bool
	text   string
}

// subtestsOf are, per package and top-level test, its direct subtests in the order they ran.
func subtestsOf(output string) map[string][]subtest {
	type place struct {
		parent string
		n      int
	}
	out := map[string][]subtest{}
	where := map[string]place{} // package and subtest name to its place
	texts := map[string]*strings.Builder{}
	sc := bufio.NewScanner(strings.NewReader(output))
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		var e testEvent
		line := sc.Text()
		if !strings.HasPrefix(line, "{") || json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		top, rest, ok := strings.Cut(e.Test, "/")
		if !ok || strings.Contains(rest, "/") {
			continue
		}
		parent, name := e.Package+"\x00"+top, e.Package+"\x00"+e.Test
		switch e.Action {
		case "run":
			if _, seen := where[name]; !seen {
				where[name] = place{parent, len(out[parent])}
				out[parent] = append(out[parent], subtest{name: rest})
				texts[name] = &strings.Builder{}
			}
		case "output":
			if b := texts[name]; b != nil {
				b.WriteString(e.Output)
			}
		case "fail":
			if w, seen := where[name]; seen {
				out[w.parent][w.n].failed = true
			}
		}
	}
	for name, w := range where {
		out[w.parent][w.n].text = ansiColour.ReplaceAllString(texts[name].String(), "")
	}
	return out
}

// withRefs gives the first failing result of rs the refs, when there are any.
func withRefs(rs []CheckResult, refs []string) []CheckResult {
	if len(refs) == 0 {
		return rs
	}
	for i := range rs {
		if !rs[i].Passed {
			rs[i].Refs = refs
			break
		}
	}
	return rs
}

// ruleRefs is a finding's rule as a ref, or none when it has no rule.
func ruleRefs(f Finding) []string {
	if f.Rule == "" {
		return nil
	}
	return []string{f.Rule}
}
