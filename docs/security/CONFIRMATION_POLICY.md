# Phase B2-3: Confirmation Policy Hardening

Status: **FAIL — full acceptance gate blocked by unchanged B1-R SQLite notification concurrency failures; B2-3 targeted tests PASS.**
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

## Cloud evidence and blocking regression

Implementation commit: `43a8a4126b673212cd70fca22914bf29025597b8`, parent `85f7c759cf84e26c2ab10c1cce9ab3b846593d61`.

[Cloud run 36797076198](https://github.com/volcanogithub/BEpusdt-bimhu-secure/actions/runs/36797076198) failed twice on the same commit. All six B2-3 tests passed in both attempts. The model package including isolated PostgreSQL 17.11 acceptance passed; full regression failed in `TestB1ConcurrentWorkersSendOnceAtATime`, `app/task/notify/outbox_test.go:76`, from `app/model/notification_delivery.go:76`: attempt 1 `database is locked (5) (SQLITE_BUSY)`, attempt 2 `database is locked (517)`.

B1-R implementation and tests are unchanged. No notification lease or outbox fix is included because the B2-3 scope explicitly prohibits those changes. A subsequent evidence-only CI change allows build and clean-worktree gates to execute despite a failed test step and adds reproduction against the unmodified parent baseline. A green subset or successful build does not make the full gate PASS.
