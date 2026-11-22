package method_test

// T004 (feature 006): the Go side of the method contract (src/engine/method/README.md, "The Go
// side"). Written before the code (T005) by a test author. Interface assumed:
//
//	type ID                                  // holds only a valid identity; zero value = none
//	func (ID) String() string                // 64 lowercase hex digits, "" for the zero ID
//	func (ID) IsZero() bool
//	func Parse(s string) (ID, error)         // 64 lowercase hex digits, one trailing "\n" trimmed
//	func Current() (ID, error)               // the identity embedded from method.sha256
//
// An error names what was found: the quoted input (strconv.Quote form, as %q prints it) or the
// length it has. Edge cases checked: unicode digits (Arabic-Indic, fullwidth), a NUL byte, a
// 64-character string whose byte length is longer, a lone "\n", whitespace other than a space.

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/agentic-platform/engine/method"
)

const valid = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestParseAccepts(t *testing.T) {
	for name, in := range map[string]string{
		"without newline": valid,
		"with newline":    valid + "\n",
		"all zeros":       strings.Repeat("0", 64),
		"all f":           strings.Repeat("f", 64),
		"all letters":     strings.Repeat("abcdef", 10) + "abcd",
	} {
		t.Run(name, func(t *testing.T) {
			id, err := method.Parse(in)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", in, err)
			}
			if id.IsZero() {
				t.Fatalf("Parse(%q) gave the zero ID", in)
			}
			if got, want := id.String(), strings.TrimSuffix(in, "\n"); got != want {
				t.Fatalf("String() = %q, want %q", got, want)
			}
		})
	}
}

func TestParseNewlineAndBareGiveSameID(t *testing.T) {
	a, err := method.Parse(valid)
	if err != nil {
		t.Fatal(err)
	}
	b, err := method.Parse(valid + "\n")
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("IDs differ: %q vs %q", a.String(), b.String())
	}
}

func TestParseRejects(t *testing.T) {
	cases := map[string]string{
		"empty":                    "",
		"only newline":             "\n",
		"63 digits":                valid[:63],
		"63 digits and newline":    valid[:63] + "\n",
		"65 digits":                valid + "0",
		"65 digits and newline":    valid + "0\n",
		"upper case":               strings.ToUpper(valid),
		"one upper case digit":     "A" + valid[1:],
		"non-hex g":                "g" + valid[1:],
		"non-hex last":             valid[:63] + "z",
		"inner space":              valid[:32] + " " + valid[33:],
		"leading space":            " " + valid[:63],
		"trailing space":           valid[:63] + " ",
		"trailing space, newline":  valid[:63] + " \n",
		"two trailing newlines":    valid + "\n\n",
		"crlf":                     valid + "\r\n",
		"lone carriage return":     valid + "\r",
		"leading newline":          "\n" + valid[:63],
		"inner newline":            valid[:32] + "\n" + valid[33:],
		"tab":                      valid[:63] + "\t",
		"0x prefix":                "0x" + valid[:62],
		"NUL byte":                 valid[:63] + "\x00",
		"NUL byte, 64 with prefix": "\x00" + valid[:63],
		"arabic-indic digit":       valid[:63-1] + "٣" + "0", // 64 runes, 65 bytes
		"fullwidth digit":          valid[:62] + "１",         // 63 ASCII + 1 rune
		"fullwidth, 64 runes":      valid[:63] + "１",
		"hex-with-dashes":          strings.Repeat("0123-", 13)[:64],
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			id, err := method.Parse(in)
			if err == nil {
				t.Fatalf("Parse(%q) accepted it as %q", in, id.String())
			}
			if !id.IsZero() || id.String() != "" {
				t.Fatalf("Parse(%q) rejected but returned non-zero ID %q", in, id.String())
			}
			trimmed := strings.TrimSuffix(in, "\n")
			msg := err.Error()
			names := strings.Contains(msg, strconv.Quote(in)) ||
				strings.Contains(msg, strconv.Quote(trimmed)) ||
				strings.Contains(msg, strconv.Itoa(len(in))) ||
				strings.Contains(msg, strconv.Itoa(len(trimmed)))
			if !names {
				t.Fatalf("Parse(%q) error %q names neither the input nor its length", in, msg)
			}
		})
	}
}

// A wrong length is reported as the length found, so a reader sees 63 or 65, not only "invalid".
func TestParseLengthErrorsNameTheLength(t *testing.T) {
	for _, n := range []int{0, 1, 63, 65, 128} {
		in := strings.Repeat("a", n)
		_, err := method.Parse(in)
		if err == nil {
			t.Fatalf("length %d accepted", n)
		}
		if !strings.Contains(err.Error(), strconv.Itoa(n)) && !strings.Contains(err.Error(), strconv.Quote(in)) {
			t.Fatalf("length %d: error %q names neither the length nor the input", n, err)
		}
	}
}

func TestZeroID(t *testing.T) {
	var z method.ID
	if !z.IsZero() {
		t.Fatal("zero ID: IsZero() is false")
	}
	if z.String() != "" {
		t.Fatalf("zero ID: String() = %q, want empty", z.String())
	}
}

func TestParsedIDIsNotZeroAndRoundTrips(t *testing.T) {
	id, err := method.Parse(valid)
	if err != nil {
		t.Fatal(err)
	}
	if id.IsZero() {
		t.Fatal("parsed ID is zero")
	}
	again, err := method.Parse(id.String())
	if err != nil {
		t.Fatalf("String() output does not parse: %v", err)
	}
	if again != id || again.String() != valid {
		t.Fatalf("round trip changed the ID: %q", again.String())
	}
}

func TestCurrentEqualsParseOfFile(t *testing.T) {
	raw, err := os.ReadFile("method.sha256")
	if err != nil {
		t.Fatalf("read method.sha256: %v", err)
	}
	want, err := method.Parse(string(raw))
	if err != nil {
		t.Fatalf("method.sha256 does not parse: %v", err)
	}
	got, err := method.Current()
	if err != nil {
		t.Fatalf("Current() error: %v", err)
	}
	if got.IsZero() {
		t.Fatal("Current() is the zero ID")
	}
	if got != want || got.String() != want.String() {
		t.Fatalf("Current() = %q, want %q", got.String(), want.String())
	}
}

func TestCurrentIsStable(t *testing.T) {
	a, err := method.Current()
	if err != nil {
		t.Fatal(err)
	}
	b, err := method.Current()
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("Current() changed between calls: %q then %q", a.String(), b.String())
	}
}
