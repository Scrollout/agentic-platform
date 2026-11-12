package api

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"

	"github.com/agentic-platform/engine/api/gen"
)

// Page tokens are opaque to clients and bound to the list that issued them: a token is
// base64url(list, 0, place, mac), where mac is the first macSize bytes of HMAC-SHA256 under a
// key made when the handler is, over the list and the place. A token another list issued, one
// changed by a client, or one from before a restart fails the check. The place is the list's
// own: an offset for the operations list, which does not change while the engine runs, and the
// number of the last slice on the page for listSlices, so slices created or deleted between
// pages are neither repeated nor make another skipped.
const macSize = 16

// Lists that issue page tokens.
const (
	operationsList = "operations"
	slicesList     = "slices"
)

// pageKeys signs and checks page tokens.
type pageKeys struct{ key []byte }

func newPageKeys() pageKeys {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic("api: no randomness for page tokens: " + err.Error()) // crypto/rand does not fail on supported systems
	}
	return pageKeys{key: key}
}

func (k pageKeys) mac(list, place string) []byte {
	m := hmac.New(sha256.New, k.key)
	m.Write([]byte(list + "\x00" + place))
	return m.Sum(nil)[:macSize]
}

// issue returns the token for place in list.
func (k pageKeys) issue(list, place string) string {
	return base64.RawURLEncoding.EncodeToString(append([]byte(list+"\x00"+place), k.mac(list, place)...))
}

// place returns the place a token of list names, or false for a token this list did not issue.
func (k pageKeys) place(list, token string) (string, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) <= macSize {
		return "", false
	}
	body, mac := raw[:len(raw)-macSize], raw[len(raw)-macSize:]
	l, place, ok := strings.Cut(string(body), "\x00")
	if !ok || l != list || !hmac.Equal(mac, k.mac(l, place)) {
		return "", false
	}
	return place, true
}

// offsetPage returns the bounds of the page a token of list asks for among total items that do
// not change while the engine runs, and the token of the page after it (nil on the last page);
// ok is false for a token this list did not issue, or one past the end.
func (k pageKeys) offsetPage(list string, token *string, total int) (from, to int, next *string, ok bool) {
	if token != nil {
		place, issued := k.place(list, *token)
		n, err := strconv.Atoi(place)
		if !issued || err != nil || n <= 0 || n >= total {
			return 0, 0, nil, false
		}
		from = n
	}
	to = min(from+pageSize, total)
	if to < total {
		t := k.issue(list, strconv.Itoa(to))
		next = &t
	}
	return from, to, next, true
}

// keysetPage returns the bounds of the page a token of list asks for among items ordered by a
// number that falls from one item to the next (numbers[i] is the number of item i), and the token
// of the page after it, naming the last item shown. The page starts at the first item after the
// one the token names, whether or not that item still exists.
func (k pageKeys) keysetPage(list string, token *string, numbers []int) (from, to int, next *string, ok bool) {
	if token != nil {
		place, issued := k.place(list, *token)
		n, err := strconv.Atoi(place)
		if !issued || err != nil || n <= 0 {
			return 0, 0, nil, false
		}
		for from < len(numbers) && numbers[from] >= n {
			from++
		}
	}
	to = min(from+pageSize, len(numbers))
	if to < len(numbers) {
		t := k.issue(list, strconv.Itoa(numbers[to-1]))
		next = &t
	}
	return from, to, next, true
}

func badPageToken() gen.BadRequestApplicationProblemPlusJSONResponse {
	return gen.BadRequestApplicationProblemPlusJSONResponse(newProblem(http.StatusBadRequest, gen.ErrorCodeInvalidRequest,
		"page_token was not issued by this list of this service"))
}
