//go:build docker

package lab

import (
	"encoding/json"
	"testing"
)

// Question is a slice's question as the engine returns it.
type Question struct {
	ID      string          `json:"id"`
	About   string          `json:"about"`
	Text    string          `json:"text"`
	Options []string        `json:"options"`
	Answer  json.RawMessage `json:"answer"`
}

// Doc is a slice as the engine returns it, as far as the tests read it.
type Doc struct {
	ID          string            `json:"id"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	State       string            `json:"state"`
	LatestRun   string            `json:"latest_run"`
	Goals       []map[string]any  `json:"goals"`
	Terms       []map[string]any  `json:"terms"`
	Rules       []map[string]any  `json:"rules"`
	RuleText    map[string]string `json:"rule_text"`
	Examples    string            `json:"examples"`
	Contract    string            `json:"contract"`
	Questions   []Question        `json:"questions"`
	Parts       map[string]string `json:"parts"`
	Approvals   []struct {
		Part        string `json:"part"`
		ContentHash string `json:"content_hash"`
	} `json:"approvals"`
	Checks  []Check `json:"checks"`
	History []struct {
		Kind        string   `json:"kind"`
		Model       string   `json:"model"`
		AgentDigest string   `json:"agent_digest"`
		Refs        []string `json:"refs"`
	} `json:"history"`
}

// Get reads a slice.
func (l *Lab) Get(t testing.TB, id string) Doc {
	t.Helper()
	st, _, b := l.Do("GET", "/slices/"+id, ClientToken, nil, nil)
	if st != 200 {
		t.Fatalf("getSlice %s = %d: %s", id, st, b)
	}
	var d Doc
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatalf("getSlice %s: %v: %s", id, err, b)
	}
	return d
}

// Approved lists the parts whose approval holds at the part's current hash.
func (d Doc) Approved() map[string]bool {
	m := map[string]bool{}
	for _, a := range d.Approvals {
		if d.Parts[a.Part] == a.ContentHash {
			m[a.Part] = true
		}
	}
	return m
}

// Failed lists the failed checks.
func (d Doc) Failed() []Check {
	var f []Check
	for _, c := range d.Checks {
		if !c.Passed {
			f = append(f, c)
		}
	}
	return f
}

// Replace sends a replaceGoals, replaceRules or replaceContract request (kind "goals", "rules" or
// "contract") and waits for it to succeed.
func (l *Lab) Replace(t testing.TB, id, kind string, body any) {
	t.Helper()
	if op := l.Wait(t, l.Accept(t, "PUT", "/slices/"+id+"/"+kind, body)); op.Status != "succeeded" {
		t.Fatalf("replace %s of %s: %+v (error %+v)", kind, id, op, op.Error)
	}
}

// Approve approves a part at its current hash and waits for it to succeed.
func (l *Lab) Approve(t testing.TB, id, part string) {
	t.Helper()
	d := l.Get(t, id)
	body := map[string]string{"part": part, "content_hash": d.Parts[part]}
	if op := l.Wait(t, l.Accept(t, "POST", "/slices/"+id+"/approvals", body)); op.Status != "succeeded" {
		t.Fatalf("approve %s of %s: %+v (error %+v)", part, id, op, op.Error)
	}
}

// GoalsBody, RulesBody and ContractBody are the replace requests that put a slice's parts back.
func (d Doc) GoalsBody() any { return map[string]any{"goals": d.Goals} }
func (d Doc) RulesBody() any {
	return map[string]any{"rules": d.Rules, "examples": d.Examples, "terms": d.Terms}
}
func (d Doc) ContractBody() any { return map[string]any{"contract": d.Contract} }
