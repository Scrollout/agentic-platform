// T094 round 2: failing tests, by a separate author, for the warm base cache (research D37
// "Cost", owner 2026-11-15): the image holds a read-only base cache at /opt/gocache-base, and each
// call copies it into its own folder as its GOCACHE and discards the copy with the folder.
//
// Names the implementer follows: checksvc.Config gains GoCacheBase, the base cache folder
// (cmd/check reads it from CHECK_GOCACHE_BASE; the image sets /opt/gocache-base). Empty means no
// base: each call starts from an empty cache, as tests and a developer's host do. Set but missing
// or not a folder, the call fails closed: it runs no go command and does not pass (a 5xx problem,
// or a verdict with passed false), because an image whose base is missing is built wrong, and
// each call would silently compile from scratch.
//
// The commands are watched with call_folder_test.go's recording go (newCfEnv).
package checksvc_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// cfBase is a base cache folder as the image has it: read-only, holding entries in go's cache
// layout. It returns the folder and its entries' paths relative to it.
func cfBase(t *testing.T) (string, []string) {
	t.Helper()
	base := t.TempDir()
	entries := map[string]string{
		"README":            "This directory holds cached build artifacts from the Go build system.\n",
		"t094-base-entry":   "made by the image build\n",
		"ab/ab0123456789-a": "base action entry\n",
		"cd/cd0123456789-d": "base output entry\n",
	}
	var rels []string
	for rel, content := range entries {
		write(t, base, rel, []byte(content))
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	cfChmodTree(t, base, 0o555, 0o444)
	t.Cleanup(func() { cfChmodTree(t, base, 0o755, 0o644) })
	return base, rels
}

// cfGocacheEntries are the entries the first command of a call found in its GOCACHE, relative to
// that GOCACHE.
func cfGocacheEntries(r cfRecord) []string {
	cache := r.vars["GOCACHE"]
	var out []string
	for _, f := range r.found {
		if cache != "" && strings.HasPrefix(f, cache+"/") {
			out = append(out, strings.TrimPrefix(f, cache+"/"))
		}
	}
	sort.Strings(out)
	return out
}

func TestACallsCacheStartsWithTheBase(t *testing.T) {
	e := newCfEnv(t, false)
	base, rels := cfBase(t)
	root, dir := fixture(t, "testspass")
	c := checkConfig(t, root)
	c.GoCacheBase = base
	srv := serve(t, c)

	rs := []cfRecord{}
	for i := 1; i <= 2; i++ {
		e.reset()
		if st, b := cfStatus(t, srv, "/checks/AllTestsPass/run", sleepCheckBody(t, dir, 60)); st != 200 {
			t.Fatalf("call %d: %d %.300s", i, st, b)
		}
		got := e.records()
		if len(got) != 1 {
			t.Fatalf("call %d: want one go command, got %v", i, got)
		}
		rs = append(rs, got[0])
	}
	for i, r := range rs {
		what := fmt.Sprintf("call %d", i+1)
		folder := e.oneFolder(what, []cfRecord{r})
		cache := r.vars["GOCACHE"]
		if filepath.Clean(cache) == filepath.Clean(base) || inside(cache, base) {
			t.Errorf("%s: GOCACHE=%s is the base itself, not the call's copy", what, cache)
		}
		if folder != "" && !inside(cache, folder) {
			t.Errorf("%s: GOCACHE=%s is not inside the call's folder %s", what, cache, folder)
		}
		if !r.first {
			t.Errorf("%s: its first command was not recorded as first, so its folder was reused", what)
			continue
		}
		if got := cfGocacheEntries(r); strings.Join(got, ",") != strings.Join(rels, ",") {
			t.Errorf("%s: the call's GOCACHE starts with %v, want exactly the base's entries %v", what, got, rels)
		}
		wantGone(t, what, folder, 0)
	}
	if rs[0].vars["GOCACHE"] == rs[1].vars["GOCACHE"] {
		t.Errorf("two calls used the same GOCACHE %s", rs[0].vars["GOCACHE"])
	}
}

// cfCacheWriterTest is a declared test that needs the base's entry in its GOCACHE, then changes
// that entry and adds one of its own.
const cfCacheWriterTest = `package tests

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWritesItsCache(t *testing.T) {
	cache := os.Getenv("GOCACHE")
	if cache == "" {
		t.Fatal("GOCACHE is not set for the test")
	}
	entry := filepath.Join(cache, "t094-base-entry")
	b, err := os.ReadFile(entry)
	if err != nil || string(b) != "made by the image build\n" {
		t.Fatalf("NO-BASE-ENTRY %q %v", b, err)
	}
	os.Chmod(entry, 0o644)
	if err := os.WriteFile(entry, []byte("changed by a check\n"), 0o644); err != nil {
		t.Logf("the base entry could not be changed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cache, "t094-call-entry"), []byte("written by a check\n"), 0o644); err != nil {
		t.Fatalf("cannot write the call's cache: %v", err)
	}
}
`

func TestAWriteInOneCallsCacheIsGoneInTheNext(t *testing.T) {
	e := newCfEnv(t, false)
	base, rels := cfBase(t)
	before := snapshot(t, base)
	root, dir := fixture(t)
	write(t, dir, "go.mod", []byte("module example.com/p\n\ngo 1.26\n"))
	write(t, dir, "tests/cache_test.go", []byte(cfCacheWriterTest))
	c := checkConfig(t, root)
	c.GoCacheBase = base
	srv := serve(t, c)

	// Call 1 runs the real go: the test reads the base's entry, changes it and adds its own.
	e.mode("real")
	e.reset()
	passed, reason := cfVerdict(t, post(t, srv, "/checks/AllTestsPass/run", sleepCheckBody(t, dir, 120)))
	if !passed {
		t.Errorf("call 1: the call's GOCACHE must be a writable copy holding the base's entries: %.600s", reason)
	}
	if d := sameSnapshot(before, snapshot(t, base)); d != "" {
		t.Errorf("the base changed during call 1: %s", d)
	}
	r1 := e.records()
	folder := e.oneFolder("call 1", r1)
	wantGone(t, "call 1", folder, 0)

	// Call 2: its GOCACHE holds the base's entries only, the base's entry as the base has it.
	e.mode("fail")
	e.reset()
	if st, b := cfStatus(t, srv, "/checks/AllTestsPass/run", sleepCheckBody(t, dir, 60)); st != 200 {
		t.Fatalf("call 2: %d %.300s", st, b)
	}
	r2 := e.records()
	if len(r2) != 1 || !r2[0].first {
		t.Fatalf("call 2: want one go command, the first with its folder, got %v", r2)
	}
	if got := cfGocacheEntries(r2[0]); strings.Join(got, ",") != strings.Join(rels, ",") {
		t.Errorf("call 2's GOCACHE starts with %v, want only the base's entries %v (call 1's write survived?)", got, rels)
	}
	if len(r1) > 0 && r1[0].vars["GOCACHE"] == r2[0].vars["GOCACHE"] {
		t.Errorf("the two calls used the same GOCACHE %s", r2[0].vars["GOCACHE"])
	}
	if b, err := os.ReadFile(filepath.Join(base, "t094-base-entry")); err != nil || string(b) != "made by the image build\n" {
		t.Errorf("the base's entry changed: %q %v", b, err)
	}
	if d := sameSnapshot(before, snapshot(t, base)); d != "" {
		t.Errorf("the base changed: %s", d)
	}
}

func TestAMissingBaseFailsClosed(t *testing.T) {
	e := newCfEnv(t, false)
	root, dir := fixture(t, "testspass")
	missing := filepath.Join(t.TempDir(), "no-gocache-base")
	notDir := filepath.Join(t.TempDir(), "a-file")
	write(t, filepath.Dir(notDir), filepath.Base(notDir), []byte("not a folder\n"))
	for _, c := range []struct{ what, base string }{{"a missing base", missing}, {"a base that is a file", notDir}} {
		cfg := checkConfig(t, root)
		cfg.GoCacheBase = c.base
		srv := serve(t, cfg)
		e.mode("fail")
		e.reset()
		r := post(t, srv, "/checks/AllTestsPass/run", sleepCheckBody(t, dir, 60))
		switch {
		case r.status >= 500:
		case r.status == 200:
			if passed, reason := cfVerdict(t, r); passed {
				t.Errorf("%s: the call passed (%s); it must fail closed", c.what, reason)
			}
		default:
			t.Errorf("%s: want a 5xx problem or passed false, got %d %.300s", c.what, r.status, r.body)
		}
		if rs := e.records(); len(rs) > 0 {
			t.Errorf("%s: the call ran go commands %v; with no base it must run none", c.what, rs)
		}
	}
}
