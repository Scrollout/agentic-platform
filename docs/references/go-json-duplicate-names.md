---
type: Reference
title: Go JSON duplicate names
description: How Go's encoding/json (v1) and encoding/json/v2 treat an object that holds a name twice, and where v2 is stable, as read on 2026-11-22 for the analyst's answers.
resource: https://www.rfc-editor.org/rfc/rfc7493#section-2.3
tags: [references, go, json]
status: draft
stale_after: 2027-05-22T00:00:00Z
sources:
  - id: rfc8259
    resource: https://www.rfc-editor.org/rfc/rfc8259#section-4
    title: RFC 8259, section 4
  - id: rfc7493
    resource: https://www.rfc-editor.org/rfc/rfc7493#section-2.3
    title: RFC 7493 (I-JSON), section 2.3
  - id: gojsonexp
    resource: https://raw.githubusercontent.com/go-json-experiment/json/master/README.md
    title: go-json-experiment/json README
  - id: research
    resource: ../../specs/007-analyst-answer-checks/research.md
    title: Feature 007 research, S3 and E1
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-22T22:00:00Z }
---

# The standards

- RFC 8259: names within an object SHOULD be unique; with duplicates the behaviour of the
  receiving software is unpredictable.
- RFC 7493 (I-JSON): objects MUST NOT have members with duplicate names.

Sources: [RFC 8259, section 4](https://www.rfc-editor.org/rfc/rfc8259#section-4); [RFC 7493, section 2.3](https://www.rfc-editor.org/rfc/rfc7493#section-2.3).

# Go

- `encoding/json` (v1) accepts duplicate names; decoding into a struct keeps the last value
  without an error (reproduced with Go 1.26.11).
- `encoding/json/v2` rejects duplicate names by default (`jsontext.AllowDuplicateNames` changes
  it). In Go 1.25 and 1.26 it exists only with `GOEXPERIMENT=jsonv2`; it is a stable API from
  Go 1.27.
- `go-json-experiment/json` (BSD-3-Clause) was v2's prototype and now mirrors it; it states no
  stability promise of its own.

Sources: [Feature 007 research, S3 and E1](../../specs/007-analyst-answer-checks/research.md); [go-json-experiment/json README](https://raw.githubusercontent.com/go-json-experiment/json/master/README.md).
