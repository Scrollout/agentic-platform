// T032: more tests for RefusalDeclared and the way OnlyHttpOperations guards what the request
// conditions cannot see (feature 010-strict-requests, contracts/strict-requests.md "Fit
// conditions", catalogue README "What the conditions keep out"). The cases:
//   - annotations (readOnly, default, ...) on the 400's message are allowed;
//   - a 400 whose only media type is "application/json; charset=utf-8" is not the application/json
//     body the condition names;
//   - the 400 object schema is followed through any number of local $refs, and a $ref loop ends;
//   - a YAML merge key or a path-item $ref can hide an operation's request body, so
//     OnlyHttpOperations refuses it while the two request conditions have nothing to judge.
//
// It adds to fit_requests_test.go and reuses its helpers.
package checks

import (
	"testing"
	"time"
)

// fitAll runs Fit with the real pattern (which lists every condition) on the contract text and
// returns the results by condition name. It fails when Fit does not end within 20 seconds.
func fitAll(t *testing.T, contract string) map[string]CheckResult {
	t.Helper()
	pattern, slice := repoPattern(t), requestsSlice(t, contract)
	done := make(chan []CheckResult, 1)
	go func() { done <- Fit(pattern, slice) }()
	select {
	case rs := <-done:
		m := map[string]CheckResult{}
		for _, r := range rs {
			if _, dup := m[r.Check]; !dup {
				m[r.Check] = r
			}
		}
		if len(m) != len(conditions) {
			t.Fatalf("want %d conditions judged, got %d: %+v", len(conditions), len(m), rs)
		}
		return m
	case <-time.After(20 * time.Second):
		t.Fatalf("Fit did not end within 20s on:\n%s", contract)
	}
	return nil
}

const newNoteSchema = "  NewNote:\n    type: object\n    required: [text]\n    properties:\n      text: {type: string}\n"

func TestRefusalDeclaredAllowsAnnotationsOnMessage(t *testing.T) {
	msg := func(kw string) string {
		return "type: object\nrequired: [message]\nproperties:\n  message: {type: string, " + kw + "}\n"
	}
	cases := map[string]string{
		"readOnly":           withErrorSchema(msg("readOnly: true")),
		"default":            withErrorSchema(msg("default: oops")),
		"writeOnly":          withErrorSchema(msg("writeOnly: false")),
		"examples":           withErrorSchema(msg("examples: [a, b]")),
		"$comment":           withErrorSchema(msg("$comment: c")),
		"readOnly + default": withErrorSchema(msg("readOnly: true, default: oops")),
		"readOnly on a message behind a $ref": withComponents("schemas:\n" + newNoteSchema +
			"  Error:\n    type: object\n    required: [message]\n    properties:\n      message: {$ref: \"#/components/schemas/Msg\"}\n" +
			"  Msg:\n    type: string\n    readOnly: true\n    default: oops\n"),
		"default on the error behind a $ref": withComponents("schemas:\n" + newNoteSchema +
			"  Error:\n    $ref: \"#/components/schemas/Inner\"\n" +
			"  Inner:\n    type: object\n    default: {message: oops}\n    readOnly: true\n    required: [message]\n    properties:\n      message: {type: string, default: oops}\n"),
	}
	for name, contract := range cases {
		t.Run(name, func(t *testing.T) {
			r, b := fitRequests(t, contract)
			wantPass(t, r)
			wantPass(t, b)
		})
	}
}

func TestRefusalDeclaredNeedsPlainApplicationJson(t *testing.T) {
	const schema = "        schema:\n          $ref: \"#/components/schemas/Error\"\n"
	for name, media := range map[string]string{
		"charset after a semicolon and space": "application/json; charset=utf-8",
		"charset after a semicolon":           "application/json;charset=utf-8",
		"upper-case type":                     "Application/JSON",
		"problem+json":                        "application/problem+json",
	} {
		t.Run(name, func(t *testing.T) {
			contract := notesContract{responses: "\"201\": {description: ok}\n\"400\":\n  description: bad\n  content:\n    \"" + media + "\":\n" + schema}.String()
			r, b := fitRequests(t, contract)
			wantFail(t, r, postLoc+noMessageReason)
			wantPass(t, b)
		})
	}
	t.Run("a parameterised type beside plain application/json passes", func(t *testing.T) {
		contract := notesContract{responses: "\"201\": {description: ok}\n\"400\":\n  description: bad\n  content:\n    \"application/json; charset=utf-8\": {schema: {type: string}}\n    application/json:\n" + schema}.String()
		r, _ := fitRequests(t, contract)
		wantPass(t, r)
	})
}

func TestRefusalDeclaredFollowsAChainOfRefs(t *testing.T) {
	chain := func(end string) string {
		return withComponents("schemas:\n" + newNoteSchema +
			"  Error:\n    $ref: \"#/components/schemas/A\"\n" +
			"  A:\n    $ref: \"#/components/schemas/B\"\n" +
			"  B:\n    $ref: \"#/components/schemas/C\"\n" +
			"  C:\n" + indent(end, 4))
	}
	good := "type: object\nrequired: [message]\nproperties:\n  message: {type: string}\n"
	t.Run("three refs ending in the right object", func(t *testing.T) {
		r, _ := fitRequests(t, chain(good))
		wantPass(t, r)
	})
	t.Run("message through a chain of refs", func(t *testing.T) {
		c := withComponents("schemas:\n" + newNoteSchema +
			"  Error:\n    type: object\n    required: [message]\n    properties:\n      message: {$ref: \"#/components/schemas/M1\"}\n" +
			"  M1:\n    $ref: \"#/components/schemas/M2\"\n  M2:\n    $ref: \"#/components/schemas/M3\"\n  M3:\n    type: string\n")
		r, _ := fitRequests(t, c)
		wantPass(t, r)
	})
	wrong := map[string]string{
		"required message and code at the end":  "type: object\nrequired: [message, code]\nproperties:\n  message: {type: string}\n  code: {type: string}\n",
		"type string at the end":                "type: string\n",
		"message is an integer at the end":      "type: object\nrequired: [message]\nproperties:\n  message: {type: integer}\n",
		"a forbidden key at the end":            "type: object\nnot: {required: [code]}\nrequired: [message]\nproperties:\n  message: {type: string}\n",
		"a value keyword on message at the end": "type: object\nrequired: [message]\nproperties:\n  message: {type: string, maxLength: 3}\n",
	}
	for name, end := range wrong {
		t.Run(name, func(t *testing.T) {
			r, _ := fitRequests(t, chain(end))
			wantFail(t, r, postLoc+noMessageReason)
		})
	}
	t.Run("the chain ends at a missing schema", func(t *testing.T) {
		c := withComponents("schemas:\n" + newNoteSchema +
			"  Error:\n    $ref: \"#/components/schemas/A\"\n  A:\n    $ref: \"#/components/schemas/B\"\n  B:\n    $ref: \"#/components/schemas/Gone\"\n")
		r, _ := fitRequests(t, c)
		wantFail(t, r, postLoc+noMessageReason)
	})
}

func TestRefusalDeclaredEndsOnARefLoop(t *testing.T) {
	cases := map[string]string{
		"loop of three": withComponents("schemas:\n" + newNoteSchema +
			"  Error:\n    $ref: \"#/components/schemas/A\"\n  A:\n    $ref: \"#/components/schemas/B\"\n  B:\n    $ref: \"#/components/schemas/A\"\n"),
		"loop of three after a tail": withComponents("schemas:\n" + newNoteSchema +
			"  Error:\n    $ref: \"#/components/schemas/T\"\n  T:\n    $ref: \"#/components/schemas/A\"\n  A:\n    $ref: \"#/components/schemas/B\"\n  B:\n    $ref: \"#/components/schemas/C\"\n  C:\n    $ref: \"#/components/schemas/A\"\n"),
		"object whose message loops through three": withComponents("schemas:\n" + newNoteSchema +
			"  Error:\n    type: object\n    required: [message]\n    properties:\n      message: {$ref: \"#/components/schemas/M1\"}\n  M1:\n    $ref: \"#/components/schemas/M2\"\n  M2:\n    $ref: \"#/components/schemas/M3\"\n  M3:\n    $ref: \"#/components/schemas/M1\"\n"),
		"loop in the response refs": notesContract{
			responses:  "\"201\": {description: ok}\n\"400\":\n  $ref: \"#/components/responses/A\"\n",
			components: okComponents + "responses:\n  A:\n    $ref: \"#/components/responses/B\"\n  B:\n    $ref: \"#/components/responses/C\"\n  C:\n    $ref: \"#/components/responses/A\"\n",
		}.String(),
	}
	for name, contract := range cases {
		t.Run(name, func(t *testing.T) {
			r, _ := fitRequests(t, contract) // fails when Fit does not end
			wantFail(t, r, postLoc+noMessageReason)
		})
	}
}

const (
	hiddenHead = "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\n"
	// A valid operation with a request body and a 400, as it would be written in a path item.
	hiddenPost    = "      requestBody:\n        required: true\n        content:\n          application/json:\n            schema: {type: object}\n      responses:\n        \"201\": {description: ok}\n" + good400
	hiddenVisible = "  /list:\n    get:\n      responses:\n        \"200\": {description: ok}\n"
)

// A shared base that a path item merges in (<<) or a path item that is a $ref can carry an
// operation with a request body the request conditions never reach. Each of these has a visible
// HTTP operation too, and the hidden operation's body and 400 are valid, so the request conditions
// pass whether or not they see through the hiding. The fit result is "not fit" because
// OnlyHttpOperations refuses the hiding.
func TestOnlyHttpOperationsRefusesHiddenRequestBodies(t *testing.T) {
	cases := map[string]struct{ contract, location string }{
		"merge key in a path item": {
			hiddenHead + "x-base: &base\n  post:\n" + hiddenPost + "paths:\n" + hiddenVisible + "  /notes:\n    <<: *base\n", "paths//notes/<<"},
		"merge key beside an own operation": {
			hiddenHead + "x-base: &base\n  post:\n" + hiddenPost + "paths:\n  /notes:\n    <<: *base\n    get:\n      responses:\n        \"200\": {description: ok}\n", "paths//notes/<<"},
		"merge of a list of mappings": {
			hiddenHead + "x-base: &base\n  post:\n" + hiddenPost + "paths:\n" + hiddenVisible + "  /notes:\n    <<: [*base]\n", "paths//notes/<<"},
		"path item that is a $ref": {
			hiddenHead + "x-shared:\n  item:\n    post:\n" + hiddenPost + "paths:\n" + hiddenVisible + "  /notes:\n    $ref: \"#/x-shared/item\"\n", "paths//notes/$ref"},
		"path item $ref beside operations": {
			hiddenHead + "x-shared:\n  item:\n    post:\n" + hiddenPost + "paths:\n  /notes:\n    $ref: \"#/x-shared/item\"\n    get:\n      responses:\n        \"200\": {description: ok}\n", "paths//notes/$ref"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			m := fitAll(t, c.contract)
			wantFail(t, m["OnlyHttpOperations"], c.location)
			wantPass(t, m[refusalDeclared])
			wantPass(t, m[requestBodiesSupported])
			passed := 0
			for _, r := range m {
				if r.Passed {
					passed++
				}
			}
			if passed == len(m) {
				t.Errorf("every condition passed, so the slice would be fit: %+v", m)
			}
		})
	}
}

// The same operation written in place is fit, so the failures above come from the hiding and not
// from the operation.
func TestOnlyHttpOperationsAcceptsTheSameOperationInPlace(t *testing.T) {
	m := fitAll(t, hiddenHead+"paths:\n  /notes:\n    post:\n"+hiddenPost)
	for _, c := range contractConditions {
		wantPass(t, m[c])
	}
}
