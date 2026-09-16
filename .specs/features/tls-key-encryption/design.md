# TLS Key Encryption Design

**Spec**: `.specs/features/tls-key-encryption/spec.md`

## Decision Summary

A new `certmagic.Storage` decorator, `EncryptedStorage`, wraps `PostgresStorage` and seals only values whose key ends in `.key` (the domain private key and the ACME account private key) with `internal/crypto` under `VANE_MASTER_KEY`, using a versioned, self-describing envelope embedded in the existing `value` bytes. Public artifacts (`.crt`, `.json`) pass through untouched, as do all other `Storage` methods. Legacy plaintext `.key` rows are returned verbatim on read (so an upgrade cannot break existing certificates) and are sealed by an idempotent, advisory-locked, best-effort backfill that runs before the HTTPS listener starts. A sealed key that will not decrypt fails explicitly — never reported as missing — so a wrong master key cannot trigger a mass ACME reissue.

## Architecture

| Component | File | Role |
| --- | --- | --- |
| `EncryptedStorage` (new) | `internal/tls/encrypted_storage.go` | Decorator implementing `certmagic.Storage`; seals/opens `.key` values around an inner storage. |
| `sealSecret` / `isSealed` / `isSecretKey` (new) | `internal/tls/encrypted_storage.go` | Envelope helpers and the `.key` predicate. |
| `EncryptLegacyKeys` (new) | `internal/tls/key_backfill.go` | Idempotent, advisory-locked migration of legacy plaintext `.key` rows. |
| `PostgresStorage` | `internal/tls/postgres_storage.go` (unchanged) | Inner storage; stays crypto-agnostic (`AD-013`). |
| `crypto.Encrypt` / `crypto.Decrypt` | `internal/crypto/secretbox.go` (unchanged) | AES-256-GCM + PBKDF2 sealing, already used for every other secret. |
| `newHTTPSServer` | `internal/cli/serve.go` (changed) | Gains `masterKey`; wraps the storage and runs the backfill before serving. |

### `EncryptedStorage`

```go
type EncryptedStorage struct {
    inner     certmagic.Storage
    masterKey string
}

func NewEncryptedStorage(inner certmagic.Storage, masterKey string) *EncryptedStorage

var _ certmagic.Storage = (*EncryptedStorage)(nil)
```

Stateless (no mutex/fields beyond the two above), so it is safe for concurrent use whenever the inner storage is. It overrides only `Store` and `Load`; all other methods (`Delete`, `Exists`, `List`, `Stat`, `Lock`, `Unlock`) delegate to `inner` unchanged.

| Method | `.key` value | non-`.key` value |
| --- | --- | --- |
| `Store` | `sealSecret` then inner `Store` | inner `Store` verbatim |
| `Load` | sealed → `crypto.Decrypt`; legacy (no marker) → returned as-is | inner `Load` verbatim |
| others | delegate, no decrypt | delegate |

### Envelope

```go
var secretEnvelopePrefix = []byte("vane:tls-secret:v1:")

func sealSecret(masterKey string, plaintext []byte) ([]byte, error) {
    enc, err := crypto.Encrypt(masterKey, plaintext) // nonce || ciphertext
    if err != nil { return nil, err }
    return append(append([]byte{}, secretEnvelopePrefix...), enc...), nil
}

func isSealed(value []byte) bool { return bytes.HasPrefix(value, secretEnvelopePrefix) }
func isSecretKey(key string) bool { return strings.HasSuffix(key, ".key") }
```

Why a text marker rather than a schema column: it is self-describing (the format version travels with the value), needs **no DDL**, and cannot collide with legacy plaintext (a PEM key begins with `-----BEGIN`). `crypto.Encrypt` itself is unchanged.

## Data Flow

**Seal on write (issuance/renewal):** CertMagic `Store(key, pem)` → decorator sees `.key` → `sealSecret` → `PostgresStorage.Store` upsert (unchanged `ON CONFLICT`). Each write uses a fresh random nonce.

**Open on read (handshake/cache miss):** decorator `Load(key)` → inner returns bytes → if `.key` and sealed, `crypto.Decrypt`; if `.key` and not sealed, return the legacy plaintext unchanged; otherwise return the inner bytes.

**Backfill at startup (when `VANE_HTTPS_ENABLED`):**

1. `newHTTPSServer` builds `PostgresStorage`, wraps it in `EncryptedStorage`, then calls `EncryptLegacyKeys(ctx, pool, masterKey, logger)` before constructing the CertMagic manager.
2. The backfill opens a transaction, takes `pg_try_advisory_xact_lock(backfillLockKey)`; if not acquired, another replica is doing it → return `nil` (skip).
3. It selects `key, value FROM certmagic_storage WHERE key LIKE '%.key'`, skips any value already carrying the marker, and for each legacy row runs `UPDATE certmagic_storage SET value = $2 WHERE key = $1 AND value = $3` (the `$3` guard makes it a no-op if the row changed under it).
4. `modified_at` is deliberately **not** touched, so nothing that reads `Stat.Modified` observes the migration.
5. The transaction-scoped lock releases on commit. The caller wraps the whole call in a short timeout and, on error, logs a warning and continues boot (`TLSKEY-08`).

## Error Handling

| Situation | Behavior | Rationale |
| --- | --- | --- |
| `.key` sealed, wrong master key / tampered / truncated | `Load` → `fmt.Errorf("tls: failed to decrypt %s (wrong VANE_MASTER_KEY?): %w", key, crypto.ErrDecryptionFailed)` | Explicit and `errors.Is`-able; **not** `fs.ErrNotExist`, so CertMagic does not treat it as "no key" and mass-reissue (`TLSKEY-05`). |
| `.key`, no marker | returned unchanged | Legacy pass-through; the backfill is responsible for sealing (`TLSKEY-06`). |
| `crypto.Encrypt` error (nonce generation) | `Store` returns the wrapped error; CertMagic surfaces the issuance/storage failure | Cannot seal — never silently store plaintext. |
| Backfill error (any cause) | `logger.Warn` with the error; boot continues | Reads still work for legacy plaintext; encryption completeness must not become a boot blocker (`TLSKEY-08`). |
| Backfill finds zero legacy rows | no-op, logs count `0` | Idempotent; normal steady state after the first run. |
| Advisory lock not acquired | return `nil` silently (debug log) | Another replica owns the work; idempotency makes the outcome identical. |
| HTTPS disabled | backfill not called | No HTTPS listener means no TLS key traffic; documented edge case. |

## Testing Strategy

Backend-only, no frontend, no schema. The decorator and envelope are pure logic — unit-tested against an in-memory `certmagic.Storage` fake. The backfill touches Postgres — integration-tested against the disposable database per `AGENTS.md` §3.

| Layer | Type | Cases |
| --- | --- | --- |
| `EncryptedStorage` seal/open | unit | `.key` stored sealed (marker present, differs from plaintext) and round-trips; non-`.key` (`*.crt`, `*.json`) stored/loaded byte-for-byte; legacy `.key` (no marker) returned unchanged; wrong master key → error `Is(crypto.ErrDecryptionFailed)` and **not** `Is(fs.ErrNotExist)`; PEM `-----BEGIN...` accepted as legacy (no false seal). |
| `EncryptedStorage` delegation | unit | `Delete`/`Exists`/`List`/`Stat`/`Lock`/`Unlock` reach the fake inner unchanged, including for a `.key` key (`Stat` must not decrypt). Compile-time `certmagic.Storage` assertion. |
| `isSecretKey` coverage | unit | Real CertMagic key shapes (`certificates/acme-v02.../example.com/example.com.key`, `acme/acme-v02.../users/.../...key`) match; `.crt`/`.json`/directory keys do not. |
| `EncryptLegacyKeys` | integration | Legacy plaintext `.key` row becomes sealed and decrypts to the original; sibling `.crt` row byte-identical; `modified_at` unchanged; second run is a no-op (idempotent); concurrently (`pg_try_advisory_xact_lock`) only one run does work. |
| End-to-end round trip | integration | Store a real PEM through the decorator into Postgres, Load it back equal; then a fresh decorator with a different master key fails loudly. |
| Regression | unit | A non-`.key` value containing arbitrary bytes is never modified, even if it happens to contain the marker text. |

Discrimination sensor (Execute flow): revert `Store` to skip sealing and confirm the seal test fails; revert `Load` to skip decrypting and confirm the round-trip test fails; revert the `.key` predicate to always-true and confirm the non-`.key` passthrough test fails; revert the decrypt error to `fs.ErrNotExist` and confirm the wrong-key test fails; revert the backfill's marker skip and confirm the idempotency test fails.

## Risks

- **Availability now depends on `VANE_MASTER_KEY` stability.** Losing/rotating the master key makes sealed `.key` values undecryptable and TLS handshakes for affected hostnames fail. This is the user-approved trade-off (no automatic reissue); every other encrypted secret already has the same dependency. Documented in `README.md` and `AD-030`.
- **PBKDF2 cost per `.key` operation.** `crypto` derives the key with 210k PBKDF2 iterations on every call. `.key` reads/writes are a cold path (CertMagic caches certificates in memory; keys are read on cache miss and written on issuance/renewal), so this is acceptable. A derived-key cache is explicitly out of scope until measured.
- **`Stat.Size` for `.key` reports the sealed length.** CertMagic uses `Stat` for existence/terminality, not size-based renewal decisions; accepted and documented.
- **`.key` suffix is the seal predicate.** A future CertMagic release could introduce a differently named secret artifact that would be missed. The threat is bounded (public artifacts already plaintext by design), and the sealed-value read path is marker-based, so format changes remain detectable; a new key type would be a follow-up, not a silent regression of existing keys.
- **Backfill on many replicas.** Idempotency plus the advisory lock make concurrent/duplicate runs safe; the guarded `UPDATE` prevents clobbering a concurrent renewal write.
- **No plaintext left behind on disk either.** `PostgresStorage` never writes to local disk (design's "no local disk cache"), so there is no filesystem copy to migrate; the only store is the table.

## Files Touched

- `internal/tls/encrypted_storage.go` (new) + `internal/tls/encrypted_storage_test.go` (new)
- `internal/tls/key_backfill.go` (new) + `internal/tls/key_backfill_integration_test.go` (new, `//go:build integration`)
- `internal/cli/serve.go` (`newHTTPSServer` signature + wiring + best-effort backfill call)
- `README.md` (`VANE_MASTER_KEY` description)
- `.specs/STATE.md` (`AD-030`)

## Rejected Alternatives

| Alternative | Why rejected |
| --- | --- |
| Encrypt inside `PostgresStorage` | Couples persistence and crypto in one struct; the decorator keeps `PostgresStorage` (and its `AD-013` contract) untouched and is unit-testable without a database. |
| New column / dedicated table + DDL | An envelope in `value` needs no migration and no dual-write; the `0018` table stays as-is. |
| Encrypt all values (no `.key` predicate) | Encrypts data that is public by design and changes `Stat.Size` for every artifact, for no threat-model gain. |
| Blocking migration subcommand / boot abort | Adds a new startup failure mode; pass-through read + best-effort backfill closes the window without it. |
| Reissue on decrypt failure | A wrong master key would trigger ACME issuance for every hostname, risking Let's Encrypt rate limits; explicit failure is safer. |
