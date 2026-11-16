package main

// Tests of the scripted model server (T054k; research D34, ADR 0038), written before the code. No
// Docker, no network, no model. They define the command's API, its script format and its
// behaviour; the implementer builds from this comment and these tests alone.
//
// # The command
//
//	scripted -set <dir> -out <dir> [-listen <addr>]     (addr defaults to :8080)
//
// serves the model gateway's `POST /v1/messages` (any query, for example `?beta=true`) from the
// answer script `<set>/answers.json`, and writes one snapshot per answered request into `-out`.
// It never calls a model or the network. In the package:
//
//	func loadScript(dir string) (*script, error) // reads dir/answers.json and every content file
//	func newServer(s *script, out string) *server // *server is an http.Handler; out may be ""
//
// newServer's out is the folder for the snapshots; "" writes none.
//
// # The script, <set>/answers.json
//
//	{
//	  "answers": [                      // at least one
//	    {
//	      "step":    "code",            // required: the step name of the message's first line
//	      "attempt": 1,                 // optional: absent or 0 answers any attempt
//	      "round":   0,                 // optional: the review round; absent or 0 = no round
//	      "turns": [                    // at least one; turns[k-1] answers turn k
//	        {
//	          "delayMs": 0,             // optional, integer 0..60000: hold the answer this long
//	          // exactly one of:
//	          "write":  [{"path": "internal/x.go", "content": "files/x.go.txt"}],
//	          "output": {"findings": []},  // any JSON value: the structured-output arguments
//	          "text":   "Done."
//	        }
//	      ]
//	    }
//	  ]
//	}
//
// Unknown fields are refused. A write's path is relative to the project (no leading "/", no ".."
// element); its content names a file relative to the set folder, read once at load, whose bytes
// must be valid UTF-8. Two answers with the same step, attempt and round are refused.
//
// # Picking the answer
//
// The first line of the first user message's text (a string, or the first text block of a content
// array) is the engine's `Run <run>, step <step>, attempt <n>[, review round <r>].`
// (src/engine/steps, worker.message), and the line `The project is the folder <dir>.` names the
// project folder. The answer is the one with that step and round and that attempt; failing that,
// the one with that step and round and any attempt. The turn is 1 plus the number of assistant
// messages in the request.
//
// # The answer
//
// Status 200, Content-Type `text/event-stream; charset=utf-8`, an Anthropic Messages stream:
// `message_start` (message.model = the request's model, role assistant, empty content), then for
// each content block i from 0: `content_block_start`, its deltas, `content_block_stop`, then
// `message_delta` with the stop reason and `message_stop`. Each event is `event: <type>` and
// `data: <JSON whose type is <type>>` followed by a blank line.
//   - write: one `tool_use` block named `write_file` per file, in order, with input
//     {"path": "<project folder>/<path>", "content": "<the content file's bytes>"} sent as
//     `input_json_delta`; stop reason `tool_use`.
//   - output: one `tool_use` block named `__structured_output__` with the output as its input;
//     stop reason `tool_use`.
//   - text: one `text` block (`text_delta`); stop reason `end_turn`.
//
// Block and message ids are deterministic (the same request gives a byte-identical stream, as an
// exact replay needs), and tool_use ids are unique within the run's conversation.
//
// # Misses
//
// A request no script answers (any other method or path, count_tokens included, a body that is
// not a step message, an unknown step, attempt or round, a turn beyond the answer's turns, or a
// write turn when the message names no project folder) gets 404 and is listed:
// `GET /__scripted/misses` returns a JSON array, oldest first, of {"reason": "<why>", "body":
// <the request body as JSON, or a string when it is not JSON>}. A miss writes no snapshot.
//
// # Exchanges
//
// Each answered request is written as `<out>/<name>.json`, a WireMock snapshot that `recording
// write -dir <recordings> -name <name>` accepts: {"mappings": [{"request": {"url": <the request's
// path and query, exactly>, "method": "POST", "bodyPatterns": [{"equalToJson": <the request body
// as a string>, "ignoreArrayOrder": false, "ignoreExtraElements": false}]}, "response": {"status":
// 200, "headers": {"Content-Type": ...}, "body": <the stream sent>, "fixedDelayMilliseconds":
// <delayMs, present only when the turn has delayMs > 0>}}]}. The name is
// `<run>-<step>-a<attempt>[-r<round>]-t<turn>`, lowercased (run `RUN-7`, step code, attempt 1,
// turn 2: `run-7-code-a1-t2`; review round 2: `run-7-review-a1-r2-t1`).

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

const (
	set     = "testdata/set"
	model   = "claude-haiku-5-5"
	project = "/projects/notes-api"
	msgURL  = "/v1/messages?beta=true"
)

// firstText is the engine's message for one attempt (and review round when round > 0).
func firstText(run, step string, attempt, round int) string {
	r := ""
	if round > 0 {
		r = fmt.Sprintf(", review round %d", round)
	}
	return fmt.Sprintf("Run %s, step %s, attempt %d%s.\nThe project is the folder %s.\n"+
		"Inputs (name: SHA-256):\n- slice: 0123abcd\n", run, step, attempt, r, project)
}

// body is a request as Docker Agent sends it: the first user message, then for each earlier turn
// an assistant message with a tool call and a user message with its result.
func body(mdl, text string, assistants int) []byte {
	msgs := []any{map[string]any{"role": "user", "content": []any{map[string]any{
		"type": "text", "text": text, "cache_control": map[string]any{"type": "ephemeral"}}}}}
	for i := 0; i < assistants; i++ {
		id := fmt.Sprintf("toolu_earlier_%d", i)
		msgs = append(msgs,
			map[string]any{"role": "assistant", "content": []any{map[string]any{
				"type": "tool_use", "id": id, "name": "read_file", "input": map[string]any{"path": "x"}}}},
			map[string]any{"role": "user", "content": []any{map[string]any{
				"type": "tool_result", "tool_use_id": id, "content": "ok"}}})
	}
	b, _ := json.Marshal(map[string]any{
		"model": mdl, "max_tokens": 16000, "stream": true, "system": "You are a step agent.",
		"messages": msgs,
		"tools": []any{
			map[string]any{"name": "write_file", "input_schema": map[string]any{"type": "object"}},
			map[string]any{"name": "__structured_output__", "input_schema": map[string]any{"type": "object"}},
		},
	})
	return b
}

func serve(t *testing.T, out string) http.Handler {
	t.Helper()
	s, err := loadScript(set)
	if err != nil {
		t.Fatalf("loadScript(%s): %v", set, err)
	}
	return newServer(s, out)
}

type reply struct {
	status  int
	header  http.Header
	body    []byte
	elapsed time.Duration
}

func do(h http.Handler, method, url string, b []byte) reply {
	req := httptest.NewRequest(method, url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	start := time.Now()
	h.ServeHTTP(rec, req)
	return reply{rec.Code, rec.Header(), rec.Body.Bytes(), time.Since(start)}
}

func post(h http.Handler, b []byte) reply { return do(h, "POST", msgURL, b) }

// block is one content block of a stream, assembled from its deltas.
type block struct {
	typ, id, name, text string
	input               any
}

type stream struct {
	blocks []block
	stop   string
}

// parse checks that s is a well-formed Anthropic Messages stream for model mdl and assembles it.
func parse(t *testing.T, s []byte, mdl string) stream {
	t.Helper()
	if !bytes.HasSuffix(s, []byte("\n\n")) {
		t.Errorf("the stream does not end with a blank line: %q", tail(s))
	}
	type event struct {
		typ  string
		data map[string]any
	}
	var evs []event
	sc := bufio.NewScanner(bytes.NewReader(s))
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	var typ string
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
		case strings.HasPrefix(line, "event: "):
			if typ != "" {
				t.Fatalf("event %s has no data line", typ)
			}
			typ = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			var d map[string]any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &d); err != nil {
				t.Fatalf("event %s: data is not JSON: %v", typ, err)
			}
			if typ == "" || d["type"] != typ {
				t.Fatalf("data of type %v under event %q", d["type"], typ)
			}
			evs = append(evs, event{typ, d})
			typ = ""
		default:
			t.Fatalf("not an event-stream line: %q", line)
		}
	}
	if len(evs) < 4 || evs[0].typ != "message_start" || evs[len(evs)-1].typ != "message_stop" ||
		evs[len(evs)-2].typ != "message_delta" {
		var types []string
		for _, e := range evs {
			types = append(types, e.typ)
		}
		t.Fatalf("events %v: want message_start ... message_delta, message_stop", types)
	}
	m, _ := evs[0].data["message"].(map[string]any)
	if m["model"] != mdl || m["role"] != "assistant" || m["type"] != "message" {
		t.Errorf("message_start = %v, want an assistant message of model %s", m, mdl)
	}
	if c, ok := m["content"].([]any); !ok || len(c) != 0 {
		t.Errorf("message_start content = %v, want []", m["content"])
	}
	var out stream
	d, _ := evs[len(evs)-2].data["delta"].(map[string]any)
	out.stop, _ = d["stop_reason"].(string)
	var cur *block
	var partial strings.Builder
	for _, e := range evs[1 : len(evs)-2] {
		idx, _ := e.data["index"].(float64)
		switch e.typ {
		case "content_block_start":
			if cur != nil {
				t.Fatalf("block %d starts before block %d stops", int(idx), len(out.blocks))
			}
			if int(idx) != len(out.blocks) {
				t.Fatalf("block index %v, want %d", idx, len(out.blocks))
			}
			cb, _ := e.data["content_block"].(map[string]any)
			cur = &block{}
			cur.typ, _ = cb["type"].(string)
			cur.id, _ = cb["id"].(string)
			cur.name, _ = cb["name"].(string)
			partial.Reset()
		case "content_block_delta":
			if cur == nil || int(idx) != len(out.blocks) {
				t.Fatalf("delta for block %v outside it", idx)
			}
			dd, _ := e.data["delta"].(map[string]any)
			switch dd["type"] {
			case "input_json_delta":
				if cur.typ != "tool_use" {
					t.Fatalf("input_json_delta in a %s block", cur.typ)
				}
				pj, _ := dd["partial_json"].(string)
				partial.WriteString(pj)
			case "text_delta":
				if cur.typ != "text" {
					t.Fatalf("text_delta in a %s block", cur.typ)
				}
				tx, _ := dd["text"].(string)
				cur.text += tx
			default:
				t.Fatalf("delta type %v", dd["type"])
			}
		case "content_block_stop":
			if cur == nil || int(idx) != len(out.blocks) {
				t.Fatalf("stop for block %v outside it", idx)
			}
			if cur.typ == "tool_use" {
				if err := json.Unmarshal([]byte(partial.String()), &cur.input); err != nil {
					t.Fatalf("tool %s: input does not parse: %v: %s", cur.name, err, partial.String())
				}
				if cur.id == "" {
					t.Errorf("tool %s has no id", cur.name)
				}
			}
			out.blocks = append(out.blocks, *cur)
			cur = nil
		default:
			t.Fatalf("unexpected event %s between message_start and message_delta", e.typ)
		}
	}
	if cur != nil {
		t.Fatal("a content block never stops")
	}
	if len(out.blocks) == 0 {
		t.Fatal("the stream holds no content block")
	}
	return out
}

func tail(b []byte) []byte {
	if len(b) > 80 {
		return b[len(b)-80:]
	}
	return b
}

// answered posts b, wants 200 and an event stream, and returns it parsed.
func answered(t *testing.T, h http.Handler, b []byte) stream {
	t.Helper()
	r := post(h, b)
	if r.status != 200 {
		t.Fatalf("status %d, want 200: %s", r.status, r.body)
	}
	if ct := r.header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("Content-Type %q, want text/event-stream", ct)
	}
	var req struct{ Model string }
	_ = json.Unmarshal(b, &req)
	return parse(t, r.body, req.Model)
}

func content(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(set, "files", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func writes(t *testing.T, s stream) map[string]string {
	t.Helper()
	got := map[string]string{}
	for _, b := range s.blocks {
		if b.typ != "tool_use" || b.name != "write_file" {
			t.Fatalf("block %+v, want only write_file calls", b)
		}
		in, _ := b.input.(map[string]any)
		p, _ := in["path"].(string)
		c, ok := in["content"].(string)
		if !ok || len(in) != 2 {
			t.Fatalf("write_file input %v, want exactly path and content", in)
		}
		got[p] = c
	}
	return got
}

func TestPicksTheAnswerByStepAttemptAndRound(t *testing.T) {
	h := serve(t, "")
	firstWrite := func(s stream) string {
		if s.blocks[0].name != "write_file" {
			return ""
		}
		p, _ := s.blocks[0].input.(map[string]any)["path"].(string)
		return p
	}
	findings := func(s stream) int {
		if s.blocks[0].name != "__structured_output__" {
			return -1
		}
		f, _ := s.blocks[0].input.(map[string]any)["findings"].([]any)
		return len(f)
	}
	// The any-attempt answer, for attempts 1 and 3.
	for _, a := range []int{1, 3} {
		s := answered(t, h, body(model, firstText("RUN-1", "tests", a, 0), 0))
		if got := firstWrite(s); got != project+"/tests/notes_test.go" {
			t.Errorf("tests attempt %d: first write %q, want the any-attempt answer", a, got)
		}
	}
	// An answer for the attempt wins over the any-attempt one.
	s := answered(t, h, body(model, firstText("RUN-1", "tests", 2, 0), 0))
	if s.blocks[0].typ != "text" || s.blocks[0].text != "Second attempt: nothing to add." {
		t.Errorf("tests attempt 2: %+v, want the attempt-2 answer", s.blocks)
	}
	// The round picks among review answers.
	if n := findings(answered(t, h, body(model, firstText("RUN-1", "review", 1, 1), 0))); n != 1 {
		t.Errorf("review round 1: %d findings, want 1", n)
	}
	if n := findings(answered(t, h, body(model, firstText("RUN-1", "review", 1, 2), 0))); n != 0 {
		t.Errorf("review round 2: %d findings, want 0", n)
	}
	// The run does not pick: another run gets the same answer.
	if got := firstWrite(answered(t, h, body(model, firstText("RUN-9", "code", 1, 0), 0))); got != project+"/internal/notes/notes.go" {
		t.Errorf("code attempt 1 of RUN-9: first write %q", got)
	}
	// Content given as one string, not an array of blocks, is read the same way.
	var b map[string]any
	_ = json.Unmarshal(body(model, "", 0), &b)
	b["messages"] = []any{map[string]any{"role": "user", "content": firstText("RUN-1", "tests", 2, 0)}}
	sb, _ := json.Marshal(b)
	if s := answered(t, h, sb); s.blocks[0].text != "Second attempt: nothing to add." {
		t.Errorf("string content: %+v", s.blocks)
	}
	// No answer: an attempt with no answer and no any-attempt one, a round with none, a review
	// message without a round, an unknown step.
	for name, text := range map[string]string{
		"code attempt 2":       firstText("RUN-1", "code", 2, 0),
		"review round 3":       firstText("RUN-1", "review", 1, 3),
		"review without round": firstText("RUN-1", "review", 1, 0),
		"tests with a round":   firstText("RUN-1", "tests", 1, 1),
		"unknown step":         firstText("RUN-1", "generate", 1, 0),
	} {
		if r := post(h, body(model, text, 0)); r.status != 404 {
			t.Errorf("%s: status %d, want 404", name, r.status)
		}
	}
}

func TestPicksTheTurnByTheNumberOfAssistantMessages(t *testing.T) {
	h := serve(t, "")
	text := firstText("RUN-1", "code", 1, 0)
	if s := answered(t, h, body(model, text, 0)); s.blocks[0].name != "write_file" || s.stop != "tool_use" {
		t.Errorf("turn 1: %+v stop %q, want the write_file calls", s.blocks, s.stop)
	}
	if s := answered(t, h, body(model, text, 1)); len(s.blocks) != 1 || s.blocks[0].text != "Done." || s.stop != "end_turn" {
		t.Errorf("turn 2: %+v stop %q, want the text Done.", s.blocks, s.stop)
	}
	if r := post(h, body(model, text, 2)); r.status != 404 {
		t.Errorf("turn 3 of a two-turn answer: status %d, want 404", r.status)
	}
}

func TestWriteFilePathIsTheProjectFolderPlusTheScriptPathWithExactBytes(t *testing.T) {
	h := serve(t, "")
	s := answered(t, h, body(model, firstText("RUN-1", "code", 1, 0), 0))
	want := map[string]string{
		project + "/internal/notes/notes.go": content(t, "notes.go.txt"),
		project + "/cmd/notes/main.go":       content(t, "main.go.txt"),
	}
	if got := writes(t, s); !reflect.DeepEqual(got, want) {
		t.Errorf("writes = %q\nwant %q", got, want)
	}
	if s.blocks[0].input.(map[string]any)["path"] != project+"/internal/notes/notes.go" {
		t.Error("the writes are not in the script's order")
	}
	if s.blocks[0].id == s.blocks[1].id {
		t.Errorf("two tool calls share the id %q", s.blocks[0].id)
	}
	// Another project folder in the message gives another path.
	other := strings.Replace(firstText("RUN-1", "code", 1, 0), project, "/projects/other", 1)
	if _, ok := writes(t, answered(t, h, body(model, other, 0)))["/projects/other/cmd/notes/main.go"]; !ok {
		t.Error("the path does not follow the message's project folder")
	}
	// A write turn for a message that names no project folder is a miss.
	noFolder := "Run RUN-1, step code, attempt 1.\nInputs (name: SHA-256):\n"
	if r := post(h, body(model, noFolder, 0)); r.status != 404 {
		t.Errorf("write turn without a project folder: status %d, want 404", r.status)
	}
}

func TestEachTurnIsAWellFormedStreamWithTheRequestsModel(t *testing.T) {
	h := serve(t, "")
	for _, mdl := range []string{model, "claude-other-test-model"} {
		// Tool calls.
		s := answered(t, h, body(mdl, firstText("RUN-1", "code", 1, 0), 0))
		if s.stop != "tool_use" || len(s.blocks) != 2 {
			t.Errorf("%s write turn: %d blocks, stop %q", mdl, len(s.blocks), s.stop)
		}
		// A structured-output answer.
		s = answered(t, h, body(mdl, firstText("RUN-1", "review", 1, 1), 0))
		want := map[string]any{"findings": []any{map[string]any{"rule": "R-1", "file": "internal/notes/notes.go",
			"line": float64(3), "text": "Listing drops the first note.", "blocking": true}}}
		if len(s.blocks) != 1 || s.blocks[0].name != "__structured_output__" || s.stop != "tool_use" ||
			!reflect.DeepEqual(s.blocks[0].input, want) {
			t.Errorf("%s output turn: %+v stop %q, want one __structured_output__ call with the findings", mdl, s.blocks, s.stop)
		}
		// A final text.
		s = answered(t, h, body(mdl, firstText("RUN-1", "tests", 1, 0), 1))
		if len(s.blocks) != 1 || s.blocks[0].typ != "text" || s.blocks[0].text != "The tests are written." || s.stop != "end_turn" {
			t.Errorf("%s text turn: %+v stop %q", mdl, s.blocks, s.stop)
		}
	}
	// The same request gives a byte-identical stream, so an exact replay of the next request
	// (which holds this answer's tool ids) matches.
	b := body(model, firstText("RUN-1", "code", 1, 0), 0)
	if r1, r2 := post(h, b), post(h, b); !bytes.Equal(r1.body, r2.body) {
		t.Error("the same request gave two different streams")
	}
	// Tool ids are unique across the turns of one run's conversation.
	a := answered(t, h, body(model, firstText("RUN-1", "tests", 1, 0), 0))
	c := answered(t, h, body(model, firstText("RUN-1", "code", 1, 0), 0))
	if a.blocks[0].id == c.blocks[0].id || a.blocks[0].id == c.blocks[1].id {
		t.Errorf("tool ids repeat across steps: %q, %q, %q", a.blocks[0].id, c.blocks[0].id, c.blocks[1].id)
	}
}

func TestARequestNoScriptAnswersGets404AndIsListed(t *testing.T) {
	out := t.TempDir()
	h := serve(t, out)
	misses := []struct {
		method, url string
		body        []byte
	}{
		{"POST", msgURL, body(model, firstText("RUN-1", "code", 2, 0), 0)},
		{"POST", msgURL, body(model, firstText("RUN-1", "code", 1, 0), 5)},
		{"POST", msgURL, body(model, "An in-memory notes service.", 0)}, // not a step message (the analyst)
		{"POST", "/v1/messages/count_tokens?beta=true", body(model, firstText("RUN-1", "code", 1, 0), 0)},
		{"POST", msgURL, []byte("not json")},
	}
	for i, m := range misses {
		if r := do(h, m.method, m.url, m.body); r.status != 404 {
			t.Errorf("miss %d: status %d, want 404", i, r.status)
		}
	}
	// An answered request is not listed.
	answered(t, h, body(model, firstText("RUN-1", "tests", 1, 0), 0))
	r := do(h, "GET", "/__scripted/misses", nil)
	if r.status != 200 {
		t.Fatalf("GET /__scripted/misses: status %d", r.status)
	}
	var listed []struct {
		Reason string          `json:"reason"`
		Body   json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(r.body, &listed); err != nil {
		t.Fatalf("misses are not a JSON array: %v: %s", err, r.body)
	}
	if len(listed) != len(misses) {
		t.Fatalf("%d misses listed, want %d: %s", len(listed), len(misses), r.body)
	}
	for i, l := range listed {
		if l.Reason == "" {
			t.Errorf("miss %d has no reason", i)
		}
		if i < 4 {
			var got, want any
			_ = json.Unmarshal(misses[i].body, &want)
			if err := json.Unmarshal(l.Body, &got); err != nil || !reflect.DeepEqual(got, want) {
				t.Errorf("miss %d: body %s, want the request body as JSON", i, l.Body)
			}
		}
	}
	var s string
	if err := json.Unmarshal(listed[4].Body, &s); err != nil || s != "not json" {
		t.Errorf("a body that is not JSON is listed as %s, want the string", listed[4].Body)
	}
	// A miss writes no snapshot: only the one answered request is in out.
	if names := snapshotNames(t, out); !reflect.DeepEqual(names, []string{"run-1-tests-a1-t1"}) {
		t.Errorf("snapshots %v, want only run-1-tests-a1-t1", names)
	}
}

func snapshotNames(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range es {
		names = append(names, strings.TrimSuffix(e.Name(), ".json"))
	}
	sort.Strings(names)
	return names
}

// recordingBin builds the committed `recording` command into a temporary folder.
func recordingBin(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "recording")
	if out, err := exec.Command("go", "build", "-o", bin, "../recording").CombinedOutput(); err != nil {
		t.Fatalf("go build ../recording: %v: %s", err, out)
	}
	return bin
}

func TestEachExchangeIsASnapshotRecordingWriteAccepts(t *testing.T) {
	out := t.TempDir()
	h := serve(t, out)
	type ex struct {
		name  string
		body  []byte
		delay int // 0: no fixedDelayMilliseconds
	}
	exs := []ex{
		{"run-7-code-a1-t1", body(model, firstText("RUN-7", "code", 1, 0), 0), 300},
		{"run-7-code-a1-t2", body(model, firstText("RUN-7", "code", 1, 0), 1), 0},
		{"run-7-tests-a2-t1", body(model, firstText("RUN-7", "tests", 2, 0), 0), 0},
		{"run-7-review-a1-r2-t1", body(model, firstText("RUN-7", "review", 1, 2), 0), 0},
	}
	streams := map[string][]byte{}
	for _, e := range exs {
		r := post(h, e.body)
		if r.status != 200 {
			t.Fatalf("%s: status %d", e.name, r.status)
		}
		streams[e.name] = r.body
	}
	var want []string
	for _, e := range exs {
		want = append(want, e.name)
	}
	sort.Strings(want)
	if got := snapshotNames(t, out); !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshots %v, want %v", got, want)
	}
	bin := recordingBin(t)
	rec := t.TempDir()
	for _, e := range exs {
		raw, err := os.ReadFile(filepath.Join(out, e.name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		// The snapshot itself.
		var snap struct {
			Mappings []struct {
				Request struct {
					URL          string           `json:"url"`
					Method       string           `json:"method"`
					BodyPatterns []map[string]any `json:"bodyPatterns"`
				} `json:"request"`
				Response map[string]any `json:"response"`
			} `json:"mappings"`
		}
		if err := json.Unmarshal(raw, &snap); err != nil || len(snap.Mappings) != 1 {
			t.Fatalf("%s: not a one-exchange snapshot: %v: %.300s", e.name, err, raw)
		}
		m := snap.Mappings[0]
		if m.Request.URL != msgURL || m.Request.Method != "POST" || len(m.Request.BodyPatterns) != 1 {
			t.Errorf("%s: request matcher %+v, want POST %s and one body pattern", e.name, m.Request, msgURL)
		} else {
			bp := m.Request.BodyPatterns[0]
			s, _ := bp["equalToJson"].(string)
			var got, sent any
			_ = json.Unmarshal(e.body, &sent)
			if err := json.Unmarshal([]byte(s), &got); err != nil || !reflect.DeepEqual(got, sent) {
				t.Errorf("%s: equalToJson does not hold the request body", e.name)
			}
			if bp["ignoreArrayOrder"] != false || bp["ignoreExtraElements"] != false {
				t.Errorf("%s: the body pattern ignores something: %v", e.name, bp)
			}
		}
		if m.Response["status"] != float64(200) || m.Response["body"] != string(streams[e.name]) {
			t.Errorf("%s: response status %v, or its body is not the stream sent", e.name, m.Response["status"])
		}
		d, has := m.Response["fixedDelayMilliseconds"]
		if e.delay > 0 && d != float64(e.delay) {
			t.Errorf("%s: fixedDelayMilliseconds = %v, want %d", e.name, d, e.delay)
		}
		if e.delay == 0 && has {
			t.Errorf("%s: fixedDelayMilliseconds = %v on a turn without delay", e.name, d)
		}
		// `recording write` accepts it and writes the mapping and the stream under its name.
		cmd := exec.Command(bin, "write", "-dir", rec, "-name", e.name)
		cmd.Stdin = bytes.NewReader(raw)
		if o, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: recording write refused the snapshot: %v: %s", e.name, err, o)
		}
		sse, err := os.ReadFile(filepath.Join(rec, "__files", e.name+".sse"))
		if err != nil || !bytes.Equal(sse, streams[e.name]) {
			t.Errorf("%s: __files/%s.sse is not the stream sent (%v)", e.name, e.name, err)
		}
		mr, err := os.ReadFile(filepath.Join(rec, "mappings", e.name+".json"))
		if err != nil {
			t.Fatalf("%s: no mapping written: %v", e.name, err)
		}
		var mapping struct{ Response map[string]any }
		_ = json.Unmarshal(mr, &mapping)
		if e.delay > 0 && mapping.Response["fixedDelayMilliseconds"] != float64(e.delay) {
			t.Errorf("%s: the written mapping lost fixedDelayMilliseconds: %s", e.name, mr)
		}
	}
}

func TestATurnsDelayHoldsItsAnswer(t *testing.T) {
	h := serve(t, "")
	if r := post(h, body(model, firstText("RUN-1", "code", 1, 0), 0)); r.status != 200 || r.elapsed < 300*time.Millisecond {
		t.Errorf("code turn 1 (delayMs 300): status %d after %v, want 200 after at least 300ms", r.status, r.elapsed)
	}
	if r := post(h, body(model, firstText("RUN-1", "code", 1, 0), 1)); r.elapsed > 250*time.Millisecond {
		t.Errorf("code turn 2 (no delay) took %v", r.elapsed)
	}
}

func TestLoadScriptRefusesABadScript(t *testing.T) {
	good := func(turn string) string {
		return `{"answers":[{"step":"code","attempt":1,"turns":[` + turn + `]}]}`
	}
	for name, script := range map[string]string{
		"not JSON":          `{`,
		"no answers":        `{"answers":[]}`,
		"unknown field":     `{"answers":[{"step":"code","atempt":1,"turns":[{"text":"x"}]}]}`,
		"no step":           `{"answers":[{"attempt":1,"turns":[{"text":"x"}]}]}`,
		"no turns":          `{"answers":[{"step":"code","turns":[]}]}`,
		"two kinds":         good(`{"text":"x","output":{}}`),
		"no kind":           good(`{"delayMs":5}`),
		"delay too long":    good(`{"delayMs":60001,"text":"x"}`),
		"negative delay":    good(`{"delayMs":-1,"text":"x"}`),
		"fractional delay":  good(`{"delayMs":1.5,"text":"x"}`),
		"absolute path":     good(`{"write":[{"path":"/etc/passwd","content":"c.txt"}]}`),
		"dot-dot path":      good(`{"write":[{"path":"internal/../../x.go","content":"c.txt"}]}`),
		"missing content":   good(`{"write":[{"path":"internal/x.go","content":"none.txt"}]}`),
		"non-UTF-8 content": good(`{"write":[{"path":"internal/x.go","content":"bad.txt"}]}`),
		"duplicate answer":  `{"answers":[{"step":"code","turns":[{"text":"x"}]},{"step":"code","attempt":0,"turns":[{"text":"y"}]}]}`,
	} {
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "c.txt"), []byte("ok"), 0o644)
		_ = os.WriteFile(filepath.Join(dir, "bad.txt"), []byte{0xff, 0xfe}, 0o644)
		_ = os.WriteFile(filepath.Join(dir, "answers.json"), []byte(script), 0o644)
		if _, err := loadScript(dir); err == nil {
			t.Errorf("%s: loadScript accepted %s", name, script)
		}
	}
	// The same folder with a good script loads, so the refusals above are the script's.
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "c.txt"), []byte("ok"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "answers.json"), []byte(good(`{"delayMs":60000,"write":[{"path":"internal/x.go","content":"c.txt"}]}`)), 0o644)
	if _, err := loadScript(dir); err != nil {
		t.Errorf("a good script: %v", err)
	}
	if _, err := loadScript(t.TempDir()); err == nil {
		t.Error("a folder without answers.json loaded")
	}
}
