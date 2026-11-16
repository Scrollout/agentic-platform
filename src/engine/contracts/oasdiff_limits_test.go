package contracts_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/agentic-platform/engine/contracts"
	"github.com/agentic-platform/engine/slices"
)

const mib4 = 4 << 20

// padded is the notes contract grown to exactly size bytes by a long description.
func padded(t *testing.T, size int) string {
	t.Helper()
	const head = `{"openapi": "3.1.0", "info": {"title": "notes", "version": "1", "description": "`
	const tail = `"}, "paths": {"/notes": {"get": {"operationId": "listNotes", "responses": {"200": {"description": "ok"}}}}}}`
	n := size - len(head) - len(tail)
	if n < 0 {
		t.Fatal("size too small")
	}
	s := head + strings.Repeat("a", n) + tail
	if len(s) != size {
		t.Fatalf("built %d bytes, want %d", len(s), size)
	}
	return s
}

// within runs the diff and fails when it does not return in time.
func within(t *testing.T, base, rev string) ([]slices.ContractFinding, error) {
	t.Helper()
	type res struct {
		fs  []slices.ContractFinding
		err error
	}
	ch := make(chan res, 1)
	go func() {
		fs, err := contracts.NewDiff().Diff(context.Background(), base, rev)
		ch <- res{fs, err}
	}()
	select {
	case r := <-ch:
		return r.fs, r.err
	case <-time.After(60 * time.Second):
		t.Fatal("the diff did not return in 60s")
		return nil, nil
	}
}

func TestARevisionAboveFourMiBIsAnError(t *testing.T) {
	if _, err := within(t, "", padded(t, mib4+1)); err == nil {
		t.Error("want an error")
	}
}

func TestABaseAboveFourMiBIsAnError(t *testing.T) {
	if _, err := within(t, padded(t, mib4+1), defaults().json()); err == nil {
		t.Error("want an error")
	}
}

func TestARevisionAtFourMiBIsAccepted(t *testing.T) {
	fs, err := within(t, "", padded(t, mib4))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e := errorFindings(fs); len(e) != 0 {
		t.Errorf("error findings: %+v", e)
	}
}

func TestABaseAtFourMiBIsAccepted(t *testing.T) {
	p := padded(t, mib4)
	fs, err := within(t, p, p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e := errorFindings(fs); len(e) != 0 {
		t.Errorf("error findings: %+v", e)
	}
}

// withRef puts ref where each test wants it in the notes contract.
func withRefs(ref string) map[string]string {
	return map[string]string{
		"schema property": `{"openapi": "3.1.0", "info": {"title": "n", "version": "1"}, "paths": {"/notes": {"get": {"operationId": "l",
 "responses": {"200": {"description": "ok", "content": {"application/json": {"schema": {"type": "object", "properties": {"id": {"$ref": "` + ref + `"}}}}}}}}}}}`,
		"parameter": `{"openapi": "3.1.0", "info": {"title": "n", "version": "1"}, "paths": {"/notes": {"get": {"operationId": "l",
 "parameters": [{"$ref": "` + ref + `"}], "responses": {"200": {"description": "ok"}}}}}}`,
		"response": `{"openapi": "3.1.0", "info": {"title": "n", "version": "1"}, "paths": {"/notes": {"get": {"operationId": "l",
 "responses": {"200": {"$ref": "` + ref + `"}}}}}}`,
		"yaml top level": "openapi: 3.1.0\ninfo: {title: n, version: '1'}\npaths:\n  /notes:\n    $ref: '" + ref + "'\n",
	}
}

func TestExternalRefsAreRefused(t *testing.T) {
	for _, ref := range []string{
		"./x.yaml", "x.yaml", "../x.yaml#/a", "./x.yaml#/components/schemas/a",
		"/etc/passwd", "/tmp/x.yaml#/a", "file:///etc/passwd", "file:///tmp/x.yaml#/a",
		"http://203.0.113.1/x.yaml", "https://example.invalid/x.json#/a",
	} {
		for where, doc := range withRefs(ref) {
			t.Run(where+" "+ref, func(t *testing.T) {
				for side, pair := range map[string][2]string{"revision": {"", doc}, "base": {doc, defaults().json()}} {
					fs, err := within(t, pair[0], pair[1])
					if err == nil && len(errorFindings(fs)) == 0 {
						t.Errorf("%s: ref accepted without error or error finding (%+v)", side, fs)
					}
				}
			})
		}
	}
}

func TestRefCyclesDoNotHang(t *testing.T) {
	self := `{"openapi": "3.1.0", "info": {"title": "n", "version": "1"}, "paths": {"/n": {"get": {"operationId": "l",
 "responses": {"200": {"description": "ok", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/A"}}}}}}}},
 "components": {"schemas": {"A": {"type": "object", "properties": {"next": {"$ref": "#/components/schemas/A"}}}}}}`
	pair := strings.Replace(self, `"A": {"type": "object", "properties": {"next": {"$ref": "#/components/schemas/A"}}}`,
		`"A": {"type": "object", "properties": {"b": {"$ref": "#/components/schemas/B"}}}, "B": {"type": "object", "properties": {"a": {"$ref": "#/components/schemas/A"}}}`, 1)
	direct := strings.Replace(self, `{"type": "object", "properties": {"next": {"$ref": "#/components/schemas/A"}}}`, `{"$ref": "#/components/schemas/A"}`, 1)
	for name, doc := range map[string]string{"self": self, "A to B to A": pair, "pure alias loop": direct} {
		t.Run(name, func(t *testing.T) {
			// Either outcome is fine; returning without a hang or a panic is the claim.
			_, _ = within(t, "", doc)
			_, _ = within(t, doc, doc)
		})
	}
}

func TestDeepNestingDoesNotPanic(t *testing.T) {
	const depth = 3000
	schema := strings.Repeat(`{"type": "array", "items": `, depth) + `{"type": "string"}` + strings.Repeat("}", depth)
	doc := `{"openapi": "3.1.0", "info": {"title": "n", "version": "1"}, "paths": {"/n": {"get": {"operationId": "l",
 "responses": {"200": {"description": "ok", "content": {"application/json": {"schema": ` + schema + `}}}}}}}}`
	// An error or a result are both fine; a panic would fail the test run.
	_, _ = within(t, "", doc)
	_, _ = within(t, doc, doc)
}

func TestACancelledContextIsAnError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d := defaults().json()
	if _, err := contracts.NewDiff().Diff(ctx, d, d); err == nil {
		t.Error("want an error")
	}
	if _, err := contracts.NewDiff().Diff(ctx, "", d); err == nil {
		t.Error("want an error")
	}
}

func TestEmptyInputIsAnError(t *testing.T) {
	d := defaults().json()
	for name, pair := range map[string][2]string{
		"empty revision": {"", ""}, "empty revision with base": {d, ""},
		"blank revision": {"", " \n\t "}, "blank base": {" \n ", d},
	} {
		if _, err := within(t, pair[0], pair[1]); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}
