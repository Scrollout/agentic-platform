package contracts_test

// Tests for the Spectral lint client (T038e, research D28). An httptest server stands in for the
// lint service. Every failure must be an error: a lint that cannot run fails closed.

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agentic-platform/engine/contracts"
	"github.com/agentic-platform/engine/slices"
)

var _ slices.ContractLint = (*contracts.Lint)(nil)

const lintDoc = "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\n"

func lintServer(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return s
}

func TestLintSendsContractWithBearerToken(t *testing.T) {
	var method, path, auth, body string
	s := lintServer(t, func(w http.ResponseWriter, r *http.Request) {
		method, path, auth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
	})
	got, err := contracts.NewLint(s.URL, "s3cret").Lint(context.Background(), lintDoc)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("findings = %v, want none", got)
	}
	if method != http.MethodPost || path != "/lint" {
		t.Errorf("request = %s %s, want POST /lint", method, path)
	}
	if auth != "Bearer s3cret" {
		t.Errorf("Authorization = %q", auth)
	}
	if body != lintDoc {
		t.Errorf("body = %q, want the contract text unchanged", body)
	}
}

func TestLintBaseURLWithTrailingSlash(t *testing.T) {
	var path string
	s := lintServer(t, func(w http.ResponseWriter, r *http.Request) { path = r.URL.Path; w.Write([]byte("[]")) })
	if _, err := contracts.NewLint(s.URL+"/", "t").Lint(context.Background(), lintDoc); err != nil {
		t.Fatal(err)
	}
	if path != "/lint" {
		t.Errorf("path = %q, want /lint", path)
	}
}

func TestLintMapsFindings(t *testing.T) {
	s := lintServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[
		 {"level":"error","code":"path-params","text":"Path parameter \"id\" is not defined","path":"paths./a/{id}"},
		 {"level":"warn","code":"operation-description","text":"Operation must have description","path":"paths./a.get"},
		 {"level":"info","code":"i","text":"an info","path":""},
		 {"level":"hint","code":"h","text":"a hint","path":""}]`))
	})
	got, err := contracts.NewLint(s.URL, "t").Lint(context.Background(), lintDoc)
	if err != nil {
		t.Fatal(err)
	}
	want := []slices.LintFinding{
		{Level: "error", Code: "path-params", Text: `Path parameter "id" is not defined`},
		{Level: "warn", Code: "operation-description", Text: "Operation must have description"},
		{Level: "info", Code: "i", Text: "an info"},
		{Level: "info", Code: "h", Text: "a hint"}, // hint maps to info
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("finding %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestLintFailsClosed(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"401":         func(w http.ResponseWriter, r *http.Request) { http.Error(w, `{"error":"unauthorized"}`, 401) },
		"413":         func(w http.ResponseWriter, r *http.Request) { http.Error(w, `{}`, 413) },
		"500":         func(w http.ResponseWriter, r *http.Request) { http.Error(w, `{}`, 500) },
		"404":         func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) },
		"204 no body": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) },
		"not JSON":    func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("<html>")) },
		"JSON object": func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"level":"error"}`)) },
		"empty body":  func(w http.ResponseWriter, r *http.Request) {},
		"truncated":   func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`[{"level":"err`)) },
		"unknown level": func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`[{"level":"fatal","code":"c","text":"t","path":""}]`))
		},
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			s := lintServer(t, h)
			got, err := contracts.NewLint(s.URL, "t").Lint(context.Background(), lintDoc)
			if err == nil {
				t.Fatalf("no error; findings %v", got)
			}
		})
	}
}

func TestLintConnectionRefused(t *testing.T) {
	s := httptest.NewServer(http.NotFoundHandler())
	url := s.URL
	s.Close()
	if _, err := contracts.NewLint(url, "t").Lint(context.Background(), lintDoc); err == nil {
		t.Fatal("no error for a refused connection")
	}
}

func TestLintTimeout(t *testing.T) {
	release := make(chan struct{})
	s := lintServer(t, func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	defer close(release)
	l := contracts.NewLint(s.URL, "t")
	l.Timeout = 100 * time.Millisecond
	start := time.Now()
	if _, err := l.Lint(context.Background(), lintDoc); err == nil {
		t.Fatal("no error on timeout")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("took %v, the Timeout field was ignored", d)
	}
}

func TestLintContextCancellation(t *testing.T) {
	s := lintServer(t, func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body) // the server notices a disconnect only after the body is read
		select {
		case <-r.Context().Done():
		case <-time.After(30 * time.Second):
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()
	done := make(chan error, 1)
	go func() {
		_, err := contracts.NewLint(s.URL, "t").Lint(ctx, lintDoc)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("no error after cancel")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation did not abort the request")
	}
}

func TestLintCancelledBeforeSending(t *testing.T) {
	var hits atomic.Int32
	s := lintServer(t, func(w http.ResponseWriter, r *http.Request) { hits.Add(1); w.Write([]byte("[]")) })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := contracts.NewLint(s.URL, "t").Lint(ctx, lintDoc); err == nil {
		t.Fatal("no error for a cancelled context")
	}
	if hits.Load() != 0 {
		t.Error("a request was sent with a cancelled context")
	}
}

func TestLintConcurrentUse(t *testing.T) {
	s := lintServer(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		w.Write([]byte(`[{"level":"warn","code":"c","text":"` + strings.TrimSpace(string(b)) + `","path":""}]`))
	})
	l := contracts.NewLint(s.URL, "t")
	errs := make(chan string, 20)
	for i := 0; i < 20; i++ {
		go func() {
			got, err := l.Lint(context.Background(), "same")
			if err != nil || len(got) != 1 || got[0].Text != "same" {
				errs <- "bad"
				return
			}
			errs <- ""
		}()
	}
	for i := 0; i < 20; i++ {
		if e := <-errs; e != "" {
			t.Fatal("concurrent Lint gave a wrong result")
		}
	}
}
