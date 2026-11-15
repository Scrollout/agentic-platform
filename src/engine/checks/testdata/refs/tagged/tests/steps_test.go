package tests

import (
	"errors"
	"testing"

	"github.com/cucumber/godog"
)

func TestFeatures(t *testing.T) {
	s := godog.TestSuite{
		ScenarioInitializer: func(sc *godog.ScenarioContext) {
			sc.Step(`^I do the thing$`, func() error { return errors.New("the stub answers 501") })
			sc.Step(`^it works$`, func() error { return nil })
			sc.Step(`^it breaks$`, func() error { return errors.New("broken") })
			sc.Step(`^I greet "([^"]*)"$`, func(string) error { return nil })
		},
		Options: &godog.Options{Format: "pretty", Paths: []string{"../features"}, Strict: true, TestingT: t},
	}
	if s.Run() != 0 {
		t.Fatal("godog failed")
	}
}
