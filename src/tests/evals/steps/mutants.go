package steps

import (
	"fmt"
	"regexp"
	"strings"
)

// Mutant is one planted bug of the reference implementation (README "Mutants").
type Mutant struct {
	Name     string // "<rule>/<what>", e.g. "R-2/always-one"; unique
	KilledBy string // the example tag that reaches the bug, e.g. "E-3"
	plantSpec
}

// Lines of the reference internal/app/app.go that the mutants replace.
const (
	refListLine   = "\twrite(w, http.StatusOK, s.notes)\n"
	refIDLine     = "\tn := gen.Note{Id: len(s.notes) + 1, Text: in.Text}\n"
	refCreateLine = "\twrite(w, http.StatusCreated, n)\n"
	refEmptyLine  = "\tif err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Text == \"\" {\n"
)

// mutants is the one home of every planted bug: the review cases take plantR2 and plantR3 from it.
var mutants = []Mutant{
	{"R-1/newest-first", "E-1", planted("R-1", "out[len(out)-1-i] = n",
		refListLine,
		"\tout := make([]gen.Note, len(s.notes)); for i, n := range s.notes { out[len(out)-1-i] = n }; write(w, http.StatusOK, out)\n",
		"the list comes back newest first")},
	// Guarded so an empty list still answers []: only a test that checks the listed notes kills it.
	{"R-1/drops-first", "E-1", planted("R-1", "s.notes[min(1, len(s.notes)):]",
		refListLine,
		"\twrite(w, http.StatusOK, s.notes[min(1, len(s.notes)):])\n",
		"the list leaves out the first note")},
	{"R-2/from-last", "E-2", planted("R-2", "s.notes[len(s.notes)-1]",
		refIDLine,
		"\tn := gen.Note{Id: s.notes[len(s.notes)-1].Id + 1, Text: in.Text} // continue from the last note\n",
		"the next id is read from the last note, which panics for the first")},
	{"R-2/zero-based", "E-2", planted("R-2", "Id: len(s.notes), Text",
		refIDLine,
		"\tn := gen.Note{Id: len(s.notes), Text: in.Text}\n",
		"the first note gets id 0")},
	{"R-2/always-one", "E-3", planted("R-2", "Id: 1, Text",
		refIDLine,
		"\tn := gen.Note{Id: 1, Text: in.Text}\n",
		"every note gets id 1")},
	{"R-2/status-200", "E-2", planted("R-2", "http.StatusOK, n)",
		refCreateLine,
		"\twrite(w, http.StatusOK, n)\n",
		"a created note is answered 200, not 201")},
	{"R-3/first-byte", "E-4", planted("R-3", "in.Text[0]",
		"if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Text == \"\" {",
		"if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Text[0] == 0 {",
		"empty text is tested by reading its first byte, which panics")},
	{"R-3/accepts-empty", "E-4", planted("R-3", "Decode(&in); err != nil {",
		refEmptyLine,
		"\tif err := json.NewDecoder(r.Body).Decode(&in); err != nil {\n",
		"empty text is stored and answered 201")},
}

// Mutants returns research R1's 8 mutants in order (a copy: callers may change it).
func Mutants() []Mutant { return append([]Mutant(nil), mutants...) }

// mutantPlant is the plant of the named mutant; a missing name is a programming error.
func mutantPlant(name string) plantSpec {
	for _, m := range mutants {
		if m.Name == name {
			return m.plantSpec
		}
	}
	panic("no mutant " + name)
}

var (
	featureRuleRe = regexp.MustCompile(`^Rule:\s*(R-\d+)\b`)
	exampleTagRe  = regexp.MustCompile(`^E-\d+$`)
)

// featureExamples maps each rule of a .feature text (its "Rule: R-<n>" lines) to the example
// tags (@E-<n>) of the scenarios under it.
func featureExamples(feature string) (rules []string, examples map[string]map[string]bool) {
	examples = map[string]map[string]bool{}
	cur := ""
	for _, l := range strings.Split(feature, "\n") {
		l = strings.TrimSpace(l)
		if m := featureRuleRe.FindStringSubmatch(l); m != nil {
			cur = m[1]
			if examples[cur] == nil {
				rules = append(rules, cur)
				examples[cur] = map[string]bool{}
			}
			continue
		}
		if cur == "" || !strings.HasPrefix(l, "@") {
			continue
		}
		for _, f := range strings.Fields(l) {
			if t := strings.TrimPrefix(f, "@"); exampleTagRe.MatchString(t) {
				examples[cur][t] = true
			}
		}
	}
	return rules, examples
}

// CheckMutants reports the first violation of the mutant table's rules, or nil: an empty or
// duplicate name, a name whose prefix before "/" is not its rule, a from that does not occur
// exactly once in ref (the reference app.go's text), from equal to to, a marker not in its to
// line, a mutant whose rule is not a rule of feature (the .feature text), a KilledBy that is not
// an example tag of a scenario under the mutant's rule, and a rule of feature with no mutant.
func CheckMutants(ms []Mutant, ref, feature string) error {
	rules, examples := featureExamples(feature)
	seen := map[string]bool{}
	covered := map[string]bool{}
	for i, m := range ms {
		switch {
		case m.Name == "":
			return fmt.Errorf("mutant %d has no name", i)
		case seen[m.Name]:
			return fmt.Errorf("mutant %q: the name is used twice", m.Name)
		}
		seen[m.Name] = true
		if rule, what, ok := strings.Cut(m.Name, "/"); !ok || rule != m.rule || what == "" {
			return fmt.Errorf("mutant %q: the name is not <rule>/<what> with its rule %q", m.Name, m.rule)
		}
		if n := strings.Count(ref, m.from); m.from == "" || n != 1 {
			return fmt.Errorf("mutant %q: the line it replaces occurs %d times in the reference, want once", m.Name, n)
		}
		if m.from == m.to {
			return fmt.Errorf("mutant %q: its replacement equals the line it replaces", m.Name)
		}
		if m.marker == "" || !strings.Contains(m.to, m.marker) {
			return fmt.Errorf("mutant %q: the marker %q is not in its replacement", m.Name, m.marker)
		}
		ex, ok := examples[m.rule]
		if !ok {
			return fmt.Errorf("mutant %q: its rule %q is not a rule of the feature", m.Name, m.rule)
		}
		if !ex[m.KilledBy] {
			return fmt.Errorf("mutant %q: killed by %q, which is not an example tag under %s", m.Name, m.KilledBy, m.rule)
		}
		covered[m.rule] = true
	}
	for _, r := range rules {
		if !covered[r] {
			return fmt.Errorf("rule %s has no mutant", r)
		}
	}
	return nil
}
