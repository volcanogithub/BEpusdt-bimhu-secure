# Credential management

## Generation

Phase B2-1 generates every bootstrap credential from independent operating-system CSPRNG material through Go `crypto/rand`:

- administrator username: 96 random bits;
- administrator password: 192 random bits;
- session/admin secret: 256 random bits;
- hidden administrator path: 144 random bits;
- API authentication token: 256 random bits.

Values use unpadded URL-safe Base64. Time, process identifiers, MD5, deterministic seeds, and other predictable inputs are not used. A CSPRNG failure aborts initialization or reset rather than falling back to a weak value.

## Storage and disclosure

- The administrator password continues to be stored only as a bcrypt hash at the existing default cost.
- The existing configuration storage model for the session secret, hidden path, username, and API token is unchanged by B2-1.
- First-install plaintext values remain available only through the existing one-time installation page. They are not printed to stdout or application logs.
- The CLI `reset` command writes its administrator credential handoff to a temporary file opened with mode `0600`; stdout contains only the file path and a deletion reminder. Delete the file immediately after secure retrieval.
- Do not commit credential handoff files, `.env` files, databases, wallet keys, API tokens, or production configuration.

On Unix-like deployment hosts, verify that the service account owns the handoff file and that its effective mode is `0600`. Windows permission bits do not express the complete ACL; Windows operators must additionally verify the temporary directory ACL before using CLI reset in production.

## Rotation

- Administrator login credentials and hidden path: use the existing `reset` command during a controlled maintenance window, retrieve the one-time file locally, and delete it after transferring the values to the authorized operator.
- API token: use the authenticated administrator token-reset operation. Update authorized API clients through the deployment secret channel; never place the new value in logs or source control.
- Session/admin secret: B2-1 makes new installations unpredictable but does not add an online rotation workflow. Rotating it requires a controlled configuration update and invalidates active administrator sessions.

## Upgrade and migration

No data migration is required. Existing non-empty configuration databases retain their administrator username, bcrypt password hash, session secret, hidden path, and API token unchanged. The new generator runs only for an empty initial configuration or an explicit credential-reset action.

Therefore upgrading does not invalidate existing credentials. Existing credentials originally produced by the predictable legacy algorithm remain weak until deliberately rotated; this is an operational migration risk, not an automatic database migration. Schedule rotation after upgrade and deliver new values through an approved secret-management channel.

BEpusdt status, MQTT, and HTTP callbacks remain `UNTRUSTED_HINT`; this credential change does not introduce or authorize BIMHU CREDIT integration.
