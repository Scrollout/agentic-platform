package analyst

// T039b: the non-HTTP case's grader, shown to pass one proposal and fail planted ones, with no model
// and no Docker. The last test needs the hooks named in nonhttp.go and fails until they exist.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func nonHTTPFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "nonhttp", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestNonHTTPGraderPassesTheGoodProposal(t *testing.T) {
	g := newGrader(t)
	if r := g.NonHTTP(nonHTTPFixture(t, "good-rename-tool")); !r.Passed {
		t.Fatalf("the good proposal fails: %s", r.Reason)
	}
	// The good proposal is otherwise valid; only the pattern does not fit.
	for _, r := range g.Grade(nonHTTPFixture(t, "good-rename-tool")) {
		if r.Grader == GFit {
			if r.Passed {
				t.Error("a contract with no HTTP operation fits the pattern")
			}
		} else if !r.Passed {
			t.Errorf("%s fails the good proposal: %s", r.Grader, r.Reason)
		}
	}
}

func TestNonHTTPGraderFailsPlantedProposals(t *testing.T) {
	g := newGrader(t)
	for name, want := range map[string]string{
		"bad-filler":             "filler",
		"bad-has-http-operation": "HTTP operations",
		"bad-no-use-case":        "never",
	} {
		r := g.NonHTTP(nonHTTPFixture(t, name))
		if r.Passed {
			t.Errorf("%s passes the non-HTTP grader", name)
		} else if !strings.Contains(r.Reason, want) {
			t.Errorf("%s fails for %q, want a reason with %q", name, r.Reason, want)
		}
	}
	if g.NonHTTP([]byte("not json")).Passed {
		t.Error("non-JSON output passes")
	}
}

func TestNonHTTPCaseIsListedAndGraded(t *testing.T) {
	var found bool
	for _, c := range Cases() {
		if c.Name == CaseNonHTTP {
			found = true
			if c.Description != NonHTTPDescription || c.Adversarial {
				t.Errorf("case = %+v", c)
			}
		}
	}
	if !found {
		t.Fatalf("Cases() has no %q case", CaseNonHTTP)
	}
	g := newGrader(t)
	var has bool
	for _, r := range GradeCase(g, CaseNonHTTP, nonHTTPFixture(t, "bad-filler")) {
		if r.Grader == GFit {
			t.Error("the case is graded for fitting the pattern, which it is expected not to")
		}
		if r.Grader == GNonHTTP {
			has = true
			if r.Passed {
				t.Error("the planted filler proposal passes")
			}
		}
	}
	if !has {
		t.Error("GradeCase does not apply the non-HTTP grader to the case")
	}
}
