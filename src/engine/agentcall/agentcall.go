// Package agentcall asks one agent of a Docker Agent `serve api` service one question and returns
// its last answer and the model it used (research D27, D29). It uses the standard library only.
// Each Ask has its own session: created with a title and `safety_policy: restricted`, run as an
// event stream read until `stream_stopped`, and deleted however the call ends. It never retries.
package agentcall

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// deleteTimeout bounds the session's deletion, which runs even after the call was cancelled.
	deleteTimeout = 5 * time.Second
	// maxLine is the longest event line read: a 32K-token answer is about 130 KB.
	maxLine = 4 << 20
	// maxReply caps a JSON reply other than the stream.
	maxReply = 1 << 20
)

// Client calls one `serve api` service. It is safe for concurrent use.
type Client struct {
	base, token string
	client      *http.Client
}

// New returns the client of the service at baseURL (for example "http://analyst:8080"),
// authenticating with the service's bearer token; an empty token sends no Authorization header.
func New(baseURL, token string) *Client {
	return &Client{
		base: strings.TrimRight(baseURL, "/"), token: token,
		client: &http.Client{Transport: &http.Transport{
			Proxy:                 nil, // the service is reached directly, never through a proxy
			DialContext:           (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
			ResponseHeaderTimeout: 30 * time.Second,
			MaxIdleConnsPerHost:   8,
			IdleConnTimeout:       90 * time.Second,
		}},
	}
}

// Answer is the agent's last answer (the last agent_choice content), the model the service
// reported (agent_info), and what the call used: input plus output tokens and cost in USD (0 when
// no token_usage event came). A token_usage event's `usage` is its session's running total, and a
// sub-session reports its own, not folded into its parent's (Docker Agent 1.160.0,
// pkg/runtime/event.go), so the use is the last event's of each session_id, added up over them.
type Answer struct {
	Content, Model string
	Tokens         int64
	CostUSD        float64
}

// Ask sends message to agent (its definition's file name without .yaml) in a new session titled
// title; a title is always sent, since an untitled session makes a second model call to name it.
// It fails unless the stream ends with stream_stopped and reason normal, without an error event,
// with an answer and a model; a failed call still returns the use seen before it failed,
// so a budget counts them. ctx bounds the call; the session is deleted on its own short deadline
// afterwards.
func (c *Client) Ask(ctx context.Context, agent, title, message string) (Answer, error) {
	var created struct {
		ID string `json:"id"`
	}
	if err := c.postJSON(ctx, "/api/sessions", map[string]string{"title": title, "safety_policy": "restricted"}, &created); err != nil {
		return Answer{}, fmt.Errorf("create session: %w", err)
	}
	if created.ID == "" {
		return Answer{}, errors.New("create session: the reply has no id")
	}
	session := "/api/sessions/" + url.PathEscape(created.ID)
	defer c.delete(session)

	body, err := json.Marshal(map[string]any{"messages": []map[string]string{{"role": "user", "content": message}}})
	if err != nil {
		return Answer{}, err
	}
	resp, err := c.do(ctx, http.MethodPost, session+"/agent/"+url.PathEscape(agent), body)
	if err != nil {
		return Answer{}, fmt.Errorf("run: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Answer{}, fmt.Errorf("run: %s", status(resp))
	}
	ans, err := readStream(resp.Body)
	if err != nil {
		return ans, fmt.Errorf("run: %w", err)
	}
	return ans, nil
}

// event is the part of a stream event the client reads.
type event struct {
	Type    string `json:"type"`
	Content string `json:"content"`
	Error   string `json:"error"`
	Session string `json:"session_id"`
	Model   string `json:"model"`
	Reason  string `json:"reason"`
	Usage   *struct {
		InputTokens  int64   `json:"input_tokens"`
		OutputTokens int64   `json:"output_tokens"`
		Cost         float64 `json:"cost"`
	} `json:"usage"`
}

// readStream reads server-sent events until stream_stopped and returns the last agent_choice
// content, the agent_info model and the use (Answer).
func readStream(r io.Reader) (Answer, error) {
	type use struct {
		tokens int64
		cost   float64
	}
	last := map[string]use{} // session_id -> its running total
	content, model, err := readEvents(r, func(e event) {
		if e.Type == "token_usage" && e.Usage != nil {
			last[e.Session] = use{max(e.Usage.InputTokens, 0) + max(e.Usage.OutputTokens, 0), max(e.Usage.Cost, 0)}
		}
	})
	ans := Answer{Content: content, Model: model}
	for _, u := range last {
		ans.Tokens += u.tokens
		ans.CostUSD += u.cost
	}
	return ans, err
}

func readEvents(r io.Reader, seen func(event)) (content, model string, err error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), maxLine)
	var data []string
	var seenChoice bool
	handle := func() (stopped bool, err error) {
		if len(data) == 0 {
			return false, nil
		}
		raw := strings.Join(data, "\n")
		data = data[:0]
		var e event
		if err := json.Unmarshal([]byte(raw), &e); err != nil {
			return false, fmt.Errorf("an event is not JSON: %v", err)
		}
		seen(e)
		switch e.Type {
		case "agent_info":
			if e.Model != "" {
				model = e.Model
			}
		case "agent_choice":
			content, seenChoice = e.Content, true
		case "error":
			return false, fmt.Errorf("the agent failed: %s", Clip(e.Error))
		case "stream_stopped":
			if e.Reason != "normal" {
				return true, fmt.Errorf("the stream stopped with reason %q", e.Reason)
			}
			return true, nil
		}
		return false, nil
	}
	for sc.Scan() {
		line := sc.Text()
		if line == "" { // the end of an event
			stopped, err := handle()
			if err != nil {
				return "", "", err
			}
			if stopped {
				return result(content, model, seenChoice)
			}
			continue
		}
		if v, ok := strings.CutPrefix(line, "data:"); ok {
			data = append(data, strings.TrimPrefix(v, " "))
		} // comments (": ping") and other fields are ignored
	}
	if err := sc.Err(); err != nil {
		return "", "", fmt.Errorf("reading the stream: %w", err)
	}
	stopped, err := handle() // a last event without its blank line
	if err != nil {
		return "", "", err
	}
	if !stopped {
		return "", "", errors.New("the stream ended without stream_stopped")
	}
	return result(content, model, seenChoice)
}

func result(content, model string, seenChoice bool) (string, string, error) {
	if !seenChoice {
		return "", "", errors.New("the stream has no agent_choice")
	}
	if model == "" {
		return "", "", errors.New("the stream names no model (agent_info)")
	}
	return content, model, nil
}

// postJSON POSTs in as JSON to path and decodes a 200 or 201 reply into out.
func (c *Client) postJSON(ctx context.Context, path string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	resp, err := c.do(ctx, http.MethodPost, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return errors.New(status(resp))
	}
	return json.NewDecoder(io.LimitReader(resp.Body, maxReply)).Decode(out)
}

// delete removes the session on its own short deadline, so a cancelled call still cleans up.
// A failure is not reported: the session database is on the service's tmpfs.
func (c *Client) delete(path string) {
	ctx, cancel := context.WithTimeout(context.Background(), deleteTimeout)
	defer cancel()
	resp, err := c.do(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, maxReply))
	resp.Body.Close()
}

func (c *Client) do(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, r)
	if err != nil {
		return nil, err
	}
	if c.token != "" { // no token, no header
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.client.Do(req)
}

// status describes a refused request by its status and the start of its body.
func status(resp *http.Response) string {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return fmt.Sprintf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
}

// Clip keeps a service's error text short enough for an operation's error detail.
func Clip(s string) string {
	const max = 512
	if len(s) > max {
		return strings.ToValidUTF8(s[:max], "") + "..."
	}
	return s
}
