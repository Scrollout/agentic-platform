---
type: Component
title: WireMock
description: The HTTP stub server used as the recorded model in tests, answering the model gateway's paths only from recordings, and as the owner's recorder in front of the real gateway.
tags: [components, testing, model]
status: draft
resource: https://github.com/wiremock/wiremock
sources:
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D19, D22 and D26
  - id: wiremock-docs
    resource: https://wiremock.org/docs/
    title: WireMock documentation
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-12T14:00:00Z }
---

# Responsibility

Stands in for the model gateway in tests, so they run against a recorded model with no key and no
cost (research D19): it answers each request from a recorded exchange and refuses everything else.
In the owner's recording sessions it proxies to the real gateway and records the exchanges. It is
WireMock's official image, licensed Apache-2.0, adopted as-is (Principle VI, research D26).

Sources: [Feature 001 research, D19 and D26](../../specs/001-prose-to-accepted-slice/research.md).

# Interfaces

- The gateway's interface on port 8080 under the alias `gateway`: `POST /v1/messages` and
  `POST /v1/messages/count_tokens`, Anthropic Messages API, answered only where a recording's
  method, exact URL (path and query as one string) and JSON body are equal to the request's; any
  other request gets 404.
- The admin API `/__admin`, behind a random per-container credential; the platform's scripts use
  it to wait for health, list misses and run a recording.

Sources: [WireMock documentation](https://wiremock.org/docs/).

# Dependencies

The container engine; when recording, the [model gateway](../../src/deploy/gateway/README.md).

# Configuration

- Image: the `wiremock` pin in `src/deploy/images.lock` (`3.13.2-3-alpine`, by digest).
- Recordings: stub-mapping files in `src/tests/recorded/recordings/`, mounted read-only.
- Runs only on an internal Docker network, as user 65532 with a read-only root, all capabilities
  dropped, `no-new-privileges`, 512 MB memory and 256 processes.

Sources: [Feature 001 research, D26](../../specs/001-prose-to-accepted-slice/research.md).

# Operations

`src/tests/recorded/replay.sh` starts, inspects and stops a replay server; `record.sh` records,
by the owner only, because it calls the provider. See `src/tests/recorded/README.md`.
