# Model gateway

The gateway gives agent containers access to the Anthropic Messages API without ever holding the
provider credential, and refuses everything else. It is the official nginx image (the `nginx` pin
in `../images.lock`) plus two config templates; there is no custom code.

## Needs

| What | How |
|------|-----|
| Credential file | The provider key as a read-only mounted file, at `/run/secrets/anthropic_api_key` (a Compose secret) unless `GATEWAY_CREDENTIAL_FILE` names another path (that variable holds only the path). Passed to the gateway alone; never in the image, the repo, any other container or an environment variable. |
| `credential.envsh` | Mounted read-only and executable at `/docker-entrypoint.d/05-credential.envsh`. The image's entrypoint sources it before rendering the templates; it exports `ANTHROPIC_API_KEY` in the entrypoint's own shell only. If the file is missing, unreadable or empty, the container exits 1 with `gateway: credential file '<path>' is missing, unreadable or empty`. |
| `GATEWAY_SERVER_NAME` | The one Host name callers use for the gateway (for example `gateway` in Compose, `localhost` in a test). |
| Templates | `default.conf.template` and `provider.inc.template` mounted read-only at `/etc/nginx/templates/`. The image renders them into `/etc/nginx/conf.d/` at start. |
| `NGINX_ENVSUBST_FILTER` | Set to `^(ANTHROPIC_API_KEY\|GATEWAY_SERVER_NAME)$` so only these two variables are substituted. |
| Network | Outbound DNS (Docker's resolver at `127.0.0.11`) and HTTPS to `api.anthropic.com:443`. Listens on port **8080** (plain HTTP, internal network only). |
| Hardening | Runs as `--user 101:101` with `--read-only`, all capabilities dropped, `no-new-privileges`. Writable `tmpfs` (owned by 101:101) at `/etc/nginx/conf.d`, `/var/cache/nginx`, `/var/run` and `/tmp`. |

## Run it for a test

```sh
IMG=$(awk '$1=="nginx"{print $2}' src/deploy/images.lock)
docker run -d --name gateway --read-only --user 101:101 --cap-drop ALL \
  --security-opt no-new-privileges \
  --tmpfs /etc/nginx/conf.d:uid=101,gid=101,mode=0755 \
  --tmpfs /var/cache/nginx:uid=101,gid=101 --tmpfs /var/run:uid=101,gid=101 \
  --tmpfs /tmp:uid=101,gid=101 \
  -v "$PWD/src/deploy/gateway:/etc/nginx/templates:ro" \
  -v "$PWD/src/deploy/gateway/credential.envsh:/docker-entrypoint.d/05-credential.envsh:ro" \
  -v /path/to/keyfile:/run/secrets/anthropic_api_key:ro \
  -e GATEWAY_SERVER_NAME=localhost \
  -e 'NGINX_ENVSUBST_FILTER=^(ANTHROPIC_API_KEY|GATEWAY_SERVER_NAME)$' \
  -p 127.0.0.1:18080:8080 "$IMG"
```

Callers send `POST http://<GATEWAY_SERVER_NAME>:8080/v1/messages` (or `/v1/messages/count_tokens`)
with their own `anthropic-version` header and body; the gateway sets `Host: api.anthropic.com`
and `x-api-key` itself, overrides any `x-api-key` the caller sends, and blanks `Authorization` and `Cookie`. Anything else the caller
sent is passed on. TLS to the provider is verified.

## Refusals

| Request | Status |
|---------|--------|
| Host header not equal to `GATEWAY_SERVER_NAME` (any other host, including `api.anthropic.com` or an IP) | **421** `gateway: host not allowed` |
| Any path other than `/v1/messages` and `/v1/messages/count_tokens` | **403** `gateway: path not allowed` |
| Any method other than POST on those two paths | **403** |

The upstream is fixed in the config (`api.anthropic.com`); a request cannot choose it. A
request with a valid host and path while the provider is unreachable returns 502 or 504.

## Limits

- No access log, so no headers or bodies are logged; the error log (stderr, level warn) holds
  nginx errors only.
- No rate or budget limit: the provider-side spending cap on the key is the only budget.
- Plain HTTP between agents and gateway; encryption between local containers is an approved
  exception (research D16).
- The key does not appear in `docker inspect`, but it is still in the rendered config
  (`/etc/nginx/conf.d/provider.inc`) in the gateway's tmpfs and in the mounted file; keep both
  out of reach of agents (no exec into the gateway, no shared mounts).
- The provider's IP is re-resolved every 30 seconds; DNS must work inside the gateway container.
