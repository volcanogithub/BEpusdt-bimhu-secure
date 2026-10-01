# Security roadmap

Development branch: `security/b2-hardening-v1.24.2`

Changes must be proposed through pull requests. Do not push directly to `main`.

## Phase B2

- H-1: credential randomization — **PASS (Phase B2-1)**
  - Commit: `security: replace predictable bootstrap credentials with cryptographic random generation`
  - Modified: `app/credential`, `app/model/conf.go`, `app/model/model.go`, `app/cmd/reset.go`, `app/handler/admin/conf.go`, and security documentation.
  - Tests: 1,000-generation uniqueness; deterministic entropy/no-time-input; stdout leak scan; bcrypt compatibility; existing credential preservation; full Go regression.
- H-2: TRON RPC/Panic Reliability Hardening — **PASS (Phase B2-2)**
  - Commit: `fix: harden tron rpc error handling and prevent scanner panics` (introducing commit of `TRON_ERROR_HANDLING.md`).
  - Modified: `app/task/tron.go`, `app/task/tron_rpc.go`, `app/utils/tron.go`, regression tests, and security documentation.
  - Tests: nil/empty transaction and receipt; RPC timeout/error; 100-order error/panic isolation; malformed calldata; actual block retry; empty API-key interceptors; full Go regression and build.
  - Evidence and limits: [TRON_ERROR_HANDLING.md](TRON_ERROR_HANDLING.md).
- H-3: Confirmation Policy Hardening — Phase B2-3
  - Status: **PASS** — final B1-R2 + B2-3 joint revalidation, run 36807328740 attempt 2, job 110197624511. Confirmation policy unchanged; historical SQLite failures retained.
  - Implementation commit: `43a8a4126b673212cd70fca22914bf29025597b8`; B1-R2 baseline `058b04d692f530209d417520d0896be1f95cf0f8`; final revalidated source `0fd3c6eeee43128346a577aaaf8c36f78ec921d8`.
  - Acceptance record: `docs: finalize B2-3 confirmation policy validation` (introducing commit of the final joint-revalidation section).
  - Commands: `go test -p 1 -count=1 ./...`; `go test -p 1 -count=1 -run TestB23 -v ./app/model ./app/task`; `go build ./main` with safe /tmp output; `git diff --check`. All PASS.
  - Results: six confirmation tests PASS; original worker concurrency 20/20; SQLite regression 120/120; PostgreSQL/outbox/lease/process-kill and full regressions PASS, clean cloud worktree. Go 1.26.2 / PostgreSQL 17.11. No application or workflow changes in this final revalidation.
  - Evidence and limitations: [CONFIRMATION_POLICY.md](CONFIRMATION_POLICY.md).
- M-1: SSRF protection
- M-2: filesystem permission hardening
- M-3: HTTPS deployment support
- M-5: login rate limiting
- M-6: secret-safe logging

Each item requires a regression test, configuration migration note, threat/impact statement, and rollback guidance before merge.

## Phase B3

Container publishing maintenance: **PASS WITH DOCUMENTED LIMITATIONS** — historical GHCR #2 SUCCESS preserved; main upstream Docker publishing disabled via PR #1; authorized security-source/SHA/concurrency controls validated in run 36807055783. Current publisher live rerun/package visibility NOT VERIFIED; no automatic hardened schedule is activated. See [CONTAINER_PUBLISHING.md](CONTAINER_PUBLISHING.md).

- PostgreSQL production deployment validation
- Dual-RPC verification
- Explicit network profiles
- BIMHU Payment Service integration

Phase B3 cannot treat BEpusdt order state, MQTT, or HTTP callback as proof of payment. BIMHU must independently verify network, transaction, event index, token contract, recipient, amount, confirmation policy, and replay uniqueness before CREDIT.

## Phase B1-R2: SQLite Concurrency Stabilization

- Status: **PASS** — strengthened cloud run 36800692733; original worker test 20/20, six SQLite tests 120/120, PostgreSQL/B1-R recovery/full regressions/build/clean-tree PASS. Earlier repeat failure 36800357211 is retained in the evidence document.
- Reason: Cloud CI SQLite contention fix.
- Commits: `0f763faf4ea9d215b66e996e2a2487197a670ae6` — `fix: stabilize sqlite concurrency under notification workers`; `cbb50808f833965439f5b3c797d2dc258ec4b1cb` — follow-up per-pool writer admission.
- Scope: private-cache WAL connections; cancellable per-pool write admission and bounded numeric-code busy retry around the original database-only transactions and writes. No canonical identity, outbox, lease or confirmation semantic changes.
- Tests: original notification race repeated 20 times; 20 repetitions of six SQLite contention/error/admission tests; PostgreSQL compatibility/recovery; B1-R and B2-1/B2-2/B2-3 full regression, build and clean-tree gate.
- Evidence, limits and rollback: [SQLITE_CONCURRENCY.md](SQLITE_CONCURRENCY.md).

## Phase B2-3 publication isolation (distinct from H-3 confirmation policy)

Status: **PASS WITH DOCUMENTED LIMITATIONS** — cloud run 36807055783; all ten isolation invariants and existing regressions pass. Current publisher live rerun NOT RUN; package visibility NOT VERIFIED.
Main build-only isolation already merged by PR #1 at ee1e24d1440ce82065dc247306481ab98bf6db60.
This follow-up restricts GHCR source to the authorized security branch, pins the
event SHA, serializes nightly publishers and records historic GHCR #2 success.
No publisher/test pipeline redesign or B2-4 work. See CONTAINER_PUBLISHING.md.

## Phase B2-4: SSRF Protection Hardening

Status: **PASS** — cloud regression [36827381505](https://github.com/volcanogithub/BEpusdt-bimhu-secure/actions/runs/36827381505), source `b28c4b6366ee3fb1c411d67c41762df59f961fd5`: seven SSRF tests, SQLite/PostgreSQL recovery, B2-3, full regression, build and clean-tree gates PASS. Final formatting/acceptance commit retains the same policy and is revalidated by the same CI. Initial implementation commit: `1048f6d9f0670d795576bc5c85d977d4f440f69c` — `security: add callback SSRF protection`.

Scope: unified HTTPS callback admission and guarded HTTP transport for durable, legacy and status-update callbacks. Event identity, order binding, outbox transactions, lease, credentials, scanner and confirmation policy unchanged. See [CALLBACK_SECURITY.md](CALLBACK_SECURITY.md).
