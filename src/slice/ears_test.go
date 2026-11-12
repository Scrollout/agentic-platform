// Tests for deriving a rule's EARS sentence (task T027). They fail until T032
// implements ears.go.
//
// API the tests call (package slice), the minimum data-model.md implies
// (a rule is structured, its sentence is derived, the slice shows it in
// `rule_text` by rule ID):
//
//	type Rule struct {
//		ID           string   `json:"id"`
//		Goal         string   `json:"goal"`
//		Pattern      string   `json:"pattern"` // ubiquitous, event, state, unwanted, option
//		Precondition string   `json:"precondition,omitempty"`
//		Trigger      string   `json:"trigger,omitempty"`
//		Feature      string   `json:"feature,omitempty"`
//		System       string   `json:"system"`
//		Response     string   `json:"response"`
//		Terms        []string `json:"terms"`
//	}
//	func EARS(r Rule) (string, error)
//	func RuleText(doc []byte) (map[string]string, error)
//
// EARS returns the one sentence for a rule, or an error naming the missing
// field or the unknown pattern. Rule has no text field: a sentence is never
// input. RuleText takes a slice document as JSON, reads only its "rules" (any
// "text" key inside a rule and any existing "rule_text" are ignored, never
// copied or merged) and returns the sentence of every rule by rule ID; the
// platform stores that as the slice's `rule_text`. Reason for a function over
// the document as well as over Rule: the guarantee "derived text is never taken
// from input" is about the document the engine stores, so it is tested there.
//
// Pattern templates are those of the EARS official guide (research.md S8,
// https://alistairmavin.com/ears/). A sentence starts with a capital letter and
// ends with a full stop; the system name is lower case after a comma. The
// response already holds "shall".
package slice

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func earsFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "ears", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func earsRule(t *testing.T, name string) Rule {
	t.Helper()
	var r Rule
	if err := json.Unmarshal(earsFixture(t, name), &r); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return r
}

func TestEARSPatterns(t *testing.T) {
	tests := []struct {
		fixture, template, want string
	}{
		{
			"ubiquitous.json",
			"Ubiquitous: The <system name> shall <system response>",
			"The notes service shall store each note.",
		},
		{
			"state.json",
			"State driven: While <precondition(s)>, the <system name> shall <system response>",
			"While a note is archived, the notes service shall refuse edits to it.",
		},
		{
			"event.json",
			"Event driven: When <trigger>, the <system name> shall <system response>",
			"When a user creates a note, the notes service shall return the new note with an identifier.",
		},
		{
			"option.json",
			"Optional feature: Where <feature is included>, the <system name> shall <system response>",
			"Where note search is included, the notes service shall list the notes that match a query.",
		},
		{
			"unwanted.json",
			"Unwanted behaviour: If <trigger>, then the <system name> shall <system response>",
			"If a note is larger than 64 KiB, then the notes service shall reject the note with a size error.",
		},
	}
	for _, tc := range tests {
		t.Run(strings.TrimSuffix(tc.fixture, ".json"), func(t *testing.T) {
			got, err := EARS(earsRule(t, tc.fixture))
			if err != nil {
				t.Fatalf("EARS: %v (template: %s)", err, tc.template)
			}
			if got != tc.want {
				t.Errorf("EARS = %q\nwant   %q\ntemplate: %s", got, tc.want, tc.template)
			}
		})
	}
}

// The guide gives no literal template for an unwanted rule with a precondition.
// It gives the generic syntax "While <optional pre-condition>, when <optional
// trigger>, the <system name> shall <system response>" and says complex unwanted
// requirements also include the If-Then keywords. So the order is: While
// <precondition>, if <trigger>, then the <system name> shall <system response>.
// The schema allows a precondition on an unwanted rule.
func TestEARSUnwantedWithPrecondition(t *testing.T) {
	got, err := EARS(earsRule(t, "unwanted-precondition.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := "While the user is signed in, if the note is larger than 64 KiB, then the notes service shall reject the note."
	if got != want {
		t.Errorf("EARS = %q, want %q", got, want)
	}
}

// Generic syntax: While <precondition>, when <trigger>, the <system name> shall
// <system response>. The guide capitalises "When" in its complex example, but
// the generic form uses lower case inside a sentence, so the test does too.
func TestEARSEventWithPrecondition(t *testing.T) {
	got, err := EARS(earsRule(t, "event-precondition.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := "While the user is signed in, when a user creates a note, the notes service shall return the new note."
	if got != want {
		t.Errorf("EARS = %q, want %q", got, want)
	}
}

func TestEARSRejectsIncompleteRules(t *testing.T) {
	tests := []struct{ fixture, mention string }{
		{"missing-precondition.json", "precondition"},
		{"unknown-pattern.json", "complex"},
	}
	for _, tc := range tests {
		t.Run(tc.fixture, func(t *testing.T) {
			got, err := EARS(earsRule(t, tc.fixture))
			if err == nil {
				t.Fatalf("EARS = %q, want an error naming %q", got, tc.mention)
			}
			if !strings.Contains(err.Error(), tc.mention) {
				t.Errorf("error %q does not name %q", err, tc.mention)
			}
			if got != "" {
				t.Errorf("EARS returned %q with an error", got)
			}
		})
	}
}

// A rule record that carries a text field gets the same sentence as one that
// does not: the field is not part of a rule and never reaches the output.
func TestEARSIgnoresInputText(t *testing.T) {
	with := earsRule(t, "rule-with-text.json")
	without := earsRule(t, "ubiquitous.json")
	a, err := EARS(with)
	if err != nil {
		t.Fatal(err)
	}
	b, err := EARS(without)
	if err != nil {
		t.Fatal(err)
	}
	if a != b || strings.Contains(a, "Anything the input claims") {
		t.Errorf("with text %q, without %q", a, b)
	}
}

func TestRuleTextIsDerivedNotTakenFromInput(t *testing.T) {
	got, err := RuleText(earsFixture(t, "slice-stale-rule-text.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"R-1": "The notes service shall store each note.",
		"R-2": "When a user creates a note, the notes service shall return the new note.",
	}
	if len(got) != len(want) {
		t.Errorf("RuleText = %v, want exactly %v", got, want)
	}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("RuleText[%s] = %q, want %q", id, got[id], w)
		}
	}
}

func TestRuleTextRejectsBadInput(t *testing.T) {
	if _, err := RuleText([]byte(`{"rules": [`)); err == nil {
		t.Error("malformed JSON: want an error")
	}
	if _, err := RuleText(earsFixture(t, "missing-precondition.json")); err == nil {
		t.Error("a document with no rules array: want an error")
	}
	doc := `{"rules":[` + string(earsFixture(t, "missing-precondition.json")) + `]}`
	_, err := RuleText([]byte(doc))
	if err == nil || !strings.Contains(err.Error(), "R-2") {
		t.Errorf("incomplete rule: error %v, want one naming R-2", err)
	}
}
