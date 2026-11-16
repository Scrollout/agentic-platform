package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Unit tests of `recording write` and `recording synth` (T020), from the README's promises: write
// keeps only the response Content-Type, refuses a proxying mapping, and synth builds a stream
// from which a structured-output tool call parses. No Docker, no network.

func tmp(t *testing.T) string { t.Helper(); return t.TempDir() }

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// withStdin runs f with os.Stdin reading content.
func withStdin(t *testing.T, content string, f func()) {
	t.Helper()
	p := writeFile(t, tmp(t), "stdin", content)
	in, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	old := os.Stdin
	os.Stdin = in
	defer func() { os.Stdin = old }()
	f()
}

// captureStdout runs f and returns what it wrote to os.Stdout.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	p := filepath.Join(tmp(t), "stdout")
	out, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = out
	f()
	os.Stdout = old
	out.Close()
	b, _ := os.ReadFile(p)
	return string(b)
}

const providerSnapshot = `{"mappings":[
 {"id":"abc","uuid":"abc","request":{"url":"/v1/messages?beta=true","method":"POST",
   "bodyPatterns":[{"equalToJson":"{\"model\":\"m\",\"n\":1}","ignoreArrayOrder":false}]},
  "response":{"status":200,
   "headers":{"content-type":"text/event-stream","Request-Id":"req_SECRET1","Anthropic-Organization-Id":"org-SECRET2",
              "Set-Cookie":"c=SECRET3","X-Request-Id":"x","Date":"today"},
   "body":"event: message_stop\ndata: {}\n\n"}},
 {"request":{"url":"/v1/messages","method":"POST","bodyPatterns":[{"equalToJson":"{\"second\":true}"}]},
  "response":{"status":429,"headers":{"Content-Type":"application/json","request-id":"req_SECRET4"},"body":"{}"}}
]}`

func TestWriteKeepsOnlyContentType(t *testing.T) {
	dir := tmp(t)
	withStdin(t, providerSnapshot, func() {
		if err := write([]string{"-dir", dir, "-name", "ex"}); err != nil {
			t.Fatal(err)
		}
	})
	for _, f := range []string{"mappings/ex.json", "__files/ex.sse", "mappings/ex-2.json", "__files/ex-2.sse"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Fatalf("a second exchange is named -2: %v", err)
		}
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "mappings/ex.json"))
	for _, secret := range []string{"SECRET1", "SECRET2", "SECRET3", "Request-Id", "request-id", "Date", "Set-Cookie"} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("mapping still holds %q:\n%s", secret, raw)
		}
	}
	var m struct {
		Request  map[string]any `json:"request"`
		Response struct {
			Status       int               `json:"status"`
			Headers      map[string]string `json:"headers"`
			BodyFileName string            `json:"bodyFileName"`
			Body         *string           `json:"body"`
		} `json:"response"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(m.Response.Headers, map[string]string{"Content-Type": "text/event-stream"}) {
		t.Errorf("headers = %v, want only Content-Type", m.Response.Headers)
	}
	if m.Response.Status != 200 || m.Response.BodyFileName != "ex.sse" || m.Response.Body != nil {
		t.Errorf("response = %+v: want status 200 and the body in ex.sse", m.Response)
	}
	body, _ := os.ReadFile(filepath.Join(dir, "__files/ex.sse"))
	if string(body) != "event: message_stop\ndata: {}\n\n" {
		t.Errorf("body file = %q", body)
	}
	// The request matcher holds the body as a JSON object, not an escaped string.
	bp := m.Request["bodyPatterns"].([]any)[0].(map[string]any)
	if want := map[string]any{"model": "m", "n": json.Number("1")}; reflect.DeepEqual(bp["equalToJson"], want) {
		// ok when decoded with UseNumber
	} else if o, isObj := bp["equalToJson"].(map[string]any); !isObj || o["model"] != "m" {
		t.Errorf("equalToJson = %#v, want a JSON object", bp["equalToJson"])
	}
	raw2, _ := os.ReadFile(filepath.Join(dir, "mappings/ex-2.json"))
	if strings.Contains(string(raw2), "SECRET4") || !strings.Contains(string(raw2), "429") {
		t.Errorf("second exchange not tidied or lost its status:\n%s", raw2)
	}
}

func TestWriteRefusesProxyingMapping(t *testing.T) {
	dir := tmp(t)
	snap := `{"mappings":[{"request":{"url":"/v1/messages"},"response":{"status":200,"proxyBaseUrl":"https://api.anthropic.com"}}]}`
	withStdin(t, snap, func() {
		if err := write([]string{"-dir", dir, "-name", "p"}); err == nil {
			t.Fatal("write accepted a proxying mapping")
		}
	})
	if _, err := os.Stat(filepath.Join(dir, "mappings", "p.json")); err == nil {
		t.Error("a proxying mapping was written")
	}
}

func TestWriteRefusesLooseMatchers(t *testing.T) {
	for name, req := range map[string]string{
		"urlPattern":          `{"urlPattern":"/v1/.*"}`,
		"urlPathPattern":      `{"urlPathPattern":"/v1/.*"}`,
		"queryParameters":     `{"url":"/v1/messages","queryParameters":{"a":{"equalTo":"b"}}}`,
		"no url":              `{"method":"POST"}`,
		"url and urlPath":     `{"url":"/v1/messages","urlPath":"/v1/messages"}`,
		"url and query":       `{"url":"/v1/messages","queryParameters":{"a":{"equalTo":"b"}}}`,
		"url not string":      `{"url":5}`,
		"contains":            `{"urlPath":"/v1/messages","queryParameters":{"a":{"contains":"b"}}}`,
		"matches":             `{"urlPath":"/v1/messages","queryParameters":{"a":{"matches":"b.*"}}}`,
		"absent":              `{"urlPath":"/v1/messages","queryParameters":{"a":{"absent":true}}}`,
		"hasExactly contains": `{"urlPath":"/v1/messages","queryParameters":{"a":{"hasExactly":[{"contains":"b"}]}}}`,
		"urlPath not rooted":  `{"urlPath":"v1/messages"}`,
	} {
		dir := tmp(t)
		withStdin(t, `{"mappings":[{"request":`+req+`,"response":{"status":200,"body":"x"}}]}`, func() {
			if err := write([]string{"-dir", dir, "-name", "x"}); err == nil {
				t.Errorf("%s: write accepted it", name)
			}
		})
	}
}

func TestWriteRefusals(t *testing.T) {
	dir := tmp(t)
	for name, c := range map[string]struct {
		args  []string
		stdin string
	}{
		"empty snapshot":      {[]string{"-dir", dir, "-name", "x"}, `{"mappings":[]}`},
		"bad name":            {[]string{"-dir", dir, "-name", "../x"}, providerSnapshot},
		"upper-case name":     {[]string{"-dir", dir, "-name", "X"}, providerSnapshot},
		"no dir":              {[]string{"-name", "x"}, providerSnapshot},
		"body stays in file":  {[]string{"-dir", dir, "-name", "x"}, `{"mappings":[{"request":{},"response":{"status":200,"bodyFileName":"f"}}]}`},
		"request not JSON":    {[]string{"-dir", dir, "-name", "x"}, `{"mappings":[{"request":{"bodyPatterns":[{"equalToJson":"nope"}]},"response":{"status":200,"body":"x"}}]}`},
		"snapshot not a JSON": {[]string{"-dir", dir, "-name", "x"}, `not json`},
	} {
		withStdin(t, c.stdin, func() {
			if err := write(c.args); err == nil {
				t.Errorf("%s: write succeeded, want an error", name)
			}
		})
	}
}

type event struct {
	typ  string
	data map[string]any
}

func parseSSE(t *testing.T, s string) []event {
	t.Helper()
	var evs []event
	var cur event
	sc := bufio.NewScanner(strings.NewReader(s))
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			cur = event{typ: strings.TrimPrefix(line, "event: ")}
		case strings.HasPrefix(line, "data: "):
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &cur.data); err != nil {
				t.Fatalf("event %s: data is not JSON: %v", cur.typ, err)
			}
			if cur.data["type"] != cur.typ {
				t.Fatalf("event %s carries type %v", cur.typ, cur.data["type"])
			}
			evs = append(evs, cur)
		}
	}
	return evs
}

const synthRequest = `{"model":"claude-haiku-5-5","stream":true,"messages":[{"role":"user","content":"hi"}],
 "tools":[{"name":"__structured_output__","input_schema":{"type":"object"}}]}`
const synthProposal = `{
  "goals": [{"id": "G-1", "text": "quote \"x\" and \\ and \u00e9 and <b> &"}],
  "examples": "Feature: A\n\n  Rule: R-1\n"
}`

func runSynth(t *testing.T, extra ...string) (string, error) {
	t.Helper()
	d := tmp(t)
	req := writeFile(t, d, "req.json", synthRequest)
	prop := writeFile(t, d, "prop.json", synthProposal)
	var err error
	out := captureStdout(t, func() {
		err = synth(append([]string{"-request", req, "-proposal", prop}, extra...))
	})
	return out, err
}

func TestSynthStreamHoldsOneStructuredOutputCall(t *testing.T) {
	out, err := runSynth(t)
	if err != nil {
		t.Fatal(err)
	}
	var snap snapshot
	if err := json.Unmarshal([]byte(out), &snap); err != nil || len(snap.Mappings) != 1 {
		t.Fatalf("synth output is not a one-exchange snapshot: %v\n%s", err, out)
	}
	m := snap.Mappings[0]
	resp := m["response"].(map[string]any)
	if resp["status"].(float64) != 200 || resp["headers"].(map[string]any)["Content-Type"] == nil ||
		!strings.HasPrefix(resp["headers"].(map[string]any)["Content-Type"].(string), "text/event-stream") {
		t.Errorf("response = %v, want 200 text/event-stream", resp)
	}
	evs := parseSSE(t, resp["body"].(string))
	var types []string
	var args strings.Builder
	var tool map[string]any
	var stop any
	for _, e := range evs {
		types = append(types, e.typ)
		switch e.typ {
		case "message_start":
			if e.data["message"].(map[string]any)["model"] != "claude-haiku-5-5" {
				t.Errorf("model not echoed: %v", e.data)
			}
		case "content_block_start":
			tool = e.data["content_block"].(map[string]any)
		case "content_block_delta":
			d := e.data["delta"].(map[string]any)
			if d["type"] != "input_json_delta" {
				t.Errorf("delta type %v", d["type"])
			}
			args.WriteString(d["partial_json"].(string))
		case "message_delta":
			stop = e.data["delta"].(map[string]any)["stop_reason"]
		}
	}
	want := []string{"message_start", "content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop"}
	if !reflect.DeepEqual(types, want) {
		t.Errorf("events = %v, want %v", types, want)
	}
	if tool["type"] != "tool_use" || tool["name"] != "__structured_output__" || tool["id"] == "" {
		t.Errorf("content block = %v, want a tool_use of __structured_output__", tool)
	}
	if stop != "tool_use" {
		t.Errorf("stop_reason = %v, want tool_use", stop)
	}
	// The arguments assembled from the deltas parse and equal the proposal, escapes included.
	var got, wantProp any
	if err := json.Unmarshal([]byte(args.String()), &got); err != nil {
		t.Fatalf("tool arguments do not parse: %v\n%s", err, args.String())
	}
	_ = json.Unmarshal([]byte(synthProposal), &wantProp)
	if !reflect.DeepEqual(got, wantProp) {
		t.Errorf("tool arguments = %v, want the proposal %v", got, wantProp)
	}
	// The matcher: POST, the default path and query, the request body equal as JSON.
	req := m["request"].(map[string]any)
	if req["method"] != "POST" || req["url"] != "/v1/messages?beta=true" {
		t.Errorf("request matcher = %v, want method POST and the exact url", req)
	}
	for _, k := range []string{"urlPath", "urlPattern", "urlPathPattern", "queryParameters"} {
		if _, ok := req[k]; ok {
			t.Errorf("request matcher has loose %s", k)
		}
	}
	bp := req["bodyPatterns"].([]any)[0].(map[string]any)
	var sent, matched any
	_ = json.Unmarshal([]byte(synthRequest), &sent)
	if err := json.Unmarshal([]byte(bp["equalToJson"].(string)), &matched); err != nil || !reflect.DeepEqual(sent, matched) {
		t.Errorf("body matcher does not equal the request: %v", bp)
	}
	if bp["ignoreExtraElements"] != false || bp["ignoreArrayOrder"] != false {
		t.Errorf("nothing may be ignored when matching: %v", bp)
	}
}

func TestSynthThenWriteRoundTrip(t *testing.T) {
	out, err := runSynth(t)
	if err != nil {
		t.Fatal(err)
	}
	dir := tmp(t)
	withStdin(t, out, func() {
		if err := write([]string{"-dir", dir, "-name", "synth-1"}); err != nil {
			t.Fatal(err)
		}
	})
	sse, err := os.ReadFile(filepath.Join(dir, "__files", "synth-1.sse"))
	if err != nil || !strings.Contains(string(sse), "__structured_output__") {
		t.Fatalf("stream file missing or wrong: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "mappings", "synth-1.json"))
	if strings.Contains(string(raw), "proxyBaseUrl") {
		t.Error("a synthesised mapping must not proxy")
	}
}

func TestSynthCustomURL(t *testing.T) {
	out, err := runSynth(t, "-url", "/v1/messages")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"url":"/v1/messages"`) || strings.Contains(out, "queryParameters") {
		t.Errorf("want the exact url /v1/messages:\n%.300s", out)
	}
	if _, err := runSynth(t, "-url", "?beta=true"); err == nil {
		t.Error("synth accepted a -url without a path")
	}
}

func TestSynthRefusals(t *testing.T) {
	d := tmp(t)
	prop := writeFile(t, d, "prop.json", synthProposal)
	noTool := writeFile(t, d, "notool.json", `{"model":"m","tools":[{"name":"other"}]}`)
	notJSON := writeFile(t, d, "bad.json", `{`)
	badProp := writeFile(t, d, "badprop.json", `{`)
	good := writeFile(t, d, "good.json", synthRequest)
	for name, args := range map[string][]string{
		"no flags":             {},
		"no structured tool":   {"-request", noTool, "-proposal", prop},
		"request not JSON":     {"-request", notJSON, "-proposal", prop},
		"proposal not JSON":    {"-request", good, "-proposal", badProp},
		"missing request file": {"-request", filepath.Join(d, "none"), "-proposal", prop},
	} {
		var err error
		captureStdout(t, func() { err = synth(args) })
		if err == nil {
			t.Errorf("%s: synth succeeded, want an error", name)
		}
	}
}

// recorderURL runs write on one exchange whose request is req and returns the mapping's request.
func recorderRequest(t *testing.T, req string) map[string]any {
	t.Helper()
	dir := tmp(t)
	withStdin(t, `{"mappings":[{"request":`+req+`,"response":{"status":200,"body":"x"}}]}`, func() {
		if err := write([]string{"-dir", dir, "-name", "x"}); err != nil {
			t.Fatalf("write refused %s: %v", req, err)
		}
	})
	raw, _ := os.ReadFile(filepath.Join(dir, "mappings", "x.json"))
	var m struct{ Request map[string]any }
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"urlPath", "queryParameters", "urlPattern", "urlPathPattern"} {
		if _, ok := m.Request[k]; ok {
			t.Errorf("%s left in the mapping: %s", k, raw)
		}
	}
	return m.Request
}

func TestWriteConvertsTheRecordersShapeToOneExactURL(t *testing.T) {
	cases := map[string]struct{ req, url string }{
		"recorder shape (hasExactly)": {
			`{"urlPath":"/v1/messages","method":"POST","queryParameters":{"beta":{"hasExactly":[{"equalTo":"true"}]}}}`,
			"/v1/messages?beta=true"},
		"equalTo": {
			`{"urlPath":"/v1/messages","queryParameters":{"beta":{"equalTo":"true"}}}`,
			"/v1/messages?beta=true"},
		"two parameters sorted": {
			`{"urlPath":"/v1/messages","queryParameters":{"z":{"equalTo":"1"},"a":{"equalTo":"2"}}}`,
			"/v1/messages?a=2&z=1"},
		"multi-valued": {
			`{"urlPath":"/v1/messages","queryParameters":{"b":{"equalTo":"2"},"a":{"hasExactly":[{"equalTo":"x"},{"equalTo":"y"}]}}}`,
			"/v1/messages?a=x&a=y&b=2"},
		"encoded": {
			`{"urlPath":"/v1/messages","queryParameters":{"q":{"equalTo":"a b&c=d"}}}`,
			"/v1/messages?q=a+b%26c%3Dd"},
		"no query":  {`{"urlPath":"/v1/messages"}`, "/v1/messages"},
		"url as is": {`{"url":"/v1/messages?beta=true"}`, "/v1/messages?beta=true"},
	}
	for name, c := range cases {
		r := recorderRequest(t, c.req)
		if r["url"] != c.url {
			t.Errorf("%s: url = %v, want %s", name, r["url"], c.url)
		}
	}
	if r := recorderRequest(t, cases["recorder shape (hasExactly)"].req); r["method"] != "POST" {
		t.Errorf("method lost: %v", r)
	}
}
