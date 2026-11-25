package procedures_test

// T030 (feature 010): the request check refuses a name used twice in one object. It scans the
// object's names linearly until the object has 16 members and then keeps them in a map. These
// cases, served like request_regress_test.go's (helpers from request_check_test.go), put the
// duplicate on each side of that switch: both occurrences before it, the second the first member
// the map holds, both after it, the first before it and the second long after it. Nested objects
// and objects in arrays make their own scan. The pinned behaviour is the contract's: the first
// duplicate in body order is refused with the pointer of the name.

import (
	"strconv"
	"strings"
	"testing"
)

// rcBS is a backslash, so that a JSON escape in a source literal below really holds one.
const rcBS = "\\"

// rcNames is the members "m<from>":1, ... "m<to>":1 as a list of JSON members.
func rcNames(from, to int) []string {
	var ms []string
	for k := from; k <= to; k++ {
		ms = append(ms, `"m`+strconv.Itoa(k)+`":1`)
	}
	return ms
}

// rcDupObject is an object of n members named m1..mn, except that member j (1-based) is written
// as name (a JSON string without its quotes) instead of its own.
func rcDupObject(n, j int, name string) string {
	ms := rcNames(1, n)
	ms[j-1] = `"` + name + `":2`
	return "{" + strings.Join(ms, ",") + "}"
}

func rcDupMapCases() []rcCase {
	post := "POST /notes"
	twice := func(p string) string { return rcMember(p, "is named twice") }
	m := func(k int) string { return "m" + strconv.Itoa(k) }
	var cs []rcCase
	// {n members, the duplicate of member i sits at position j}
	for _, c := range []struct{ n, i, j int }{
		{18, 1, 17}, {18, 16, 17}, {18, 17, 18}, // the switch and the members after it
		{18, 15, 16}, {18, 1, 16}, {18, 1, 15}, // the last positions of the linear scan
		{20, 1, 20}, {20, 2, 17}, {20, 16, 20}, {20, 17, 20}, {20, 18, 19},
		{17, 1, 17}, {17, 16, 17}, {16, 1, 16}, {16, 15, 16}, // the object ends at or just after the switch
		{3, 1, 3}, {2, 1, 2}, // small objects
	} {
		name := "a duplicate of member " + strconv.Itoa(c.i) + " at " + strconv.Itoa(c.j) + " of " + strconv.Itoa(c.n)
		cs = append(cs, rcRefuse(name, post, rcDupObject(c.n, c.j, m(c.i)), twice("/"+m(c.i))))
	}
	// the declared name is the one repeated, in a long object
	text := strings.Join(append(append([]string{`"text":"a"`}, rcNames(2, 16)...), `"text":"b"`, `"z":1`), ",")
	cs = append(cs, rcRefuse("text at 1 and 17 of 18", post, "{"+text+"}", twice("/text")))
	// two duplicates: the one whose second occurrence comes first is named
	both := strings.Join(append(rcNames(1, 16), `"m3":2`, `"m1":2`), ",")
	cs = append(cs, rcRefuse("two duplicates, the earlier second occurrence is named", post, "{"+both+"}", twice("/m3")))

	// a duplicate written with a real JSON escape, on the map side
	cs = append(cs,
		rcRefuse("an escaped duplicate at 17 of member 1", post, rcDupObject(18, 17, rcBS+"u006d1"), twice("/m1")),
		rcRefuse("a duplicate of an escaped member 1 at 17", post,
			strings.Replace(rcDupObject(18, 17, "m1"), `"m1":1`, `"`+rcBS+`u006d1":1`, 1), twice("/m1")),
		rcRefuse("an escaped duplicate at 18 of member 17", post, rcDupObject(18, 18, rcBS+"u006d17"), twice("/m17")),
		rcRefuse("an escaped duplicate at 18 of member 16", post, rcDupObject(18, 18, "m"+rcBS+"u0031"+"6"), twice("/m16")),
	)

	// no duplicate: the object passes whatever its size, undeclared members removed
	for _, n := range []int{15, 16, 17, 18, 40} {
		ms := rcNames(1, n)
		ms[n-1] = `"text":"a"` // the declared member last, after the switch
		cs = append(cs, rcPass("no duplicate in "+strconv.Itoa(n)+" members, text last", post, "{"+strings.Join(ms, ",")+"}", `{"text":"a"}`))
	}
	first := append([]string{`"text":"a"`}, rcNames(2, 17)...)
	cs = append(cs, rcPass("no duplicate in 17 members, text first", post, "{"+strings.Join(first, ",")+"}", `{"text":"a"}`))

	// nested objects make their own scan: the pointer names the nesting
	nested := func(inner string) string { return `{"text":"a","extra":` + inner + `}` }
	cs = append(cs,
		rcRefuse("nested: a duplicate at 1 and 17", post, nested(rcDupObject(18, 17, "m1")), twice("/extra/m1")),
		rcRefuse("nested: a duplicate at 16 and 17", post, nested(rcDupObject(18, 17, "m16")), twice("/extra/m16")),
		rcRefuse("nested: a duplicate at 17 and 18", post, nested(rcDupObject(18, 18, "m17")), twice("/extra/m17")),
		rcRefuse("nested in an array: a duplicate at 1 and 17", post,
			nested(`[{"a":1},`+rcDupObject(18, 17, "m1")+`]`), twice("/extra/1/m1")),
		rcRefuse("a long object holding a nested object with a duplicate", post,
			`{`+strings.Join(rcNames(1, 17), ",")+`,"inner":{"k":1,"k":2}}`, twice("/inner/k")),
		// an object the scan has left must not leave its names behind
		rcPass("nested: 17 members, no duplicate", post, nested("{"+strings.Join(rcNames(1, 17), ",")+"}"), `{"text":"a"}`),
		rcPass("siblings of 17 members share no names", post,
			nested(`[{`+strings.Join(rcNames(1, 17), ",")+`},{`+strings.Join(rcNames(1, 17), ",")+`}]`), `{"text":"a"}`),
		rcPass("a nested object reuses its parent's names", post,
			`{`+strings.Join(rcNames(1, 17), ",")+`,"text":"a","inner":{"m1":1,"m17":2}}`, `{"text":"a"}`),
		rcPass("a member named like another, in a different case, is not a duplicate", post,
			`{`+strings.Join(rcNames(1, 17), ",")+`,"M1":2,"text":"a"}`, `{"text":"a"}`),
	)
	return cs
}

func TestRequestDuplicateNamesInLongObjectsServed(t *testing.T) {
	servers := rcServe(t, "notes.yaml", rcNotesMethods)
	rcRunCases(t, servers, rcDupMapCases())
}
