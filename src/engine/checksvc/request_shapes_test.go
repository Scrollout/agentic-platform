// T005 (feature 010): failing tests, by a separate author, for runCheck serving the check
// RequestShapesGenerated. It judges gen/request_shapes.go against the text inside the `contract`
// entry of `approved` (its content is the canonical JSON {"contract": "<text>"}), never
// api/openapi.yaml in the project folder (check contract, runCheck; catalogue README, "Request
// check"). The check runs no Go. The exact rendering of a passing file is T007's and the
// generator's, so these tests judge only what the contract fixes: the name is served, a missing
// or differing file fails naming gen/request_shapes.go, and which contract text is judged.
// Over HTTP only, requests as plain JSON maps.
package checksvc_test

import (
	"strings"
	"testing"
)

// rqContract is a contract with one request body whose only member is `member`.
func rqContract(member string) string {
	return "openapi: 3.0.3\ninfo:\n  title: Notes\n  version: \"1\"\npaths:\n  /notes:\n    post:\n" +
		"      operationId: createNote\n      requestBody:\n        required: true\n        content:\n" +
		"          application/json:\n            schema:\n              type: object\n" +
		"              properties:\n                " + member + ":\n                  type: string\n" +
		"      responses:\n        \"201\":\n          description: created\n"
}

var rqGenStep = step("generate", []string{"gen/**"}, []string{"generated-code"}, nil, nil)

const rqCheck = "RequestShapesGenerated"

// rqBody is a runCheck body whose approved contract is the text `contract`.
func rqBody(t testing.TB, dir, contract string) map[string]any {
	t.Helper()
	return checkBody(t, dir, rqGenStep, nil, contract, approvedExamples(t))
}

// rqRawContractPart replaces the approved `contract` entry by one whose content is raw bytes,
// with their correct sha256 (so only the content is wrong).
func rqRawContractPart(body map[string]any, raw string) {
	for _, a := range body["approved"].([]any) {
		if p := a.(map[string]any); p["part"] == "contract" {
			p["content"], p["sha256"] = b64([]byte(raw)), sum([]byte(raw))
		}
	}
}

// rqWantFailNaming runs the check and wants a failed verdict whose reason holds every text.
func rqWantFailNaming(t *testing.T, base string, body map[string]any, what string, texts ...string) {
	t.Helper()
	r := post(t, base, "/checks/"+rqCheck+"/run", body)
	if r.status == 404 {
		t.Fatalf("%s: %s must be served, not 404 UnknownCheck: %.300s", what, rqCheck, r.body)
	}
	v := wantVerdict(t, r, rqCheck)
	if v.Passed {
		t.Fatalf("%s: must fail: %+v", what, v)
	}
	for _, s := range texts {
		if !strings.Contains(v.Reason, s) {
			t.Fatalf("%s: the reason must name %q: %+v", what, s, v)
		}
	}
}

func TestRequestShapesGeneratedIsServed(t *testing.T) {
	root, dir := fixture(t)
	write(t, dir, "gen/other.go", []byte("package gen\n"))
	base := serve(t, checkConfig(t, root))
	r := post(t, base, "/checks/"+rqCheck+"/run", rqBody(t, dir, rqContract("text")))
	if r.status == 404 {
		t.Fatalf("%s must be served, not 404 UnknownCheck: %.300s", rqCheck, r.body)
	}
	v := wantVerdict(t, r, rqCheck)
	if v.Passed || !strings.Contains(v.Reason, "gen/request_shapes.go") {
		t.Fatalf("a folder without gen/request_shapes.go must fail, naming it: %+v", v)
	}
}

func TestRequestShapesGeneratedFailsNamingADifferingFile(t *testing.T) {
	root, dir := fixture(t)
	write(t, dir, "gen/request_shapes.go", []byte("// not the rendering\npackage gen\n"))
	base := serve(t, checkConfig(t, root))
	rqWantFailNaming(t, base, rqBody(t, dir, rqContract("text")), "a differing file", "gen/request_shapes.go")
}

// The folder's api/openapi.yaml is not consulted: a folder contract that cannot be read does not
// change the verdict about a readable approved contract (a missing-file failure, not a contract
// failure), and a readable folder contract does not rescue an approved contract that cannot be read.
func TestRequestShapesGeneratedJudgesTheApprovedContract(t *testing.T) {
	root, dir := fixture(t)
	base := serve(t, checkConfig(t, root))
	write(t, dir, "gen/other.go", []byte("package gen\n"))

	// Folder copy unreadable, approved contract readable: the verdict is about the missing file.
	write(t, dir, "api/openapi.yaml", []byte("a: 1\na: 2\n"))
	rqWantFailNaming(t, base, rqBody(t, dir, rqContract("text")), "unreadable folder copy", "gen/request_shapes.go")

	// Folder copy readable, approved contract not (duplicate keys): fails closed naming the contract.
	write(t, dir, "api/openapi.yaml", []byte(rqContract("text")))
	body := rqBody(t, dir, "a: 1\na: 2\n")
	r := post(t, base, "/checks/"+rqCheck+"/run", body)
	if r.status == 404 {
		t.Fatalf("%s must be served: %.300s", rqCheck, r.body)
	}
	v := wantVerdict(t, r, rqCheck)
	if v.Passed || !strings.Contains(strings.ToLower(v.Reason), "contract") {
		t.Fatalf("an approved contract with a duplicate key must fail closed naming the contract: %+v", v)
	}
}

// A different request body in the folder copy does not change the verdict: whichever file is
// written, the reason is the same for both folder copies.
func TestRequestShapesGeneratedIgnoresAFolderContractWithAnotherBody(t *testing.T) {
	root, dir := fixture(t)
	base := serve(t, checkConfig(t, root))
	write(t, dir, "gen/request_shapes.go", []byte("package gen\n"))
	write(t, dir, "api/openapi.yaml", []byte(rqContract("text")))
	a := runCheck(t, base, rqCheck, rqBody(t, dir, rqContract("text")))
	write(t, dir, "api/openapi.yaml", []byte(rqContract("other")))
	b := runCheck(t, base, rqCheck, rqBody(t, dir, rqContract("text")))
	if a.Passed != b.Passed || a.Reason != b.Reason {
		t.Fatalf("the folder's api/openapi.yaml must not change the verdict:\n%+v\n%+v", a, b)
	}
	if a.Passed || !strings.Contains(a.Reason, "gen/request_shapes.go") {
		t.Fatalf("a non-rendering file must fail naming gen/request_shapes.go: %+v", a)
	}
}

// A request whose `contract` entry has the right hash but not the content {"contract": "<text>"}
// fails closed, naming the contract; the check does not fall back to the folder's copy.
func TestRequestShapesGeneratedFailsClosedOnUnreadableApprovedContract(t *testing.T) {
	root, dir := fixture(t)
	write(t, dir, "api/openapi.yaml", []byte(rqContract("text")))
	write(t, dir, "gen/other.go", []byte("package gen\n"))
	base := serve(t, checkConfig(t, root))
	good := rqContract("text")
	bad := map[string]string{
		"a JSON string":              `"` + strings.ReplaceAll(good, "\n", `\n`) + `"`,
		"an object without contract": `{"text":"x"}`,
		"an empty object":            `{}`,
		"contract a number":          `{"contract":200}`,
		"contract null":              `{"contract":null}`,
		"not JSON":                   good,
		"contract not one document":  `{"contract":"a: 1\na: 2\n"}`,
		"contract empty":             `{"contract":""}`,
	}
	for name, raw := range bad {
		body := rqBody(t, dir, good)
		rqRawContractPart(body, raw)
		r := post(t, base, "/checks/"+rqCheck+"/run", body)
		if r.status == 404 {
			t.Fatalf("%s must be served: %.300s", rqCheck, r.body)
		}
		if r.status != 200 {
			if r.status < 400 {
				t.Errorf("approved contract %s: status %d is neither a verdict nor an error", name, r.status)
			}
			continue
		}
		v := wantVerdict(t, r, rqCheck)
		if v.Passed || !strings.Contains(strings.ToLower(v.Reason), "contract") {
			t.Errorf("approved contract %s: must fail closed naming the contract: %+v", name, v)
		}
	}
}

// A request with no `contract` entry in `approved` fails closed, naming the contract.
func TestRequestShapesGeneratedFailsClosedWithoutTheApprovedContract(t *testing.T) {
	root, dir := fixture(t)
	write(t, dir, "api/openapi.yaml", []byte(rqContract("text")))
	write(t, dir, "gen/other.go", []byte("package gen\n"))
	base := serve(t, checkConfig(t, root))
	// Control: the same request with its contract reaches the check (a verdict, not 404).
	if c := post(t, base, "/checks/"+rqCheck+"/run", rqBody(t, dir, rqContract("text"))); c.status != 200 {
		t.Fatalf("control: %s with its approved contract must answer a verdict, got %d: %.300s", rqCheck, c.status, c.body)
	}
	body := rqBody(t, dir, rqContract("text"))
	kept := []any{}
	for _, a := range body["approved"].([]any) {
		if a.(map[string]any)["part"] != "contract" {
			kept = append(kept, a)
		}
	}
	body["approved"] = kept
	r := post(t, base, "/checks/"+rqCheck+"/run", body)
	if r.status == 404 {
		t.Fatalf("%s must be served: %.300s", rqCheck, r.body)
	}
	if r.status != 200 {
		if r.status < 400 {
			t.Fatalf("status %d is neither a verdict nor an error", r.status)
		}
		return
	}
	v := wantVerdict(t, r, rqCheck)
	if v.Passed || !strings.Contains(strings.ToLower(v.Reason), "contract") {
		t.Fatalf("no approved contract must fail closed naming the contract: %+v", v)
	}
}
