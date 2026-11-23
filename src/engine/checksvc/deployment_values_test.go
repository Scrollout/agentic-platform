// T009 (feature 009): a failing test, by a separate author, for runCheck serving
// DeploymentValuesConfigured (check contract, runCheck; catalogue README, "Deployment values";
// declared-values contract, "Check DeploymentValuesConfigured"). The check judges every .go file
// the step's `writable` matches, except _test.go files, and runs no Go, so no toolchain is needed.
// Over HTTP only, requests as plain JSON maps. Helpers here are prefixed dvc so they clash with no
// other test file.
package checksvc_test

import (
	"strings"
	"testing"
)

const dvcContract = "openapi: 3.0.3\ninfo:\n  title: Notes\n  version: \"1\"\npaths: {}\n"

var dvcCodeStep = step("code", []string{"internal/**"}, []string{"code"}, nil, nil)

const dvcCheck = "DeploymentValuesConfigured"

func dvcRun(t *testing.T, base, dir string, st map[string]any) verdict {
	t.Helper()
	r := post(t, base, "/checks/"+dvcCheck+"/run", checkBody(t, dir, st, nil, dvcContract, approvedExamples(t)))
	if r.status == 404 {
		t.Fatalf("%s must be served, not 404 UnknownCheck: %.300s", dvcCheck, r.body)
	}
	return wantVerdict(t, r, dvcCheck)
}

// A clean project passes with the contract's pass text, which counts the judged files: a _test.go
// file holding an address is not judged, and a file outside `writable` is not judged either.
func TestDeploymentValuesConfiguredIsServedAndPassesACleanProject(t *testing.T) {
	root, dir := fixture(t)
	write(t, dir, "internal/note.go", []byte("package internal\n\nfunc Count(n []string) int { return len(n) }\n"))
	write(t, dir, "internal/note_test.go", []byte("package internal\n\nvar testAddress = \":8080\"\n"))
	write(t, dir, "cmd/main.go", []byte("package main\n\nvar address = \":9090\"\n"))
	base := serve(t, checkConfig(t, root))
	v := dvcRun(t, base, dir, dvcCodeStep)
	if !v.Passed || v.Reason != "examined 1 files: no address, credential or environment read" {
		t.Fatalf("a clean project must pass with %q: %+v", "examined 1 files: no address, credential or environment read", v)
	}
}

// Each offence fails once, naming file:line and the kind in the contract's words, saying it
// belongs in configuration, and never quoting the literal (a URL can carry a password).
func TestDeploymentValuesConfiguredFailsPerOffenceWithoutTheLiteral(t *testing.T) {
	root, dir := fixture(t)
	write(t, dir, "internal/server.go", []byte(`package internal

import (
	"net/http"
	"os"
)

var listen = ":8080"

var upstream = "http://svcuser:pw123secret@notes.local/api"

func home() string { return os.Getenv("HOME") }

func call(r *http.Request) { r.SetBasicAuth("svc", "hunter2value") }
`))
	base := serve(t, checkConfig(t, root))
	v := dvcRun(t, base, dir, dvcCodeStep)
	if v.Passed {
		t.Fatalf("a project holding an address, a credential and an environment read must fail: %+v", v)
	}
	tail := ": belongs in configuration: read it through gen/config.go"
	for _, want := range []string{
		"internal/server.go:8 holds an address" + tail,
		"internal/server.go:10 holds an address" + tail,
		"internal/server.go:12 holds an environment read" + tail,
		"internal/server.go:14 holds a credential" + tail,
	} {
		if !strings.Contains(v.Reason, want) {
			t.Errorf("the reason must contain %q: %s", want, v.Reason)
		}
	}
	for _, secret := range []string{":8080", "pw123secret", "svcuser", "notes.local", "hunter2value"} {
		if strings.Contains(v.Reason, secret) {
			t.Errorf("the reason must never quote a literal, but it holds %q: %s", secret, v.Reason)
		}
	}
	noHost(t, v.Reason)
}

// An address in a _test.go file is not judged, even next to a judged file that offends.
func TestDeploymentValuesConfiguredSkipsTestFilesOnly(t *testing.T) {
	root, dir := fixture(t)
	write(t, dir, "internal/a_test.go", []byte("package internal\n\nvar a = \":8080\"\n"))
	write(t, dir, "internal/b.go", []byte("package internal\n\nvar b = \"localhost:9000\"\n"))
	base := serve(t, checkConfig(t, root))
	v := dvcRun(t, base, dir, dvcCodeStep)
	if v.Passed || !strings.Contains(v.Reason, "internal/b.go:3 holds an address") {
		t.Fatalf("the non-test file must fail naming internal/b.go:3: %+v", v)
	}
	if strings.Contains(v.Reason, "a_test.go") {
		t.Fatalf("a _test.go file is not judged: %s", v.Reason)
	}
}

// A `writable` that matches no .go file judges none: it passes and says it examined 0 files.
func TestDeploymentValuesConfiguredWritableMatchingNothingPasses(t *testing.T) {
	root, dir := fixture(t)
	write(t, dir, "internal/server.go", []byte("package internal\n\nvar listen = \":8080\"\n"))
	base := serve(t, checkConfig(t, root))
	st := step("code", []string{"nothing/**"}, []string{"code"}, nil, nil)
	v := dvcRun(t, base, dir, st)
	if !v.Passed || v.Reason != "examined 0 files: no address, credential or environment read" {
		t.Fatalf("no judged file must pass with the 0-file pass text: %+v", v)
	}
}

// A judged file that does not parse as Go fails closed, naming the file.
func TestDeploymentValuesConfiguredFailsClosedOnAFileThatDoesNotParse(t *testing.T) {
	root, dir := fixture(t)
	write(t, dir, "internal/broken.go", []byte("package internal\n\nfunc (\n"))
	base := serve(t, checkConfig(t, root))
	v := dvcRun(t, base, dir, dvcCodeStep)
	if v.Passed || !strings.Contains(v.Reason, "internal/broken.go") {
		t.Fatalf("a judged file that does not parse must fail, naming it: %+v", v)
	}
}
