// Package malformedrequests_test sends the malformed-request grader's requests (README "Malformed
// requests") to POST /notes, each to a fresh service, and answers the grader's questions about
// each answer as passing or failing top-level tests. The grader adds this file and the one that
// declares requestBodies, wantText and the tests (each calling check) to a scratch copy of the
// project; the project itself is never written.
package malformedrequests_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"example.com/service/gen"
	"example.com/service/internal/app"
)

// answer is what the service answered to one request: status 0 when it gave no HTTP answer.
type answer struct {
	status int
	body   []byte
}

var (
	once    sync.Map // request index -> *sync.Once
	answers sync.Map // request index -> answer
)

// answerTo sends request i once, to a fresh service, and returns the answer.
func answerTo(i int) answer {
	o, _ := once.LoadOrStore(i, &sync.Once{})
	o.(*sync.Once).Do(func() {
		s := httptest.NewServer(gen.Handler(app.New()))
		defer s.Close()
		var a answer
		req, err := http.NewRequest(http.MethodPost, s.URL+"/notes", strings.NewReader(requestBodies[i]))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
			resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
			if err == nil {
				b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
				_ = resp.Body.Close()
				if err == nil {
					a = answer{status: resp.StatusCode, body: b}
				}
			}
		}
		answers.Store(i, a)
	})
	a, _ := answers.Load(i)
	return a.(answer)
}

// check passes when the answer to request i has the property q: "answered" (an HTTP answer came),
// "bit<n>" (bit n of the status is set), "object" (the body is a JSON object), "text" (the body's
// member text is the string wantText[i]).
func check(t *testing.T, i int, q string) {
	t.Helper()
	a := answerTo(i)
	ok := false
	switch {
	case q == "answered":
		ok = a.status != 0
	case strings.HasPrefix(q, "bit"):
		n, err := strconv.Atoi(strings.TrimPrefix(q, "bit"))
		ok = err == nil && a.status&(1<<n) != 0
	case q == "object":
		var m map[string]json.RawMessage
		ok = json.Unmarshal(a.body, &m) == nil && m != nil
	case q == "text":
		var m struct {
			Text *string `json:"text"`
		}
		ok = json.Unmarshal(a.body, &m) == nil && m.Text != nil && *m.Text == wantText[i]
	}
	if !ok {
		t.Fail()
	}
}
