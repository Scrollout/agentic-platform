package api

import (
	"net/http"
	"testing"
	"time"
)

// T039: the contract's security on the three trial-set operations. startTrialSet takes the client
// token only; getTrialSet and listTrialSets take the client token or the page token. A step
// worker's token is refused on all three, and a missing token is Unauthorized (Principle V).

func TestTSTokensReadsRefuseAStepWorkerAndNoToken(t *testing.T) {
	x := newTRRig(t)
	x.registerSet("trial", 1)
	op := x.startSetOK("trial", 1)
	x.settled(op, "ended")
	for _, step := range []string{"generate", "tests"} {
		for name, path := range map[string]string{
			"getTrialSet":   apiPath("/trial-sets/" + op.Target),
			"listTrialSets": spURL("S-1", "/trial-sets"),
		} {
			x.r.clock.Advance(80 * time.Millisecond)
			if p := x.r.do("GET", path, stepToken("S-1", step), "", nil); p.Code != http.StatusForbidden {
				t.Errorf("%s with the %s worker's token = %d, want 403: %s", name, step, p.Code, p.Body.String())
			}
		}
	}
	for name, path := range map[string]string{
		"getTrialSet":   apiPath("/trial-sets/" + op.Target),
		"listTrialSets": spURL("S-1", "/trial-sets"),
	} {
		x.r.clock.Advance(80 * time.Millisecond)
		if p := x.r.do("GET", path, "", "", nil); p.Code != http.StatusUnauthorized {
			t.Errorf("%s without a token = %d, want 401: %s", name, p.Code, p.Body.String())
		}
	}
}

func TestTSTokensReadsAllowThePageAndClientToken(t *testing.T) {
	x := newTRRig(t)
	x.registerSet("trial", 1)
	op := x.startSetOK("trial", 1)
	x.settled(op, "ended")
	for tokName, tok := range map[string]string{"page": pageToken, "client": clientToken} {
		for name, path := range map[string]string{
			"getTrialSet":   apiPath("/trial-sets/" + op.Target),
			"listTrialSets": spURL("S-1", "/trial-sets"),
		} {
			x.r.clock.Advance(80 * time.Millisecond)
			if p := x.r.do("GET", path, tok, "", nil); p.Code != http.StatusOK {
				t.Errorf("%s with the %s token = %d, want 200: %s", name, tokName, p.Code, p.Body.String())
			}
		}
	}
}

// A refused token for an unknown set or slice is still refused, not NotFound: the caller's right
// to ask comes before whether the thing exists.
func TestTSTokensRefusalComesBeforeNotFound(t *testing.T) {
	x := newTRRig(t)
	for name, path := range map[string]string{
		"getTrialSet unknown set":     apiPath("/trial-sets/TS-99"),
		"listTrialSets unknown slice": spURL("S-99", "/trial-sets"),
	} {
		x.r.clock.Advance(80 * time.Millisecond)
		if p := x.r.do("GET", path, stepToken("S-1", "generate"), "", nil); p.Code != http.StatusForbidden {
			t.Errorf("%s with a worker token = %d, want 403: %s", name, p.Code, p.Body.String())
		}
		x.r.clock.Advance(80 * time.Millisecond)
		if p := x.r.do("GET", path, "", "", nil); p.Code != http.StatusUnauthorized {
			t.Errorf("%s without a token = %d, want 401: %s", name, p.Code, p.Body.String())
		}
	}
}

func TestTSTokensStartRefusesThePageTokenAndRecordsNothing(t *testing.T) {
	x := newTRRig(t)
	x.registerSet("trial", 2)
	p := x.r.do("POST", spURL("S-1", "/trial-sets"), pageToken, trBody("trial", 2), nil)
	if p.Code != http.StatusForbidden {
		t.Errorf("startTrialSet with the page token = %d, want 403: %s", p.Code, p.Body.String())
	}
	x.nothingRecorded("a start with the page token", 0)
	// The client token still starts it: the refusal left the slice free.
	x.settled(x.startSetOK("trial", 2), "ended")
}
