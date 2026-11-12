package store

// U2: ArtifactCount(sliceID) (int, error) counts the distinct artifacts stored for a slice.

import "testing"

func TestArtifactCount(t *testing.T) {
	st, err := Open(t.TempDir(), testIDs(t))
	if err != nil {
		t.Fatal(err)
	}
	count := func(sl string) int {
		t.Helper()
		n, err := st.ArtifactCount(sl)
		if err != nil {
			t.Fatalf("ArtifactCount(%s): %v", sl, err)
		}
		return n
	}
	put := func(sl, data string) {
		t.Helper()
		if _, err := st.PutArtifact(sl, []byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if n := count("S-1"); n != 0 {
		t.Errorf("empty slice: %d", n)
	}
	put("S-1", "one")
	put("S-1", "two")
	put("S-1", "one") // the same content again
	if n := count("S-1"); n != 2 {
		t.Errorf("two distinct artifacts, counted %d", n)
	}
	put("S-2", "one") // another slice, same bytes
	put("S-2", "three")
	put("S-2", "four")
	if n := count("S-1"); n != 2 {
		t.Errorf("another slice's artifacts changed S-1's count to %d", n)
	}
	if n := count("S-2"); n != 3 {
		t.Errorf("S-2 counted %d, want 3", n)
	}
	if err := st.EraseSlice("S-1"); err != nil {
		t.Fatal(err)
	}
	if n := count("S-1"); n != 0 {
		t.Errorf("after erasure: %d", n)
	}
	if n := count("S-2"); n != 3 {
		t.Errorf("erasing S-1 changed S-2's count to %d", n)
	}
}

func TestArtifactCountRefusesAMalformedSliceID(t *testing.T) {
	st, err := Open(t.TempDir(), testIDs(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ArtifactCount("../x"); err == nil {
		t.Error("a malformed slice ID was accepted")
	}
}
