package tests

import (
	"errors"
	"fmt"
	"testing"

	"github.com/cucumber/godog"
)

func TestFeatures(t *testing.T) {
	s := godog.TestSuite{
		ScenarioInitializer: func(sc *godog.ScenarioContext) {
			sc.Step(`^I do the thing$`, func() error { return errors.New("the stub answers 501") })
			sc.Step(`^it works$`, func() error { return nil })
			sc.Step(`^I greet "([^"]*)"$`, func(string) error { return nil })
			sc.Step(`^the text:$`, func(*godog.DocString) error { return nil })
		},
		Options: &godog.Options{Format: "pretty", Paths: []string{"../features"}, Strict: true, TestingT: t, Tags: "@run"},
	}
	if s.Run() != 0 {
		t.Fatal("godog failed")
	}
}

var _ = fmt.Sprint
