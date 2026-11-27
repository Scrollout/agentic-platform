package api

// T034 R2-1 of feature 012 (US1, review round 2; data model "States" rules 1-4; ADR 0106; engine
// contract `TrialSet`, whose `allOf` requires `failed_because`, a string, exactly with state `failed`):
// a set whose operation ends `failed` with an EMPTY problem detail is still answered `failed` WITH a
// non-empty `failed_because` (the problem's code, `OperationFailed`, when the detail is empty), and
// every answer validates against the contract's `TrialSet` schema. A non-empty detail is still copied
// as it is. Written by a test author separate from the code author; the existing rigs and helpers of
// trial_sets_failed_test.go and operations_durable_test.go are used unchanged.
//
// Edge cases checked: the detail empty through Operator.End (the end the failTrialSet hook handles
// at once); the problem with no `detail` field at all and with a whitespace-only detail, planted as the
// crash window leaves them and repaired by the next read (getTrialSet and listTrialSets); a blank
// reason is not accepted (non-blank after trimming); the schema check on getTrialSet and on every
// listTrialSets item, and again after a later read (repair once, the answer is the same); a non-empty
// detail copied exactly, with its surrounding and inner spaces; no `stopped_because` with it; the
// client's line: the check is on the raw JSON body the CLI and SDKs parse, not on a decoded struct.
// A job's non-retryable WorkError with an empty Detail cannot be planted through the rigs (their work
// functions give refusals with a detail), so it is covered as the operation record it writes: Do's
// newProblem(0, code, "") is the same record Operator.End writes with an empty reason.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// trfEndRaw writes the operation's failed end with the raw problem JSON given, as the Operation
// workflow would, leaving the set `running` (the crash window).
func trfEndRaw(t *testing.T, x *dopRig, id, problem string) {
	t.Helper()
	if ended, err := x.st.EndOperation(id, time.Now().UTC(), "failed", nil, json.RawMessage(problem)); err != nil || !ended {
		t.Fatalf("planting the failed end of %s: ended %v, %v", id, ended, err)
	}
}

// trfWant checks a decoded set as the contract shows a failed one; want "" accepts any non-blank reason.
func trfWant(t *testing.T, m map[string]any, want string) {
	t.Helper()
	if m["state"] != "failed" {
		t.Fatalf("set %v state = %v, want failed", m["id"], m["state"])
	}
	got, has := m["failed_because"].(string)
	if !has || strings.TrimSpace(got) == "" {
		t.Errorf("set %v: failed_because = %q (present %v), want a non-blank reason", m["id"], got, has)
	}
	if want != "" && got != want {
		t.Errorf("set %v: failed_because = %q, want %q", m["id"], got, want)
	}
	if _, has := m["stopped_because"]; has {
		t.Errorf("set %v is failed and has stopped_because", m["id"])
	}
	if m["ended_at"] == nil {
		t.Errorf("set %v is failed and has no ended_at", m["id"])
	}
}

// trfReadBoth reads the set through getTrialSet and listTrialSets, checking both against the schema.
func trfReadBoth(t *testing.T, x *dopRig, id, want string) {
	t.Helper()
	p := x.get("/trial-sets/" + id + "?api-version=" + apiVersion)
	m := tfMap(t, p, "getTrialSet")
	runSchemaCheck(t, "TrialSet", p.Body.Bytes())
	trfWant(t, m, want)
	page := tfMap(t, x.get(spURL("S-1", "/trial-sets")), "listTrialSets")
	items, _ := page["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("listTrialSets = %v, want the one set", page)
	}
	item, _ := items[0].(map[string]any)
	raw, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	runSchemaCheck(t, "TrialSet", raw)
	trfWant(t, item, want)
}

func TestTSFRAnEmptyDetailOnTheFailedEndStillGivesTheSetAReason(t *testing.T) { // T034 R2-1: Operator.End with ""
	x, op := tfStartedSet(t)
	x.tfEnd(op, "")
	tfWaitRawState(t, x.st, op.Target, "failed")
	trfReadBoth(t, x, op.Target, "OperationFailed")
	// Once: a later read answers the same.
	x.r.clock.Advance(time.Hour)
	trfReadBoth(t, x, op.Target, "OperationFailed")
}

func TestTSFRAnEmptyOrMissingOrBlankDetailLeftByACrashIsRepairedWithAReason(t *testing.T) { // T034 R2-1: repair path on read
	problems := map[string]string{
		"empty detail":      `{"type":"about:blank","title":"The operation failed","code":"OperationFailed","detail":""}`,
		"no detail field":   `{"type":"about:blank","title":"The operation failed","code":"OperationFailed"}`,
		"whitespace detail": `{"type":"about:blank","title":"The operation failed","code":"OperationFailed","detail":"   \t "}`,
	}
	for name, prob := range problems {
		t.Run(name, func(t *testing.T) {
			x, op := tfStartedSet(t)
			trfEndRaw(t, x, op.ID, prob)
			if s := tfRawState(t, x.st, op.Target); s != "running" {
				t.Fatalf("planted crash window has the set %q, want running", s)
			}
			trfReadBoth(t, x, op.Target, "")
		})
	}
}

func TestTSFRANonEmptyDetailIsStillCopiedAsItIs(t *testing.T) { // T034 R2-1: no regression
	const reason = "  failed after 4 attempts:  the durable service was down "
	x, op := tfStartedSet(t)
	x.tfEnd(op, reason)
	tfWaitRawState(t, x.st, op.Target, "failed")
	trfReadBoth(t, x, op.Target, reason)
}
