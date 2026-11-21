package steps

// Self-test of HeldOutTestsPass (feature 004, T023): the refusal's message must be a JSON string,
// so an implementation answering {"message":null} fails TestR3_RefusalIsAnError and nothing else.

import (
	"reflect"
	"testing"
)

func TestGraderHeldOutNullMessage(t *testing.T) {
	e, b := testEnv(t), base(t)
	names := heldOutNames(t)

	ref := e.HeldOutTestsPass(dir(t, codeGood(b)))
	if c := requireCounts(t, ref, names); !ref.Passed || c.Passed != 6 || c.Total != 6 {
		t.Fatalf("the reference implementation must pass 6 of 6: %+v", ref)
	}

	planted, err := data.ReadFile("testdata/planted/null-message/internal/app/app.go")
	if err != nil {
		t.Fatal(err)
	}
	r := e.HeldOutTestsPass(dir(t, Overlay(codeGood(b), Files{implFile: string(planted)})))
	c := requireCounts(t, r, names)
	if r.Passed || c.Passed != 5 || !reflect.DeepEqual(failedOf(c), []string{"TestR3_RefusalIsAnError"}) {
		t.Fatalf("want exactly TestR3_RefusalIsAnError failed, got %+v (counts %+v)", r, *c)
	}
}
