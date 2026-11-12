package slices

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Answer records the developer's answers to open questions and revises the slice through the
// analyst (answerQuestions). The analyst gets the description, the slice's current proposal and
// all its questions, the answered ones with their answers; its revision replaces the parts, its
// questions are new ones with the next Q-<n> IDs, and an unusable revision is sent back as in
// Create, up to the pattern's limits.attempts. The checks run again, a part whose hash changed
// loses its approval, and the history gains "answered" then "revised"; any other part that now
// fails a check loses its approval too, recorded as "withdrawn".
//
// No answers, an answer with neither or both of an option and a text, an option out of range or
// a text longer than 200 characters give InvalidRequest; an answer to a question that is
// answered or does not exist gives QuestionNotOpen; more than 10 questions in total give
// TooManyQuestions. A refused Answer changes nothing.
//
// The analyst runs without the lock. When the slice changed meanwhile, the revision is dropped:
// QuestionNotOpen when one of the questions is no longer open, else OperationFailed.
func (s *Service) Answer(ctx context.Context, id string, answers []Answer) (Slice, error) {
	s.mu.Lock()
	r, err := s.find(id)
	if err != nil {
		s.mu.Unlock()
		return Slice{}, err
	}
	cur := clone(r.slice)
	base := record{baseline: r.baseline, compat: slices.Clone(r.compat), lint: slices.Clone(r.lint)}
	s.mu.Unlock()

	if err := answerable(cur.Questions, answers); err != nil {
		return Slice{}, err
	}
	now := s.now()
	qs := slices.Clone(cur.Questions) // cur is already a deep copy
	var answered []string
	for _, a := range answers {
		i := slices.IndexFunc(qs, func(q Question) bool { return q.ID == a.Question })
		qs[i].Answer, qs[i].AnsweredAt = &Answer{Question: a.Question, Option: copyInt(a.Option), Text: a.Text}, now
		answered = append(answered, a.Question)
	}

	model, digest := lastProposal(cur.History)
	current := Proposal{Goals: cur.Goals, Terms: cur.Terms, Rules: cur.Rules, Examples: cur.Examples,
		Contract: cur.Contract, Model: model, AgentDigest: digest}
	p, sl, err := s.propose(ctx, AnalystInput{Description: cur.Description, Current: &current, Questions: clone(Slice{Questions: qs}).Questions}, len(qs))
	if err != nil {
		return Slice{}, err
	}
	next := uint(len(qs))
	for _, q := range qs {
		if n, err := strconv.ParseUint(strings.TrimPrefix(q.ID, "Q-"), 10, 0); err == nil && uint(n) > next {
			next = uint(n)
		}
	}
	for _, q := range p.Questions {
		next++
		qs = append(qs, Question{ID: "Q-" + strconv.FormatUint(uint64(next), 10), About: q.About, Text: q.Text,
			Options: slices.Clone(q.Options)})
	}
	sl.ID, sl.Questions, sl.History = cur.ID, qs, cur.History
	rec := &record{slice: sl, baseline: base.baseline, compat: base.compat, lint: base.lint}
	if err := setParts(&rec.slice); err != nil {
		return Slice{}, &Error{Code: "OperationFailed", Detail: "the analyst's revision cannot be hashed: " + err.Error()}
	}
	var changed []string
	for _, part := range allParts {
		if rec.slice.Parts.of(part) != cur.Parts.of(part) {
			changed = append(changed, string(part))
		}
	}
	if err := s.recheck(ctx, rec, rec.slice.Parts.Contract != cur.Parts.Contract); err != nil {
		return Slice{}, err
	}
	rec.slice.Approvals = slices.DeleteFunc(slices.Clone(cur.Approvals), func(a Approval) bool {
		return a.ContentHash != rec.slice.Parts.of(a.Part)
	})
	rec.slice.History = append(slices.Clone(cur.History),
		HistoryEntry{Kind: "answered", At: now, Refs: answered},
		HistoryEntry{Kind: "revised", At: s.now(), Model: p.Model, AgentDigest: p.AgentDigest, Refs: orEmpty(changed)})
	withdrawFailing(&rec.slice, s.now())
	rec.slice.State = state(&rec.slice)

	s.mu.Lock()
	defer s.mu.Unlock()
	latest, err := s.find(id)
	if err != nil {
		return Slice{}, err
	}
	if latest != r || len(latest.slice.History) != len(cur.History) {
		if err := answerable(latest.slice.Questions, answers); err != nil {
			return Slice{}, err
		}
		return Slice{}, &Error{Code: "OperationFailed", Detail: "the slice changed while the analyst revised it; answer again"}
	}
	if err := s.save(rec); err != nil {
		return Slice{}, err
	}
	*r = *rec
	return clone(r.slice), nil
}

// CheckAnswers refuses exactly as Answer would before it calls the analyst, and changes nothing.
func (s *Service) CheckAnswers(id string, answers []Answer) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.find(id)
	if err != nil {
		return err
	}
	return answerable(r.slice.Questions, answers)
}

// answerable checks answers against the slice's questions: the shape of each answer first
// (InvalidRequest), then that its question is open (QuestionNotOpen), then that a chosen option
// exists (InvalidRequest).
func answerable(qs []Question, answers []Answer) error {
	invalid := func(format string, a ...any) error {
		return &Error{Code: "InvalidRequest", Detail: fmt.Sprintf(format, a...)}
	}
	if len(answers) == 0 {
		return invalid("no answers; answer at least one open question")
	}
	seen := map[string]bool{}
	for _, a := range answers {
		switch {
		case seen[a.Question]:
			return invalid("question %s is answered twice", a.Question)
		case a.Option == nil && a.Text == "":
			return invalid("the answer to %s gives neither an option nor a text", a.Question)
		case a.Option != nil && a.Text != "":
			return invalid("the answer to %s gives both an option and a text; give one", a.Question)
		case a.Option == nil && strings.TrimSpace(a.Text) == "":
			return invalid("the answer to %s is blank", a.Question)
		case utf8.RuneCountInString(a.Text) > maxAnswerText:
			return invalid("the answer to %s is longer than %d characters", a.Question, maxAnswerText)
		}
		seen[a.Question] = true
	}
	for _, a := range answers {
		i := slices.IndexFunc(qs, func(q Question) bool { return q.ID == a.Question })
		if i < 0 || !open(qs[i]) {
			return &Error{Code: "QuestionNotOpen", Detail: fmt.Sprintf("question %q does not exist or is answered", a.Question)}
		}
		if a.Option != nil && (*a.Option < 0 || *a.Option >= len(qs[i].Options)) {
			return invalid("question %s has %d options; option %d does not exist", a.Question, len(qs[i].Options), *a.Option)
		}
	}
	return nil
}

// lastProposal returns the model and agent digest of the latest proposed or revised entry.
func lastProposal(h []HistoryEntry) (model, digest string) {
	for i := len(h) - 1; i >= 0; i-- {
		if h[i].Kind == "proposed" || h[i].Kind == "revised" {
			return h[i].Model, h[i].AgentDigest
		}
	}
	return "", ""
}

func copyInt(p *int) *int {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// Delete erases a slice and its records (deleteSlice); its ID is not given out again. An unknown
// slice gives NotFound. Runs, and the refusal while one has not ended, come with the run store.
// The slice's stored record is removed before the slice is.
func (s *Service) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.find(id); err != nil {
		return err
	}
	if s.records != nil {
		if err := s.records.DeleteSliceRecord(id); err != nil {
			return fmt.Errorf("slices: removing the record of slice %s: %w", id, err)
		}
	}
	delete(s.slices, id)
	return nil
}
