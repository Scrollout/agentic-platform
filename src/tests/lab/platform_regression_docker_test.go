//go:build docker

// T054i regression (research D34, the fourth cause of the first `ap test planted` failure): since
// T054c and T053e the engine refuses to start without AP_CHECK_URL, AP_CHECK_TOKEN, AP_PROJECT_URL
// and AP_PROJECT_TOKEN (src/engine/cmd/engine/setup.go), and `lab.Start(t)` passed none, so no lab
// could start its engine: not the contract suites (T030), not Story 1's acceptance (T031). This file
// uses only what lab.go has today, so it compiles and fails on the current lab.
package lab

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

// TestStartGivesEngineCheckAndProjectAddresses: `lab.Start(t)` (no option, as the existing suites
// call it) starts again, and its engine has the check service's and the project worker's address
// and token and a grants folder, whether or not anything listens there.
func TestStartGivesEngineCheckAndProjectAddresses(t *testing.T) {
	l := Start(t)
	var env []string
	if err := json.Unmarshal([]byte(Docker(t, "inspect", "-f", "{{json .Config.Env}}", l.Engine)), &env); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, e := range env {
		k, v, _ := strings.Cut(e, "=")
		got[k] = v
	}
	for _, name := range []string{"AP_CHECK_URL", "AP_PROJECT_URL"} {
		if u, err := url.Parse(got[name]); err != nil || u.Scheme != "http" || u.Host == "" {
			t.Errorf("the lab's engine has %s=%q, want an http URL with a host", name, got[name])
		}
	}
	for _, name := range []string{"AP_CHECK_TOKEN", "AP_PROJECT_TOKEN", "ENGINE_GRANTS_DIR"} {
		if got[name] == "" {
			t.Errorf("the lab's engine has no %s", name)
		}
	}
	// The engine answers (Start waited for it); the operations list is open to the client token.
	if st, _, b := l.Do("GET", "/providers/Agentic.Engine/operations", ClientToken, nil, nil); st != 200 {
		t.Errorf("the lab's engine answered %d: %s", st, b)
	}
}
