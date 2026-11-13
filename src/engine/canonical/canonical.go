// Package canonical writes RFC 8785 (JSON Canonicalization Scheme) canonical JSON for the values a
// slice's parts hold, and hashes it: the hashes approvals store (research D17, D25). It is the one
// owner of those hashes; the slices service and the step checks both use it.
package canonical

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// The part hashes: the SHA-256, in lowercase hex, of the RFC 8785 (JSON Canonicalization Scheme)
// form of {goals}, {rules, examples, terms} and {contract} (slice-defs.schema.json goals_part,
// rules_part and contract_part; research D17), built in slices and, for the contract, ContractHash.
//
// Every value in a part is a string, an array or an object (the schemas give no part a number,
// boolean or null), so the canonical form is written straight from the part's records. RFC 8785
// then asks for: no whitespace (§3.2.1); object members sorted by their names as arrays of UTF-16
// code units (§3.2.3); strings with only the escapes of §3.2.2.2 (\" \\ \b \t \n \f \r, \u00xx in
// lowercase hex for the other controls) and every other character, < > & and U+2028 included, as
// itself in UTF-8; and only valid Unicode (I-JSON, §3.1). Number serialisation (§3.2.2.3) is not
// needed because no part holds a number; a value of any other type is refused, not guessed at.

// Object and Array are the canonical form's object and array; a value is built from them and
// strings.
type (
	Object map[string]any
	Array  []any
)

// ContractHash is the hash an approval of a slice's contract part stores: Hash of
// {"contract": text}. The engine's step check ContractMatchesApproval compares a project's
// api/openapi.yaml with it, so both use this one function.
func ContractHash(text string) (string, error) {
	return Hash(Object{"contract": text})
}

// Hash is the SHA-256, in lowercase hex, of v's RFC 8785 canonical form.
func Hash(v any) (string, error) {
	var b strings.Builder
	if err := writeCanonical(&b, v); err != nil {
		return "", err
	}
	h := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(h[:]), nil
}

func writeCanonical(b *strings.Builder, v any) error {
	switch v := v.(type) {
	case string:
		return writeCanonicalString(b, v)
	case Array:
		b.WriteByte('[')
		for i, e := range v {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := writeCanonical(b, e); err != nil {
				return err
			}
		}
		b.WriteByte(']')
		return nil
	case Object:
		names := make([]string, 0, len(v))
		for k := range v {
			names = append(names, k)
		}
		slices.SortFunc(names, compareUTF16)
		b.WriteByte('{')
		for i, k := range names {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := writeCanonicalString(b, k); err != nil {
				return err
			}
			b.WriteByte(':')
			if err := writeCanonical(b, v[k]); err != nil {
				return err
			}
		}
		b.WriteByte('}')
		return nil
	default:
		return fmt.Errorf("canonical JSON: a %T is not a value any slice part holds", v)
	}
}

func writeCanonicalString(b *strings.Builder, s string) error {
	if !utf8.ValidString(s) {
		return fmt.Errorf("canonical JSON: the text %q is not valid UTF-8", s)
	}
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 {
				fmt.Fprintf(b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return nil
}

// compareUTF16 orders two names by their UTF-16 code units (RFC 8785 §3.2.3), which differs from
// Go's byte order for characters above U+FFFF against U+E000 to U+FFFF.
func compareUTF16(a, b string) int {
	return slices.Compare(utf16.Encode([]rune(a)), utf16.Encode([]rune(b)))
}
