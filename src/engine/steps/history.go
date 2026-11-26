package steps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/agentic-platform/check/api/gen"
	"github.com/agentic-platform/engine/canonical"
	"github.com/agentic-platform/engine/checksvc"
	"github.com/agentic-platform/engine/machine"
)

// NewHistory returns the machine's History: restoreStep and commitStep at the project worker
// (Config.ProjectURL, called with Config.ProjectToken). It is also a machine.Writer: writeStep at
// the project worker, then readStepFiles at the check service (Config.CheckURL). A reused result's outputs are loaded with
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

// Write makes the step's writable files exactly r.Files with writeStep at the project worker, then
// reads them back with readStepFiles at the check service, and returns the project's files. The
// files read back must be the ones written; any other answer is an error, wrapping
// machine.ErrWriteRefused when a repeat cannot pass it (writeRefused, or a read-back that differs).
func (h *history) Write(ctx context.Context, r machine.WriteRequest) (machine.WriteResult, error) {
	if h.project == nil {
		return machine.WriteResult{}, errors.New("writeStep: no project worker URL")
	}
	if h.check == nil {
		return machine.WriteResult{}, errors.New("readStepFiles: no check service URL")
	}
	files := make([]gen.FileContent, 0, len(r.Files))
	for _, f := range r.Files {
		files = append(files, gen.FileContent{Path: f.Path, Content: f.Content, HashValue: sum(f.Content)})
	}
	limit := historyTimeLimit(r.TimeLimit)
	resp, err := h.project.WriteStepWithResponse(ctx, &gen.WriteStepParams{ApiVersion: checksvc.APIVersion()},
		gen.WriteRequest{Run: r.Run, Project: r.Project, Step: facts(r.Step), Attempt: r.Attempt, Round: roundOf(r.Round),
			Before: projectFiles(r.Before), Files: files, TimeLimitSeconds: limit})
	if err != nil {
		return machine.WriteResult{}, fmt.Errorf("writeStep: %w", err)
	}
	if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
		return machine.WriteResult{}, writeRefusal("writeStep", resp.StatusCode(), resp.Body)
	}
	read, err := h.check.ReadStepFilesWithResponse(ctx, &gen.ReadStepFilesParams{ApiVersion: checksvc.APIVersion()},
		gen.CallCommon{Run: r.Run, Project: r.Project, Step: facts(r.Step), Attempt: r.Attempt, Round: roundOf(r.Round),
			Before: projectFiles(r.Before), TimeLimitSeconds: limit})
	if err != nil {
		return machine.WriteResult{}, fmt.Errorf("readStepFiles: %w", err)
	}
	if read.StatusCode() != http.StatusOK || read.JSON200 == nil {
		return machine.WriteResult{}, writeRefusal("readStepFiles", read.StatusCode(), read.Body)
	}
	got, err := contentFiles(read.JSON200.Files)
	if err != nil {
		return machine.WriteResult{}, fmt.Errorf("readStepFiles: %w", err)
	}
	if canonical.FramedHash(got) != canonical.FramedHash(r.Files) {
		return machine.WriteResult{}, fmt.Errorf("readStepFiles: the step's files are not the ones writeStep was given: %w",
			machine.ErrWriteRefused)
	}
	return machine.WriteResult{Written: nonNil(resp.JSON200.Written), Removed: nonNil(resp.JSON200.Removed),
		ProjectFiles: machineFiles(read.JSON200.ProjectFiles)}, nil
}

// writeRefusal is the error of a writeStep or readStepFiles answer that is not a 200 for the
// request's files: it names the status and the problem code, and wraps machine.ErrWriteRefused
// when writeRefused says a repeat cannot pass it.
func writeRefusal(call string, status int, body []byte) error {
	if writeRefused(status, body) {
		return fmt.Errorf("%s: %s: %w", call, refusal(status, body), machine.ErrWriteRefused)
	}
	return fmt.Errorf("%s: %s", call, refusal(status, body))
}

// writeRefused says whether a write's answer is one the developer must fix, so a repeat cannot
// pass it (T040, the owner's decision of 2026-11-26): 400, 401, 404 and 422 (any code), 409
// HistoryChanged and 500 ResponseTooLarge. Every other answer (409 ProjectStateDiffers, 429,
// 500 WriteFailed, 503, 504, a 409 or 500 with no problem code, any other status) is one a repeat
// can pass.
func writeRefused(status int, body []byte) bool {
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusNotFound, http.StatusUnprocessableEntity:
		return true
	case http.StatusConflict:
		return problemCode(body) == "HistoryChanged"
	case http.StatusInternalServerError:
		return problemCode(body) == "ResponseTooLarge"
	}
	return false
}

// problemCode is the code of a Problem body, "" when the body is none.
func problemCode(body []byte) string {
	var p gen.Problem
	if json.Unmarshal(body, &p) != nil {
		return ""
	}
	return string(p.Code)
}
