package api

// T020 of feature 012 (US3; FR-008, FR-009; research R6, R7; data model "Trial set", "Trial result"
// `previous`; spec Clarifications 2026-11-28, last bullet) and T053 (a running set's read):
// listTrialSets and the comparison of getTrialSet through HTTP. Written before the code (T022) by a
// test author, judged from the spec, the engine contract (listTrialSets, TrialSetList, TrialSet,
// TrialResult, TrialComparison) and the interface sheet; the code author must not change it
// (ADR 0029). It reuses trRig and its helpers as they are; only new helpers (prefix `tl`) are here.
//
// How the sets are made: real sets on the trRig (faked workers; a trial whose tests fail is handed
// over, so a set can end with a done and a handed-over trial); sets a real run cannot make cheaply
// (a cancelled or failed set with an ended trial, a stopped one, one with no ended trial, 51 sets
// for a page) are planted by writing a copy of a real set's record, with another ID, state or
// runs, through the store the engine reads (store.PutTrialSet), after giving out the IDs in
// between through the store's own sequence. The "other method" of the spec's Independent Test is
// planted by rewriting a finished set's recorded `method`.
//
// Edge cases checked: newest first; exactly 50 sets (no token) and 51 (a token, the second page
// holds the rest, no set twice or missed); a token of another slice's list, of the same slice read
// the other way round, and garbage: 400 InvalidRequest; an unknown slice 404; each item's state,
// trials, method, done, ended, unstable; `previous` on getTrialSet and on the list item alike (same
// value); the Independent Test (both methods named, less_reliable, methods_differ); an equal share
// (no less_reliable, methods equal); a rise and a fall to the extremes (+1, -1); a cancelled, a
// failed and an ended-with-no-trial-ended set skipped for an older ended one; a stopped set chosen
// as previous; the first set, a running set (before and after a trial ended), a set stopped with no
// ended trial: no `previous` key at all (not null); a running set turning into an ended one gaining
// `previous`; T053's running read (steps and totals over every started trial, the unfinished one's
// partial values, `ended` counting only finished trials, no `previous`).
//
// Left out, and why: a `running` earlier set (the engine allows one running set per slice, so a
// running set is never earlier than the one being read; the unit tests of trials.Previous cover
// it); a crash-left `running` set shown `failed` through its failed operation as the earlier set
// (repair seams are in trial_sets_failed_test.go; the unit tests cover a failed earlier set);
// seconds, tokens and cost of the running read (the unit test measures_test.go:439 covers them);
// a page token whose last set was erased meanwhile (sets are only erased with their slice).
//
// What already passes today, and why that is right: the list's order, paging, tokens, 404 and item
// fields exist since US1 (listTrialSets is implemented); T053's steps and totals exist since US2.
// What fails today is every assertion on `previous`.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/agentic-platform/engine/machine"
)

// ---- views of the answers ----

type tlPrev struct {
	TrialSet      string  `json:"trial_set"`
	Method        string  `json:"method"`
	Done          int     `json:"done"`
	Ended         int     `json:"ended"`
	ShareChange   float64 `json:"share_change"`
	LessReliable  bool    `json:"less_reliable"`
	MethodsDiffer bool    `json:"methods_differ"`
}

type tlSpread struct{ Lowest, Median, Highest float64 }

type tlSet struct {
	ID     string `json:"id"`
	Slice  string `json:"slice"`
	Trials int    `json:"trials"`
	Method string `json:"method"`
	State  string `json:"state"`
	Result struct {
		Ended    int  `json:"ended"`
		Done     int  `json:"done"`
		Unstable bool `json:"unstable"`
		Outcomes []struct {
			Trial   int     `json:"trial"`
			Run     string  `json:"run"`
			Outcome *string `json:"outcome"`
		} `json:"outcomes"`
		Steps []struct {
			Step     string               `json:"step"`
			Reached  int                  `json:"reached"`
			Measures map[string]*tlSpread `json:"measures"`
		} `json:"steps"`
		Totals   map[string]*tlSpread `json:"totals"`
		Previous *tlPrev              `json:"previous"`
	} `json:"result"`
}

// tlParse reads one set as the API answered it, and whether its result has a `previous` key at all
// (a null counts as present: the contract's property is absent, not null).
func tlParse(t *testing.T, raw []byte) (tlSet, bool) {
	t.Helper()
	var v tlSet
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("a trial set: %v: %s", err, raw)
	}
	var keys struct {
		Result map[string]json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	_, has := keys.Result["previous"]
	return v, has
}

// tlGet reads a set with getTrialSet.
func (x *trRig) tlGet(id string) (tlSet, bool) {
	x.t.Helper()
	p := x.do("GET", apiPath("/trial-sets/"+id), "")
	if p.Code != 200 {
		x.t.Fatalf("getTrialSet %s = %d: %s", id, p.Code, p.Body.String())
	}
	return tlParse(x.t, p.Body.Bytes())
}

type tlPage struct {
	Items []tlSet
	Has   []bool // whether item i has a `previous` key
	Next  *string
}

// tlListReply is listTrialSets of a slice, with an optional page token, unparsed.
func (x *trRig) tlListReply(slice string, token *string) reply {
	x.t.Helper()
	u := spURL(slice, "/trial-sets")
	if token != nil {
		u += "&page_token=" + url.QueryEscape(*token)
	}
	return x.do("GET", u, "")
}

// tlList is one page of listTrialSets, which must answer 200.
func (x *trRig) tlList(slice string, token *string) tlPage {
	x.t.Helper()
	p := x.tlListReply(slice, token)
	if p.Code != 200 {
		x.t.Fatalf("listTrialSets %s = %d: %s", slice, p.Code, p.Body.String())
	}
	var raw struct {
		Items []json.RawMessage `json:"items"`
		Next  *string           `json:"next_page_token"`
	}
	if err := json.Unmarshal(p.Body.Bytes(), &raw); err != nil {
		x.t.Fatal(err)
	}
	pg := tlPage{Next: raw.Next}
	for _, it := range raw.Items {
		v, has := tlParse(x.t, it)
		pg.Items = append(pg.Items, v)
		pg.Has = append(pg.Has, has)
	}
	return pg
}

func tlIDs(items []tlSet) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.ID)
	}
	return out
}

// ---- planting ----

// tlRecord is a set's stored record and its slice.
func (x *trRig) tlRecord(id string) (string, map[string]any) {
	x.t.Helper()
	slice, b, err := x.st.TrialSet(id)
	if err != nil {
		x.t.Fatalf("the record of %s: %v", id, err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		x.t.Fatal(err)
	}
	return slice, m
}

func (x *trRig) tlPut(slice, id string, m map[string]any) {
	x.t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		x.t.Fatal(err)
	}
	if err := x.st.PutTrialSet(slice, id, b); err != nil {
		x.t.Fatalf("planting %s in %s: %v", id, slice, err)
	}
}

// tlPlant writes a copy of the record of from under the ID id (in the slice of from, or toSlice
// when set), changed by mutate.
func (x *trRig) tlPlant(from, id, toSlice string, mutate func(map[string]any)) {
	x.t.Helper()
	slice, m := x.tlRecord(from)
	m["id"] = id
	if toSlice != "" {
		slice, m["slice"] = toSlice, toSlice
	}
	mutate(m)
	x.tlPut(slice, id, m)
}

// tlReserve gives out the set IDs up to and including TS-n through the store's sequence, so a
// planted set holds a number no real set can take.
func (x *trRig) tlReserve(n int) {
	x.t.Helper()
	want := fmt.Sprintf("TS-%d", n)
	for range n + 1 {
		id, err := x.st.NextTrialSetID()
		if err != nil {
			x.t.Fatal(err)
		}
		if id == want {
			return
		}
	}
	x.t.Fatalf("the ID sequence never reached %s", want)
}

// tlSetMethod rewrites the method a finished set was accepted with: the planted "other method".
func (x *trRig) tlSetMethod(id, method string) {
	x.t.Helper()
	slice, m := x.tlRecord(id)
	m["method"] = method
	x.tlPut(slice, id, m)
}

func tlWantPrev(t *testing.T, what string, got *tlPrev, has bool, want *tlPrev) {
	t.Helper()
	if want == nil {
		if got != nil || has {
			t.Errorf("%s: result.previous = %+v (key present: %v), want no `previous` key at all", what, got, has)
		}
		return
	}
	if got == nil || !has {
		t.Errorf("%s: result.previous is absent, want %+v", what, *want)
		return
	}
	if got.TrialSet != want.TrialSet || got.Method != want.Method || got.Done != want.Done || got.Ended != want.Ended ||
		got.LessReliable != want.LessReliable || got.MethodsDiffer != want.MethodsDiffer ||
		got.ShareChange < want.ShareChange-1e-6 || got.ShareChange > want.ShareChange+1e-6 {
		t.Errorf("%s: result.previous = %+v, want %+v", what, *got, *want)
	}
}

// ---- tests ----

// US3 Independent Test: a second set with one more trial handed over, under another method, names
// both methods and is less reliable.
func TestTSASecondSetWithAHandedOverTrialUnderAnotherMethodIsLessReliableAndNamesBothMethods(t *testing.T) {
	x := newTRRig(t)
	x.registerSet("first", 1)
	x.registerSet("second", 2)
	a := x.startSetOK("first", 1)
	x.settled(a, "ended")
	planted := strings.Repeat("a", 64)
	x.tlSetMethod(a.Target, planted)
	x.ctl.failTests("second-2")
	b := x.startSetOK("second", 2)
	x.settled(b, "ended")

	first, firstHas := x.tlGet(a.Target)
	second, secondHas := x.tlGet(b.Target)
	if first.Method != planted || second.Method == planted || second.Method == "" {
		t.Fatalf("methods = %q and %q, want the planted one and a different recorded one", first.Method, second.Method)
	}
	if second.Result.Done != 1 || second.Result.Ended != 2 || !second.Result.Unstable {
		t.Fatalf("the second set's result = %+v, want 1 of 2 done and unstable", second.Result)
	}
	want := &tlPrev{TrialSet: a.Target, Method: planted, Done: 1, Ended: 1, ShareChange: -0.5, LessReliable: true, MethodsDiffer: true}
	tlWantPrev(t, "getTrialSet of the second set", second.Result.Previous, secondHas, want)
	tlWantPrev(t, "getTrialSet of the first set", first.Result.Previous, firstHas, nil)

	pg := x.tlList("S-1", nil)
	if got := tlIDs(pg.Items); !slices.Equal(got, []string{b.Target, a.Target}) || pg.Next != nil {
		t.Fatalf("list = %v next %v, want %s then %s on one page", got, pg.Next, b.Target, a.Target)
	}
	tlWantPrev(t, "list item of the second set", pg.Items[0].Result.Previous, pg.Has[0], want)
	tlWantPrev(t, "list item of the first set", pg.Items[1].Result.Previous, pg.Has[1], nil)
	for i, want := range []tlSet{second, first} {
		it := pg.Items[i]
		if it.State != "ended" || it.Trials != want.Trials || it.Method != want.Method || it.Slice != "S-1" ||
			it.Result.Done != want.Result.Done || it.Result.Ended != want.Result.Ended || it.Result.Unstable != want.Result.Unstable {
			t.Errorf("list item %s = state %q trials %d method %q done/ended %d/%d unstable %v, want the set as getTrialSet shows it: %q %d %q %d/%d %v",
				it.ID, it.State, it.Trials, it.Method, it.Result.Done, it.Result.Ended, it.Result.Unstable,
				"ended", want.Trials, want.Method, want.Result.Done, want.Result.Ended, want.Result.Unstable)
		}
		if !reflect.DeepEqual(it, want) {
			t.Errorf("list item %s = %+v, want what getTrialSet answers: %+v", it.ID, it, want)
		}
	}
}

// The same method and an equal share: no less_reliable, no methods_differ; then a fall to none done
// and a rise back to all done, the extremes of share_change.
func TestTSThePreviousSetOfEqualRisingAndFallingShares(t *testing.T) {
	x := newTRRig(t)
	prefixes := []string{"one", "two", "three", "four"}
	for _, p := range prefixes {
		x.registerSet(p, 1)
	}
	x.ctl.failTests("three-1") // the third set's only trial is handed over
	var ids []string
	for _, p := range prefixes {
		op := x.startSetOK(p, 1)
		x.settled(op, "ended")
		ids = append(ids, op.Target)
	}
	m, _ := x.tlGet(ids[0])
	want := []*tlPrev{
		nil, // the first set
		{TrialSet: ids[0], Method: m.Method, Done: 1, Ended: 1, ShareChange: 0},                      // 1/1 against 1/1: equal
		{TrialSet: ids[1], Method: m.Method, Done: 1, Ended: 1, ShareChange: -1, LessReliable: true}, // 0/1 against 1/1
		{TrialSet: ids[2], Method: m.Method, Done: 0, Ended: 1, ShareChange: 1},                      // 1/1 against 0/1
	}
	for i, id := range ids {
		v, has := x.tlGet(id)
		tlWantPrev(t, "getTrialSet "+id, v.Result.Previous, has, want[i])
	}
	pg := x.tlList("S-1", nil)
	for i, it := range pg.Items { // newest first
		tlWantPrev(t, "list item "+it.ID, it.Result.Previous, pg.Has[i], want[len(ids)-1-i])
	}
}

// A cancelled, a failed and an ended set with no ended trial are skipped; a stopped one with an
// ended trial is chosen; the latest earlier one wins; a stopped set with no ended trial has none.
func TestTSThePreviousSetIsTheLatestEarlierEndedOrStoppedOneWithAnEndedTrial(t *testing.T) {
	x := newTRRig(t)
	x.registerSet("first", 1)
	x.registerSet("fifth", 1)
	x.registerSet("seventh", 2)
	one := x.startSetOK("first", 1)
	x.settled(one, "ended")
	base := one.Target // a finished set with its trial's run: its record is the copy's model
	x.tlReserve(4)     // TS-2 to TS-4 are planted
	x.tlPlant(base, "TS-2", "", func(m map[string]any) { m["state"] = "cancelled" })
	x.tlPlant(base, "TS-3", "", func(m map[string]any) { m["state"] = "failed"; m["failed_because"] = "planted" })
	x.tlPlant(base, "TS-4", "", func(m map[string]any) { m["runs"] = []string{} }) // ended, no trial started
	five := x.startSetOK("fifth", 1)
	if five.Target != "TS-5" {
		t.Fatalf("the next real set is %s, want TS-5 after the planted TS-2 to TS-4", five.Target)
	}
	x.settled(five, "ended")
	x.tlReserve(6)
	x.tlPlant(base, "TS-6", "", func(m map[string]any) { m["state"] = "stopped"; m["stopped_because"] = "budget" })
	x.ctl.failTests("seventh-2")
	seven := x.startSetOK("seventh", 2)
	if seven.Target != "TS-7" {
		t.Fatalf("the next real set is %s, want TS-7", seven.Target)
	}
	x.settled(seven, "ended")
	x.tlReserve(8)
	x.tlPlant(base, "TS-8", "", func(m map[string]any) {
		m["state"] = "stopped"
		m["stopped_because"] = "budget"
		m["runs"] = []string{}
	})

	method := func(id string) string { v, _ := x.tlGet(id); return v.Method }
	m := method("TS-1")
	same := func(id string, done, ended int, change float64, less bool) *tlPrev {
		return &tlPrev{TrialSet: id, Method: m, Done: done, Ended: ended, ShareChange: change, LessReliable: less}
	}
	want := map[string]*tlPrev{
		"TS-8": nil,                            // stopped with no ended trial of its own
		"TS-7": same("TS-6", 1, 1, -0.5, true), // the stopped one, over the older real one; 1 of 2 against 1 of 1
		"TS-6": same("TS-5", 1, 1, 0, false),   // a stopped set is compared too
		"TS-5": same("TS-1", 1, 1, 0, false),   // TS-4, TS-3 and TS-2 skipped
		"TS-4": nil,                            // ended, no trial ended
		"TS-3": nil,                            // failed
		"TS-2": nil,                            // cancelled
		"TS-1": nil,                            // the first
	}
	order := []string{"TS-8", "TS-7", "TS-6", "TS-5", "TS-4", "TS-3", "TS-2", "TS-1"}
	pg := x.tlList("S-1", nil)
	if got := tlIDs(pg.Items); !slices.Equal(got, order) {
		t.Fatalf("list = %v, want %v (newest first)", got, order)
	}
	for i, id := range order {
		got, has := x.tlGet(id)
		tlWantPrev(t, "getTrialSet "+id, got.Result.Previous, has, want[id])
		tlWantPrev(t, "list item "+id, pg.Items[i].Result.Previous, pg.Has[i], want[id])
	}
}

// A running set has no `previous` before any trial ended, and gains it when it ends.
func TestTSARunningSetWithNoEndedTrialHasNoPreviousAndGainsItWhenItEnds(t *testing.T) {
	x := newTRRig(t)
	x.registerSet("first", 1)
	x.registerSet("second", 1)
	a := x.startSetOK("first", 1)
	x.settled(a, "ended")
	release := x.ctl.hold("second-1")
	b := x.startSetOK("second", 1)
	x.waitRuns(b.Target, 1)
	v, has := x.tlGet(b.Target)
	if v.State != "running" || v.Result.Ended != 0 {
		t.Fatalf("set %s = state %q ended %d, want running with no ended trial", v.ID, v.State, v.Result.Ended)
	}
	tlWantPrev(t, "getTrialSet of the running set", v.Result.Previous, has, nil)
	pg := x.tlList("S-1", nil)
	if got := tlIDs(pg.Items); !slices.Equal(got, []string{b.Target, a.Target}) {
		t.Fatalf("list = %v, want %s then %s", got, b.Target, a.Target)
	}
	tlWantPrev(t, "list item of the running set", pg.Items[0].Result.Previous, pg.Has[0], nil)
	tlWantPrev(t, "list item of the first set", pg.Items[1].Result.Previous, pg.Has[1], nil)
	release()
	x.settled(b, "ended")
	end, has := x.tlGet(b.Target)
	tlWantPrev(t, "getTrialSet once the set ended", end.Result.Previous, has,
		&tlPrev{TrialSet: a.Target, Method: end.Method, Done: 1, Ended: 1, ShareChange: 0})
}

// listTrialSets: at most 50 per page, a token that reads the rest, no set twice or missed, a token
// of another list refused, an unknown slice NotFound.
func TestTSListPagesAreFiftyAndFollowOpaqueTokensOfTheirOwnSlice(t *testing.T) {
	x := newTRRig(t)
	x.registerSet("first", 1)
	one := x.startSetOK("first", 1)
	x.settled(one, "ended")
	x.sp.seed(t, "another slice") // S-2: it has its own sets and its own tokens
	for n := 2; n <= 52; n++ {
		x.tlPlant(one.Target, fmt.Sprintf("TS-%d", n), "", func(m map[string]any) { m["runs"] = []string{} })
	}
	first := x.tlList("S-1", nil)
	if len(first.Items) != 50 || first.Next == nil || *first.Next == "" {
		t.Fatalf("first page: %d items, next %v; want 50 and a next_page_token", len(first.Items), first.Next)
	}
	if first.Items[0].ID != "TS-52" || first.Items[49].ID != "TS-3" {
		t.Errorf("first page runs %s to %s, want TS-52 to TS-3", first.Items[0].ID, first.Items[49].ID)
	}
	second := x.tlList("S-1", first.Next)
	if got := tlIDs(second.Items); !slices.Equal(got, []string{"TS-2", "TS-1"}) || second.Next != nil {
		t.Fatalf("second page = %v next %v, want TS-2 and TS-1 and no next_page_token", got, second.Next)
	}
	seen := map[string]bool{}
	for _, id := range append(tlIDs(first.Items), tlIDs(second.Items)...) {
		if seen[id] {
			t.Errorf("%s is on both pages", id)
		}
		seen[id] = true
	}
	if len(seen) != 52 {
		t.Errorf("the pages hold %d distinct sets, want 52", len(seen))
	}

	// Exactly 50 sets of the other slice: one page, no token. One more: a token.
	for n := 101; n <= 150; n++ {
		x.tlPlant(one.Target, fmt.Sprintf("TS-%d", n), "S-2", func(m map[string]any) { m["runs"] = []string{} })
	}
	if exact := x.tlList("S-2", nil); len(exact.Items) != 50 || exact.Next != nil {
		t.Fatalf("50 sets: %d items, next %v; want 50 and no next_page_token", len(exact.Items), exact.Next)
	}
	x.tlPlant(one.Target, "TS-151", "S-2", func(m map[string]any) { m["runs"] = []string{} })
	other := x.tlList("S-2", nil)
	if len(other.Items) != 50 || other.Next == nil || other.Items[0].ID != "TS-151" || other.Items[0].Slice != "S-2" {
		t.Fatalf("51 sets: %d items from %v, next %v; want 50 of S-2, newest TS-151, and a token", len(other.Items), tlIDs(other.Items), other.Next)
	}
	if rest := x.tlList("S-2", other.Next); !slices.Equal(tlIDs(rest.Items), []string{"TS-101"}) || rest.Next != nil {
		t.Errorf("S-2's second page = %v next %v, want TS-101 alone", tlIDs(rest.Items), rest.Next)
	}

	// A token is the list's own: another slice's and garbage are 400 InvalidRequest.
	x.tlListReply("S-2", first.Next).problem(t, 400, "InvalidRequest")
	x.tlListReply("S-1", other.Next).problem(t, 400, "InvalidRequest")
	for _, junk := range []string{"garbage", "TS-3", "!!!", strings.Repeat("A", 200)} {
		x.tlListReply("S-1", &junk).problem(t, 400, "InvalidRequest")
	}
	// The slice's own token still reads its page after the refusals.
	if again := x.tlList("S-1", first.Next); len(again.Items) != 2 {
		t.Errorf("the slice's own token after refusals gave %d items, want 2", len(again.Items))
	}
	x.tlListReply("S-99", nil).problem(t, 404, "NotFound")
}

// tlRunEvent is what T053 reads of a trial's run record.
type tlRunEvent struct {
	Kind    string `json:"kind"`
	Step    string `json:"step"`
	Round   int    `json:"round"`
	Attempt int    `json:"attempt"`
}

func (x *trRig) tlEvents(run string) []tlRunEvent {
	x.t.Helper()
	var all []tlRunEvent
	q := ""
	for range 20 {
		p := x.do("GET", apiPath("/runs/"+run+"/events"+q), "")
		if p.Code != 200 {
			x.t.Fatalf("listRunEvents %s = %d: %s", run, p.Code, p.Body.String())
		}
		var pg struct {
			Items []tlRunEvent `json:"items"`
			Next  *string      `json:"next_page_token"`
		}
		if err := json.Unmarshal(p.Body.Bytes(), &pg); err != nil {
			x.t.Fatal(err)
		}
		all = append(all, pg.Items...)
		if pg.Next == nil || len(pg.Items) == 0 { // a run that has not ended keeps a token on its last page
			return all
		}
		q = "&page_token=" + url.QueryEscape(*pg.Next)
	}
	x.t.Fatal("listRunEvents did not end after 20 pages")
	return nil
}

// tlAttempts is a trial's attempts as the data model counts them: the AttemptMade events plus each
// StepRejected with no AttemptMade of the same step, round and attempt.
func tlAttempts(evs []tlRunEvent) int {
	type key struct {
		step           string
		round, attempt int
	}
	made := map[key]bool{}
	n := 0
	for _, e := range evs {
		if e.Kind == "AttemptMade" {
			made[key{e.Step, e.Round, e.Attempt}] = true
			n++
		}
	}
	for _, e := range evs {
		if e.Kind == "StepRejected" && !made[key{e.Step, e.Round, e.Attempt}] {
			n++
		}
	}
	return n
}

// T053: getTrialSet on a running set spreads steps and totals over every started trial, the
// unfinished one with its partial values; `ended` counts the finished ones only; no `previous`.
func TestTSARunningSetSpreadsItsStepsAndTotalsOverEveryStartedTrialAndHasNoPrevious(t *testing.T) {
	x := newTRRig(t)
	x.registerSet("first", 1)
	x.registerSet("second", 2)
	x.settled(x.startSetOK("first", 1), "ended") // an earlier set the running one could be compared with

	// Trial 2 of the running set stops at its review step, after its earlier steps were accepted.
	gate, hit := make(chan struct{}), make(chan struct{}, 1)
	base := x.ctl.work(x.w)
	x.w.set(func(ctx context.Context, r machine.Request) (machine.Result, error) {
		if r.Project == "second-2" && r.Step.Name == "review" {
			select {
			case hit <- struct{}{}:
			default:
			}
			select {
			case <-gate:
			case <-ctx.Done():
				return machine.Result{}, ctx.Err()
			}
		}
		return base(ctx, r)
	})
	opened := false
	open := func() {
		if !opened {
			opened = true
			close(gate)
		}
	}
	t.Cleanup(open)
	op := x.startSetOK("second", 2)
	select {
	case <-hit:
	case <-time.After(30 * time.Second):
		t.Fatal("trial 2 never reached its review step")
	}
	v := x.waitRuns(op.Target, 2)
	got, has := x.tlGet(op.Target)
	if got.State != "running" || len(v.Runs) != 2 {
		t.Fatalf("set %s is %q with %d runs, want running with 2", got.ID, got.State, len(v.Runs))
	}
	if got.Result.Ended != 1 || got.Result.Done != 1 {
		t.Errorf("ended/done = %d/%d, want 1/1: only the finished trial counts", got.Result.Ended, got.Result.Done)
	}
	if len(got.Result.Outcomes) != 2 || got.Result.Outcomes[0].Outcome == nil || *got.Result.Outcomes[0].Outcome != "done" || got.Result.Outcomes[1].Outcome != nil {
		t.Errorf("outcomes = %+v, want trial 1 done and trial 2 with none yet", got.Result.Outcomes)
	}
	tlWantPrev(t, "getTrialSet of the running set", got.Result.Previous, has, nil)
	if pg := x.tlList("S-1", nil); pg.Items[0].ID != op.Target || pg.Items[0].Result.Previous != nil || pg.Has[0] {
		t.Errorf("list item of the running set = %s with previous %+v (key %v), want none", pg.Items[0].ID, pg.Items[0].Result.Previous, pg.Has[0])
	}

	// What each trial's record holds now, and so what the spreads must show.
	evs := [2][]tlRunEvent{x.tlEvents(v.Runs[0]), x.tlEvents(v.Runs[1])}
	a1, a2 := tlAttempts(evs[0]), tlAttempts(evs[1])
	if a2 >= a1 || a2 == 0 {
		t.Fatalf("attempts so far: trial 1 %d, trial 2 %d; want trial 2 to have some but fewer (it waits at review)", a1, a2)
	}
	tot := got.Result.Totals["attempts"]
	if tot == nil {
		t.Fatalf("totals.attempts is absent; totals = %+v, want a spread over both trials", got.Result.Totals)
	}
	if want := (tlSpread{float64(a2), float64(a1+a2) / 2, float64(a1)}); *tot != want {
		t.Errorf("totals.attempts = %+v, want %+v (trial 2's partial %d included)", *tot, want, a2)
	}
	if len(got.Result.Steps) == 0 {
		t.Fatal("the running set shows no steps")
	}
	for _, st := range got.Result.Steps {
		want := 0
		for _, es := range evs {
			if slices.ContainsFunc(es, func(e tlRunEvent) bool { return e.Step == st.Step }) {
				want++
			}
		}
		if st.Reached != want {
			t.Errorf("step %s reached = %d, want %d (trials whose record has the step, the unfinished one included)", st.Step, st.Reached, want)
		}
	}
	for _, st := range got.Result.Steps {
		if st.Step == "tests" && st.Reached != 2 {
			t.Errorf("step tests reached = %d, want 2: trial 2 accepted it before waiting at review", st.Reached)
		}
	}

	// Once the trial goes on and the set ends, the comparison appears.
	open()
	x.settled(op, "ended")
	end, has := x.tlGet(op.Target)
	tlWantPrev(t, "getTrialSet of the ended set", end.Result.Previous, has,
		&tlPrev{TrialSet: "TS-1", Method: end.Method, Done: 1, Ended: 1, ShareChange: 0})
}
