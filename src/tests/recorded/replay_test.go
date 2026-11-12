//go:build docker

package recorded

// The replay server's test suite (T020), written from the README's promises. It drives
// replay.sh and the WireMock container it starts. It needs Docker and the pinned WireMock image;
// no model key, no egress. A missing Docker is a failure, never a skip (no-skip policy). It is
// behind the `docker` build tag because the containers that run `go test ./...` have no Docker;
// scripts/check.sh builds it in the pinned Go container and runs it on the host.

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var seq atomic.Int64

func uniq(prefix string) string {
	return fmt.Sprintf("%s-%d-%d", prefix, os.Getpid(), seq.Add(1))
}

func run(name string, args ...string) (string, int) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		code = -1
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
	}
	return strings.TrimSpace(string(out)), code
}

func docker(t *testing.T, args ...string) string {
	t.Helper()
	out, code := run("docker", args...)
	if code != 0 {
		t.Fatalf("docker %s: exit %d: %s", strings.Join(args, " "), code, out)
	}
	return out
}

func needDocker(t *testing.T) {
	t.Helper()
	if out, code := run("docker", "info", "--format", "{{.ServerVersion}}"); code != 0 {
		t.Fatalf("Docker is required and is not available (no skip): %s", out)
	}
}

func image(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../deploy/images.lock")
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range strings.Split(string(b), "\n") {
		if f := strings.Fields(l); len(f) >= 2 && f[0] == "wiremock" {
			return f[1]
		}
	}
	t.Fatal("no wiremock pin in images.lock")
	return ""
}

func network(t *testing.T, internal bool) string {
	t.Helper()
	needDocker(t)
	n := uniq("aptest-net")
	args := []string{"network", "create"}
	if internal {
		args = append(args, "--internal", "-o", "com.docker.network.bridge.gateway_mode_ipv4=isolated")
	}
	docker(t, append(args, n)...)
	t.Cleanup(func() { run("docker", "network", "rm", n) })
	return n
}

func replaySh(env []string, args ...string) (string, int) {
	cmd := exec.Command("sh", append([]string{"replay.sh"}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		code = -1
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
	}
	return strings.TrimSpace(string(out)), code
}

// recordings builds a recordings folder from name -> mapping JSON (body file = name.sse).
func recordings(t *testing.T, mappings map[string]string, files map[string]string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "aptest-rec")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	for _, d := range []string{"", "mappings", "__files"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
		os.Chmod(filepath.Join(dir, d), 0o755)
	}
	for n, c := range mappings {
		must(t, os.WriteFile(filepath.Join(dir, "mappings", n+".json"), []byte(c), 0o644))
	}
	for n, c := range files {
		must(t, os.WriteFile(filepath.Join(dir, "__files", n), []byte(c), 0o644))
	}
	return dir
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

const answer1 = "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

var testMappings = map[string]string{
	"messages": `{"request":{"method":"POST","url":"/v1/messages?beta=true",
	  "bodyPatterns":[{"equalToJson":{"model":"m","n":1,"list":[1,2],"nested":{"a":"x","b":null}}}]},
	  "response":{"status":200,"headers":{"Content-Type":"text/event-stream; charset=utf-8"},"bodyFileName":"messages.sse"}}`,
	"count": `{"request":{"method":"POST","url":"/v1/messages/count_tokens",
	  "bodyPatterns":[{"equalToJson":{"q":1}}]},
	  "response":{"status":200,"headers":{"Content-Type":"application/json"},"body":"{\"input_tokens\":7}"}}`,
}

type lab struct {
	net, name, img string
}

// start launches the replay server on a fresh internal network with the given recordings
// (nil: the repository's own).
func start(t *testing.T, rec string) lab {
	t.Helper()
	img := image(t)
	n := network(t, true)
	name := uniq("aptest-replay")
	var env []string
	if rec != "" {
		env = []string{"REPLAY_RECORDINGS=" + rec}
	}
	t.Cleanup(func() { run("docker", "rm", "-f", name) })
	out, code := replaySh(env, "start", n, name)
	if code != 0 || out != name {
		t.Fatalf("replay.sh start: exit %d: %s", code, out)
	}
	t.Cleanup(func() { replaySh(nil, "stop", name) })
	return lab{n, name, img}
}

var statusRe = regexp.MustCompile(`HTTP/1\.[01] (\d{3})`)

// post sends a POST from inside the replay container and returns status and body.
func (l lab) post(url, body string, headers ...string) (int, string) {
	args := []string{"exec", l.name, "wget", "-q", "-O", "-", "--post-data", body}
	if len(headers) == 0 || !strings.HasPrefix(strings.ToLower(headers[0]), "content-type") {
		// Like the agent: without a JSON Content-Type Jetty parses a large body as a form and fails.
		headers = append([]string{"Content-Type: application/json"}, headers...)
	}
	for _, h := range headers {
		args = append(args, "--header", h)
	}
	return wget(append(args, "http://127.0.0.1:8080"+url))
}

func (l lab) get(url string) (int, string) {
	return wget([]string{"exec", l.name, "wget", "-q", "-O", "-", "http://127.0.0.1:8080" + url})
}

func wget(args []string) (int, string) {
	out, code := run("docker", args...)
	if code == 0 {
		return 200, out
	}
	if m := statusRe.FindStringSubmatch(out); m != nil {
		var s int
		fmt.Sscan(m[1], &s)
		return s, out
	}
	return -1, out
}

// fromAgent sends a request from a separate container on the same network, as the agent would.
func (l lab) fromAgent(method, url, body string) (int, string) {
	args := []string{"run", "--rm", "--network", l.net, "--entrypoint", "wget", l.img, "-q", "-O", "-"}
	if method == "POST" {
		args = append(args, "--post-data", body)
	}
	return wget(append(args, "http://gateway:8080"+url))
}

func (l lab) credential(t *testing.T) string {
	t.Helper()
	env := docker(t, "inspect", "-f", `{{range .Config.Env}}{{println .}}{{end}}`, l.name)
	for _, e := range strings.Split(env, "\n") {
		if v, ok := strings.CutPrefix(e, "WIREMOCK_OPTIONS=--admin-api-basic-auth "); ok {
			return v
		}
	}
	t.Fatal("no admin credential in the container environment")
	return ""
}

const body1 = `{"model":"m","n":1,"list":[1,2],"nested":{"a":"x","b":null}}`

func TestMatchingRequestGetsRecordedAnswer(t *testing.T) {
	l := start(t, recordings(t, testMappings, map[string]string{"messages.sse": answer1}))
	st, out := l.post("/v1/messages?beta=true", body1)
	if st != 200 || out != strings.TrimSpace(answer1) {
		t.Fatalf("status %d, body %q; want 200 and the recorded stream", st, out)
	}
	if st, out := l.post("/v1/messages/count_tokens", `{"q":1}`); st != 200 || !strings.Contains(out, `"input_tokens":7`) {
		t.Errorf("a recorded count_tokens: status %d, %q", st, out)
	}
}

func TestRecordedContentTypeIsServed(t *testing.T) {
	l := start(t, recordings(t, testMappings, map[string]string{"messages.sse": answer1}))
	out := docker(t, "exec", l.name, "wget", "-S", "-q", "-O", "/dev/null", "--post-data", body1,
		"http://127.0.0.1:8080/v1/messages?beta=true")
	if !strings.Contains(strings.ToLower(out), "content-type: text/event-stream; charset=utf-8") {
		t.Errorf("response headers lack the recorded Content-Type:\n%s", out)
	}
}

func TestBodyMatchedAsJson(t *testing.T) {
	l := start(t, recordings(t, testMappings, map[string]string{"messages.sse": answer1}))
	u := "/v1/messages?beta=true"
	for name, b := range map[string]string{
		"key order and whitespace": "{ \"nested\" : { \"b\" : null , \"a\":\"x\" },\n \"list\":[ 1, 2 ],\t\"n\":1, \"model\" : \"m\" }",
		"number form":              `{"model":"m","n":1.0,"list":[1,2],"nested":{"a":"x","b":null}}`,
	} {
		if st, _ := l.post(u, b); st != 200 {
			t.Errorf("%s: status %d, want 200", name, st)
		}
	}
	for name, b := range map[string]string{
		"one field value differs": `{"model":"m","n":2,"list":[1,2],"nested":{"a":"x","b":null}}`,
		"nested value differs":    `{"model":"m","n":1,"list":[1,2],"nested":{"a":"y","b":null}}`,
		"array order differs":     `{"model":"m","n":1,"list":[2,1],"nested":{"a":"x","b":null}}`,
		"extra field":             `{"model":"m","n":1,"list":[1,2],"nested":{"a":"x","b":null},"extra":true}`,
		"missing field":           `{"model":"m","n":1,"list":[1,2]}`,
		"empty body":              ``,
		"not JSON":                `model=m`,
		"string case differs":     `{"model":"M","n":1,"list":[1,2],"nested":{"a":"x","b":null}}`,
	} {
		if st, _ := l.post(u, b); st != 404 {
			t.Errorf("%s: status %d, want 404", name, st)
		}
	}
}

func TestNoHeaderIsMatched(t *testing.T) {
	l := start(t, recordings(t, testMappings, map[string]string{"messages.sse": answer1}))
	u := "/v1/messages?beta=true"
	for name, h := range map[string][]string{
		"no key, odd content type": {"Content-Type: application/json; charset=latin1", "X-Api-Key: placeholder", "Anthropic-Version: 1999-01-01"},
		"user agent and beta":      {"User-Agent: other/9", "Anthropic-Beta: anything", "Authorization: Bearer zzz"},
		"none beyond defaults":     nil,
	} {
		if st, _ := l.post(u, body1, h...); st != 200 {
			t.Errorf("%s: status %d, want 200", name, st)
		}
	}
}

func TestMethodPathAndQueryMustEqual(t *testing.T) {
	l := start(t, recordings(t, testMappings, map[string]string{"messages.sse": answer1}))
	for name, u := range map[string]string{
		"missing query":    "/v1/messages",
		"other query":      "/v1/messages?beta=false",
		"other path":       "/v1/other?beta=true",
		"trailing slash":   "/v1/messages/?beta=true",
		"root":             "/",
		"chat completions": "/v1/chat/completions",
	} {
		if st, _ := l.post(u, body1); st != 404 {
			t.Errorf("%s: POST %s: status %d, want 404", name, u, st)
		}
	}
	if st, _ := l.get("/v1/messages?beta=true"); st != 404 {
		t.Errorf("GET of a POST recording: status %d, want 404", st)
	}
}

// The README says the query must be equal; the recorder's matcher constrains only the listed
// parameters, so an extra one is still answered. A known gap, reported to the owner.
func TestExtraQueryParameterMisses(t *testing.T) {
	l := start(t, recordings(t, testMappings, map[string]string{"messages.sse": answer1}))
	if st, _ := l.post("/v1/messages?beta=true&x=1", body1); st != 404 {
		t.Errorf("an extra query parameter was answered (status %d); README: the query must be equal", st)
	}
}

func TestCountTokensWithoutRecordingAndOtherPaths404(t *testing.T) {
	// The repository's own recordings hold no count_tokens exchange.
	l := start(t, "")
	for _, u := range []string{"/v1/messages/count_tokens", "/v1/messages/count_tokens?beta=true", "/v1/models", "/anything", "/v1/messages/batches"} {
		if st, _ := l.post(u, `{}`); st != 404 {
			t.Errorf("POST %s: status %d, want 404", u, st)
		}
	}
	if st, _ := l.get("/v1/models"); st != 404 {
		t.Errorf("GET /v1/models: status %d, want 404", st)
	}
}

func TestRepositoryRecordingAnswersItsOwnRequest(t *testing.T) {
	// The recording's matcher body, replayed as sent, gets the recorded stream.
	raw, err := os.ReadFile("recordings/mappings/analyst-example-notes.json")
	must(t, err)
	var m struct {
		Request struct {
			BodyPatterns []struct {
				Body json.RawMessage `json:"equalToJson"`
			} `json:"bodyPatterns"`
		} `json:"request"`
	}
	must(t, json.Unmarshal(raw, &m))
	if len(m.Request.BodyPatterns) != 1 {
		t.Fatal("expected one body pattern")
	}
	l := start(t, "")
	body := string(m.Request.BodyPatterns[0].Body)
	st, out := l.post("/v1/messages?beta=true", body)
	if st != 200 || !strings.Contains(out, "__structured_output__") {
		t.Fatalf("status %d: %.200s", st, out)
	}
	// One changed field misses.
	if st, _ := l.post("/v1/messages?beta=true", strings.Replace(body, "claude-haiku-5-5", "claude-haiku-5-6", 1)); st != 404 {
		t.Errorf("changed model: status %d, want 404", st)
	}
}

func TestMissesListsUnmatchedRequestsWithBodies(t *testing.T) {
	l := start(t, recordings(t, testMappings, map[string]string{"messages.sse": answer1}))
	out, code := replaySh(nil, "misses", l.name)
	if code != 0 {
		t.Fatalf("misses: exit %d: %s", code, out)
	}
	if n := countMisses(t, out); n != 0 {
		t.Errorf("fresh server lists %d misses", n)
	}
	l.post("/v1/messages?beta=true", body1) // a match: not a miss
	l.post("/v1/messages?beta=true", `{"model":"m","n":999,"marker":"MISS-ONE"}`)
	l.post("/v1/messages/count_tokens", `{"marker":"MISS-TWO"}`)
	l.post("/v1/unknown", `{"marker":"MISS-THREE"}`)
	out, code = replaySh(nil, "misses", l.name)
	if code != 0 {
		t.Fatalf("misses: exit %d: %s", code, out)
	}
	if n := countMisses(t, out); n != 3 {
		t.Errorf("misses lists %d requests, want 3:\n%s", n, out)
	}
	for _, s := range []string{"MISS-ONE", "MISS-TWO", "MISS-THREE", "/v1/unknown", "/v1/messages/count_tokens"} {
		if !strings.Contains(out, s) {
			t.Errorf("misses does not show %q", s)
		}
	}
	if strings.Contains(out, `"n":1,`) || strings.Contains(out, `\"n\":1,`) {
		t.Errorf("the matched request is listed as a miss")
	}
}

func countMisses(t *testing.T, out string) int {
	t.Helper()
	var v struct {
		Requests []json.RawMessage `json:"requests"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("misses output is not JSON: %v\n%s", err, out)
	}
	return len(v.Requests)
}

func TestStartRefusals(t *testing.T) {
	needDocker(t)
	rec := recordings(t, testMappings, map[string]string{"messages.sse": answer1})
	env := []string{"REPLAY_RECORDINGS=" + rec}

	check := func(name, wantMsg string, e []string, args ...string) {
		t.Helper()
		out, code := replaySh(e, args...)
		if code != 1 || !strings.Contains(out, wantMsg) {
			t.Errorf("%s: exit %d, output %q; want exit 1 mentioning %q", name, code, out, wantMsg)
		}
		if ps, _ := run("docker", "ps", "-a", "-q", "--filter", "name="+args[2]); ps != "" && len(args) > 2 {
			run("docker", "rm", "-f", args[2])
			t.Errorf("%s: a container was left behind", name)
		}
	}

	open := network(t, false)
	check("non-internal network", "not internal", env, "start", open, uniq("aptest-refused"))
	check("missing network", "not internal", env, "start", uniq("aptest-nonet"), uniq("aptest-refused"))

	// Loose matchers and a mapping with no url are refused, each beside a good recording.
	for name, req := range map[string]string{
		"urlPath":         `{"method":"POST","urlPath":"/v1/messages"}`,
		"urlPattern":      `{"method":"POST","urlPattern":"/v1/.*"}`,
		"urlPathPattern":  `{"method":"POST","urlPathPattern":"/v1/.*"}`,
		"queryParameters": `{"method":"POST","url":"/v1/messages","queryParameters":{"beta":{"equalTo":"true"}}}`,
		"no url":          `{"method":"POST"}`,
		"url not string":  `{"method":"POST","url":5}`,
	} {
		loose := recordings(t, map[string]string{
			"good":  testMappings["count"],
			"loose": `{"request":` + req + `,"response":{"status":200,"body":"x"}}`,
		}, nil)
		check("loose matcher "+name, "", []string{"REPLAY_RECORDINGS=" + loose}, "start", network(t, true), uniq("aptest-refused"))
	}

	// The same in a subfolder of mappings/.
	for name, m := range map[string]string{
		"loose": `{"request":{"method":"POST","urlPath":"/v1/messages"},"response":{"status":200,"body":"x"}}`,
		"proxy": `{"request":{"method":"POST","url":"/v1/messages"},"response":{"proxyBaseUrl":"https://api.anthropic.com"}}`,
	} {
		sub := recordings(t, map[string]string{"good": testMappings["count"]}, nil)
		must(t, os.MkdirAll(filepath.Join(sub, "mappings", "sub"), 0o755))
		must(t, os.WriteFile(filepath.Join(sub, "mappings", "sub", "bad.json"), []byte(m), 0o644))
		check(name+" mapping in mappings/sub", "", []string{"REPLAY_RECORDINGS=" + sub}, "start", network(t, true), uniq("aptest-refused"))
	}

	// A recording that proxies, even beside a good one.
	proxy := recordings(t, map[string]string{
		"good": testMappings["count"],
		"leak": `{"request":{"method":"ANY","urlPattern":".*"},"response":{"proxyBaseUrl":"https://api.anthropic.com"}}`,
	}, nil)
	internal := network(t, true)
	check("proxying recording", "proxies", []string{"REPLAY_RECORDINGS=" + proxy}, "start", internal, uniq("aptest-refused"))

	empty, err := os.MkdirTemp("", "aptest-empty")
	must(t, err)
	t.Cleanup(func() { os.RemoveAll(empty) })
	check("no recordings folder", "no recordings", []string{"REPLAY_RECORDINGS=" + empty}, "start", internal, uniq("aptest-refused"))

	// Wrong use is exit 2.
	for _, args := range [][]string{{}, {"start"}, {"misses"}, {"stop"}, {"bogus"}} {
		if out, code := replaySh(nil, args...); code != 2 {
			t.Errorf("replay.sh %v: exit %d (%s), want 2", args, code, out)
		}
	}
}

func TestAdminAPIRefusesWithoutCredential(t *testing.T) {
	l := start(t, recordings(t, testMappings, map[string]string{"messages.sse": answer1}))
	pw := l.credential(t)
	if len(pw) < 16 || !strings.HasPrefix(pw, "replay:") {
		t.Fatalf("credential %q looks weak", pw)
	}
	for _, p := range []string{"/__admin/health", "/__admin/mappings", "/__admin/requests", "/__admin/requests/unmatched", "/__admin/settings", "/__admin/shutdown", "/__admin/reset"} {
		if st, out := l.post(p, `{}`); st != 401 {
			t.Errorf("POST %s without credential: %d (%s), want 401", p, st, out)
		}
		if st, _ := l.get(p); st != 401 {
			t.Errorf("GET %s without credential: %d, want 401", p, st)
		}
		if st, _ := wget([]string{"exec", l.name, "wget", "-q", "-O", "-", "http://replay:wrong@127.0.0.1:8080" + p}); st != 401 {
			t.Errorf("GET %s with a wrong credential: %d, want 401", p, st)
		}
	}
	// With the credential the admin API works (the control).
	if st, _ := wget([]string{"exec", l.name, "wget", "-q", "-O", "-", "http://" + pw + "@127.0.0.1:8080/__admin/health"}); st != 200 {
		t.Errorf("health with the credential: %d, want 200", st)
	}
	// Two servers do not share a credential, and it is not in the command line or labels.
	insp := docker(t, "inspect", "-f", `{{json .Config.Cmd}} {{json .Args}} {{json .Config.Labels}} {{json .Config.Entrypoint}}`, l.name)
	if strings.Contains(insp, strings.TrimPrefix(pw, "replay:")) {
		t.Errorf("the credential appears outside the environment: %s", insp)
	}
	l2 := start(t, "")
	if l2.credential(t) == pw {
		t.Error("two servers share one credential")
	}
}

func TestAgentCannotAddMappings(t *testing.T) {
	l := start(t, recordings(t, testMappings, map[string]string{"messages.sse": answer1}))
	st, out := l.fromAgent("POST", "/__admin/mappings",
		`{"request":{"method":"POST","urlPath":"/v1/evil"},"response":{"status":200,"body":"pwned"}}`)
	if st != 401 {
		t.Errorf("agent network POST /__admin/mappings: %d (%s), want 401", st, out)
	}
	for _, p := range []string{"/__admin/mappings/import", "/__admin/mappings/new", "/__admin/recordings/snapshot", "/__admin/recordings/start"} {
		if st, _ := l.fromAgent("POST", p, `{}`); st != 401 {
			t.Errorf("agent network POST %s: %d, want 401", p, st)
		}
	}
	if st, _ := l.fromAgent("GET", "/__admin/mappings", ""); st != 401 {
		t.Errorf("agent network GET /__admin/mappings: %d, want 401", st)
	}
	if st, out := l.post("/v1/evil", `{}`); st != 404 {
		t.Errorf("the refused mapping answers: %d %s, want 404", st, out)
	}
	// The agent still reaches the model paths through the alias, with a recorded answer.
	if st, _ := l.fromAgent("POST", "/v1/messages?beta=true", body1); st != 200 {
		t.Errorf("agent request through the gateway alias: %d, want 200", st)
	}
}

func TestHardeningAndWiring(t *testing.T) {
	rec := recordings(t, testMappings, map[string]string{"messages.sse": answer1})
	l := start(t, rec)
	var v struct {
		Config struct {
			User   string
			Labels map[string]string
		}
		HostConfig struct {
			ReadonlyRootfs bool
			CapDrop        []string
			CapAdd         []string
			SecurityOpt    []string
			Memory         int64
			PidsLimit      int64
			Privileged     bool
		}
		Mounts []struct {
			Type, Destination string
			RW                bool
		}
		NetworkSettings struct {
			Networks map[string]struct{ Aliases []string }
		}
	}
	must(t, json.Unmarshal([]byte(docker(t, "inspect", "-f", "{{json .}}", l.name)), &v))
	if !v.HostConfig.ReadonlyRootfs {
		t.Error("root filesystem is not read-only")
	}
	if v.Config.User != "65532:65532" {
		t.Errorf("user = %q, want 65532:65532", v.Config.User)
	}
	if strings.Join(v.HostConfig.CapDrop, ",") != "ALL" || len(v.HostConfig.CapAdd) != 0 {
		t.Errorf("capabilities: drop %v add %v, want drop ALL only", v.HostConfig.CapDrop, v.HostConfig.CapAdd)
	}
	if v.HostConfig.Privileged {
		t.Error("container is privileged")
	}
	if !strings.Contains(strings.Join(v.HostConfig.SecurityOpt, " "), "no-new-privileges") {
		t.Errorf("security opts %v lack no-new-privileges", v.HostConfig.SecurityOpt)
	}
	if v.HostConfig.Memory != 512*1024*1024 {
		t.Errorf("memory = %d, want 512 MB", v.HostConfig.Memory)
	}
	if v.HostConfig.PidsLimit != 256 {
		t.Errorf("pids limit = %d, want 256", v.HostConfig.PidsLimit)
	}
	found := false
	for _, m := range v.Mounts {
		if m.Destination == "/home/wiremock" {
			found = true
			if m.RW {
				t.Error("the recordings are mounted read-write")
			}
		} else if m.Type == "bind" {
			t.Errorf("unexpected bind mount %s", m.Destination)
		}
	}
	if !found {
		t.Error("recordings are not mounted at /home/wiremock")
	}
	if a := v.NetworkSettings.Networks[l.net].Aliases; !contains(a, "gateway") {
		t.Errorf("aliases on %s = %v, want gateway", l.net, a)
	}
	if len(v.NetworkSettings.Networks) != 1 {
		t.Errorf("attached to %d networks, want only the given one", len(v.NetworkSettings.Networks))
	}
	// The mount really is read-only, and the root is writable nowhere but /tmp.
	if out, code := run("docker", "exec", l.name, "sh", "-c", "touch /home/wiremock/x"); code == 0 {
		t.Errorf("wrote to the recordings: %s", out)
	}
	if out, code := run("docker", "exec", l.name, "sh", "-c", "touch /etc/x"); code == 0 {
		t.Errorf("wrote to the root filesystem: %s", out)
	}
	if out := docker(t, "exec", l.name, "id", "-u"); out != "65532" {
		t.Errorf("runs as uid %s", out)
	}
	if _, ok := v.Config.Labels["ap.replay"]; !ok {
		t.Error("missing the ap.replay label")
	}
}

func TestNoEgressFromTheServer(t *testing.T) {
	// On an internal network the server cannot reach the outside, so nothing can be passed through.
	l := start(t, "")
	if out, code := run("docker", "exec", l.name, "wget", "-q", "-T", "3", "-O", "-", "http://1.1.1.1/"); code == 0 {
		t.Errorf("the replay server reached the Internet: %s", out)
	}
}

func TestStopRemovesTheContainer(t *testing.T) {
	l := start(t, "")
	if out, code := replaySh(nil, "stop", l.name); code != 0 {
		t.Fatalf("stop: exit %d: %s", code, out)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ps, _ := run("docker", "ps", "-a", "-q", "--filter", "name=^"+l.name+"$"); ps == "" {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Error("container still exists after stop")
}

func contains(s []string, x string) bool {
	for _, e := range s {
		if e == x {
			return true
		}
	}
	return false
}
