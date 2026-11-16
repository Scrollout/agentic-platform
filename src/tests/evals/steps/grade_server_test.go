package steps

// T116's grader self-tests: no model, no Docker, no go toolchain. WitnessServesThroughServer fails
// a review trial when a witness written in this round (a new review/r<n>/*_test.go) calls a
// handler's ServeHTTP directly, since a panic there stops the test binary before its coverage is
// written; a witness served through httptest.NewServer passes, and an earlier round's witness is
// not judged. The wiring subtests run GradeReview, which needs the go toolchain.

import (
	"strings"
	"testing"
)

const serverWitnessHead = `package r2

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"example.com/service/gen"
	"example.com/service/internal/app"
)
`

// throughServer is the clean witness: the handler served by httptest.NewServer, whose server
// recovers a handler's panic, so the request fails and the witness fails having run the line.
const throughServer = serverWitnessHead + `
func TestR2FirstNoteGetsIdOne(t *testing.T) {
	srv := httptest.NewServer(gen.Handler(app.New()))
	defer srv.Close()
	res, err := srv.Client().Post(srv.URL+"/notes", "application/json", strings.NewReader(` + "`" + `{"text":"a"}` + "`" + `))
	if err != nil {
		t.Fatalf("POST /notes: %v", err)
	}
	defer res.Body.Close()
	var n gen.Note
	if err := json.NewDecoder(res.Body).Decode(&n); err != nil || res.StatusCode != http.StatusCreated || n.Id != 1 {
		t.Fatalf("the first note: status %d, id %d, %v; want 201 and id 1", res.StatusCode, n.Id, err)
	}
}
`

// onRecorder calls ServeHTTP on a recorder through a handler variable.
const onRecorder = serverWitnessHead + `
func TestR2FirstNoteGetsIdOne(t *testing.T) {
	h := gen.Handler(app.New())
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/notes", strings.NewReader(` + "`" + `{"text":"a"}` + "`" + `))
	h.ServeHTTP(w, r)
	var n gen.Note
	if err := json.Unmarshal(w.Body.Bytes(), &n); err != nil || w.Code != http.StatusCreated || n.Id != 1 {
		t.Fatalf("status %d, id %d", w.Code, n.Id)
	}
}
`

// chained calls ServeHTTP on gen.Handler(app.New()) directly.
const chained = serverWitnessHead + `
func TestR2FirstNoteGetsIdOne(t *testing.T) {
	w := httptest.NewRecorder()
	gen.Handler(app.New()).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/notes", strings.NewReader("{}")))
	var n gen.Note
	_ = json.Unmarshal(w.Body.Bytes(), &n)
	if w.Code != http.StatusCreated {
		t.Fatalf("status %d", w.Code)
	}
}
`

// mentionsOnly names ServeHTTP in a comment and a string, and serves through the server.
const mentionsOnly = `
// Never call h.ServeHTTP(w, r) directly: a panic there stops the test binary.
func TestR2Mentions(t *testing.T) {
	srv := httptest.NewServer(gen.Handler(app.New()))
	defer srv.Close()
	t.Log("not h.ServeHTTP(w, r), but srv.Client()")
}
`

func TestGraderWitnessServesThroughServer(t *testing.T) {
	before := Files{"go.mod": "module example.com/service\n", "internal/app/app.go": "package app\n"}
	const wf = "review/r2/first_note_test.go"
	grade := func(b, a Files) Result { return witnessServesThroughServer(dir(t, b), dir(t, a)) }
	pass := func(t *testing.T, r Result, examined string) {
		t.Helper()
		if r.Grader != GThroughSrv || !r.Passed {
			t.Fatalf("a clean witness failed: %+v", r)
		}
		if !strings.Contains(r.Reason, examined) {
			t.Fatalf("the reason does not say what was examined (%q): %s", examined, r.Reason)
		}
	}
	fails := func(t *testing.T, r Result, names string) {
		t.Helper()
		if r.Grader != GThroughSrv || r.Passed {
			t.Fatalf("a witness calling ServeHTTP directly passed: %+v", r)
		}
		if !strings.Contains(r.Reason, names) || !strings.Contains(r.Reason, "ServeHTTP") {
			t.Fatalf("the reason does not name %s and ServeHTTP: %s", names, r.Reason)
		}
	}

	t.Run("clean: a witness served through httptest.NewServer", func(t *testing.T) {
		pass(t, grade(before, Overlay(before, Files{wf: throughServer})), "1 new witness file")
	})
	t.Run("clean: no witness written", func(t *testing.T) {
		pass(t, grade(before, before), "0 new witness files")
	})
	t.Run("clean: ServeHTTP only in a comment and a string", func(t *testing.T) {
		pass(t, grade(before, Overlay(before, Files{wf: throughServer + mentionsOnly})), wf)
	})
	t.Run("clean: an earlier round's witness calling it is not judged", func(t *testing.T) {
		const earlier = "review/r2/round1_test.go"
		b := Overlay(before, Files{earlier: onRecorder})
		r := grade(b, Overlay(b, Files{"review/r3/round2_test.go": strings.Replace(throughServer, "package r2", "package r3", 1)}))
		pass(t, r, "1 new witness file")
		if strings.Contains(r.Reason, earlier+":") {
			t.Fatalf("the earlier witness was judged: %s", r.Reason)
		}
	})
	t.Run("clean: a non-witness file calling it is not judged", func(t *testing.T) {
		pass(t, grade(before, Overlay(before, Files{"review/r2/helper.go": strings.Replace(onRecorder, "func Test", "func test", 1)})), "0 new witness files")
	})
	t.Run("planted: ServeHTTP on a recorder", func(t *testing.T) {
		fails(t, grade(before, Overlay(before, Files{wf: onRecorder})), wf+":")
	})
	t.Run("planted: gen.Handler(app.New()).ServeHTTP chained", func(t *testing.T) {
		fails(t, grade(before, Overlay(before, Files{wf: chained})), wf+":")
	})
	t.Run("planted: one bad witness among clean ones", func(t *testing.T) {
		a := Overlay(before, Files{wf: throughServer, "review/r3/x_test.go": strings.Replace(chained, "package r2", "package r3", 1)})
		fails(t, grade(before, a), "review/r3/x_test.go:")
	})
	t.Run("wiring: GradeReview judges the new witness", func(t *testing.T) {
		e := testEnv(t)
		c := witnessCase(t, "review/witness-r1")
		b, err := c.Before()
		if err != nil {
			t.Fatal(err)
		}
		plant, err := c.Plant(b)
		if err != nil {
			t.Fatal(err)
		}
		bd := dir(t, b)
		of := func(witness string) Result {
			t.Helper()
			for _, r := range e.GradeReview(bd, dir(t, Overlay(b, Files{wf: witness})), answerOf(), plant) {
				if r.Grader == GThroughSrv {
					return r
				}
			}
			t.Fatalf("GradeReview does not run %s", GThroughSrv)
			return Result{}
		}
		fails(t, of(onRecorder), wf+":")
		pass(t, of(throughServer), "1 new witness file")
	})
	t.Run("planted: a witness that does not parse fails closed", func(t *testing.T) {
		fails(t, grade(before, Overlay(before, Files{wf: "package r2\n\nfunc TestR2X(t *testing.T) {\n"})), wf)
	})
}
