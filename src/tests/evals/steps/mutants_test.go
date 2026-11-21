package steps

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The mutant table (research R1, README "Mutants"): its rows, their order, where each plants
// its bug, and the checks CheckMutants makes, each proved with a planted violation.

var wantMutants = []struct{ name, rule, killedBy string }{
	{"R-1/newest-first", "R-1", "E-1"},
	{"R-1/drops-first", "R-1", "E-1"},
	{"R-2/from-last", "R-2", "E-2"},
	{"R-2/zero-based", "R-2", "E-2"},
	{"R-2/always-one", "R-2", "E-3"},
	{"R-2/status-200", "R-2", "E-2"},
	{"R-3/first-byte", "R-3", "E-4"},
	{"R-3/accepts-empty", "R-3", "E-4"},
}

func graderMutantTexts(t *testing.T) (ref, feature string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "ref", "impl", "internal", "app", "app.go"))
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.ReadFile(filepath.Join("testdata", "slice", "slice.feature"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b), string(f)
}

func graderMutantsCopy() []Mutant { return append([]Mutant(nil), Mutants()...) }

func TestGraderMutantsTable(t *testing.T) {
	ms := Mutants()
	if len(ms) != len(wantMutants) {
		t.Fatalf("got %d mutants, want %d", len(ms), len(wantMutants))
	}
	seen := map[string]bool{}
	for i, w := range wantMutants {
		m := ms[i]
		if m.Name != w.name {
			t.Errorf("row %d: name %q, want %q", i, m.Name, w.name)
		}
		if seen[m.Name] {
			t.Errorf("duplicate name %q", m.Name)
		}
		seen[m.Name] = true
		if m.rule != w.rule || !strings.HasPrefix(m.Name, w.rule+"/") || len(m.Name) == len(w.rule)+1 {
			t.Errorf("%s: rule %q, want %q as the name's prefix", m.Name, m.rule, w.rule)
		}
		if m.KilledBy != w.killedBy {
			t.Errorf("%s: killed by %q, want %q", m.Name, m.KilledBy, w.killedBy)
		}
		if m.what == "" || m.marker == "" {
			t.Errorf("%s: empty what or marker", m.Name)
		}
	}
}

func TestGraderMutantsLocateOneLine(t *testing.T) {
	ref, _ := graderMutantTexts(t)
	refLines := strings.Split(ref, "\n")
	for _, m := range Mutants() {
		if m.from == m.to {
			t.Errorf("%s: from equals to", m.Name)
		}
		if n := strings.Count(ref, m.from); n != 1 {
			t.Errorf("%s: from occurs %d times in the reference, want 1", m.Name, n)
			continue
		}
		if !strings.Contains(m.to, m.marker) {
			t.Errorf("%s: marker %q is not in the to line", m.Name, m.marker)
		}
		mut := strings.Replace(ref, m.from, m.to, 1)
		if mut == ref {
			t.Errorf("%s: applying it leaves the reference unchanged", m.Name)
		}
		mutLines := strings.Split(mut, "\n")
		// Trim the common prefix and suffix: exactly one line must be replaced by one line.
		p := 0
		for p < len(refLines) && p < len(mutLines) && refLines[p] == mutLines[p] {
			p++
		}
		s := 0
		for s < len(refLines)-p && s < len(mutLines)-p &&
			refLines[len(refLines)-1-s] == mutLines[len(mutLines)-1-s] {
			s++
		}
		if a, b := len(refLines)-p-s, len(mutLines)-p-s; a != 1 || b != 1 {
			t.Errorf("%s: changes %d reference line(s) into %d, want 1 into 1", m.Name, a, b)
		}
	}
}

var graderRuleRe = regexp.MustCompile(`(?m)^\s*Rule:\s*(R-\d+)`)

func TestGraderMutantsCoverFeature(t *testing.T) {
	_, feature := graderMutantTexts(t)
	rules := graderRuleRe.FindAllStringSubmatch(feature, -1)
	if len(rules) != 3 {
		t.Fatalf("the feature has %d rules, want 3", len(rules))
	}
	for _, r := range rules {
		n := 0
		for _, m := range Mutants() {
			if m.rule == r[1] {
				n++
			}
		}
		if n == 0 {
			t.Errorf("rule %s has no mutant", r[1])
		}
	}
	// Every KilledBy is an example tag of a scenario under the mutant's rule.
	for _, m := range Mutants() {
		if !graderExampleUnderRule(feature, m.rule, m.KilledBy) {
			t.Errorf("%s: %s is not an example tag under %s", m.Name, m.KilledBy, m.rule)
		}
	}
}

// graderExampleUnderRule reports whether a scenario under "Rule: <rule>" carries the tag @<tag>.
func graderExampleUnderRule(feature, rule, tag string) bool {
	cur := ""
	for _, l := range strings.Split(feature, "\n") {
		l = strings.TrimSpace(l)
		if m := graderRuleRe.FindStringSubmatch(l); m != nil {
			cur = m[1]
			continue
		}
		if cur == rule && strings.HasPrefix(l, "@") {
			for _, f := range strings.Fields(l) {
				if f == "@"+tag {
					return true
				}
			}
		}
	}
	return false
}

func graderRow(t *testing.T, name string) Mutant {
	t.Helper()
	for _, m := range Mutants() {
		if m.Name == name {
			return m
		}
	}
	t.Fatalf("no mutant %s", name)
	return Mutant{}
}

func TestGraderMutantsOwnThePlants(t *testing.T) {
	if got, want := plantR2, graderRow(t, "R-2/from-last").plantSpec; got != want {
		t.Errorf("plantR2 = %+v, want the row R-2/from-last %+v", got, want)
	}
	if got, want := plantR3, graderRow(t, "R-3/first-byte").plantSpec; got != want {
		t.Errorf("plantR3 = %+v, want the row R-3/first-byte %+v", got, want)
	}
}

func TestGraderMutantsReviewCasesPlantTheRows(t *testing.T) {
	ref, _ := graderMutantTexts(t)
	byName := map[string]Case{}
	for _, c := range Cases() {
		byName[c.Name] = c
	}
	for caseName, row := range map[string]string{
		"review/planted-r2": "R-2/from-last",
		"review/planted-r3": "R-3/first-byte",
	} {
		c, ok := byName[caseName]
		if !ok {
			t.Errorf("no case %s", caseName)
			continue
		}
		f, err := c.Before()
		if err != nil {
			t.Fatalf("%s: %v", caseName, err)
		}
		m := graderRow(t, row)
		if want := strings.Replace(ref, m.from, m.to, 1); f[implFile] != want {
			t.Errorf("%s: %s is not the reference with only %s planted", caseName, implFile, row)
		}
		if f[implFile] == ref {
			t.Errorf("%s: nothing is planted", caseName)
		}
	}
}

func TestGraderMutantsCheckAcceptsTheTable(t *testing.T) {
	ref, feature := graderMutantTexts(t)
	if err := CheckMutants(Mutants(), ref, feature); err != nil {
		t.Fatalf("CheckMutants on the real table: %v", err)
	}
}

func TestGraderMutantsCheckRejectsPlantedViolations(t *testing.T) {
	if len(Mutants()) != len(wantMutants) {
		t.Fatalf("the table has %d rows, want %d; the planted violations index them", len(Mutants()), len(wantMutants))
	}
	ref, feature := graderMutantTexts(t)
	// Rows may share a from line; the error names the first row in table order that has it.
	firstSameFrom := ""
	for _, m := range Mutants() {
		if m.from == Mutants()[4].from {
			firstSameFrom = m.Name
			break
		}
	}
	cases := []struct {
		name string
		edit func(ms []Mutant) ([]Mutant, string) // returns the table and the ref
		want string                               // text the error should hold
	}{
		{"duplicate name", func(ms []Mutant) ([]Mutant, string) { ms[1].Name = ms[0].Name; ms[1].rule = ms[0].rule; return ms, ref }, "R-1/newest-first"},
		{"empty name", func(ms []Mutant) ([]Mutant, string) { ms[2].Name = ""; return ms, ref }, ""},
		{"from absent from the reference", func(ms []Mutant) ([]Mutant, string) { ms[3].from = "no such line in the code\n"; return ms, ref }, "R-2/zero-based"},
		{"from twice in the reference", func(ms []Mutant) ([]Mutant, string) { return ms, ref + "\n" + ms[4].from }, firstSameFrom},
		{"from equals to", func(ms []Mutant) ([]Mutant, string) { ms[5].to = ms[5].from; return ms, ref }, "R-2/status-200"},
		{"rule with no mutant", func(ms []Mutant) ([]Mutant, string) {
			var out []Mutant
			for _, m := range ms {
				if m.rule != "R-3" {
					out = append(out, m)
				}
			}
			return out, ref
		}, "R-3"},
		{"mutant of a rule the feature lacks", func(ms []Mutant) ([]Mutant, string) {
			ms[0].rule = "R-9"
			ms[0].Name = "R-9/x"
			return ms, ref
		}, "R-9"},
		{"killed by an example of another rule", func(ms []Mutant) ([]Mutant, string) { ms[0].KilledBy = "E-4"; return ms, ref }, "R-1/newest-first"},
		{"killed by a tag that does not exist", func(ms []Mutant) ([]Mutant, string) { ms[6].KilledBy = "E-9"; return ms, ref }, "R-3/first-byte"},
		{"killed by empty", func(ms []Mutant) ([]Mutant, string) { ms[7].KilledBy = ""; return ms, ref }, "R-3/accepts-empty"},
		{"killed by a rule tag, not an example", func(ms []Mutant) ([]Mutant, string) { ms[7].KilledBy = "R-3"; return ms, ref }, "R-3/accepts-empty"},
		{"name prefix is not its rule", func(ms []Mutant) ([]Mutant, string) { ms[4].Name = "R-1/always-one"; return ms, ref }, "R-1/always-one"},
		{"name without a slash", func(ms []Mutant) ([]Mutant, string) { ms[4].Name = "always-one"; return ms, ref }, "always-one"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ms, r := c.edit(graderMutantsCopy())
			err := CheckMutants(ms, r, feature)
			if err == nil {
				t.Fatal("CheckMutants accepted the planted violation")
			}
			if c.want != "" && !strings.Contains(err.Error(), c.want) {
				t.Errorf("the error %q does not name %q", err, c.want)
			}
		})
	}
	// The copies never touched the real table.
	if err := CheckMutants(Mutants(), ref, feature); err != nil {
		t.Errorf("the real table changed: %v", err)
	}
}
