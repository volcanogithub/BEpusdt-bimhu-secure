# Phase B2-5: Login / Access Control Hardening

Status: **PASS** for administrator authentication, authorization and login abuse
protection. The payment shared signing secret is an explicitly accepted residual
risk; this phase does not claim to harden its storage or protocol.

Repository: `volcanogithub/BEpusdt-bimhu-secure`.
Branch: `security/b2-hardening-v1.24.2`.
Audited parent: `d09c6cee11ce08b18042fe08fdf8bff167076a4a`.
Validation: GitHub cloud checkout, Go 1.26.2, PostgreSQL 17.11, 2026-10-01.

## Authentication model and audited baseline

The existing model has one administrator and one globally active administrator
login. There are no new accounts, roles, database columns, dependencies or
credential migrations. Existing bcrypt passwords, administrator names, secret
entrance paths, cookie signing keys and payment API credentials are preserved.

Before B2-5, visiting the secret entrance set an in-memory session's
`admin_secure` marker. `POST /api/auth/login` checked that marker, then username
and bcrypt password, returned a bearer token and kept its plaintext in a global
24-hour cache. Unknown usernames skipped bcrypt. Login did not rotate the cookie
session. Logout removed the marker and blanked the token cache. Cookie flags
were HttpOnly, SameSite=Strict, Path=/, MaxAge=86400; server-side marker expiry
was not independently enforced. Password change blanked the token but retained
the entrance session. Management APIs checked both the entrance marker and the
global token; the token comparison was already constant-time. A compile-time
debug branch could skip authentication checks.

The resulting request flow is:

```text
GET secret administrator entrance
  -> fresh signed opaque session cookie + server-side entrance grant
POST /api/auth/login
  -> valid unexpired entrance session
  -> username + direct-peer IP attempt budgets
  -> bcrypt password check + fixed-size username digest comparison
  -> delete entrance session, rotate session ID
  -> return random token; retain only token digest, session ID and expiry
management request
  -> registered method/path in administrator namespace
  -> valid entrance session, absolute expiry
  -> Authorization token digest matches active login AND its session ID
  -> administrator authorization
  -> handler
```

The secret entrance is a preliminary access gate, **not** proof of administrator
identity. The login endpoint requires that gate but is the only registered
administrator endpoint not requiring an authenticated bearer token. Invalid
username, password, malformed JSON, missing fields and oversized parsed login
input receive HTTP 401 with `invalid credentials`. Unknown usernames perform
the same bcrypt check as known usernames. Login reads are capped at 4096 bytes
with `http.MaxBytesReader`; errors do not echo binding details or submitted data.

## Authorization and administrator access rules

All current authentication, configuration, wallet, order, rate and dashboard
management endpoints require both the entrance session and the active token,
except `POST /api/auth/login`. That exception still requires the entrance session
and attempt limits. Missing, invalid, expired, revoked or differently bound
credentials receive HTTP 403 before the handler. Token alone, cookie alone, and
a token combined with a different browser's entrance cookie cannot authorize.

The `/api/auth`, `/api/conf`, `/api/wallet`, `/api/order`, `/api/rate` and
`/api/dashboard` namespaces deny unregistered method/path combinations, including
future direct Gin registrations that bypass the registration helpers. The debug
authentication bypass is removed. New management endpoints must use the existing
helpers with `checkAuth=true`; extending this single-role model is separate work.

Public checkout/payment endpoints and payment signature checks are unchanged.
No order state, payment event identity, event binding, outbox transaction, lease,
confirmation or SSRF admission logic changes are included.

## Login abuse protection

`app/access` reserves attempt budgets atomically **before bcrypt**, preventing
concurrent failures from bypassing the threshold. It limits submitted usernames
independently of whether they exist, as well as source IPs:

| Dimension | Budget | Window | Cooldown after final allowed attempt |
| --- | --- | --- | --- |
| Submitted username | 5 attempts | 5 minutes | 5 minutes |
| Direct peer IP | 20 attempts | 5 minutes | 5 minutes |

Thus five failed attempts for a username are evaluated, and the next attempt is
denied. Rotating IPs does not bypass the username budget; rotating usernames does
not bypass the IP budget. Invalid JSON attempts also consume the IP budget.
Successful login clears its username budget, but retains the IP work budget.
At a cooldown's exact expiry, login is allowed again. Denied attempts do not
extend the cooldown. There is no persistent account lock flag.

HTTP 429 uses the same `invalid credentials` message for every username, with
`Retry-After: 300` (a conservative retry delay). Username/IP map keys are hashed.
The map has at most 4096 buckets; expired buckets are removed and saturation
fails closed until capacity recovers. None of this state is persisted to the
administrator database.

Administrator login protections use the direct TCP peer. Clients cannot choose a different limiter IP
using `X-Forwarded-For` or `X-Real-IP`. Gin's existing global proxy handling and
payment client-fingerprint semantics remain unchanged. Behind a reverse proxy, the proxy's direct
address shares the budget; configuring a narrowly trusted proxy requires a
separate deployment decision. Distributed sources and sustained targeted
attempts can still impair availability. Limits are process-local and reset on
restart; shared limiting and edge request/connection protection are alternatives
for multi-instance or hostile public deployments, not implemented here.

## Session policy

- Signed opaque IDs and server-side values continue to use the existing memstore.
- Entrance and successful login each delete the previous server-side session and
  generate a fresh session ID. Secret entrance activation is GET-only.
- HttpOnly, SameSite=Strict, Path=/ and a 24-hour browser MaxAge are retained.
- Cookie Secure is enabled on actual TLS connections. Forwarded protocol headers
  cannot enable or disable this decision. HTTP development remains usable.
- An issuance timestamp enforces **absolute** 24-hour server-side grant expiry;
  accessing an API does not refresh it. Future timestamps and legacy sessions
  without the timestamp are denied.
- Logout revokes the active token, clears all session values and saves MaxAge=-1,
  deleting the server-side session as well as the browser cookie. Replaying the
  old cookie cannot authorize or reopen login.
- Successful password change checks the bcrypt generation and database update
  errors, revokes the active token, and deletes the current session. Failed
  password changes retain the valid login and stored password.

The frontend login interceptor rejects both HTTP 401 and 429 instead of treating
an error handler return value as successful login data. It displays the same
`invalid credentials` message for both. Successful login responses are unchanged.

Process restart clears server-side authentication state. Existing signed-in
browsers must visit their configured entrance and log in again after upgrade.
The global token revocation invalidates all authenticated administrator requests;
unrelated entrance-only cookies are not all erased. Already-authorized in-flight
requests are not canceled retroactively. The existing memstore has no background
garbage collection of unused session entries; explicit expiry rejects them but
does not redesign storage. A bounded production session store is an alternative
for a later structural review.

## Token policy and accepted structural issue

Administrator tokens use 32 bytes from the OS CSPRNG, encoded as URL-safe base64.
Only a fixed-size SHA-256 digest is held server-side, together with the bound
session ID and 24-hour absolute expiration. Presented tokens are hashed and
compared using `crypto/subtle.ConstantTimeCompare`. Plaintext administrator
tokens are not written to the database, cache or logs. The token is returned to
the authenticated client, which must possess it to send the Authorization header.
Replacement login revokes the old token, preserving the existing single-active
login behavior. Logout and password change revoke the digest immediately.

**Accepted residual risk:** `model.ApiAuthToken` is a separate payment shared
signing secret stored as plaintext configuration. The existing payment signature
protocol needs the original secret and uses its existing signature comparison.
Replacing that secret with a one-way hash would break existing integrations;
changing bootstrap credentials would violate this phase's compatibility scope.
The user explicitly selected documenting this issue and preserving the payment
signature protocol. No payment secret rotation, storage migration or signature
comparison change is made.

A compatible alternative is versioned envelope encryption backed by a separately
managed external key, with migration and rollback handling for existing rows;
another is a versioned signing protocol. Both require a separately authorized
design and configuration plan. The browser's existing persisted bearer-token
handling also remains exposed to same-origin script compromise; this phase does
not claim to solve XSS or replace the frontend authentication model.

## Observable validation

Fifteen new top-level Go tests (17 passing results including logout/password-change
subtests) cover:

- `TestB25NormalLoginAndSessionRotation`
- `TestB25InvalidCredentialsAreUniformAndBounded`
- `TestB25BruteForceCooldownAndRecovery`
- `TestB25IPLimitWithUsernameAndForwardedHeaderRotation`
- `TestB25AllAdministratorRoutesDenyUnauthenticatedRequests`
- `TestB25LogoutAndPasswordChangeInvalidateOldSessions`
- `TestB25CookieFlagsAndAbsoluteSessionExpiration`
- `TestB25LegacyAndFutureSessionTimestampsAreDenied`
- `TestB25DirectPeerIPCanonicalizationAndInvalidAddresses`
- `TestB25ReplacementLoginRevokesPreviousAuthentication`
- `TestB25RejectedPasswordChangesPreserveLogin`
- `TestB25TokenDigestBindingExpirationAndRevocation`
- `TestB25LoginWindowCooldownAndSuccess`
- `TestB25IPLimitConcurrencyAndBoundedMemory`
- `TestB25CooldownStartsAtThresholdAndDeniedAttemptsDoNotExtendIt`

Tests use a controlled clock instead of sleeping through cooldown or expiry.
The route test discovers every current management route and verifies five
unauthenticated/incorrectly bound credential combinations against each. It also
tests an unregistered configuration endpoint. Unit tests admit exactly 20 of 100
concurrent attempts at one IP, then verify recovery and bounded-map saturation.
Token tests check the stored digest, incorrect/empty tokens and wrong session,
exact expiry, replacement and explicit revocation.

A separate frontend Node test exercises the actual bundled Axios interceptors
with UI stubs and mock adapters: successful login resolves, both HTTP 401 and 429
reject, and both display the same error. It uses existing locked dependencies and
no browser or external requests. Frontend type checking and production build also
pass. Run `node --test tests/login-errors.test.mjs` from an installed frontend
copy (using the repository CI's `--shamefully-hoist` installation).

Cloud validation commands:

```bash
# Use the isolated local PostgreSQL test instance; no production database.
export BEPUSDT_TEST_POSTGRES_DSN='postgres://agent@127.0.0.1:55432/bepusdt_test?sslmode=disable'
go test -p 1 -count=1 ./...
go test -race -p 1 -count=1 -run TestB25 ./app/access ./app/router
# The checkout contains a directory named main: direct output outside it.
GOFLAGS='-o=/workspace/.cloud-setup/b25-main' go build ./main
git diff --check
```

Results: full regression **PASS**, 71 top-level tests and 78 test/subtest passing
results, zero failed or skipped tests. Fourteen packages have no tests; they are
not described as tested suites. PostgreSQL process-kill/recovery and compatibility
tests executed. B2-5 race checks, build and whitespace checks **PASS**. Existing
B1-R, B1-R2 and B2-1 through B2-4 regression tests remain passing. Test outputs and
database files are outside the checkout and are not committed.

## Migration, impact and rollback

No schema or stored-credential changes. Existing clients retain the same login
success JSON and Authorization header format, but must renew their session after
upgrade. Invalid login now uses actual HTTP 401 and limited login HTTP 429, rather
than HTTP 200 carrying an application error. Management denial remains HTTP 403.
Opening the entrance or logging in rotates the cookie; clients must accept the
new cookie and use the new token together.

Rollback is a normal revert of `security: harden login and access control
protections`, followed by a process restart and renewed administrator login.
No administrator data or database rollback is needed. Rollback restores the
weaker baseline policies and plaintext in-memory administrator token cache.
No main branch, deployment, HTTPS, Docker-security or Phase B3 work is included.
