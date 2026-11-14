package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/agentic-platform/cli/api/gen"
)

var parts = []gen.Part{gen.PartGoals, gen.PartRules, gen.PartContract}

func parsePart(s string) (gen.Part, bool) {
	for _, p := range parts {
		if string(p) == s {
			return p, true
		}
	}
	return "", false
}

func needSlice(cmd, s string) error {
	if !sliceID.MatchString(s) {
		return usagef("%s: %q is not a slice ID (S-<n>)", cmd, s)
	}
	return nil
}

// ---- describe ----

func parseDescribe(args []string) (func(context.Context, *session) error, error) {
	return func(ctx context.Context, s *session) error {
		desc := strings.Join(args, " ")
		if len(args) == 0 {
			b, err := io.ReadAll(s.in)
			if err != nil {
				return failf(exitFailed, "describe: reading standard input: %v", err)
			}
			desc = strings.TrimSpace(string(b))
		}
		if strings.TrimSpace(desc) == "" {
			return usagef("describe: the description is empty")
		}
		r, err := s.api.CreateSliceWithResponse(ctx, &gen.CreateSliceParams{ApiVersion: APIVersion},
			gen.CreateSliceRequest{Description: desc})
		if err := expect(r, err, http.StatusAccepted); err != nil {
			return err
		}
		op, err := s.finish(ctx, r.JSON202)
		if err != nil {
			return err
		}
		id, reused := op.Target, false
		if op.Result != nil && op.Result.ReusedSlice != nil {
			id, reused = *op.Result.ReusedSlice, true
		} else if op.Result != nil && op.Result.Slice != nil {
			id = *op.Result.Slice
		}
		sl, err := s.getSlice(ctx, id)
		if err != nil {
			return err
		}
		if reused {
			fmt.Fprintf(s.out, "A slice with this description exists already:\n")
		}
		fmt.Fprintf(s.out, "%s  %s  (%s)\n", sl.Id, sl.Title, sl.State)
		writeMisfit(s.out, sl)
		if n := len(openQuestions(sl)); n > 0 {
			fmt.Fprintf(s.out, "%d open question(s): run `questions %s`\n", n, sl.Id)
		} else {
			fmt.Fprintf(s.out, "Next: `show %s`\n", sl.Id)
		}
		return nil
	}, nil
}

// ---- questions ----

func openQuestions(sl *gen.Slice) []gen.Question {
	var open []gen.Question
	for _, q := range sl.Questions {
		if q.AnswerValue == nil && q.AnsweredAt == nil {
			open = append(open, q)
		}
	}
	return open
}

func parseQuestions(args []string) (func(context.Context, *session) error, error) {
	if len(args) != 1 {
		return nil, usagef("questions: want exactly one slice")
	}
	if err := needSlice("questions", args[0]); err != nil {
		return nil, err
	}
	id := args[0]
	return func(ctx context.Context, s *session) error {
		sl, err := s.getSlice(ctx, id)
		if err != nil {
			return err
		}
		open := openQuestions(sl)
		if len(open) == 0 {
			fmt.Fprintf(s.out, "%s has no open questions (%s).\n", id, sl.State)
			return nil
		}
		fmt.Fprintf(s.out, "%s has %d open question(s). Answer each on one line: an option's number, or your own text.\n", id, len(open))
		answers := make([]gen.Answer, 0, len(open))
		for _, q := range open {
			fmt.Fprintf(s.out, "\n%s (about %s): %s\n", q.Id, q.About, q.Text)
			var opts []string
			if q.Options != nil {
				opts = *q.Options
			}
			for i, o := range opts {
				fmt.Fprintf(s.out, "  %d) %s\n", i, o)
			}
			fmt.Fprint(s.out, "> ")
			line, ok := s.readLine()
			line = strings.TrimSpace(line)
			if !ok || line == "" {
				return usagef("questions: no answer for %s; nothing was recorded", q.Id)
			}
			var v gen.AnswerValue
			if n, err := strconv.Atoi(line); err == nil && len(opts) > 0 {
				if n < 0 || n >= len(opts) {
					return usagef("questions: %s has options 0 to %d, not %d; nothing was recorded", q.Id, len(opts)-1, n)
				}
				err = v.FromAnswer0(gen.OptionAnswer{Option: n})
				if err != nil {
					return failf(exitFailed, "questions: %v", err)
				}
			} else if err := v.FromAnswer1(gen.TextAnswer{Text: line}); err != nil {
				return failf(exitFailed, "questions: %v", err)
			}
			answers = append(answers, gen.Answer{Question: q.Id, AnswerValue: v})
		}
		r, err := s.api.AnswerQuestionsWithResponse(ctx, id, &gen.AnswerQuestionsParams{ApiVersion: APIVersion},
			gen.AnswerRequest{Answers: answers})
		if err := expect(r, err, http.StatusAccepted); err != nil {
			return err
		}
		if _, err := s.finish(ctx, r.JSON202); err != nil {
			return err
		}
		sl, err = s.getSlice(ctx, id)
		if err != nil {
			return err
		}
		fmt.Fprintf(s.out, "\nRecorded %d answer(s). %s is %s", len(answers), id, sl.State)
		if n := len(openQuestions(sl)); n > 0 {
			fmt.Fprintf(s.out, " with %d new open question(s)", n)
		}
		fmt.Fprintln(s.out, ".")
		return nil
	}, nil
}

// ---- show ----

// partOf names the part a check result's `part` (a part or one of its items) belongs to.
func partOf(ref string) gen.Part {
	switch {
	case ref == "goals" || strings.HasPrefix(ref, "G-"):
		return gen.PartGoals
	case ref == "contract" || strings.HasPrefix(ref, "contract/"):
		return gen.PartContract
	default: // rules, R-<n>, T-<n>, R-<n>/E-<n>
		return gen.PartRules
	}
}

func partHash(sl *gen.Slice, p gen.Part) string {
	switch p {
	case gen.PartGoals:
		return sl.Parts.Goals
	case gen.PartRules:
		return sl.Parts.Rules
	}
	return sl.Parts.Contract
}

func approved(sl *gen.Slice, p gen.Part) bool {
	for _, a := range sl.Approvals {
		if a.Part == p && a.ContentHash == partHash(sl, p) {
			return true
		}
	}
	return false
}

func failingChecks(sl *gen.Slice, only gen.Part) []gen.CheckResult {
	var out []gen.CheckResult
	for _, c := range sl.Checks {
		if !c.Passed && (only == "" || partOf(c.Part) == only) {
			out = append(out, c)
		}
	}
	return out
}

// writeMisfit prints one plain line when the slice fails a pattern condition, a check result the
// engine marks with `pattern` (absent counts as false): the pattern does not fit, and each failing
// condition with its reason. A fitting slice prints nothing.
func writeMisfit(w io.Writer, sl *gen.Slice) {
	var why []string
	for _, c := range sl.Checks {
		if !c.Passed && c.Pattern {
			why = append(why, fmt.Sprintf("%s: %s", c.Check, c.Reason))
		}
	}
	if len(why) > 0 {
		fmt.Fprintf(w, "The pattern does not fit this description: %s\n", strings.Join(why, "; "))
	}
}

func writeChecks(w io.Writer, checks []gen.CheckResult) {
	for _, c := range checks {
		verdict := "passed"
		if !c.Passed {
			verdict = "FAILED"
		}
		fmt.Fprintf(w, "  %-6s  %s (%s): %s\n", verdict, c.Check, c.Part, c.Reason)
	}
}

func writePart(w io.Writer, sl *gen.Slice, p gen.Part) {
	state := "not approved"
	if approved(sl, p) {
		state = "approved"
	}
	fmt.Fprintf(w, "== %s  %s  (%s)\n", p, partHash(sl, p), state)
	switch p {
	case gen.PartGoals:
		for _, g := range sl.Goals {
			fmt.Fprintf(w, "  %s  %s\n", g.Id, g.Text)
		}
	case gen.PartRules:
		fmt.Fprintln(w, "rules:")
		for _, r := range sl.Rules {
			text, ok := sl.RuleText[r.Id]
			if !ok {
				text = r.Response
			}
			fmt.Fprintf(w, "  %s  (%s, goal %s)  %s\n", r.Id, r.Pattern, r.Goal, text)
		}
		fmt.Fprintln(w, "terms:")
		for _, t := range sl.Terms {
			fmt.Fprintf(w, "  %s  %s: %s\n", t.Id, t.Name, t.Definition)
		}
		fmt.Fprintln(w, "examples:")
		writeIndented(w, sl.Examples)
	case gen.PartContract:
		writeIndented(w, sl.Contract)
	}
	var checks []gen.CheckResult
	for _, c := range sl.Checks {
		if partOf(c.Part) == p {
			checks = append(checks, c)
		}
	}
	if len(checks) > 0 {
		fmt.Fprintln(w, "checks:")
		writeChecks(w, checks)
	}
}

func writeIndented(w io.Writer, text string) {
	for _, l := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		fmt.Fprintf(w, "  %s\n", l)
	}
}

func parseShow(args []string) (func(context.Context, *session) error, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, usagef("show: want a slice and at most one part")
	}
	if err := needSlice("show", args[0]); err != nil {
		return nil, err
	}
	var part gen.Part
	if len(args) == 2 {
		p, ok := parsePart(args[1])
		if !ok {
			return nil, usagef("show: %q is not a part (goals, rules or contract)", args[1])
		}
		part = p
	}
	id := args[0]
	return func(ctx context.Context, s *session) error {
		sl, err := s.getSlice(ctx, id)
		if err != nil {
			return err
		}
		if part != "" {
			writePart(s.out, sl, part)
			return nil
		}
		fmt.Fprintf(s.out, "%s  %s  (%s)\n", sl.Id, sl.Title, sl.State)
		fmt.Fprintf(s.out, "description:\n")
		writeIndented(s.out, sl.Description)
		for _, p := range parts {
			writePart(s.out, sl, p)
		}
		writeMisfit(s.out, sl)
		if n := len(openQuestions(sl)); n > 0 {
			fmt.Fprintf(s.out, "%d open question(s): run `questions %s`\n", n, sl.Id)
		}
		if sl.LatestRun != nil {
			fmt.Fprintf(s.out, "latest run: %s\n", *sl.LatestRun)
		}
		return nil
	}, nil
}

// ---- edit ----

func parseEdit(args []string, env Env) (func(context.Context, *session) error, error) {
	if len(args) != 2 {
		return nil, usagef("edit: want a slice and a part")
	}
	if err := needSlice("edit", args[0]); err != nil {
		return nil, err
	}
	part, ok := parsePart(args[1])
	if !ok {
		return nil, usagef("edit: %q is not a part (goals, rules or contract)", args[1])
	}
	editor := strings.Fields(env.Editor)
	if len(editor) == 0 {
		return nil, failf(exitUsage, "edit: EDITOR is not set")
	}
	id := args[0]
	return func(ctx context.Context, s *session) error {
		sl, err := s.getSlice(ctx, id)
		if err != nil {
			return err
		}
		before, ext, err := editText(sl, part)
		if err != nil {
			return err
		}
		dir, err := os.MkdirTemp("", "ap-edit-")
		if err != nil {
			return failf(exitFailed, "edit: %v", err)
		}
		keep := false
		defer func() {
			if !keep {
				_ = os.RemoveAll(dir)
			}
		}()
		file := filepath.Join(dir, id+"-"+string(part)+ext)
		if err := os.WriteFile(file, before, 0o600); err != nil {
			return failf(exitFailed, "edit: %v", err)
		}
		cmd := exec.CommandContext(ctx, editor[0], append(editor[1:], file)...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = s.stdin, s.out, s.out
		if err := cmd.Run(); err != nil {
			return &failure{code: exitFailed, cause: ctx.Err(),
				msg: fmt.Sprintf("edit: the editor failed (%v); nothing was replaced", err)}
		}
		after, err := os.ReadFile(file)
		if err != nil {
			return failf(exitFailed, "edit: %v", err)
		}
		if bytes.Equal(before, after) {
			fmt.Fprintf(s.out, "%s of %s is unchanged; nothing was replaced.\n", part, id)
			return nil
		}
		op, err := s.replace(ctx, id, part, after)
		if err != nil {
			keep = true
			return fmt.Errorf("%w\nyour edit is kept in %s", err, file)
		}
		if _, err := s.finish(ctx, op); err != nil {
			keep = true
			return fmt.Errorf("%w\nyour edit is kept in %s", err, file)
		}
		sl, err = s.getSlice(ctx, id)
		if err != nil {
			return err
		}
		fmt.Fprintf(s.out, "Replaced %s of %s; it is now %s (%s).\n", part, id, partHash(sl, part), sl.State)
		if failing := failingChecks(sl, ""); len(failing) > 0 {
			fmt.Fprintln(s.out, "failing checks:")
			writeChecks(s.out, failing)
			return failf(exitFailed, "edit: %s has %d failing check(s)", id, len(failing))
		}
		return nil
	}, nil
}

// editText is the file the editor opens: the JSON of the replace body for goals and rules, the YAML
// text itself for the contract.
func editText(sl *gen.Slice, p gen.Part) ([]byte, string, error) {
	var v any
	switch p {
	case gen.PartGoals:
		v = gen.GoalsPart{Goals: sl.Goals}
	case gen.PartRules:
		v = gen.RulesPart{Rules: sl.Rules, Examples: sl.Examples, Terms: sl.Terms}
	default:
		return []byte(sl.Contract), ".yaml", nil
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, "", failf(exitFailed, "edit: %v", err)
	}
	return b.Bytes(), ".json", nil
}

// decodeStrict reads exactly one JSON value with only the body's own fields.
func decodeStrict(b []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return fmt.Errorf("more than one JSON value")
	}
	return nil
}

func (s *session) replace(ctx context.Context, id string, p gen.Part, text []byte) (*gen.Operation, error) {
	switch p {
	case gen.PartGoals:
		var body gen.GoalsPart
		if err := decodeStrict(text, &body); err != nil {
			return nil, failf(exitUsage, "edit: the goals are not valid JSON of {\"goals\": [...]}: %v", err)
		}
		r, err := s.api.ReplaceGoalsWithResponse(ctx, id, &gen.ReplaceGoalsParams{ApiVersion: APIVersion}, body)
		if err := expect(r, err, http.StatusAccepted); err != nil {
			return nil, err
		}
		return r.JSON202, nil
	case gen.PartRules:
		var body gen.RulesPart
		if err := decodeStrict(text, &body); err != nil {
			return nil, failf(exitUsage, "edit: the rules are not valid JSON of {\"rules\", \"examples\", \"terms\"}: %v", err)
		}
		r, err := s.api.ReplaceRulesWithResponse(ctx, id, &gen.ReplaceRulesParams{ApiVersion: APIVersion}, body)
		if err := expect(r, err, http.StatusAccepted); err != nil {
			return nil, err
		}
		return r.JSON202, nil
	}
	r, err := s.api.ReplaceContractWithResponse(ctx, id, &gen.ReplaceContractParams{ApiVersion: APIVersion},
		gen.ContractPart{Contract: string(text)})
	if err := expect(r, err, http.StatusAccepted); err != nil {
		return nil, err
	}
	return r.JSON202, nil
}

// ---- approve ----

func parseApprove(args []string) (func(context.Context, *session) error, error) {
	if len(args) != 2 {
		return nil, usagef("approve: want a slice and a part")
	}
	if err := needSlice("approve", args[0]); err != nil {
		return nil, err
	}
	part, ok := parsePart(args[1])
	if !ok {
		return nil, usagef("approve: %q is not a part (goals, rules or contract)", args[1])
	}
	id := args[0]
	return func(ctx context.Context, s *session) error {
		sl, err := s.getSlice(ctx, id)
		if err != nil {
			return err
		}
		writePart(s.out, sl, part)
		hash := partHash(sl, part)
		fmt.Fprintf(s.out, "Approving %s of %s at %s\n", part, id, hash)
		r, err := s.api.ApprovePartWithResponse(ctx, id, &gen.ApprovePartParams{ApiVersion: APIVersion},
			gen.ApprovalRequest{Part: part, ContentHash: hash})
		err = expect(r, err, http.StatusAccepted)
		if err == nil {
			_, err = s.finish(ctx, r.JSON202)
		}
		if f, ok := err.(*failure); ok && f.prob != nil && f.prob.Code == gen.ErrorCodePartFailsChecks {
			f.code = exitFailed
		}
		if err != nil {
			return err
		}
		fmt.Fprintf(s.out, "Approved %s of %s at %s.\n", part, id, hash)
		return nil
	}, nil
}

// ---- slices ----

func parseSlices(args []string) (func(context.Context, *session) error, error) {
	if len(args) != 0 {
		return nil, usagef("slices: takes no arguments")
	}
	return func(ctx context.Context, s *session) error {
		var items []gen.SliceSummary
		var token *string
		for {
			r, err := s.api.ListSlicesWithResponse(ctx, &gen.ListSlicesParams{ApiVersion: APIVersion, PageToken: token})
			if err := expect(r, err, http.StatusOK); err != nil {
				return err
			}
			if r.JSON200 == nil {
				return failf(exitFailed, "engine: the slice list has no body")
			}
			items = append(items, r.JSON200.Items...)
			if r.JSON200.NextPageToken == nil {
				break
			}
			token = r.JSON200.NextPageToken
		}
		if len(items) == 0 {
			fmt.Fprintln(s.out, "No slices yet. Start one with `describe`.")
			return nil
		}
		tw := tabwriter.NewWriter(s.out, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "SLICE\tSTATE\tLATEST RUN\tTITLE")
		for _, it := range items {
			run := "-"
			if it.LatestRun != nil {
				run = *it.LatestRun
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", it.Id, it.State, run, it.Title)
		}
		return tw.Flush()
	}, nil
}

// ---- cancel ----

func parseCancel(args []string) (func(context.Context, *session) error, error) {
	if len(args) != 1 {
		return nil, usagef("cancel: want exactly one run or operation")
	}
	target := args[0]
	if !runID.MatchString(target) && !operationID.MatchString(target) {
		return nil, usagef("cancel: %q is not a run (RUN-<n>) or operation (OP-<n>)", target)
	}
	return func(ctx context.Context, s *session) error {
		opID := target
		if runID.MatchString(target) {
			r, err := s.api.GetRunWithResponse(ctx, target, &gen.GetRunParams{ApiVersion: APIVersion})
			if err := expect(r, err, http.StatusOK); err != nil {
				return err
			}
			if r.JSON200 == nil {
				return failf(exitFailed, "engine: run %s has no body", target)
			}
			opID = r.JSON200.Operation
		}
		r, err := s.api.CancelOperationWithResponse(ctx, opID, &gen.CancelOperationParams{ApiVersion: APIVersion})
		if err := expect(r, err, http.StatusAccepted); err != nil {
			return err
		}
		op, err := s.wait(ctx, r.JSON202)
		if err != nil {
			return err
		}
		switch op.Status {
		case gen.OperationStatusCancelled:
			fmt.Fprintf(s.out, "Cancelled %s (%s of %s).\n", op.Id, op.Kind, op.Target)
			return nil
		case gen.OperationStatusFailed:
			return refused(op.Error)
		}
		return refused(&gen.Problem{Code: gen.ErrorCodeOperationEnded,
			Title: fmt.Sprintf("%s ended %s before it was cancelled", op.Id, op.Status)})
	}, nil
}

// ---- delete ----

func parseDelete(args []string) (func(context.Context, *session) error, error) {
	yes := false
	var rest []string
	for _, a := range args {
		if a == "--yes" {
			yes = true
		} else {
			rest = append(rest, a)
		}
	}
	if len(rest) != 1 {
		return nil, usagef("delete: want exactly one slice")
	}
	if err := needSlice("delete", rest[0]); err != nil {
		return nil, err
	}
	id := rest[0]
	return func(ctx context.Context, s *session) error {
		sl, err := s.getSlice(ctx, id)
		if err != nil {
			return err
		}
		var runs []gen.Run
		var token *string
		for {
			r, err := s.api.ListRunsWithResponse(ctx, &gen.ListRunsParams{ApiVersion: APIVersion, PageToken: token})
			if err := expect(r, err, http.StatusOK); err != nil {
				return err
			}
			if r.JSON200 == nil {
				return failf(exitFailed, "engine: the run list has no body")
			}
			for _, run := range r.JSON200.Items {
				if run.Slice == id {
					runs = append(runs, run)
				}
			}
			if r.JSON200.NextPageToken == nil {
				break
			}
			token = r.JSON200.NextPageToken
		}
		fmt.Fprintf(s.out, "This erases slice %s %q and its records:\n", sl.Id, sl.Title)
		if sl.ArtifactCount != nil {
			fmt.Fprintf(s.out, "  stored artifacts: %d\n", *sl.ArtifactCount)
		}
		if len(runs) == 0 {
			fmt.Fprintln(s.out, "  runs: none")
		}
		for _, run := range runs {
			outcome := ""
			if run.Outcome != nil {
				outcome = ", " + string(*run.Outcome)
			}
			fmt.Fprintf(s.out, "  run %s (%s%s, project %s) and its stored artifacts\n", run.Id, run.Status, outcome, run.Project)
		}
		fmt.Fprintln(s.out, "Project folders are kept.")
		if !yes {
			fmt.Fprintf(s.out, "Type the slice ID (%s) to erase it: ", id)
			answer, _ := s.readLine()
			if answer != id {
				fmt.Fprintln(s.out)
				return failf(exitFailed, "delete: the answer was not %s; nothing was erased", id)
			}
		}
		r, err := s.api.DeleteSliceWithResponse(ctx, id, &gen.DeleteSliceParams{ApiVersion: APIVersion})
		if err := expect(r, err, http.StatusAccepted); err != nil {
			return err
		}
		if _, err := s.finish(ctx, r.JSON202); err != nil {
			return err
		}
		fmt.Fprintf(s.out, "Erased %s.\n", id)
		return nil
	}, nil
}
