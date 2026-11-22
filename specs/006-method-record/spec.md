# Feature Specification: Results are reused only under the method that judged them

**Feature Branch**: `006-method-record`

**Created**: 2026-11-22

**Status**: Draft

**Input**: Backlog tasks T136 and T144 (owner chose T136 as the next work, 2026-11-22; T144 shares
its design). T136 (HIGH): "The reuse key and the run record name the method that judged a result,
not only the pattern's id@version: `pattern.yaml` has changed in six commits and is still `1.0.0`,
and the key does not cover the check and procedure code, the template or the Docker Agent pin, so
after an upgrade a run reuses steps accepted under old checks and the record cannot say which
implementation decided." T144: "Record the pattern id@version with a slice's check results and,
when a loaded slice's differs from the engine's pattern, run its checks again (stored results are
restored as they are)."

**Serves needs** ([problem space](../../docs/overview/problem-space.md)): N-050 (the same method
on the same input gives the same accepted result), N-053 (every result can be traced to the
method, inputs, model and rules that produced it), N-052 (a proven result is reused, not
generated again).
**Uses terms** ([glossary](../../docs/overview/glossary.md)): Slice, Pattern, Run, Step, Check,
Accepted result, Method.
**Builds on**: [feature 001](../001-prose-to-accepted-slice/spec.md) FR-009 (pattern fit),
FR-018 (record), FR-019 (reuse), and its research D8 (run records and reuse); Constitution
Principle XX ("record the method, inputs, model, and rule versions").

## User Scenarios & Testing *(mandatory)*

The user is the developer who builds slices with the platform, and the project owner who
upgrades it. Today a result accepted by one version of the platform is reused, or a slice's
stored check results are shown, after the platform's checks, template or steps have changed,
because only the pattern's declared version is compared, and that version is not changed when
they change.

### User Story 1 - A step's result is reused only under the method that accepted it (Priority: P1)

When the developer builds a slice again after the platform was upgraded, a step whose accepted
result was judged by a different method runs again and is judged by the current checks; a step
whose method is unchanged is still reused.

**Why this priority**: reusing a result the current checks never judged accepts work without its
checks passing (Principle XX), which the platform exists to prevent.

**Independent Test**: with a store holding an accepted result for a step, change one part of the
method that judges the step and start the same run: the step runs again; with nothing changed,
the step is reused.

**Acceptance Scenarios**:

1. **Given** an accepted result for a step and an unchanged method and input, **When** the run
   reaches that step, **Then** the result is reused, as today.
2. **Given** an accepted result for a step and a platform whose pattern declaration, template,
   checks, fixed procedures, step agents or pinned agent runtime differ from the ones that
   accepted it, **When** the run reaches that step, **Then** the step runs again and its result is
   judged by the current checks.
3. **Given** a review round left open under one method, **When** a later run under a different
   method reaches that round, **Then** the round runs again rather than reusing the open result.
4. **Given** results stored before this feature, which name no method, **When** a run reaches
   their steps, **Then** they are not reused.

---

### User Story 2 - A slice's check results are judged again under a new method (Priority: P1)

When the platform loads a slice whose stored check results were produced by a different method,
it runs that slice's checks again and shows the new results; results from the current method are
shown as stored.

**Why this priority**: a slice shown as fitting the pattern, or passing its checks, on an old
pattern's judgement misleads the developer and lets a build start on a stale verdict (FR-009).

**Independent Test**: store a slice with check results under one method, load it under another:
its checks are run again and the results name the current method; load it under the same method:
the stored results are shown unchanged.

**Acceptance Scenarios**:

1. **Given** a slice whose check results name the current method, **When** it is loaded,
   **Then** its results are shown as stored.
2. **Given** a slice whose check results name another method, or none, **When** it is loaded,
   **Then** its checks run again before its results are shown, and the new results name the
   current method.
3. **Given** a slice whose checks, run again, now fail, **When** the developer asks to build it,
   **Then** the build is refused as for any slice whose checks fail; the slice's content is kept,
   and the approval of a part whose check now fails is withdrawn and recorded, as after any check.

---

### User Story 3 - Every result names the method that produced it (Priority: P2)

The developer, or anyone comparing two runs, can read from the run record and from a slice which
method judged each accepted step and each check result, and tell whether two runs used the same
method.

**Why this priority**: without it a result cannot be traced or reproduced (N-053), but reuse
(US1, US2) is what makes stale results harmful.

**Independent Test**: run a slice, read its run record and its slice: every accepted, reused or
rejected step and every check result names a method identity, and two runs on the same platform
name the same one.

**Acceptance Scenarios**:

1. **Given** a finished run, **When** its record is read, **Then** every step outcome names the
   method identity it was judged under, with the pattern's id and declared version.
2. **Given** two installations built from the same source, **When** each runs, **Then** both
   name the same method identity.
3. **Given** two platform versions that differ in any part of the method, **When** each runs,
   **Then** they name different method identities.

---

### Edge Cases

- A change outside the method (documentation, the developer's terminal client, the page, tests or
  evals that are not shipped) must not change the method identity, so reuse is not lost for it.
- A change to the model a step uses or to the step's agent definition already prevents reuse
  today; it must still do so.
- A rebuild of the same source on another machine or at another time must give the same method
  identity (no build time, path or host in it).
- A method change while a run is in progress: a run keeps the method it started with until it
  ends or is resumed; a resumed run after an upgrade judges its remaining steps under the new
  method and does not reuse results from the old one.
- A slice loaded under a new method whose checks cannot run (for example a check's service is
  down) is shown with no current check results, never with its old ones, and cannot be built
  until its checks run.
- The pattern's declared version may still be changed by hand; a method change without a version
  change must still be detected.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The platform MUST give its method one identity that covers every part that can
  change what is produced or accepted: the pattern declaration, the template, the step
  definitions and their agents, the checks, the fixed procedures and the pinned agent runtime.
- **FR-002**: The method identity MUST be the same for every build of the same source and MUST
  differ when any part named in FR-001 differs; parts outside FR-001 MUST NOT change it.
- **FR-003**: A step's accepted or open result MUST be reused only when it was produced under the
  current method identity, besides the conditions that apply today (same step, inputs, agent
  definition, model and review round).
- **FR-004**: Every step outcome in a run record (made, accepted, rejected, reused) MUST name the
  method identity it was judged under, besides the pattern's id and declared version.
- **FR-005**: A slice's stored check results MUST name the method identity that produced them.
- **FR-006**: When a slice is loaded and its check results name a different method identity, or
  none, the platform MUST run its checks again and keep the new results before showing them; it
  MUST NOT show the old results as current.
- **FR-007**: Re-checking a slice MUST NOT change its content; its approvals follow the same rule
  as after any check (a part whose check fails loses its approval, recorded in its history), and a
  check that now fails MUST block a build as any failing check does.
- **FR-008**: Results and check results stored before this feature MUST be treated as produced by
  an unknown method: not reused, and checked again.
- **FR-009**: The developer and the client MUST be able to read the method identity of a run and
  of a slice's check results through the platform's existing views of runs and slices.
- **FR-010**: A build of the platform MUST fail when the method identity it carries does not match
  the parts it ships (so the identity cannot go stale by hand).

### Key Entities

- **Method**: the parts of the platform that decide what a step produces and whether it is
  accepted (FR-001), named by one identity.
- **Accepted result**: a step's output that passed its checks, or a review round left open, kept
  for reuse; now also names the method identity that judged it.
- **Check result**: the outcome of one check on a slice's part; now also names the method
  identity that produced it.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: After a change to any one part of the method, 0 steps of a repeated run are reused
  and every slice's checks are run again; with no change, every step of a repeated run is reused
  as before (same count as today).
- **SC-002**: Two builds of the same source give the same method identity, and a change to a part
  outside the method (documentation, the terminal client) leaves it unchanged.
- **SC-003**: 100% of step outcomes in a run record and of a slice's check results name a method
  identity.
- **SC-004**: A run that reuses every step takes no longer than today, within 1 second.

## Assumptions

- The method is compared as a whole: a change to any part of it makes every result judged under
  the old method run again, even for a step that part does not touch. This trades some repeated
  model calls after an upgrade for one simple, sound rule; finer reuse can come later.
- The model id and the agent definition digest stay in the reuse rule as they are; the method
  identity is added to them.
- The developer's own slice content and approvals are inputs, not part of the method.
- One developer on one machine, one pattern in the catalogue (feature 001's scope) still holds;
  with one pattern, a slice's method is the engine's.
- How the identity is computed or declared (a digest of the shipped parts, or a checked version
  bump) is decided in the plan's research.
