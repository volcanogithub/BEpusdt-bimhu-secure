# TRON RPC error handling — Phase B2-2

Status: PASS for local isolated regression. This is not deployment acceptance.

Commit: `fix: harden tron rpc error handling and prevent scanner panics` (the commit introducing this document; obtain its SHA with `git log -1 --format=%H -- docs/security/TRON_ERROR_HANDLING.md`).

## RPC failure strategy

Block and transaction responses are validated before dereferencing nested fields. Nil blocks, missing block headers/raw data, missing transaction results/raw data/IDs, nil contracts/parameters, empty or nil transaction results, and missing receipts are errors. A missing receipt is never interpreted as protobuf's default SUCCESS value. Contract decode errors are logged and cause block retry.

Confirmation calls have a five-second deadline derived from the task context. RPC failures, timeout, cancellation, and malformed hashes leave the order in its current state for the existing five-second confirmation sweep. Valid failed on-chain results do not finalize orders. Confirmation thresholds are unchanged.

## Retry strategy

The existing block scheduler remains authoritative: exponential delays of 5, 10, 20, 40, and 80 seconds, capped at 80 seconds. Client creation, block RPC, malformed block data, contract decode, and receipt RPC/validation failures schedule retry. Healthy transactions in a partially invalid block continue to be processed; the block remains unsuccessful and is retried. Retry state is cleared only after successful complete processing.

Replay uses the existing B1-R canonical event identity and binding constraints. No event-index, order-binding, outbox, notification-lease, or database migration change is introduced.

## Panic isolation

Recovery is limited to one block job and one confirmation order. A recovered block panic is logged and schedules block retry. A recovered order panic is converted to a logged error; other orders continue, and the failed order is retried by the next sweep. There is no process-wide recovery. Nil/empty response guards remain the primary protection.

The TRON API-key interceptors tolerate an empty key list without modulo-by-zero. Notification workers and their lease/retry design are unchanged; their full existing regression suite remains part of the gate.

## Local validation

Environment: Windows amd64, Go 1.27.0. No production RPC, wallet, or callback is used by the new tests.

- `TestB22EmptyTransactionResult`: nil, empty GetRet, nil result element.
- `TestB22MissingReceipt`: nil and empty transaction info.
- `TestB22RPCTimeoutAndError`: RPC deadline error/cancellation propagation through both native and TRC20 confirmation paths.
- `TestB22OneHundredOrdersErrorIsolation`: 99 orders continue after one RPC failure or one injected panic.
- `TestB22MalformedInputsDoNotPanic`: malformed block/transaction structures and calldata lengths 0–449.
- `TestB22BlockFailureSchedulesRetry`: actual block entry point with nil/empty response, RPC deadline error, and injected panic.
- `TestB22EmptyAPIKeyInterceptors`: empty key lists continue unary and streaming RPC calls.

Commands:

```text
go test -p 1 -count=1 -run TestB22 -v ./app/task ./app/utils
go test -p 1 -count=1 ./...
go build -o bepusdt-b2-2-local.exe ./main
git diff --check
```

Live RPC outage tests, production load tests, and a fresh PostgreSQL integration run are NOT RUN in B2-2. Local tests establish the covered error paths, not safety for every possible input or dependency fault. Retry queues remain in-memory as before; persistent scanner recovery is outside this change.

## Payment trust boundary

BEpusdt order states, MQTT, and HTTP callbacks remain `UNTRUSTED_HINT`. BIMHU Payment Service independently verifies chain events and decides CREDIT. B2-2 introduces no CREDIT integration. H-3 confirmation policy and other B2 tasks remain pending.
