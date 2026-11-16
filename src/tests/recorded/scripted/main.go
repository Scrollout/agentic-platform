// Command scripted answers a step agent's model requests from a reviewed answer script and writes
// each exchange as a replay-server snapshot (T054k; research D34, ADR 0038). It never calls a model
// or the network.
//
//	scripted -set <dir> -out <dir> [-listen <addr>]     (addr defaults to :8080)
//
// It serves the model gateway's `POST /v1/messages` (any query) from `<set>/answers.json`. The
// answer is picked by the step, attempt and review round of the engine's message (its first line,
// `Run <run>, step <step>, attempt <n>[, review round <r>].`), an answer for that attempt before
// one for any attempt; the turn is 1 plus the number of assistant messages in the request. A turn
// writes files (`write_file` calls, each path the project folder the message names plus the
// script's path, with the content file's exact bytes), gives a structured-output answer, or ends
// with a text, after an optional delay. The answer is an Anthropic Messages event stream, the same
// bytes for the same request.
//
// Each answered request is written as `<out>/<run>-<step>-a<attempt>[-r<round>]-t<turn>.json`
// (lowercase), a WireMock snapshot that `recording write` turns into a recording; a turn's delay
// becomes its `fixedDelayMilliseconds`. Any other request gets 404 and is listed, oldest first, by
// `GET /__scripted/misses` as {"reason", "body"}; a miss writes nothing.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	structuredOutputTool = "__structured_output__"
	writeFileTool        = "write_file"
	maxDelayMs           = 60000
)

func main() {
	set := flag.String("set", "", "the answer script's folder (holds answers.json and its content files)")
	out := flag.String("out", "", "the folder the exchanges are written to")
	listen := flag.String("listen", ":8080", "the address to serve on")
	flag.Parse()
	if *set == "" || *out == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: scripted -set <dir> -out <dir> [-listen <addr>]")
		os.Exit(2)
	}
	s, err := loadScript(*set)
	if err != nil {
		log.Fatalf("scripted: %v", err)
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatalf("scripted: %v", err)
	}
	srv := &http.Server{Addr: *listen, Handler: newServer(s, *out), ReadHeaderTimeout: 10 * time.Second}
	log.Printf("scripted: serving %s on %s", *set, *listen)
	log.Fatal(srv.ListenAndServe())
}

// The script: one answer per step, attempt (0: any) and round (0: none).
type script struct {
	answers map[key][]turn
}

type key struct {
	step           string
	attempt, round int
}

type turn struct {
	delayMs int
	writes  []fileWrite     // a write turn
	output  json.RawMessage // a structured-output turn
	text    *string         // a text turn
}

type fileWrite struct {
	path    string // relative to the project folder
	content string // the content file's bytes
}

type scriptFile struct {
	Answers []struct {
		Step    string `json:"step"`
		Attempt int    `json:"attempt"`
		Round   int    `json:"round"`
		Turns   []struct {
			DelayMs int `json:"delayMs"`
			Write   []struct {
				Path    string `json:"path"`
				Content string `json:"content"`
			} `json:"write"`
			Output json.RawMessage `json:"output"`
			Text   *string         `json:"text"`
		} `json:"turns"`
	} `json:"answers"`
}

// loadScript reads dir/answers.json and every content file it names, and refuses a bad script.
func loadScript(dir string) (*script, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "answers.json"))
	if err != nil {
		return nil, err
	}
	var f scriptFile
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("answers.json: %w", err)
	}
	if dec.More() {
		return nil, errors.New("answers.json: more than one JSON value")
	}
	if len(f.Answers) == 0 {
		return nil, errors.New("answers.json: no answers")
	}
	s := &script{answers: map[key][]turn{}}
	for i, a := range f.Answers {
		where := fmt.Sprintf("answers.json: answer %d", i+1)
		if a.Step == "" {
			return nil, fmt.Errorf("%s: no step", where)
		}
		if a.Attempt < 0 || a.Round < 0 {
			return nil, fmt.Errorf("%s: a negative attempt or round", where)
		}
		k := key{a.Step, a.Attempt, a.Round}
		if _, dup := s.answers[k]; dup {
			return nil, fmt.Errorf("%s: a second answer for step %s, attempt %d, round %d", where, a.Step, a.Attempt, a.Round)
		}
		if len(a.Turns) == 0 {
			return nil, fmt.Errorf("%s: no turns", where)
		}
		var turns []turn
		for j, t := range a.Turns {
			where := fmt.Sprintf("%s, turn %d", where, j+1)
			if t.DelayMs < 0 || t.DelayMs > maxDelayMs {
				return nil, fmt.Errorf("%s: delayMs %d is not from 0 to %d", where, t.DelayMs, maxDelayMs)
			}
			kinds := 0
			if t.Write != nil {
				kinds++
			}
			if t.Output != nil {
				kinds++
			}
			if t.Text != nil {
				kinds++
			}
			if kinds != 1 {
				return nil, fmt.Errorf("%s: want exactly one of write, output and text", where)
			}
			tu := turn{delayMs: t.DelayMs, output: t.Output, text: t.Text}
			if t.Write != nil && len(t.Write) == 0 {
				return nil, fmt.Errorf("%s: write names no file", where)
			}
			for _, w := range t.Write {
				if err := relativePath(w.Path); err != nil {
					return nil, fmt.Errorf("%s: path %q: %w", where, w.Path, err)
				}
				if err := relativePath(w.Content); err != nil {
					return nil, fmt.Errorf("%s: content %q: %w", where, w.Content, err)
				}
				b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(w.Content)))
				if err != nil {
					return nil, fmt.Errorf("%s: %w", where, err)
				}
				if !utf8.Valid(b) {
					return nil, fmt.Errorf("%s: content %s is not UTF-8", where, w.Content)
				}
				tu.writes = append(tu.writes, fileWrite{w.Path, string(b)})
			}
			turns = append(turns, tu)
		}
		s.answers[k] = turns
	}
	return s, nil
}

// relativePath refuses an empty or absolute path and one with an empty, "." or ".." element.
func relativePath(p string) error {
	if p == "" || strings.HasPrefix(p, "/") {
		return errors.New("not a relative path")
	}
	for _, e := range strings.Split(p, "/") {
		if e == "" || e == "." || e == ".." {
			return errors.New("not a plain relative path")
		}
	}
	return nil
}

type server struct {
	s   *script
	out string

	mu     sync.Mutex
	misses []miss
}

type miss struct {
	Reason string          `json:"reason"`
	Body   json.RawMessage `json:"body"`
}

// newServer serves s and writes each answered exchange into out ("" writes none).
func newServer(s *script, out string) *server { return &server{s: s, out: out} }

func (v *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && r.URL.Path == "/__scripted/misses" {
		v.mu.Lock()
		list := append([]miss{}, v.misses...)
		v.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(list)
		return
	}
	body, err := readBody(r)
	if err != nil {
		v.miss(w, "the request body could not be read: "+err.Error(), body)
		return
	}
	if r.Method != http.MethodPost || r.URL.Path != "/v1/messages" {
		v.miss(w, fmt.Sprintf("%s %s is not POST /v1/messages", r.Method, r.URL.Path), body)
		return
	}
	var req struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		v.miss(w, "the body is not a Messages request: "+err.Error(), body)
		return
	}
	first, assistants := "", 0
	for _, m := range req.Messages {
		switch {
		case m.Role == "assistant":
			assistants++
		case m.Role == "user" && first == "" && assistants == 0:
			first = leadingText(m.Content)
		}
	}
	msg, ok := parseMessage(first)
	if !ok {
		v.miss(w, "the first user message is not a step message (Run <run>, step <step>, attempt <n>[, review round <r>].)", body)
		return
	}
	turns, ok := v.s.answers[key{msg.step, msg.attempt, msg.round}]
	if !ok {
		turns, ok = v.s.answers[key{msg.step, 0, msg.round}]
	}
	if !ok {
		v.miss(w, fmt.Sprintf("no answer for step %s, attempt %d, round %d", msg.step, msg.attempt, msg.round), body)
		return
	}
	n := assistants + 1
	if n > len(turns) {
		v.miss(w, fmt.Sprintf("turn %d of step %s, attempt %d, round %d: the answer has %d turns", n, msg.step, msg.attempt, msg.round, len(turns)), body)
		return
	}
	tu := turns[n-1]
	if tu.writes != nil && msg.project == "" {
		v.miss(w, "a write turn, but the message names no project folder (The project is the folder <dir>.)", body)
		return
	}
	name := msg.name(n)
	stream := answer(req.Model, name, tu, msg.project)
	if v.out != "" {
		uri := r.RequestURI
		if uri == "" {
			uri = r.URL.RequestURI()
		}
		if err := v.snapshot(name, uri, body, stream, tu.delayMs); err != nil {
			log.Printf("scripted: writing %s: %v", name, err)
			http.Error(w, "the exchange could not be written: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if tu.delayMs > 0 {
		time.Sleep(time.Duration(tu.delayMs) * time.Millisecond)
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(stream)
}

func readBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	var b bytes.Buffer
	_, err := b.ReadFrom(http.MaxBytesReader(nil, r.Body, 64<<20))
	return b.Bytes(), err
}

// miss answers 404 and lists the request.
func (v *server) miss(w http.ResponseWriter, reason string, body []byte) {
	b := json.RawMessage(body)
	if !json.Valid(body) {
		b, _ = json.Marshal(string(body))
	}
	v.mu.Lock()
	v.misses = append(v.misses, miss{reason, b})
	v.mu.Unlock()
	log.Printf("scripted: miss: %s", reason)
	http.Error(w, "no script answers this request: "+reason, http.StatusNotFound)
}

// leadingText is the text of a message's text: a string, or the first text block of an array.
func leadingText(content json.RawMessage) string {
	var s string
	if json.Unmarshal(content, &s) != nil {
		var blocks []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(content, &blocks) != nil {
			return ""
		}
		for _, b := range blocks {
			if b.Type == "text" {
				s = b.Text
				break
			}
		}
	}
	return s
}

var (
	stepLine    = regexp.MustCompile(`^Run ([^,\s]+), step ([^,\s]+), attempt ([0-9]+)(?:, review round ([0-9]+))?\.$`)
	projectLine = regexp.MustCompile(`^The project is the folder (\S.*)\.$`)
	notName     = regexp.MustCompile(`[^a-z0-9-]+`)
)

type message struct {
	run, step, project string
	attempt, round     int
}

func parseMessage(text string) (message, bool) {
	lines := strings.Split(text, "\n")
	m := stepLine.FindStringSubmatch(lines[0])
	if m == nil {
		return message{}, false
	}
	msg := message{run: m[1], step: m[2]}
	msg.attempt, _ = strconv.Atoi(m[3])
	if m[4] != "" {
		msg.round, _ = strconv.Atoi(m[4])
	}
	if msg.attempt < 1 {
		return message{}, false
	}
	for _, l := range lines[1:] {
		if p := projectLine.FindStringSubmatch(l); p != nil {
			msg.project = strings.TrimRight(p[1], "/")
			break
		}
	}
	return msg, true
}

// name is the exchange's name, <run>-<step>-a<attempt>[-r<round>]-t<turn>, lowercased.
func (m message) name(turn int) string {
	n := fmt.Sprintf("%s-%s-a%d", m.run, m.step, m.attempt)
	if m.round > 0 {
		n += fmt.Sprintf("-r%d", m.round)
	}
	n = strings.Trim(notName.ReplaceAllString(strings.ToLower(n), "-"), "-")
	return fmt.Sprintf("%s-t%d", n, turn)
}

// id is a deterministic id, unique for the exchange name and the block.
func id(prefix, name string, block int) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s/%d", name, block)))
	return prefix + hex.EncodeToString(h[:12])
}

// answer is the turn's Anthropic Messages event stream.
func answer(model, name string, t turn, project string) []byte {
	var b bytes.Buffer
	ev := func(typ string, v map[string]any) {
		v["type"] = typ
		var j bytes.Buffer
		enc := json.NewEncoder(&j)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(v)
		fmt.Fprintf(&b, "event: %s\ndata: %s\n\n", typ, bytes.TrimRight(j.Bytes(), "\n"))
	}
	ev("message_start", map[string]any{"message": map[string]any{
		"id": id("msg_", name, -1), "type": "message", "role": "assistant", "model": model,
		"content": []any{}, "stop_reason": nil, "stop_sequence": nil,
		"usage": map[string]any{"input_tokens": 0, "output_tokens": 0}}})
	tool := func(i int, toolName, input string) {
		ev("content_block_start", map[string]any{"index": i, "content_block": map[string]any{
			"type": "tool_use", "id": id("toolu_", name, i), "name": toolName, "input": map[string]any{}}})
		ev("content_block_delta", map[string]any{"index": i,
			"delta": map[string]any{"type": "input_json_delta", "partial_json": input}})
		ev("content_block_stop", map[string]any{"index": i})
	}
	stop := "tool_use"
	switch {
	case t.writes != nil:
		for i, w := range t.writes {
			var in bytes.Buffer
			enc := json.NewEncoder(&in)
			enc.SetEscapeHTML(false)
			_ = enc.Encode(map[string]string{"path": project + "/" + w.path, "content": w.content})
			tool(i, writeFileTool, strings.TrimRight(in.String(), "\n"))
		}
	case t.output != nil:
		var c bytes.Buffer
		_ = json.Compact(&c, t.output)
		tool(0, structuredOutputTool, c.String())
	default:
		stop = "end_turn"
		ev("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{"type": "text", "text": ""}})
		ev("content_block_delta", map[string]any{"index": 0, "delta": map[string]any{"type": "text_delta", "text": *t.text}})
		ev("content_block_stop", map[string]any{"index": 0})
	}
	ev("message_delta", map[string]any{
		"delta": map[string]any{"stop_reason": stop, "stop_sequence": nil},
		"usage": map[string]any{"output_tokens": 0}})
	ev("message_stop", map[string]any{})
	return b.Bytes()
}

// snapshot writes the exchange as <out>/<name>.json, the shape WireMock's recorder returns.
func (v *server) snapshot(name, uri string, body, stream []byte, delayMs int) error {
	resp := map[string]any{
		"status":  200,
		"headers": map[string]any{"Content-Type": "text/event-stream; charset=utf-8"},
		"body":    string(stream),
	}
	if delayMs > 0 {
		resp["fixedDelayMilliseconds"] = delayMs
	}
	snap := map[string]any{"mappings": []any{map[string]any{
		"request": map[string]any{
			"url":    uri,
			"method": "POST",
			"bodyPatterns": []any{map[string]any{
				"equalToJson": string(body), "ignoreArrayOrder": false, "ignoreExtraElements": false,
			}},
		},
		"response": resp,
	}}}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(snap); err != nil {
		return err
	}
	tmp := filepath.Join(v.out, "."+name+".tmp")
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(v.out, name+".json"))
}
