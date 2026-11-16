// Tests for T014a (the run-store cases T014 left open) and T017a (the store's
// slice ID, run ID and hash rules are the contract's, owned by
// slice-defs.schema.json). Written from the tasks and the contract, not from
// store.go.
//
// T017a: the patterns are read from the contract's slice-defs.schema.json at
// test time ($defs.slice_id, $defs.run_id, $defs.hash), never written here, and
// a table of values, valid and invalid, is run through every public method that
// takes an ID or hash. A value is accepted exactly when the schema's pattern
// matches it. If the contract changes a pattern, this table is still judged by
// the new pattern; the values themselves cover the edges of today's patterns
// (leading zeros, any length, signs, case, trailing newline, non-ASCII digits,
// path syntax).
package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/agentic-platform/slice"
)

// testIDs is the contract's rules, loaded from the newest contract version the
// way the engine loads them (T017a). The store takes them at Open.
func testIDs(t *testing.T) *slice.Schemas {
	t.Helper()
	dirs, _ := filepath.Glob("../api/preview/*")
	if len(dirs) == 0 {
		t.Fatal("no contract version under ../api/preview")
	}
	sort.Strings(dirs)
	ids, err := slice.LoadSchemas(dirs[len(dirs)-1])
	if err != nil {
		t.Fatal(err)
	}
	return ids
}

// contractPattern returns the pattern of $defs.<def> in the newest contract
// version's slice-defs.schema.json.
func contractPattern(t *testing.T, def string) *regexp.Regexp {
	t.Helper()
	files, err := filepath.Glob("../api/preview/*/slice-defs.schema.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no slice-defs.schema.json under ../api/preview: %v", err)
	}
	sort.Strings(files)
	raw, err := os.ReadFile(files[len(files)-1])
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Defs map[string]struct {
			Pattern string `json:"pattern"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	p := doc.Defs[def].Pattern
	if p == "" {
		t.Fatalf("%s has no $defs.%s.pattern", files[len(files)-1], def)
	}
	re, err := regexp.Compile(p)
	if err != nil {
		t.Fatalf("$defs.%s.pattern %q: %v", def, p, err)
	}
	return re
}

const (
	validHash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	other     = "S-9" // a valid slice ID used where the ID under test is another kind
)

// idCandidates are values around the slice and run ID patterns; prefix is "S-" or "RUN-".
func idCandidates(prefix string) []string {
	other := "RUN-"
	if prefix == "RUN-" {
		other = "S-"
	}
	return []string{
		prefix + "1", prefix + "0", prefix + "007", prefix + "42", prefix + "9999999999999999999999999999",
		prefix + "1234567890", prefix + strings.Repeat("1", 100),
		"", prefix, strings.TrimSuffix(prefix, "-"), strings.ToLower(prefix) + "1", prefix + "1\n", prefix + "1 ", " " + prefix + "1",
		"\n" + prefix + "1", prefix + "\n1", prefix + "-1", prefix + "+1", prefix + "1.0", prefix + "1e3", prefix + "0x1", prefix + "１", // fullwidth
		prefix + "١", // Arabic-Indic digit
		prefix + "1\x00", prefix + "1/..", "../" + prefix + "1", "./" + prefix + "1", prefix + "1/", "/" + prefix + "1",
		prefix + "1\\", prefix + "1_", prefix + "a", prefix + "1a", prefix[:len(prefix)-1] + "_1", prefix + "1" + prefix + "2",
		other + "1", strings.ToUpper(prefix) + "1" + "\r", "S-1,RUN-1",
	}
}

func hashCandidates() []string {
	return []string{
		validHash, strings.Repeat("0", 64), strings.Repeat("f", 64), strings.Repeat("a", 64),
		"", "abc", validHash[:63], validHash + "0", validHash[:32],
		strings.ToUpper(validHash), "A" + validHash[1:], validHash[:63] + "G", validHash[:63] + "g", validHash[:63] + "-",
		validHash + "\n", " " + validHash, validHash + " ", "\n" + validHash, validHash[:63] + "\n", validHash[:30] + "\n" + validHash[31:],
		"0x" + validHash[2:], "../" + validHash, "../../etc/passwd", "." + validHash[1:], validHash[:32] + "/" + validHash[33:],
		validHash[:63] + "０", // fullwidth zero
		validHash[:63] + "\x00", strings.Repeat("0", 128), "sha256:" + validHash,
	}
}

// rule is the oracle for a value: the contract's own rule as the slice module
// applies it (JSON Schema semantics, not Go regexp), found by $defs name.
type rule struct {
	def   string
	valid func(string) bool
}

func (r rule) MatchString(v string) bool { return r.valid(v) }
func (r rule) String() string            { return "$defs." + r.def }

func contractRule(t *testing.T, def string) rule {
	t.Helper()
	ids := testIDs(t)
	var f func(string) bool
	switch def {
	case "slice_id":
		f = func(v string) bool { _, err := ids.ParseID(v); return err == nil }
	case "run_id":
		f = func(v string) bool { _, err := ids.ParseRunID(v); return err == nil }
	case "hash":
		f = func(v string) bool { _, err := ids.ParseHash(v); return err == nil }
	default:
		t.Fatalf("no rule %s", def)
	}
	return rule{def, f}
}

// treeOf lists every file and folder under dir with the file contents, so that
// a call that must change nothing can be shown to have changed nothing.
func treeOf(t *testing.T, dir string) map[string]string {
	t.Helper()
	m := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if d.IsDir() {
			m[rel+"/"] = ""
			return nil
		}
		b, err := os.ReadFile(p)
		m[rel] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func sameTree(t *testing.T, what string, before, after map[string]string) {
	t.Helper()
	for p, b := range before {
		if a, ok := after[p]; !ok {
			t.Errorf("%s: %q disappeared", what, p)
		} else if a != b {
			t.Errorf("%s: %q changed", what, p)
		}
	}
	for p := range after {
		if _, ok := before[p]; !ok {
			t.Errorf("%s: %q was created", what, p)
		}
	}
}

// callsWithID runs one public method that takes the ID kind under test with
// the candidate value, in a fresh store; it returns the error. A "not found"
// for a well-formed value is reported as nil: the value was accepted.
type idCall struct {
	name string
	run  func(s *Store, v string) error
}

func sliceCalls() []idCall {
	return []idCall{
		{"Append", func(s *Store, v string) error {
			_, err := s.Append(v, Event{Run: "RUN-1", Kind: "RunStarted"})
			return err
		}},
		{"PutArtifact", func(s *Store, v string) error { _, err := s.PutArtifact(v, []byte("x")); return err }},
		{"EraseSlice", func(s *Store, v string) error { return s.EraseSlice(v) }},
	}
}

func runCalls() []idCall {
	return []idCall{
		{"Append", func(s *Store, v string) error {
			_, err := s.Append("S-1", Event{Run: v, Kind: "RunStarted"})
			return err
		}},
		{"Events", func(s *Store, v string) error { _, err := s.Events(v); return err }},
	}
}

func hashCalls() []idCall {
	return []idCall{
		{"Artifact", func(s *Store, v string) error {
			_, err := s.Artifact(v)
			if errors.Is(err, ErrNotFound) {
				return nil // well-formed, just not stored
			}
			return err
		}},
	}
}

func checkIDTable(t *testing.T, what string, re interface {
	MatchString(string) bool
	String() string
}, values []string, calls []idCall) {
	t.Helper()
	sawValid, sawInvalid := false, false
	for _, v := range values {
		valid := re.MatchString(v)
		sawValid, sawInvalid = sawValid || valid, sawInvalid || !valid
		for _, c := range calls {
			root := t.TempDir()
			dir := filepath.Join(root, "store")
			s := open(t, dir)
			before := treeOf(t, root)
			err := c.run(s, v)
			if valid && err != nil {
				t.Errorf("%s %s(%q): refused with %v, but the contract pattern %s matches", what, c.name, v, err, re)
			}
			if !valid {
				if err == nil {
					t.Errorf("%s %s(%q): accepted, but the contract pattern %s does not match", what, c.name, v, re)
				}
				sameTree(t, what+" "+c.name+"("+v+") refused", before, treeOf(t, root))
			}
			s.Close()
		}
	}
	if !sawValid || !sawInvalid {
		t.Fatalf("%s: the table has no %s values for pattern %s; it must cover both", what, map[bool]string{true: "invalid", false: "valid"}[sawValid], re)
	}
}

func TestSliceIDsAreAcceptedExactlyWhenTheContractPatternMatches(t *testing.T) {
	re := contractRule(t, "slice_id")
	checkIDTable(t, "slice ID", re, idCandidates("S-"), sliceCalls())
}

func TestRunIDsAreAcceptedExactlyWhenTheContractPatternMatches(t *testing.T) {
	re := contractRule(t, "run_id")
	checkIDTable(t, "run ID", re, idCandidates("RUN-"), runCalls())
}

func TestHashesAreAcceptedExactlyWhenTheContractPatternMatches(t *testing.T) {
	re := contractRule(t, "hash")
	checkIDTable(t, "hash", re, hashCandidates(), hashCalls())
}

// The patterns are what the tests above exercise; this pins today's contract
// so that a loosened pattern (say, S-<anything>) is a visible decision, and
// shows the table is not vacuous.
func TestContractPatternsAreWhatTheTableAssumes(t *testing.T) {
	for def, want := range map[string]string{"slice_id": "S-1", "run_id": "RUN-1", "hash": validHash} {
		if re := contractPattern(t, def); !re.MatchString(want) {
			t.Errorf("$defs.%s pattern %s does not match %q", def, re, want)
		}
	}
	if contractPattern(t, "slice_id").MatchString("RUN-1") || contractPattern(t, "run_id").MatchString("S-1") {
		t.Error("slice and run ID patterns overlap")
	}
	for def, bad := range map[string][]string{
		"slice_id": {"", "S-", "s-1", "S-1\n", "S-a"},
		"run_id":   {"", "RUN-", "run-1", "RUN-1\n", "RUN-a"},
		"hash":     {"", validHash[:63], validHash + "0", strings.ToUpper(validHash), validHash[:63] + "g"},
	} {
		for _, v := range bad {
			if contractPattern(t, def).MatchString(v) {
				t.Errorf("$defs.%s pattern now matches %q; this table's edge cases were chosen for the pattern it replaced", def, v)
			}
		}
	}
}

// An ID's validity must not depend on what is already stored: the refusal of a
// bad ID holds in a store that has records, and the records are untouched.
func TestInvalidIDsAreRefusedInAStoreWithRecordsAndChangeNothing(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "store")
	s := open(t, dir)
	defer s.Close()
	appendEv(t, s, "S-1", "RUN-1", "RunStarted")
	h, err := s.PutArtifact("S-1", []byte("kept"))
	if err != nil {
		t.Fatal(err)
	}
	before := treeOf(t, root)
	for _, v := range []string{"", "S-", "s-1", "S-1\n", "../S-1", "RUN-1", "S-x"} { // bad slice IDs
		if _, err := s.Append(v, Event{Run: "RUN-2", Kind: "K"}); err == nil {
			t.Errorf("Append under slice %q accepted", v)
		}
		if _, err := s.PutArtifact(v, []byte("new")); err == nil {
			t.Errorf("PutArtifact under slice %q accepted", v)
		}
		if err := s.EraseSlice(v); err == nil {
			t.Errorf("EraseSlice(%q) accepted", v)
		}
	}
	for _, v := range []string{"", "RUN-", "run-1", "RUN-1\n", "../RUN-1", "S-1", "RUN-x"} { // bad run IDs
		if _, err := s.Append("S-1", Event{Run: v, Kind: "K"}); err == nil {
			t.Errorf("Append of run %q accepted", v)
		}
		if _, err := s.Events(v); err == nil {
			t.Errorf("Events(%q) accepted", v)
		}
	}
	for _, v := range []string{"", "abc", "S-1", "../" + validHash, strings.ToUpper(validHash)} { // bad hashes
		if _, err := s.Artifact(v); err == nil {
			t.Errorf("Artifact(%q) accepted", v)
		}
	}
	if _, err := s.Append("S-1", Event{Run: "RUN-1"}); err == nil {
		t.Error("Append with an empty Kind accepted")
	}
	sameTree(t, "refused calls", before, treeOf(t, root))
	if b, err := s.Artifact(h); err != nil || string(b) != "kept" {
		t.Errorf("earlier artifact = %q, %v", b, err)
	}
	if evs, _ := s.Events("RUN-1"); len(evs) != 1 {
		t.Errorf("RUN-1 has %d events, want 1", len(evs))
	}
}

// A refused ID that is path syntax must not touch anything outside the store.
func TestPathLikeIDsNeverLeaveTheStoreFolder(t *testing.T) {
	root := t.TempDir()
	s := open(t, filepath.Join(root, "inner", "store"))
	defer s.Close()
	before := treeOf(t, root)
	for _, v := range []string{"..", "../outside", "S-1/../../outside", "/abs", "S-1/x"} {
		s.Append(v, Event{Run: "RUN-1", Kind: "K"})
		s.PutArtifact(v, []byte("x"))
		s.EraseSlice(v)
		s.Artifact(v)
		s.Events(v)
	}
	sameTree(t, "path-like IDs", before, treeOf(t, root))
}

// Open applies the same rules to what it finds on disk: a slice folder whose
// name is not a slice ID, or an ownership line that is not a hash, is refused.
func TestOpenRefusesRecordsWhoseIDsBreakTheContractPatterns(t *testing.T) {
	sliceRe, hashRe := contractRule(t, "slice_id"), contractRule(t, "hash")
	for _, name := range []string{"S-1", "S-007", "S-", "s-1", "RUN-1", "S-1x", "x", "S-1.bak", "S--1"} {
		dir := filepath.Join(t.TempDir(), "store")
		if err := os.MkdirAll(filepath.Join(dir, "slices", name), 0o700); err != nil {
			t.Fatal(err)
		}
		s, err := Open(dir, testIDs(t))
		if want := sliceRe.MatchString(name); want != (err == nil) {
			t.Errorf("Open with slice folder %q: err = %v, but the contract pattern says valid = %v", name, err, want)
		}
		if s != nil {
			s.Close()
		}
	}
	for _, h := range []string{validHash, validHash[:63], strings.ToUpper(validHash), "../" + validHash, validHash + "0"} {
		dir := filepath.Join(t.TempDir(), "store")
		sd := filepath.Join(dir, "slices", "S-1")
		if err := os.MkdirAll(sd, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sd, "artifacts"), []byte(h+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		s, err := Open(dir, testIDs(t))
		if want := hashRe.MatchString(h); want != (err == nil) {
			t.Errorf("Open with ownership line %q: err = %v, but the contract pattern says valid = %v", h, err, want)
		}
		if s != nil {
			s.Close()
		}
	}
}

// --- T014a: a run appended under a second slice is refused ---

func TestRunAppendedUnderASecondSliceIsRefused(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "store")
	s := open(t, dir)
	appendEv(t, s, "S-1", "RUN-1", "RunStarted")
	before := treeOf(t, root)

	if _, err := s.Append("S-2", Event{Run: "RUN-1", Kind: "StepStarted"}); err == nil {
		t.Fatal("Append of RUN-1 under S-2 accepted, though RUN-1 belongs to S-1")
	}
	sameTree(t, "refused append", before, treeOf(t, root))
	if anywhere(t, dir, "S-2") {
		t.Error("the refused append left a trace of S-2 on disk")
	}
	evs, _ := s.Events("RUN-1")
	if len(evs) != 1 {
		t.Fatalf("RUN-1 has %d events after the refused append, want 1", len(evs))
	}
	// The run goes on under its own slice with no gap in Seq.
	if got := appendEv(t, s, "S-1", "RUN-1", "StepStarted"); got.Seq != 2 {
		t.Errorf("Seq after a refused append = %d, want 2", got.Seq)
	}
	// Another slice's own runs are unaffected.
	if got := appendEv(t, s, "S-2", "RUN-2", "RunStarted"); got.Seq != 1 {
		t.Errorf("RUN-2 under S-2: Seq = %d, want 1", got.Seq)
	}
	// And the refusal holds for a store opened later.
	s.Close()
	s = open(t, dir)
	defer s.Close()
	if _, err := s.Append("S-2", Event{Run: "RUN-1", Kind: "X"}); err == nil {
		t.Error("after reopen, RUN-1 under S-2 accepted")
	}
	if _, err := s.Append("S-1", Event{Run: "RUN-2", Kind: "X"}); err == nil {
		t.Error("after reopen, RUN-2 under S-1 accepted")
	}
	if evs, _ := s.Events("RUN-1"); len(evs) != 2 {
		t.Errorf("RUN-1 has %d events after reopen, want 2", len(evs))
	}
}

// Erasing a slice frees its runs: another slice may then use the ID, and does
// not inherit the erased events.
func TestErasedSlicesRunIDCanBeUsedByAnotherSlice(t *testing.T) {
	s := open(t, t.TempDir())
	defer s.Close()
	appendEv(t, s, "S-1", "RUN-1", "RunStarted")
	if err := s.EraseSlice("S-1"); err != nil {
		t.Fatal(err)
	}
	if got := appendEv(t, s, "S-2", "RUN-1", "RunStarted"); got.Seq != 1 {
		t.Errorf("Seq = %d, want 1", got.Seq)
	}
}

// --- T014a: a torn last line in events.jsonl is dropped on open ---

func eventFiles(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() == "events.jsonl" {
			out = append(out, p)
		}
		return nil
	})
	if len(out) == 0 {
		t.Fatalf("no events.jsonl under %s", dir)
	}
	return out
}

func appendRaw(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
}

func TestTornLastEventLineIsDroppedOnOpenAndEarlierLinesKept(t *testing.T) {
	torn := []struct{ name, tail string }{
		{"cut inside a key", `{"seq":4,"ru`},
		{"cut inside a value", `{"seq":4,"run":"RUN-1","kind":"Step`},
		{"one character", `{`},
		{"cut before the closing brace", `{"seq":4,"run":"RUN-1","kind":"StepAccepted"`},
		{"a whole event without its newline", `{"seq":4,"run":"RUN-1","kind":"StepAccepted"}`},
		{"cut inside a multi-byte character", "{\"seq\":4,\"run\":\"RUN-1\",\"kind\":\"\xe2\x82"},
		{"garbage", `not json at all`},
	}
	for _, c := range torn {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			s := open(t, dir)
			for _, k := range []string{"RunStarted", "StepStarted", "StepAccepted"} {
				appendEv(t, s, "S-1", "RUN-1", k)
			}
			s.Close()
			file := eventFiles(t, dir)[0]
			good, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			appendRaw(t, file, c.tail)

			s, err = Open(dir, testIDs(t))
			if err != nil {
				t.Fatalf("Open with a torn last line: %v", err)
			}
			defer s.Close()
			evs, err := s.Events("RUN-1")
			if err != nil || len(evs) != 3 {
				t.Fatalf("Events = %+v, %v; want the 3 complete events", evs, err)
			}
			for i, want := range []string{"RunStarted", "StepStarted", "StepAccepted"} {
				if evs[i].Seq != i+1 || evs[i].Kind != want {
					t.Errorf("event %d = %+v, want Seq %d %s", i, evs[i], i+1, want)
				}
			}
			// The next event takes the dropped one's place and the file stays whole lines.
			got, err := s.Append("S-1", Event{Run: "RUN-1", Kind: "StepFailed"})
			if err != nil || got.Seq != 4 {
				t.Fatalf("Append after the torn line = %+v, %v; want Seq 4", got, err)
			}
			now, _ := os.ReadFile(file)
			if !bytes.HasPrefix(now, good) {
				t.Errorf("the complete lines changed:\n%s", now)
			}
			if lines := bytes.Count(now, []byte("\n")); lines != 4 {
				t.Errorf("%d lines in the file, want 4 (3 kept, 1 new):\n%s", lines, now)
			}
			if !bytes.HasSuffix(now, []byte("\n")) {
				t.Errorf("the file does not end with a newline:\n%s", now)
			}
			for i, l := range bytes.Split(bytes.TrimSuffix(now, []byte("\n")), []byte("\n")) {
				if !json.Valid(l) {
					t.Errorf("line %d is not JSON: %q", i+1, l)
				}
			}
			// A later open sees all four.
			s.Close()
			s2 := open(t, dir)
			defer s2.Close()
			if evs, _ := s2.Events("RUN-1"); len(evs) != 4 || evs[3].Kind != "StepFailed" {
				t.Errorf("after a second open Events = %+v, want 4 ending in StepFailed", evs)
			}
		})
	}
}

func TestTornOnlyLineLeavesAnEmptyRun(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	appendEv(t, s, "S-1", "RUN-1", "RunStarted")
	s.Close()
	file := eventFiles(t, dir)[0]
	if err := os.WriteFile(file, []byte(`{"seq":1,"run":"RUN-1","ki`), 0o600); err != nil {
		t.Fatal(err)
	}
	s = open(t, dir)
	defer s.Close()
	if evs, err := s.Events("RUN-1"); err != nil || len(evs) != 0 {
		t.Fatalf("Events = %+v, %v; want none", evs, err)
	}
	if got := appendEv(t, s, "S-1", "RUN-1", "RunStarted"); got.Seq != 1 {
		t.Errorf("Seq = %d, want 1", got.Seq)
	}
}

// A torn tail in one slice's file does not disturb another slice's records.
func TestTornTailInOneSliceKeepsOtherSlicesIntact(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	appendEv(t, s, "S-1", "RUN-1", "RunStarted")
	appendEv(t, s, "S-2", "RUN-2", "RunStarted")
	appendEv(t, s, "S-2", "RUN-2", "StepStarted")
	s.Close()
	for _, f := range eventFiles(t, dir) {
		if strings.Contains(f, "S-1") {
			appendRaw(t, f, `{"seq":2,"run":"RUN-1","ki`)
		}
	}
	s = open(t, dir)
	defer s.Close()
	if evs, _ := s.Events("RUN-1"); len(evs) != 1 {
		t.Errorf("RUN-1: %d events, want 1", len(evs))
	}
	if evs, _ := s.Events("RUN-2"); len(evs) != 2 {
		t.Errorf("RUN-2: %d events, want 2", len(evs))
	}
}

// A damaged line that is not the last is not a torn write; dropping it silently
// would hide a damaged record, so Open refuses.
func TestDamagedLineBeforeTheLastIsRefusedNotSkipped(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	appendEv(t, s, "S-1", "RUN-1", "RunStarted")
	s.Close()
	file := eventFiles(t, dir)[0]
	appendRaw(t, file, "{\"seq\":2,\"run\":\"RUN-1\",\"ki\n"+`{"seq":3,"run":"RUN-1","kind":"X"}`+"\n")
	if s, err := Open(dir, testIDs(t)); err == nil {
		s.Close()
		t.Fatal("Open accepted a damaged line in the middle of the record")
	}
}

// A torn last line of the ownership list is the same unacknowledged write.
func TestTornOwnershipLineIsDroppedOnOpen(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	h, err := s.PutArtifact("S-1", []byte("kept"))
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	var owned string
	filepath.WalkDir(filepath.Join(dir, "slices"), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() != "events.jsonl" {
			owned = p
		}
		return nil
	})
	if owned == "" {
		t.Skip("the store keeps no per-slice ownership file; nothing to tear")
	}
	appendRaw(t, owned, validHash[:20])
	s, err = Open(dir, testIDs(t))
	if err != nil {
		t.Fatalf("Open with a torn ownership line: %v", err)
	}
	defer s.Close()
	if b, err := s.Artifact(h); err != nil || string(b) != "kept" {
		t.Errorf("artifact = %q, %v", b, err)
	}
}
