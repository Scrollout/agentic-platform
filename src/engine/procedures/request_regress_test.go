package procedures_test

// T024 (feature 010): regression tests, by a test author, for review round 1 of US1 (F1-F5). They
// are served like T004's: the contract testdata/requests/regress.yaml is generated with the real
// template, built with a recording handler and asked over HTTP. They pin that PUT, PATCH and DELETE
// bodies on a path with a parameter are refused and rewritten like POST, that a name written with a
// JSON escape is the name it spells, that YAML booleans in any spelling are read as booleans
// (additionalProperties, requestBody.required), and that a 1 MiB array of {} is answered quickly.
// Helpers (rcServe, rcRunCases, rcRefuse, rcPass, rcDo, rcCase) come from request_check_test.go.

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

const rcRegressMethods = `
func (rec) PutNote(w http.ResponseWriter, r *http.Request, id string)    { echo(w, r) }
func (rec) PatchNote(w http.ResponseWriter, r *http.Request, id string)  { echo(w, r) }
func (rec) DeleteNote(w http.ResponseWriter, r *http.Request, id string) { echo(w, r) }
func (rec) CreateClosedCapital(w http.ResponseWriter, r *http.Request)   { echo(w, r) }
func (rec) CreateClosedUpper(w http.ResponseWriter, r *http.Request)     { echo(w, r) }
func (rec) CreateClosedLower(w http.ResponseWriter, r *http.Request)     { echo(w, r) }
func (rec) CreateOpenCapital(w http.ResponseWriter, r *http.Request)     { echo(w, r) }
func (rec) CreateNeededCapital(w http.ResponseWriter, r *http.Request)   { echo(w, r) }
func (rec) CreateNeededUpper(w http.ResponseWriter, r *http.Request)     { echo(w, r) }
func (rec) CreateOptionalCapital(w http.ResponseWriter, r *http.Request) { echo(w, r) }
func (rec) CreateBulk(w http.ResponseWriter, r *http.Request)            { echo(w, r) }
`

func rcRegressCases() []rcCase {
	var cs []rcCase
	for _, m := range []string{"PUT", "PATCH", "DELETE"} {
		for _, id := range []string{"7", "a%2Fb"} { // a plain and an escaped path parameter
			op := m + " /notes/" + id
			n := m + " " + id + ": "
			cs = append(cs,
				rcRefuse(n+"a missing required member", op, `{}`, `member "/text" is required`),
				rcRefuse(n+"wrong case of the member", op, `{"TEXT":"a"}`, `member "/text" is required`),
				rcRefuse(n+"the wrong type", op, `{"text":5}`, `member "/text" must be a string`),
				rcRefuse(n+"not JSON", op, `{"text":`, "the request body is not valid JSON"),
				rcRefuse(n+"empty and required", op, ``, "the request body is missing"),
				rcRefuse(n+"a duplicate", op, `{"text":"a","text":"b"}`, `member "/text" is named twice`),
				rcRefuse(n+"a duplicate written with an escape", op, `{"text":"a","\u0074ext":"b"}`, `member "/text" is named twice`),
				rcRefuse(n+"an escaped duplicate first", op, `{"\u0074ext":"a","text":"b"}`, `member "/text" is named twice`),
				rcPass(n+"an undeclared member is removed", op, `{"text":"a","extra":1,"more":{"x":[1]}}`, `{"text":"a"}`),
				rcPass(n+"an undeclared member before the declared one", op, `{"extra":1,"text":"a"}`, `{"text":"a"}`),
				rcCase{name: n + "only declared members pass byte for byte", op: op, body: `{ "text" : "a" }`, status: 501, want: `{ "text" : "a" }`, exact: true},
				rcCase{name: n + "a declared name written with an escape passes byte for byte", op: op,
					body: `{"\u0074ext":"a"}`, status: 501, want: `{"\u0074ext":"a"}`, exact: true},
				rcPass(n+"an escaped declared name beside a removed member is written with its exact name", op,
					`{"\u0074ext":"a","y":1}`, `{"text":"a"}`),
				rcPass(n+"an undeclared name written with an escape is removed", op, `{"text":"a","\u0078":1}`, `{"text":"a"}`),
			)
		}
	}
	return cs
}

func rcRegressBooleanCases() []rcCase {
	var cs []rcCase
	for _, p := range []string{"/closed-capital", "/closed-upper", "/closed-lower"} {
		op := "POST " + p
		cs = append(cs,
			rcRefuse(p+": an undeclared member is refused", op, `{"text":"a","x":1}`, `member "/x" is not declared`),
			rcRefuse(p+": an undeclared member first", op, `{"x":1,"text":"a"}`, `member "/x" is not declared`),
			rcRefuse(p+": required is judged before undeclared", op, `{"x":1}`, `member "/text" is required`),
			rcCase{name: p + ": declared only passes", op: op, body: `{"text":"a"}`, status: 501, want: `{"text":"a"}`, exact: true},
		)
	}
	// additionalProperties: True leaves the object open: an undeclared member is removed, not refused
	cs = append(cs, rcPass("/open-capital: True does not close", "POST /open-capital", `{"text":"a","x":1}`, `{"text":"a"}`))
	for _, p := range []string{"/needed-capital", "/needed-upper"} {
		op := "POST " + p
		cs = append(cs,
			rcRefuse(p+": an empty body is refused", op, ``, "the request body is missing"),
			rcRefuse(p+": white space only is refused", op, " \n\t ", "the request body is missing"),
			rcPass(p+": a body passes", op, `{"text":"a"}`, ""),
		)
	}
	cs = append(cs,
		rcCase{name: "/needed-capital: no body at all", op: "POST /needed-capital", nobody: true, status: 400, msg: "the request body is missing"},
		rcCase{name: "/optional-capital: required False lets an empty body through", op: "POST /optional-capital", body: ``, status: 501, want: ``, exact: true},
		rcRefuse("/optional-capital: a body is still judged", "POST /optional-capital", `{}`, `member "/text" is required`),
	)
	return cs
}

func TestRequestRegressMethodsAndEscapesServed(t *testing.T) {
	servers := rcServe(t, "regress.yaml", rcRegressMethods)
	rcRunCases(t, servers, rcRegressCases())
}

func TestRequestRegressYAMLBooleansServed(t *testing.T) {
	servers := rcServe(t, "regress.yaml", rcRegressMethods)
	rcRunCases(t, servers, rcRegressBooleanCases())
}

// A 1 MiB array of {} against an array of objects of 300 declared optional properties is answered
// within 2 seconds of wall time, whatever the status (F4: the member lookup was rebuilt per object).
func TestRequestRegressLargeArrayOfEmptyObjectsIsAnsweredQuickly(t *testing.T) {
	const n = 349000
	body := "[" + strings.TrimSuffix(strings.Repeat("{},", n), ",") + "]"
	if len(body) >= rcLimit || len(body) < rcLimit*9/10 {
		t.Fatalf("the body is %d bytes, want just under %d", len(body), rcLimit)
	}
	servers := rcServe(t, "regress.yaml", rcRegressMethods)
	for _, s := range servers {
		t.Run(s.name, func(t *testing.T) {
			// measured around the request only: rcDo times the call itself
			r := rcDo(t, s, "POST "+"/bulk", "", []byte(body))
			if r.took > 2*time.Second {
				t.Errorf("answered after %v, want within 2s (status %d)", r.took, r.status)
			}
			if r.status != 200 && r.status != 501 {
				t.Errorf("status %d (%.200s), want the request to pass (an array of empty objects is valid)", r.status, r.body)
			}
			if s.recording && r.seen != strconv.Itoa(len(r.body)) {
				t.Errorf("the handler saw Content-Length %q for a body of %d bytes", r.seen, len(r.body))
			}
		})
	}
}
