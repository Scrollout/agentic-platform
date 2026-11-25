---
type: Reference
title: Go JSON duplicate names
description: How Go's encoding/json (v1) and encoding/json/v2 treat an object that holds a name twice or a name in another case, and where v2 is stable, as read on 2026-11-22 and 2026-11-25.
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
  - id: research-010
    resource: ../../specs/010-strict-requests/research.md
    title: Feature 010 research, E2
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-25T12:00:00Z }
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

# Names in another case

- `encoding/json` (v1) matches an object's names to struct fields preferring an exact match but
  accepting one that differs only in case: `{"TEXT":"x"}` fills a field tagged `text`, and of
  `{"text":"a","TEXT":"b"}` the last, `"b"`, is kept. A missing member leaves the zero value
  without an error (reproduced with Go 1.27.1).
- `encoding/json/v2` matches names exactly by default; its sources still carry
  `//go:build goexperiment.jsonv2` in Go 1.27.1 as installed here.

Sources: [Feature 010 research, E2](../../specs/010-strict-requests/research.md).
