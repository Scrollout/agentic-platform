package contracts

// The client of the contract-lint service (src/deploy/lint/server.mjs; T038e, research D28): it
// posts a contract to POST /lint and maps the service's Spectral findings onto LintFinding.
// Every failure is an error, so a lint that cannot run fails the check closed.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/agentic-platform/engine/slices"
)

// maxLintAnswer bounds the service's answer that the client reads.
const maxLintAnswer = 4 << 20

// Lint calls the contract-lint service. It is safe for concurrent use.
type Lint struct {
	base, token string
	// Timeout bounds one call, including reading the answer. The service gives up after 30 s.
	Timeout time.Duration
	client  *http.Client
}

var _ slices.ContractLint = (*Lint)(nil)

// NewLint returns a client of the service at baseURL (for example http://10.0.0.5:8080) that
// sends token as its bearer token.
func NewLint(baseURL, token string) *Lint {
	return &Lint{base: strings.TrimRight(baseURL, "/"), token: token, Timeout: 35 * time.Second, client: &http.Client{Transport: &http.Transport{Proxy: nil}}}
}

// Lint sends the contract and returns its findings. Spectral's "hint" level maps to "info".
func (l *Lint) Lint(ctx context.Context, contract string) ([]slices.LintFinding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, l.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.base+"/lint", strings.NewReader(contract))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+l.token)
	req.Header.Set("Content-Type", "application/yaml")
	resp, err := l.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("contract lint: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxLintAnswer+1))
	if err != nil {
		return nil, fmt.Errorf("contract lint: reading the answer: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("contract lint: status %d", resp.StatusCode)
	}
	if len(body) > maxLintAnswer {
		return nil, fmt.Errorf("contract lint: answer above %d bytes", maxLintAnswer)
	}
	// A JSON array is required: null, an object or anything else is not a finding list.
	if !bytes.HasPrefix(bytes.TrimSpace(body), []byte("[")) {
		return nil, fmt.Errorf("contract lint: answer is not a finding list")
	}
	var raw []struct{ Level, Code, Text, Path string }
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("contract lint: answer is not a finding list: %w", err)
	}
	out := make([]slices.LintFinding, 0, len(raw))
	for _, f := range raw {
		level := f.Level
		switch level {
		case "error", "warn", "info":
		case "hint":
			level = "info"
		default:
			return nil, fmt.Errorf("contract lint: unknown level %q", f.Level)
		}
		out = append(out, slices.LintFinding{Level: level, Code: f.Code, Text: f.Text})
	}
	return out, nil
}
