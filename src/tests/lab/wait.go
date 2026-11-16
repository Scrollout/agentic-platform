//go:build docker

package lab

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
)

// WaitModelRequest returns once the step model (StepModel) has received a request whose first user
// message starts `Run <run>, step <step>` (any attempt or round), and fails t when limit passes
// first. It sees the request while the model still holds its answer, so a test can act inside one
// model call (T054l; research D34): in replay mode from the replay server's request journal, which
// lists a request before a mapping's `fixedDelayMilliseconds` has passed; in scripted mode from the
// exchanges the scripted server writes before it holds a `delayMs` answer. It reads only what the
// model received; it says nothing about whether the answer was sent.
func (l *Lab) WaitModelRequest(t testing.TB, run, step string, limit time.Duration) {
	t.Helper()
	if l.model == nil {
		t.Fatalf("WaitModelRequest(%s, %s): the lab has no model", run, step)
	}
	seen := l.replaySaw
	if l.model.scripted != "" {
		seen = l.scriptedSaw
	}
	deadline := time.Now().Add(limit)
	last := ""
	for {
		ok, why := seen(run, step)
		if ok {
			return
		}
		if why != "" {
			last = why
		}
		if !time.Now().Before(deadline) {
			if last != "" {
				last = " (last read: " + last + ")"
			}
			t.Fatalf("the step model received no request of run %s, step %s within %s%s", run, step, limit, last)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// replaySaw reads the replay server's request journal, through its admin API with the credential
// that lives only in the container's environment (as replay.sh's admin does). It returns whether a
// request of run and step is listed, or why the journal could not be read.
func (l *Lab) replaySaw(run, step string) (bool, string) {
	out, code := Run("docker", "exec", l.gateway, "sh", "-c",
		`set -- $WIREMOCK_OPTIONS; exec wget -qO- "http://$2@127.0.0.1:8080/__admin/requests"`)
	if code != 0 {
		return false, fmt.Sprintf("exit %d: %.200s", code, out)
	}
	var j struct {
		Requests []struct {
			Request struct {
				Body string `json:"body"`
			} `json:"request"`
		} `json:"requests"`
	}
	if err := json.Unmarshal([]byte(out), &j); err != nil {
		return false, fmt.Sprintf("not JSON: %v", err)
	}
	for _, r := range j.Requests {
		if stepRequestOf(r.Request.Body, run, step) {
			return true, ""
		}
	}
	return false, ""
}

// scriptedSaw lists the scripted server's exchanges, named `<run>-<step>-a<attempt>...` in
// lowercase, which it writes before it holds the answer.
func (l *Lab) scriptedSaw(run, step string) (bool, string) {
	out, code := Run("docker", "exec", l.model.scripted, "ls", "/out")
	if code != 0 {
		return false, fmt.Sprintf("exit %d: %.200s", code, out)
	}
	prefix := regexp.MustCompile(`^` + regexp.QuoteMeta(exchangePrefix(run, step)) + `a[0-9]+(-r[0-9]+)?-t[0-9]+\.json$`)
	for _, n := range strings.Fields(out) {
		if prefix.MatchString(n) {
			return true, ""
		}
	}
	return false, ""
}

var notExchangeName = regexp.MustCompile(`[^a-z0-9-]+`)

// exchangePrefix is the scripted server's exchange name up to the attempt: `<run>-<step>-`, in
// lowercase with anything but letters, digits and `-` replaced by `-`.
func exchangePrefix(run, step string) string {
	return notExchangeName.ReplaceAllString(strings.ToLower(run+"-"+step), "-") + "-"
}

// stepRequestOf reports whether body is a Messages request whose first user message starts
// `Run <run>, step <step>` followed by `,` or `.`.
func stepRequestOf(body, run, step string) bool {
	var req struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if json.Unmarshal([]byte(body), &req) != nil {
		return false
	}
	for _, m := range req.Messages {
		if m.Role != "user" {
			continue
		}
		rest, ok := strings.CutPrefix(firstText(m.Content), "Run "+run+", step "+step)
		return ok && (strings.HasPrefix(rest, ",") || strings.HasPrefix(rest, "."))
	}
	return false
}

// firstText is a message content's text: a string, or the first text block of an array.
func firstText(content json.RawMessage) string {
	var s string
	if json.Unmarshal(content, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(content, &blocks) != nil {
		return ""
	}
	for _, b := range blocks {
		if b.Type == "text" {
			return b.Text
		}
	}
	return ""
}
