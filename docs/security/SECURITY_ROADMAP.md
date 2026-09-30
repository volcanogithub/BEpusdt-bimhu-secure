# Security roadmap

Development branch: `security/b2-hardening-v1.24.2`

Changes must be proposed through pull requests. Do not push directly to `main`.

## Phase B2

- H-1: credential randomization
- H-2: TRON panic fix
- H-3: confirmation default policy
- M-1: SSRF protection
- M-2: filesystem permission hardening
- M-3: HTTPS deployment support
- M-5: login rate limiting
- M-6: secret-safe logging

Each item requires a regression test, configuration migration note, threat/impact statement, and rollback guidance before merge.

## Phase B3

- PostgreSQL production deployment validation
- Dual-RPC verification
- Explicit network profiles
- BIMHU Payment Service integration

Phase B3 cannot treat BEpusdt order state, MQTT, or HTTP callback as proof of payment. BIMHU must independently verify network, transaction, event index, token contract, recipient, amount, confirmation policy, and replay uniqueness before CREDIT.