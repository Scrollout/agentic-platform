package tests

import (
	"flag"
	"fmt"
	"testing"

	"github.com/cucumber/godog"

	"example.com/p/internal/greet"
)

var opts = godog.Options{Format: "pretty", NoColors: true, Paths: []string{"../features"}, Strict: true}

func init() { godog.BindFlags("godog.", flag.CommandLine, &opts) }

type world struct{ got string }

func (w *world) ask(name string) { w.got = greet.Greeting(name) }

func (w *world) text(want string) error {
	if w.got != want {
		return fmt.Errorf("internal/greet/greet.go:7: the greeting is %q, want %q", w.got, want)
	}
	return nil
}

func (w *world) notEmpty() error {
	if w.got == "" {
		return fmt.Errorf("internal/greet/greet.go:7: the greeting is empty")
	}
	return nil
}

func TestFeatures(t *testing.T) {
	o := opts
	o.TestingT = t
	s := godog.TestSuite{
		ScenarioInitializer: func(sc *godog.ScenarioContext) {
			w := &world{}
			sc.Step(`^I ask for the greeting for "([^"]*)"$`, w.ask)
			sc.Step(`^the text is "([^"]*)"$`, w.text)
			sc.Step(`^the text is not empty$`, w.notEmpty)
		},
		Options: &o,
	}
	if s.Run() != 0 {
		t.Fatal("godog failed")
	}
}
