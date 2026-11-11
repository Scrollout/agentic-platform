---
type: Decision
title: "ADR 0017: Principle XIII - Secure Supply Chain"
description: Why the constitution adopts Principle XIII, 'Secure Supply Chain'.
tags: [decisions, constitution]
status: draft
decision: accepted
sources:
  - id: constitution
    resource: ../../.specify/memory/constitution.md
    title: Agentic Platform Constitution v1.4.0
  - id: slsa
    resource: https://slsa.dev/spec/v1.2/
    title: "SLSA"
  - id: sigstore
    resource: https://www.sigstore.dev/
    title: "Sigstore"
  - id: nist-sp-800
    resource: https://csrc.nist.gov/pubs/sp/800/218/a/final
    title: "NIST SP 800-218A"
  - id: cisa-kev
    resource: https://www.cisa.gov/known-exploited-vulnerabilities-catalog
    title: "CISA KEV"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T18:45:00Z }
---

# Origin

The project owner ratified the constitution, with Principle XIII, "Secure Supply Chain", on
2026-11-11. Constitution v1.4.0 moved this rationale out of the constitution into this record,
without changing any rule.

# Context

A platform assembled from adopted open source, external models, and third-party tools is only as
secure as what it pulls in (SLSA[^slsa]; Sigstore[^sigstore]; NIST SP 800-218A[^nist-sp-800]; CISA
KEV[^cisa-kev]).

# Decision

Adopt Principle XIII, "Secure Supply Chain", exactly as the
[constitution](../../.specify/memory/constitution.md) states it. The constitution holds the rules;
this record holds why they exist and what backs them.

# Consequences

- Changing the principle needs a new decision that supersedes this one, approved by the project
  owner (constitution, Governance).

[^slsa]: SLSA
[^sigstore]: Sigstore
[^nist-sp-800]: NIST SP 800-218A
[^cisa-kev]: CISA KEV
