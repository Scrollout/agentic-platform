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
generated: { by: claude-code/claude-sonnet-5-5, at: 2026-11-13T12:00:00Z }
---

# Responsibility

Two roles, both the official `nginx` image with templates only. The **model gateway** gives agent
containers access to the Anthropic Messages API without their holding the provider key, and refuses
everything else. The **page forwarder** is a static nginx that alone publishes the run page's
port on 127.0.0.1. Licence BSD-2, OpenSSF Scorecard 7.9 (research D22).

Sources: [Feature 001 research](../../specs/001-prose-to-accepted-slice/research.md), [Model gateway README](../../src/deploy/gateway/README.md).

# Interfaces

The gateway listens on port 8080 on internal networks and passes `POST /v1/messages` and
`POST /v1/messages/count_tokens` to `api.anthropic.com` over verified TLS. It sets `x-api-key`
itself, overriding a caller's, and blanks `Authorization` and `Cookie`.

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

Sources: [Model gateway README](../../src/deploy/gateway/README.md).

# Operations

`src/deploy/gateway/test_gateway.sh` tests it. The forwarder is configured in `src/deploy/page-proxy/`.
