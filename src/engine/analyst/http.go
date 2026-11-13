// Package analyst is the engine's client of the analyst service: the pinned Docker Agent running
// `serve api` with the analyst's definition (research D27). It implements slices.Analyst: one
// session per call (package agentcall).
package analyst

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/agentic-platform/engine/agentcall"
	"github.com/agentic-platform/engine/slices"
)

const (
	// callTimeout bounds one Propose: just above the agent's own budget (max_time 5m in
	// src/agents/analyst.yaml), so the agent stops first and says why.
	callTimeout = 330 * time.Second
	// sessionTitle is sent with every session: without a title the service makes a second model
	// call to name the session, which costs money and sends the description to the model again.
	sessionTitle = "ap-analyst"
	agentName    = "analyst" // the definition's file name without .yaml
)

// HTTP calls the analyst service. It is safe for concurrent use; each call has its own session.
type HTTP struct {
	digest string
	client *agentcall.Client
}

// NewHTTP returns the client of the analyst service at baseURL (for example
// "http://analyst:8080"), authenticating with the service's bearer token. digest is the agent
// definition's digest, which every proposal names as its AgentDigest; the service does not report it.
func NewHTTP(baseURL, token, digest string) *HTTP {
	return &HTTP{digest: digest, client: agentcall.New(baseURL, token)}
}

var _ slices.Analyst = (*HTTP)(nil)

// Propose sends slices.AnalystMessage(in) to a new session and returns the last structured answer
// as the proposal, with the model the service reports and the client's digest. It fails unless
// the stream ends with stream_stopped and reason normal, without an error event. It never retries:
// slices.Service decides whether to ask again. The session is deleted however the call ends.
func (a *HTTP) Propose(ctx context.Context, in slices.AnalystInput) (slices.Proposal, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	ans, err := a.client.Ask(ctx, agentName, sessionTitle, slices.AnalystMessage(in))
	if err != nil {
		return slices.Proposal{}, fmt.Errorf("analyst: %w", err)
	}
	var p slices.Proposal
	if err := json.Unmarshal([]byte(ans.Content), &p); err != nil {
		return slices.Proposal{}, fmt.Errorf("analyst: the answer is not a proposal: %v", err)
	}
	p.Model, p.AgentDigest = ans.Model, a.digest
	return p, nil
}
