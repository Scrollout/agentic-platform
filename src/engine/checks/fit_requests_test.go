// T009: failing tests for the two request fit conditions, RefusalDeclared and
// RequestBodiesSupported (feature 010-strict-requests, contracts/strict-requests.md "Fit
// conditions", catalogue README "What the conditions keep out").
//
// The conditions are run the way the engine runs them: through Fit, with a pattern that lists them.
// The real pattern.yaml gains them only in T012, so the test pattern is the real one with the two
// names appended to its conditions list; until T012 the engine's answer for them is "not
// implemented", a failing result, which is why the passing tests fail now.
package checks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	refusalDeclared        = "RefusalDeclared"
	requestBodiesSupported = "RequestBodiesSupported"
)

// requestsPattern is the real pattern with the two conditions appended after the last one it lists
// today. It fails loudly when that anchor is gone; a name the pattern already lists is not added
// twice (after T012).
func requestsPattern(t *testing.T) []byte {
	t.Helper()
	p := string(repoPattern(t))
	const anchor = "  - RulesNameOneSystem\n"
	if strings.Count(p, anchor) != 1 {
		t.Fatalf("pattern.yaml: want exactly one %q line to append the request conditions after", anchor)
	}
	add := ""
	for _, c := range []string{refusalDeclared, requestBodiesSupported} {
		if !strings.Contains(p, "  - "+c+"\n") {
			add += "  - " + c + "\n"
		}
	}
	return []byte(strings.Replace(p, anchor, anchor+add, 1))
}

func requestsFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "fit", "requests", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func requestsSlice(t *testing.T, contract string) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"rules":    json.RawMessage(requestsFixture(t, "rules-notes.json")),
		"contract": contract,
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// fitRequests runs Fit on the contract text and returns the two request conditions' results. It
// fails when Fit does not end within 20 seconds (a $ref loop must end).
func fitRequests(t *testing.T, contract string) (refusal, bodies CheckResult) {
	t.Helper()
	pattern, slice := requestsPattern(t), requestsSlice(t, contract)
	done := make(chan []CheckResult, 1)
	go func() { done <- Fit(pattern, slice) }()
	var rs []CheckResult
	select {
	case rs = <-done:
	case <-time.After(20 * time.Second):
		t.Fatalf("Fit did not end within 20s on:\n%s", contract)
	}
	found := map[string]bool{}
	for _, r := range rs {
		if r.Reason == "" {
			t.Errorf("%s: empty reason", r.Check)
		}
		switch r.Check {
		case refusalDeclared:
			if !found[r.Check] {
				refusal = r
			}
		case requestBodiesSupported:
			if !found[r.Check] {
				bodies = r
			}
		default:
			continue
		}
		found[r.Check] = true
	}
	for _, c := range []string{refusalDeclared, requestBodiesSupported} {
		if !found[c] {
			t.Fatalf("no result for %s in %+v", c, rs)
		}
	}
	return refusal, bodies
}

func wantPass(t *testing.T, r CheckResult) {
	t.Helper()
	if !r.Passed {
		t.Errorf("%s failed, want pass: %s", r.Check, r.Reason)
	}
	if r.Part != "contract" {
		t.Errorf("%s: part %q, want contract", r.Check, r.Part)
	}
}

func wantFail(t *testing.T, r CheckResult, parts ...string) {
	t.Helper()
	if r.Passed {
		t.Errorf("%s passed, want fail (reason %q)", r.Check, r.Reason)
		return
	}
	if r.Part != "contract" {
		t.Errorf("%s: part %q, want contract", r.Check, r.Part)
	}
	mustContain(t, r, parts...)
}

// examined asserts a passing result says what it examined: it names how many operations with a
// request body it looked at (a digit, or "no" when there are none) and which kind of thing.
func examined(t *testing.T, r CheckResult, n int, what string) {
	t.Helper()
	low := strings.ToLower(r.Reason)
	if n == 0 {
		if !strings.Contains(low, "no ") && !strings.Contains(r.Reason, "0") {
			t.Errorf("%s reason %q does not say that nothing was examined (no / 0)", r.Check, r.Reason)
		}
	} else if !strings.Contains(r.Reason, strconv.Itoa(n)) {
		t.Errorf("%s reason %q does not give the count %d it examined", r.Check, r.Reason, n)
	}
	if !strings.Contains(low, what) {
		t.Errorf("%s reason %q does not mention %q", r.Check, r.Reason, what)
	}
}

func indent(s string, n int) string {
	pad := strings.Repeat(" ", n)
	var b strings.Builder
	for _, l := range strings.SplitAfter(s, "\n") {
		if strings.TrimSpace(l) == "" {
			b.WriteString(l)
			continue
		}
		b.WriteString(pad + l)
	}
	return b.String()
}

// notesContract is POST /notes with a request body, a 201 and a 400, and the components they use.
// Each field replaces its default when set.
type notesContract struct {
	requestBody string // the requestBody entry, as "requestBody:\n  ..." (unindented)
	responses   string // the entries under responses, unindented
	components  string // the entries under components, unindented
}

const (
	okRequestBody = "requestBody:\n  required: true\n  content:\n    application/json:\n      schema:\n        $ref: \"#/components/schemas/NewNote\"\n"
	okResponses   = "\"201\": {description: ok}\n\"400\":\n  description: bad\n  content:\n    application/json:\n      schema:\n        $ref: \"#/components/schemas/Error\"\n"
	okComponents  = "schemas:\n  NewNote:\n    type: object\n    required: [text]\n    properties:\n      text: {type: string}\n  Error:\n    type: object\n    required: [message]\n    properties:\n      message: {type: string}\n"
)

func (c notesContract) String() string {
	if c.requestBody == "" {
		c.requestBody = okRequestBody
	}
	if c.responses == "" {
		c.responses = okResponses
	}
	if c.components == "" {
		c.components = okComponents
	}
	return "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths:\n  /notes:\n    post:\n      operationId: createNote\n" +
		indent(c.requestBody, 6) + "      responses:\n" + indent(c.responses, 8) +
		"components:\n" + indent(c.components, 2)
}

// withErrorSchema is a notes contract whose 400 holds the given schema inline.
func withErrorSchema(schema string) string {
	return notesContract{responses: "\"201\": {description: ok}\n\"400\":\n  description: bad\n  content:\n    application/json:\n      schema:\n" + indent(schema, 8)}.String()
}

// withBodySchema is a notes contract whose request body holds the given schema inline (and a
// valid 400, so only RequestBodiesSupported has anything to judge).
func withBodySchema(schema string) string {
	return notesContract{requestBody: "requestBody:\n  required: true\n  content:\n    application/json:\n      schema:\n" + indent(schema, 8)}.String()
}

// withComponents is a notes contract with the components given whole.
func withComponents(components string) string {
	return notesContract{components: components}.String()
}

const (
	noMessageReason = ": declares no 400 response with a JSON object body that requires only the string message"
	postLoc         = "paths//notes/post"
)

func TestRefusalDeclaredPasses(t *testing.T) {
	inlineError := "type: object\nrequired: [message]\nproperties:\n  message: {type: string}\n"
	cases := map[string]string{
		"evals notes contract":              requestsFixture(t, "contract-evals-notes.yaml"),
		"acceptance example, no body":       requestsFixture(t, "contract-acceptance-notes.yaml"),
		"Error behind a $ref":               notesContract{}.String(),
		"Error inline":                      withErrorSchema(inlineError),
		"optional members beside message":   withErrorSchema("type: object\nrequired: [message]\nproperties:\n  message: {type: string}\n  code: {type: integer}\n  details: {type: array, items: {type: string}}\n"),
		"annotations on object and message": withErrorSchema("type: object\ntitle: Error\ndescription: d\nexample: {message: m}\nx-note: {allOf: [1]}\nrequired: [message]\nproperties:\n  message: {type: string, description: d, title: t, default: x, example: y, deprecated: false, x-a: 1}\n"),
		"additionalProperties false":        withErrorSchema("type: object\nadditionalProperties: false\nrequired: [message]\nproperties:\n  message: {type: string}\n"),
		"additionalProperties true":         withErrorSchema("type: object\nadditionalProperties: true\nrequired: [message]\nproperties:\n  message: {type: string}\n"),
		"required in block style":           withErrorSchema("type: object\nrequired:\n  - message\nproperties:\n  message: {type: string}\n"),
		"400 key unquoted":                  notesContract{responses: "201: {description: ok}\n400:\n  description: bad\n  content:\n    application/json:\n      schema:\n        $ref: \"#/components/schemas/Error\"\n"}.String(),
		"400 a $ref to components/responses": notesContract{
			responses:  "\"201\": {description: ok}\n\"400\":\n  $ref: \"#/components/responses/Bad\"\n",
			components: okComponents + "responses:\n  Bad:\n    description: bad\n    content:\n      application/json:\n        schema:\n          $ref: \"#/components/schemas/Error\"\n",
		}.String(),
		"Error through two $refs":                  withComponents("schemas:\n  NewNote:\n    type: object\n    required: [text]\n    properties:\n      text: {type: string}\n  Error:\n    $ref: \"#/components/schemas/Inner\"\n  Inner:\n    type: object\n    required: [message]\n    properties:\n      message: {$ref: \"#/components/schemas/Msg\"}\n  Msg:\n    type: string\n"),
		"other media type beside application/json": notesContract{responses: "\"201\": {description: ok}\n\"400\":\n  description: bad\n  content:\n    text/plain: {schema: {type: string}}\n    application/json:\n      schema:\n        $ref: \"#/components/schemas/Error\"\n"}.String(),
		"4XX beside a good 400":                    notesContract{responses: okResponses + "\"4XX\": {description: other}\ndefault: {description: other}\n"}.String(),
	}
	for name, contract := range cases {
		t.Run(name, func(t *testing.T) {
			r, _ := fitRequests(t, contract)
			wantPass(t, r)
		})
	}
}

func TestRefusalDeclaredPassSaysWhatItExamined(t *testing.T) {
	r, _ := fitRequests(t, notesContract{}.String())
	wantPass(t, r)
	examined(t, r, 1, "400")
	r, _ = fitRequests(t, requestsFixture(t, "contract-acceptance-notes.yaml"))
	wantPass(t, r)
	examined(t, r, 0, "request")
}

func TestRefusalDeclaredFails(t *testing.T) {
	obj := func(extra string) string {
		return "type: object\nrequired: [message]\n" + extra + "properties:\n  message: {type: string}\n"
	}
	msg := func(kw string) string {
		return "type: object\nrequired: [message]\nproperties:\n  message: {type: string, " + kw + "}\n"
	}
	cases := map[string]string{
		"no 400 at all":                       notesContract{responses: "\"201\": {description: ok}\n"}.String(),
		"4XX alone":                           notesContract{responses: "\"201\": {description: ok}\n4XX:\n  description: bad\n  content:\n    application/json:\n      schema:\n        $ref: \"#/components/schemas/Error\"\n"}.String(),
		"4XX quoted alone":                    notesContract{responses: "\"201\": {description: ok}\n\"4XX\":\n  description: bad\n  content:\n    application/json:\n      schema:\n        $ref: \"#/components/schemas/Error\"\n"}.String(),
		"default alone":                       notesContract{responses: "\"201\": {description: ok}\ndefault:\n  description: bad\n  content:\n    application/json:\n      schema:\n        $ref: \"#/components/schemas/Error\"\n"}.String(),
		"400 other code 404":                  notesContract{responses: "\"201\": {description: ok}\n\"404\":\n  description: bad\n  content:\n    application/json:\n      schema:\n        $ref: \"#/components/schemas/Error\"\n"}.String(),
		"400 without content":                 notesContract{responses: "\"201\": {description: ok}\n\"400\": {description: bad}\n"}.String(),
		"400 content empty":                   notesContract{responses: "\"201\": {description: ok}\n\"400\":\n  description: bad\n  content: {}\n"}.String(),
		"400 only text/plain":                 notesContract{responses: "\"201\": {description: ok}\n\"400\":\n  description: bad\n  content:\n    text/plain:\n      schema: {type: string}\n"}.String(),
		"400 json without schema":             notesContract{responses: "\"201\": {description: ok}\n\"400\":\n  description: bad\n  content:\n    application/json: {}\n"}.String(),
		"400 $ref to a missing response":      notesContract{responses: "\"201\": {description: ok}\n\"400\":\n  $ref: \"#/components/responses/Missing\"\n"}.String(),
		"schema type string":                  withErrorSchema("type: string\n"),
		"schema type array":                   withErrorSchema("type: array\nitems: {type: string}\n"),
		"schema without type":                 withErrorSchema("required: [message]\nproperties:\n  message: {type: string}\n"),
		"schema type list":                    withErrorSchema("type: [object, \"null\"]\nrequired: [message]\nproperties:\n  message: {type: string}\n"),
		"required missing":                    withErrorSchema("type: object\nproperties:\n  message: {type: string}\n"),
		"required message and code":           withErrorSchema("type: object\nrequired: [message, code]\nproperties:\n  message: {type: string}\n  code: {type: integer}\n"),
		"required empty":                      withErrorSchema("type: object\nrequired: []\nproperties:\n  message: {type: string}\n"),
		"required only code":                  withErrorSchema("type: object\nrequired: [code]\nproperties:\n  message: {type: string}\n  code: {type: string}\n"),
		"required message twice":              withErrorSchema("type: object\nrequired: [message, message]\nproperties:\n  message: {type: string}\n"),
		"required name in other case":         withErrorSchema("type: object\nrequired: [Message]\nproperties:\n  Message: {type: string}\n"),
		"no properties":                       withErrorSchema("type: object\nrequired: [message]\n"),
		"message absent":                      withErrorSchema("type: object\nrequired: [message]\nproperties:\n  code: {type: string}\n"),
		"message not string":                  withErrorSchema("type: object\nrequired: [message]\nproperties:\n  message: {type: integer}\n"),
		"message without type":                withErrorSchema("type: object\nrequired: [message]\nproperties:\n  message: {}\n"),
		"message type list":                   withErrorSchema("type: object\nrequired: [message]\nproperties:\n  message: {type: [string, \"null\"]}\n"),
		"message type array":                  withErrorSchema("type: object\nrequired: [message]\nproperties:\n  message: {type: array, items: {type: string}}\n"),
		"ref does not resolve":                notesContract{responses: "\"201\": {description: ok}\n\"400\":\n  description: bad\n  content:\n    application/json:\n      schema:\n        $ref: \"#/components/schemas/Nope\"\n"}.String(),
		"message ref does not resolve":        withErrorSchema("type: object\nrequired: [message]\nproperties:\n  message: {$ref: \"#/components/schemas/Nope\"}\n"),
		"message maxLength":                   withErrorSchema(msg("maxLength: 100")),
		"message minLength":                   withErrorSchema(msg("minLength: 1")),
		"message pattern":                     withErrorSchema(msg("pattern: \"^a\"")),
		"message format":                      withErrorSchema(msg("format: email")),
		"message enum":                        withErrorSchema(msg("enum: [a, b]")),
		"message const":                       withErrorSchema(msg("const: a")),
		"message value keyword behind a $ref": withComponents("schemas:\n  NewNote:\n    type: object\n    required: [text]\n    properties:\n      text: {type: string}\n  Error:\n    type: object\n    required: [message]\n    properties:\n      message: {$ref: \"#/components/schemas/Msg\"}\n  Msg:\n    type: string\n    maxLength: 5\n"),
		"object minProperties":                withErrorSchema(obj("minProperties: 1\n")),
		"object not":                          withErrorSchema(obj("not: {required: [code]}\n")),
		"object allOf":                        withErrorSchema(obj("allOf: [{type: object}]\n")),
		"object const":                        withErrorSchema(obj("const: {message: x}\n")),
		"object enum":                         withErrorSchema(obj("enum: [{message: x}]\n")),
		"object oneOf":                        withErrorSchema(obj("oneOf: [{type: object}]\n")),
		"object unknown key":                  withErrorSchema(obj("frobnicate: 1\n")),
		"object maxProperties":                withErrorSchema(obj("maxProperties: 3\n")),
		"object patternProperties":            withErrorSchema(obj("patternProperties: {\"^a\": {type: string}}\n")),
		"object nullable":                     withErrorSchema(obj("nullable: true\n")),
		"a $ref loop in the 400 schema":       withComponents("schemas:\n  NewNote:\n    type: object\n    required: [text]\n    properties:\n      text: {type: string}\n  Error:\n    $ref: \"#/components/schemas/Loop\"\n  Loop:\n    $ref: \"#/components/schemas/Error\"\n"),
		"a $ref to itself":                    withComponents("schemas:\n  NewNote:\n    type: object\n    required: [text]\n    properties:\n      text: {type: string}\n  Error:\n    $ref: \"#/components/schemas/Error\"\n"),
		"a $ref loop through message":         withComponents("schemas:\n  NewNote:\n    type: object\n    required: [text]\n    properties:\n      text: {type: string}\n  Error:\n    type: object\n    required: [message]\n    properties:\n      message: {$ref: \"#/components/schemas/M\"}\n  M:\n    $ref: \"#/components/schemas/M2\"\n  M2:\n    $ref: \"#/components/schemas/M\"\n"),
		"400 response loop":                   notesContract{responses: "\"201\": {description: ok}\n\"400\":\n  $ref: \"#/components/responses/A\"\n", components: okComponents + "responses:\n  A:\n    $ref: \"#/components/responses/B\"\n  B:\n    $ref: \"#/components/responses/A\"\n"}.String(),
	}
	for name, contract := range cases {
		t.Run(name, func(t *testing.T) {
			r, _ := fitRequests(t, contract)
			wantFail(t, r, postLoc+noMessageReason)
		})
	}
}

// The failing 400 is RefusalDeclared's business only: the same contracts keep RequestBodiesSupported
// passing, so the two conditions judge separately.
func TestRefusalDeclaredFailureLeavesRequestBodiesSupportedAlone(t *testing.T) {
	_, b := fitRequests(t, notesContract{responses: "\"201\": {description: ok}\n"}.String())
	wantPass(t, b)
}

const twoOpsHead = "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths:\n"

func bodyOp(method string, loc400 string) string {
	return "    " + method + ":\n      requestBody:\n        content:\n          application/json:\n            schema: {type: object}\n      responses:\n        \"201\": {description: ok}\n" + loc400
}

const good400 = "        \"400\":\n          description: bad\n          content:\n            application/json:\n              schema: {type: object, required: [message], properties: {message: {type: string}}}\n"

func TestRefusalDeclaredNamesEveryFailingOperationInDocumentOrder(t *testing.T) {
	contract := twoOpsHead +
		"  /zeta:\n" + bodyOp("post", "") +
		"  /alpha:\n" + bodyOp("put", "        \"400\": {description: no body}\n") +
		"    get:\n      responses:\n        \"200\": {description: ok}\n" +
		"  /mid:\n" + bodyOp("patch", good400) +
		"  /omega:\n" + bodyOp("delete", "")
	r, _ := fitRequests(t, contract)
	wantFail(t, r, "paths//zeta/post"+noMessageReason, "paths//alpha/put"+noMessageReason, "paths//omega/delete"+noMessageReason)
	for _, not := range []string{"paths//mid/patch", "paths//alpha/get"} {
		if strings.Contains(r.Reason, not) {
			t.Errorf("reason %q names %s, which is fine or has no request body", r.Reason, not)
		}
	}
	z, a, o := strings.Index(r.Reason, "paths//zeta/post"), strings.Index(r.Reason, "paths//alpha/put"), strings.Index(r.Reason, "paths//omega/delete")
	if z < 0 || z >= a || a >= o {
		t.Errorf("operations not in document order (zeta %d, alpha %d, omega %d): %q", z, a, o, r.Reason)
	}
}

func TestRefusalDeclaredIgnoresOperationsWithoutBody(t *testing.T) {
	contract := twoOpsHead + "  /notes:\n    get:\n      responses:\n        \"200\": {description: ok}\n    delete:\n      responses:\n        \"204\": {description: ok}\n"
	r, _ := fitRequests(t, contract)
	wantPass(t, r)
}

func TestRefusalDeclaredJudgesEveryOperationWithABody(t *testing.T) {
	contract := twoOpsHead + "  /a:\n" + bodyOp("post", good400) + "  /b:\n" + bodyOp("put", good400)
	r, _ := fitRequests(t, contract)
	wantPass(t, r)
	examined(t, r, 2, "400")
}

func TestRefusalDeclaredFollowsRequestBodyRef(t *testing.T) {
	// A requestBody that is a $ref to components/requestBodies is still an operation with a body.
	contract := notesContract{
		requestBody: "requestBody:\n  $ref: \"#/components/requestBodies/NewNote\"\n",
		responses:   "\"201\": {description: ok}\n",
		components:  okComponents + "requestBodies:\n  NewNote:\n    content:\n      application/json:\n        schema: {type: object}\n",
	}.String()
	r, _ := fitRequests(t, contract)
	wantFail(t, r, postLoc+noMessageReason)
}

func TestRequestBodiesSupportedPasses(t *testing.T) {
	everything := "type: object\n" +
		"title: T\ndescription: d\ndefault: {}\nexamples: [{}]\nexample: {}\ndeprecated: false\nreadOnly: false\nwriteOnly: false\n$comment: c\n" +
		"x-vendor: {allOf: [1], oneOf: 2, nullable: true}\n" +
		"required: [s]\nadditionalProperties: false\nminProperties: 1\nmaxProperties: 9\n" +
		"properties:\n" +
		"  s: {type: string, maxLength: 9, minLength: 1, pattern: \"^a\", format: email, enum: [a], const: a}\n" +
		"  n: {type: number, maximum: 9, minimum: 1, exclusiveMaximum: 10, exclusiveMinimum: 0, multipleOf: 1}\n" +
		"  i: {type: [integer, \"null\"]}\n" +
		"  l: {type: [\"null\", string]}\n" +
		"  a: {type: array, items: {type: string}, maxItems: 3, minItems: 1, uniqueItems: true}\n" +
		"  b: {type: boolean}\n" +
		"  z: {type: \"null\"}\n" +
		"  o: {type: object, additionalProperties: true}\n" +
		"  r: {$ref: \"#/components/schemas/Other\", description: d, x-y: {not: 1}, title: t, $comment: c}\n" +
		"  oneOf: {type: string}\n  nullable: {type: string}\n  allOf: {type: string}\n  not: {type: string}\n  if: {type: string}\n" +
		"  any: {}\n"
	other := "schemas:\n  Other:\n    type: string\n  Error:\n    type: object\n    required: [message]\n    properties:\n      message: {type: string}\n"
	cases := map[string]string{
		"evals notes contract":             requestsFixture(t, "contract-evals-notes.yaml"),
		"acceptance example, no body":      requestsFixture(t, "contract-acceptance-notes.yaml"),
		"every allowed keyword":            notesContract{requestBody: "requestBody:\n  required: true\n  content:\n    application/json:\n      schema:\n" + indent(everything, 8), components: other}.String(),
		"a requestBody $ref":               notesContract{requestBody: "requestBody:\n  $ref: \"#/components/requestBodies/NewNote\"\n", components: okComponents + "requestBodies:\n  NewNote:\n    required: true\n    content:\n      application/json:\n        schema: {$ref: \"#/components/schemas/NewNote\"}\n"}.String(),
		"recursive schema":                 withComponents(okComponents[:len("schemas:\n")] + "  Tree:\n    type: object\n    properties:\n      children: {type: array, items: {$ref: \"#/components/schemas/Tree\"}}\n  NewNote: {$ref: \"#/components/schemas/Tree\"}\n  Error:\n    type: object\n    required: [message]\n    properties:\n      message: {type: string}\n"),
		"body schema that is a bare type":  withBodySchema("type: string\n"),
		"body schema with no type":         withBodySchema("{}\n"),
		"body schema array of objects":     withBodySchema("type: array\nitems:\n  type: object\n  properties: {a: {type: string}}\n"),
		"property named like a keyword":    withBodySchema("type: object\nproperties:\n  oneOf: {type: string}\n  nullable: {type: boolean}\n  $ref: {type: string}\n"),
		"x- value holding allOf":           withBodySchema("type: object\nx-extra:\n  allOf: [{type: string}]\n  nullable: true\nproperties:\n  a: {type: string, x-b: {oneOf: []}}\n"),
		"$ref with annotation and x- only": withBodySchema("$ref: \"#/components/schemas/Error\"\ndescription: d\nx-a: 1\n"),
		"additionalProperties false":       withBodySchema("type: object\nadditionalProperties: false\nproperties: {a: {type: string}}\n"),
	}
	for name, contract := range cases {
		t.Run(name, func(t *testing.T) {
			_, b := fitRequests(t, contract)
			wantPass(t, b)
		})
	}
}

func TestRequestBodiesSupportedPassSaysWhatItExamined(t *testing.T) {
	_, b := fitRequests(t, notesContract{}.String())
	wantPass(t, b)
	examined(t, b, 1, "request")
	_, b = fitRequests(t, requestsFixture(t, "contract-acceptance-notes.yaml"))
	wantPass(t, b)
	examined(t, b, 0, "request")
	_, b = fitRequests(t, twoOpsHead+"  /a:\n"+bodyOp("post", good400)+"  /b:\n"+bodyOp("put", good400))
	wantPass(t, b)
	examined(t, b, 2, "request")
}

var notSupported = []string{
	"allOf", "anyOf", "oneOf", "not", "if", "then", "else", "dependentSchemas", "dependentRequired",
	"patternProperties", "propertyNames", "prefixItems", "contains", "unevaluatedProperties",
	"unevaluatedItems", "$dynamicRef", "$dynamicAnchor", "$anchor", "$id", "$defs", "$schema",
	"contentMediaType", "discriminator", "xml", "externalDocs", "minContains", "frobnicate",
}

// keywordValue is a value that is well formed for the keyword, so only the keyword is at fault.
func keywordValue(kw string) string {
	switch kw {
	case "allOf", "anyOf", "oneOf", "prefixItems":
		return "[{type: string}]"
	case "dependentRequired":
		return "{a: [b]}"
	case "if", "then", "else", "not", "contains", "propertyNames", "unevaluatedProperties", "unevaluatedItems":
		return "{type: string}"
	case "dependentSchemas", "patternProperties":
		return "{a: {type: string}}"
	case "minContains":
		return "1"
	}
	return "\"x\""
}

func TestRequestBodiesSupportedFailsForEachUnsupportedKeyword(t *testing.T) {
	for _, kw := range notSupported {
		v := keywordValue(kw)
		cases := map[string]struct{ contract, loc string }{
			"top level":           {withBodySchema("type: object\n" + kw + ": " + v + "\nproperties: {a: {type: string}}\n"), postLoc},
			"nested property":     {withBodySchema("type: object\nproperties:\n  a:\n    type: object\n    properties:\n      b: {type: string, " + kw + ": " + v + "}\n"), postLoc},
			"array items":         {withBodySchema("type: array\nitems: {type: string, " + kw + ": " + v + "}\n"), postLoc},
			"only through a $ref": {withComponents("schemas:\n  NewNote:\n    type: object\n    properties:\n      a: {$ref: \"#/components/schemas/Deep\"}\n  Deep:\n    type: string\n    " + kw + ": " + v + "\n  Error:\n    type: object\n    required: [message]\n    properties:\n      message: {type: string}\n"), ""},
		}
		for place, c := range cases {
			t.Run(kw+"/"+place, func(t *testing.T) {
				r, b := fitRequests(t, c.contract)
				wantFail(t, b, kw, "is not supported in a request body")
				if c.loc != "" {
					mustContain(t, b, c.loc)
				}
				wantPass(t, r)
			})
		}
	}
}

func TestRequestBodiesSupportedFailsOnOtherShapes(t *testing.T) {
	cases := map[string]string{
		"nullable true":                            withBodySchema("type: string\nnullable: true\n"),
		"nullable false":                           withBodySchema("type: string\nnullable: false\n"),
		"additionalProperties schema":              withBodySchema("type: object\nadditionalProperties: {type: string}\n"),
		"additionalProperties empty schema":        withBodySchema("type: object\nadditionalProperties: {}\n"),
		"shaping key beside $ref (type)":           withBodySchema("$ref: \"#/components/schemas/Error\"\ntype: object\n"),
		"shaping key beside $ref (not)":            withBodySchema("$ref: \"#/components/schemas/Error\"\nnot: {type: string}\n"),
		"shaping key beside $ref (properties)":     withBodySchema("$ref: \"#/components/schemas/Error\"\nproperties: {a: {type: string}}\n"),
		"name twice in required":                   withBodySchema("type: object\nrequired: [a, a]\nproperties: {a: {type: string}}\n"),
		"name twice in required, not declared":     withBodySchema("type: object\nrequired: [a, b, a]\n"),
		"requestBody without content":              notesContract{requestBody: "requestBody:\n  required: true\n"}.String(),
		"requestBody with empty content":           notesContract{requestBody: "requestBody:\n  content: {}\n"}.String(),
		"type integer and number":                  withBodySchema("type: [integer, number]\n"),
		"type string, integer and null":            withBodySchema("type: [string, integer, \"null\"]\n"),
		"type list of one":                         withBodySchema("type: [string, string]\n"),
		"type null alone in a list twice":          withBodySchema("type: [\"null\", \"null\"]\n"),
		"type list without null":                   withBodySchema("type: [string, object]\n"),
		"type str":                                 withBodySchema("type: str\n"),
		"type list string and date":                withBodySchema("type: [string, date]\n"),
		"type Object in other case":                withBodySchema("type: Object\n"),
		"type empty list":                          withBodySchema("type: []\n"),
		"type list with unquoted null":             withBodySchema("type: [string, null]\n"),
		"type unquoted null alone":                 withBodySchema("type: null\n"),
		"type list with a number":                  withBodySchema("type: [string, 1]\n"),
		"type a mapping":                           withBodySchema("type: {a: b}\n"),
		"nested type str":                          withBodySchema("type: object\nproperties:\n  a: {type: str}\n"),
		"items true":                               withBodySchema("type: array\nitems: true\n"),
		"items false":                              withBodySchema("type: array\nitems: false\n"),
		"property schema true":                     withBodySchema("type: object\nproperties:\n  a: true\n"),
		"schema false":                             notesContract{requestBody: "requestBody:\n  content:\n    application/json:\n      schema: false\n"}.String(),
		"schema true":                              notesContract{requestBody: "requestBody:\n  content:\n    application/json:\n      schema: true\n"}.String(),
		"YAML anchor in a schema":                  withBodySchema("type: object\nproperties:\n  a: &a {type: string}\n"),
		"YAML anchor and alias in a schema":        withBodySchema("type: object\nproperties:\n  a: &a {type: string}\n  b: *a\n"),
		"YAML alias of a component schema":         withComponents("schemas:\n  Base: &base {type: string}\n  NewNote:\n    type: object\n    properties:\n      a: *base\n  Error:\n    type: object\n    required: [message]\n    properties:\n      message: {type: string}\n"),
		"body with no schema":                      notesContract{requestBody: "requestBody:\n  content:\n    application/json: {}\n"}.String(),
		"body with schema null":                    notesContract{requestBody: "requestBody:\n  content:\n    application/json:\n      schema:\n"}.String(),
		"a $ref that does not resolve":             withBodySchema("$ref: \"#/components/schemas/Nope\"\n"),
		"a property $ref that does not resolve":    withBodySchema("type: object\nproperties:\n  a: {$ref: \"#/components/schemas/Nope\"}\n"),
		"a requestBody $ref that does not resolve": notesContract{requestBody: "requestBody:\n  $ref: \"#/components/requestBodies/Nope\"\n"}.String(),
		"$ref loop":                                withComponents("schemas:\n  NewNote:\n    $ref: \"#/components/schemas/A\"\n  A:\n    $ref: \"#/components/schemas/B\"\n  B:\n    $ref: \"#/components/schemas/A\"\n  Error:\n    type: object\n    required: [message]\n    properties:\n      message: {type: string}\n"),
		"$ref to itself":                           withComponents("schemas:\n  NewNote:\n    $ref: \"#/components/schemas/NewNote\"\n  Error:\n    type: object\n    required: [message]\n    properties:\n      message: {type: string}\n"),
	}
	for name, contract := range cases {
		t.Run(name, func(t *testing.T) {
			r, b := fitRequests(t, contract)
			wantFail(t, b, postLoc)
			wantPass(t, r)
		})
	}
}

func TestRequestBodiesSupportedFailsOnMediaTypes(t *testing.T) {
	schema := "schema: {type: object}\n"
	body := func(content string) string {
		return notesContract{requestBody: "requestBody:\n  content:\n" + indent(content, 4)}.String()
	}
	cases := map[string]struct{ content, names string }{
		"text/plain alone":                   {"text/plain:\n  " + schema, "text/plain"},
		"text/plain beside application/json": {"application/json:\n  " + schema + "text/plain:\n  " + schema, "text/plain"},
		"application/xml alone":              {"application/xml:\n  " + schema, "application/xml"},
		"json with a charset":                {"application/json; charset=utf-8:\n  " + schema, "application/json; charset=utf-8"},
		"two other media types":              {"application/json:\n  " + schema + "text/plain:\n  " + schema + "application/xml:\n  " + schema, "text/plain"},
		"wildcard":                           {"\"*/*\":\n  " + schema, "*/*"},
		"application/problem+json":           {"application/problem+json:\n  " + schema, "application/problem+json"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r, b := fitRequests(t, body(c.content))
			wantFail(t, b, postLoc+"/requestBody: media types other than application/json: ", c.names)
			wantPass(t, r)
		})
	}
	t.Run("two other media types name both", func(t *testing.T) {
		_, b := fitRequests(t, body(cases["two other media types"].content))
		wantFail(t, b, "text/plain", "application/xml")
	})
	t.Run("a no-schema body is named", func(t *testing.T) {
		_, b := fitRequests(t, notesContract{requestBody: "requestBody:\n  content:\n    application/json: {}\n"}.String())
		wantFail(t, b, postLoc+"/requestBody: no schema")
	})
}

func TestRequestBodiesSupportedNamesEveryFailingLocationInDocumentOrder(t *testing.T) {
	bad := func(method, kw string) string {
		return "    " + method + ":\n      requestBody:\n        content:\n          application/json:\n            schema: {type: object, " + kw + ": {type: string}}\n      responses:\n        \"201\": {description: ok}\n" + good400
	}
	contract := twoOpsHead +
		"  /zeta:\n" + bad("post", "not") +
		"  /mid:\n" + bodyOp("put", good400) +
		"  /alpha:\n" + bad("patch", "contains") +
		"  /omega:\n    delete:\n      requestBody:\n        content:\n          text/plain: {schema: {type: string}}\n      responses:\n        \"201\": {description: ok}\n" + good400
	_, b := fitRequests(t, contract)
	wantFail(t, b, "paths//zeta/post", "paths//alpha/patch", "paths//omega/delete", "not is not supported", "contains is not supported", "text/plain")
	if strings.Contains(b.Reason, "paths//mid/put") {
		t.Errorf("reason %q names the operation that is fine", b.Reason)
	}
	z, a, o := strings.Index(b.Reason, "paths//zeta/post"), strings.Index(b.Reason, "paths//alpha/patch"), strings.Index(b.Reason, "paths//omega/delete")
	if z < 0 || z >= a || a >= o {
		t.Errorf("locations not in document order (zeta %d, alpha %d, omega %d): %q", z, a, o, b.Reason)
	}
}

func TestRequestBodiesSupportedFailureIsNotAnotherConditionsBusiness(t *testing.T) {
	// A 400 problem alone does not fail RequestBodiesSupported (checked above); here an operation
	// with no request body is never judged by either condition, whatever its schemas hold.
	contract := twoOpsHead + "  /notes:\n    get:\n      responses:\n        \"200\":\n          description: ok\n          content:\n            application/json:\n              schema: {allOf: [{type: string}], nullable: true}\n"
	r, b := fitRequests(t, contract)
	wantPass(t, r)
	wantPass(t, b)
}

func TestRequestConditionsFailAnUnreadableContract(t *testing.T) {
	for _, name := range []string{"contract-unparseable.yaml", "contract-multi-document.yaml", "contract-duplicate-key.yaml"} {
		t.Run(name, func(t *testing.T) {
			r, b := fitRequests(t, requestsFixture(t, name))
			for _, res := range []CheckResult{r, b} {
				wantFail(t, res)
				if !res.Unreadable {
					t.Errorf("%s: Unreadable not set; the contract was not checked at all (reason %q)", res.Check, res.Reason)
				}
			}
		})
	}
	// The key 400 written as 400 and as "400" is one key to the YAML reader, so a contract with
	// both is a duplicate key: unreadable, not merely unfit.
	t.Run("both 400 and \"400\" in one responses mapping", func(t *testing.T) {
		both := "\"201\": {description: ok}\n400:\n  description: bad\n  content:\n    application/json:\n      schema:\n        $ref: \"#/components/schemas/Error\"\n\"400\":\n  description: bad\n  content:\n    application/json:\n      schema:\n        $ref: \"#/components/schemas/Error\"\n"
		r, b := fitRequests(t, notesContract{responses: both}.String())
		for _, res := range []CheckResult{r, b} {
			wantFail(t, res)
			if !res.Unreadable {
				t.Errorf("%s: Unreadable not set (reason %q)", res.Check, res.Reason)
			}
		}
	})
	t.Run("duplicate key inside a request body schema", func(t *testing.T) {
		r, b := fitRequests(t, withBodySchema("type: object\nproperties:\n  a: {type: string}\n  a: {type: integer}\n"))
		for _, res := range []CheckResult{r, b} {
			wantFail(t, res)
			if !res.Unreadable {
				t.Errorf("%s: Unreadable not set (reason %q)", res.Check, res.Reason)
			}
		}
	})
}

func TestRequestConditionsFailASliceWithoutAContract(t *testing.T) {
	rs := Fit(requestsPattern(t), []byte(`{"rules": []}`))
	seen := 0
	for _, r := range rs {
		if r.Check == refusalDeclared || r.Check == requestBodiesSupported {
			seen++
			wantFail(t, r)
		}
	}
	if seen != 2 {
		t.Errorf("got %d request results in %+v, want 2", seen, rs)
	}
}
