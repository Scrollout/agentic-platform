package captured_test

// Tests of captured.Fill (feature 011, task T003: spec FR-001, FR-003, SC-006; research R4). The
// parameters are built here from an independent scanner (propose_test.go), not from Propose, so
// a failure of Fill is Fill's. The expected files come from strings.NewReplacer, whose rule
// (argument order, left to right, no overlap) is the contract's "longest first".

import (
	"bytes"
	"strings"
	"testing"

	"github.com/agentic-platform/engine/canonical"
	"github.com/agentic-platform/engine/captured"
)

func t3ByPath(t *testing.T, files []canonical.File) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, f := range files {
		if _, dup := out[f.Path]; dup {
			t.Fatalf("two files at %s", f.Path)
		}
		out[f.Path] = string(f.Content)
	}
	return out
}

func t3Clone(files []canonical.File) []canonical.File {
	out := make([]canonical.File, len(files))
	for i, f := range files {
		out[i] = canonical.File{Path: f.Path, Content: bytes.Clone(f.Content)}
	}
	return out
}

// t3Expect is the files with every note/notes form replaced by the values, by NewReplacer.
func t3Expect(files []canonical.File, note, notes string) map[string]string {
	cap := func(s string) string { return strings.ToUpper(s[:1]) + s[1:] }
	r := strings.NewReplacer(
		"notes", notes, "Notes", cap(notes), "NOTES", strings.ToUpper(notes),
		"note", note, "Note", cap(note), "NOTE", strings.ToUpper(note))
	out := map[string]string{}
	for _, f := range files {
		out[r.Replace(f.Path)] = r.Replace(string(f.Content))
	}
	return out
}

// T003, FR-003: Fill with each parameter's source value gives every input file back, byte for
// byte, at its own path, in every output (tests, code), the inline forms file included.
func TestFillWithSourceValuesGivesTheInputBack(t *testing.T) {
	f := t3Load(t, "notes")
	tpl := t3WithFile(f.template, "code", t3Forms)
	ps := t3Params(f.src, tpl, []t3Spec{t3Notes, t3Note})
	values := map[string]string{"note": "note", "notes": "notes"}
	for _, out := range []string{"tests", "code"} {
		in := tpl[out]
		keep := t3Clone(in)
		got, err := captured.Fill(in, ps, values)
		if err != nil {
			t.Fatalf("%s: %v", out, err)
		}
		if len(got) == 0 || len(got) != len(in) {
			t.Fatalf("%s: %d files, want %d", out, len(got), len(in))
		}
		want := t3ByPath(t, keep)
		for p, c := range t3ByPath(t, got) {
			if w, ok := want[p]; !ok || w != c {
				t.Errorf("%s: %s changed under the source values", out, p)
			}
		}
		for i := range in {
			if in[i].Path != keep[i].Path || !bytes.Equal(in[i].Content, keep[i].Content) {
				t.Errorf("%s: Fill changed its input file %s", out, keep[i].Path)
			}
		}
	}
	// No parameters: nothing to replace.
	got, err := captured.Fill(tpl["code"], nil, nil)
	if err != nil || len(got) != len(tpl["code"]) {
		t.Errorf("no parameters: %d files, %v", len(got), err)
	}
}

// T003, FR-003, research R4: `task`/`tasks` give `NewTask`, `listTasks`, `TASK` where `NOTE` was,
// `task_id` and `detask` where `note_id` and `denote` were, in every occurrence, inside words too;
// `nOTE` is unchanged; the paths are filled too: `cmd/notes/main.go` becomes `cmd/tasks/main.go`.
func TestFillReplacesEveryFormInsideWordsAndPaths(t *testing.T) {
	f := t3Load(t, "notes")
	tpl := t3WithFile(f.template, "code", t3Forms)
	ps := t3Params(f.src, tpl, []t3Spec{t3Notes, t3Note})
	values := map[string]string{"note": "task", "notes": "tasks"}
	for _, out := range []string{"tests", "code"} {
		in := tpl[out]
		got, err := captured.Fill(in, ps, values)
		if err != nil {
			t.Fatalf("%s: %v", out, err)
		}
		gotBy, want := t3ByPath(t, got), t3Expect(in, "task", "tasks")
		if len(gotBy) != len(want) {
			t.Errorf("%s: %d files, want %d", out, len(gotBy), len(want))
		}
		for p, w := range want {
			if g, ok := gotBy[p]; !ok {
				t.Errorf("%s: no file %s", out, p)
			} else if g != w {
				t.Errorf("%s: %s differs from the expected fill:\n got %q\nwant %q", out, p, clip([]string{g})[0], clip([]string{w})[0])
			}
		}
	}
	// By hand.
	code, err := captured.Fill(tpl["code"], ps, values)
	if err != nil {
		t.Fatal(err)
	}
	by := t3ByPath(t, code)
	if _, ok := by["cmd/tasks/main.go"]; !ok {
		t.Errorf("no file cmd/tasks/main.go among %v", keys(by))
	}
	if _, ok := by["cmd/notes/main.go"]; ok {
		t.Error("the file cmd/notes/main.go was not renamed")
	}
	if g := by["internal/app/forms.go"]; !strings.Contains(g, "var TASK, task_id, detask, nOTE = 1, 2, 3, 4") {
		t.Errorf("the forms file is %q: want NOTE, note_id, denote replaced and nOTE left", g)
	}
	app := by["internal/app/app.go"]
	for _, w := range []string{"// Package app is the in-memory tasks service", "// ListTasks returns every task", "// CreateTask stores a task", "gen.NewTask", "tasks []gen.Task"} {
		if !strings.Contains(app, w) {
			t.Errorf("app.go lacks %q", w)
		}
	}
	if strings.Contains(strings.ToLower(app), "note") {
		t.Error("app.go still holds a note form")
	}
}

// T003: values of another length shift later places on the same line, and a value that holds a
// digit keeps its case forms (`item2` gives `Item2` and `ITEM2`).
func TestFillWithValuesOfAnotherLength(t *testing.T) {
	f := t3Load(t, "notes")
	tpl := t3WithFile(f.template, "code", t3Forms)
	ps := t3Params(f.src, tpl, []t3Spec{t3Notes, t3Note})
	values := map[string]string{"note": "item2", "notes": "reminders"}
	got, err := captured.Fill(tpl["code"], ps, values)
	if err != nil {
		t.Fatal(err)
	}
	gotBy, want := t3ByPath(t, got), t3Expect(tpl["code"], "item2", "reminders")
	for p, w := range want {
		if gotBy[p] != w {
			t.Errorf("%s differs from the expected fill", p)
		}
	}
	if g := gotBy["internal/app/forms.go"]; !strings.Contains(g, "var ITEM2, item2_id, deitem2, nOTE = 1, 2, 3, 4") {
		t.Errorf("forms file: %q", g)
	}
	if _, ok := gotBy["cmd/reminders/main.go"]; !ok {
		t.Errorf("no cmd/reminders/main.go among %v", keys(gotBy))
	}
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

// T003, SC-006: on the limit fixture, with `100` for 200 and `101` for 201, every 200 and 201
// place carries the new value, and only whole numbers change.
func TestFillLimitValues(t *testing.T) {
	f := t3Load(t, "limit")
	tpl := t3WithFile(f.template, "code", t3Numbers)
	specs := []t3Spec{{"limit", captured.KindNumber, "200"}, {"over", captured.KindNumber, "201"}}
	ps := t3Params(f.src, tpl, specs)
	values := map[string]string{"limit": "100", "over": "101"}
	code, err := captured.Fill(tpl["code"], ps, values)
	if err != nil {
		t.Fatal(err)
	}
	by := t3ByPath(t, code)
	app := by["internal/app/app.go"]
	for _, w := range []string{`in.Text == "" || len(in.Text) > 100 {`, "must not be empty or exceed 100 characters", "or a text over 100 characters with a 400"} {
		if !strings.Contains(app, w) {
			t.Errorf("app.go lacks %q", w)
		}
	}
	if strings.Contains(app, "200") || strings.Contains(app, "201") {
		t.Error("app.go still holds 200 or 201")
	}
	if want := "2001 1.200 200ms v200 -100 \"100\" 100. 100\n200.5 _200 200_ a200 200a 1200\n100"; by[t3Numbers.Path] != want {
		t.Errorf("numbers.txt:\n got %q\nwant %q", by[t3Numbers.Path], want)
	}
	tests, err := captured.Fill(tpl["tests"], ps, values)
	if err != nil {
		t.Fatal(err)
	}
	ft := t3ByPath(t, tests)["tests/features_test.go"]
	if !strings.Contains(ft, "const textLimit, overLimit = 100, 101") {
		t.Errorf("features_test.go lacks the filled constants")
	}
	if strings.Contains(ft, "200") || strings.Contains(ft, "201") {
		t.Error("features_test.go still holds 200 or 201")
	}
	// With the source values the files come back unchanged.
	same, err := captured.Fill(tpl["code"], ps, map[string]string{"limit": "200", "over": "201"})
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range tpl["code"] {
		if t3ByPath(t, same)[in.Path] != string(in.Content) {
			t.Errorf("%s changed under the source values", in.Path)
		}
	}
}

// T003: an empty template output (here `witnesses`, and an empty slice) fills to no file, without
// an error.
func TestFillEmptyOutputGivesNoFile(t *testing.T) {
	f := t3Load(t, "notes")
	ps := t3Params(f.src, f.template, []t3Spec{t3Notes, t3Note})
	values := map[string]string{"note": "task", "notes": "tasks"}
	for what, in := range map[string][]canonical.File{"witnesses": f.template["witnesses"], "empty": {}, "nil": nil} {
		got, err := captured.Fill(in, ps, values)
		if err != nil {
			t.Errorf("%s: %v", what, err)
		}
		if len(got) != 0 {
			t.Errorf("%s: %d files, want none", what, len(got))
		}
	}
	if got, err := captured.Fill(nil, nil, nil); err != nil || len(got) != 0 {
		t.Errorf("no input at all: %d files, %v", len(got), err)
	}
}

// T003, research R4: two files that would get the same filled path do not fill: the refusal names
// the paths. `pkg/note.go` fills to `pkg/task.go`, which the other file already is.
func TestFillRefusesTwoFilesAtOnePath(t *testing.T) {
	files := []canonical.File{
		{Path: "pkg/note.go", Content: []byte("package pkg\n")},
		{Path: "pkg/task.go", Content: []byte("package pkg\n\nvar x = 1\n")},
	}
	for _, order := range [][]canonical.File{files, {files[1], files[0]}} {
		tpl := captured.Template{"code": order}
		ps := t3Params(captured.Copy{}, tpl, []t3Spec{t3Note})
		got, err := captured.Fill(order, ps, map[string]string{"note": "task"})
		if err == nil {
			t.Fatalf("filled to %d files, want a refusal", len(got))
		}
		t3NotStub(t, err)
		for _, p := range []string{"pkg/note.go", "pkg/task.go"} {
			if !strings.Contains(err.Error(), p) {
				t.Errorf("%q does not name %s", err, p)
			}
		}
		if len(got) != 0 {
			t.Errorf("a refused fill gave %d files", len(got))
		}
	}
	// Two different files stay two files.
	tpl := captured.Template{"code": files[:1]}
	ps := t3Params(captured.Copy{}, tpl, []t3Spec{t3Note})
	got, err := captured.Fill(files[:1], ps, map[string]string{"note": "task"})
	if err != nil || len(got) != 1 || got[0].Path != "pkg/task.go" {
		t.Errorf("one file: %v %v", got, err)
	}
}
