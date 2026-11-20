//go:build docker

// T024 (feature 002, US2, SC-002, FR-009, FR-011, FR-016; data model "Operation record"): an
// operation survives a stop of the engine. One lab (CI's `docker` job, real Temporal, PostgreSQL
// and the engine container; this file is never run on the developer's host) shows, across one kill
// and restart of the engine (l.RestartEngine):
//
//  1. An operation accepted just before the kill, its work held in flight, is followed after the
//     restart by the same ID, through getOperation, to exactly one end, and its work is done once:
//     the slice list holds one slice for it (the interrupted analyst call may repeat, so the model
//     may be asked twice, but no second slice and no second result).
//  2. IDs given out before and after the restart are all distinct (SC-002).
//  3. An operation that ended before the restart is still answered, with the same end, from its
//     record.
//  4. A resend after the restart with the same Idempotency-Key and body returns the same operation
//     ID; the same key with another body is 409 IdempotencyKeyReused (the key answer survives
//     because the request created an operation, contract info text).
//  5. No row of the lab's `durable-store` (both databases, text and decoded byte columns) holds the
//     distinctive text of a request body (FR-016). The check reports what it examined, finds a
//     planted copy of the marker, and requires the dump to hold the held operation's ID (so it
//     did read workflow rows).
//
// How the work is held, with no sleep as the cue: the lab's replay server (the analyst's model) is
// paused before the held operations are accepted, so the analyst's model call hangs; the operation
// is then read as not ended and the engine is killed. The replay server is resumed after the
// restart so the continued work can finish. The held operations are createSlice for the recorded
// description (lab.Example, which the replay server answers, so it can succeed) and createSlice for
// descriptions no recording answers carrying the marker (the model call gets 404, so each ends
// failed); the marker rides only on those, as a recorded description cannot carry it.
//
// Assumed beyond the brief: the engine answers 503 Unavailable (or no answer) while it starts, so
// reads after the restart tolerate both; operation IDs are strings; a first answer under a key is
// 202 (a repeat of it may also be 200); GET /slices lists the slices (one page, at most 50, is
// enough here).
//
// Time bounds: the lab starts once (no trials); every wait is at most 4 minutes (the ended-before
// operation 3 minutes, as lab.Wait). The expected run after the lab is started is about a minute.
//
// Edge cases checked: the operation held when the engine dies is neither lost nor repeated; an
// operation that ended before the restart answers the same from its record; a client that never
// saw the 202 resends with its key and gets the operation, not a second one; an unmatched
// request under a key is refused, not merged. Left untested: a kill between the record and the
// 202 (it cannot be made exact from outside; the keyed resend covers what the client sees), a
// cancellation across a restart (T022/T023 and the API tests), and retention expiry.
package acceptance

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/agentic-platform/tests/lab"
)

const orMarker = "restart-marker-7f3a9c41d2e86b05"

const orLimit = 4 * time.Minute

// orDescription is a description no recording answers, carrying the marker and a number.
func orDescription(n int) string {
	return fmt.Sprintf("A weather widget that shows tomorrow's temperature for a city the user picks. %s-%d", orMarker, n)
}

func orEnded(s string) bool { return s == "succeeded" || s == "failed" || s == "cancelled" }

// orGet reads an operation; st is the HTTP status (0 when the call failed).
func orGet(l *lab.Lab, id string) (st int, op lab.Operation, raw []byte) {
	st, _, raw = l.Do("GET", "/operations/"+url.PathEscape(id), lab.ClientToken, nil, nil)
	if st == 200 {
		if json.Unmarshal(raw, &op) != nil {
			st = -1
		}
	}
	return
}

// orAwait polls an operation to its end, tolerating 503 and no answer while the engine starts.
func orAwait(t *testing.T, l *lab.Lab, id string) (lab.Operation, []byte) {
	t.Helper()
	deadline := time.Now().Add(orLimit)
	last := ""
	for time.Now().Before(deadline) {
		st, op, raw := orGet(l, id)
		switch {
		case st == 200 && orEnded(op.Status):
			return op, raw
		case st == 200 && (op.Status == "accepted" || op.Status == "running"):
			last = op.Status
		case st == 503 || st <= 0:
			last = fmt.Sprintf("GET = %d", st)
		default:
			t.Fatalf("getOperation %s = %d, status %q: %s (the operation must be followed to its end by the same ID)", id, st, op.Status, raw)
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("operation %s did not end within %s after the restart; last: %s", id, orLimit, last)
	return lab.Operation{}, nil
}

// orAccept sends a createSlice, with a key when key is not "", and returns the operation ID.
func orAccept(t *testing.T, l *lab.Lab, desc, key string) string {
	t.Helper()
	var extra []string
	if key != "" {
		extra = []string{"Idempotency-Key: " + key}
	}
	st, _, b := l.Do("POST", "/slices", lab.ClientToken, map[string]string{"description": desc}, extra)
	var op lab.Operation
	if st != 202 || json.Unmarshal(b, &op) != nil || op.ID == "" {
		t.Fatalf("createSlice = %d, want 202 with an operation: %s", st, b)
	}
	return op.ID
}

// orReplay is the lab's replay server container (the analyst's model), named
// aptest-lab-replay-<pid>-<n> by the lab.
func orReplay(t *testing.T) string {
	t.Helper()
	out, code := lab.Run("docker", "ps", "--filter", fmt.Sprintf("name=aptest-lab-replay-%d-", os.Getpid()), "--format", "{{.Names}}")
	names := strings.Fields(out)
	if code != 0 || len(names) != 1 {
		t.Fatalf("the lab has %d replay server container(s) (exit %d): %s", len(names), code, out)
	}
	return names[0]
}

func TestOperationsSurviveRestart(t *testing.T) {
	l := lab.Start(t)
	replay := orReplay(t)

	// An operation that ends before the restart (its model call gets 404 and it fails, quickly).
	ended := orAccept(t, l, orDescription(0), "")
	before := l.Wait(t, ended)
	if !orEnded(before.Status) {
		t.Fatalf("the first operation did not end: %+v", before)
	}

	// Hold the analyst's model answer, then accept the operations whose work is held. The keyed
	// one is the held createSlice for the recorded description.
	lab.Docker(t, "pause", replay)
	resumed := false
	resume := func() {
		if !resumed {
			resumed = true
			lab.Run("docker", "unpause", replay)
		}
	}
	t.Cleanup(resume)

	const key = "restart-key-T024"
	held := orAccept(t, l, lab.Example, key)
	m1 := orAccept(t, l, orDescription(1), "")
	m2 := orAccept(t, l, orDescription(2), "")
	ids := map[string]string{ended: "ended before", held: "held (keyed)", m1: "held marker 1", m2: "held marker 2"}
	if len(ids) != 4 {
		t.Fatalf("IDs given out before the restart are not distinct: %v", ids)
	}
	for _, id := range []string{held, m1, m2} {
		if st, op, raw := orGet(l, id); st != 200 || orEnded(op.Status) {
			t.Fatalf("operation %s before the kill: GET = %d, %s; its work must be in flight (model held), not ended", id, st, raw)
		}
	}

	l.RestartEngine(t) // kills the engine's container and starts it again
	resume()

	// 2. IDs after the restart do not repeat one given before (SC-002).
	var after []string
	for i := 3; i <= 4; i++ {
		id := orAccept(t, l, orDescription(i), "")
		if why, dup := ids[id]; dup {
			t.Errorf("operation ID %s given after the restart repeats one given before (%s): SC-002", id, why)
		}
		ids[id] = fmt.Sprintf("after the restart (%d)", i)
		after = append(after, id)
	}
	if len(ids) != 6 {
		t.Errorf("6 operations were accepted, %d distinct IDs: %v", len(ids), ids)
	}

	// 3. The operation that ended before the restart answers the same, from its record.
	if st, op, raw := orGet(l, ended); st != 200 || op.ID != ended || op.Status != before.Status || op.Kind != before.Kind {
		t.Errorf("operation %s ended %q before the restart and reads %d %s after it", ended, before.Status, st, raw)
	}

	// 4. The keyed request, resent as a client that never saw the 202 would.
	st, _, b := l.Do("POST", "/slices", lab.ClientToken, map[string]string{"description": lab.Example}, []string{"Idempotency-Key: " + key})
	var again lab.Operation
	if (st != 202 && st != 200) || json.Unmarshal(b, &again) != nil || again.ID != held {
		t.Errorf("the same Idempotency-Key and body after the restart = %d %s, want the operation %s again", st, b, held)
	}
	if st, ct, b := l.Do("POST", "/slices", lab.ClientToken, map[string]string{"description": orDescription(9)}, []string{"Idempotency-Key: " + key}); st != 409 ||
		!strings.HasPrefix(ct, "application/problem+json") || !strings.Contains(string(b), "IdempotencyKeyReused") {
		t.Errorf("the same Idempotency-Key with another body after the restart = %d %s %s, want 409 IdempotencyKeyReused", st, ct, b)
	}

	// 1. Exactly one end for each operation accepted before the kill, the same ID throughout.
	hop, _ := orAwait(t, l, held)
	if hop.ID != held || hop.Kind == "" {
		t.Errorf("operation %s reads as %+v after the restart", held, hop)
	}
	if hop.Status == "succeeded" && hop.Result.Slice == "" {
		t.Errorf("operation %s succeeded with no slice: %+v", held, hop)
	}
	for _, id := range []string{m1, m2} {
		op, _ := orAwait(t, l, id)
		if op.ID != id || (op.Status == "succeeded" && op.Result.Slice != "") {
			t.Errorf("operation %s (its model call is 404): %+v, want one end and no slice", id, op)
		}
	}
	for _, id := range after {
		orAwait(t, l, id)
	}
	// An end stays: read again, same status and result.
	if _, op2, _ := orGet(l, held); op2.Status != hop.Status || op2.Result != hop.Result {
		t.Errorf("operation %s read %+v, then %+v: it has more than one end", held, hop, op2)
	}

	// The work was done once: one slice for the held operation, and none for the others.
	var list struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	get(t, l, "/slices", &list)
	want := 0
	if hop.Status == "succeeded" {
		want = 1
	}
	if len(list.Items) != want {
		t.Errorf("the slice list holds %d slice(s) after the restart, want %d (one for the held operation if it succeeded, none for the failed ones): %+v", len(list.Items), want, list.Items)
	}
	if want == 1 && len(list.Items) == 1 && list.Items[0].ID != hop.Result.Slice {
		t.Errorf("the one slice is %s, the operation %s created %s", list.Items[0].ID, held, hop.Result.Slice)
	}

	// 5. FR-016: no row of durable-store holds the request bodies' text.
	orStoreHoldsNoMarker(t, l, held)
}

// orStoreHoldsNoMarker dumps both databases of the lab's durable-store (text and decoded bytea
// columns, as spStoreHoldsNoText does) and requires that the marker is in none of it. It requires
// the dump to hold the operation ID (so it read workflow rows) and to find a planted marker.
func orStoreHoldsNoMarker(t *testing.T, l *lab.Lab, opID string) {
	t.Helper()
	_, _, store := spDurable(t, l)
	var blob []byte
	for _, db := range []string{"temporal", "temporal_visibility"} {
		out, code := lab.Run("docker", "exec", store, "pg_dump", "-U", "temporal", "-d", db, "--data-only")
		if code != 0 {
			t.Fatalf("pg_dump of %s in %s: exit %d: %.300s", db, store, code, out)
		}
		blob = append(blob, out...)
		blob = append(blob, '\n')
		for _, m := range spHex.FindAllSubmatch([]byte(out), -1) {
			if b, err := hex.DecodeString(string(m[1])); err == nil {
				blob = append(blob, b...)
				blob = append(blob, '\n')
			}
		}
	}
	t.Logf("durable-store: %d bytes examined for the marker %q", len(blob), orMarker)
	if !strings.Contains(string(blob), opID) {
		t.Errorf("the dump of durable-store does not hold the operation ID %s, so it did not examine the workflow's rows", opID)
	}
	if len(spFound(append([]byte("planted: "+orMarker+"\n"), blob...), []string{orMarker})) == 0 {
		t.Fatalf("the search does not find a planted marker")
	}
	if len(spFound(blob, []string{orMarker, lab.Example})) > 0 {
		t.Errorf("durable-store holds request text (the marker or the description), FR-016")
	}
}
