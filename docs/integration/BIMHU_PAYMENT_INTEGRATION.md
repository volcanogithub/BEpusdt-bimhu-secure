# BIMHU Payment Service integration boundary

Status: design placeholder only. Integration has **not** started.

## Trust boundary

All BEpusdt order states, MQTT messages, and HTTP callbacks are `UNTRUSTED_HINT`.

BIMHU Payment Service is the sole authority for CREDIT and must independently verify:

1. expected network/profile and chain identity;
2. transaction and receipt finality;
3. canonical `network + tx_hash + event_index` identity;
4. token contract and event signature;
5. sender/recipient and exact integer token amount;
6. confirmation threshold and reorganization handling;
7. durable replay uniqueness and idempotent CREDIT;
8. agreement between independent RPC providers where required.

## Prohibited coupling

- No direct CREDIT from a BEpusdt `success` state.
- No direct CREDIT from MQTT or HTTP callbacks.
- No production API tokens, wallet private keys, database files, or Docker secrets in this repository.
- No Nile or mainnet transaction is authorized by this document.

The concrete protocol, authentication, retry contract, and reconciliation workflow will be designed and reviewed in Phase B3.