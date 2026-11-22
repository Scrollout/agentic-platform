// Package method gives the engine the identity of the platform's method: the same for the same
// source, different when any declared part of the method differs (README.md).
package method

import (
	_ "embed"
	"fmt"
	"strings"
	"sync"
)

// size is the length of an identity: a SHA-256 in hex.
const size = 64

// ID is a method identity, 64 lowercase hex digits. The zero ID means no identity.
type ID struct{ hex string }

// String gives the identity's 64 lowercase hex digits, or "" for the zero ID.
func (id ID) String() string { return id.hex }

// IsZero says whether id is the zero ID, no identity.
func (id ID) IsZero() bool { return id.hex == "" }

// Parse accepts exactly 64 lowercase hex digits after one trailing newline is trimmed; anything
// else is an error naming what was found, with the zero ID.
func Parse(s string) (ID, error) {
	t := strings.TrimSuffix(s, "\n")
	if len(t) != size {
		return ID{}, fmt.Errorf("a method identity is %d lowercase hex digits, found %d characters: %q", size, len(t), s)
	}
	for i := 0; i < len(t); i++ {
		c := t[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return ID{}, fmt.Errorf("a method identity is %d lowercase hex digits, found %q at position %d in %q", size, c, i+1, s)
		}
	}
	return ID{hex: t}, nil
}

//go:embed method.sha256
var embedded string

var current = sync.OnceValues(func() (ID, error) { return Parse(embedded) })

// Current gives the identity embedded from method.sha256, parsed once: the same as Parse of that
// file's bytes.
func Current() (ID, error) { return current() }
