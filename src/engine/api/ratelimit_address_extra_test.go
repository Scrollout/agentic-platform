package api

// Tests for T018b: the per-address rate-limit cases T018a left open. Written from
// the engine contract (info.description, x-limits, the 401 and 429 responses) and
// the task, not from the implementation. The limits are read from the contract.
//
//   - concurrent requests from one address respect the limit (run under -race),
//   - a non-integer requests-per-second-per-address-without-token is refused at start,
//   - a request refused with 429 is refused before its body is read and before an
//     Idempotency-Key is recorded.

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// burst sends the requests, one goroutine each, released together, and returns
// the status codes in the order of the requests.
func burst(r *rig, reqs []func() int) []int {
	codes := make([]int, len(reqs))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, send := range reqs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			codes[i] = send()
		}()
	}
	close(start)
	wg.Wait()
	return codes
}

func tally(codes []int) map[int]int {
	m := map[int]int{}
	for _, c := range codes {
		m[c]++
	}
	return m
}

func TestConcurrentRequestsFromOneAddressRespectTheLimit(t *testing.T) {
	n := contractLimit(t, addrLimitKey)
	for round := 0; round < 25; round++ {
		r := newRig(t, nil) // one fixed clock: every request is in one window
		total := 4 * n
		reqs := make([]func() int, total)
		for i := range reqs {
			reqs[i] = func() int { return r.doFrom("192.0.2.20:"+strconv.Itoa(1000+i), "GET", slicesURL(""), "", nil).Code }
		}
		got := tally(burst(r, reqs))
		if got[401] != n || got[429] != total-n || len(got) != 2 {
			t.Fatalf("round %d: %d requests from one address: 401s=%d 429s=%d (all: %v), want exactly %d and %d",
				round, total, got[401], got[429], got, n, total-n)
		}
		if r.rec.count() != 0 {
			t.Fatal("Work ran for an unauthenticated request")
		}
	}
}

func TestConcurrentRequestsFromSeveralAddressesAreCountedPerAddress(t *testing.T) {
	n := contractLimit(t, addrLimitKey)
	r := newRig(t, nil)
	const addrs = 6
	var reqs []func() int
	owner := []int{}
	for a := 0; a < addrs; a++ {
		for i := 0; i < 3*n; i++ {
			remote := "192.0.2." + strconv.Itoa(30+a) + ":" + strconv.Itoa(2000+i)
			reqs = append(reqs, func() int { return r.doFrom(remote, "GET", slicesURL(""), "", nil).Code })
			owner = append(owner, a)
		}
	}
	codes := burst(r, reqs)
	per := make([]map[int]int, addrs)
	for a := range per {
		per[a] = map[int]int{}
	}
	for i, c := range codes {
		per[owner[i]][c]++
	}
	for a, got := range per {
		if got[401] != n || got[429] != 2*n || len(got) != 2 {
			t.Errorf("address %d: %v, want %d 401s and %d 429s", a, got, n, 2*n)
		}
	}
}

// Requests with an accepted token and requests without one from the same address,
// at once: each kind gets its own budget, neither takes from the other.
func TestConcurrentTokenAndNoTokenRequestsFromOneAddressKeepSeparateBudgets(t *testing.T) {
	n := contractLimit(t, addrLimitKey)
	tl := contractLimit(t, "requests-per-second-per-token")
	for round := 0; round < 10; round++ {
		r := newRig(t, nil)
		var reqs []func() int
		for i := 0; i < 3*n; i++ {
			reqs = append(reqs, func() int { return r.doFrom("192.0.2.40:1", "GET", slicesURL(""), "", nil).Code })
		}
		for i := 0; i < 3*tl; i++ {
			reqs = append(reqs, func() int { return r.doFrom("192.0.2.40:1", "GET", slicesURL(""), clientToken, nil).Code })
		}
		codes := burst(r, reqs)
		anon, tok := tally(codes[:3*n]), tally(codes[3*n:])
		if anon[401] != n || anon[429] != 2*n || len(anon) != 2 {
			t.Fatalf("round %d: no-token requests: %v, want %d 401s and %d 429s", round, anon, n, 2*n)
		}
		if tok[200] != tl || tok[429] != 2*tl || len(tok) != 2 {
			t.Fatalf("round %d: token requests: %v, want %d 200s and %d 429s", round, tok, tl, 2*tl)
		}
	}
}

// --- start-up: the limit must be an integer ---

func TestContractWithANonIntegerAddressLimitIsRefused(t *testing.T) {
	raw, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatal(err)
	}
	line := "  " + addrLimitKey + ": " + strconv.Itoa(contractLimit(t, addrLimitKey)) + "\n"
	if !strings.Contains(string(raw), line) {
		t.Fatalf("the contract has no line %q to modify", line)
	}
	if _, err := parseContract(raw, apiVersion); err != nil {
		t.Fatalf("the unmodified contract is refused: %v", err)
	}
	for name, value := range map[string]string{
		"fraction":       "2.5",
		"just over":      "20.5",
		"just under":     "19.9",
		"quoted integer": `"20"`,
		"single quoted":  `'20'`,
		"boolean":        "true",
		"list":           "[20]",
		"map":            "{n: 20}",
		"words":          "twenty",
		"hex fraction":   "0x1.8p1",
	} {
		t.Run(name, func(t *testing.T) {
			mutated := strings.Replace(string(raw), line, "  "+addrLimitKey+": "+value+"\n", 1)
			if _, err := parseContract([]byte(mutated), apiVersion); err == nil {
				t.Fatalf("parseContract accepted %s: %s", addrLimitKey, value)
			}
		})
	}
}

// --- a limited request: body not read, key not recorded ---

// spyBody is a request body that notes whether anything read or closed it.
type spyBody struct {
	mu    sync.Mutex
	r     io.Reader
	reads int
}

func newSpy(body string) *spyBody { return &spyBody{r: strings.NewReader(body)} }
func (b *spyBody) Read(p []byte) (int, error) {
	b.mu.Lock()
	b.reads++
	b.mu.Unlock()
	return b.r.Read(p)
}
func (b *spyBody) Close() error { return nil }
func (b *spyBody) count() int   { b.mu.Lock(); defer b.mu.Unlock(); return b.reads }

const limitedKey = "limited-request-key"

// sendWithBody posts createBody-like content with a key and the spy as the body.
func (r *rig) sendWithBody(remote, token, body, key string) (reply, *spyBody) {
	spy := newSpy(body)
	req := httptest.NewRequest("POST", slicesURL(""), spy)
	req.RemoteAddr = remote
	req.ContentLength = int64(len(body))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.h.ServeHTTP(w, req)
	return reply{w}, spy
}

func TestAddressLimitedRequestWithBodyAndKeyIs429WithoutReadingOrRecording(t *testing.T) {
	n := contractLimit(t, addrLimitKey)
	if c := engineContract; len(limitedKey) < c.keyMin || len(limitedKey) > c.keyMax {
		t.Fatalf("test key length %d is outside the contract's %d to %d", len(limitedKey), c.keyMin, c.keyMax)
	}
	const remote = "192.0.2.50:1"
	for _, other := range []string{createBody, `{"description":"a different body"}`} {
		t.Run("later body "+strconv.Quote(other[:20]), func(t *testing.T) {
			r := newRig(t, nil)
			r.fill(t, remote, n, nil)
			w, spy := r.sendWithBody(remote, "", createBody, limitedKey)
			w.want429(t, "over the address limit, with a body and a key")
			if spy.count() != 0 {
				t.Errorf("the body of a 429 request was read (%d reads)", spy.count())
			}
			// Again, still limited: not a replay of anything either.
			w, spy = r.sendWithBody(remote, "", createBody, limitedKey)
			w.want429(t, "again over the address limit")
			if spy.count() != 0 {
				t.Errorf("the body of a second 429 request was read (%d reads)", spy.count())
			}
			if r.rec.count() != 0 {
				t.Fatal("Work ran for a limited request")
			}

			// The same key from a token holder, same address: a first use, not a replay,
			// and not a reuse with a different body.
			ok, _ := r.sendWithBody(remote, clientToken, other, limitedKey)
			if ok.Code != 202 {
				t.Fatalf("same key with a token and body %q: status = %d, want 202 (a first use); body: %s", other, ok.Code, ok.Body.String())
			}
			first := ok.operation(t, 202)
			r.waitFor(t, first.Id, "succeeded")
			if r.rec.count() != 1 {
				t.Fatalf("Work ran %d times, want 1", r.rec.count())
			}
			// And the key works as a key from then on: a retry replays the first result.
			again, _ := r.sendWithBody(remote, clientToken, other, limitedKey)
			if op := again.operation(t, 202); op.Id != first.Id {
				t.Errorf("a retry got operation %s, want a replay of %s", op.Id, first.Id)
			}
			if r.rec.count() != 1 {
				t.Errorf("Work ran %d times after the retry, want 1", r.rec.count())
			}
		})
	}
}

// Within the limit the request is refused with 401 before its body is read, too.
func TestUnauthenticatedRequestWithBodyAndKeyIs401WithoutReadingOrRecording(t *testing.T) {
	r := newRig(t, nil)
	w, spy := r.sendWithBody("192.0.2.51:1", "", createBody, limitedKey)
	w.want401(t, "no token, with a body and a key")
	if spy.count() != 0 {
		t.Errorf("the body of a 401 request was read (%d reads)", spy.count())
	}
	ok, _ := r.sendWithBody("192.0.2.51:1", clientToken, `{"description":"other"}`, limitedKey)
	if ok.Code != 202 {
		t.Fatalf("same key with a token: status = %d, want 202; body: %s", ok.Code, ok.Body.String())
	}
}

// A token over its own limit is refused the same way: nothing read, nothing recorded.
func TestTokenLimitedRequestWithBodyAndKeyIs429WithoutReadingOrRecording(t *testing.T) {
	tl := contractLimit(t, "requests-per-second-per-token")
	r := newRig(t, nil)
	for i := 0; i < tl; i++ {
		if w := r.doFrom("192.0.2.52:1", "GET", slicesURL(""), clientToken, nil); w.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d", i+1, w.Code)
		}
	}
	w, spy := r.sendWithBody("192.0.2.52:1", clientToken, createBody, limitedKey)
	w.problem(t, 429, "RateLimited")
	if spy.count() != 0 {
		t.Errorf("the body of a 429 request was read (%d reads)", spy.count())
	}
	r.clock.Advance(1100 * 1e6) // 1.1 s
	ok, _ := r.sendWithBody("192.0.2.52:1", clientToken, `{"description":"other"}`, limitedKey)
	if ok.Code != 202 {
		t.Fatalf("same key after the window: status = %d, want 202 (the key was not recorded); body: %s", ok.Code, ok.Body.String())
	}
}
