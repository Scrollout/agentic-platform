package captured_test

// Tests of the cap on Propose's candidates (feature 011, task T037, review finding B1: spec
// Clarifications "Session 2026-11-26", FR-001, FR-003; engine contract `captureRun`,
// `x-limits.captured-pattern-parameters-max` = captured.ParametersMax = 32; contract draft
// "Fit and fill", `Propose`). The rule (coordinator's update, commit c6656b8): candidates come in
// Propose's order (names in byte order, then declared values in the contract's order); those not
// of their kind's form, or equal to an earlier one in a case form, are dropped first; then the
// first 32 are kept; then places are found among the kept ones only; a kept one left with no
// place (or over the places limit) is left out too, so a proposal may hold fewer than 32, with
// no error.
//
// The fixtures are built in code from testdata/notes (copy and template) with a replaced
// contract; the expected candidates are computed by hand from the stated order, and the expected
// places by propose_test.go's independent scanner (t3Places), over the kept candidates only.

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/agentic-platform/engine/captured"
)

// capProp is one property of the generated contract's schema `Holder`, with an optional declared
// `maxLength` and an optional extra line (e.g. `default: Ab31`).
type capProp struct{ name, maxLength, extra string }

// capContract is an OpenAPI contract with no paths and one schema, Holder, whose properties come
// in the order given. Its names are the properties' and `holder`; its declared values are the
// maxLengths and extras, in this order.
func capContract(props []capProp) string {
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo:\n  title: Wide\n  version: 1.0.0\npaths: {}\ncomponents:\n  schemas:\n    Holder:\n      type: object\n      properties:\n")
	for _, p := range props {
		fmt.Fprintf(&b, "        %s:\n          type: string\n", p.name)
		if p.maxLength != "" {
			fmt.Fprintf(&b, "          maxLength: %s\n", p.maxLength)
		}
		if p.extra != "" {
			fmt.Fprintf(&b, "          %s\n", p.extra)
		}
	}
	return b.String()
}

// capNames is the property names ab01 .. ab<n> in descending order (so the document's order is
// not the byte order), the first of them carrying the given maxLengths in turn.
func capNames(n int, maxLengths ...string) []capProp {
	var out []capProp
	for i := n; i >= 1; i-- {
		p := capProp{name: fmt.Sprintf("ab%02d", i)}
		if k := n - i; k < len(maxLengths) {
			p.maxLength = maxLengths[k]
		}
		out = append(out, p)
	}
	return out
}

// capWant is the specs of the first 32 candidates: the given names (with `holder` added) in byte
// order, then the declared numbers in the order given, as the stated order says; the left-out
// rest is returned apart. Names given must all be of the name form.
func capWant(props []capProp, declared ...string) (kept, out []t3Spec) {
	names := []string{"holder"}
	for _, p := range props {
		names = append(names, p.name)
	}
	sort.Strings(names)
	var all []t3Spec
	for _, n := range names {
		all = append(all, t3Spec{n, captured.KindName, n})
	}
	for _, d := range declared {
		all = append(all, t3Spec{"", captured.KindNumber, d})
	}
	if len(all) <= captured.ParametersMax {
		return all, nil
	}
	return all[:captured.ParametersMax], all[captured.ParametersMax:]
}

func t37Values(ps []captured.Parameter) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Value)
	}
	return out
}

// t37Check proposes for contract and judges the result against the candidates kept and left out:
// exactly the kept, in order, each with the places the scanner finds among the kept only.
func t37Check(t *testing.T, contract string, kept, out []t3Spec) {
	t.Helper()
	f := t3Load(t, "notes")
	src := f.withContract(t, contract)
	got, err := captured.Propose(src, f.template)
	if err != nil {
		t.Fatal(err)
	}
	var wantValues []string
	for _, s := range kept {
		wantValues = append(wantValues, s.Value)
	}
	gotValues := t37Values(got)
	if !slices.Equal(gotValues, wantValues) {
		t.Fatalf("Propose gave %d candidates %v,\nwant %d in order, %v", len(got), clip(gotValues), len(kept), clip(wantValues))
	}
	for _, s := range out {
		if t3Find(got, s.Value) != nil {
			t.Errorf("the left-out candidate %q is proposed", s.Value)
		}
	}
	want := t3Places(src, f.template, kept)
	for i, p := range got {
		if len(p.Places) == 0 {
			t.Errorf("%q has no place", p.Value)
		}
		t3SamePlaces(t, p.Value, p.Places, want[i])
		if p.Origin != captured.OriginProposed {
			t.Errorf("%q origin %q, want proposed", p.Value, p.Origin)
		}
	}
	if len(got) > captured.ParametersMax {
		t.Errorf("%d candidates, more than %d", len(got), captured.ParametersMax)
	}
	// What is proposed is what Check accepts.
	if err := captured.Check(src, f.template, got); err != nil {
		t.Errorf("Check refuses the proposal: %v", err)
	}
	again, err := captured.Propose(src, f.template)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(t37Values(again), gotValues) {
		t.Errorf("a second Propose gave another list: %v, then %v", clip(gotValues), clip(t37Values(again)))
	}
}

// T037, FR-003, Clarifications 2026-11-26: 41 name candidates (ab01 .. ab40 and `holder`, written
// in descending order in the document) give the first 32 in byte order. By hand: byte order is
// ab01 .. ab40, then holder, so ab01 .. ab32 are kept and ab33 .. ab40 and holder are left out.
func TestProposeKeepsTheFirst32NamesInByteOrder(t *testing.T) {
	props := capNames(40)
	kept, out := capWant(props)
	if len(kept) != 32 || kept[0].Value != "ab01" || kept[31].Value != "ab32" || len(out) != 9 {
		t.Fatalf("the hand-computed expectation is wrong: %v / %v", kept, out)
	}
	t37Check(t, capContract(props), kept, out)
}

// T037, FR-003: the boundary. 31 properties and `holder` are exactly 32 candidates: all are
// proposed. 32 properties and `holder` are 33: `holder`, last in byte order, is left out. 30 and
// `holder` are 31: nothing is cut.
func TestProposeCapBoundary31And32And33Candidates(t *testing.T) {
	for _, c := range []struct {
		props      int
		keptN, cut int
	}{{30, 31, 0}, {31, 32, 0}, {32, 32, 1}} {
		t.Run(fmt.Sprintf("%d candidates", c.props+1), func(t *testing.T) {
			props := capNames(c.props)
			kept, out := capWant(props)
			if len(kept) != c.keptN || len(out) != c.cut {
				t.Fatalf("the hand-computed expectation is wrong: kept %d left out %d", len(kept), len(out))
			}
			if c.cut == 1 && out[0].Value != "holder" {
				t.Fatalf("the hand-computed left-out candidate is %q, want holder", out[0].Value)
			}
			t37Check(t, capContract(props), kept, out)
		})
	}
}

// T037, FR-003: declared values come after the names, in the contract's order, and are cut after
// them. 30 properties and `holder` are 31 names; the maxLengths 9003, 9001, 9002 follow in the
// document's order (ab30, ab29 and ab28 are written first, in that order): the 32nd candidate is
// 9003; 9001 and 9002 are left out (neither the numeric nor the reverse order).
func TestProposeCapCutsDeclaredValuesInContractOrder(t *testing.T) {
	props := capNames(30, "9003", "9001", "9002")
	kept, out := capWant(props, "9003", "9001", "9002")
	if len(kept) != 32 || kept[31].Value != "9003" || len(out) != 2 || out[0].Value != "9001" || out[1].Value != "9002" {
		t.Fatalf("the hand-computed expectation is wrong: %v / %v", kept, out)
	}
	t37Check(t, capContract(props), kept, out)
}

// T037, FR-003: the boundary between names and declared values. 31 names and one declared value
// are exactly 32: all are kept. 32 names (31 properties and `holder`) and one declared value are
// 33: the declared value is left out, though it has places, because the names come first.
func TestProposeCapNamesBeforeDeclaredValues(t *testing.T) {
	t.Run("31 names and 1 declared value", func(t *testing.T) {
		props := capNames(30, "9001")
		kept, out := capWant(props, "9001")
		if len(kept) != 32 || len(out) != 0 || kept[31].Value != "9001" {
			t.Fatalf("the hand-computed expectation is wrong: %v / %v", kept, out)
		}
		t37Check(t, capContract(props), kept, out)
	})
	t.Run("32 names and 1 declared value", func(t *testing.T) {
		props := capNames(31, "9001")
		kept, out := capWant(props, "9001")
		if len(kept) != 32 || len(out) != 1 || out[0].Value != "9001" || kept[31].Value != "holder" {
			t.Fatalf("the hand-computed expectation is wrong: %v / %v", kept, out)
		}
		t37Check(t, capContract(props), kept, out)
	})
}

// T037, FR-003: the places are found among the kept candidates only (a left-out long name frees
// places for a kept shorter one). Candidates: ab01 .. ab30, holder, zk (the 32nd in byte order)
// and zkx (the 33rd, left out). `zk` occurs in its own key `zk:` and inside the key `zkx:`. Were
// zkx kept, it would take its key whole (longest first) and zk would keep only the first; with
// zkx left out, zk keeps both places, both in the contract part, on line 1.
func TestProposeLeftOutCandidateTakesNoPlaceFromAKeptOne(t *testing.T) {
	props := append(capNames(30), capProp{name: "zkx"}, capProp{name: "zk"})
	contract := capContract(props)
	kept, out := capWant(props)
	if len(kept) != 32 || kept[31].Value != "zk" || len(out) != 1 || out[0].Value != "zkx" {
		t.Fatalf("the hand-computed expectation is wrong: %v / %v", kept, out)
	}
	t37Check(t, contract, kept, out)

	f := t3Load(t, "notes")
	src := f.withContract(t, contract)
	got, err := captured.Propose(src, f.template)
	if err != nil {
		t.Fatal(err)
	}
	zk := t3Find(got, "zk")
	if zk == nil {
		t.Fatalf("zk is not proposed among %d candidates", len(got))
	}
	// By hand: the part's text reads `zkx:` at one of the two places and `zk:` at the other.
	text := string(src.Contract)
	var inLong, inOwn int
	for _, p := range zk.Places {
		if p.Part != "slice/contract" || p.Line != 1 || p.Column < 1 {
			t.Fatalf("place %+v, want one in slice/contract on line 1", p)
		}
		switch rest := text[p.Column-1:]; {
		case strings.HasPrefix(rest, "zkx:"):
			inLong++
		case strings.HasPrefix(rest, "zk:"):
			inOwn++
		}
	}
	if len(zk.Places) != 2 || inLong != 1 || inOwn != 1 {
		t.Errorf("zk places %+v: %d inside zkx, %d in its own key, want 2 places, one in each", zk.Places, inLong, inOwn)
	}
}

// T037, FR-003 (coordinator's update): a candidate refused by form, or equal to an earlier one in
// a case form, is dropped before the cut, so it does not take one of the 32. By hand: the
// properties are a1 (one letter: not of the name form, and first in byte order), ab01 .. ab31 and
// the declared string `Ab31` (the default of ab31: equal to the name ab31 in a case form, and
// later in order). With `holder` that is 34 candidates; a1 and `Ab31` are dropped, leaving
// exactly 32 names (ab01 .. ab31 and holder), all proposed. A cut before the drop would lose
// holder (the 32nd after a1) or the last name.
func TestProposeDropsRefusedCandidatesBeforeTheCut(t *testing.T) {
	props := capNames(31)
	props[0].extra = "default: Ab31" // props[0] is ab31
	props = append(props, capProp{name: "a1"})
	contract := capContract(props)
	var kept []t3Spec
	for i := 1; i <= 31; i++ {
		n := fmt.Sprintf("ab%02d", i)
		kept = append(kept, t3Spec{n, captured.KindName, n})
	}
	kept = append(kept, t3Spec{"holder", captured.KindName, "holder"})
	if len(kept) != 32 {
		t.Fatalf("the hand-computed expectation is wrong: %d", len(kept))
	}
	t37Check(t, contract, kept, []t3Spec{{"a1", captured.KindName, "a1"}, {"ab31-default", captured.KindString, "Ab31"}})
}

// T037, FR-003 (coordinator's update): a kept candidate over the places limit is left out too,
// with no error, so the proposal holds fewer than 32, and a candidate that was cut is not
// brought back in its place. Candidates: ab01 .. ab29, holder, qq, zy and zz (33): qq is the 31st
// in byte order, zy the 32nd, zz the 33rd (cut). The contract ends in a comment with `qq` 10001
// times (the key `qq:` makes 10002, more than PlacesMax = 10000), so qq is left out: 31 are
// proposed (ab01 .. ab29, holder, zy). A kept candidate with no place at all cannot be built from
// a contract: its own key is a place (see the report).
func TestProposeLeavesOutAKeptCandidateOverThePlacesLimit(t *testing.T) {
	props := append(capNames(29), capProp{name: "zz"}, capProp{name: "zy"}, capProp{name: "qq"})
	contract := capContract(props) + "# " + strings.Repeat("qq ", captured.PlacesMax+1) + "\n"
	names, _ := capWant(props)
	if len(names) != 32 || names[30].Value != "qq" || names[31].Value != "zy" {
		t.Fatalf("the hand-computed expectation is wrong: %v", names)
	}
	var kept []t3Spec
	for _, s := range names {
		if s.Value != "qq" {
			kept = append(kept, s)
		}
	}
	if len(kept) != 31 {
		t.Fatalf("the hand-computed expectation is wrong: %d", len(kept))
	}
	t37Check(t, contract, kept, []t3Spec{{"qq", captured.KindName, "qq"}, {"zz", captured.KindName, "zz"}})
}
