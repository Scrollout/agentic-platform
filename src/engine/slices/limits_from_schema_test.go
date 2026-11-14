// Tests for task T071 (Constitution, Engineering: Single owner for rules). They fail until the
// service takes the free-answer length limit (slice-defs.schema.json `answer` text maxLength) and
// the question limit (slice.schema.json `questions` maxItems) from the loaded schemas instead of
// the Go literals maxAnswerText and maxQuestions.
//
// Seam: the one T017a used for ID patterns. The contract's schema files are loaded through
// slice.LoadSchemasFS from an in-memory copy in which only those two numbers are changed, and the
// *slice.Schemas is passed to slices.New as Config.Schemas. Nothing else in the copy changes; in
// particular slice-proposal.schema.json still allows 10 new questions per proposal.
package slices_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/agentic-platform/engine/slices"
	"github.com/agentic-platform/slice"
)

const lfsDir = "../api/preview/2026-11-12-preview"

// lfsSchemas loads the contract's schemas with the free-answer maxLength set to maxText and the
// slice's questions maxItems set to maxQs.
func lfsSchemas(t *testing.T, maxText, maxQs int) *slice.Schemas {
	t.Helper()
	m := fstest.MapFS{}
	ents, err := os.ReadDir(lfsDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if !strings.HasSuffix(e.Name(), ".schema.json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(lfsDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		swap := func(old, new string) {
			if n := strings.Count(s, old); n != 1 {
				t.Fatalf("%s: %q occurs %d times, want once", e.Name(), old, n)
			}
			s = strings.Replace(s, old, new, 1)
		}
		switch e.Name() {
		case "slice-defs.schema.json":
			swap(`"maxLength": 200,`, fmt.Sprintf(`"maxLength": %d,`, maxText))
		case "slice.schema.json":
			swap(`"maxItems": 10,`, fmt.Sprintf(`"maxItems": %d,`, maxQs))
		}
		m[e.Name()] = &fstest.MapFile{Data: []byte(s)}
	}
	sc, err := slice.LoadSchemasFS(m)
	if err != nil {
		t.Fatal(err)
	}
	return sc
}

func lfsService(t *testing.T, sc *slice.Schemas, a slices.Analyst) *slices.Service {
	t.Helper()
	pattern, err := os.ReadFile("../../catalogue/api-feature-go/pattern.yaml")
	if err != nil {
		t.Fatal(err)
	}
	s, err := slices.New(slices.Config{Schemas: sc, Analyst: a, Diff: &fakeDiff{}, Pattern: pattern, Lint: &fakeLint{}})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func lfsQuestions(t *testing.T, n int) slices.Proposal {
	p := notesProposal(t)
	for i := 0; i < n; i++ {
		p.Questions = append(p.Questions, slices.QuestionDraft{About: "G-1", Text: fmt.Sprintf("Question %d?", i+1)})
	}
	return p
}

func TestAnswerTextLimitComesFromTheSchema(t *testing.T) {
	ctx := context.Background()
	sc := lfsSchemas(t, 50, 10)
	for _, c := range []struct {
		name   string
		length int
		refuse bool
	}{{"60 characters", 60, true}, {"51 characters", 51, true}, {"50 characters", 50, false}} {
		t.Run(c.name, func(t *testing.T) {
			a := &fakeAnalyst{results: []analystResult{{p: lfsQuestions(t, 1)}, {p: notesProposal(t)}}}
			s := lfsService(t, sc, a)
			sl := create(t, s, "Let me keep notes.")
			if len(sl.Questions) != 1 {
				t.Fatalf("setup: %d questions", len(sl.Questions))
			}
			ans := []slices.Answer{{Question: sl.Questions[0].ID, Text: strings.Repeat("a", c.length)}}
			checkErr := s.CheckAnswers(sl.ID, ans)
			_, err := s.Answer(ctx, sl.ID, ans)
			if c.refuse {
				if code(t, checkErr) != "InvalidRequest" {
					t.Errorf("CheckAnswers: want InvalidRequest for %d characters over a schema limit of 50, got %v", c.length, checkErr)
				}
				if code(t, err) != "InvalidRequest" {
					t.Errorf("Answer: want InvalidRequest for %d characters over a schema limit of 50, got %v", c.length, err)
				}
				if a.calls != 1 {
					t.Errorf("a refused answer ran the analyst (calls %d)", a.calls)
				}
				return
			}
			if checkErr != nil || err != nil {
				t.Errorf("an answer at the schema limit was refused: CheckAnswers %v, Answer %v", checkErr, err)
			}
		})
	}
}

func TestQuestionLimitComesFromTheSchema(t *testing.T) {
	sc := lfsSchemas(t, 200, 3)
	t.Run("4 questions on create", func(t *testing.T) {
		s := lfsService(t, sc, &fakeAnalyst{results: []analystResult{{p: lfsQuestions(t, 4)}}})
		_, err := s.Create(context.Background(), "Let me keep notes.")
		if err == nil {
			t.Fatal("4 questions were accepted over a schema limit of 3")
		}
		if code(t, err) != "TooManyQuestions" {
			t.Errorf("want TooManyQuestions, got %v", err)
		}
		if _, err := s.Get("S-1"); err == nil {
			t.Error("a refused slice was stored")
		}
	})
	t.Run("3 questions on create", func(t *testing.T) {
		s := lfsService(t, sc, &fakeAnalyst{results: []analystResult{{p: lfsQuestions(t, 3)}}})
		if _, err := s.Create(context.Background(), "Let me keep notes."); err != nil {
			t.Errorf("3 questions were refused at a schema limit of 3: %v", err)
		}
	})
	t.Run("2 then 2 on revision", func(t *testing.T) {
		a := &fakeAnalyst{results: []analystResult{{p: lfsQuestions(t, 2)}, {p: lfsQuestions(t, 2)}}}
		s := lfsService(t, sc, a)
		sl := create(t, s, "Let me keep notes.")
		var ans []slices.Answer
		for _, q := range sl.Questions {
			ans = append(ans, slices.Answer{Question: q.ID, Text: "ok"})
		}
		_, err := s.Answer(context.Background(), sl.ID, ans)
		if err == nil {
			t.Fatal("a revision to 4 questions was accepted over a schema limit of 3")
		}
		if code(t, err) != "TooManyQuestions" {
			t.Errorf("want TooManyQuestions, got %v", err)
		}
	})
}
