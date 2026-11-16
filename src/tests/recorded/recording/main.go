// Command recording turns model exchanges into replay-server recordings (WireMock stub mappings,
// T020; research D26). It never calls a model or the network.
//
//	recording write  -dir <recordings> -name <name> < snapshot.json
//	recording synth  -request <request.json> -proposal <proposal.json> > snapshot.json
//
// `write` reads WireMock's own recording output (the JSON that POST /__admin/recordings/stop
// returns, {"mappings": [...]}) and writes one mapping per exchange to <dir>/mappings/<name>.json
// with its response body in <dir>/__files/<name>.sse. It keeps only the response's Content-Type
// header (and a fixedDelayMilliseconds from 0 to 60000), so provider headers such as request or
// organisation ids never reach the repository, and it refuses a mapping that would proxy (pass a
// request through) instead of answering.
//
// `synth` builds the same snapshot shape without a model: the request body the agent sent (as the
// replay server's journal shows it for a miss) and a proposal become one exchange whose response
// is an Anthropic Messages stream in which the model calls the structured-output tool with the
// proposal as its arguments.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	neturl "net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// The tool Docker Agent gives the model for `structured_output` in tool mode.
const structuredOutputTool = "__structured_output__"

type snapshot struct {
	Mappings []map[string]any `json:"mappings"`
}

func main() {
	if len(os.Args) < 2 {
		fail(errors.New("usage: recording write|synth [flags]"))
	}
	var err error
	switch os.Args[1] {
	case "write":
		err = write(os.Args[2:])
	case "synth":
		err = synth(os.Args[2:])
	default:
		err = fmt.Errorf("unknown command %q: use write or synth", os.Args[1])
	}
	if err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "recording:", err)
	os.Exit(1)
}

var validName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

func write(args []string) error {
	fs := flag.NewFlagSet("write", flag.ContinueOnError)
	dir := fs.String("dir", "", "recordings folder (holds mappings/ and __files/)")
	name := fs.String("name", "", "file name for the exchange; a second and later exchange get -2, -3, ...")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" || !validName.MatchString(*name) {
		return errors.New("write needs -dir and -name (lowercase letters, digits, hyphens)")
	}
	var s snapshot
	dec := json.NewDecoder(os.Stdin)
	dec.UseNumber()
	if err := dec.Decode(&s); err != nil {
		return fmt.Errorf("reading the snapshot: %w", err)
	}
	if len(s.Mappings) == 0 {
		return errors.New("the snapshot holds no exchange: nothing was recorded")
	}
	for _, d := range []string{"mappings", "__files"} {
		if err := os.MkdirAll(filepath.Join(*dir, d), 0o755); err != nil {
			return err
		}
	}
	for i, m := range s.Mappings {
		n := *name
		if i > 0 {
			n = fmt.Sprintf("%s-%d", *name, i+1)
		}
		out, body, err := tidy(m, n)
		if err != nil {
			return fmt.Errorf("exchange %d: %w", i+1, err)
		}
		if err := os.WriteFile(filepath.Join(*dir, "__files", n+".sse"), body, 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(*dir, "mappings", n+".json"), out, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "recording: wrote %s (status %v)\n", n, m["response"].(map[string]any)["status"])
	}
	return nil
}

// tidy keeps the request matcher, the status, the Content-Type and the fixedDelayMilliseconds (an
// integer from 0 to 60000) of one exchange, and moves the response body to its own file.
func tidy(m map[string]any, name string) ([]byte, []byte, error) {
	req, ok := m["request"].(map[string]any)
	if !ok {
		return nil, nil, errors.New("no request")
	}
	resp, ok := m["response"].(map[string]any)
	if !ok {
		return nil, nil, errors.New("no response")
	}
	if _, p := resp["proxyBaseUrl"]; p {
		return nil, nil, errors.New("the response proxies the request; a recording must answer it")
	}
	// The matcher is the exact URL, path and query as one string. WireMock's recorder writes the
	// path and each query parameter's exact values instead, which would also answer a request with
	// an extra parameter, so those become the one URL; any other URL matcher is refused.
	if err := exactURL(req); err != nil {
		return nil, nil, err
	}
	// The matcher holds the body as an object, not an escaped string, so a diff is readable.
	if bps, ok := req["bodyPatterns"].([]any); ok {
		for _, bp := range bps {
			if p, ok := bp.(map[string]any); ok {
				if s, ok := p["equalToJson"].(string); ok {
					var v any
					d := json.NewDecoder(bytes.NewReader([]byte(s)))
					d.UseNumber()
					if err := d.Decode(&v); err != nil {
						return nil, nil, fmt.Errorf("request body is not JSON: %w", err)
					}
					p["equalToJson"] = v
				}
			}
		}
	}
	var body []byte
	switch b := resp["body"].(type) {
	case string:
		body = []byte(b)
	case nil:
		if _, f := resp["bodyFileName"]; f {
			return nil, nil, errors.New("the response body is in a file; record with persist false so it comes inline")
		}
	default:
		return nil, nil, errors.New("the response body is not text")
	}
	headers := map[string]any{}
	if h, ok := resp["headers"].(map[string]any); ok {
		for k, v := range h {
			if strings.EqualFold(k, "Content-Type") {
				headers["Content-Type"] = v
			}
		}
	}
	kept := map[string]any{
		"status":       resp["status"],
		"headers":      headers,
		"bodyFileName": name + ".sse",
	}
	// A turn's delay is kept, so WireMock holds the answer as long as the exchange did.
	if d, ok := resp["fixedDelayMilliseconds"]; ok {
		n, isNum := d.(json.Number)
		ms, err := n.Int64()
		if !isNum || err != nil || ms < 0 || ms > 60000 {
			return nil, nil, fmt.Errorf("fixedDelayMilliseconds %v is not an integer from 0 to 60000", d)
		}
		kept["fixedDelayMilliseconds"] = ms
	}
	out := map[string]any{
		"name":     name,
		"request":  req,
		"response": kept,
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return nil, nil, err
	}
	return buf.Bytes(), body, nil
}

func synth(args []string) error {
	fs := flag.NewFlagSet("synth", flag.ContinueOnError)
	reqFile := fs.String("request", "", "the request body the agent sent (JSON)")
	propFile := fs.String("proposal", "", "the tool arguments the model answers with (JSON)")
	url := fs.String("url", "/v1/messages?beta=true", "the request URL the agent used")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *reqFile == "" || *propFile == "" {
		return errors.New("synth needs -request and -proposal")
	}
	reqBody, err := os.ReadFile(*reqFile)
	if err != nil {
		return err
	}
	var req map[string]any
	if err := json.Unmarshal(reqBody, &req); err != nil {
		return fmt.Errorf("request: %w", err)
	}
	if !offersTool(req, structuredOutputTool) {
		return fmt.Errorf("the request offers no %s tool: it is not a structured-output request", structuredOutputTool)
	}
	model, _ := req["model"].(string)
	// The same matcher shape WireMock's recorder writes: the exact URL, path and query as one
	// string, so an extra or missing query parameter misses.
	u, err := neturl.Parse(*url)
	if err != nil || u.Path == "" {
		return fmt.Errorf("-url: not a request URL: %q", *url)
	}
	prop, err := os.ReadFile(*propFile)
	if err != nil {
		return err
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, prop); err != nil {
		return fmt.Errorf("proposal: %w", err)
	}
	s := snapshot{Mappings: []map[string]any{{
		"request": map[string]any{
			"url":    u.String(),
			"method": "POST",
			"bodyPatterns": []any{map[string]any{
				"equalToJson": string(reqBody), "ignoreArrayOrder": false, "ignoreExtraElements": false,
			}},
		},
		"response": map[string]any{
			"status":  200,
			"headers": map[string]any{"Content-Type": "text/event-stream; charset=utf-8"},
			"body":    toolCallStream(model, compact.String()),
		},
	}}}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	return enc.Encode(s)
}

func offersTool(req map[string]any, name string) bool {
	tools, _ := req["tools"].([]any)
	for _, t := range tools {
		if m, ok := t.(map[string]any); ok && m["name"] == name {
			return true
		}
	}
	return false
}

// toolCallStream is the Anthropic Messages streaming answer in which the model calls the
// structured-output tool once with args, and stops for the tool's result.
func toolCallStream(model, args string) string {
	var b bytes.Buffer
	ev := func(typ string, v map[string]any) {
		v["type"] = typ
		j, _ := json.Marshal(v)
		fmt.Fprintf(&b, "event: %s\ndata: %s\n\n", typ, j)
	}
	ev("message_start", map[string]any{"message": map[string]any{
		"id": "msg_recorded", "type": "message", "role": "assistant", "model": model,
		"content": []any{}, "stop_reason": nil, "stop_sequence": nil,
		"usage": map[string]any{"input_tokens": 0, "output_tokens": 0}}})
	ev("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{
		"type": "tool_use", "id": "toolu_recorded", "name": structuredOutputTool, "input": map[string]any{}}})
	ev("content_block_delta", map[string]any{"index": 0,
		"delta": map[string]any{"type": "input_json_delta", "partial_json": args}})
	ev("content_block_stop", map[string]any{"index": 0})
	ev("message_delta", map[string]any{
		"delta": map[string]any{"stop_reason": "tool_use", "stop_sequence": nil},
		"usage": map[string]any{"output_tokens": 0}})
	ev("message_stop", map[string]any{})
	return b.String()
}

// exactURL turns a recorder's urlPath and queryParameters (each parameter's exact values, by
// equalTo or hasExactly of equalTo) into one exact url, and refuses any other URL matcher.
func exactURL(req map[string]any) error {
	for _, k := range []string{"urlPattern", "urlPathPattern"} {
		if _, ok := req[k]; ok {
			return fmt.Errorf("the request matcher uses %s; a recording matches the exact url", k)
		}
	}
	path, hasPath := req["urlPath"]
	_, hasQuery := req["queryParameters"]
	if _, ok := req["url"]; ok {
		if hasPath || hasQuery {
			return errors.New("the request matcher has both url and urlPath or queryParameters")
		}
		if _, ok := req["url"].(string); !ok {
			return errors.New("the request matcher's url is not a string")
		}
		return nil
	}
	p, ok := path.(string)
	if !hasPath || !ok || !strings.HasPrefix(p, "/") {
		return errors.New("the request matcher has no exact url")
	}
	q := neturl.Values{}
	if hasQuery {
		params, ok := req["queryParameters"].(map[string]any)
		if !ok {
			return errors.New("the request matcher's queryParameters is not an object")
		}
		for k, m := range params {
			vs, err := exactValues(m)
			if err != nil {
				return fmt.Errorf("query parameter %q: %w", k, err)
			}
			q[k] = vs
		}
	}
	u := neturl.URL{Path: p, RawQuery: q.Encode()}
	req["url"] = u.String()
	delete(req, "urlPath")
	delete(req, "queryParameters")
	return nil
}

// exactValues reads one query parameter matcher that names its exact values.
func exactValues(m any) ([]string, error) {
	mm, ok := m.(map[string]any)
	if !ok {
		return nil, errors.New("not a matcher")
	}
	if v, ok := mm["equalTo"].(string); ok && len(mm) == 1 {
		return []string{v}, nil
	}
	list, ok := mm["hasExactly"].([]any)
	if !ok || len(mm) != 1 || len(list) == 0 {
		return nil, errors.New("only equalTo or hasExactly of equalTo give an exact url")
	}
	var out []string
	for _, e := range list {
		em, ok := e.(map[string]any)
		v, isStr := em["equalTo"].(string)
		if !ok || !isStr || len(em) != 1 {
			return nil, errors.New("only equalTo or hasExactly of equalTo give an exact url")
		}
		out = append(out, v)
	}
	return out, nil
}
