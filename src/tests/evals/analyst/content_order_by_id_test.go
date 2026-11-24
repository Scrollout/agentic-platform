package analyst

// Tests of the creation-order check of ExampleContent when the order scenario names the listed
// notes by id instead of by their quoted texts (T036). The fixture is the proposal of live eval
// trial `example` 1; it differs from a passing proposal only in scenario E-2's last Then step.

import (
	"encoding/json"
	"strings"
	"testing"
)

const orderMiss = "no scenario creates two notes and expects them listed in creation order"

const byIDThen = "And the response is a list of the notes with id 1 and id 2, in that order"

// withThen returns the fixture with scenario E-2's by-id Then step replaced by repl.
func withThen(t *testing.T, repl string) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(contentFixture(t, "bad-order-by-id"), &m); err != nil {
		t.Fatal(err)
	}
	ex, _ := m["examples"].(string)
	if strings.Count(ex, byIDThen) != 1 {
		t.Fatalf("fixture lacks the by-id Then step exactly once")
	}
	m["examples"] = strings.Replace(ex, byIDThen, repl, 1)
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestContentOrderRejectsNotesNamedByID(t *testing.T) {
	r := newGrader(t).Content(contentFixture(t, "bad-order-by-id"))
	if r.Passed {
		t.Fatal("ids listed in order without the created texts pass the content grader")
	}
	if r.Reason != orderMiss {
		t.Errorf("fails for %q, want only the creation-order miss", r.Reason)
	}
}

func TestContentOrderAcceptsTextsInThenOfByIDProposal(t *testing.T) {
	r := newGrader(t).Content(withThen(t, `And the response lists "buy milk" then "walk dog"`))
	if !r.Passed {
		t.Fatalf("the same proposal with the texts in creation order in Then fails: %s", r.Reason)
	}
}

func TestContentOrderRejectsVariantsOfByIDProposal(t *testing.T) {
	g := newGrader(t)
	cases := map[string]string{
		"one text in Then":      `And the response lists "buy milk" first`,
		"texts reversed":        `And the response lists "walk dog" then "buy milk"`,
		"same text twice":       `And the response lists "buy milk" then "buy milk"`,
		"ids and one text only": `And the response lists the notes with id 1 and id 2, "buy milk" first`,
	}
	for name, repl := range cases {
		r := g.Content(withThen(t, repl))
		if r.Passed {
			t.Errorf("%s passes the content grader", name)
		} else if r.Reason != orderMiss {
			t.Errorf("%s fails for %q, want only the creation-order miss", name, r.Reason)
		}
	}
}
