// Tests for T013a: tighten T013's assertions. Each planted fixture must break
// its one rule only, so the validation error holds exactly one cause (one leaf
// of the error tree), and that cause must name the right place and rule.
// Written from the contract and the task, not from schema.go.
//
// The tests read the structure of the error (*jsonschema.ValidationError, found
// with errors.As), not only its text: a document with a second defect, or an
// error that blames the wrong part, fails here though the T013 substring checks
// would still pass.
package slice

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
)

// leaf is one innermost cause of a validation error.
type leaf struct {
	loc  string // JSON Pointer of the failing value; "" is the document itself
	kind jsonschema.ErrorKind
}

func leavesOf(t *testing.T, err error) []leaf {
	t.Helper()
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("error %T is not (or does not wrap) a *jsonschema.ValidationError: %v", err, err)
	}
	var out []leaf
	var walk func(e *jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) == 0 {
			loc := ""
			if len(e.InstanceLocation) > 0 {
				loc = "/" + strings.Join(e.InstanceLocation, "/")
			}
			out = append(out, leaf{loc, e.ErrorKind})
			return
		}
		for _, c := range e.Causes {
			walk(c)
		}
	}
	walk(ve)
	return out
}

func checkFixture(t *testing.T, s *Schemas, name string, proposal bool) error {
	t.Helper()
	doc := fixture(t, name)
	if proposal {
		return s.CheckProposal(doc)
	}
	return s.CheckSlice(doc)
}

func TestEachPlantedFixtureFailsForItsOneRuleOnly(t *testing.T) {
	missing := func(prop string) func(*testing.T, leaf) {
		return func(t *testing.T, l leaf) {
			k, ok := l.kind.(*kind.Required)
			if !ok || len(k.Missing) != 1 || k.Missing[0] != prop {
				t.Errorf("cause %T %+v, want exactly the missing property %q", l.kind, l.kind, prop)
			}
		}
	}
	cases := []struct {
		fixture  string
		proposal bool
		loc      string
		what     func(*testing.T, leaf)
	}{
		{"slice-state-no-precondition", false, "/rules/0", missing("precondition")},
		{"slice-event-no-trigger", false, "/rules/0", missing("trigger")},
		{"slice-unwanted-no-trigger", false, "/rules/0", missing("trigger")},
		{"slice-option-no-feature", false, "/rules/0", missing("feature")},
		{"proposal-state-no-precondition", true, "/rules/0", missing("precondition")},
		{"slice-bad-goal-id", false, "/goals/0/id", func(t *testing.T, l leaf) {
			k, ok := l.kind.(*kind.Pattern)
			if want := goalIDPattern(t); !ok || k.Got != "G-one" || k.Want != want {
				t.Errorf("cause %T %+v, want the contract's goal_id pattern %s failing for \"G-one\"", l.kind, l.kind, want)
			}
		}},
		{"slice-too-many-questions", false, "/questions", func(t *testing.T, l leaf) {
			k, ok := l.kind.(*kind.MaxItems)
			if !ok || k.Want != 10 || k.Got != 11 {
				t.Errorf("cause %T %+v, want maxItems 10 exceeded by 11 questions", l.kind, l.kind)
			}
		}},
		{"proposal-with-approvals", true, "", func(t *testing.T, l leaf) {
			k, ok := l.kind.(*kind.AdditionalProperties)
			if !ok || len(k.Properties) != 1 || k.Properties[0] != "approvals" {
				t.Errorf("cause %T %+v, want the one unexpected property \"approvals\"", l.kind, l.kind)
			}
		}},
	}
	s := loadSchemas(t)
	for _, c := range cases {
		t.Run(c.fixture, func(t *testing.T) {
			err := checkFixture(t, s, c.fixture, c.proposal)
			if err == nil {
				t.Fatal("document accepted, want rejection")
			}
			ls := leavesOf(t, err)
			if len(ls) != 1 {
				t.Fatalf("%d causes, want exactly 1 (the fixture must break one rule only): %v", len(ls), err)
			}
			if ls[0].loc != c.loc {
				t.Errorf("cause at %q, want %q: %v", ls[0].loc, c.loc, err)
			}
			c.what(t, ls[0])
		})
	}
}

// slice-long-answer fails a oneOf (an answer is an option or a free text). The
// other branch fails too, as every oneOf branch that does not match does; the
// one cause is the text branch's maxLength.
func TestLongAnswerFailsForItsLengthOnly(t *testing.T) {
	s := loadSchemas(t)
	err := checkFixture(t, s, "slice-long-answer", false)
	if err == nil {
		t.Fatal("document accepted, want rejection")
	}
	var tooLong []leaf
	for _, l := range leavesOf(t, err) {
		switch k := l.kind.(type) {
		case *kind.MaxLength:
			tooLong = append(tooLong, l)
			if l.loc != "/questions/0/answer/text" || k.Want != 200 || k.Got != 201 {
				t.Errorf("maxLength cause at %q %+v, want /questions/0/answer/text, 201 over 200", l.loc, k)
			}
		case *kind.Required, *kind.AdditionalProperties:
			// The option branch of the oneOf, which a free-text answer does not match.
			if l.loc != "/questions/0/answer" {
				t.Errorf("unexpected cause %T at %q: %v", l.kind, l.loc, err)
			}
		default:
			t.Errorf("unexpected cause %T at %q: %v", l.kind, l.loc, err)
		}
	}
	if len(tooLong) != 1 {
		t.Fatalf("%d maxLength causes, want 1: %v", len(tooLong), err)
	}
}

// The approvals fixture must say which property is wrong, in the text too: a
// reader of the message alone learns "approvals" is not allowed.
func TestProposalWithApprovalsNamesTheProperty(t *testing.T) {
	err := checkFixture(t, loadSchemas(t), "proposal-with-approvals", true)
	if err == nil {
		t.Fatal("document accepted, want rejection")
	}
	if !strings.Contains(err.Error(), "'approvals'") && !strings.Contains(err.Error(), "\"approvals\"") {
		t.Errorf("error does not name the approvals property: %v", err)
	}
	// The same document without approvals is a valid proposal.
	if err := loadSchemas(t).CheckProposal(fixture(t, "proposal-valid")); err != nil {
		t.Fatalf("proposal-valid rejected: %v", err)
	}
}

// The maxItems fixture must name the limit in the text as well as the path.
func TestTooManyQuestionsNamesMaxItems(t *testing.T) {
	err := checkFixture(t, loadSchemas(t), "slice-too-many-questions", false)
	if err == nil {
		t.Fatal("document accepted, want rejection")
	}
	if !strings.Contains(err.Error(), "maxItems") || !strings.Contains(err.Error(), "/questions") {
		t.Errorf("error does not name maxItems at /questions: %v", err)
	}
}

// A document with two defects has two causes, so the count above really counts
// rules: this guards the helper against always seeing one cause.
func TestCauseCountSeesASecondDefect(t *testing.T) {
	s := loadSchemas(t)
	doc := strings.Replace(string(fixture(t, "slice-bad-goal-id")), `"id": "S-1"`, `"id": "bad"`, 1)
	if doc == string(fixture(t, "slice-bad-goal-id")) {
		t.Fatal("test edit did not apply: fixture has no \"id\": \"S-1\"")
	}
	err := s.CheckSlice([]byte(doc))
	if err == nil {
		t.Fatal("accepted")
	}
	if n := len(leavesOf(t, err)); n < 2 {
		t.Fatalf("%d causes for a document with two defects, want at least 2: %v", n, err)
	}
}

// goalIDPattern is $defs.goal_id.pattern of slice-defs.schema.json, the rule's one owner.
func goalIDPattern(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(schemaDir, "slice-defs.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Defs map[string]struct{ Pattern string } `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Defs["goal_id"].Pattern == "" {
		t.Fatal("slice-defs.schema.json has no $defs.goal_id.pattern")
	}
	return doc.Defs["goal_id"].Pattern
}
