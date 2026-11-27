package api

import (
	"context"
	"errors"

	"github.com/agentic-platform/engine/api/gen"
)

// The trial-sets resource (engine contract, tag trial-sets): build an approved slice several
// times, reusing nothing, and see how much the builds vary. The calls are declared in the
// contract and routed; their work comes with feature 012's T009, T017 and T022, and until then
// each answers 500 with errTrialSetsNotImplemented.

// errTrialSetsNotImplemented is what a trial-set call answers until its work exists.
var errTrialSetsNotImplemented = errors.New("trial sets are not implemented yet")

func (s *sliceServer) StartTrialSet(context.Context, gen.StartTrialSetRequestObject) (gen.StartTrialSetResponseObject, error) {
	return nil, errTrialSetsNotImplemented
}

func (s *sliceServer) ListTrialSets(context.Context, gen.ListTrialSetsRequestObject) (gen.ListTrialSetsResponseObject, error) {
	return nil, errTrialSetsNotImplemented
}

func (s *sliceServer) GetTrialSet(context.Context, gen.GetTrialSetRequestObject) (gen.GetTrialSetResponseObject, error) {
	return nil, errTrialSetsNotImplemented
}

// ---- an engine that keeps no slices ----

func (s *server) StartTrialSet(context.Context, gen.StartTrialSetRequestObject) (gen.StartTrialSetResponseObject, error) {
	return gen.StartTrialSet404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("the slice")}, nil
}

func (s *server) ListTrialSets(context.Context, gen.ListTrialSetsRequestObject) (gen.ListTrialSetsResponseObject, error) {
	return gen.ListTrialSets404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("the slice")}, nil
}

func (s *server) GetTrialSet(context.Context, gen.GetTrialSetRequestObject) (gen.GetTrialSetResponseObject, error) {
	return gen.GetTrialSet404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("the trial set")}, nil
}
