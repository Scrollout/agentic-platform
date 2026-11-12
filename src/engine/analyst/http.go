// Package analyst is the engine's client of the analyst service: the pinned Docker Agent running
// `serve api` with the analyst's definition (research D27). It implements slices.Analyst with the
// standard library only: one session per call, read as an event stream until it stops.
package analyst

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

	"github.com/agentic-platform/engine/slices"
)

const (
	// callTimeout bounds one Propose: just above the agent's own budget (max_time 5m in
	// src/agents/analyst.yaml), so the agent stops first and says why.
	callTimeout = 330 * time.Second
	// deleteTimeout bounds the session's deletion, which runs even after the call was cancelled.
	deleteTimeout = 5 * time.Second
	// maxLine is the longest event line read: a 32K-token proposal is about 130 KB.
	maxLine = 4 << 20
	// maxReply caps a JSON reply other than the stream.
	maxReply = 1 << 20
	// sessionTitle is sent with every session: without a title the service makes a second model
	// call to name the session, which costs money and sends the description to the model again.
	sessionTitle = "ap-analyst"
	agentName    = "analyst" // the definition's file name without .yaml
)

// HTTP calls the analyst service. It is safe for concurrent use; each call has its own session.
type HTTP struct {
	base, token, digest string
	client              *http.Client
}

// NewHTTP returns the client of the analyst service at baseURL (for example
// "http://analyst:8080"), authenticating with the service's bearer token. digest is the agent
// definition's digest, which every proposal names as its AgentDigest; the service does not report it.
func NewHTTP(baseURL, token, digest string) *HTTP {
	return &HTTP{
		base: strings.TrimRight(baseURL, "/"), token: token, digest: digest,
		client: &http.Client{Transport: &http.Transport{
			Proxy:                 nil, // the analyst is reached directly, never through a proxy
			DialContext:           (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
			ResponseHeaderTimeout: 30 * time.Second,
			MaxIdleConnsPerHost:   8,
			IdleConnTimeout:       90 * time.Second,
		}},
	}
}

var _ slices.Analyst = (*HTTP)(nil)

// Propose sends slices.AnalystMessage(in) to a new session and returns the last structured answer
// as the proposal, with the model the service reports and the client's digest. It fails unless
// the stream ends with stream_stopped and reason normal, without an error event. It never retries:
// slices.Service decides whether to ask again. The session is deleted however the call ends.
func (a *HTTP) Propose(ctx context.Context, in slices.AnalystInput) (slices.Proposal, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	var created struct {
		ID string `json:"id"`
	}
	if err := a.postJSON(ctx, "/api/sessions", map[string]string{"title": sessionTitle, "safety_policy": "restricted"}, &created); err != nil {
		return slices.Proposal{}, fmt.Errorf("analyst: create session: %w", err)
	}
	if created.ID == "" {
		return slices.Proposal{}, errors.New("analyst: create session: the reply has no id")
	}
	session := "/api/sessions/" + url.PathEscape(created.ID)
	defer a.delete(session)

	body, err := json.Marshal(map[string]any{"messages": []map[string]string{{"role": "user", "content": slices.AnalystMessage(in)}}})
	if err != nil {
		return slices.Proposal{}, err
	}
	resp, err := a.do(ctx, http.MethodPost, session+"/agent/"+agentName, body)
	if err != nil {
		return slices.Proposal{}, fmt.Errorf("analyst: run: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return slices.Proposal{}, fmt.Errorf("analyst: run: %s", status(resp))
	}
	content, model, err := readStream(resp.Body)
	if err != nil {
		return slices.Proposal{}, fmt.Errorf("analyst: run: %w", err)
	}
	var p slices.Proposal
	if err := json.Unmarshal([]byte(content), &p); err != nil {
		return slices.Proposal{}, fmt.Errorf("analyst: the answer is not a proposal: %v", err)
	}
	p.Model, p.AgentDigest = model, a.digest
	return p, nil
}

// event is the part of a stream event the client reads.
type event struct {
	Type    string `json:"type"`
	Content string `json:"content"`
	Error   string `json:"error"`
	Model   string `json:"model"`
	Reason  string `json:"reason"`
}

// readStream reads server-sent events until stream_stopped and returns the last agent_choice
// content and the agent_info model.
func readStream(r io.Reader) (content, model string, err error) {
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
		switch e.Type {
		case "agent_info":
			if e.Model != "" {
				model = e.Model
			}
		case "agent_choice":
			content, seenChoice = e.Content, true
		case "error":
			return false, fmt.Errorf("the agent failed: %s", clip(e.Error))
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
func (a *HTTP) postJSON(ctx context.Context, path string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	resp, err := a.do(ctx, http.MethodPost, path, body)
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
func (a *HTTP) delete(path string) {
	ctx, cancel := context.WithTimeout(context.Background(), deleteTimeout)
	defer cancel()
	resp, err := a.do(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, maxReply))
	resp.Body.Close()
}

func (a *HTTP) do(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.base+path, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return a.client.Do(req)
}

// status describes a refused request by its status and the start of its body.
func status(resp *http.Response) string {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return fmt.Sprintf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
}

// clip keeps an agent's error text short enough for an operation's error detail.
func clip(s string) string {
	const max = 512
	if len(s) > max {
		return strings.ToValidUTF8(s[:max], "") + "..."
	}
	return s
}
