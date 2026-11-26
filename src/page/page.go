// Package page serves the read-only run page (FR-024, research D18): a developer reads a run's
// record and the differences between two runs in a browser. It calls only the engine API's GET
// operations, with its own token, and accepts only GET requests whose Host header is the one it
// was configured with (the DNS rebinding guard of the plan's threat model).
package page

import (
	"context"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/agentic-platform/page/api/gen"
)

// apiVersion is the engine contract version the page is built against.
const apiVersion = "2026-11-12-preview"

// maxPages bounds how many pages of one list the page reads from the engine for one view.
const maxPages = 50

// engineTimeout bounds one engine call.
const engineTimeout = 10 * time.Second

// Config is what the page needs: the engine's base URL, the page's own token for it, and the one
// Host header (name and port, such as "127.0.0.1:8123") the page answers; "localhost" with the
// same port is accepted too.
type Config struct{ EngineURL, Token, Host string }

// NewHandler returns the page's handler. GET /runs/{runId} shows a run's events in order; GET
// /compare?run={a}&with={b} shows how run a differs from run b. Any other method gets 405, any
// other Host 403, both before the engine is called.
func NewHandler(c Config) http.Handler {
	allowed := map[string]bool{c.Host: true}
	if _, port, err := net.SplitHostPort(c.Host); err == nil {
		allowed[net.JoinHostPort("localhost", port)] = true
	}
	bearer := func(_ context.Context, r *http.Request) error {
		r.Header.Set("Authorization", "Bearer "+c.Token)
		return nil
	}
	client, err := gen.NewClientWithResponses(c.EngineURL,
		gen.WithHTTPClient(&http.Client{Timeout: engineTimeout}), gen.WithRequestEditorFn(bearer))

	mux := http.NewServeMux()
	mux.HandleFunc("/runs/{runId}", func(w http.ResponseWriter, r *http.Request) {
		runEvents(w, r, client)
	})
	mux.HandleFunc("/compare", func(w http.ResponseWriter, r *http.Request) {
		comparison(w, r, client)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fail(w, http.StatusNotFound, "There is no such page. Open /runs/{run ID} or /compare?run={run ID}&with={run ID}.")
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; form-action 'none'; base-uri 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		if !allowed[r.Host] {
			fail(w, http.StatusForbidden, "This page answers only at its own address.")
			return
		}
		if r.Method != http.MethodGet {
			h.Set("Allow", http.MethodGet)
			fail(w, http.StatusMethodNotAllowed, "This page is read-only: only GET is allowed.")
			return
		}
		if err != nil {
			fail(w, http.StatusInternalServerError, "The page's engine address is not usable.")
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func runEvents(w http.ResponseWriter, r *http.Request, client *gen.ClientWithResponses) {
	id := r.PathValue("runId")
	var events []gen.RunEventSchema
	var token *string
	for range maxPages {
		resp, err := client.ListRunEventsWithResponse(r.Context(), id,
			&gen.ListRunEventsParams{ApiVersion: apiVersion, PageToken: token})
		var hr *http.Response
		if resp != nil {
			hr = resp.HTTPResponse
		}
		if status, msg := engineFailure(err, hr, resp != nil && resp.JSON200 != nil); status != 0 {
			fail(w, status, msg)
			return
		}
		events = append(events, resp.JSON200.Items...)
		if token = resp.JSON200.NextPageToken; token == nil || *token == "" {
			render(w, "run", map[string]any{"Run": id, "Events": events})
			return
		}
	}
	render(w, "run", map[string]any{"Run": id, "Events": events, "Truncated": maxPages})
}

func comparison(w http.ResponseWriter, r *http.Request, client *gen.ClientWithResponses) {
	q := r.URL.Query()
	a, b := q.Get("run"), q.Get("with")
	if a == "" || b == "" {
		fail(w, http.StatusBadRequest, "Name both runs: /compare?run={run ID}&with={run ID}.")
		return
	}
	var diffs []gen.Difference
	var token *string
	for range maxPages {
		resp, err := client.CompareRunsWithResponse(r.Context(), a,
			&gen.CompareRunsParams{ApiVersion: apiVersion, With: b, PageToken: token})
		var hr *http.Response
		if resp != nil {
			hr = resp.HTTPResponse
		}
		if status, msg := engineFailure(err, hr, resp != nil && resp.JSON200 != nil); status != 0 {
			fail(w, status, msg)
			return
		}
		diffs = append(diffs, resp.JSON200.Items...)
		if token = resp.JSON200.NextPageToken; token == nil || *token == "" {
			render(w, "compare", map[string]any{"Run": a, "With": b, "Differences": diffs})
			return
		}
	}
	render(w, "compare", map[string]any{"Run": a, "With": b, "Differences": diffs, "Truncated": maxPages})
}

// engineFailure turns an engine call (its error, response, and whether a 200 list was read) into
// the page's status and message; status 0 means the list can be shown. The engine's own text is
// not shown.
func engineFailure(err error, resp *http.Response, listRead bool) (int, string) {
	if err != nil || resp == nil {
		return http.StatusBadGateway, "The engine could not be reached or its answer could not be read."
	}
	switch code := resp.StatusCode; {
	case code == http.StatusOK && listRead:
		return 0, ""
	case code == http.StatusOK:
		return http.StatusBadGateway, "The engine's answer could not be read."
	case code == http.StatusNotFound:
		return http.StatusNotFound, "The engine has no such run."
	case code == http.StatusBadRequest:
		return http.StatusBadRequest, "The engine refused the request: check the run IDs."
	case code == http.StatusTooManyRequests:
		return http.StatusServiceUnavailable, "The engine is busy; try again in a moment."
	default:
		return http.StatusBadGateway, "The engine could not answer."
	}
}

func fail(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	_ = pages.ExecuteTemplate(w, "error", map[string]any{"Status": status, "Message": message})
}

func render(w http.ResponseWriter, name string, data map[string]any) {
	_ = pages.ExecuteTemplate(w, name, data)
}

// pages are the page's views. html/template escapes every value: event and comparison text come
// from the run store and are untrusted. They hold no forms, inputs, buttons or scripts.
var pages = template.Must(template.New("").Funcs(template.FuncMap{
	"text": func(s *string) string {
		if s == nil {
			return "(none)"
		}
		return *s
	},
	"record": record,
	"joined": func(refs *[]gen.Ref) string {
		if refs == nil {
			return ""
		}
		return strings.Join(*refs, ",")
	},
}).Parse(`
{{define "head"}}<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>{{.}}</title>
<style>body{font-family:sans-serif;margin:1rem}table{border-collapse:collapse}td,th{border:1px solid #999;padding:.2rem .5rem;text-align:left;vertical-align:top}</style>
</head><body>{{end}}

{{define "truncated"}}{{if .Truncated}}<p><strong>Only the first {{.Truncated}} pages are shown.</strong></p>{{end}}{{end}}

{{define "run"}}{{template "head" (print "Run " .Run)}}
<h1>Run {{.Run}}</h1>
{{if .Events}}<table>
<tr><th>Seq</th><th>At</th><th>Kind</th><th>Step</th><th>Attempt</th><th>Round</th><th>Check</th><th>Finding</th><th>Record</th><th>Refs</th><th>Detail</th></tr>
{{range .Events}}<tr><td>{{.Seq}}</td><td>{{.At.UTC.Format "2006-01-02 15:04:05Z"}}</td><td>{{.Kind}}</td><td>{{with .Step}}{{.}}{{end}}</td><td>{{with .Attempt}}{{.}}{{end}}</td><td>{{with .Round}}{{.}}{{end}}</td><td>{{with .Check}}{{.Name}}: {{if .Passed}}passed{{else}}failed{{end}}{{with .Reason}} ({{.}}){{end}}{{end}}</td><td>{{with .Finding}}{{.Rule}}{{if .Blocking}} (blocking){{else}} (suggestion){{end}}: {{.Text}}{{with .File}} at {{.}}{{end}}{{with .Line}}:{{.}}{{end}}{{with .Check}} check {{.}}{{end}}{{with .Witness}} witness {{.File}} {{.Test}}{{end}}{{end}}</td><td>{{record .}}</td><td>{{joined .Refs}}</td><td>{{with .Detail}}{{.}}{{end}}</td></tr>
{{end}}</table>{{else}}<p>The run has no events.</p>{{end}}
{{template "truncated" .}}
</body></html>{{end}}

{{define "compare"}}{{template "head" (print "Run " .Run " compared with " .With)}}
<h1>Run {{.Run}} compared with run {{.With}}</h1>
{{if .Differences}}<table>
<tr><th>Step</th><th>Field</th><th>{{.Run}}</th><th>{{.With}}</th></tr>
{{range .Differences}}<tr><td>{{.Step}}</td><td>{{.Field}}</td><td>{{text .Left}}</td><td>{{text .Right}}</td></tr>
{{end}}</table>{{else}}<p>The runs do not differ.</p>{{end}}
{{template "truncated" .}}
</body></html>{{end}}

{{define "error"}}{{template "head" (print "Error " .Status)}}
<h1>Error {{.Status}}</h1>
<p>{{.Message}}</p>
</body></html>{{end}}
`))

// record is the fields of an event's kind, as the terminal's event lines give them
// (src/cli/contract.md, "Event lines"): each field the event carries, labelled, separated by two
// spaces, hashes in full and lists comma-separated in their recorded order; empty when it
// carries none. The template escapes it.
func record(e gen.RunEventSchema) string {
	var f []string
	add := func(label, value string) { f = append(f, label+" "+value) }
	if e.Slice != nil {
		add("slice", *e.Slice)
	}
	if e.Pattern != nil {
		add("pattern", *e.Pattern)
	}
	if e.PatternReason != nil && *e.PatternReason != "" {
		add("reason", string(*e.PatternReason))
	}
	if e.Proof != nil && bool(*e.Proof) {
		f = append(f, "proof")
	}
	if e.SourceRun != nil {
		add("source", *e.SourceRun)
	}
	if e.Parameters != nil && len(*e.Parameters) > 0 {
		names := make([]string, 0, len(*e.Parameters))
		for n := range *e.Parameters {
			names = append(names, n)
		}
		sort.Strings(names) // byte order, as the terminal's line gives them
		pairs := make([]string, len(names))
		for i, n := range names {
			pairs[i] = n + "=" + string((*e.Parameters)[n])
		}
		add("parameters", strings.Join(pairs, ","))
	}
	if e.Worker != nil {
		add("worker", *e.Worker)
	}
	if e.Model != nil {
		add("model", *e.Model)
	}
	if e.AgentDigest != nil {
		add("agent", *e.AgentDigest)
	}
	if e.Inputs != nil && len(*e.Inputs) > 0 {
		add("inputs", strings.Join(*e.Inputs, ","))
	}
	if e.Artifacts != nil && len(*e.Artifacts) > 0 {
		add("artifacts", strings.Join(*e.Artifacts, ","))
	}
	if e.ReuseKey != nil {
		add("reuse", *e.ReuseKey)
	}
	if e.OpenRound != nil && bool(*e.OpenRound) {
		f = append(f, "open round")
	}
	if e.ChangedLines != nil && len(*e.ChangedLines) > 0 {
		lines := make([]string, len(*e.ChangedLines))
		for i, c := range *e.ChangedLines {
			lines[i] = fmt.Sprintf("%s:%d", c.File, c.Line)
		}
		add("changed", strings.Join(lines, ","))
	}
	if e.Usage != nil {
		add("tokens", fmt.Sprint(e.Usage.Tokens))
		add("cost", fmt.Sprintf("$%.4f", e.Usage.CostUsd))
	}
	return strings.Join(f, "  ")
}
