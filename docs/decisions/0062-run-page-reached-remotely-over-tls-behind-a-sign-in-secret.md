---
type: Decision
title: "ADR 0062: The run page is reached from other machines only over TLS behind a sign-in secret"
description: With ap up --listen, the pinned nginx forwarder serves the read-only run page to other machines over TLS, behind HTTP Basic authentication with a secret new per session, rate-limited per address and limited to the given names; each change is recorded; without --listen the page stays on 127.0.0.1.
tags: [decisions, deployment, security, network]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's request, 2026-11-18
  - id: research
    resource: ../../specs/003-one-command-start/research.md
    title: Feature 003 research, R5
  - id: plan
    resource: ../../specs/003-one-command-start/plan.md
    title: Feature 003 plan, Decisions and Threat model
  - id: nginx-ref
    resource: /references/nginx-access-control.md
    title: nginx access control reference
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-18T12:00:00Z }
---

# Origin

The project owner runs the platform on a VM and asked to reach it "on external IP 0.0.0.0"
(2026-11-18), then, offered an SSH tunnel, asked for "the direct remote access". Feature 003's
spec adds it as User Story 4, and its [plan](../../specs/003-one-command-start/plan.md) records
this decision as research R5.

Sources: [Project owner's request, 2026-11-18](https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np), [Feature 003 research, R5](../../specs/003-one-command-start/research.md).

# Context

- The run page has no sign-in and no encryption; it relies on 127.0.0.1 and a Host check.
- Constitution XI requires encryption to every client, V authenticated access, and XIV makes a
  less safe setting explicit, audited and rate-limited.
- The pinned nginx image already serves TLS, Basic authentication and `limit_req`, and has
  `mkpasswd`; the pinned Go image has `generate_cert.go`. An experiment on 2026-11-18 proved each
  promise on these images: 401 without or with a wrong secret, 400 for plain HTTP, 421 for an
  unknown name, 429 after the burst, and no secret in the log.

Sources: [Feature 003 research, R5](../../specs/003-one-command-start/research.md), [nginx access control reference](/references/nginx-access-control.md).

# Decision

- Remote access is off unless `ap up --listen <address>` turns it on.
- On, a Compose override changes only the page forwarder: it publishes on the given address,
  serves TLS, asks for user `ap` and a secret, answers 10 requests per second per address with a
  burst of 20, and answers only the given names.
- The certificate is the developer's, or one the platform makes for the names with no network
  (ECDSA P-256, 365 days, renewed before it expires); its SHA-256 fingerprint is printed.
- The secret has 256 random bits and is new at every start after `ap down`. It is shown only on the
  developer's terminal and kept in the session file and as a SHA-512 crypt hash.
- Each change of remote access is appended to `access.log`, without the secret.
- Only the read-only page is reachable; the terminal client stays on the machine.

# Consequences

- The owner can inspect runs on a VM from their own browser without a tunnel.
- No new component or pin: nginx and the Go image are reused.
- Whoever has the secret can read every run; there are no accounts.
- Browsers warn about the platform's own certificate until the developer accepts it.
- Docker's published ports bypass host firewall rules such as ufw's, so the port is open on the
  chosen address.

Sources: [Feature 003 plan, Decisions and Threat model](../../specs/003-one-command-start/plan.md).
