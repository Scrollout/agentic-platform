package api

// Tests for the slice read operations served by the engine (task T038c): listSlices and
// getSlice. They are written before the code and fail until it exists. The tests are the
// specification; the implementer must not change them (ADR 0029).
//
// # Interface the tests use (implement exactly this in package api)
//
//	// SliceStore is what the API needs from the run store about a slice.
//	type SliceStore interface {
//		ArtifactCount(sliceID string) (int, error)
//		EraseSlice(sliceID string) error
//	}
//
//	type Config struct {
//		...
//		Slices *slices.Service // nil: the handler behaves as the skeleton does
//		Store  SliceStore
//	}
//
// With Slices set the server does slice work itself; Config.Work is then used only for other
// kinds of operation, so these tests assert that Work is never called for a slice operation.
// A slice operation's target is the slice's ID, which the server draws from Service.Reserve
// when it accepts createSlice, so the operation names the slice before it exists.
//
// The helpers below (the sp prefix) are shared by slices_write_test.go and
// slices_startrun_test.go.

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentic-platform/engine/slices"
	"github.com/agentic-platform/engine/store"
	"github.com/agentic-platform/slice"
)

const specDir = "preview/" + apiVersion

// ---- fakes ----

type spResult struct {
	p   slices.Proposal
	err error
}

// spAnalyst answers one result per call, in order; the last repeats.
type spAnalyst struct {
	mu      sync.Mutex
	results []spResult
	inputs  []slices.AnalystInput
}

func (a *spAnalyst) Propose(_ context.Context, in slices.AnalystInput) (slices.Proposal, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	i := min(len(a.inputs), len(a.results)-1)
	a.inputs = append(a.inputs, in)
	return a.results[i].p, a.results[i].err
}

func (a *spAnalyst) calls() int { a.mu.Lock(); defer a.mu.Unlock(); return len(a.inputs) }

type spDiff struct{}

func (spDiff) Diff(context.Context, string, string) ([]slices.ContractFinding, error) {
	return nil, nil
}

type spLint struct{ findings []slices.LintFinding }

func (l spLint) Lint(context.Context, string) ([]slices.LintFinding, error) { return l.findings, nil }

// spStore is a SliceStore that counts artifacts by hand and records erasures.
type spStore struct {
	mu     sync.Mutex
	counts map[string]int
	erased []string
}

func (s *spStore) ArtifactCount(id string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts[id], nil
}

func (s *spStore) EraseSlice(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.erased = append(s.erased, id)
	return nil
}

func (s *spStore) erasures() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.erased...)
}

// ---- rig ----

type spRig struct {
	*rig
	svc     *slices.Service
	analyst *spAnalyst
	store   *spStore
	schemas *slice.Schemas
}

func spNotes(t *testing.T) slices.Proposal {
	t.Helper()
	raw, err := os.ReadFile("../slices/testdata/proposal-notes.json")
	if err != nil {
		t.Fatal(err)
	}
	var p slices.Proposal
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func spPattern(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("../../catalogue/api-feature-go/pattern.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func spSchemas(t *testing.T) *slice.Schemas {
	t.Helper()
	s, err := slice.LoadSchemas(specDir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// newSP returns a handler over a real slices.Service whose analyst answers results in order.
// mods change the service's Config; a nil store is replaced by a spStore.
func newSP(t *testing.T, results []spResult, st SliceStore, mods ...func(*slices.Config)) *spRig {
	t.Helper()
	sp := &spRig{analyst: &spAnalyst{results: results}, store: &spStore{counts: map[string]int{}}, schemas: spSchemas(t)}
	cfg := slices.Config{Schemas: sp.schemas, Analyst: sp.analyst, Diff: spDiff{}, Lint: spLint{}, Pattern: spPattern(t)}
	for _, m := range mods {
		m(&cfg)
	}
	svc, err := slices.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	sp.svc = svc
	if st == nil {
		st = sp.store
	}
	sp.rig = newRig(t, func(c *Config) { c.Slices, c.Store = svc, st })
	return sp
}

func newNotes(t *testing.T, mods ...func(*slices.Config)) *spRig {
	t.Helper()
	return newSP(t, []spResult{{p: spNotes(t)}}, nil, mods...)
}

// seed creates a slice through the service (S-1, S-2, ...).
func (sp *spRig) seed(t *testing.T, description string) slices.Slice {
	t.Helper()
	res, err := sp.svc.Create(context.Background(), description)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return res.Slice
}

func (sp *spRig) approveAll(t *testing.T, id string) slices.Slice {
	t.Helper()
	sl, err := sp.svc.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []struct {
		part slices.Part
		hash string
	}{{slices.PartGoals, sl.Parts.Goals}, {slices.PartRules, sl.Parts.Rules}, {slices.PartContract, sl.Parts.Contract}} {
		if sl, err = sp.svc.Approve(context.Background(), id, p.part, p.hash); err != nil {
			t.Fatalf("approve %s: %v", p.part, err)
		}
	}
	return sl
}

func spURL(id, sub string) string {
	return "/slices/" + id + sub + "?api-version=" + apiVersion
}

// spOp is an operation as the tests read it.
type spOp struct {
	ID     string            `json:"id"`
	Kind   string            `json:"kind"`
	Status string            `json:"status"`
	Target string            `json:"target"`
	Result map[string]string `json:"result"`
	Error  *struct {
		Code   string `json:"code"`
		Detail string `json:"detail"`
	} `json:"error"`
}

// accepted checks a 202 and returns the operation.
func (sp *spRig) accepted(t *testing.T, p reply, kind, target string) spOp {
	t.Helper()
	op := p.operation(t, http.StatusAccepted)
	if string(op.Kind) != kind || op.Target != target {
		t.Fatalf("operation kind/target = %s/%s, want %s/%s", op.Kind, op.Target, kind, target)
	}
	return sp.settle(t, op.Id)
}

// settle follows the operation until it has ended.
func (sp *spRig) settle(t *testing.T, id string) spOp {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		r := sp.do("GET", "/operations/"+id+"?api-version="+apiVersion, clientToken, "", nil)
		if r.Code != 200 {
			t.Fatalf("getOperation %s = %d: %s", id, r.Code, r.Body.String())
		}
		var op spOp
		if err := json.Unmarshal(r.Body.Bytes(), &op); err != nil {
			t.Fatal(err)
		}
		switch op.Status {
		case "succeeded", "failed", "cancelled":
			return op
		}
		if time.Now().After(deadline) {
			t.Fatalf("operation %s is still %q after 5s", id, op.Status)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// noOperation fails when the engine has created any operation, as a synchronous refusal must
// not create one.
func (sp *spRig) noOperation(t *testing.T) {
	t.Helper()
	sp.do("GET", "/operations/OP-1?api-version="+apiVersion, clientToken, "", nil).problem(t, 404, "NotFound")
}

func (sp *spRig) noWork(t *testing.T) {
	t.Helper()
	if n := sp.rec.count(); n != 0 {
		t.Errorf("Config.Work was called %d times for slice operations; the server does slice work itself", n)
	}
}

// ---- the slice as a client reads it ----

type spSlice struct {
	ID          string            `json:"id"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	State       string            `json:"state"`
	LatestRun   *string           `json:"latest_run"`
	ArtifactN   *int              `json:"artifact_count"`
	Goals       []slices.Goal     `json:"goals"`
	Terms       []slices.Term     `json:"terms"`
	Rules       []slices.Rule     `json:"rules"`
	RuleText    map[string]string `json:"rule_text"`
	Examples    string            `json:"examples"`
	Contract    string            `json:"contract"`
	Questions   []struct {
		ID      string   `json:"id"`
		About   string   `json:"about"`
		Text    string   `json:"text"`
		Options []string `json:"options"`
		Answer  *struct {
			Option *int   `json:"option"`
			Text   string `json:"text"`
		} `json:"answer"`
		AnsweredAt *time.Time `json:"answered_at"`
	} `json:"questions"`
	Parts     map[string]string `json:"parts"`
	Approvals []struct {
		Part        string    `json:"part"`
		ContentHash string    `json:"content_hash"`
		At          time.Time `json:"at"`
	} `json:"approvals"`
	Checks []struct {
		Part   string `json:"part"`
		Check  string `json:"check"`
		Passed bool   `json:"passed"`
		Reason string `json:"reason"`
	} `json:"checks"`
}

type spHistory struct {
	Kind        string    `json:"kind"`
	At          time.Time `json:"at"`
	Model       string    `json:"model"`
	AgentDigest string    `json:"agent_digest"`
	Refs        []string  `json:"refs"`
}

type spSliceFull struct {
	spSlice
	History []spHistory `json:"history"`
}

func (sp *spRig) getSlice(t *testing.T, id string) (spSliceFull, []byte) {
	t.Helper()
	r := sp.do("GET", spURL(id, ""), clientToken, "", nil)
	if r.Code != 200 {
		t.Fatalf("getSlice %s = %d: %s", id, r.Code, r.Body.String())
	}
	if ct := r.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q", ct)
	}
	var s spSliceFull
	if err := json.Unmarshal(r.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	return s, r.Body.Bytes()
}

// same compares two values by their JSON, with a nil list and an empty one equal.
func same(t *testing.T, what string, got, want any) {
	t.Helper()
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	norm := func(b []byte) string { return strings.ReplaceAll(string(b), "null", "[]") }
	if norm(g) != norm(w) {
		t.Errorf("%s:\n got  %s\n want %s", what, g, w)
	}
}

// ---- listSlices ----

type spList struct {
	Items []struct {
		ID        string  `json:"id"`
		Title     string  `json:"title"`
		State     string  `json:"state"`
		LatestRun *string `json:"latest_run"`
	} `json:"items"`
	Next *string `json:"next_page_token"`
}

func (sp *spRig) list(t *testing.T, token, query string) spList {
	t.Helper()
	r := sp.do("GET", slicesURL(query), token, "", nil)
	if r.Code != 200 {
		t.Fatalf("listSlices = %d: %s", r.Code, r.Body.String())
	}
	var l spList
	if err := json.Unmarshal(r.Body.Bytes(), &l); err != nil {
		t.Fatal(err)
	}
	return l
}

func TestListSlicesIsNewestFirstWithTheSummaryFields(t *testing.T) {
	sp := newNotes(t)
	var want []slices.Slice
	for _, d := range []string{"one", "two", "three"} {
		want = append(want, sp.seed(t, d))
	}
	approved := sp.approveAll(t, "S-1")
	for _, token := range []string{clientToken, pageToken} { // the page token may read too
		l := sp.list(t, token, "")
		if len(l.Items) != 3 || l.Next != nil {
			t.Fatalf("items %d, next %v; want 3 and no next page", len(l.Items), l.Next)
		}
		for i, id := range []string{"S-3", "S-2", "S-1"} {
			it := l.Items[i]
			if it.ID != id {
				t.Fatalf("item %d is %s, want %s (newest first)", i, it.ID, id)
			}
			if it.Title != want[0].Goals[0].Text {
				t.Errorf("%s title %q, want the first goal %q", id, it.Title, want[0].Goals[0].Text)
			}
			if it.LatestRun != nil {
				t.Errorf("%s has latest_run %q, but no run exists", id, *it.LatestRun)
			}
		}
		if l.Items[2].State != approved.State || l.Items[0].State != want[2].State {
			t.Errorf("states %q/%q, want %q/%q", l.Items[2].State, l.Items[0].State, approved.State, want[2].State)
		}
	}
	sp.noWork(t)
}

func TestListSlicesPagesAreFiftyAndFollowTheirTokens(t *testing.T) {
	sp := newNotes(t)
	for i := 1; i <= 51; i++ {
		sp.seed(t, "slice "+strconv.Itoa(i))
	}
	first := sp.list(t, clientToken, "")
	if len(first.Items) != 50 || first.Next == nil || *first.Next == "" {
		t.Fatalf("first page: %d items, next %v; want 50 and a next_page_token", len(first.Items), first.Next)
	}
	if first.Items[0].ID != "S-51" || first.Items[49].ID != "S-2" {
		t.Errorf("first page runs %s to %s, want S-51 to S-2", first.Items[0].ID, first.Items[49].ID)
	}
	second := sp.list(t, clientToken, "&page_token="+*first.Next)
	if len(second.Items) != 1 || second.Items[0].ID != "S-1" || second.Next != nil {
		t.Fatalf("second page: %+v, want S-1 alone and no next_page_token", second)
	}
}

func TestListSlicesRefusesAPageTokenItDidNotIssue(t *testing.T) {
	sp := newNotes(t)
	sp.seed(t, "one")
	for _, tok := range []string{"bogus", "!!!", "MTAw"} {
		sp.do("GET", slicesURL("&page_token="+tok), clientToken, "", nil).problem(t, 400, "InvalidRequest")
	}
}

// ---- getSlice ----

func TestGetSliceMapsEveryFieldOfTheServiceSlice(t *testing.T) {
	first := spNotes(t)
	first.Questions = []slices.QuestionDraft{
		{About: "R-1", Text: "Keep forever?", Options: []string{"yes", "no"}},
		{About: "G-1", Text: "Which notes?"},
	}
	revised := spNotes(t)
	revised.Goals[0].Text = "Keep a note for good"
	revised.Model, revised.AgentDigest = "revise-model", strings.Repeat("c", 64)
	sp := newSP(t, []spResult{{p: first}, {p: revised}}, nil)
	created := sp.seed(t, "Let me keep notes.")
	ctx := context.Background()
	one := 0
	if _, err := sp.svc.Answer(ctx, created.ID, []slices.Answer{{Question: "Q-1", Option: &one}, {Question: "Q-2", Text: "All of them"}}); err != nil {
		t.Fatal(err)
	}
	cur, err := sp.svc.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sp.svc.Approve(ctx, created.ID, slices.PartGoals, cur.Parts.Goals); err != nil {
		t.Fatal(err)
	}
	want, _ := sp.svc.Get(created.ID)

	got, raw := sp.getSlice(t, created.ID)
	if got.ID != want.ID || got.Description != want.Description || got.State != want.State {
		t.Errorf("id/description/state = %q/%q/%q, want %q/%q/%q", got.ID, got.Description, got.State, want.ID, want.Description, want.State)
	}
	if got.Title != want.Goals[0].Text {
		t.Errorf("title %q, want the first goal's text %q", got.Title, want.Goals[0].Text)
	}
	same(t, "goals", got.Goals, want.Goals)
	same(t, "terms", got.Terms, want.Terms)
	same(t, "rules", got.Rules, want.Rules)
	if len(got.RuleText) != len(want.Rules) {
		t.Errorf("rule_text has %d entries, want one per rule (%d)", len(got.RuleText), len(want.Rules))
	}
	for _, r := range want.Rules {
		if got.RuleText[r.ID] == "" {
			t.Errorf("rule_text lacks an EARS sentence for %s", r.ID)
		}
	}
	if got.Examples != want.Examples || got.Contract != want.Contract {
		t.Errorf("examples or contract differ from the service's")
	}
	if got.Parts["goals"] != want.Parts.Goals || got.Parts["rules"] != want.Parts.Rules || got.Parts["contract"] != want.Parts.Contract || len(got.Parts) != 3 {
		t.Errorf("parts %v, want %+v", got.Parts, want.Parts)
	}

	if len(got.Questions) != len(want.Questions) || len(want.Questions) != 2 {
		t.Fatalf("questions %d, want %d", len(got.Questions), len(want.Questions))
	}
	for i, q := range want.Questions {
		g := got.Questions[i]
		if g.ID != q.ID || g.About != q.About || g.Text != q.Text {
			t.Errorf("question %d = %+v, want %+v", i, g, q)
		}
		same(t, "options of "+q.ID, g.Options, q.Options)
		if g.Answer == nil || g.AnsweredAt == nil || !g.AnsweredAt.Equal(q.AnsweredAt) {
			t.Errorf("question %s lacks its answer or answered_at: %+v", q.ID, g)
			continue
		}
		if (q.Answer.Option != nil) != (g.Answer.Option != nil) || (q.Answer.Option != nil && *q.Answer.Option != *g.Answer.Option) || g.Answer.Text != q.Answer.Text {
			t.Errorf("question %s answer %+v, want %+v", q.ID, g.Answer, q.Answer)
		}
	}

	if len(got.Approvals) != len(want.Approvals) || len(want.Approvals) != 1 {
		t.Fatalf("approvals %d, want %d", len(got.Approvals), len(want.Approvals))
	}
	if a, w := got.Approvals[0], want.Approvals[0]; a.Part != string(w.Part) || a.ContentHash != w.ContentHash || !a.At.Equal(w.At) {
		t.Errorf("approval %+v, want %+v", a, w)
	}

	if len(got.Checks) != len(want.Checks) || len(want.Checks) == 0 {
		t.Fatalf("checks %d, want %d (the latest result of every check)", len(got.Checks), len(want.Checks))
	}
	for i, c := range want.Checks {
		if g := got.Checks[i]; g.Part != c.Part || g.Check != c.Check || g.Passed != c.Passed || g.Reason != c.Reason {
			t.Errorf("check %d = %+v, want %+v", i, g, c)
		}
	}

	if len(got.History) != len(want.History) || len(want.History) < 4 {
		t.Fatalf("history %d entries, want %d (proposed, answered, revised, approved)", len(got.History), len(want.History))
	}
	for i, h := range want.History {
		g := got.History[i]
		if g.Kind != h.Kind || g.Model != h.Model || g.AgentDigest != h.AgentDigest || !g.At.Equal(h.At) {
			t.Errorf("history %d = %+v, want %+v", i, g, h)
		}
		same(t, "refs of history "+strconv.Itoa(i), g.Refs, h.Refs)
	}
	if rev := got.History[2]; rev.Model != "revise-model" {
		t.Errorf("the revision names model %q, want revise-model", rev.Model)
	}

	if err := sp.schemas.CheckSlice(raw); err != nil {
		t.Errorf("the response does not match slice.schema.json: %v", err)
	}
	sp.noWork(t)
}

func TestGetSliceCarriesTheStoresArtifactCount(t *testing.T) {
	sp := newNotes(t)
	sp.seed(t, "one")
	sp.seed(t, "two")
	sp.store.counts["S-1"] = 2
	one, raw := sp.getSlice(t, "S-1")
	if one.ArtifactN == nil || *one.ArtifactN != 2 {
		t.Errorf("artifact_count = %v, want 2 from the store", one.ArtifactN)
	}
	if err := sp.schemas.CheckSlice(raw); err != nil {
		t.Errorf("the response does not match slice.schema.json: %v", err)
	}
	two, _ := sp.getSlice(t, "S-2")
	if two.ArtifactN == nil || *two.ArtifactN != 0 {
		t.Errorf("artifact_count = %v, want 0 sent explicitly (the engine always sends it)", two.ArtifactN)
	}
}

func TestGetSliceCountsDistinctArtifactsInARealStore(t *testing.T) {
	schemas := spSchemas(t)
	st, err := store.Open(t.TempDir(), schemas)
	if err != nil {
		t.Fatal(err)
	}
	sp := newSP(t, []spResult{{p: spNotes(t)}}, st)
	sp.seed(t, "one")
	for _, data := range []string{"alpha", "beta", "alpha"} {
		if _, err := st.PutArtifact("S-1", []byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := sp.getSlice(t, "S-1")
	if got.ArtifactN == nil || *got.ArtifactN != 2 {
		t.Errorf("artifact_count = %v, want 2 (the same bytes count once)", got.ArtifactN)
	}
}

func TestGetSliceMayBeCalledWithThePageToken(t *testing.T) {
	sp := newNotes(t)
	sp.seed(t, "one")
	if r := sp.do("GET", spURL("S-1", ""), pageToken, "", nil); r.Code != 200 {
		t.Errorf("getSlice with the page token = %d, want 200", r.Code)
	}
}

func TestGetUnknownSliceIs404(t *testing.T) {
	sp := newNotes(t)
	sp.seed(t, "one")
	sp.do("GET", spURL("S-99", ""), clientToken, "", nil).problem(t, 404, "NotFound")
}
