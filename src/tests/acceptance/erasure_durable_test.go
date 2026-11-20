//go:build docker

// T028 (feature 002, US3, FR-015, FR-017 as clarified 2026-11-19, SC-006; research R2, R8, S9; data
// model "Erasing a slice"): erasure reaches the durable service, and a run record needs no workflow.
// CI's `docker` job only (real Temporal, PostgreSQL and the engine container; this file is never
// run on the developer's host). One lab, the `happy` step set, shows:
//
//  1. A slice is built to an ended run (RUN-1) and replayed (RUN-2): several ended operations
//     (createSlice, answers, three approvals, startRun, replayRun) each have a record and a workflow.
//  2. FR-015: the answers of inspecting RUN-1 (getRun, its whole record, every artifact's bytes, and
//     the comparison of its replay with it) are recorded; RUN-1's workflow is then deleted from the
//     durable service (the pinned admin-tools CLI on the lab's durable network), as feature 001's
//     runs never had one; the engine is restarted; the same answers are required, and a new replay
//     of RUN-1 (RUN-3) must reach the same outcome with the same artifacts, reusing every result.
//  3. Before the slice is deleted, the Temporal run IDs (`executions.run_id`) of every RUN-* and
//     OP-* workflow of the slice are read, and the engine store's operation records, slice folder and
//     `workers.json` are read to show they hold the slice. The slice is deleted; the erasing
//     operation is followed to `succeeded` and its record holds only the slice's ID as target (no
//     run or other operation ID). Its own workflow stays (retention removes it, judged in T066).
//  4. Polling with a bound (3 min; the deletion is asynchronous, S9): no row names those workflow
//     IDs in `executions`, `current_executions` (database temporal) or `executions_visibility`
//     (database temporal_visibility), and no `history_node` row has one of those Temporal run IDs as
//     `tree_id`. getOperation answers 404 for each of those operations at once, and getRun and
//     getSlice answer 404.
//  5. In the engine store (a host folder, found from the engine container's /store mount): no
//     `operations/OP-<n>.json` or `.body` of those operations, no `slices/<slice>` folder, and no
//     entry of the slice in `workers.json`.
//
// Every search proves it can find: the Temporal search is run before the erasure over the same
// workflows and must find rows in each of the four tables (and a `history_node` row for each run
// ID, which also proves tree_id carries the run ID); the store search is run on a planted fixture
// and on a clean one (TestErasureSearchesFindPlantedCopies, no lab). Each search logs what it
// examined. The table and column names are read from information_schema in the running store and
// the test fails clearly when one is missing; byte and UUID columns are compared through one
// normalisation (hex without dashes).
//
// Expected to fail today (T029 does not exist): 4 (workflow, visibility and history rows stay),
// 5 (operation records stay, via the searches), the 404 of the other operations, and the erasing
// operation's record check if it names runs. Part 2 (FR-015) is feature 001's behaviour and may
// pass today; the test guards that deleting a workflow is harmless to the record.
//
// Assumed beyond the brief: the engine's /store mount is a host folder readable by the test (as
// in lab.Start); records are operations/OP-<n>.json with `id`, `target`, `status` (data model);
// workers.json is JSON whose string values name the slice's ID for its entries; a Temporal run ID
// is the `tree_id` of its first history branch; `temporal workflow delete` for one workflow ID
// needs no confirmation; DeleteWorkflowExecution is applied within the polling bound.
//
// Time bounds: the lab starts once. After it starts: the first run and its replays are bounded at
// 10 min each (expected about 2 min in all), each Temporal wait at 3 min (expected seconds), the
// engine restart at 2 min. Expected about 5 min after the lab starts, at most about 25 min.
//
// Edge cases checked: a workflow deleted before the restart is gone (waited for) so the restart
// really meets a run with no workflow; the replay after that; step workers registered when the
// slice is deleted (workers.json holds entries before); a repeat of deleteSlice answers 404; the
// erasing operation's own record and workflow are not deleted; the run IDs are read before
// deletion since history_node names no workflow; byte versus text ID columns; leftovers found
// across both databases; ended operations of the slice only (every record read has ended).
// Left untested: another slice's runs and operations staying untouched (needs a second slice; the
// in-process T066 covers it), the durable service unavailable while erasing and a workflow
// already gone as NotFound (T066's fake ports), the erasing operation's 24 h retention (T066), a
// store write failing, an erasure repeated after a restart.
package acceptance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/agentic-platform/tests/lab"
)

const edWait = 3 * time.Minute

var (
	edWorkflowID = regexp.MustCompile(`^(RUN|OP)-[1-9][0-9]*$`)
	edHex        = regexp.MustCompile(`^[0-9a-f]{8,64}$`)
	edAnyID      = regexp.MustCompile(`(RUN|OP)-[0-9]+`)
)

// ---- the durable store ----------------------------------------------------------------------------

func edSQL(t *testing.T, store, db, q string) []string {
	t.Helper()
	out, code := lab.Run("docker", "exec", store, "psql", "-U", "temporal", "-d", db, "-At", "-F", "|", "-c", q)
	if code != 0 {
		t.Fatalf("psql in %s (%s): exit %d: %.300s\nquery: %s", store, db, code, out, q)
	}
	var rows []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			rows = append(rows, l)
		}
	}
	return rows
}

// edSchema is what the running store's tables look like for the columns this test reads.
type edSchema struct{ runID, treeID string } // SQL expressions: the normalised run_id and tree_id

func edNorm(col, typ string) string {
	if typ == "bytea" {
		return "encode(" + col + ", 'hex')"
	}
	return "lower(replace(" + col + "::text, '-', ''))"
}

// edReadSchema checks the named columns against information_schema and fails clearly on a missing
// one (Temporal 1.32, PostgreSQL schema v12).
func edReadSchema(t *testing.T, store string) edSchema {
	t.Helper()
	want := []struct{ db, table, col string }{
		{"temporal", "executions", "workflow_id"}, {"temporal", "executions", "run_id"},
		{"temporal", "current_executions", "workflow_id"}, {"temporal", "current_executions", "run_id"},
		{"temporal", "history_node", "tree_id"},
		{"temporal_visibility", "executions_visibility", "workflow_id"}, {"temporal_visibility", "executions_visibility", "run_id"},
	}
	var types = map[string]string{}
	for _, w := range want {
		rows := edSQL(t, store, w.db, fmt.Sprintf(
			"select data_type from information_schema.columns where table_name = '%s' and column_name = '%s'", w.table, w.col))
		if len(rows) != 1 {
			t.Fatalf("the store's %s.%s has no column %s (information_schema returned %v): the table or column names this test assumes do not match Temporal's schema",
				w.db, w.table, w.col, rows)
		}
		types[w.table+"."+w.col] = rows[0]
	}
	return edSchema{
		runID:  edNorm("run_id", types["executions.run_id"]),
		treeID: edNorm("tree_id", types["history_node.tree_id"]),
	}
}

func edQuoted(ids []string) string {
	q := make([]string, len(ids))
	for i, id := range ids {
		q[i] = "'" + id + "'"
	}
	return strings.Join(q, ",")
}

// edRunIDs returns, per workflow ID, the normalised Temporal run IDs `executions` holds.
func edRunIDs(t *testing.T, store string, sch edSchema, workflows []string) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	if len(workflows) == 0 {
		return out
	}
	for _, w := range workflows {
		if !edWorkflowID.MatchString(w) {
			t.Fatalf("not a workflow ID: %q", w)
		}
	}
	for _, row := range edSQL(t, store, "temporal", fmt.Sprintf(
		"select workflow_id, %s from executions where workflow_id in (%s)", sch.runID, edQuoted(workflows))) {
		w, r, _ := strings.Cut(row, "|")
		if !edHex.MatchString(r) {
			t.Fatalf("executions.run_id %q of %s is not read as hex", r, w)
		}
		out[w] = append(out[w], r)
	}
	return out
}

// edLeft returns what the store still holds for the workflows and their Temporal run IDs, one line
// per table row group, and a line saying what it examined.
func edLeft(t *testing.T, store string, sch edSchema, workflows, runIDs []string) (left []string, examined string) {
	t.Helper()
	for _, w := range workflows {
		if !edWorkflowID.MatchString(w) {
			t.Fatalf("not a workflow ID: %q", w)
		}
	}
	for _, r := range runIDs {
		if !edHex.MatchString(r) {
			t.Fatalf("not a hex run ID: %q", r)
		}
	}
	for _, q := range []struct{ db, table string }{
		{"temporal", "executions"}, {"temporal", "current_executions"}, {"temporal_visibility", "executions_visibility"},
	} {
		if len(workflows) == 0 {
			continue
		}
		for _, w := range edSQL(t, store, q.db, fmt.Sprintf("select workflow_id from %s where workflow_id in (%s)", q.table, edQuoted(workflows))) {
			left = append(left, q.table+": row of "+w)
		}
	}
	if len(runIDs) > 0 {
		for _, row := range edSQL(t, store, "temporal", fmt.Sprintf(
			"select %s, count(*) from history_node where %s in (%s) group by 1", sch.treeID, sch.treeID, edQuoted(runIDs))) {
			id, n, _ := strings.Cut(row, "|")
			left = append(left, fmt.Sprintf("history_node: %s node(s) of tree_id %s", n, id))
		}
	}
	sort.Strings(left)
	examined = fmt.Sprintf("looked for %d workflow ID(s) in executions (%s rows), current_executions (%s) and executions_visibility (%s), and for %d run ID(s) in history_node (%s rows)",
		len(workflows),
		edSQL(t, store, "temporal", "select count(*) from executions")[0],
		edSQL(t, store, "temporal", "select count(*) from current_executions")[0],
		edSQL(t, store, "temporal_visibility", "select count(*) from executions_visibility")[0],
		len(runIDs),
		edSQL(t, store, "temporal", "select count(*) from history_node")[0])
	return
}

// edDeleteWorkflow deletes one workflow from the durable service with the pinned admin-tools CLI
// on the durable network (as spNamespace runs it).
func edDeleteWorkflow(t *testing.T, network, id string) {
	t.Helper()
	out, code := lab.Run("docker", "run", "--rm", "--network", network, "--read-only", "--cap-drop", "ALL",
		"--security-opt", "no-new-privileges", "--user", "1000:1000", "--tmpfs", "/tmp:mode=1777,size=16m", "-e", "HOME=/tmp",
		"--memory", "128m", "--pids-limit", "64", "--entrypoint", "temporal", lab.Pin(t, "temporal-admin-tools"),
		"workflow", "delete", "--address", "durable:7233", "--namespace", "ap", "--workflow-id", id)
	if code != 0 {
		t.Fatalf("temporal workflow delete %s: exit %d: %s", id, code, out)
	}
}

// ---- the engine store -----------------------------------------------------------------------------

// edStoreDir is the host folder the engine container's /store mount comes from.
func edStoreDir(t *testing.T, l *lab.Lab) string {
	t.Helper()
	out, code := lab.Run("docker", "inspect", "-f", `{{range .Mounts}}{{if eq .Destination "/store"}}{{.Source}}{{end}}{{end}}`, l.Engine)
	dir := strings.TrimSpace(out)
	if code != 0 || dir == "" {
		t.Fatalf("the engine container has no /store mount (exit %d): %s", code, out)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("the engine's store folder %s is not readable by the test: %v", dir, err)
	}
	return dir
}

type edRecord struct {
	ID     string `json:"id"`
	Target string `json:"target"`
	Status string `json:"status"`
}

// edOperationRecords reads every operations/OP-<n>.json of the store.
func edOperationRecords(t *testing.T, root string) []edRecord {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(root, "operations", "OP-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	var recs []edRecord
	for _, f := range files {
		b, err := os.ReadFile(filepath.Clean(f))
		if err != nil {
			t.Fatal(err)
		}
		var r edRecord
		if err := json.Unmarshal(b, &r); err != nil || r.ID == "" {
			t.Fatalf("%s is not an operation record: %v: %.200s", f, err, b)
		}
		recs = append(recs, r)
	}
	return recs
}

// edMentions is whether any string value of the JSON document equals s.
func edMentions(v any, s string) bool {
	switch x := v.(type) {
	case string:
		return x == s
	case map[string]any:
		for _, c := range x {
			if edMentions(c, s) {
				return true
			}
		}
	case []any:
		for _, c := range x {
			if edMentions(c, s) {
				return true
			}
		}
	}
	return false
}

// edStoreLeft returns what the engine store folder still holds of the slice: the operation records
// and bodies of the operations, the slice's folder, and an entry in workers.json. examined says
// what it looked at.
func edStoreLeft(t *testing.T, root, slice string, ops []string) (left []string, examined string) {
	t.Helper()
	files := 0
	for _, op := range ops {
		for _, suffix := range []string{".json", ".body"} {
			files++
			if _, err := os.Stat(filepath.Join(root, "operations", op+suffix)); err == nil {
				left = append(left, "operations/"+op+suffix)
			}
		}
	}
	files++
	if _, err := os.Stat(filepath.Join(root, "slices", slice)); err == nil {
		left = append(left, "slices/"+slice)
	}
	entries := 0
	if b, err := os.ReadFile(filepath.Clean(filepath.Join(root, "workers.json"))); err == nil {
		files++
		var doc any
		if err := json.Unmarshal(b, &doc); err != nil {
			t.Fatalf("workers.json is not JSON: %v: %.200s", err, b)
		}
		if arr, ok := doc.([]any); ok {
			entries = len(arr)
		} else if m, ok := doc.(map[string]any); ok {
			entries = len(m)
		}
		if edMentions(doc, slice) {
			left = append(left, "workers.json: an entry of "+slice)
		}
	}
	examined = fmt.Sprintf("looked for %d operation file(s), the folder slices/%s and workers.json (%d top-level entries) under %s (%d paths)",
		2*len(ops), slice, entries, root, files)
	return
}

// TestErasureSearchesFindPlantedCopies shows the store search finds planted copies and nothing in a
// clean store, and leaves another slice's and operation's files alone (no lab).
func TestErasureSearchesFindPlantedCopies(t *testing.T) {
	write := func(root, name, body string) {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	planted := t.TempDir()
	write(planted, "operations/OP-7.json", `{"id":"OP-7","target":"S-9"}`)
	write(planted, "operations/OP-7.body", `{}`)
	write(planted, "slices/S-9/slice.json", `{}`)
	write(planted, "workers.json", `[{"Slice":"S-9","Step":"code","Endpoint":"e","Grant":"g"}]`)
	left, ex := edStoreLeft(t, planted, "S-9", []string{"OP-7"})
	t.Log(ex)
	if len(left) != 4 {
		t.Errorf("the search found %v in a store holding all four planted copies, want 4", left)
	}
	clean := t.TempDir()
	write(clean, "operations/OP-8.json", `{"id":"OP-8","target":"S-8"}`)
	write(clean, "operations/OP-8.body", `{}`)
	write(clean, "slices/S-8/slice.json", `{}`)
	write(clean, "workers.json", `[{"Slice":"S-8","Step":"code","Endpoint":"e","Grant":"g"}]`)
	if left, _ := edStoreLeft(t, clean, "S-9", []string{"OP-7"}); len(left) != 0 {
		t.Errorf("the search reports %v in a store with only another slice's files", left)
	}
	if left, _ := edStoreLeft(t, t.TempDir(), "S-9", []string{"OP-7"}); len(left) != 0 {
		t.Errorf("the search reports %v in an empty store", left)
	}
}

// ---- the main test --------------------------------------------------------------------------------

// edAnswers is what inspecting an ended run answers.
type edAnswers struct {
	run, comparison any
	events          []Event
	artifacts       map[string][]byte
}

// edRead reads a path, tolerating 503 and no answer for up to 2 minutes (the engine just started).
func edRead(t *testing.T, l *lab.Lab, path string) []byte {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	for {
		st, _, b := do(l, "GET", path, nil)
		if st == 200 {
			return b
		}
		if (st != 503 && st > 0) || time.Now().After(deadline) {
			t.Fatalf("GET %s = %d: %s", path, st, b)
		}
		time.Sleep(time.Second)
	}
}

func edJSON(t *testing.T, b []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("not JSON: %v: %.200s", err, b)
	}
	return v
}

// edInspect reads what inspecting `run` answers: getRun, the whole record, every accepted
// artifact's bytes, and the comparison of `other` (a replay) with it.
func edInspect(t *testing.T, l *lab.Lab, run, other string) edAnswers {
	t.Helper()
	a := edAnswers{artifacts: map[string][]byte{}}
	a.run = edJSON(t, edRead(t, l, "/runs/"+run))
	a.comparison = edJSON(t, edRead(t, l, "/runs/"+other+"/comparison?with="+run))
	a.events = events(t, l, run)
	for _, hashes := range acceptedArtifacts(a.events) {
		for _, h := range hashes {
			a.artifacts[h] = edRead(t, l, "/runs/"+run+"/artifacts/"+h)
		}
	}
	return a
}

func edSame(t *testing.T, when string, before, after edAnswers) {
	t.Helper()
	if len(before.events) == 0 || len(before.artifacts) == 0 {
		t.Fatalf("the answers recorded before hold %d event(s) and %d artifact(s): nothing to compare", len(before.events), len(before.artifacts))
	}
	if !reflect.DeepEqual(before.run, after.run) {
		t.Errorf("%s: getRun answers %v, it answered %v before", when, after.run, before.run)
	}
	if !reflect.DeepEqual(before.events, after.events) {
		t.Errorf("%s: the run's record has %d event(s), it had %d before (or they differ)", when, len(after.events), len(before.events))
	}
	if !reflect.DeepEqual(before.comparison, after.comparison) {
		t.Errorf("%s: the comparison answers %v, it answered %v before", when, after.comparison, before.comparison)
	}
	if !reflect.DeepEqual(before.artifacts, after.artifacts) {
		t.Errorf("%s: the run's artifacts differ from those read before (%d now, %d before)", when, len(after.artifacts), len(before.artifacts))
	}
}

func edPoll(limit time.Duration, f func() bool) bool {
	deadline := time.Now().Add(limit)
	for {
		if f() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(3 * time.Second)
	}
}

func TestErasureReachesTheDurableService(t *testing.T) {
	l, slice := buildLab(t, happySet)
	network, _, store := spDurable(t, l)
	sch := edReadSchema(t, store)
	root := edStoreDir(t, l)

	// 1. A built slice: an ended run and its replay.
	op := startBuild(t, l, slice, "notes")
	run1 := op.Target
	if r := waitRun(t, l, run1, 10*time.Minute, ended); r.Outcome != "done" {
		t.Fatalf("the first run is %+v, want done", r)
	}
	l.StopStepWorkers(t, slice, "notes")
	replay := func(project string) string {
		l.StepWorkers(t, slice, project) // a replay's project has its own step workers, which stay registered
		o := l.Wait(t, l.Accept(t, "POST", "/runs/"+run1+"/replay", map[string]string{"project": project}))
		if o.Status != "succeeded" || o.Result.Run == "" {
			t.Fatalf("replayRun into %s: %+v (error %+v)", project, o, o.Error)
		}
		if r := getRun(t, l, o.Result.Run); r.Status != "ended" || r.Outcome != "done" {
			t.Fatalf("the replay %s is %+v, want an ended, done run", o.Result.Run, r)
		}
		return o.Result.Run
	}
	run2 := replay("notes-replay")
	l.StopStepWorkers(t, slice, "notes-replay")

	// 2. FR-015: what inspecting RUN-1 answers, recorded; its workflow deleted; the engine restarted.
	before := edInspect(t, l, run1, run2)
	arts := acceptedArtifacts(before.events)
	if rows := edRunIDs(t, store, sch, []string{run1}); len(rows[run1]) == 0 {
		t.Fatalf("the durable store holds no workflow %s, so there is nothing to delete (the run must have one in feature 002)", run1)
	}
	edDeleteWorkflow(t, network, run1)
	if !edPoll(edWait, func() bool { left, _ := edLeft(t, store, sch, []string{run1}, nil); return len(left) == 0 }) {
		left, ex := edLeft(t, store, sch, []string{run1}, nil)
		t.Fatalf("the workflow %s is still in the durable store %s after its deletion: %v (%s)", run1, edWait, left, ex)
	}
	l.RestartEngine(t)
	edSame(t, "after RUN-1's workflow was deleted and the engine restarted", before, edInspect(t, l, run1, run2))

	run3 := replay("notes-replay2") // its step workers stay registered: workers.json holds the slice's entries
	revs := events(t, l, run3)
	if n := count(revs, "AttemptMade", ""); n != 0 {
		t.Errorf("the replay of a run with no workflow made %d attempt(s); every step must reuse its result", n)
	}
	rgot := acceptedArtifacts(revs)
	for s, hashes := range arts {
		if !reflect.DeepEqual(rgot[s], hashes) {
			t.Errorf("step %s: the replay of the run with no workflow has artifacts %v, the run %v", s, rgot[s], hashes)
		}
	}
	var cmp struct{ Items []json.RawMessage }
	get(t, l, "/runs/"+run3+"/comparison?with="+run1, &cmp)
	if len(cmp.Items) != 0 {
		t.Errorf("the replay of the run with no workflow differs from it in %d place(s): %s", len(cmp.Items), cmp.Items)
	}

	// 3. Before the erasure: the workflows and Temporal run IDs of the slice, and the store's files.
	runs := []string{run1, run2, run3}
	var ops []string
	for _, r := range edOperationRecords(t, root) {
		if r.Target == slice || r.Target == run1 || r.Target == run2 || r.Target == run3 {
			if r.Status == "" {
				t.Fatalf("operation %s has not ended before the erasure: %+v", r.ID, r)
			}
			ops = append(ops, r.ID)
		}
	}
	if len(ops) < 8 {
		t.Fatalf("the store holds %d ended operation(s) of the slice %v, want at least 8 (create, answers, 3 approvals, startRun, 2 replays)", len(ops), ops)
	}
	t.Logf("operations of the slice: %v; runs: %v", ops, runs)
	workflows := append(append([]string{}, runs...), ops...)
	live := edRunIDs(t, store, sch, workflows)
	var liveIDs, runIDs []string
	for _, w := range workflows {
		if w == run1 {
			continue // deleted above
		}
		if len(live[w]) == 0 {
			t.Errorf("the durable store holds no workflow %s before the erasure (every run and operation has one)", w)
			continue
		}
		liveIDs = append(liveIDs, w)
		runIDs = append(runIDs, live[w]...)
	}
	// The planted copy: the search finds these rows before the erasure, in every table.
	pre, ex := edLeft(t, store, sch, liveIDs, runIDs)
	t.Log("before the erasure: " + ex)
	for _, table := range []string{"executions: ", "current_executions: ", "executions_visibility: ", "history_node: "} {
		found := 0
		for _, p := range pre {
			if strings.HasPrefix(p, table) {
				found++
			}
		}
		if found == 0 {
			t.Fatalf("the search finds no row in %s before the erasure (%d workflow ID(s), %d run ID(s)): it cannot judge a leftover; found %v", strings.TrimSuffix(table, ": "), len(liveIDs), len(runIDs), pre)
		}
	}
	for _, id := range runIDs {
		if l, _ := edLeft(t, store, sch, nil, []string{id}); len(l) == 0 {
			t.Fatalf("history_node holds no row with tree_id %s (a Temporal run ID) before the erasure: tree_id does not carry the run ID", id)
		}
	}
	if left, ex := edStoreLeft(t, root, slice, ops); len(left) < len(ops)+1 {
		t.Fatalf("before the erasure the store search finds only %v (%s); it must find the operations' files, the slice's folder and its worker entries", left, ex)
	} else {
		t.Log("before the erasure: " + ex)
		if !strings.Contains(strings.Join(left, " "), "workers.json") {
			t.Fatalf("workers.json holds no entry of %s before the erasure; the step workers' registrations were not kept: %v", slice, left)
		}
	}

	// The erasure, followed to its end.
	delID := l.Accept(t, "DELETE", "/slices/"+slice, nil)
	del := l.Wait(t, delID)
	if del.Status != "succeeded" {
		t.Fatalf("deleteSlice: %+v (error %+v)", del, del.Error)
	}
	if del.Target != slice {
		t.Errorf("the erasing operation's target is %q, want the slice %s", del.Target, slice)
	}
	st, _, raw := do(l, "GET", "/operations/"+delID, nil)
	if st != 200 {
		t.Fatalf("getOperation %s (the erasing operation) = %d: %s", delID, st, raw)
	}
	for _, id := range edAnyID.FindAllString(string(raw), -1) {
		if id != delID {
			t.Errorf("the erasing operation's record names %s; it may hold only the slice's ID as target: %s", id, raw)
		}
	}

	// Other operations' records are gone at once, and so are the slice and its runs.
	for _, id := range ops {
		if st, _, b := do(l, "GET", "/operations/"+id, nil); st != 404 {
			t.Errorf("getOperation %s after the erasure = %d, want 404 (its record is removed at once): %.200s", id, st, b)
		}
	}
	for _, r := range runs {
		if st, _, _ := do(l, "GET", "/runs/"+r, nil); st != 404 {
			t.Errorf("getRun %s after the erasure = %d, want 404", r, st)
		}
	}
	refused(t, l, "DELETE", "/slices/"+slice, nil, 404, "NotFound") // a repeat finds nothing

	// 5. The engine store.
	if left, ex := edStoreLeft(t, root, slice, ops); len(left) > 0 {
		t.Errorf("the engine store still holds %v after the erasure (%s)", left, ex)
	} else {
		t.Log("after the erasure: " + ex)
	}

	// 4. The durable store, polled: deletion is asynchronous. The erasing operation's own workflow stays.
	var left []string
	var examined string
	if !edPoll(edWait, func() bool { left, examined = edLeft(t, store, sch, workflows, runIDs); return len(left) == 0 }) {
		t.Errorf("after %s the durable store still holds %d row group(s) of the slice's workflows: %v (%s)", edWait, len(left), left, examined)
	} else {
		t.Log("after the erasure: " + examined)
	}
	if own := edRunIDs(t, store, sch, []string{delID}); len(own[delID]) == 0 {
		t.Errorf("the erasing operation's own workflow %s is gone from the durable store; it stays until the retention removes it", delID)
	}
}
