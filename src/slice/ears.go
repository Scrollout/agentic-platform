package slice

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Rule is a business rule in structured EARS, as slice-defs.schema.json `$defs/rule` gives it. It
// has no text field: its sentence is always derived by EARS, never taken from input.
type Rule struct {
	ID           string   `json:"id"`
	Goal         string   `json:"goal"`
	Pattern      string   `json:"pattern"`
	Precondition string   `json:"precondition,omitempty"`
	Trigger      string   `json:"trigger,omitempty"`
	Feature      string   `json:"feature,omitempty"`
	System       string   `json:"system"`
	Response     string   `json:"response"`
	Terms        []string `json:"terms"`
}

// earsTemplate is what one EARS pattern adds to the generic sentence "Where <feature>, while
// <precondition>, when <trigger>, the <system> shall <response>": the clause it cannot do without,
// and the keywords its trigger takes. The pattern names and which field each one requires are the
// schema's rule; this table holds only what a sentence needs, so a field the schema demands is
// asked for here only because the sentence cannot be written without it.
type earsTemplate struct {
	needs  string // "", "precondition", "trigger" or "feature"
	ifThen bool   // the trigger reads "if <trigger>, then" instead of "when <trigger>,"
}

var earsTemplates = map[string]earsTemplate{
	"ubiquitous": {},
	"state":      {needs: "precondition"},
	"event":      {needs: "trigger"},
	"unwanted":   {needs: "trigger", ifThen: true},
	"option":     {needs: "feature"},
}

// EARS returns the one EARS sentence of r, following the templates of the EARS guide
// (https://alistairmavin.com/ears/). Every clause r holds is written, in the order "Where
// <feature>, while <precondition>, when <trigger>, the <system> <response>." An unwanted rule's
// trigger reads "if <trigger>, then". The sentence starts with a capital letter and ends with a
// full stop; the system and response are written as given, so the response holds "shall".
//
// It fails, returning "", when r's pattern has no EARS template or when a field the sentence
// needs is empty; the error names the rule ID and the pattern or field. It does not check the
// rest of the schema (IDs, terms): parse the slice against its schema for that.
func EARS(r Rule) (string, error) {
	t, ok := earsTemplates[r.Pattern]
	if !ok {
		return "", fmt.Errorf("rule %s: no EARS template for pattern %q", r.ID, r.Pattern)
	}
	fields := map[string]string{
		"precondition": r.Precondition, "trigger": r.Trigger, "feature": r.Feature,
		"system": r.System, "response": r.Response,
	}
	for _, f := range []string{t.needs, "system", "response"} {
		if f != "" && strings.TrimSpace(fields[f]) == "" {
			return "", fmt.Errorf("rule %s: a %s rule needs %q", r.ID, r.Pattern, f)
		}
	}
	var clauses []string
	if r.Feature != "" {
		clauses = append(clauses, "where "+r.Feature+",")
	}
	if r.Precondition != "" {
		clauses = append(clauses, "while "+r.Precondition+",")
	}
	if r.Trigger != "" {
		if t.ifThen {
			clauses = append(clauses, "if "+r.Trigger+", then")
		} else {
			clauses = append(clauses, "when "+r.Trigger+",")
		}
	}
	clauses = append(clauses, r.System, strings.TrimSuffix(r.Response, "."))
	return capitalise(strings.Join(clauses, " ")) + ".", nil
}

func capitalise(s string) string {
	c, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(c)) + s[n:]
}

// RuleText returns the EARS sentence of every rule in a slice document (JSON), by rule ID: the
// slice's `rule_text`. It reads only the document's "rules"; an existing "rule_text" and any text
// inside a rule are ignored, so no sentence is ever taken from input.
//
// It fails on malformed JSON, on a document without a "rules" array, on two rules with one ID, and
// on a rule EARS cannot write a sentence for; the error names the rule. It does not otherwise
// check the document against its schema.
func RuleText(doc []byte) (map[string]string, error) {
	var d struct {
		Rules *[]json.RawMessage `json:"rules"`
	}
	if err := json.Unmarshal(doc, &d); err != nil {
		return nil, fmt.Errorf("slice document: %w", err)
	}
	if d.Rules == nil {
		return nil, errors.New(`slice document: no "rules" array`)
	}
	out := make(map[string]string, len(*d.Rules))
	for i, raw := range *d.Rules {
		var r Rule
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, fmt.Errorf("slice document: rules[%d]: %w", i, err)
		}
		if _, dup := out[r.ID]; dup {
			return nil, fmt.Errorf("slice document: rule %s appears twice", r.ID)
		}
		s, err := EARS(r)
		if err != nil {
			return nil, err
		}
		out[r.ID] = s
	}
	return out, nil
}
