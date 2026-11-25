package steps

// MalformedRequestsRefused (README "Malformed requests", feature 010 FR-010, research R6): the
// code agent's service, judged on request bodies the contract does not allow.

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// GRefused is the malformed-request grader.
const GRefused = "MalformedRequestsRefused"

const (
	requestsSrc = "testdata/requests/requests_test.go" // in the embedded test data; Go ignores testdata/
	requestsPkg = "malformedrequests"                  // the folder the scratch copy gets it in
	statusBits  = 10                                   // a status below 1024 is read bit by bit
)

// sentRequest is one request body sent to POST /notes and the answer it must get: status, and for
// 201 the note's text.
type sentRequest struct {
	Body   string
	Status int
	Text   string
}

// sentRequests are the grader's three requests, in the order they are sent and named.
var sentRequests = []sentRequest{
	{Body: `{"TEXT":"x"}`, Status: 400},                       // a member name in another case
	{Body: `{"text":"a","TEXT":"b"}`, Status: 201, Text: "a"}, // the declared name and an undeclared one
	{Body: `{"text":"a","text":"b"}`, Status: 400},            // a name twice (Go's decoder keeps the last)
}

// MalformedRequestsRefused sends each of sentRequests to POST /notes of the service built from
// project, a fresh gen.Handler(app.New()) per request, and passes only when every one gets its
// answer. A pass names the three bodies as sent; a failure names each request that got a wrong
// answer, by its body, with what it got. The requests are a Go test package added to a scratch
// copy of project (the project itself is never written), built and run as the held-out tests are
// (VariantRun). It fails with a reason, and no request named, when the project is not there, the
// code does not build, or the project already holds the folder the requests go in.
//
// The test package answers yes or no questions only (one top-level test each), so the status is
// read bit by bit; a status of 1024 or more reads as its lowest ten bits.
func (e Env) MalformedRequestsRefused(project string) Result {
	if project == "" {
		return fail(GRefused, "no project folder was given")
	}
	if _, err := os.Lstat(filepath.Join(project, requestsPkg)); err == nil {
		return fail(GRefused, "the project already holds %s/, where the grader's requests go", requestsPkg)
	}
	src, err := data.ReadFile(requestsSrc)
	if err != nil {
		return fail(GRefused, "the requests cannot be read: %v", err)
	}
	gen, names := requestTests()
	run := VariantRun{Runner: e.variantRunner(), Pkg: requestsPkg, Expected: names, Add: map[string]string{
		requestsPkg + "/requests_test.go": string(src),
		requestsPkg + "/sent_test.go":     gen,
	}}
	res, err := run.Variants(project, []Variant{{Name: "requests"}})
	if err != nil {
		return fail(GRefused, "the requests cannot be set up: %v", err)
	}
	if b := res[0].BuildError; b != "" {
		return fail(GRefused, "the code does not build with the grader's requests: %s", firstLines(b, 10))
	}
	tests := map[string]Outcome{}
	for _, b := range res[0].Binaries {
		if b.Dir == requestsPkg {
			tests = b.Tests
		}
	}
	var wrong, sent []string
	for i, q := range sentRequests {
		sent = append(sent, q.Body)
		if got := judgeAnswer(i, q, tests); got != "" {
			wrong = append(wrong, fmt.Sprintf("%s got %s", q.Body, got))
		}
	}
	if len(wrong) > 0 {
		return fail(GRefused, "POST /notes answered wrongly: %s", strings.Join(wrong, "; "))
	}
	return Result{Grader: GRefused, Passed: true, Reason: "POST /notes refused " + sent[0] + " and " + sent[2] +
		" with 400 and created the note \"a\" from " + sent[1] + " with 201"}
}

// judgeAnswer returns what request i got when that is not what q wants, or "" when it is.
func judgeAnswer(i int, q sentRequest, tests map[string]Outcome) string {
	yes := func(what string) (bool, string) {
		switch o := tests[requestTest(i, what)]; o {
		case OutcomePassed:
			return true, ""
		case OutcomeFailed:
			return false, ""
		case OutcomeTimedOut:
			return false, "no answer (timed out)"
		default:
			return false, "no answer (" + string(OutcomeDidNotRun) + ")"
		}
	}
	answered, why := yes("answered")
	if why != "" {
		return why
	}
	if !answered {
		return "no answer"
	}
	status := 0
	for n := range statusBits {
		set, why := yes("bit" + strconv.Itoa(n))
		if why != "" {
			return why
		}
		if set {
			status |= 1 << n
		}
	}
	if status != q.Status {
		return strconv.Itoa(status)
	}
	if q.Status != 201 {
		return ""
	}
	object, why := yes("object")
	if why != "" {
		return why
	}
	if !object {
		return "201 with a body that is not a JSON object"
	}
	text, why := yes("text")
	if why != "" {
		return why
	}
	if !text {
		return "201 with a note whose text is not " + strconv.Quote(q.Text)
	}
	return ""
}

// requestTests is the generated test file that declares the requests and one top-level test per
// question about each answer, and the tests' names.
func requestTests() (string, []string) {
	var b strings.Builder
	b.WriteString("package " + requestsPkg + "_test\n\nimport \"testing\"\n\nvar requestBodies = []string{")
	for _, q := range sentRequests {
		b.WriteString(strconv.Quote(q.Body) + ", ")
	}
	b.WriteString("}\n\nvar wantText = []string{")
	for _, q := range sentRequests {
		b.WriteString(strconv.Quote(q.Text) + ", ")
	}
	b.WriteString("}\n")
	var names []string
	for i := range sentRequests {
		whats := []string{"answered"}
		for n := range statusBits {
			whats = append(whats, "bit"+strconv.Itoa(n))
		}
		whats = append(whats, "object", "text")
		for _, w := range whats {
			n := requestTest(i, w)
			names = append(names, n)
			fmt.Fprintf(&b, "\nfunc %s(t *testing.T) { check(t, %d, %q) }\n", n, i, w)
		}
	}
	return b.String(), names
}

// requestTest is the name of the test that asks question what about the answer to request i.
func requestTest(i int, what string) string { return fmt.Sprintf("TestRequest%d_%s", i, what) }
