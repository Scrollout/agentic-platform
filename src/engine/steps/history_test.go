package steps_test

// T053e: the engine's adapter to the project worker's version history (check contract commitStep
// and restoreStep), against an httptest fake that speaks the contract. Written before the code.
//
// Seam assumed (the machine side is in machine/history_test.go):
//
//	steps.Config gains ProjectURL and ProjectToken: the project worker's base URL and the token
//	the engine calls it with (`Authorization: Bearer <ProjectToken>`).
//	steps.NewHistory(c steps.Config) machine.History, over the generated check client:
//	  Restore -> POST {ProjectURL}/project-versions/restore?api-version=2026-11-13-preview with
//	    the RestoreRequest (CallCommon): run, step (StepFacts), attempt, round (omitted when 0),
//	    project, before, time_limit_seconds.
//	  Commit -> POST {ProjectURL}/project-versions/commit?api-version=... with the CommitRequest:
//	    run, step, project, before, project_files, accepted_at, time_limit_seconds, and either
//	    attempt (round when not 0) or reused_from with outputs: each machine.Artifact's content
//	    loaded with Config.Load and checked against its hash before anything is sent.
//	  time_limit_seconds is the request's TimeLimit in whole seconds, at most the contract's 600
//	  and at least 1. A 200 answers created (Commit) or nil (Restore), and only when its
//	  project_files equal the request's (project_files for commit, before for restore). Every
//	  other answer is an error naming the HTTP status and the problem code (409
//	  ProjectStateDiffers or HistoryChanged, 504 TimeLimitExceeded, ...), so the run's reason
//	  says why.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/agentic-platform/engine/canonical"
	"github.com/agentic-platform/engine/machine"
	"github.com/agentic-platform/engine/steps"
)

const projTok = "proj-tok-4"

// versionsFake imitates the project worker. answer gives the status and body of each call; the
// default is 200 with the files the request names.
type versionsFake struct {
	rec    recorder
	srv    *httptest.Server
	answer func(path string, body map[string]any) (int, any)
}

func newVersions(t *testing.T) *versionsFake {
	f := &versionsFake{}
	h := func(w http.ResponseWriter, r *http.Request) {
		body := f.rec.add(r)
		code, out := http.StatusOK, any(nil)
		if f.answer != nil {
			code, out = f.answer(r.URL.Path, body)
		}
		if out == nil {
			switch r.URL.Path {
			case "/project-versions/commit":
				out = map[string]any{"commit": strings.Repeat("a", 40), "created": true, "project_files": body["project_files"]}
			default:
				out = map[string]any{"restored": []string{}, "project_files": body["before"]}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if code != http.StatusOK {
			w.Header().Set("Content-Type", "application/problem+json")
		}
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(out)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/project-versions/commit", h)
	mux.HandleFunc("/project-versions/restore", h)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func problem(code string) map[string]any {
	return map[string]any{"type": "about:blank", "title": code, "status": 0, "code": code, "detail": "the detail of " + code}
}

func historyStep() machine.Step {
	return machine.Step{Name: "generate", Worker: "procedure:generate", Inputs: []string{"contract"},
		Outputs: []string{"generated-code"}, Checks: []string{"GeneratedCodeBuilds"},
		Writable: []string{"gen/**", "go.mod", "go.sum", "vendor/**"}}
}

var (
	filesBefore = []machine.File{{Path: "api/openapi.yaml", SHA256: hashA}}
	filesAfter  = []machine.File{{Path: "api/openapi.yaml", SHA256: hashA}, {Path: "gen/api.go", SHA256: hashB}}
)

func historyFor(f *versionsFake, load func(string) ([]byte, error)) machine.History {
	return steps.NewHistory(steps.Config{ProjectURL: f.srv.URL, ProjectToken: projTok, Load: load})
}

func ctx10(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func wireFiles(v any) []machine.File {
	var out []machine.File
	b, _ := json.Marshal(v)
	var raw []map[string]string
	_ = json.Unmarshal(b, &raw)
	for _, f := range raw {
		out = append(out, machine.File{Path: f["path"], SHA256: f["sha256"]})
	}
	return out
}

func equalFiles(a, b []machine.File) bool { return fmt.Sprint(a) == fmt.Sprint(b) }

func checkCall(t *testing.T, c call, path string) {
	t.Helper()
	if c.method != http.MethodPost || c.path != path || c.query != "api-version="+apiVer {
		t.Fatalf("call = %s %s?%s, want POST %s?api-version=%s", c.method, c.path, c.query, path, apiVer)
	}
	if c.auth != "Bearer "+projTok {
		t.Fatalf("Authorization = %q, want the project token", c.auth)
	}
	if !strings.HasPrefix(c.ctype, "application/json") {
		t.Fatalf("Content-Type = %q", c.ctype)
	}
}

func TestHistoryRestoreSendsTheAttemptAndBefore(t *testing.T) {
	f := newVersions(t)
	err := historyFor(f, nil).Restore(ctx10(t), machine.RestoreRequest{Run: "RUN-1", Step: historyStep(), Attempt: 2,
		Project: "proj-a", Before: filesBefore, TimeLimit: 90 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	calls := f.rec.all()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	c := calls[0]
	checkCall(t, c, "/project-versions/restore")
	b := c.body
	step, _ := b["step"].(map[string]any)
	switch {
	case b["run"] != "RUN-1" || b["project"] != "proj-a" || b["attempt"] != float64(2):
		t.Fatalf("body = %v", b)
	case step["name"] != "generate" || fmt.Sprint(step["writable"]) != "[gen/** go.mod go.sum vendor/**]":
		t.Fatalf("step = %v", step)
	case b["round"] != nil:
		t.Fatalf("round sent for a step without rounds: %v", b["round"])
	case !equalFiles(wireFiles(b["before"]), filesBefore):
		t.Fatalf("before = %v", b["before"])
	case b["time_limit_seconds"] != float64(90):
		t.Fatalf("time_limit_seconds = %v, want 90", b["time_limit_seconds"])
	}
}

func TestHistoryRestoreOfAReviewRoundWithAnEmptyBeforeAndALongTimeLimit(t *testing.T) {
	f := newVersions(t)
	review := machine.Step{Name: "review", Worker: "agent:review", Outputs: []string{"findings"},
		Checks: []string{"FindingsCarryEvidence", "NoBlockingFindingOpen"}}
	if err := historyFor(f, nil).Restore(ctx10(t), machine.RestoreRequest{Run: "RUN-1", Step: review, Attempt: 1, Round: 2,
		Project: "proj-a", TimeLimit: 29 * time.Minute}); err != nil {
		t.Fatal(err)
	}
	b := f.rec.all()[0].body
	if b["round"] != float64(2) || b["time_limit_seconds"] != float64(600) {
		t.Fatalf("round %v, time_limit_seconds %v; want 2 and the contract's 600", b["round"], b["time_limit_seconds"])
	}
	if before, ok := b["before"].([]any); !ok || len(before) != 0 {
		t.Fatalf("before = %#v, want an empty list (never null)", b["before"])
	}
}

func TestHistoryCommitOfAnAcceptedStep(t *testing.T) {
	f := newVersions(t)
	at := time.Date(2026, 11, 12, 10, 0, 1, 500_000_000, time.UTC)
	created, err := historyFor(f, nil).Commit(ctx10(t), machine.CommitRequest{Run: "RUN-1", Step: historyStep(), Attempt: 3,
		Project: "proj-a", Before: filesBefore, ProjectFiles: filesAfter, AcceptedAt: at, TimeLimit: 5 * time.Minute})
	if err != nil || !created {
		t.Fatalf("Commit = %v, %v; want created", created, err)
	}
	c := f.rec.all()[0]
	checkCall(t, c, "/project-versions/commit")
	b := c.body
	got, perr := time.Parse(time.RFC3339Nano, fmt.Sprint(b["accepted_at"]))
	switch {
	case b["run"] != "RUN-1" || b["project"] != "proj-a" || b["attempt"] != float64(3):
		t.Fatalf("body = %v", b)
	case b["round"] != nil || b["reused_from"] != nil || b["outputs"] != nil:
		t.Fatalf("round %v reused_from %v outputs %v sent for an accepted attempt", b["round"], b["reused_from"], b["outputs"])
	case !equalFiles(wireFiles(b["before"]), filesBefore) || !equalFiles(wireFiles(b["project_files"]), filesAfter):
		t.Fatalf("before %v project_files %v", b["before"], b["project_files"])
	case perr != nil || !got.Truncate(time.Second).Equal(at.Truncate(time.Second)):
		t.Fatalf("accepted_at = %v, want the event's %v", b["accepted_at"], at)
	case b["time_limit_seconds"] != float64(300):
		t.Fatalf("time_limit_seconds = %v, want 300", b["time_limit_seconds"])
	}
	if s, _ := b["step"].(map[string]any); s["name"] != "generate" {
		t.Fatalf("step = %v", b["step"])
	}
}

func TestHistoryCommitOfAReusedResultSendsTheStoredArtifacts(t *testing.T) {
	f := newVersions(t)
	f.answer = func(_ string, body map[string]any) (int, any) {
		return http.StatusOK, map[string]any{"commit": strings.Repeat("b", 40), "created": false, "project_files": body["project_files"]}
	}
	framed := canonical.Framed([]canonical.File{{Path: "gen/api.go", Content: []byte("package gen\n")}})
	h := hex256(framed)
	load := func(want string) ([]byte, error) {
		if want != h {
			return nil, errors.New("not stored")
		}
		return framed, nil
	}
	created, err := historyFor(f, load).Commit(ctx10(t), machine.CommitRequest{Run: "RUN-2", Step: historyStep(), Round: 0,
		ReusedFrom: "RUN-1", Project: "proj-b", Before: filesBefore, ProjectFiles: filesAfter,
		AcceptedAt: time.Date(2026, 11, 12, 11, 0, 0, 0, time.UTC), TimeLimit: time.Minute,
		Outputs: []machine.Artifact{{Name: "generated-code", SHA256: h}}})
	if err != nil || created {
		t.Fatalf("Commit = %v, %v; want a repeat (created false) and no error", created, err)
	}
	b := f.rec.all()[0].body
	if b["reused_from"] != "RUN-1" || b["attempt"] != nil || b["run"] != "RUN-2" || b["project"] != "proj-b" {
		t.Fatalf("body = %v, want reused_from RUN-1 and no attempt", b)
	}
	outs, _ := b["outputs"].([]any)
	if len(outs) != 1 {
		t.Fatalf("outputs = %v, want one", b["outputs"])
	}
	o, _ := outs[0].(map[string]any)
	content, _ := base64.StdEncoding.DecodeString(fmt.Sprint(o["content"]))
	if o["name"] != "generated-code" || o["sha256"] != h || string(content) != string(framed) {
		t.Fatalf("output = %v, want generated-code with its stored framed form", o)
	}
}

func TestHistoryCommitRefusesAStoredArtifactWithOtherContent(t *testing.T) {
	f := newVersions(t)
	load := func(string) ([]byte, error) { return []byte("other bytes"), nil }
	_, err := historyFor(f, load).Commit(ctx10(t), machine.CommitRequest{Run: "RUN-2", Step: historyStep(), ReusedFrom: "RUN-1",
		Project: "proj-b", Before: filesBefore, ProjectFiles: filesAfter, AcceptedAt: time.Now().UTC(), TimeLimit: time.Minute,
		Outputs: []machine.Artifact{{Name: "generated-code", SHA256: hashC}}})
	if err == nil {
		t.Fatal("Commit sent an artifact whose content does not have its hash")
	}
	if n := len(f.rec.all()); n != 0 {
		t.Fatalf("calls = %d, want none", n)
	}
}

func TestHistoryRefusalsAreErrorsNamingStatusAndCode(t *testing.T) {
	commit := machine.CommitRequest{Run: "RUN-1", Step: historyStep(), Attempt: 1, Project: "proj-a", Before: filesBefore,
		ProjectFiles: filesAfter, AcceptedAt: time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC), TimeLimit: time.Minute}
	restore := machine.RestoreRequest{Run: "RUN-1", Step: historyStep(), Attempt: 1, Project: "proj-a", Before: filesBefore,
		TimeLimit: time.Minute}
	for _, tc := range []struct {
		op, code string
		status   int
	}{
		{"commit", "ProjectStateDiffers", http.StatusConflict},
		{"commit", "HistoryChanged", http.StatusConflict},
		{"commit", "TimeLimitExceeded", http.StatusGatewayTimeout},
		{"restore", "ProjectStateDiffers", http.StatusConflict},
		{"restore", "HistoryChanged", http.StatusConflict},
		{"restore", "TimeLimitExceeded", http.StatusGatewayTimeout},
	} {
		t.Run(tc.op+"/"+tc.code, func(t *testing.T) {
			f := newVersions(t)
			f.answer = func(string, map[string]any) (int, any) {
				p := problem(tc.code)
				p["status"] = tc.status
				return tc.status, p
			}
			var err error
			created := false
			if tc.op == "commit" {
				created, err = historyFor(f, nil).Commit(ctx10(t), commit)
			} else {
				err = historyFor(f, nil).Restore(ctx10(t), restore)
			}
			if err == nil || created {
				t.Fatalf("HTTP %d %s gave created %v, err %v; want an error", tc.status, tc.code, created, err)
			}
			if msg := err.Error(); !strings.Contains(msg, fmt.Sprint(tc.status)) || !strings.Contains(msg, tc.code) {
				t.Fatalf("error %q does not name %d and %s", msg, tc.status, tc.code)
			}
		})
	}
}

func TestHistoryAnswerForOtherFilesIsAnError(t *testing.T) {
	other := []map[string]string{{"path": "gen/other.go", "sha256": hashD}}
	f := newVersions(t)
	f.answer = func(path string, _ map[string]any) (int, any) {
		if path == "/project-versions/commit" {
			return http.StatusOK, map[string]any{"commit": strings.Repeat("c", 40), "created": true, "project_files": other}
		}
		return http.StatusOK, map[string]any{"restored": []string{"gen/other.go"}, "project_files": other}
	}
	h := historyFor(f, nil)
	if _, err := h.Commit(ctx10(t), machine.CommitRequest{Run: "RUN-1", Step: historyStep(), Attempt: 1, Project: "proj-a",
		Before: filesBefore, ProjectFiles: filesAfter, AcceptedAt: time.Now().UTC(), TimeLimit: time.Minute}); err == nil {
		t.Error("a commit answered with other project files was taken as recorded")
	}
	if err := h.Restore(ctx10(t), machine.RestoreRequest{Run: "RUN-1", Step: historyStep(), Attempt: 1, Project: "proj-a",
		Before: filesBefore, TimeLimit: time.Minute}); err == nil {
		t.Error("a restore answered with other project files was taken as done")
	}
}
