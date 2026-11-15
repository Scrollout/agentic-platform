package steps

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/agentic-platform/check/api/gen"
	"github.com/agentic-platform/engine/checksvc"
	"github.com/agentic-platform/engine/machine"
)

// NewHistory returns the machine's History: restoreStep and commitStep at the project worker
// (Config.ProjectURL, called with Config.ProjectToken). A reused result's outputs are loaded with
// Config.Load and checked against their hashes before anything is sent. Every answer but a 200
// for the request's files is an error naming the HTTP status and the problem code.
func NewHistory(c Config) machine.History {
	a := newAdapter(c)
	h := &history{adapter: a}
	h.project, _ = newClient(c.ProjectURL, c.ProjectToken, a.hc)
	return h
}

type history struct {
	*adapter
	project *gen.ClientWithResponses
}

// historyTimeLimit is time_limit_seconds for a History call: the request's time limit in whole
// seconds, at most the contract's maximum and at least one.
func historyTimeLimit(d time.Duration) int {
	return min(max(int(d/time.Second), 1), checksvc.CallTimeLimitMax())
}

func (h *history) Restore(ctx context.Context, r machine.RestoreRequest) error {
	if h.project == nil {
		return errors.New("restoreStep: no project worker URL")
	}
	resp, err := h.project.RestoreStepWithResponse(ctx, &gen.RestoreStepParams{ApiVersion: checksvc.APIVersion()},
		gen.RestoreRequest{Run: r.Run, Project: r.Project, Step: facts(r.Step), Attempt: r.Attempt, Round: roundOf(r.Round),
			Before: projectFiles(r.Before), TimeLimitSeconds: historyTimeLimit(r.TimeLimit)})
	if err != nil {
		return fmt.Errorf("restoreStep: %w", err)
	}
	if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
		return fmt.Errorf("restoreStep: %s", refusal(resp.StatusCode(), resp.Body))
	}
	if !slices.Equal(machineFiles(resp.JSON200.ProjectFiles), nonNilFiles(r.Before)) {
		return errors.New("restoreStep: the project worker answered with other project files than before")
	}
	return nil
}

func (h *history) Commit(ctx context.Context, r machine.CommitRequest) (bool, error) {
	if h.project == nil {
		return false, errors.New("commitStep: no project worker URL")
	}
	body := gen.CommitRequest{Run: r.Run, Project: r.Project, Step: facts(r.Step), Round: roundOf(r.Round),
		Before: projectFiles(r.Before), ProjectFiles: projectFiles(r.ProjectFiles), AcceptedAt: r.AcceptedAt.UTC(),
		TimeLimitSeconds: historyTimeLimit(r.TimeLimit)}
	if r.ReusedFrom != "" {
		from := r.ReusedFrom
		body.ReusedFrom = &from
		outs := []gen.ReusedOutput{}
		for _, o := range r.Outputs {
			if h.c.Load == nil {
				return false, errors.New("commitStep: no source of stored outputs")
			}
			b, err := h.c.Load(o.SHA256)
			if err != nil {
				return false, fmt.Errorf("commitStep: output %s: %w", o.Name, err)
			}
			if sum(b) != o.SHA256 {
				return false, fmt.Errorf("commitStep: output %s: the stored content does not have its hash", o.Name)
			}
			outs = append(outs, gen.ReusedOutput{Name: o.Name, HashValue: o.SHA256, Content: b})
		}
		body.Outputs = &outs
	} else {
		attempt := r.Attempt
		body.Attempt = &attempt
	}
	if r.OpenRound { // an open review round's commit says so, live or reused (check contract, CommitRequest)
		open := true
		body.OpenRound = &open
	}
	resp, err := h.project.CommitStepWithResponse(ctx, &gen.CommitStepParams{ApiVersion: checksvc.APIVersion()}, body)
	if err != nil {
		return false, fmt.Errorf("commitStep: %w", err)
	}
	if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
		return false, fmt.Errorf("commitStep: %s", refusal(resp.StatusCode(), resp.Body))
	}
	if !slices.Equal(machineFiles(resp.JSON200.ProjectFiles), nonNilFiles(r.ProjectFiles)) {
		return false, errors.New("commitStep: the project worker answered with other project files than the step's")
	}
	return resp.JSON200.Created, nil
}
