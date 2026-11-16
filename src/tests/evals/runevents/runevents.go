// Package runevents reads one `docker-agent run --exec --json` run: the answer `--last` would
// print and the run's token use and cost (T109, "The trial's usage").
package runevents

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Usage is a run's token use and cost, in the shape the eval reports hold per trial
// (scripts/ci-evals-summary.py). The cost is Docker Agent's estimate from the price the agent
// definition declares, not the provider's bill.
type Usage struct {
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
}

// Run is what one run gave: its answer and its usage.
type Run struct {
	Answer string
	Usage  Usage
}

type event struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	Content   string `json:"content"`
	Reason    string `json:"reason"`
	Usage     *struct {
		InputTokens  int64   `json:"input_tokens"`
		OutputTokens int64   `json:"output_tokens"`
		Cost         float64 `json:"cost"`
	} `json:"usage"`
}

// Parse reads a run's NDJSON events. The answer is the agent_choice content of the root
// session's last assistant message (between its last two message_added events), which is what
// `--last` prints; the usage is each session's last token_usage (a running total), summed over
// the sessions. It fails on a line that is not a JSON event, when the last stream_stopped is
// missing or not "normal", and when the answer is empty; even then it returns what it read (the
// usage so far and any answer), since a failed run's tokens were spent too.
func Parse(r io.Reader) (Run, error) {
	var (
		root, stopped string
		seenStop      bool
		segment       strings.Builder
		answer        string
		last          = map[string]Usage{}
		order         []string
	)
	run := func() Run {
		var u Usage
		for _, s := range order {
			u.InputTokens += last[s].InputTokens
			u.OutputTokens += last[s].OutputTokens
			u.CostUSD += last[s].CostUSD
		}
		return Run{Answer: answer, Usage: u}
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e event
		if err := json.Unmarshal([]byte(line), &e); err != nil || e.Type == "" {
			return run(), fmt.Errorf("runevents: line %d is not a JSON event: %.80q", n, line)
		}
		if root == "" && e.SessionID != "" {
			root = e.SessionID
		}
		switch e.Type {
		case "agent_choice":
			segment.WriteString(e.Content)
		case "message_added":
			// agent_choice events name no session: text ended by another session's message
			// is that session's, never the root's answer.
			if e.SessionID == root {
				answer = segment.String()
			}
			segment.Reset()
		case "token_usage":
			if e.Usage != nil {
				if _, ok := last[e.SessionID]; !ok {
					order = append(order, e.SessionID)
				}
				last[e.SessionID] = Usage{e.Usage.InputTokens, e.Usage.OutputTokens, e.Usage.Cost}
			}
		case "stream_stopped":
			seenStop, stopped = true, e.Reason
		}
	}
	if err := sc.Err(); err != nil {
		return run(), fmt.Errorf("runevents: %w", err)
	}
	if !seenStop {
		return run(), errors.New("runevents: the run has no stream_stopped event")
	}
	if stopped != "normal" {
		return run(), fmt.Errorf("runevents: the run stopped with reason %q, not normal", stopped)
	}
	if answer == "" {
		return run(), errors.New("runevents: the run's last message has no answer")
	}
	return run(), nil
}
