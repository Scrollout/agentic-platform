// Package slices owns a slice from the analyst's proposal to its approval: it builds the slice
// from the proposal, runs the checks of each part (schema, traceability, pattern fit, contract
// compatibility), keeps each part's hash, and records approvals that hold only while that hash
// is unchanged (spec FR-002 to FR-007, data-model.md).
//
// The analyst (a model) and oasdiff are behind the Analyst and ContractDiff interfaces. Slices
// are kept durably through Records (the run store in the engine), or in memory without it.
package slices

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/agentic-platform/slice"
)

// Part is a part the developer approves (slice-defs.schema.json `part`).
type Part string

const (
	PartGoals    Part = "goals"
	PartRules    Part = "rules" // the rules, examples and terms
	PartContract Part = "contract"
)

var allParts = []Part{PartGoals, PartRules, PartContract}

// The records of a slice, with the contract's JSON names (slice-defs.schema.json).
type (
	Goal struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}
	Term struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Definition string `json:"definition"`
	}
	Rule struct {
		ID           string   `json:"id"`
		Goal         string   `json:"goal"`
		Pattern      string   `json:"pattern"`
		Precondition string   `json:"precondition,omitempty"`
		Trigger      string   `json:"trigger,omitempty"`
		Feature      string   `json:"feature,omitempty"`
		System       string   `json:"system"`
		Response     string   `json:"response"`
		Terms        []string `json:"terms"` // nil is the empty list
	}
)

// RulesPart is the body of a rules change: the rules part covers rules, examples and terms.
type RulesPart struct {
	Rules    []Rule `json:"rules"`
	Examples string `json:"examples"`
	Terms    []Term `json:"terms"`
}

// QuestionDraft is a question the analyst asks; the platform gives it its ID.
type QuestionDraft struct {
	About   string   `json:"about"`
	Text    string   `json:"text"`
	Options []string `json:"options,omitempty"`
}

// Proposal is the analyst's output (slice-proposal.schema.json) plus the model and agent
// definition digest that produced it, which the slice's history names.
type Proposal struct {
	Goals     []Goal          `json:"goals"`
	Terms     []Term          `json:"terms"`
	Rules     []Rule          `json:"rules"`
	Examples  string          `json:"examples"`
	Contract  string          `json:"contract"`
	Questions []QuestionDraft `json:"questions,omitempty"`

	Model       string `json:"model"`
	AgentDigest string `json:"agent_digest"`
}

// AnalystInput is what the analyst proposes from. Current and Questions are set when revising a
// slice after answers. Previous and Errors are set when Create asks again: Previous is the
// unusable proposal of the attempt before, and Errors says what was wrong with it, one entry per
// finding; both are empty on the first call.
type AnalystInput struct {
	Description string
	Current     *Proposal
	Questions   []Question
	Previous    *Proposal
	Errors      []string
}

// Analyst is the model behind the analyst agent.
type Analyst interface {
	Propose(ctx context.Context, in AnalystInput) (Proposal, error)
}

// ErrMultipleUseCases is what an analyst returns for a description that covers more than one use
// case; Create answers it with NarrowDescription.
var ErrMultipleUseCases = errors.New("the description covers more than one use case")

// ContractFinding is one oasdiff finding. Level is "error", "warn" or "info"; only "error" fails
// the compatibility check.
type ContractFinding struct{ Level, ID, Text string }

// ContractDiff compares a contract revision with base, oasdiff's way. base is "" (an empty
// document) when no contract of the slice was approved yet (research D4).
type ContractDiff interface {
	Diff(ctx context.Context, base, revision string) ([]ContractFinding, error)
}

// Check is one check's latest result on a part (check_result). Part is "goals", "rules" or
// "contract"; Reason names the item when one is at fault.
type Check struct {
	Part   string
	Check  string
	Passed bool
	Reason string

	// Pattern is true for a condition of the catalogue pattern (checks.Fit, or PatternFit when no
	// pattern can be read), false for a check of this package (check_result `pattern`).
	Pattern bool
}

// The names of the checks this package runs besides the pattern's conditions.
const (
	CheckSchema                = "Schema"                // the part against slice-defs.schema.json
	CheckTraceability          = "Traceability"          // slice.Trace: the links between IDs
	CheckContractCompatibility = "ContractCompatibility" // oasdiff against the baseline
	CheckContractLint          = "ContractLint"          // Spectral with OpenAPI's standard rules
	CheckExampleTagged         = "ExampleTagged"         // each scenario's @R-<n> and @E-<n> tags
)

// Parts holds each part's current hash, lowercase hex SHA-256.
type Parts struct{ Goals, Rules, Contract string }

func (p Parts) of(part Part) string {
	switch part {
	case PartGoals:
		return p.Goals
	case PartRules:
		return p.Rules
	default:
		return p.Contract
	}
}

// Approval is a part's approval at the hash the developer reviewed.
type Approval struct {
	Part        Part
	ContentHash string
	At          time.Time
}

// Question is a question with its platform ID. It is open while Answer is nil; AnsweredAt is
// when the answer was recorded.
type Question struct {
	ID         string
	About      string
	Text       string
	Options    []string
	Answer     *Answer
	AnsweredAt time.Time
}

// Answer is the developer's answer to one question (slice-defs.schema.json `answer`): exactly one
// of Option, the position of a chosen option from 0, or Text, a free answer no longer
// than the schema's limit (Schemas.MaxAnswerText).
type Answer struct {
	Question string
	Option   *int
	Text     string
}

// LintFinding is one Spectral finding on a contract. Level is "error", "warn" or "info"; only
// "error" fails the lint check. Code is the rule's name.
type LintFinding struct{ Level, Code, Text string }

// ContractLint lints a contract with Spectral and OpenAPI's standard rules (spectral:oas), not
// the engine's own ruleset. The service may call Lint from several goroutines at once.
type ContractLint interface {
	Lint(ctx context.Context, contract string) ([]LintFinding, error)
}

// HistoryEntry is one change to the slice. Kind is proposed, revised, answered, changed,
// approved or withdrawn (approvals of the parts in Refs withdrawn because they now fail a check); a proposal or revision names its model and agent digest; Refs are the parts it
// concerns, or for answered the questions answered.
type HistoryEntry struct {
	Kind        string
	At          time.Time
	Model       string
	AgentDigest string
	Refs        []string
	// Attempts is, on a proposed or revised entry, how many times the analyst was asked before
	// its output was usable; 0 on other entries and on entries recorded before it was kept.
	Attempts int
}

// Slice is one use case and its state before a build. State is proposed, questions-open, checked
// or approved.
type Slice struct {
	ID, Description, Contract, Examples, State string
	Goals                                      []Goal
	Terms                                      []Term
	Rules                                      []Rule
	Questions                                  []Question
	Parts                                      Parts
	Approvals                                  []Approval // only those that hold
	Checks                                     []Check
	History                                    []HistoryEntry
	// ChecksMethod is the method identity (hex) every check of the slice ran under; "" while a
	// check could not run, or for a record written before feature 006. A service under another
	// method runs the checks again before it shows them (ADR 0080).
	ChecksMethod string
	// ProofOf names the captured pattern a proof slice proves (feature 011); "" for any other
	// slice.
	ProofOf string
	// ArtifactCount is the number of distinct artifacts stored for the slice's runs. The service
	// does not keep runs; whoever serves the slice fills it from the run store.
	ArtifactCount int `json:"artifact_count"`
}

// Result is what Create returns. Reused is true when Slice is an approved slice of the same
// description, offered instead of a new proposal.
type Result struct {
	Slice  Slice
	Reused bool
}

// Error is a refusal in the contract's terms: Code is an openapi.yaml ErrorCode, and Failures
// lists the failing checks of PartFailsChecks and PatternDoesNotFit.
type Error struct {
	Code     string
	Detail   string
	Failures []Check
}

func (e *Error) Error() string {
	if len(e.Failures) == 0 {
		return e.Code + ": " + e.Detail
	}
	reasons := make([]string, len(e.Failures))
	for i, f := range e.Failures {
		reasons[i] = fmt.Sprintf("%s %s: %s", f.Part, f.Check, f.Reason)
	}
	return fmt.Sprintf("%s: %s (%s)", e.Code, e.Detail, strings.Join(reasons, "; "))
}

// Config is what a Service needs.
type Config struct {
	Schemas *slice.Schemas // the contract's schemas: proposal, parts and IDs
	Analyst Analyst
	Diff    ContractDiff
	// Lint lints the contract when the slice is created and whenever its contract changes. nil
	// fails closed: every slice carries a failed ContractLint result on its contract, which then
	// cannot be approved; a lint that cannot run fails the check too.
	Lint ContractLint
	// Pattern is the catalogue pattern's pattern.yaml, whose conditions run as checks.Fit and
	// whose limits.attempts bounds how often Create asks the analyst for a usable proposal. nil
	// fails closed: every slice carries a failed PatternFit result on its contract, which then
	// cannot be approved, and the analyst is asked once.
	Pattern []byte
	Now     func() time.Time // nil means time.Now
	// Records keeps the slices durably: New loads every stored slice and the ID sequence from it
	// and fails, naming the slice, on a record it cannot read or check; every change is stored
	// before it is reported. nil keeps slices in memory only, so a restart loses them.
	Records Records
	// Log receives what the service keeps out of the slices, such as a runner's full error when
	// oasdiff or the lint cannot run (a check reason only says it could not run). nil discards it.
	Log *slog.Logger
	// Method is the method identity (64 lowercase hex digits, as method.ID.String gives it) the
	// service's checks run under (feature 006); "" re-checks nothing. A plain string, so the
	// slices, and what judges them, do not depend on the engine's embedded identity.
	Method string
}
