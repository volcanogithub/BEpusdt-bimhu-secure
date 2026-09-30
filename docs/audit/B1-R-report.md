# Phase B1-R verification report

Status: **PASS** (local isolated verification, 2026-09-29 Asia/Shanghai)

This status is not production deployment acceptance. No mainnet transaction, production callback, wallet secret, BIMHU CREDIT integration, or VPS deployment was used.

## Imported security layer

- Canonical event identity: `network + tx_hash + event_index`.
- Database uniqueness proving one event maps to at most one order and one order to at most one event.
- Conditional order state transitions with affected-row checks.
- Atomic `success + notification outbox` transaction.
- Persistent notification claims, leases, retry budget, crash recovery, and stable `event_id`.
- TRON TRC20 identity derived from receipt log index, supporting multiple `Transfer` events in one transaction.
- Database-clock lease decisions and in-flight HTTP lease renewal.
- At-least-once HTTP delivery with receiver-side atomic `event_id` deduplication; no exactly-once claim.

## Evidence

- Pinned upstream commit: `4d88040fd4096e77e8fb9ad2650e775753a977b6`
- Original B1 patch SHA-256: `4B0F6D01543EBC8AAD91F005671B1A0D2BE2DF6309BDEA02CF0DF0E26D1E832A`
- B1-R cumulative patch SHA-256: `8AEED09DB6E5077B6160BE7B34A2632E72BEA8EDC8E9B95EF85F6413EFC061AE`
- Final source manifest SHA-256: `3AC82EC13CE05134C51BDB7BAB3EE4AB4565C818B491419E1C699A2175944443`
- PostgreSQL tested version: `17.11 x86_64-windows`
- SQLite full regression: PASS
- PostgreSQL migration/constraint/concurrency tests: PASS
- Real subprocess kill fault injection: PASS
- Slow HTTP beyond lease duration: PASS

Commands used for the final gates:

```powershell
go test -p 1 -count=1 ./...
go test -p 1 -count=1 -run '^TestB1R' -v ./app/model ./app/task/notify
go build -o bepusdt-b1r-local.exe ./main
```

BEpusdt state, MQTT, and HTTP callbacks are always `UNTRUSTED_HINT`. BIMHU Payment Service must independently validate the chain event and decide the only CREDIT.