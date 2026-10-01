# Phase B3-0: Interface and gap audit

Date: 2026-10-01
Status: PARTIAL — BEpusdt source-side audit completed; Payment Service source comparison BLOCKED pending repository/path and exact P1 commit.
Source baseline: `8625b1df9e1012a58777117c90cd34f1e5e6d5e1`
Branch: `security/b2-hardening-v1.24.2`

This is a source review and implementation plan, not integration acceptance, production readiness, or a fresh B2 regression run. No application code, schema, payment protocol, deployment or chain transaction is changed by this phase.

## Evidence and limits

Reviewed at the fixed baseline: app/router/epusdt.go; app/handler/epusdt/epusdt.go; app/model/order.go; app/model/trade.go; app/model/chain_event.go; app/model/notification_delivery.go; app/task/tron.go; app/task/tron_rpc.go; app/task/notify/notify.go; app/task/notify/outbox.go; app/utils/utils.go; docs/security/CONFIRMATION_POLICY.md; docs/security/SECURITY_ROADMAP.md; docs/integration/BIMHU_PAYMENT_INTEGRATION.md.
The complete recursive repository tree contains no AGENTS.md.
The security branch ref matched the baseline when checked.

The accessible repository inventory includes epusdt-bimhu-private, but its master tree and README identify an Epusdt project; no Payment Service implementation or P1 evidence was located there. It is not assumed to be the P1 service. Historical user-reported P1 milestones are requirements/reuse candidates, not independently verified code evidence in this audit.

## Existing interfaces

| Interface | Current behavior | Adapter implication |
| --- | --- | --- |
| POST /api/v1/order/create-transaction | Shared-secret signature; amount is float64; creates a selected trade type | Not an integer invoice API; establish precise conversion and persisted actual payable amount |
| POST /api/v1/order/create-order | Shared-secret signature; amount is float64; payment method selected later | Avoid silently binding an invoice before address/network/amount are fixed |
| POST /api/v1/order/cancel-transaction | Shared-secret signature; waiting orders only | Cancellation is business state, never reversal of verified CREDIT |
| POST /api/v1/pay/info | Checkout-facing lookup, optional fingerprint binding | Not a service event evidence endpoint; does not expose the complete canonical event tuple |
| POST /api/v1/pay/notify | Verifies signature and replies ok | Not a BIMHU CREDIT receiver |
| Durable order.success notification | POST EpNotify with event_id and Idempotency-Key / X-BEpusdt-Event-ID; requires HTTP 200 | Receiver must persist a deduplicated hint before ACK |
| Legacy/status callbacks | May omit event_id and durable headers | Must not assume every notification has durable identity |
| Epay notification | GET; event_id appended separately to existing signed parameters | Do not use as the initial BIMHU canonical-event transport |

EpNotify fields: event_id (optional), trade_id, order_id, amount (float64 fiat), actual_amount (decimal string), token (recipient address, NOT token contract), block_transaction_id (transaction hash, NOT block hash), signature, status.
Durable payload event_id is included in EpusdtSign; request headers are not separately covered by that signature. Require body/header agreement when headers are present.
EpusdtSign sorts fields and computes MD5(canonical fields + shared secret). This is the existing compatibility protocol, not HMAC; do not change it in B3-0. A signed callback is still UNTRUSTED_HINT, not payment evidence.

## Confirmed gaps and reuse opportunities

| Finding | Evidence | Required resolution / ownership |
| --- | --- | --- |
| Network labels are not explicit Nile/Mainnet identity | ChainEvent.Network; conf.Tron in scanner; hard-coded token contract bytes | BEpusdt explicit profiles; Payment Service independently validates chain identity. Do not relabel historical identities silently |
| Event identity is available internally | ChainEvent has network, tx_hash, event_index; TRC20 uses zero-based full receipt log position | Reuse binding/outbox; define mapping to Payment Service's network-qualified identity and event index convention |
| Notification omits typed event_index, contract and block hash | EpNotify / outbox payload | Derive receipt facts independently; parse event_id only as hint. Consider additive versioned hint fields later |
| Amount input traverses float64 | createReq / createOrderReq; decimal.NewFromFloat | Invoice/ledger stay exact integer units; verify roundtrip or design additive exact-amount interface before implementation |
| Actual payable amount may differ from requested amount | CalcTradeAmount increments occupied address/amount combinations | Persist selected payable amount before exposing payment instructions; never credit fiat amount or trust callback actual_amount |
| Create retry is not a documented immutable invoice contract | StartBuildOrder may rebuild waiting orders; buildMutex is process-local; OrderId is indexed, not unique | Payment Service owns idempotent operation + account + request key and durable order mapping; test conflicting retries and concurrent creation |
| BEpusdt confirmation still relies on one RPC at a time | TRON confirmation code and confirmation-policy limitations | Payment Service remains independent dual-provider CREDIT authority; fail closed on disagreement, outage or incomplete evidence |
| Delivery can repeat after receiver ACK / sender crash | Notification lease/outbox lifecycle | Durable inbox + unique chain event + unique CREDIT + downstream outbox; never equate HTTP exactly-once delivery with exactly-once credit |
| Finite notification retries can end dead | ProcessOne / FailNotification | Reconciliation and controlled replay are required; callback-only discovery is insufficient |
| Post-success canonicality/reorg reconciliation is not established | CONFIRMATION_POLICY remaining risks | Define handling explicitly before funds integration; do not infer finality from confirmation count alone |
| Callback network policy matters | Existing callback SSRF protections | Integration endpoint must comply with current policy; do not weaken SSRF to allow private targets without separate reviewed design |

Existing local payment event binding, unique event constraints, atomic success/outbox and database-clock notification lease are reuse candidates on the BEpusdt side, not replacement implementations of the BIMHU ledger.

## Proposed contract (design only)

BEpusdt supplies a hint. Payment Service authenticates and validates the envelope, durably stores a deduplicated inbox record, and ACKs HTTP 200 only after commit (including an already persisted duplicate). ACK means receipt, not CREDIT. Invalid or conflicting inputs cannot credit; persistence failures must remain retryable.

Payment Service resolves a durable invoice-to-BEpusdt mapping and independently verifies expected profile/chain identity, transaction hash, successful receipt, canonical inclusion/block hash, exact full-receipt log index, Transfer signature, contract, recipient, integer amount, confirmation policy and provider agreement. Missing or ambiguous event identity is held for reconciliation, never guessed.

Only Payment Service can transition an eligible invoice and atomically create the unique ledger CREDIT and its transactional outbox. Web and BEpusdt have no CREDIT permission and no wallet private keys. Provider disagreement cannot fall back to single-provider CREDIT.

Proposed normalized hint fields: schema_version, source, source_event_id, source_order_id, source_trade_id, expected_network_profile, tx_hash, event_index. Claimed contract/recipient/amount/height/hash, if included, remain untrusted. Exact schema and authentication are deferred until the existing P1 receiver is inspected.

## P1 reuse evidence needed

Obtain the Payment Service repository/path, exact P1 final commit, acceptance report and relevant code/test paths. Verify rather than recreate: invoice idempotency; integer amount representation; cursor rewind/CAS; canonical event uniqueness; 20-confirmation Nile profile; dual-provider authority; unique ledger CREDIT/outbox; downstream idempotency; restart/concurrency tests.
Do not assert these capabilities exist in the currently accessible BEpusdt repository.

## Implementation sequence and gates

1. Complete B3-0 two-sided comparison. Record immutable service SHA, interface mapping, accepted semantics and no duplicate settlement authority.
2. B3-1 explicit network profiles (isolated Nile first): reject wrong chain/contract/decimals/provider; preserve historical event identity through an explicit migration design if needed.
3. B3-2 provider verification responsibility: preferentially reuse P1 verifier; BEpusdt-side dual RPC is a separate scanner-hardening choice, not required duplicate CREDIT authority.
4. B3-3 adapter: durable inbox, exact payable amount mapping, contract tests, reconciliation, unique CREDIT and downstream retry integration.
5. Deployment release gates remain separate: real production image build/runtime, actual domain TLS and renewal, production PostgreSQL validation from the existing roadmap, and unresolved credentials-at-rest treatment.

Required later tests: forged success callback; wrong chain/contract/recipient/amount/log index; multiple logs in one transaction; mismatched body/header IDs; below/at/above confirmation threshold; provider discrepancy/outage; duplicate/concurrent/reordered hints; durable ACK then crash; lost callback/dead-letter replay; request-key payload conflict; payable amount adjustment; expired/cancelled invoice and late payment; reorg/inclusion mismatch; restart recovery and unique CREDIT.
These tests are a plan; no new implementation tests were executed in this documentation-only audit.

## Stop boundary

B3-0 has not passed the complete two-sided audit. No B3-1/2/3 implementation, live Nile transaction, mainnet access, production deployment or secret migration is authorized by this document. No fresh full Go regression is claimed. B1-R/B1-R2/B2 code remains unchanged by this documentation commit.
