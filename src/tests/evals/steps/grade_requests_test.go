package steps

// Self-tests of MalformedRequestsRefused (feature 010, US3; research R6; README "Malformed
// requests"): no model, no Docker. The grader passes the reference implementation in a project
// generated on the changed pattern, fails on planted violations and names the request each time,
// never runs when an earlier grader failed, and never changes the agent's project (the
// constitution: checks prove they can fail).

import (
	"reflect"
	"strings"
	"testing"
)

// The three requests the grader sends to POST /notes, as sent, and the answer each must get.
const (
	reqUpper = `{"TEXT":"x"}`            // a member name in another case: 400
	reqBoth  = `{"text":"a","TEXT":"b"}` // the declared name and an undeclared one: 201, text "a"
	reqTwice = `{"text":"a","text":"b"}` // a name twice: 400

	genFile  = "gen/api.gen.go"  // the generated handler
	reqsFile = "gen/requests.go" // the generated request check
	wrapped  = `refuseMalformed("POST /notes", wrapper.CreateNote)`
)

var allRequests = []string{reqUpper, reqBoth, reqTwice}

// requireNamed fails unless the result is a failure whose reason names exactly the wanted
// requests, by body as sent.
func requireNamed(t *testing.T, r Result, want ...string) {
	t.Helper()
	if r.Grader != GRefused {
		t.Fatalf("the grader is %q, want %s", r.Grader, GRefused)
	}
	if r.Passed {
		t.Fatalf("passed, want a failure naming %q: %+v", want, r)
	}
	if r.Reason == "" {
		t.Fatalf("a failure without a reason: %+v", r)
	}
	wanted := map[string]bool{}
	for _, w := range want {
		wanted[w] = true
		if !strings.Contains(r.Reason, w) {
			t.Fatalf("the reason does not name the request %s: %q", w, r.Reason)
		}
	}
	for _, q := range allRequests {
		if !wanted[q] && strings.Contains(r.Reason, q) {
			t.Fatalf("the reason names %s, whose answer was right: %q", q, r.Reason)
		}
	}
}

// withCreateNote is the reference implementation with CreateNote's body replaced.
func withCreateNote(t *testing.T, body string) Files {
	t.Helper()
	ref := Ref("impl")[implFile]
	i := strings.Index(ref, "func (s *Service) CreateNote(")
	if i < 0 {
		t.Fatal("the reference code has no CreateNote")
	}
	return Files{implFile: ref[:i] + "func (s *Service) CreateNote(w http.ResponseWriter, r *http.Request) {\n" + body + "}\n"}
}

// The reference implementation, generated on the changed pattern, is passed; the reason says what
// was examined; the project is not written. The generated project really holds the wrapper that
// the other tests remove, so none of them can pass for want of it.
func TestGraderRequestsReference(t *testing.T) {
	e, b := testEnv(t), base(t)
	if n := strings.Count(b[genFile], wrapped); n != 1 {
		t.Fatalf("%q occurs %d times in %s of the generated project, want 1 (the pattern changed?)", wrapped, n, genFile)
	}
	proj := dir(t, codeGood(b))
	before := snapshot(t, proj)
	r := e.MalformedRequestsRefused(proj)
	if r.Grader != GRefused || !r.Passed || r.Reason == "" {
		t.Fatalf("the reference implementation failed or has no reason: %+v", r)
	}
	for _, q := range allRequests {
		if !strings.Contains(r.Reason, q) {
			t.Errorf("the pass does not say it examined %s: %q", q, r.Reason)
		}
	}
	requireSame(t, proj, before, "after MalformedRequestsRefused")
	if r2 := e.MalformedRequestsRefused(proj); !reflect.DeepEqual(r, r2) {
		t.Fatalf("a second call answered differently:\n%+v\n%+v", r, r2)
	}
	requireSame(t, proj, before, "after the second call")
}

// The generated handler without refuseMalformed (the state before feature 010) answers 201 "x",
// 201 "b" and 201 "b" (Go keeps the last of a name twice): all three requests fail, each named
// with the status it got.
func TestGraderRequestsUnwrapped(t *testing.T) {
	e, b := testEnv(t), base(t)
	gen := replaceOnce(t, b[genFile], wrapped, "wrapper.CreateNote")
	proj := dir(t, Overlay(codeGood(b), Files{genFile: gen}))
	before := snapshot(t, proj)
	r := e.MalformedRequestsRefused(proj)
	requireNamed(t, r, reqUpper, reqBoth, reqTwice)
	if !strings.Contains(r.Reason, "201") {
		t.Fatalf("the reason does not say what each answer was (201): %q", r.Reason)
	}
	requireSame(t, proj, before, "after MalformedRequestsRefused")
}

// Single breaks: each request is judged on its own. Each edit is of the generated request check
// (and, for the first, of the reference code) and fails exactly one request.
func TestGraderRequestsSingleBreaks(t *testing.T) {
	e, b := testEnv(t), base(t)
	ref := Ref("impl")[implFile]
	reqs := b[reqsFile]
	for _, tc := range []struct {
		name  string
		files Files
		named string
	}{
		{"a name twice is passed on: only the third request fails", Files{
			reqsFile: replaceOnce(t, reqs, "(seen == nil && v.has(name)) || seen[name]", "false")}, reqTwice},
		{"undeclared members are kept: only the second request fails", Files{
			reqsFile: replaceOnce(t, reqs, "if s.Properties == nil || len(present) == len(v.names) {", "if true {")}, reqBoth},
		{"a missing required member is passed on and empty text accepted: only the first request fails", Files{
			reqsFile: replaceOnce(t, reqs, "for _, n := range s.RequiredMembers {", "for _, n := range []string(nil) {"),
			implFile: replaceOnce(t, ref, refEmptyLine, "\tif err := json.NewDecoder(r.Body).Decode(&in); err != nil {\n")}, reqUpper},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proj := dir(t, Overlay(codeGood(b), tc.files))
			r := e.MalformedRequestsRefused(proj)
			requireNamed(t, r, tc.named)
			if !strings.Contains(r.Reason, "201") {
				t.Fatalf("the reason does not show the status 201: %q", r.Reason)
			}
		})
	}
}

// What the service answers to the request that must succeed: a wrong status, a wrong text, a body
// that is not JSON, has no text or is empty, each fails that request alone (the wrapper still
// refuses the other two). The reason shows the status.
func TestGraderRequestsWrongAnswers(t *testing.T) {
	e, b := testEnv(t), base(t)
	const create = "\tvar in gen.NewNote\n\t_ = json.NewDecoder(r.Body).Decode(&in)\n"
	for _, tc := range []struct {
		name, body, status string
	}{
		{"400 for the request that must succeed", create + "\twrite(w, http.StatusBadRequest, gen.Error{Message: \"no\"})\n", "400"},
		{"422", create + "\twrite(w, 422, gen.Error{Message: \"no\"})\n", "422"},
		{"500", "\tw.WriteHeader(500)\n", "500"},
		{"201 with the wrong text", create + "\twrite(w, http.StatusCreated, gen.Note{Id: 1, Text: in.Text + \"!\"})\n", "201"},
		{"201 with a body that is not JSON", "\tw.WriteHeader(http.StatusCreated)\n\t_, _ = w.Write([]byte(\"created\"))\n", "201"},
		{"201 with a body that has no text", "\twrite(w, http.StatusCreated, map[string]int{\"id\": 1})\n", "201"},
		{"201 with an empty body", "\tw.WriteHeader(http.StatusCreated)\n", "201"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proj := dir(t, Overlay(codeGood(b), withCreateNote(t, tc.body)))
			r := e.MalformedRequestsRefused(proj)
			requireNamed(t, r, reqBoth)
			if !strings.Contains(r.Reason, tc.status) {
				t.Fatalf("the reason does not show the status %s: %q", tc.status, r.Reason)
			}
		})
	}
}

// Code that does not build, and a folder that is not there, fail with a reason.
func TestGraderRequestsDoesNotBuild(t *testing.T) {
	e, b := testEnv(t), base(t)
	for name, f := range map[string]Files{
		"a syntax error":    Overlay(codeGood(b), Files{implFile: "package app\n\nfunc {\n"}),
		"an unknown symbol": Overlay(codeGood(b), Files{implFile: strings.Replace(Ref("impl")[implFile], "gen.NewNote", "gen.NoSuchType", 1)}),
		"no module":         {"README.md": "x\n"},
	} {
		t.Run(name, func(t *testing.T) {
			r := e.MalformedRequestsRefused(dir(t, f))
			if r.Grader != GRefused || r.Passed || r.Reason == "" {
				t.Fatalf("an unbuildable project passed or has no reason: %+v", r)
			}
		})
	}
	t.Run("a missing folder", func(t *testing.T) {
		for _, p := range []string{t.TempDir() + "/absent", ""} {
			r := e.MalformedRequestsRefused(p)
			if r.Grader != GRefused || r.Passed || r.Reason == "" {
				t.Fatalf("project %q: %+v", p, r)
			}
		}
	})
}

// GradeCode: for every code case's clean project the result is there once, passed, right after
// DeploymentValuesConfigured and before the last result HeldOutTestsPass; the projects stay as
// they were.
func TestGraderRequestsInGradeCode(t *testing.T) {
	e := testEnv(t)
	n := 0
	for _, c := range Cases() {
		if c.Agent != AgentCode {
			continue
		}
		n++
		t.Run(c.Name, func(t *testing.T) {
			before, err := c.Before()
			if err != nil {
				t.Fatal(err)
			}
			bd, ad := dir(t, before), dir(t, Overlay(before, Ref("impl")))
			bSnap, aSnap := snapshot(t, bd), snapshot(t, ad)
			rs := e.GradeCode(bd, ad, c.Slice.Contract)
			mustPass(t, rs)
			at, seen := -1, 0
			for i, r := range rs {
				if r.Grader == GRefused {
					at = i
					seen++
					if !r.Passed || r.Reason == "" {
						t.Fatalf("%+v", r)
					}
				}
			}
			if seen != 1 {
				t.Fatalf("%d %s results in %+v", seen, GRefused, rs)
			}
			if at < 1 || at != len(rs)-2 || rs[len(rs)-1].Grader != GHeldOut || rs[at-1].Grader != GDeployValues {
				t.Fatalf("%s is at %d of %d: want right after %s and before the last %s (%+v)",
					GRefused, at, len(rs), GDeployValues, GHeldOut, rs)
			}
			requireSame(t, ad, aSnap, "after GradeCode")
			requireSame(t, bd, bSnap, "(the starting project) after GradeCode")
		})
	}
	if n < 2 {
		t.Fatalf("only %d code cases", n)
	}
}

// GradeCode: the grader runs only when every earlier grader passed, and its reason names the
// first failing one; HeldOutTestsPass, still last, says the same. (A project that passes every
// earlier grader and fails only this one cannot be made from the reference code: the second
// request is the first scenario of the reference tests.)
func TestGraderRequestsFailFast(t *testing.T) {
	e, b := testEnv(t), base(t)
	before := dir(t, codeBase(b))
	approved := ExampleSlice().Contract
	ref := Ref("impl")[implFile]

	for _, tc := range []struct {
		name  string
		files Files
		first string // the first failing grader
	}{
		{"a file outside the scope", Overlay(codeGood(b), Files{"docs/notes.md": "x\n"}), GScope},
		{"the reference tests fail", Overlay(codeGood(b), Files{implFile: replaceOnce(t, ref, refListLine, "\twrite(w, http.StatusOK, []gen.Note{})\n")}), GAllPass},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proj := dir(t, tc.files)
			pSnap := snapshot(t, proj)
			rs := e.GradeCode(before, proj, approved)
			first := ""
			var got, held *Result
			for i := range rs {
				switch rs[i].Grader {
				case GRefused:
					got = &rs[i]
				case GHeldOut:
					held = &rs[i]
				default:
					if first == "" && !rs[i].Passed {
						first = rs[i].Grader
					}
				}
			}
			if got == nil || held == nil || held != &rs[len(rs)-1] {
				t.Fatalf("results: %+v", rs)
			}
			if first != tc.first {
				t.Fatalf("the first failing grader is %q, the case expects %q (%v)", first, tc.first, Failures(rs))
			}
			want := "not run: " + tc.first + " failed"
			if got.Passed || got.Reason != want || got.Counts != nil {
				t.Fatalf("%s is %+v, want failed with reason %q", GRefused, *got, want)
			}
			if held.Passed || held.Reason != want {
				t.Fatalf("%s is %+v, want failed with reason %q", GHeldOut, *held, want)
			}
			requireSame(t, proj, pSnap, "after GradeCode")
		})
	}
}
