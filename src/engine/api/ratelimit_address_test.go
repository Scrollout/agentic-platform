package api

// Tests for T018a: requests without an accepted token are rate-limited per source
// address. Written from the engine contract (info.description, x-limits, the 401 and
// 429 responses) by a test author who has not seen the implementation. The limit is
// read from the contract, never written as a literal.
//
// Boundaries exactly one second apart are not tested: "any one-second window" does not
// say whether the window is open or closed at its edge.

import (
	"fmt"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/agentic-platform/engine/api/gen"
)

const addrLimitKey = "requests-per-second-per-address-without-token"

// doFrom is rig.do with the connection's peer address set to remote ("host:port").
func (r *rig) doFrom(remote, method, path, token string, hdr map[string]string) reply {
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = remote
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.h.ServeHTTP(w, req)
	return reply{w}
}

func (p reply) want401(t *testing.T, what string) {
	t.Helper()
	if p.Code != 401 {
		t.Fatalf("%s: status = %d, want 401; body: %s", what, p.Code, p.Body.String())
	}
}

func (p reply) want429(t *testing.T, what string) {
	t.Helper()
	p.problem(t, 429, gen.ErrorCodeRateLimited)
	ra, err := strconv.Atoi(p.Header().Get("Retry-After"))
	if err != nil || ra < 1 {
		t.Fatalf("%s: Retry-After = %q, want a positive integer", what, p.Header().Get("Retry-After"))
	}
}

// fill sends n requests from remote with no token and wants 401 for each.
func (r *rig) fill(t *testing.T, remote string, n int, hdr func(i int) map[string]string) {
	t.Helper()
	for i := 0; i < n; i++ {
		var h map[string]string
		if hdr != nil {
			h = hdr(i)
		}
		r.doFrom(remote, "GET", slicesURL(""), "", h).want401(t, "request "+strconv.Itoa(i+1)+" from "+remote)
	}
}

func TestAddressNoTokenLimitThen429ThenRecovers(t *testing.T) {
	n := contractLimit(t, addrLimitKey)
	r := newRig(t, nil)
	r.fill(t, "192.0.2.1:5000", n, nil)
	r.doFrom("192.0.2.1:5000", "GET", slicesURL(""), "", nil).want429(t, "request N+1")
	r.doFrom("192.0.2.1:5000", "POST", slicesURL(""), "", nil).want429(t, "request N+2")
	r.clock.Advance(1100 * time.Millisecond)
	r.doFrom("192.0.2.1:5000", "GET", slicesURL(""), "", nil).want401(t, "after the window")
	if r.rec.count() != 0 {
		t.Fatal("Work ran for an unauthenticated request")
	}
}

func TestAddressLimitCountsMissingAndUnknownTokensTogether(t *testing.T) {
	n := contractLimit(t, addrLimitKey)
	r := newRig(t, nil)
	kinds := []func(i int) reply{
		func(int) reply { return r.doFrom("192.0.2.2:1", "GET", slicesURL(""), "", nil) },
		func(i int) reply {
			return r.doFrom("192.0.2.2:1", "GET", slicesURL(""), "garbage-"+strconv.Itoa(i), nil)
		},
		func(int) reply {
			return r.doFrom("192.0.2.2:1", "GET", slicesURL(""), "", map[string]string{"Authorization": "Basic " + clientToken})
		},
		func(int) reply {
			return r.doFrom("192.0.2.2:1", "GET", slicesURL(""), "", map[string]string{"Authorization": "Bearer "})
		},
	}
	for i := 0; i < n; i++ {
		kinds[i%len(kinds)](i).want401(t, "mixed request "+strconv.Itoa(i+1))
	}
	// Each kind, tried next, is over the one shared count.
	for k, send := range kinds {
		send(n+k).want429(t, "kind "+strconv.Itoa(k)+" after the shared limit")
	}
}

func TestAddressesAreCountedSeparately(t *testing.T) {
	n := contractLimit(t, addrLimitKey)
	r := newRig(t, nil)
	r.fill(t, "192.0.2.3:1", n, nil)
	r.doFrom("192.0.2.3:1", "GET", slicesURL(""), "", nil).want429(t, "first address over")
	// A second address starts from zero, and the first stays limited.
	r.fill(t, "192.0.2.4:1", n, nil)
	r.doFrom("192.0.2.4:1", "GET", slicesURL(""), "", nil).want429(t, "second address over")
	r.doFrom("192.0.2.3:1", "GET", slicesURL(""), "", nil).want429(t, "first address still over")
	// IPv6 is an address too, and a different one.
	r.fill(t, "[2001:db8::1]:1", n, nil)
	r.doFrom("[2001:db8::1]:1", "GET", slicesURL(""), "", nil).want429(t, "IPv6 over")
	r.doFrom("[2001:db8::2]:1", "GET", slicesURL(""), "", nil).want401(t, "other IPv6 address")
}

func TestSameHostDifferentPortsIsOneAddress(t *testing.T) {
	n := contractLimit(t, addrLimitKey)
	r := newRig(t, nil)
	for i := 0; i < n; i++ {
		r.doFrom("192.0.2.5:"+strconv.Itoa(10000+i), "GET", slicesURL(""), "", nil).want401(t, "request "+strconv.Itoa(i+1))
	}
	r.doFrom("192.0.2.5:60000", "GET", slicesURL(""), "", nil).want429(t, "new port, same host")
	r2 := newRig(t, nil)
	for i := 0; i < n; i++ {
		r2.doFrom("[2001:db8::5]:"+strconv.Itoa(10000+i), "GET", slicesURL(""), "", nil).want401(t, "IPv6 request "+strconv.Itoa(i+1))
	}
	r2.doFrom("[2001:db8::5]:60000", "GET", slicesURL(""), "", nil).want429(t, "IPv6 new port, same host")
}

var spoofHeaders = []string{"X-Forwarded-For", "X-Real-IP", "Forwarded", "X-Client-IP", "True-Client-IP", "CF-Connecting-IP"}

func spoofValue(name, ip string) string {
	if name == "Forwarded" {
		return "for=" + ip
	}
	return ip
}

func TestForwardingHeadersDoNotChangeTheAddress(t *testing.T) {
	n := contractLimit(t, addrLimitKey)
	for _, name := range spoofHeaders {
		t.Run(name+" varying from one peer is still limited", func(t *testing.T) {
			r := newRig(t, nil)
			r.fill(t, "192.0.2.6:1", n, func(i int) map[string]string {
				return map[string]string{name: spoofValue(name, "203.0.113."+strconv.Itoa(i+1))}
			})
			r.doFrom("192.0.2.6:1", "GET", slicesURL(""), "", map[string]string{name: spoofValue(name, "198.51.100.200")}).
				want429(t, "spoofed fresh header value")
			r.doFrom("192.0.2.6:1", "GET", slicesURL(""), "", nil).want429(t, "no header")
		})
		t.Run(name+" equal from different peers is not merged", func(t *testing.T) {
			r := newRig(t, nil)
			same := map[string]string{name: spoofValue(name, "203.0.113.9")}
			r.fill(t, "192.0.2.7:1", n, func(int) map[string]string { return same })
			r.doFrom("192.0.2.7:1", "GET", slicesURL(""), "", same).want429(t, "first peer over")
			r.doFrom("192.0.2.8:1", "GET", slicesURL(""), "", same).want401(t, "second peer with the same header")
		})
	}
	t.Run("several headers together", func(t *testing.T) {
		r := newRig(t, nil)
		r.fill(t, "192.0.2.9:1", n, func(i int) map[string]string {
			ip := "203.0.114." + strconv.Itoa(i+1)
			return map[string]string{"X-Forwarded-For": ip + ", 10.0.0.1", "X-Real-IP": ip, "Forwarded": "for=" + ip}
		})
		r.doFrom("192.0.2.9:1", "GET", slicesURL(""), "", map[string]string{"X-Forwarded-For": "1.2.3.4"}).want429(t, "over")
	})
}

func TestAcceptedTokenIsNeverCountedPerAddress(t *testing.T) {
	n := contractLimit(t, addrLimitKey)
	tokenLimit := contractLimit(t, "requests-per-second-per-token")
	r := newRig(t, nil)
	const addr = "192.0.2.10:1"
	r.fill(t, addr, n, nil)
	r.doFrom(addr, "GET", slicesURL(""), "", nil).want429(t, "address over")
	// The same address, now with accepted tokens: all pass, up to the token's own budget.
	for i := 0; i < tokenLimit; i++ {
		if w := r.doFrom(addr, "GET", slicesURL(""), clientToken, nil); w.Code != 200 {
			t.Fatalf("token request %d from a limited address: status = %d: %s", i+1, w.Code, w.Body.String())
		}
	}
	// The token's own limit still applies, and is the per-token one.
	r.doFrom(addr, "GET", slicesURL(""), clientToken, nil).problem(t, 429, gen.ErrorCodeRateLimited)
	// And the address is still limited without a token.
	r.doFrom(addr, "GET", slicesURL(""), "", nil).want429(t, "address still over")
}

func TestAcceptedTokenRequestsDoNotUseUpTheAddressBudget(t *testing.T) {
	n := contractLimit(t, addrLimitKey)
	r := newRig(t, nil)
	const addr = "192.0.2.11:1"
	// Token requests (within the token's budget) first; the address then has its whole budget.
	for i := 0; i < min(n, contractLimit(t, "requests-per-second-per-token")); i++ {
		if w := r.doFrom(addr, "GET", slicesURL(""), pageToken, nil); w.Code != 200 {
			t.Fatalf("token request %d: status = %d", i+1, w.Code)
		}
	}
	r.fill(t, addr, n, nil)
	r.doFrom(addr, "GET", slicesURL(""), "", nil).want429(t, "after N no-token requests")
}

func TestNoTokenRequestsDoNotUseUpATokensBudget(t *testing.T) {
	n := contractLimit(t, addrLimitKey)
	tokenLimit := contractLimit(t, "requests-per-second-per-token")
	r := newRig(t, nil)
	// Many addresses, each within its limit, and one over its limit: lots of no-token traffic.
	for a := 1; a <= 3; a++ {
		remote := "192.0.2." + strconv.Itoa(100+a) + ":1"
		r.fill(t, remote, n, nil)
		r.doFrom(remote, "GET", slicesURL(""), "", nil).want429(t, "no-token traffic over the limit")
	}
	for i := 0; i < tokenLimit; i++ {
		if w := r.doFrom("192.0.2.150:1", "GET", slicesURL(""), clientToken, nil); w.Code != 200 {
			t.Fatalf("token request %d after no-token traffic: status = %d: %s", i+1, w.Code, w.Body.String())
		}
	}
}

func TestAddressWindowSlides(t *testing.T) {
	n := contractLimit(t, addrLimitKey)
	if n < 2 {
		t.Skip("needs a limit of at least 2")
	}
	a, b := n/2, n-n/2
	const addr = "192.0.2.12:1"

	t.Run("a request is refused while the window holds N, and allowed once it is empty", func(t *testing.T) {
		r := newRig(t, nil)
		r.fill(t, addr, a, nil) // t
		r.clock.Advance(600 * time.Millisecond)
		r.fill(t, addr, b, nil) // t+0.6s
		r.clock.Advance(300 * time.Millisecond)
		r.doFrom(addr, "GET", slicesURL(""), "", nil).want429(t, "t+0.9s: N within one second") // t+0.9s
		r.clock.Advance(800 * time.Millisecond)
		r.doFrom(addr, "GET", slicesURL(""), "", nil).want401(t, "t+1.7s: both batches are more than a second old")
	})

	t.Run("only the older batch leaves the window at t+1.05s", func(t *testing.T) {
		r := newRig(t, nil)
		r.fill(t, addr, a, nil) // t
		r.clock.Advance(600 * time.Millisecond)
		r.fill(t, addr, b, nil) // t+0.6s
		r.clock.Advance(450 * time.Millisecond)
		// The window (t+0.05s, t+1.05s] holds b requests, so exactly a more fit.
		r.fill(t, addr, a, nil)
		r.doFrom(addr, "GET", slicesURL(""), "", nil).want429(t, "t+1.05s: window full again")
	})

	t.Run("a steady stream never gets more than N in any second", func(t *testing.T) {
		r := newRig(t, nil)
		step := time.Second / time.Duration(n) / 2 // twice the allowed rate
		var allowed []time.Time
		for i := 0; i < 6*n; i++ {
			w := r.doFrom(addr, "GET", slicesURL(""), "", nil)
			switch w.Code {
			case 401:
				allowed = append(allowed, r.clock.Now())
			case 429:
			default:
				t.Fatalf("status = %d", w.Code)
			}
			r.clock.Advance(step)
		}
		if len(allowed) == 0 || len(allowed) == 6*n {
			t.Fatalf("allowed %d of %d: the limit did not bite or nothing passed", len(allowed), 6*n)
		}
		for i := range allowed {
			c := 0
			for _, x := range allowed[i:] {
				if x.Before(allowed[i].Add(time.Second)) {
					c++
				}
			}
			if c > n {
				t.Fatalf("%d requests passed within one second of %v, limit %d", c, allowed[i], n)
			}
		}
	})
}

func TestProviderOperationsListIsLimitedPerAddress(t *testing.T) {
	n := contractLimit(t, addrLimitKey)
	r := newRig(t, nil)
	const path = "/providers/Agentic.Engine/operations"
	for i := 0; i < n; i++ {
		r.doFrom("192.0.2.13:1", "GET", path, "", nil).want401(t, "list request "+strconv.Itoa(i+1))
	}
	r.doFrom("192.0.2.13:1", "GET", path, "", nil).want429(t, "list over the limit")
	// The count is the address's, shared with every other path.
	r.doFrom("192.0.2.13:2", "GET", slicesURL(""), "", nil).want429(t, "another path, same address")
	r.doFrom("192.0.2.14:1", "GET", path, "", nil).want401(t, "other address")
	// With an accepted token the list is served from the limited address.
	w := r.doFrom("192.0.2.13:1", "GET", path, clientToken, nil)
	if w.Code != 200 {
		t.Fatalf("list with a token from a limited address: status = %d: %s", w.Code, w.Body.String())
	}
}

// --- address spelling ---

func TestIPv4MappedIPv6SharesTheIPv4Count(t *testing.T) {
	n := contractLimit(t, addrLimitKey)
	r := newRig(t, nil)
	for i := 0; i < n; i++ {
		remote := "192.0.2.60:1"
		if i%2 == 1 {
			remote = "[::ffff:192.0.2.60]:2"
		}
		r.doFrom(remote, "GET", slicesURL(""), "", nil).want401(t, "request "+strconv.Itoa(i+1))
	}
	r.doFrom("192.0.2.60:3", "GET", slicesURL(""), "", nil).want429(t, "plain form over")
	r.doFrom("[::ffff:192.0.2.60]:4", "GET", slicesURL(""), "", nil).want429(t, "mapped form over")
}

func TestRemoteAddrWithoutPortIsUsedWhole(t *testing.T) {
	n := contractLimit(t, addrLimitKey)
	r := newRig(t, nil)
	r.fill(t, "192.0.2.61", n, nil)
	r.doFrom("192.0.2.61", "GET", slicesURL(""), "", nil).want429(t, "no-port address over")
	r.doFrom("192.0.2.62", "GET", slicesURL(""), "", nil).want401(t, "another no-port address")
	r.fill(t, "unix-peer", n, nil)
	r.doFrom("unix-peer", "GET", slicesURL(""), "", nil).want429(t, "non-IP peer over")
}

// --- start-up: the address limit must be in the contract ---

func TestContractWithoutAPositiveAddressLimitIsRefused(t *testing.T) {
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
	for name, repl := range map[string]string{
		"missing":  "",
		"zero":     "  " + addrLimitKey + ": 0\n",
		"negative": "  " + addrLimitKey + ": -5\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseContract([]byte(strings.Replace(string(raw), line, repl, 1)), apiVersion); err == nil {
				t.Fatal("parseContract accepted a contract without a positive address limit")
			}
		})
	}
}

// --- limiter memory (unit tests on the limiter itself) ---

func (l *limiter) has(key string) bool { _, ok := l.windows[key]; return ok }

func TestLimiterDropsIdleKeysAndKeepsLiveOnes(t *testing.T) {
	const per = 2
	clock := newFakeClock()
	l := newLimiter(clock.Now, per)
	// More than minSweep keys, two requests each: every one is at its limit.
	for i := 0; i < 3*minSweep; i++ {
		for j := 0; j < per; j++ {
			if _, ok := l.allow(fmt.Sprintf("old-%d", i)); !ok {
				t.Fatalf("old-%d request %d refused", i, j+1)
			}
		}
	}
	if len(l.windows) < 3*minSweep-minSweep { // idle-free keys are never dropped early
		t.Fatalf("only %d windows after %d fresh keys: live keys were dropped", len(l.windows), 3*minSweep)
	}
	clock.Advance(1200 * time.Millisecond)
	for j := 0; j < per; j++ { // "warm" has one request used and one left, then is at its limit
		if _, ok := l.allow("warm"); !ok {
			t.Fatal("warm refused")
		}
	}
	clock.Advance(300 * time.Millisecond) // the old keys are 1.5s idle, warm 0.3s
	if _, ok := l.allow("hot"); !ok {
		t.Fatal("hot refused")
	}
	before := len(l.windows)
	added := 0
	for len(l.windows) >= before && added < 10*minSweep {
		if _, ok := l.allow(fmt.Sprintf("new-%d", added)); !ok {
			t.Fatal("a new key was refused")
		}
		added++
	}
	if len(l.windows) >= before {
		t.Fatalf("no sweep after %d new keys: %d windows", added, len(l.windows))
	}
	for i := 0; i < 3*minSweep; i++ {
		if l.has(fmt.Sprintf("old-%d", i)) {
			t.Fatalf("old-%d, idle for 1.5s, is still held", i)
		}
	}
	if want := added + 2; len(l.windows) != want { // the new keys, warm and hot
		t.Fatalf("%d windows held, want %d live ones", len(l.windows), want)
	}
	// Live keys survived with their counts: warm is at its limit, so it is refused.
	if _, ok := l.allow("warm"); ok {
		t.Fatal("warm was dropped while live: its limit no longer applies")
	}
	if _, ok := l.allow("hot"); !ok {
		t.Fatal("hot has one request left")
	}
	if _, ok := l.allow("hot"); ok {
		t.Fatal("hot over its limit was allowed")
	}
	// A dropped key gets the answer it would have had with no eviction: a fresh window.
	ref := newLimiter(clock.Now, per)
	for _, key := range []string{"old-0", "old-1"} {
		for j := 0; j < per+1; j++ {
			_, got := l.allow(key)
			_, want := ref.allow(key)
			if got != want {
				t.Fatalf("%s request %d: allowed=%v after eviction, %v without any history", key, j+1, got, want)
			}
		}
	}
}

func TestLimiterDoesNotSweepOnEveryNewKeyWhenNothingIsIdle(t *testing.T) {
	clock := newFakeClock()
	l := newLimiter(clock.Now, 1)
	changes := 0
	last := l.sweepAt
	for i := 0; i < 2000; i++ {
		l.allow(fmt.Sprintf("k-%d", i))
		if l.sweepAt != last {
			changes++
			last = l.sweepAt
		}
		if l.sweepAt < len(l.windows) {
			t.Fatalf("after %d keys sweepAt = %d is below %d windows", i+1, l.sweepAt, len(l.windows))
		}
	}
	if len(l.windows) != 2000 {
		t.Fatalf("%d windows, want 2000: live keys were dropped", len(l.windows))
	}
	if changes > 8 { // doubling from 64 reaches 2000 in about 5 steps
		t.Fatalf("sweepAt moved %d times for 2000 keys: sweeps are not amortized", changes)
	}
}
