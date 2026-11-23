---
type: Reference
title: gosec hard-coded credentials
description: What gosec's G101 rule found in plain Go service code, its size and health, as measured on 2026-11-23 for feature 009.
resource: https://github.com/securego/gosec
tags: [references, go, security, linters]
status: draft
stale_after: 2027-05-23T00:00:00Z
sources:
  - id: research
    resource: ../../specs/009-no-hard-coded-values/research.md
    title: Feature 009 research, E2 and S7
  - id: gosec
    resource: https://github.com/securego/gosec
    title: securego/gosec repository
  - id: mnd
    resource: https://pkg.go.dev/github.com/tommy-muehle/go-mnd
    title: go-mnd package documentation
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-23T20:00:00Z }
---

# gosec G101

- gosec v2.30.0 (2026-10-23, Apache-2.0, OpenSSF Scorecard 6.6 on 2026-10-19) with
  `-include=G101` reported nothing for `const apiToken = "s3cr3t-Value-1234567890"`,
  `var password = "hunter2"`, a literal `SetBasicAuth` and a literal `Authorization` header.
- Its binary built with `-trimpath` is 96 MB.

Sources: [Feature 009 research, E2](../../specs/009-no-hard-coded-values/research.md); [securego/gosec repository](https://github.com/securego/gosec).

# go-mnd

- Reports numeric literals in argument, case, condition, operation, return and assign positions,
  minus a list of ignored numbers; it does not know where a value is declared and does not judge
  strings.

Sources: [go-mnd package documentation](https://pkg.go.dev/github.com/tommy-muehle/go-mnd).
