# Feature Specification: Start the platform with one command, and reach its run page from another machine

**Feature Branch**: `003-one-command-start`

**Created**: 2026-11-18

**Status**: Draft

**Input**: Project owner, 2026-11-18, after reading how to start the MVP (generate the code, set the
signing password, build and sign seven images one by one, make a projects folder with the right
mode, then `ap up --credentials <file> --projects <folder>`): "Build and start is complex. Apply
abstraction principles." Then: "I need to be able to connect on the VM on external IP 0.0.0.0"
and, after the SSH tunnel was offered, "add the direct remote access". Constitution XVII (Converge, don't sequence: one command that makes
reality match the declarations, safe to run again, with no order of steps to know) and XVIII
(Abstractions say what, not how: no name or option that exists only because of how it is built).

**Serves needs** ([problem space](../../docs/overview/problem-space.md)): N-005 (run on the user's
own infrastructure), for one developer's machine, which may be a remote machine such as a VM.
**Uses terms** ([glossary](../../docs/overview/glossary.md)): Slice, Run.
**Builds on**: [feature 001](../001-prose-to-accepted-slice/spec.md); it changes how the platform is
started and where its run page can be reached, not what it does.

## Clarifications

### Session 2026-11-18

- Q: Is the signing key's password asked once per start that has to build (or read from the
  environment), or made and kept by the platform so the developer is never asked? → A: Asked once
  per start that has to build, or read from the environment; the platform does not keep it, so
  today's protection of the key stays (owner).
- Q: May the run page be reached from another machine, without an SSH tunnel? → A: Yes, directly,
  as an explicit choice at start (owner). Constitution XI (encrypted to every client), V
  (authenticated access) and XIV (a less safe setting is explicit and audited) shape User Story 4.
- Q: After `/speckit-analyze` found that building inside `ap up` changes about seven existing test
  files, which command is the start command? → A: A new `ap start`; `ap up` keeps what it does
  today and is what CI and the test suites use (owner, option B). Remote access options go on
  `ap up` and `ap start` passes them on.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - One command takes a fresh clone to a started platform (Priority: P1)

A developer clones the repository on a machine that has a container runtime with gVisor and a
model key in a file. They run one command. The platform builds what it needs, verifies it and
starts. They then describe their first slice. They never ran a build, generator or signing step by
hand, and never had to know in which order such steps run.

**Why this priority**: Today the developer runs about ten commands in a fixed order before the
first `describe`, which Principle XVII forbids for an operation; it is the first thing every user
meets.

**Independent Test**: On a clean clone, run the start command once; the platform is started and
`describe` works. Count the commands typed before `describe`: one.

**Acceptance Scenarios**:

1. **Given** a fresh clone and a model key at the default place, **When** the developer runs the
   start command with no options, **Then** the platform builds and signs every part it needs,
   verifies each, starts, and says where the run page is.
2. **Given** a started platform and no change to the source, **When** the developer runs the start
   command again, **Then** nothing is built, nothing is restarted, and it says the platform is
   already started.
3. **Given** a change to the source of one part, **When** the developer runs the start command,
   **Then** only the parts built from that source are built again, and the platform runs the new
   ones.
4. **Given** the platform was stopped, **When** the developer runs the start command, **Then** it
   starts again with its run records and projects kept, building nothing that has not changed.

---

### User Story 2 - Missing prerequisites are named, with the fix (Priority: P2)

When something the platform needs from the machine is missing or wrong, the start command stops
before it starts anything and says what is missing and how to fix it.

**Why this priority**: A start that fails half way, or with a message from a tool underneath, is as
hard to use as the sequence it replaces.

**Independent Test**: Remove each prerequisite in turn; the start command stops with a message that
names it and the fix, and starts nothing.

**Acceptance Scenarios**:

1. **Given** no model key at the default place and none given, **When** the developer runs the
   start command, **Then** it says where it looked and how to give one, and starts nothing.
2. **Given** a container runtime that is too old, or no gVisor runtime, **When** the developer runs
   the start command, **Then** it names what is missing and the install guide, and starts nothing.
3. **Given** no projects folder, **When** the developer runs the start command, **Then** it makes
   the default one, readable by the platform's containers.
4. **Given** a projects folder the platform's containers cannot open, **When** the developer runs
   the start command, **Then** it names the folder and its mode and starts nothing.

---

### User Story 3 - What the developer types says what, not how (Priority: P3)

The commands and their help speak of the developer's work (start, stop, describe, build, replay),
never of how the platform is put together (images, registries, signing, generated code, step
workers).

**Why this priority**: Principle XVIII; every internal name in the interface is something the
developer has to learn and can get wrong.

**Independent Test**: Read the start command's usage and the quickstart: no step, option or
message names an internal part, except a diagnosis the developer must act on.

**Acceptance Scenarios**:

1. **Given** the quickstart, **When** a developer follows it from a clean clone, **Then** it has
   one start command, then the slice and build commands, and no build, generator or signing step.
2. **Given** the start command's usage, **When** the developer reads it, **Then** every option is
   about where their key and projects are, or whether other machines may reach the run page, and
   each has a default.

---

### User Story 4 - Reach the run page from another machine (Priority: P2)

The developer runs the platform on a remote machine, such as a VM, and wants to inspect runs from
their own computer's browser, without an SSH tunnel. They start the platform with an explicit
choice to accept connections from other machines. The start command shows that choice, the
address to open, the secret to sign in with and how to recognise the platform's certificate. From
their computer, the developer opens the address over an encrypted connection, signs in with the
secret and sees the run page as on the machine itself. Anyone without the secret sees nothing of
any run.

**Why this priority**: The owner runs the platform on a VM; without this, the run page is only
reachable from the VM itself or through a tunnel.

**Independent Test**: Start the platform with remote access on a VM. From another machine, open
the address: with the secret the run page shows the runs; without it, or over an unencrypted
connection, nothing of any run is shown. Start it without the choice: the page is not reachable
from another machine.

**Acceptance Scenarios**:

1. **Given** a start with no remote-access choice, **When** another machine connects to the run
   page's port, **Then** it cannot reach the page, as in feature 001.
2. **Given** a start that chooses remote access, **When** it finishes, **Then** it says that the
   page accepts connections from other machines, and prints the address, the sign-in secret and
   the certificate's fingerprint; the choice is recorded with its time and address.
3. **Given** remote access, **When** the developer opens the address from another machine and
   signs in with the secret, **Then** they see the same runs, records and comparisons as on the
   machine itself, and still cannot change anything.
4. **Given** remote access, **When** someone connects without the secret, with a wrong one, or
   without encryption, **Then** they see no run content, and repeated failed sign-ins are slowed
   down.
5. **Given** a platform started with remote access, **When** the developer stops it and starts it
   again without the choice, **Then** the page is reachable only from the machine again, and the
   old secret no longer works.

### Edge Cases

- A build of one part fails: the start command stops, names the part and shows the build's own
  error, and starts nothing; the parts already built are kept for the next try.
- The start command is interrupted while it builds: the next run finishes the work and changes
  nothing that was already done.
- The signing key does not exist yet: it is made on the first start, as today.
- The platform is already started with another key file or projects folder: the start command
  refuses, as today, and says to stop it first.
- No network on the first start: the parts that need the network to build (the pinned tools and
  modules) cannot be built; the start command says so and starts nothing. Once built, the platform
  starts without the network.
- Remote access is chosen while the platform is already started without it (or the other way
  round): the start refuses and says to stop it first, as for another key file.
- The remote address or port is already in use, or is not an address of this machine: the start
  names it and starts nothing.
- The certificate the platform made for remote access expires: the start makes a new one and prints
  its new fingerprint.
- The developer reaches the page by the machine's name rather than its address: the page accepts
  the names and addresses the developer gave at start, and refuses any other.
- Two start commands at once: one builds, the other waits for it or refuses; no part is built
  twice at the same time and no record is lost.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: One command MUST take the machine from any state (fresh clone, stopped, started,
  source changed) to a started platform that runs the parts built from the current source.
- **FR-002**: Running the start command again with nothing changed MUST build nothing and change
  nothing.
- **FR-003**: The start command MUST build again only the parts whose source or pins changed since
  they were last built, and MUST verify every part before anything starts, as feature 001 does.
- **FR-004**: The start command MUST check the machine's prerequisites (the container runtime's
  version, the gVisor runtime, the model key file, the projects folder) before it builds or starts
  anything, and MUST name each one that fails with its fix.
- **FR-005**: The model key file and the projects folder MUST each have a default, so the start
  command needs no option; options MAY override them.
- **FR-006**: The start command MUST make a missing default projects folder, readable by the
  platform's containers, and MUST refuse one they cannot open, naming its mode.
- **FR-007**: The signing key's password MUST be asked once per start, and only when the start
  has something to build (entered a second time to confirm only when the signing key is first
  made); when the environment gives it (`COSIGN_PASSWORD`, as in CI), it MUST NOT be asked. The
  platform MUST NOT store the password.
- **FR-008**: The start command MUST say what it will do before a long step (which parts it
  builds) and what it did at the end (built, verified, started).
- **FR-009**: The developer-facing commands, their usage and the quickstart MUST NOT require any
  build, generator, signing or registry step; those MAY stay available for the platform's own
  development and CI.
- **FR-010**: Stopping the platform MUST stay one command that keeps run records and projects.

- **FR-011**: Everything feature 001 delivered after the platform is started MUST work unchanged.
- **FR-018**: The start command MUST be able to show, without building, signing, recording or
  starting anything, which prerequisites fail, which parts it would build, and whether it would
  start the platform (constitution XVII, Prove agreement).

#### Remote access to the run page

- **FR-012**: By default the run page MUST be reachable only from the machine the platform runs
  on, as in feature 001.
- **FR-013**: The start command MUST accept an explicit choice to make the run page reachable from
  other machines, naming the address (all of the machine's addresses, or one) and optionally the
  names the page answers to; the platform MUST say so at start and record the choice with its time
  and address (Principle XIV).
- **FR-014**: With remote access, every connection to the run page MUST be encrypted; an
  unencrypted request MUST get no run content. The platform MUST make its own certificate when
  the developer gives none and MUST print its fingerprint; the developer MAY give their own
  certificate and key.
- **FR-015**: With remote access, every request MUST be authenticated with a sign-in secret the
  platform makes at start and shows only to the developer; a request without a valid one MUST get
  no run content. The secret MUST change when the platform is stopped and started again.
- **FR-016**: With remote access, failed sign-ins and requests MUST be rate-limited per source
  address, and each failed sign-in MUST be logged with its time and source, never with the secret
  tried.
- **FR-017**: Remote access MUST expose only the read-only run page: nothing reachable from another
  machine may change a slice, a run or a project, or reach the engine, the model gateway or any
  other part.

### Key Entities

- **Platform part**: one container image the platform runs, built from the repository's source
  and pins, signed and verified before it starts.
- **Remote-access choice**: the recorded decision, made at start, to let other machines reach the
  run page: its address, the names it answers to, its time.
- **Sign-in secret**: the secret the platform makes at a start with remote access; it proves a
  viewer is the developer, and changes at every new start.
- **Prerequisite**: something the platform needs from the machine and cannot provide itself: the
  container runtime with Compose, the gVisor runtime, a model key file.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: From a clean clone on a machine with the prerequisites, the developer types exactly
  one command before `describe`.
- **SC-002**: A second start with nothing changed builds nothing and finishes in under 30 seconds.
- **SC-003**: After a change to one part's source, a start builds only that part and the parts
  built from it.
- **SC-004**: Each missing prerequisite (key file, gVisor, too old a container runtime, an unusable
  projects folder) stops the start before anything starts, with a message that names it and its
  fix, in 4 out of 4 cases.
- **SC-005**: The quickstart's start section is one command, and every feature 001 acceptance,
  planted and end-to-end suite passes.
- **SC-006**: From another machine, the run page opens over an encrypted connection with the
  sign-in secret, and shows no run content without it, with a wrong one or unencrypted, in 4 out of
  4 cases.
- **SC-007**: Started without the remote-access choice, the run page is unreachable from another
  machine.

## Assumptions

- The start command is `ap start`. `ap up`, which starts parts already built, stays as it is for
  CI and the test suites (Clarifications, 2026-11-18).
- The prerequisites stay what feature 001 needs: a container runtime with Compose (Docker 28 or
  later) and the gVisor runtime, installed once by the developer; installing them is out of scope.
- The default key file is `~/.config/agentic-platform/anthropic_api_key` and the default projects
  folder is `~/projects`, the paths the quickstart already uses.
- The first start builds every part and needs the network for the pinned tools and modules, as
  `build.sh` does today; how long it takes is measured in the plan.
- Images stay signed and verified (ADR 0028); this feature only stops asking the developer to run
  the steps.
- `build.sh`, `generate.sh` and the other development scripts stay for the platform's own
  development and CI; this feature does not remove them.
- The slice and build commands (`describe`, `questions`, `approve`, `build`, ...) are out of scope;
  only starting and stopping change.
- Remote access covers the read-only run page only. The terminal client stays on the machine the
  platform runs on (over SSH for a VM); reaching the engine API from another machine needs the web
  console of a later feature (ADR 0027).
- One developer, one secret: no accounts, roles or multiple viewers with separate access.
- A certificate the platform makes for itself is not trusted by browsers; the developer compares
  the printed fingerprint once, or gives a certificate of their own. Getting a public certificate
  (for example through ACME) is out of scope.
- Firewalls between the developer's computer and the machine (for example a cloud VM's security
  group) are the developer's to open; the platform only says which port it listens on.
