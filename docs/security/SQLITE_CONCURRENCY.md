# Phase B1-R2: SQLite Concurrency Stabilization

Status: **FAIL (repeat acceptance exposed contention; stabilization in progress)**.
Reason: Cloud CI SQLite contention fix.
Implementation commit: `0f763faf4ea9d215b66e996e2a2487197a670ae6`, message
`fix: stabilize sqlite concurrency under notification workers`.

## Root cause and preserved baseline evidence

Before B1-R2, production `app/model/model.go` used WAL, shared cache, a pool of
five connections, and an 8,000 ms per-connection busy timeout. The failing
`TestB1ConcurrentWorkersSendOnceAtATime` used eight connections and the same timeout.
`ClaimNotification` starts a deferred transaction, reads database time and a candidate,
then conditionally updates the candidate and commits. Concurrent readers can race
to become the writer; WAL read snapshots cannot safely be promoted after another
writer commits. Shared cache additionally permits table-lock contention.

The timeout was already present, so simply raising it cannot solve stale-snapshot
promotion. This is an SQLite concurrency limitation plus a missing application-level
handling of recoverable transaction contention, not a B2-3 confirmation defect.

Cloud run [36798373025](https://github.com/volcanogithub/BEpusdt-bimhu-secure/actions/runs/36798373025)
failed at `notification_delivery.go:76` / `outbox_test.go:76` with
`SQLITE_BUSY (5)`. Unmodified baseline `85f7c759cf84e26c2ab10c1cce9ab3b846593d61`
reproduced it 5/5 times. Earlier attempts also produced extended code 517.
No baseline source or old evidence is removed.

References: [SQLite isolation](https://www.sqlite.org/isolation.html),
[SQLite result codes](https://www.sqlite.org/rescode.html),
[busy timeout](https://www.sqlite.org/c3ref/busy_timeout.html).

## Connection and retry policy

- Keep WAL, 8,000 ms busy timeout installed by the DSN on **every** new connection,
  existing durability setting, and the multi-connection pool. Remove production
  shared-cache mode; use ordinary private-cache WAL connections. Existing shared-cache
  notification tests remain unchanged and must still pass.
- `databaseWrite` only retries SQLite driver numeric BUSY primary code 5
  (including extended BUSY_SNAPSHOT 517), or LOCKED_SHAREDCACHE 262.
  Generic LOCKED 6, SQL errors, uniqueness failures, IO errors and domain state
  errors are not retried. No error-string matching.
- At most 16 attempts. Backoff doubles from 1 ms to a 16 ms cap with jitter
  (actual waits are 1–32 ms). A five-second context budget is passed to all attempts;
  earlier caller cancellation is respected. Driver busy handling can delay observing
  cancellation, so this is not a claim of a hard five-second wall-clock guarantee.
  The per-attempt SQLite busy handler also retains its finite eight-second limit.
- Admit one database-only write operation at a time per SQLite connection pool,
  through a context-cancellable gate. Pool connections and independent pools remain
  enabled; independent processes are coordinated only by SQLite locks and bounded
  retries. This follows SQLite's one-writer limit and does not weaken isolation.
  Waiting for admission is included in the context budget; no HTTP happens under
  the gate. Production shutdown retires the pool's gate.
- Retry the **whole rolled-back transaction** with a fresh snapshot and a fresh
  database-time calculation. Never retry an UPDATE in the failed old snapshot.
  All conditional predicates, affected-row checks, unique constraints and
  order-success/outbox atomicity remain unchanged.
- Retry callbacks contain only database work. HTTP/MQTT delivery is outside the
  callback and is never replayed by this helper. Notification retry/backoff policy
  and lease ownership/deadline/renewal semantics are unchanged.
- On exhaustion, return the error, preserving its cause. No infinite retries or
  swallowed database errors. Database time is still authoritative for leases.
- PostgreSQL takes the original one-call path: no SQLite retry, connection change
  or extra context budget is applied.

## Tests and cloud commands

New tests cover deterministic WAL snapshot invalidation/re-read, numeric code
allowlisting, finite exhaustion, cancellation, real malformed SQL and constraint
rollback, 24 concurrent finalize/outbox producers plus 8 claim/renew/complete
workers and a second independent write pool, timeout on three simultaneously held
connections, and PostgreSQL single-call pass-through.

Cloud CI uses an isolated PostgreSQL 17 service and local mock receivers only:

```bash
go test -p 1 -count=20 -run '^TestB1ConcurrentWorkersSendOnceAtATime$' -v ./app/task/notify
go test -p 1 -count=20 -run '^TestB1R2SQLite' -v ./app/model
go test -p 1 -count=1 -run 'TestB1R.*Postgres' -v ./app/model ./app/task/notify
go test -p 1 -count=1 -run TestB23 -v ./app/model ./app/task
go test -p 1 -count=1 ./...
GOFLAGS=-o=/tmp/bepusdt-b2-3 go build ./main
git diff --check
test -z "$(git status --porcelain)"
```

The historic baseline-failure step is replaced by acceptance of the fixed test,
not by skipping or weakening its assertions. No local workstation is used.

## Compatibility, rollback and remaining risks

No schema migration, canonical identity/event index change, order-binding semantic
change, outbox protocol change, lease design change, credential change or confirmation
policy change. Only database retry boundaries surround the existing SQL.

SQLite remains a single-writer database. Sustained contention or storage failures can
still exhaust the finite retry budget and must be surfaced/monitored, not suppressed.
This does not guarantee queue throughput or safety during unbounded process pauses.
Windows connection-release and deployment acceptance are not established by Linux CI.
Rollback is reverting B1-R2; it restores the documented contention risk without data
migration. PostgreSQL is the recommended production database for multiple workers,
subject to separate deployment validation.

BEpusdt status, MQTT and HTTP remain **UNTRUSTED_HINT**. Delivery remains at-least-once;
receivers must atomically deduplicate stable event IDs. BIMHU independently verifies
the chain and exclusively decides CREDIT. No deployment or chain transaction occurs.

## Acceptance evidence (2026-10-01)

- Functional commit: `0f763faf4ea9d215b66e996e2a2487197a670ae6`.
- Tested commit: `e93e99441da5194a0c52b4344cd588da048ef33d`; the only follow-up change was CI syntax correction, no application source change.
- [Run 36799257159](https://github.com/volcanogithub/BEpusdt-bimhu-secure/actions/runs/36799257159), job `110169681611`: **PASS**.
- Linux amd64, Go 1.26.2; isolated PostgreSQL **17.11** (Debian 17.11-1.pgdg13+2).
- Original concurrent-worker failure: **PASS 20/20** with unchanged test assertions/shared-cache configuration.
- Five new SQLite tests, each repeated five times: **PASS 25/25**. The 24-producer / 8-worker / independent-pool stress case passed every repetition.
- New PostgreSQL pass-through test and existing migration, conflict, unique-binding, atomic rollback, lease competition/database clock/recovery, process-kill and receiver deduplication tests: **PASS**, not skipped.
- Six B2-3 tests: **PASS**.
- Full `go test -p 1 -count=1 ./...`: **PASS**, including B1-R/B2-1/B2-2/B2-3.
- Build and diff/clean-tree gates: **PASS**.
- Initial run [36799196981](https://github.com/volcanogithub/BEpusdt-bimhu-secure/actions/runs/36799196981) failed workflow syntax validation before any tests; corrected transparently in the CI-only follow-up commit. This was not a passed test run.
- All operations used the GitHub connector and GitHub Actions. Main unchanged; no deployment, chain transactions, production notifications or BIMHU CREDIT.

### Repeat acceptance failure and follow-up

Final documentation-only tip `da29e6b846c7b082c72015735545908001f4b39f` triggered
[run 36800357211](https://github.com/volcanogithub/BEpusdt-bimhu-secure/actions/runs/36800357211):
the original worker test passed 20/20, but one stress repetition exhausted the
16-attempt budget (only 5/24 outbox rows completed). Other four stress repetitions
passed. Therefore the earlier green run alone was insufficient and its PASS is
superseded pending reacceptance. No failed evidence is deleted.

Follow-up adds per-pool, cancellable write admission around database-only work,
retaining finite busy retries between independent pools/processes. The stress
test still runs 24 producers, 8 workers, two pools with 8 connections each, and
unchanged assertions. No test workload is reduced.

Final acceptance strengthens (not relaxes) the SQLite repetition gate to 20 runs
per test and adds queued-writer cancellation/independent-pool admission coverage.
