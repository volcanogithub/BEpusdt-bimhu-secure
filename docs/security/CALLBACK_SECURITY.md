# Callback Security — Phase B2-4

Status: PASS — cloud run [36827381505](https://github.com/volcanogithub/BEpusdt-bimhu-secure/actions/runs/36827381505), tested source `b28c4b6366ee3fb1c411d67c41762df59f961fd5`.
Base: `9b4682f9a949f8c34fca4a468157662a151b9719`.
Implementation: `1048f6d9f0670d795576bc5c85d977d4f440f69c`,
`security: add callback SSRF protection`. Subsequent test-isolation and
formatting/acceptance commits are retained without squashing.

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

## Cloud acceptance evidence

Ubuntu 24.04.5, Go 1.26.2 linux/amd64, isolated PostgreSQL 17.11.
Seven TestB24 cases PASS: URL admission; all DNS answers; rebinding at dial;
redirect and pinned public HTTPS; arbitrary client cannot bypass; failure
persisted without changing payment success; all durable/legacy/status callbacks.
Original SQLite worker race 20/20; six B1-R2 SQLite cases 120/120;
PostgreSQL process-kill/receiver dedupe, transaction/outbox/lease recovery,
six B2-3 tests, full `go test -p 1 -count=1 ./...`, build and diff/clean gates PASS.
Build runs `go build ./main` with GOFLAGS=-o=/tmp/bepusdt-b2-3 to avoid the
existing main directory output-name collision.

Historical first run [36827148082](https://github.com/volcanogithub/BEpusdt-bimhu-secure/actions/runs/36827148082)
FAIL: `TestDeliverBepusdtStatusUpdateDoesNotHoldDBWhileHTTPIsPending` reported
`notification request never reached test server`. New SSRF status fixture
reused the existing order's trade ID and populated its in-process cache key.
A unique SSRF fixture trade ID corrected test isolation, not payment/cache logic.
All second-run gates PASS; failure evidence is intentionally preserved.

The CI preservation guard now permits the explicitly scoped callback adapter
and tests, while still rejecting changes to model/event/outbox DB/lease,
credential, scanner, confirmation policy, build manifests and UI source.
ProcessOne body remains byte-for-byte unchanged from the base.
No local checkout, production wallet, live callback, chain payment or deployment.
