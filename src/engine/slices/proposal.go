package slices

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// AnswerFault says why an analyst's answer cannot be used as a proposal although it is one JSON
// object. Its reason is fit to send back to the analyst as feedback.
type AnswerFault struct {
	Reason string
}

// Error returns the fault's reason.
func (f *AnswerFault) Error() string { return f.Reason }

// ParseProposal reads an analyst's answer as a proposal. An answer that is not exactly one JSON
// object (whitespace around it aside) gives a plain error. An object at any depth that holds a key
// twice, without regard to letter case, gives an *AnswerFault naming the second spelling of the
// key and the JSON Pointer (RFC 6901) of its object, each cut to 64 characters and quoted.
func ParseProposal(answer []byte) (Proposal, error) {
	if !json.Valid(answer) || !bytes.HasPrefix(bytes.TrimLeft(answer, " \t\r\n"), []byte("{")) {
		return Proposal{}, errors.New("the answer is not one JSON object")
	}
	if err := walkKeys(json.NewDecoder(bytes.NewReader(answer)), ""); err != nil {
		return Proposal{}, err
	}
	var p Proposal
	if err := json.Unmarshal(answer, &p); err != nil {
		return Proposal{}, err
	}
	return p, nil
}

// pointerEscape escapes a key as a JSON Pointer reference token (RFC 6901).
var pointerEscape = strings.NewReplacer("~", "~0", "/", "~1")

// walkKeys reads one JSON value from dec, whose JSON Pointer is pointer, and returns an
// *AnswerFault for the first object in it that holds a key twice.
func walkKeys(dec *json.Decoder, pointer string) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	switch tok {
	case json.Delim('{'):
		seen := map[string]bool{}
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return err
			}
			key, _ := keyTok.(string)
			folded := foldKey(key)
			if seen[folded] {
				at := pointer
				if at == "" {
					at = "/"
				}
				return &AnswerFault{Reason: fmt.Sprintf("the answer holds the key %s twice at %s", quoteCut(key), quoteCut(at))}
			}
			seen[folded] = true
			if err := walkKeys(dec, pointer+"/"+pointerEscape.Replace(key)); err != nil {
				return err
			}
		}
		_, err = dec.Token() // '}'
		return err
	case json.Delim('['):
		for i := 0; dec.More(); i++ {
			if err := walkKeys(dec, pointer+"/"+strconv.Itoa(i)); err != nil {
				return err
			}
		}
		_, err = dec.Token() // ']'
		return err
	}
	return nil
}

// foldKey maps s to one spelling shared by every string strings.EqualFold holds equal to it:
// each rune becomes the smallest rune of its simple case-folding orbit.
func foldKey(s string) string {
	return strings.Map(func(r rune) rune {
		least := r
		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			least = min(least, f)
		}
		return least
	}, s)
}

// quoteCut cuts s to its first 64 runes and quotes it as a Go string literal.
func quoteCut(s string) string {
	if r := []rune(s); len(r) > 64 {
		s = string(r[:64])
	}
	return strconv.Quote(s)
}
