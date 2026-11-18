---
type: Reference
title: nginx access control for the run page
description: The nginx directives that serve TLS, ask for HTTP Basic authentication, limit requests per address and refuse unknown names, as proven on the pinned nginx image on 2026-11-18.
resource: https://nginx.org/en/docs/
tags: [references, nginx, tls, security]
status: draft
stale_after: 2027-05-18T00:00:00Z
sources:
  - id: auth-basic
    resource: https://nginx.org/en/docs/http/ngx_http_auth_basic_module.html
    title: nginx ngx_http_auth_basic_module
  - id: limit-req
    resource: https://nginx.org/en/docs/http/ngx_http_limit_req_module.html
    title: nginx ngx_http_limit_req_module
  - id: ssl
    resource: https://nginx.org/en/docs/http/ngx_http_ssl_module.html
    title: nginx ngx_http_ssl_module
  - id: generate-cert
    resource: https://go.dev/src/crypto/tls/generate_cert.go
    title: "Go source: crypto/tls/generate_cert.go"
  - id: docker-firewall
    resource: https://docs.docker.com/engine/network/packet-filtering-firewalls/
    title: "Docker: packet filtering and firewalls"
  - id: research
    resource: ../../specs/003-one-command-start/research.md
    title: Feature 003 research, S1 and R5
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-18T12:00:00Z }
---

# Directives

- `auth_basic` and `auth_basic_user_file` ask for a user and password and check them against a
  file; the file may hold `crypt()` hashes such as SHA-512 crypt (`$6$`).
- `limit_req_zone $binary_remote_addr` with `limit_req ... burst=N nodelay` limits requests per
  client address; `limit_req_status 429` sets the refusal.
- `listen ... ssl` with `ssl_certificate`, `ssl_certificate_key` and `ssl_protocols TLSv1.2
  TLSv1.3` serves TLS; a plain HTTP request to that port gets 400.
- A `default_server` block with `return 421` answers every Host the named server does not list.

Sources: [nginx ngx_http_auth_basic_module](https://nginx.org/en/docs/http/ngx_http_auth_basic_module.html), [nginx ngx_http_limit_req_module](https://nginx.org/en/docs/http/ngx_http_limit_req_module.html), [nginx ngx_http_ssl_module](https://nginx.org/en/docs/http/ngx_http_ssl_module.html).

# Proven on the pinned image

- The image is `nginx:1.29-alpine` (images.lock `nginx`). It has the SSL module, `auth_basic`,
  `limit_req` and busybox `mkpasswd -m sha512`.
- With the secret the page answered 200. Without it, or with a wrong one, 401. Plain HTTP got
  400, an unknown Host 421, and 40 wrong sign-ins in a row 401 twenty times, then 429.
- The error log names the user, client and "password mismatch", never the password tried.
- A location that answers with `return` does so before authentication runs, so a protected
  location must proxy or serve.
- Go's `generate_cert.go`, in the pinned `golang:1.26-alpine` with no network, made an ECDSA P-256
  certificate for given names and addresses in 5.3 s. It needs an executable temporary folder.

Sources: [Feature 003 research, S1 and R5](../../specs/003-one-command-start/research.md), ["Go source: crypto/tls/generate_cert.go"](https://go.dev/src/crypto/tls/generate_cert.go).

# Host firewall

Docker's published ports are reached through rules Docker adds ahead of host firewall rules
such as ufw's, so a port published on `0.0.0.0` is open on every address even when ufw denies it.

Sources: ["Docker: packet filtering and firewalls"](https://docs.docker.com/engine/network/packet-filtering-firewalls/).
