# Phase B2-3: Confirmation Policy Hardening

Status: **PASS — final B1-R2 + B2-3 joint cloud revalidation completed.**
Commit: `security: enforce safe blockchain confirmation policy defaults`.
Run evidence: repository Actions, workflow `confirmation-policy-ci.yml`, source commit shown by the run. This document is not a deployment approval.

## Model and defaults

Protection is mandatory for TRON and every EVM network initialized by this application. A confirmation depth is the number of successor blocks: fresh RPC head height minus transaction inclusion height. The inclusion block does not count. This conservative definition retains the old enabled-mode offset semantics without an off-by-one relaxation.

| Network | Configuration key | Default / minimum successor blocks |
| --- | --- | --- |
| TRON | confirmation_depth_tron | 20 |
| Ethereum | confirmation_depth_ethereum | 12 |
| BSC | confirmation_depth_bsc | 15 |
| Polygon | confirmation_depth_polygon | 40 |
| Arbitrum | confirmation_depth_arbitrum | 40 |
| Base | confirmation_depth_base | 40 |
| Xlayer | confirmation_depth_xlayer | 12 |
| Plasma | confirmation_depth_plasma | 40 |

Each network is independently configurable at or above its minimum. EVM defaults retain existing chain-specific enabled-mode offsets; they are engineering depth floors, not claims of protocol finality. L2 depth is not L1 settlement finality. TRON uses the requested minimum of 20.

There is no development bypass. Zero, negative, malformed, overflow, or below-minimum values reject task initialization and fail closed during confirmation. Missing new values use safe defaults, including upgraded instances. Startup prints effective network/depth/minimum values and no credential values.

## Legacy compatibility and upgrades

New installations retain `block_offset_confirm=1` for old UI compatibility and persist the explicit per-chain defaults. The legacy field is deprecated: `0` is parsed as a compatibility value, emits a startup warning, and cannot disable the new policy; `1` is accepted, other nonempty values are rejected. The old UI toggle does not control enforcement. Operators should configure the new keys through existing configuration tooling.

Existing database initialization adds missing defaults through the existing configuration mechanism; no schema or B1-R migration changes are introduced. Upgrading a legacy instance with `0` deliberately strengthens confirmation behavior and delays successful orders/callbacks. Already successful orders and already queued notifications are not retroactively revoked.

Rollback to older code can restore the unsafe legacy-zero behavior; do not use rollback as a confirmation bypass. Review pending orders and legacy values before rollback.

## Runtime gate

TRON queries a fresh RPC block head, checks the configured depth against the bound inclusion height, validates transaction-info ID and inclusion height, then requires the existing transaction/receipt success result. Below threshold, absent head, bad configuration, moved inclusion, or RPC failure does not enter finalization. Failed attempts remain under the existing confirmation retry sweep.

EVM queries a fresh `eth_blockNumber` for each order rather than trusting an old cached scanner height. Depth is checked before receipt finalization; receipt transaction hash and inclusion height must match the bound transaction. Invalid/missing heads and receipts fail closed. The existing receipt-success requirement remains.

The call to `markFinalConfirmed` stays after the policy gate. That call uses unchanged B1-R atomic success/outbox logic. No canonical event, identity/index, binding, transaction outbox, or notification lease is changed. MQTT scan messages and non-order notifications are not payment-finality evidence and remain unchanged.

## Verification

Cloud workflow `Confirmation Policy CI` runs on security-branch push with contents-read permissions, an isolated PostgreSQL 17 service, and no wallet/production secret. It runs:

```text
go test -p 1 -count=1 -run TestB23 -v ./app/model ./app/task
go test -p 1 -count=1 ./...
go build ./main
git diff --check
```

The build step sets `GOFLAGS=-o=/tmp/bepusdt-b2-3` to avoid the existing `main/` source-directory name collision. The full suite sets a loopback-only test DSN so B1-R PostgreSQL tests are not skipped. Final worktree validation rejects generated tracked or untracked artifacts.

New tests cover actual first-install persisted defaults, below/at/above threshold, invalid production configuration, legacy-zero protection, independent EVM keys, TRON RPC gate and inclusion mismatch/head failure, and simulated EVM head responses. No live chain transaction or production notification is required.

## Remaining risks and BIMHU boundary

A single RPC may lie or return inconsistent data; depth does not replace independent verification, reorganization reconciliation, token/amount/recipient checks, or protocol finality. Block-hash canonicality, dual RPC, L2 settlement, and post-success reorg handling remain separate work. Confirmation delays may exceed order expiry under chain/RPC outages; operators must review reconciliation requirements. Other chain handlers (TON/Solana/APTOS) are not redesigned here.

All BEpusdt statuses, MQTT and HTTP callbacks remain `UNTRUSTED_HINT`. BIMHU Payment Service independently verifies the chain event and decides unique CREDIT. B2-3 adds no CREDIT integration.

## Historical cloud evidence and blocking regression (resolved by B1-R2)

Implementation commit: `43a8a4126b673212cd70fca22914bf29025597b8`, parent `85f7c759cf84e26c2ab10c1cce9ab3b846593d61`.

[Cloud run 36797076198](https://github.com/volcanogithub/BEpusdt-bimhu-secure/actions/runs/36797076198) failed twice on the same commit. All six B2-3 tests passed in both attempts. The model package including isolated PostgreSQL 17.11 acceptance passed; full regression failed in `TestB1ConcurrentWorkersSendOnceAtATime`, `app/task/notify/outbox_test.go:76`, from `app/model/notification_delivery.go:76`: attempt 1 `database is locked (5) (SQLITE_BUSY)`, attempt 2 `database is locked (517)`.

At the time of the original B2-3 implementation, B1-R implementation and tests were unchanged. No notification lease or outbox fix was included in that phase because its scope explicitly prohibited those changes. A subsequent evidence-only CI change allows build and clean-worktree gates to execute despite a failed test step and adds reproduction against the unmodified parent baseline. A green subset or successful build does not make the full gate PASS.

## Final B1-R2 + B2-3 joint revalidation (2026-10-01)

This section supersedes the original blocked acceptance status without deleting
its failure evidence. This PASS refers to **Confirmation Policy Hardening**, not
the separately documented publication-isolation phase with the same B2-3 label.

- Confirmation implementation: `43a8a4126b673212cd70fca22914bf29025597b8`.
- B1-R2 final source baseline: `058b04d692f530209d417520d0896be1f95cf0f8`.
- Final revalidated source commit: `0fd3c6eeee43128346a577aaaf8c36f78ec921d8`.
- Fresh [run 36807328740, attempt 2](https://github.com/volcanogithub/BEpusdt-bimhu-secure/actions/runs/36807328740/attempts/2),
  job `110197624511`: **SUCCESS**. It was explicitly rerun for this final acceptance,
  rather than merely reusing the earlier publication-audit result.
- Environment: Linux amd64, Go **1.26.2**, isolated PostgreSQL **17.11**.
- Branch/remote: `security/b2-hardening-v1.24.2`,
  `https://github.com/volcanogithub/BEpusdt-bimhu-secure`.
  Cloud checkout clean and remote source/ref synchronized; no local worktree.
- Only the final acceptance documentation changes in this request. Application,
  canonical event/index, binding, outbox, lease, credential and TRON RPC source
  remains identical to the B1-R2 final baseline; confirmation policy unchanged.

Required commands actually executed by the fresh cloud job:

```bash
go test -p 1 -count=1 ./...
go build ./main
go test -p 1 -count=1 -run TestB23 -v ./app/model ./app/task
git diff --check
test -z "$(git status --porcelain)"
```

Build uses `GOFLAGS=-o=/tmp/bepusdt-b2-3` to avoid creating an executable over
the existing main/ source directory. This is the requested build command with
a safe output location, not a different application build.

All six targeted tests **PASS**:

| Test | Covered property |
| --- | --- |
| TestB23NewInstallDefaults | New installation persists enabled per-chain protection |
| TestB23DepthBoundariesAndInvalidConfiguration | Depth boundary and invalid/minimum configuration rejection |
| TestB23LegacyZeroCannotDisableProtection | Old BlockOffsetConfirm cannot bypass protection |
| TestB23IndependentEVMPolicies | Independent EVM chain settings |
| TestB23TronRPCConfirmationGate | TRON below/at threshold, receipt inclusion and RPC failure gates |
| TestB23EVMFreshHeadValidation | Fresh eth_blockNumber parsing and failure handling |

B1-R2 original concurrent-worker case **20/20 PASS**; six SQLite retry,
contention/error/cancellation cases **120/120 PASS**. PostgreSQL pass-through,
migration/uniqueness/concurrent updates, atomic success/outbox rollback,
database-clock lease competition/recovery, transaction process-kill and HTTP
receiver deduplication tests **PASS**, not skipped. Full model/notify regressions
include SQLite outbox and lease tests. Full Go suite, build and clean-tree gates
**PASS**; no failure required or justified a code change.

Acceptance record commit: the commit introducing this section,
`docs: finalize B2-3 confirmation policy validation`; its actual final branch
SHA is recorded in the completion report and the resulting push-triggered Actions
run. The final documentation-only tree does not alter the validated code.

This is isolated regression acceptance, not production deployment or live-chain
payment validation. Single-RPC trust, reorg reconciliation, L2 finality, prolonged
SQLite contention and previously listed risks remain. Status/MQTT/HTTP remain
UNTRUSTED_HINT; BIMHU independently decides CREDIT. No other B2 work starts.
