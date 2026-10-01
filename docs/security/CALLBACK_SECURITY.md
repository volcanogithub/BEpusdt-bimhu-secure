# Callback Security — Phase B2-4

Status: NOT RUN — cloud regression pending.

## Boundary

Server-side callbacks (Epay GET, Epusdt POST, durable success and status updates)
use one guarded client. Only absolute HTTPS URLs are accepted. HTTP is not retained:
there is no production allow-HTTP or allow-private switch. Userinfo, fragments,
ambiguous numeric hosts, local names and invalid DNS labels are rejected.
Browser return/redirect URLs share admission checks, but are not fetched by the
server. Telegram uses its fixed upstream API, not a user-configurable webhook.

The transport resolves all A/AAAA results before every request and again at dial.
Any private, loopback, link-local, unspecified, multicast, reserved/test address
in the answer rejects the entire destination; empty/error answers fail closed.
IPv4-mapped IPv6 is unwrapped. Only native global IPv6 2000::/3 is allowed,
excluding special/tunneling/documentation ranges. This rejects AWS metadata
169.254.169.254 / fd00:ec2::254, ULA and NAT64 equivalents.

Dial connects directly to a validated IP, preserving URL Host and TLS hostname
verification. No environment proxies, custom TLS dial hooks or automatic
redirects. Connections are not pooled/reused to avoid stale DNS authority.
Redirect responses fail the existing status-200 delivery rule.

## Reliability compatibility

Only the HTTP transport selection changes in the outbox sender; ProcessOne,
canonical identity, order binding, transaction and notification lease code are
unchanged. Errors follow existing bounded retry/dead-letter handling with a
failure reason; rejection never alters the successful payment state. Older
stored URLs are rechecked at delivery, not grandfathered in.
HTTP callbacks and local/private endpoints now fail; migrate these receivers
to public HTTPS before rollout. No database schema/data migration.

## Tests and deployment

Test-only resolver/dial injection connects public synthetic destinations to
isolated TLS httptest receivers. TLS verification bypass exists only in *_test.go;
no runtime network exemption. Tests cover admission, mixed/public-private DNS,
empty/error DNS, rebinding, pinned dial, redirect, arbitrary client bypass,
persisted failure and payment-state preservation. Existing SQLite/PostgreSQL
delivery, lease and process-kill tests retain assertions and use test-only routing.

Run: `go test -p 1 -count=1 -run TestB24 -v ./app/security ./app/task/notify`;
`go test -p 1 -count=1 ./...`; `go build ./main`; `git diff --check`.
CI retains isolated PostgreSQL and all prior regression gates.

Use independent egress firewall controls; public IP validation cannot prove that
a public service is trustworthy or prevent public-to-private forwarding outside
this process. Special-use IP ranges need ongoing review. HTTPS protects transport,
not payment truth; restrict destinations operationally and retain receiver atomic
event_id deduplication. Callback errors may still contain URL query parameters in
standard HTTP errors: do not embed secrets in URLs. No deployment/live payments
are part of this phase.

Rollback requires an explicit security decision: reverting re-enables SSRF.
All states, MQTT and HTTP signals remain UNTRUSTED_HINT. BIMHU independently
verifies the chain and decides the unique CREDIT. No BIMHU integration changes.
