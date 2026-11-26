package api

import (
	"context"
	"errors"

	"github.com/agentic-platform/engine/api/gen"
)

// The /captured-patterns resource (engine contract, tag captured-patterns): capture a done run as a
// pattern that builds later slices of the same kind with no model call, change its parameters,
// make its proof slice, approve, withdraw and erase it. The calls are declared in the contract
// and routed; their work comes with feature 011's later tasks (T007, T020, T029), and until then
// each answers 500 with errNotImplemented.

// errNotImplemented is what a captured-pattern call answers until its work exists.
var errNotImplemented = errors.New("captured patterns are not implemented yet")

func (s *server) CaptureRun(context.Context, gen.CaptureRunRequestObject) (gen.CaptureRunResponseObject, error) {
	return nil, errNotImplemented
}

func (s *server) ListCapturedPatterns(context.Context, gen.ListCapturedPatternsRequestObject) (gen.ListCapturedPatternsResponseObject, error) {
	return nil, errNotImplemented
}

func (s *server) GetCapturedPattern(context.Context, gen.GetCapturedPatternRequestObject) (gen.GetCapturedPatternResponseObject, error) {
	return nil, errNotImplemented
}

func (s *server) ReplaceParameters(context.Context, gen.ReplaceParametersRequestObject) (gen.ReplaceParametersResponseObject, error) {
	return nil, errNotImplemented
}

func (s *server) MakeProofSlice(context.Context, gen.MakeProofSliceRequestObject) (gen.MakeProofSliceResponseObject, error) {
	return nil, errNotImplemented
}

func (s *server) ApproveCapturedPattern(context.Context, gen.ApproveCapturedPatternRequestObject) (gen.ApproveCapturedPatternResponseObject, error) {
	return nil, errNotImplemented
}

func (s *server) WithdrawCapturedPattern(context.Context, gen.WithdrawCapturedPatternRequestObject) (gen.WithdrawCapturedPatternResponseObject, error) {
	return nil, errNotImplemented
}

func (s *server) EraseCapturedPattern(context.Context, gen.EraseCapturedPatternRequestObject) (gen.EraseCapturedPatternResponseObject, error) {
	return nil, errNotImplemented
}
