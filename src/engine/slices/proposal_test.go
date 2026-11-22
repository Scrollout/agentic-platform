// Tests for ParseProposal and AnswerFault (feature 007, task T003). They fail to compile until
// package slices adds both.
package slices_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/agentic-platform/engine/slices"
)

// wantReason is the reason the contract gives: key and pointer cut to 64 runes, then quoted.
func wantReason(key, pointer string) string {
	cut := func(s string) string {
		r := []rune(s)
		if len(r) > 64 {
			r = r[:64]
		}
		return fmt.Sprintf("%q", string(r))
	}
	return "the answer holds the key " + cut(key) + " twice at " + cut(pointer)
}

func parseFault(t *testing.T, answer string) *slices.AnswerFault {
	t.Helper()
	_, err := slices.ParseProposal([]byte(answer))
	if err == nil {
		t.Fatalf("ParseProposal(%.120s): want an AnswerFault, got no error", answer)
	}
	var f *slices.AnswerFault
	if !errors.As(err, &f) {
		t.Fatalf("ParseProposal(%.120s): want an *AnswerFault, got %T: %v", answer, err, err)
	}
	if f.Error() != f.Reason {
		t.Fatalf("Error() = %q, want the reason %q", f.Error(), f.Reason)
	}
	return f
}

func TestParseProposalRepeatedKeyIsAnAnswerFault(t *testing.T) {
	cases := []struct {
		name, answer, key, pointer string
	}{
		{"top level", `{"goals":[],"examples":"a","terms":[],"examples":"b"}`, "examples", "/"},
		{"top level, same value twice", `{"examples":"a","examples":"a"}`, "examples", "/"},
		{"in a goal", `{"goals":[{"id":"G-1","text":"a","text":"b"}]}`, "text", "/goals/0"},
		{"in the second goal", `{"goals":[{"id":"G-1","text":"a"},{"id":"G-2","id":"G-2","text":"b"}]}`, "id", "/goals/1"},
		{"in a rule", `{"rules":[{"id":"R-1","goal":"G-1"},{"id":"R-2","goal":"G-1","goal":"G-2"}]}`, "goal", "/rules/1"},
		{"in a question", `{"questions":[{"about":"R-1","text":"a","text":"a"}]}`, "text", "/questions/0"},
		{"in a nested object of a question", `{"questions":[{"about":"R-1","text":"a","extra":{"k":1,"k":2}}]}`, "k", "/questions/0/extra"},
		{"in an object inside an array of a question", `{"questions":[{"extra":[1,{"k":1,"k":2}]}]}`, "k", "/questions/0/extra/1"},
		{"in a member name needing pointer escaping", `{"a/b":{"k":1,"k":2},"c~d":{}}`, "k", "/a~1b"},
		{"in a member name with a tilde", `{"c~d":{"k":1,"k":2}}`, "k", "/c~0d"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := parseFault(t, c.answer)
			if want := wantReason(c.key, c.pointer); f.Reason != want {
				t.Fatalf("reason = %q, want %q", f.Reason, want)
			}
		})
	}
}

func TestParseProposalExamplesTopLevelReason(t *testing.T) {
	f := parseFault(t, `{"examples":"x","examples":"y"}`)
	if want := `the answer holds the key "examples" twice at "/"`; f.Reason != want {
		t.Fatalf("reason = %q, want %q", f.Reason, want)
	}
	f = parseFault(t, `{"rules":[{"id":"R-1"},{"id":"R-2","id":"R-2"}]}`)
	if want := `the answer holds the key "id" twice at "/rules/1"`; f.Reason != want {
		t.Fatalf("reason = %q, want %q", f.Reason, want)
	}
}

func TestParseProposalKeyWrittenWithEscapeIsTheSameKey(t *testing.T) {
	for _, answer := range []string{
		`{"ab":1,"ab":2}`,
		`{"ab":1,"ab":2}`,
		`{"ab":1,"ab":2}`,
		`{"ab":1,"aB":2}`, // "ab" and "aB": one key without regard to case
	} {
		f := parseFault(t, answer)
		if !strings.HasPrefix(f.Reason, "the answer holds the key \"a") || !strings.HasSuffix(f.Reason, `" twice at "/"`) {
			t.Fatalf("%s: reason = %q", answer, f.Reason)
		}
	}
	f := parseFault(t, `{"ab":1,"ab":2}`)
	if want := wantReason("ab", "/"); f.Reason != want {
		t.Fatalf("reason = %q, want %q", f.Reason, want)
	}
}

func TestParseProposalKeysDifferingOnlyInLetterCaseAreOneKey(t *testing.T) {
	for _, answer := range []string{
		`{"examples":"a","Examples":"b"}`,
		`{"Examples":"a","examples":"b"}`,
		`{"EXAMPLES":"a","examples":"b"}`,
	} {
		f := parseFault(t, answer)
		if !strings.HasPrefix(strings.ToLower(f.Reason), `the answer holds the key "examples" twice at "/"`) {
			t.Fatalf("%s: reason = %q", answer, f.Reason)
		}
	}
	f := parseFault(t, `{"goals":[{"Text":"a","text":"b"}]}`)
	if !strings.HasSuffix(f.Reason, `twice at "/goals/0"`) {
		t.Fatalf("reason = %q", f.Reason)
	}
}

func TestParseProposalExamplesTextInsideTheGherkinStringIsNotARepeatedKey(t *testing.T) {
	answer := `{"examples":"Feature: A\n  Rule: R-1\n    examples: one\n    examples: two\n","contract":"examples: 1\nexamples: 2\n","terms":[{"id":"T-1","name":"\"examples\":1,\"examples\":2"}]}`
	got, err := slices.ParseProposal([]byte(answer))
	if err != nil {
		t.Fatalf("ParseProposal: %v", err)
	}
	var want slices.Proposal
	if err := json.Unmarshal([]byte(answer), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestParseProposalSameKeyInDifferentObjectsIsNotARepeat(t *testing.T) {
	answer := `{"goals":[{"id":"G-1","text":"a"},{"id":"G-2","text":"b"}],` +
		`"rules":[{"id":"R-1","goal":"G-1"},{"id":"R-2","goal":"G-2"}],` +
		`"questions":[{"about":"R-1","text":"a","extra":{"text":"x"}},{"about":"R-2","text":"b"}],` +
		`"examples":"e"}`
	got, err := slices.ParseProposal([]byte(answer))
	if err != nil {
		t.Fatalf("ParseProposal: %v", err)
	}
	var want slices.Proposal
	if err := json.Unmarshal([]byte(answer), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestParseProposalNotOneJSONObjectIsAPlainError(t *testing.T) {
	for name, answer := range map[string]string{
		"empty":                      ``,
		"blank":                      "  \n",
		"not JSON":                   `this is not json`,
		"truncated":                  `{"goals":[`,
		"an array":                   `[{"examples":"a"}]`,
		"an empty array":             `[]`,
		"a string":                   `"examples"`,
		"a number":                   `42`,
		"one object then another":    `{"examples":"a"}{"examples":"b"}`,
		"one object then a space":    `{"examples":"a"} {"examples":"b"}`,
		"one object then text":       `{"examples":"a"} trailing`,
		"one object then an array":   `{"examples":"a"}[]`,
		"a repeated key behind junk": `{"examples":"a","examples":"b"} junk`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := slices.ParseProposal([]byte(answer))
			if err == nil {
				t.Fatal("want an error, got none")
			}
			var f *slices.AnswerFault
			if errors.As(err, &f) {
				t.Fatalf("want a plain error, got an AnswerFault: %v", f)
			}
		})
	}
}

func TestParseProposalGoodAnswerEqualsJSONUnmarshal(t *testing.T) {
	data, err := os.ReadFile(filepath.Clean("testdata/proposal-notes.json"))
	if err != nil {
		t.Fatal(err)
	}
	var want slices.Proposal
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	got, err := slices.ParseProposal(data)
	if err != nil {
		t.Fatalf("ParseProposal: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if len(got.Goals) != 2 || got.Goals[1].Text != "List\tnotes" || got.Model != "claude-haiku-test" {
		t.Fatalf("the fixture was not read as expected: %+v", got)
	}
}

func TestParseProposalQuotesAndCutsKeyAndPointer(t *testing.T) {
	long := strings.Repeat("ké", 50) // 100 runes, multi-byte
	cases := []struct {
		name, answer, key, pointer string
	}{
		{"key longer than 64 runes", `{"` + long + `":1,"` + long + `":2}`, long, "/"},
		{"key of exactly 64 runes", `{"` + strings.Repeat("x", 64) + `":1,"` + strings.Repeat("x", 64) + `":2}`, strings.Repeat("x", 64), "/"},
		{"key of 65 runes", `{"` + strings.Repeat("x", 65) + `":1,"` + strings.Repeat("x", 65) + `":2}`, strings.Repeat("x", 65), "/"},
		{"key with a line break", `{"a\nb":1,"a\nb":2}`, "a\nb", "/"},
		{"key with a carriage return and a quote", `{"a\r\"b":1,"a\r\"b":2}`, "a\r\"b", "/"},
		{"pointer longer than 64 runes", `{"` + long + `":{"k":1,"k":2}}`, "k", "/" + long},
		{"pointer with a line break", `{"a\nb":{"k":1,"k":2}}`, "k", "/a\nb"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := parseFault(t, c.answer)
			if strings.ContainsAny(f.Reason, "\r\n") {
				t.Fatalf("reason holds a raw line break: %q", f.Reason)
			}
			if want := wantReason(c.key, c.pointer); f.Reason != want {
				t.Fatalf("reason = %q, want %q", f.Reason, want)
			}
		})
	}
	// The cut is on the text, before quoting: a long key shows at most 64 of its runes.
	f := parseFault(t, `{"`+long+`":1,"`+long+`":2}`)
	if strings.Contains(f.Reason, long) {
		t.Fatalf("reason holds the whole long key: %q", f.Reason)
	}
	if !strings.Contains(f.Reason, strings.Repeat("ké", 32)) || strings.Contains(f.Reason, strings.Repeat("ké", 33)) {
		t.Fatalf("reason does not hold exactly the first 64 runes of the key: %q", f.Reason)
	}
}
