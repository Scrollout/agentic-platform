package steps_test

// Feature 011, T040 (the `Write` cases): which answers of writeStep and readStepFiles make the
// History's Write wrap machine.ErrWriteRefused (a refusal the developer must fix: the run hands
// over) and which do not (one a repeat can pass: the run is interrupted). Judged from T040 and the
// owner's decision of 2026-11-26 under it, the check contract's writeStep and readStepFiles, and
// the Writer and ErrWriteRefused doc comments in machine/writer.go. Nothing here changes an
// existing test, helper or fixture; it calls the helpers of history_test.go and adapters_test.go.
//
// Unlisted answers are not tested, because the task does not settle them: an unknown 4xx or 5xx
// code, a status with no Problem body, a 200 with no body, 400 for a missing or unsupported
// api-version, and a readStepFiles answer that fails (as opposed to one that differs).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agentic-platform/engine/canonical"
	"github.com/agentic-platform/engine/machine"
	"github.com/agentic-platform/engine/steps"
)

const writeCheckTok = "check-tok-write"

// writeFake is a project worker (writeStep) and a check service (readStepFiles) in one test.
// writeAnswer and readAnswer give the status and body of each; nil means the contract's 200 for
// the request's files.
type writeFake struct {
	project, check *httptest.Server
	writeAnswer    func(body map[string]any) (int, any)
	readAnswer     func(body map[string]any) (int, any)
	writeCalls     atomic.Int32
	readCalls      atomic.Int32
}

func newWriteFake(t *testing.T) *writeFake {
	f := &writeFake{}
	reply := func(w http.ResponseWriter, code int, out any) {
		w.Header().Set("Content-Type", "application/json")
		if code != http.StatusOK {
			w.Header().Set("Content-Type", "application/problem+json")
		}
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(out)
	}
	pm := http.NewServeMux()
	pm.HandleFunc("/project-versions/write", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.writeCalls.Add(1)
		if f.writeAnswer != nil {
			code, out := f.writeAnswer(body)
			reply(w, code, out)
			return
		}
		paths := []string{}
		files, _ := body["files"].([]any)
		for _, x := range files {
			m, _ := x.(map[string]any)
			paths = append(paths, fmt.Sprint(m["path"]))
		}
		reply(w, http.StatusOK, map[string]any{"written": paths, "removed": []string{}})
	})
	cm := http.NewServeMux()
	cm.HandleFunc("/step-files", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.readCalls.Add(1)
		if f.readAnswer != nil {
			code, out := f.readAnswer(body)
			reply(w, code, out)
			return
		}
		reply(w, http.StatusOK, stepFilesOf(writeFiles(), filesAfter))
	})
	f.project, f.check = httptest.NewServer(pm), httptest.NewServer(cm)
	t.Cleanup(f.project.Close)
	t.Cleanup(f.check.Close)
	return f
}

func writeFiles() []canonical.File {
	return []canonical.File{{Path: "gen/api.go", Content: []byte("package gen\n")}, {Path: "gen/types.go", Content: []byte("package gen\n// types\n")}}
}

// stepFilesOf is readStepFiles' 200 for files and the project's files afterwards.
func stepFilesOf(files []canonical.File, project []machine.File) map[string]any {
	fc := []map[string]any{}
	for _, f := range files {
		fc = append(fc, map[string]any{"path": f.Path, "content": f.Content, "sha256": sum(f.Content)})
	}
	pf := []map[string]string{}
	for _, f := range project {
		pf = append(pf, map[string]string{"path": f.Path, "sha256": f.SHA256})
	}
	return map[string]any{"sha256": hex256(canonical.Framed(files)), "files": fc, "removed": []string{}, "project_files": pf}
}

func (f *writeFake) writer(t *testing.T) machine.Writer {
	t.Helper()
	h := steps.NewHistory(steps.Config{ProjectURL: f.project.URL, ProjectToken: projTok, CheckURL: f.check.URL, CheckToken: writeCheckTok})
	w, ok := h.(machine.Writer)
	if !ok {
		t.Fatal("the History steps.NewHistory returns is not a machine.Writer")
	}
	return w
}

func writeRequest() machine.WriteRequest {
	return machine.WriteRequest{Run: "RUN-1", Step: historyStep(), Attempt: 1, Project: "proj-a", Before: filesBefore,
		Files: writeFiles(), TimeLimit: time.Minute}
}

func problemWith(status int, code string) map[string]any {
	p := problem(code)
	p["status"] = status
	return p
}

// Regression guard (passes now): a write that is answered 200 and read back equal is no error.
func TestWriteOfAFilledStepThatReadsBackEqualIsNoError(t *testing.T) {
	f := newWriteFake(t)
	res, err := f.writer(t).Write(ctx10(t), writeRequest())
	if err != nil {
		t.Fatalf("Write = %v, want no error", err)
	}
	if f.writeCalls.Load() != 1 || f.readCalls.Load() != 1 {
		t.Fatalf("write calls %d, read calls %d, want one each", f.writeCalls.Load(), f.readCalls.Load())
	}
	if len(res.Written) != 2 || len(res.ProjectFiles) != len(filesAfter) {
		t.Fatalf("result = %+v", res)
	}
}

// T040 and the owner's decision: the writeStep answers that name a hand-over (a refusal the
// developer must fix) give an error that wraps machine.ErrWriteRefused; the ones that name an
// interrupt (a repeat can pass) give one that does not. One row per code, as the decision lists.
// The interrupt rows already pass: they are regression guards.
func TestWriteWrapsErrWriteRefusedOnlyForAnswersNamedAsAHandOver(t *testing.T) {
	for _, tc := range []struct {
		status  int
		code    string
		refused bool
	}{
		// hand over (T040 text: 400 outside the writable entries, 422 StepFilesTooLarge,
		// InputHashMismatch; the owner's decision: 422 UnreadableProject, 409 HistoryChanged, 401,
		// 404, 500 ResponseTooLarge)
		{http.StatusBadRequest, "InvalidRequest", true},
		{http.StatusUnauthorized, "Unauthorized", true},
		{http.StatusNotFound, "VersionHistoryNotServed", true},
		{http.StatusConflict, "HistoryChanged", true},
		{http.StatusUnprocessableEntity, "UnreadableProject", true},
		{http.StatusUnprocessableEntity, "InputHashMismatch", true},
		{http.StatusUnprocessableEntity, "StepFilesTooLarge", true},
		{http.StatusInternalServerError, "ResponseTooLarge", true},
		// interrupt (T040 text: 503, 504, ProjectStateDiffers; the decision: 429, 500 WriteFailed)
		{http.StatusConflict, "ProjectStateDiffers", false},
		{http.StatusTooManyRequests, "RateLimited", false},
		{http.StatusInternalServerError, "WriteFailed", false},
		{http.StatusServiceUnavailable, "Busy", false},
		{http.StatusGatewayTimeout, "TimeLimitExceeded", false},
	} {
		t.Run(fmt.Sprintf("%d %s", tc.status, tc.code), func(t *testing.T) {
			f := newWriteFake(t)
			f.writeAnswer = func(map[string]any) (int, any) { return tc.status, problemWith(tc.status, tc.code) }
			_, err := f.writer(t).Write(ctx10(t), writeRequest())
			if err == nil {
				t.Fatalf("HTTP %d %s gave no error", tc.status, tc.code)
			}
			if got := errors.Is(err, machine.ErrWriteRefused); got != tc.refused {
				t.Errorf("HTTP %d %s: errors.Is(err, ErrWriteRefused) = %v, want %v; err: %v", tc.status, tc.code, got, tc.refused, err)
			}
			if msg := err.Error(); !strings.Contains(msg, fmt.Sprint(tc.status)) || !strings.Contains(msg, tc.code) {
				t.Errorf("the error %q must still name %d and %s, so the run's reason says why (history_test.go)", msg, tc.status, tc.code)
			}
			if f.readCalls.Load() != 0 {
				t.Errorf("readStepFiles was called after a refused write (%d calls)", f.readCalls.Load())
			}
		})
	}
}

// T040 / the owner's decision, a transport error: the project worker cannot be reached (its
// server is closed): an interrupt, so the error does not wrap ErrWriteRefused. Regression guard
// (passes now).
func TestWriteTransportErrorToTheProjectWorkerDoesNotWrapErrWriteRefused(t *testing.T) {
	f := newWriteFake(t)
	w := f.writer(t)
	f.project.Close()
	_, err := w.Write(ctx10(t), writeRequest())
	if err == nil {
		t.Fatal("Write to a closed project worker gave no error")
	}
	if errors.Is(err, machine.ErrWriteRefused) {
		t.Errorf("a transport error wraps ErrWriteRefused: %v", err)
	}
}

// T040 / the owner's decision, a read-back (readStepFiles) that differs from the files written
// hands over: wraps ErrWriteRefused. Rows: other content, a missing file, an extra file, another
// path, and none for a non-empty write.
func TestWriteWrapsErrWriteRefusedWhenTheReadBackDiffersFromTheFilesWritten(t *testing.T) {
	good := writeFiles()
	for name, read := range map[string][]canonical.File{
		"other content":  {{Path: good[0].Path, Content: []byte("package other\n")}, good[1]},
		"a file missing": {good[0]},
		"an extra file":  append(append([]canonical.File{}, good...), canonical.File{Path: "gen/extra.go", Content: []byte("x")}),
		"another path":   {{Path: "gen/other.go", Content: good[0].Content}, good[1]},
		"no file at all": {},
	} {
		t.Run(name, func(t *testing.T) {
			f := newWriteFake(t)
			f.readAnswer = func(map[string]any) (int, any) { return http.StatusOK, stepFilesOf(read, filesAfter) }
			_, err := f.writer(t).Write(ctx10(t), writeRequest())
			if err == nil {
				t.Fatal("a read-back that differs from the files written was accepted")
			}
			if !errors.Is(err, machine.ErrWriteRefused) {
				t.Errorf("read-back %q: the error does not wrap ErrWriteRefused: %v", name, err)
			}
		})
	}
}

// T040 edge: an empty write (writeStep's "files may be empty") that reads back files differs too.
func TestWriteOfNoFilesWhoseReadBackHoldsFilesWrapsErrWriteRefused(t *testing.T) {
	f := newWriteFake(t)
	f.readAnswer = func(map[string]any) (int, any) { return http.StatusOK, stepFilesOf(writeFiles(), filesAfter) }
	r := writeRequest()
	r.Files = nil
	if _, err := f.writer(t).Write(ctx10(t), r); !errors.Is(err, machine.ErrWriteRefused) {
		t.Errorf("err = %v, want one wrapping ErrWriteRefused", err)
	}
}

// Regression guard (passes now): an empty write read back empty is no error.
func TestWriteOfNoFilesThatReadsBackEmptyIsNoError(t *testing.T) {
	f := newWriteFake(t)
	f.readAnswer = func(map[string]any) (int, any) { return http.StatusOK, stepFilesOf(nil, filesBefore) }
	r := writeRequest()
	r.Files = nil
	if _, err := f.writer(t).Write(context.Background(), r); err != nil {
		t.Errorf("err = %v, want none", err)
	}
}
