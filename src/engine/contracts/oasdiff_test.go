package contracts_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/agentic-platform/engine/contracts"
	"github.com/agentic-platform/engine/slices"
)

var _ slices.ContractDiff = contracts.NewDiff()

// doc builds a small OpenAPI 3.1 notes contract. Each piece can be swapped by a test.
type parts struct {
	list      string // the paths entry for /notes (GET), responses included
	listProps string // properties of a listed note
	listReq   string // required of a listed note
	createReq string // required of the create request
	createPrp string // properties of the create request
	extra     string // further paths
}

func defaults() parts {
	return parts{
		listProps: `"id": {"type": "integer"}, "text": {"type": "string"}`,
		listReq:   `"id", "text"`,
		createReq: `"text"`,
		createPrp: `"text": {"type": "string"}`,
	}
}

func (p parts) json() string {
	list := p.list
	if list == "" {
		list = `"get": {"operationId": "listNotes", "responses": {"200": {"description": "ok", "content": {"application/json": {"schema": {"type": "array", "items": {"type": "object", "required": [` + p.listReq + `], "properties": {` + p.listProps + `}}}}}}}}`
	}
	s := `{"openapi": "3.1.0", "info": {"title": "notes", "version": "1"}, "paths": {"/notes": {` + list + `,
 "post": {"operationId": "createNote", "requestBody": {"required": true, "content": {"application/json": {"schema": {"type": "object", "required": [` + p.createReq + `], "properties": {` + p.createPrp + `}}}}},
  "responses": {"201": {"description": "created"}}}}`
	if p.extra != "" {
		s += "," + p.extra
	}
	return s + `}}`
}

const notesYAML = `openapi: 3.1.0
info: {title: notes, version: '1'}
paths:
  /notes:
    get:
      operationId: listNotes
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                type: array
                items:
                  type: object
                  required: [id, text]
                  properties:
                    id: {type: integer}
                    text: {type: string}
`

func diff(t *testing.T, base, rev string) ([]slices.ContractFinding, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return contracts.NewDiff().Diff(ctx, base, rev)
}

func errorFindings(fs []slices.ContractFinding) []slices.ContractFinding {
	var out []slices.ContractFinding
	for _, f := range fs {
		if f.Level == "error" {
			out = append(out, f)
		}
	}
	return out
}

func noErrors(t *testing.T, base, rev string) {
	t.Helper()
	fs, err := diff(t, base, rev)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e := errorFindings(fs); len(e) != 0 {
		t.Fatalf("want no error-level finding, got %+v", e)
	}
}

// breaking wants at least one error-level finding whose ID or text contains each of names.
func breaking(t *testing.T, base, rev string, names ...string) {
	t.Helper()
	fs, err := diff(t, base, rev)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	e := errorFindings(fs)
	if len(e) == 0 {
		t.Fatalf("want an error-level finding, got %+v", fs)
	}
	var all strings.Builder
	for _, f := range e {
		if f.ID == "" || f.Text == "" {
			t.Errorf("finding without ID or text: %+v", f)
		}
		all.WriteString(f.ID + " " + f.Text + "\n")
	}
	for _, n := range names {
		if !strings.Contains(all.String(), n) {
			t.Errorf("no error finding names %q: %s", n, all.String())
		}
	}
}

func TestIdenticalContractsGiveNoErrorFinding(t *testing.T) {
	d := defaults().json()
	noErrors(t, d, d)
}

func TestRemovingAnOperationIsAnError(t *testing.T) {
	p := defaults()
	p.extra = `"/notes/{id}": {"delete": {"operationId": "deleteNote", "parameters": [{"name": "id", "in": "path", "required": true, "schema": {"type": "integer"}}], "responses": {"204": {"description": "gone"}}}}`
	base := p.json()
	breaking(t, base, defaults().json(), "/notes/{id}")
}

func TestRemovingAnOperationOfAPathNamesIt(t *testing.T) {
	base := defaults().json()
	p := defaults()
	p.list = `"head": {"operationId": "headNotes", "responses": {"200": {"description": "ok"}}}`
	breaking(t, base, p.json(), "GET", "/notes")
}

func TestRemovingARequiredResponsePropertyIsAnError(t *testing.T) {
	p := defaults()
	p.listProps = `"id": {"type": "integer"}`
	p.listReq = `"id"`
	breaking(t, defaults().json(), p.json(), "text")
}

func TestAddingARequiredRequestPropertyIsAnError(t *testing.T) {
	p := defaults()
	p.createReq = `"text", "title"`
	p.createPrp = `"text": {"type": "string"}, "title": {"type": "string"}`
	breaking(t, defaults().json(), p.json(), "title")
}

func TestChangingAPropertyTypeIsAnError(t *testing.T) {
	p := defaults()
	p.listProps = `"id": {"type": "string"}, "text": {"type": "string"}`
	breaking(t, defaults().json(), p.json(), "id")
}

func TestAddingAnOptionalResponsePropertyIsNotAnError(t *testing.T) {
	p := defaults()
	p.listProps += `, "tag": {"type": "string"}`
	noErrors(t, defaults().json(), p.json())
}

func TestAddingAnOperationIsNotAnError(t *testing.T) {
	p := defaults()
	p.extra = `"/health": {"get": {"operationId": "health", "responses": {"200": {"description": "ok"}}}}`
	noErrors(t, defaults().json(), p.json())
}

func TestFirstContractAgainstTheEmptyBaselineGivesNoError(t *testing.T) {
	noErrors(t, "", defaults().json())
	noErrors(t, "", notesYAML)
}

func TestYAMLAndJSONContractsBothWork(t *testing.T) {
	noErrors(t, notesYAML, notesYAML)
	// A YAML base against a JSON revision still finds a breaking change.
	p := defaults()
	p.listProps = `"id": {"type": "integer"}`
	p.listReq = `"id"`
	breaking(t, notesYAML, p.json(), "text")
	// And a JSON base against a YAML revision with a removed operation.
	breaking(t, defaults().json(), notesYAML, "/notes")
}

func TestUnparseableContractFailsClosed(t *testing.T) {
	for _, bad := range []string{
		"{not json or yaml: [",
		"just some prose",
		`{"hello": "world"}`,
		`{"swagger": "2.0", "info": {"title": "x", "version": "1"}, "paths": {}}`,
	} {
		for name, pair := range map[string][2]string{"revision": {"", bad}, "revision against base": {defaults().json(), bad}} {
			fs, err := diff(t, pair[0], bad+"")
			if err == nil && len(errorFindings(fs)) == 0 {
				t.Errorf("%s %q: neither an error nor an error-level finding (%+v)", name, bad, fs)
			}
		}
	}
}

func TestUnparseableBaselineFailsClosed(t *testing.T) {
	fs, err := diff(t, "{not json or yaml: [", defaults().json())
	if err == nil && len(errorFindings(fs)) == 0 {
		t.Errorf("neither an error nor an error-level finding (%+v)", fs)
	}
}

func TestAnExternalURLRefIsNotFetched(t *testing.T) {
	// If the diff fetched this it would hang or reach a network; it must refuse at once.
	ref := strings.Replace(defaults().json(), `{"type": "integer"}`, `{"$ref": "http://203.0.113.1/schemas/id.json"}`, 1)
	done := make(chan struct{})
	var fs []slices.ContractFinding
	var err error
	go func() {
		defer close(done)
		fs, err = diff(t, "", ref)
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("the diff did not return: it may be fetching a URL")
	}
	if err == nil && len(errorFindings(fs)) == 0 {
		t.Errorf("an external URL $ref gave neither an error nor an error-level finding (%+v)", fs)
	}
}
