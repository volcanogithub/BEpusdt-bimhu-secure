# Phase B2-6: Deployment Security Hardening

Status: **PASS** for the validated production configuration, startup guards,
headers, non-root runtime, permissions and logging controls. The full multi-stage
Alpine image build could not be validated in this cloud instance because Docker
Hub rejected anonymous pulls with its rate limit. This is a recorded limitation,
not a successful image-build claim or a production deployment claim.

Repository: `volcanogithub/BEpusdt-bimhu-secure`.
Branch: `security/b2-hardening-v1.24.2`.
Parent baseline: `d6bc9cd8035c2af53bd571367eb203c4fc17d0e9`.
Evidence: GitHub cloud checkout, Go 1.26.2, PostgreSQL 17.11, Nginx 1.26.3,
Docker 28.4.0 / Compose 2.40.3, 2026-10-01.

## Recommended deployment and trust boundary

```text
Internet -> HTTPS Nginx :443 -> private HTTP BEpusdt :8080 -> private database
              TLS ends here      loopback/container bridge
```

Use `deploy/nginx.production.conf` and `deploy/compose.production.yml`. Replace
`example.invalid` and the certificate paths with the actual host and securely
mounted certificate/key. Run `nginx -t` before activating the configuration.
The sample is an include inside Nginx's `http` context. It assumes host Nginx
and a backend port published **only** at `127.0.0.1:8080`. Native deployment should
bind `LISTEN=127.0.0.1:8080`. A separate-container proxy needs a dedicated bridge
and an upstream service name instead; never publish the backend publicly.
Normal backend outbound RPC/callback traffic still needs network access.

TLS 1.2/1.3 terminates at Nginx; public HTTP redirects to the fixed configured
HTTPS hostname. Unknown HTTP hosts are rejected and unknown TLS SNI handshakes
are rejected. Provision real CA-issued certificates and renewal monitoring
outside this repository. Test certificates used below are temporary and are not
committed. Cloudflare is an optional outer proxy: use authenticated/verified
origin HTTPS (Full Strict), restrict origin access, and never use Flexible mode.
Caddy is an alternative only after matching and validating the same contracts;
neither Caddy nor Cloudflare was live-tested in this phase.

Nginx overrides `Host`, `X-Forwarded-Proto`, `X-Forwarded-For` and `X-Real-IP`;
it clears `X-Forwarded-Ssl`, `Front-End-Https`, `X-Url-Scheme`, `CF-Visitor` and
`Forwarded`. Client-supplied forwarding chains are not appended. Backend direct
access would circumvent that boundary, so private binding and host firewall
restrictions are essential. Existing payment host/proxy-header interpretation
and client-fingerprint logic are **unchanged**. B2-5 administrator limiting
continues to use the direct peer; behind one proxy, administrators share its IP
budget. No broad proxy trust is added to authentication.

B2-5 derives Secure from actual backend TLS. With internal HTTP, Nginx's
`proxy_cookie_flags session secure httponly samesite=strict` adds Secure at the
HTTPS boundary without modifying authentication. Preserve this directive with
any alternative proxy and test it. Enable HSTS only on the HTTPS listener;
the sample uses one year without preload or includeSubDomains.

## Security headers and compatibility

Application responses, including denied/unknown/panic responses, contain:

- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: SAMEORIGIN`
- `Referrer-Policy: no-referrer`
- `Content-Security-Policy` restricting default resources to self, object loading
  to none, base/form destinations to self, and ancestors to self.

The proxy applies them with `always`, including its own errors, and suppresses
duplicate upstream copies. The shipped frontend and checkout templates use
inline scripts/styles; the policy therefore retains `unsafe-inline` for those
two resource types. HTTPS images and data images/fonts remain usable. This is a
compatible baseline, **not** a nonce-based strict CSP or proof against all XSS.
Cross-origin iframe embedding and custom checkout templates using external
scripts are intentionally restricted; review such integrations separately before
deploying. The JSON payment API/signature protocol is unchanged.

Nginx limits request bodies to 1 MiB (tested at the allowed boundary and one byte
over it), with connection/read timeouts. The backend sets a 10-second request
header timeout, 60-second idle timeout and 1 MiB header budget. Port binding errors
now fail startup instead of announcing successful startup with no listener.

## Production configuration and secret lifecycle

Set `BEPUSDT_PRODUCTION=1`. Production startup rejects a publicly bound backend
unless `BEPUSDT_PRIVATE_NETWORK=1` explicitly acknowledges an isolated private
network. Public numeric addresses are still rejected; loopback is preferred.
The acknowledgement cannot inspect host firewall rules and is not a firewall.
The Compose sample sets it for its wildcard container listener while publishing
only loopback. Invalid production flags/listener ports are rejected.

Supported existing variables are `LISTEN`, `LOG`, `SQLITE`, `POSTGRESQL_DSN`.
`DATABASE_URL`, `API_SECRET`, `TOKEN` and `PASSWORD` are not alternative application
configuration names. Do not assume setting them supplies a database or payment
secret. `.env.example` contains placeholders only; **do not run it unchanged**.
If production loads a real `.env`, it must be a regular owner-only file. Prefer
runtime environment injection or a mounted private file over command-line DSNs,
which may appear in process listings. Never put credentials in VITE variables:
frontend build variables are public.

`POSTGRESQL_DSN_FILE` is an optional mounted regular file, mode 0600 or stricter.
It is mutually exclusive with a supplied PostgreSQL DSN. Missing, empty,
placeholder, readable-by-others or symlink files cause startup failure. No secret
value appears in those errors. For Compose PostgreSQL deployments, mount an
external owner-readable DSN at `/run/secrets/database_dsn` and supply its path;
do not commit the source file, hard-code a password or publish a database port.
The default production sample uses SQLite and contains no database service.
Use certificate-verified PostgreSQL transport for external databases according
to the database provider's requirements.

Existing administrator/bootstrap/payment credential generation and storage are
unchanged. A new empty database uses B2-1's generated credentials; an existing
production database with missing/placeholder administrator username, password,
cookie signing key or payment API secret fails before tasks start. No default
production password is introduced. Perform first-time bootstrap on the private
backend and securely retrieve the one-time installation handoff **before**
allowing public traffic. B2-5's accepted plaintext payment signing secret at rest
remains a residual risk; database backups and volume access must be protected.
No encryption/protocol/credential migration is introduced.

## Docker baseline

Build this branch's Dockerfile rather than assuming an upstream tag carries its
security controls. The final Alpine runtime uses numeric UID/GID `10001:10001`,
private data/log directories and a non-writable executable/entrypoint. The
entrypoint sets `umask 077`. Runtime paths remain compatible with existing flags.
The Go builder copies explicit source directories instead of `ADD . .`.
`.dockerignore` excludes real environment files, private keys, databases, logs,
Git metadata, dependency trees and previously generated backend frontend files.
Only the binary is copied from the backend build stage to the final image.
Certificates and secrets are runtime mounts, never image layers or build args.

```bash
docker compose -f deploy/compose.production.yml config
docker compose -f deploy/compose.production.yml build
docker compose -f deploy/compose.production.yml up -d
```

The sample uses read-only rootfs, all capabilities dropped, no-new-privileges,
a PID budget, owner-private `/tmp` tmpfs and separate writable data/log volumes.
It exposes only the loopback backend port and no PostgreSQL port. These options
were inspected in a real running non-root validation container. That container
used a locally built static binary in a scratch image to avoid blocked registry
pulls; it is **not** the production Alpine image and is not a published artifact.
Do not deploy it as a replacement: the production image also supplies trusted
CA certificates and timezone data for outbound services.

Existing bind mounts must be prepared offline for UID/GID 10001. Do not recursively
change an unrelated directory or volume; back up the database and scope ownership
changes to the application data/log files. An inaccessible owner, missing writable
mount or public listener misconfiguration must fail, not trigger a root fallback.
The sample has not been tested on Windows Docker or rootless Docker. Base images
remain version tags rather than digest pins; rebuild and scan the resulting image
with approved registries before release. No image publication occurs here.

## File and temporary-file permissions

Linux native process initialization sets `umask 077` before reading `.env` or
creating database/log/temp files. Dedicated application data/log directories are
created/tightened to 0700. Logs and their rotated replacements are 0600; existing
log files are tightened. Production SQLite main/WAL/SHM files are explicitly
tightened to 0600. GORM and all transactional/lease/concurrency settings retain
their existing semantics; only logging and directory protection change.

Recommended native setup (performed by the deployment operator):

```bash
install -d -m 0700 -o bepusdt -g bepusdt /var/lib/bepusdt /var/log/bepusdt
install -d -m 0700 -o bepusdt -g bepusdt /var/lib/bepusdt/tmp
# Run the existing binary with an unprivileged service identity:
export TMPDIR=/var/lib/bepusdt/tmp
export BEPUSDT_PRODUCTION=1
export LISTEN=127.0.0.1:8080
bepusdt start
```

Private configuration/DSN files must be 0600. TLS keys can use operator-controlled
0600 or 0640 with a narrowly scoped Nginx group; directory traversal can use 0700
or 0750 where a separate proxy identity needs it. The app's own private files use
the stricter owner-only policy. B2-1's one-time credential handoff remains 0600,
with its existing permission regression test retained. Container `/tmp` is 0700
tmpfs owned by 10001; native TMPDIR should also be a dedicated private directory.

The application tightens named files/directories only, not an entire filesystem.
POSIX modes are not equivalent to Windows ACLs or every network filesystem's
permission model. Linux umask setup is Linux-specific; other platforms require
operator-managed ACLs and independent validation.

## Logging policy

Ordinary and task log formatters suppress sensitive-labelled lines/fields,
private-key blocks, credential-bearing URL queries/userinfo and registered
configured credential values. Registration includes secret configuration values
and secret entrance paths, and refresh does not alter authentication values.
Database diagnostic logging uses a sanitizing writer. Split lines, oversized
lines and multi-line private keys are covered; incomplete non-newline writer
fragments are retained/suppressed rather than emitted unfiltered.

HTTP access logs contain method, matched route template and status only. Raw
paths, secret entrances, queries, request bodies and headers are not logged.
Panic recovery does not dump raw requests or panic values: it emits a generic
panic event and returns 500. CLI errors are sanitized instead of panic-dumped.
Sensitive-labelled diagnostics may be intentionally lost; this favors secrecy
over verbose debugging. No order, outbox or notification payload behavior changes.

Nginx access logging is disabled on all sample listeners. Its error logs are
disabled as raw proxy errors can include request URIs. Use external aggregate
metrics or an independently reviewed metadata-only logging policy; do not re-enable
default request logs or body/header tracing in production. Application log files
retain the existing rotation limits (300 MiB, five backups, seven days, compressed)
with private modes. Limit retention and protect backups/collector access.

Redaction is defense in depth, not proof that arbitrary future third-party stdout
or novel unlabelled secrets can never leak. Do not log secrets in the first place;
keep scanner tests and review new logging paths.

## Validation evidence

Thirteen new Go top-level tests cover headers on normal/denied/unknown/panic
responses; production normal startup, missing credentials/files, unsafe/invalid
listener and occupied port; secret scans with deliberate negative fixtures;
parsed Docker identity/ports/read-only/capability/tmpfs policy; Linux file modes,
symlink/readable/empty secret-file failures; actual rotated log files; and
split/oversized/PEM/structured/bare-value redaction. Existing B2-1 credential temp
file and B2-5 authentication tests remain passing.

```bash
go test -p 1 -count=1 ./...
go test -race -p 1 -count=1 ./app/log ./app/deployment ./app/router ./app/cmd
NGINX_BIN=nginx python3 deploy/test_proxy.py
docker compose -f deploy/compose.production.yml config
# The source directory is named main; keep the binary outside the checkout.
GOFLAGS='-o=/workspace/.cloud-setup/b26-main' go build ./main
git diff --check
```

Full cloud regression: **84 top-level Go tests, 95 passing results including
subtests, zero failed/skipped tests**, including actual isolated PostgreSQL
process-kill/recovery suites. Five actual Nginx tests pass: trusted test-certificate
HTTPS, header/cookie rewriting, spoofed forwarded-header replacement, redirect and
error headers, wrong Host/missing TLS key, and 1 MiB body-limit boundary behavior.
TLS verification remained enabled. Targeted race tests, Compose parsing and build/
whitespace checks pass. A real non-root/read-only container served the application
through HTTPS, with its generated stored credential values absent from logs.
Unsafe private-network acknowledgement and missing writable storage produced
nonzero startup exits. Test files, keys, databases and images remain outside Git.

Full multi-stage production image build is **UNVERIFIED** in this instance due to
Docker Hub anonymous pull rate limiting. No real-domain certificate, renewal,
Cloudflare/Caddy deployment, Internet exposure or production rollout is claimed.

## Impact and rollback

Authentication handlers/token/session policy, payment API signatures, canonical
event identity, order binding, transactional outbox, notification lease,
confirmation policy and SSRF validator are unchanged. Model edits are limited
to directory protection, sanitized diagnostics and registering secret values
for redaction. B1-R/B1-R2/B2-1 through B2-5 full regressions pass.

No schema changes or credential replacements. Upgrade requires preparation of
non-root volume ownership and private file modes, reviewed proxy host/certificates
and a renewed administrator login if the process restarts. Roll back with a
normal revert of `security: harden production deployment configuration` and
restart the prior application/proxy configuration. File modes/ownership already
tightened are not automatically loosened; restore only deliberately documented
operator access requirements. Preserve database data and commit history.

No Dual RPC, Nile profile or BIMHU/B3 work is included.
