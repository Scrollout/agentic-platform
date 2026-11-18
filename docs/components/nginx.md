---
type: Component
title: nginx
description: "The web server image the model gateway and the run page's forwarder are made of: official image plus config templates, no custom code."
tags: [components, gateway, network]
status: draft
resource: https://github.com/nginx/nginx
sources:
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research
  - id: gateway-readme
    resource: ../../src/deploy/gateway/README.md
    title: Model gateway README
  - id: nginx-docs
    resource: https://nginx.org/en/docs/
    title: nginx documentation
  - id: adr-0062
    resource: /decisions/0062-run-page-reached-remotely-over-tls-behind-a-sign-in-secret.md
    title: "ADR 0062: The run page is reached from other machines only over TLS behind a sign-in secret"
  - id: nginx-ref
    resource: /references/nginx-access-control.md
    title: nginx access control reference
  - id: deploy-readme
    resource: ../../src/deploy/README.md
    title: Deployment README, Compose and the launcher
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-18T21:00:00Z }
---

# Responsibility

Two roles, both the official `nginx` image with templates only. The **model gateway** gives agent
containers access to the Anthropic Messages API without their holding the provider key, and refuses
everything else. The **page forwarder** is a static nginx that alone publishes the run page's
port on 127.0.0.1. With `ap up --listen` (feature 003, [ADR 0062](/decisions/0062-run-page-reached-remotely-over-tls-behind-a-sign-in-secret.md)) the
forwarder also serves the page to other machines over TLS behind a sign-in secret, rate-limited
and limited to the given names ([access control reference](/references/nginx-access-control.md)).
Licence BSD-2, OpenSSF Scorecard 7.9 (research D22).

Sources: [Feature 001 research](../../specs/001-prose-to-accepted-slice/research.md), [Model gateway README](../../src/deploy/gateway/README.md), [ADR 0062](/decisions/0062-run-page-reached-remotely-over-tls-behind-a-sign-in-secret.md), [nginx access control reference](/references/nginx-access-control.md).

# Interfaces

The gateway listens on port 8080 on internal networks and passes `POST /v1/messages` and
`POST /v1/messages/count_tokens` to `api.anthropic.com` over verified TLS. It sets `x-api-key`
itself, overriding a caller's, and blanks `Authorization` and `Cookie`.

The forwarder serves the run page on 127.0.0.1. With remote access it answers only over TLS 1.2
or 1.3 on the `--listen` address: 400 for plain HTTP, 421 for a name not given, 401 without user
`ap` and the session's secret, and 429 beyond 10 requests per second per client address with a
burst of 20; it sets the Host to the page's own.

# Dependencies

HTTPS egress to the provider (the gateway is the only container on the `egress` network) and the
credential file, mounted into the gateway alone. [WireMock](/components/wiremock.md) stands in for it
in tests.

# Configuration

- Image: the `nginx` pin in `src/deploy/images.lock` (`nginx:1.29-alpine`, by digest); no cosign
  signature (research D24).
- Templates `src/deploy/gateway/default.conf.template` and `provider.inc.template`, `credential.envsh`,
  and `NGINX_ENVSUBST_FILTER` limiting substitution to the key and the server name.
- User from the host (to read the mode-600 key file), read-only root, all capabilities dropped,
  `no-new-privileges`, tmpfs for writable paths, 128 MB memory.
- The forwarder: `src/deploy/page-proxy/nginx.conf`; with remote access `compose.remote.yaml`
  makes it run as the developer's user with `page-proxy/nginx-remote.conf` and the state folder's
  `page-remote/` (certificate and key, the secret's SHA-512 crypt hash from the image's `mkpasswd`,
  and `names.conf`) mounted read-only.

Sources: [Model gateway README](../../src/deploy/gateway/README.md), [Deployment README, Compose and the launcher](../../src/deploy/README.md).

# Operations

`src/deploy/gateway/test_gateway.sh` tests the gateway. `scripts/tests/test_page_remote.py` tests
the forwarder's remote mode on the pinned image.
