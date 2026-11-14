package slice

import (
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// limits are the counts the schemas set that a caller enforces before it builds a document: the
// numbers are read from the compiled schemas, so they live in the contract alone.
type limits struct {
	answerText, questions int
}

// compileLimits reads the free answer's maxLength (slice-defs.schema.json `$defs/answer`, the
// branch with `text`) and the slice's questions maxItems (slice.schema.json). A limit the schemas
// do not set is 0; a caller that enforces it refuses to start without it (engine slices.New).
func compileLimits(c *jsonschema.Compiler, sl *jsonschema.Schema, defs string) (limits, error) {
	var l limits
	for s := sl; s != nil; s = s.Ref {
		if q := s.Properties["questions"]; q != nil && q.MaxItems != nil {
			l.questions = *q.MaxItems
			break
		}
	}
	a, err := c.Compile(defs + "#/$defs/answer")
	if err != nil {
		return limits{}, err
	}
	for _, b := range append([]*jsonschema.Schema{a}, a.OneOf...) {
		if t := b.Properties["text"]; t != nil && t.MaxLength != nil {
			l.answerText = *t.MaxLength
			break
		}
	}
	return l, nil
}

// MaxAnswerText is the longest free answer, in characters (slice-defs.schema.json `$defs/answer`
// text maxLength), or 0 when the schema sets none.
func (s *Schemas) MaxAnswerText() int { return s.limits.answerText }

// MaxQuestions is the most questions a slice may have (slice.schema.json `questions` maxItems), or 0 when the
// schema sets none.
func (s *Schemas) MaxQuestions() int { return s.limits.questions }
