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
  - Status: PASS only for a successful introducing-commit Confirmation Policy CI run; FAIL otherwise.
  - Commit: `security: enforce safe blockchain confirmation policy defaults`.
  - Tests: new-install defaults, TRON depth boundary/RPC gate, invalid/legacy configuration, independent EVM policies, full B1-R/B2 regressions with PostgreSQL and build.
  - Evidence and limitations: [CONFIRMATION_POLICY.md](CONFIRMATION_POLICY.md).
- M-1: SSRF protection
- M-2: filesystem permission hardening
- M-3: HTTPS deployment support
- M-5: login rate limiting
- M-6: secret-safe logging

Each item requires a regression test, configuration migration note, threat/impact statement, and rollback guidance before merge.

## Phase B3

Container publishing maintenance: **PASS WITH LIMITATIONS** — nightly configuration migrated to GHCR on the security branch; upstream latest publisher disabled there. Static validation completed; Actions/GHCR publication NOT VERIFIED and default-branch schedule migration pending. See [CONTAINER_PUBLISHING.md](CONTAINER_PUBLISHING.md).

- PostgreSQL production deployment validation
- Dual-RPC verification
- Explicit network profiles
- BIMHU Payment Service integration

Phase B3 cannot treat BEpusdt order state, MQTT, or HTTP callback as proof of payment. BIMHU must independently verify network, transaction, event index, token contract, recipient, amount, confirmation policy, and replay uniqueness before CREDIT.
