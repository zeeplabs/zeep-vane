# TLS Key Encryption Specification

**Scope: Small.** One new storage decorator, one idempotent backfill, and the HTTPS wiring that uses them. No schema change, no new config surface, no new dependency. It closes the last plaintext-at-rest gap: the private key material CertMagic persists in `certmagic_storage`.

## Problem Statement

`internal/tls.PostgresStorage` implements `certmagic.Storage` over the `certmagic_storage` table (migration `0018`, `AD-013`), persisting whatever bytes CertMagic hands it in `value BYTEA`. CertMagic stores not only public artifacts (`*.crt`, metadata `*.json`) but also **private key material**: each certificate's domain key (`certificates/<ca>/<host>/<host>.key`) and the ACME account key (`acme/<ca>/users/<email>/<email>.key`). Those bytes sit in the clear. Every other secret Vane persists at rest is already encrypted with `VANE_MASTER_KEY` through `internal/crypto` (AES-256-GCM + PBKDF2) — the Datadog API/app keys, email-provider credentials, LLM provider keys, and TOTP secrets — so the TLS keys are the one exception. Whoever can read the database (a leaked backup, a read replica, a SQL-injection foothold, an operator with `psql`) can read a private key that is valid for a public status-page hostname. This spec encrypts only that private material in place, keeping the public artifacts in plaintext, and migrates the already-stored plaintext keys transparently.

## Goals

- [ ] A `.key` value written through the storage layer is never persisted as plaintext — it is sealed with the operator's `VANE_MASTER_KEY`.
- [ ] Public artifacts (`.crt`, `.json`) are stored and returned byte-for-byte unchanged, with no encryption attempted.
- [ ] Keys already stored in plaintext keep working (pass-through read) and are sealed by an idempotent backfill at startup.
- [ ] A sealed key that cannot be decrypted with the current master key fails loudly and is never silently reported as "missing" (which would trigger a mass ACME reissue and risk Let's Encrypt rate limits).
- [ ] No new environment variable, no schema migration, and no change to `PostgresStorage` itself — the crypto concern is isolated in a decorator implementing `certmagic.Storage` in full.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Encrypting `*.crt` / `*.json` (public artifacts) | They are public by design (served to visitors); encrypting them adds crypto cost to every read and changes `Stat.Size` for no threat-model gain. |
| Encrypting other secrets (Datadog, email, LLM, TOTP) | Already covered by `internal/crypto` at their own call sites; this feature only closes the TLS-key gap. |
| A schema change (new column / table) | The envelope is embedded in the existing `value` bytes, so no DDL is needed and the `0018` table is untouched. |
| Master-key rotation or a key-verification sentinel | Out of scope; losing/rotating `VANE_MASTER_KEY` already breaks every other encrypted secret today. TLS keys join that set. |
| Automatic ACME reissue on decrypt failure | Deliberately rejected (see Assumptions): it can mass-reissue and trip Let's Encrypt rate limits. |
| A derived-key cache to avoid repeated PBKDF2 | `.key` reads/writes are a cold path (CertMagic caches certificates in memory); optimizing it is YAGNI until measured. |
| `VANE_MASTER_KEY` becoming optional | It is already required at boot (`config.Load` → `requireSecret`); nothing changes there. |
| Migrating fallback/reconciliation of any other `AD-013` mechanism | Unrelated; each degrades per its own fail mode. |

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| What is encrypted | Only values whose key ends in `.key` (domain key + ACME account key) | Confirmed with the user (2026-09-12): protect the actual secret (private key) with the smallest blast radius, and leave public artifacts untouched. | y (user) |
| Migration of existing plaintext rows | Versioned envelope + pass-through read + idempotent backfill at startup | Confirmed with the user (2026-09-12): closes the exposure window immediately without DDL and without a blocking boot failure mode. | y (user) |
| Decrypt failure posture | Explicit error (wrapping `crypto.ErrDecryptionFailed`), never `fs.ErrNotExist`; no automatic reissue | Confirmed with the user (2026-09-12): a wrong/lost master key must not silently trigger mass ACME issuance and risk Let's Encrypt rate limits. | y (user) |
| Architecture | A `certmagic.Storage` decorator (`EncryptedStorage`) wrapping `PostgresStorage` | Confirmed with the user (2026-09-12): keeps `PostgresStorage` crypto-agnostic, isolates the concern, and is unit-testable without a database. | y (user) |
| Envelope format | `vane:tls-secret:v1:` ASCII prefix, then the raw `crypto.Encrypt` blob (nonce + ciphertext) | Self-describing and versioned so a future algorithm change can be detected from the value alone; a PEM private key starts with `-----BEGIN`, so the prefix cannot collide with legacy plaintext. | y — agent default, no objection raised |
| Legacy detection on read | Absence of the envelope prefix on a `.key` value means legacy plaintext and is returned as-is | No ambiguity with PEM; avoids a "probe decrypt and treat failure as legacy" path that would mask a genuinely wrong master key. | y — agent default, no objection raised |
| Backfill placement / durability | Runs in the HTTPS server construction path at startup, before the listener serves; best-effort (logs a warning, never aborts boot) | The keys only exist to serve the HTTPS listener; a backfill failure is not a reason to refuse to start, and reads still work for legacy plaintext. | y — agent default, no objection raised |
| Backfill concurrency (HA) | Guarded by a transaction-scoped advisory lock (`pg_try_advisory_xact_lock`); a replica that does not get the lock skips; the operation is idempotent regardless | Multiple replicas boot together; the lock avoids duplicated work, and idempotency makes correctness independent of the lock. | y — agent default, no objection raised |
| Backfill and `modified_at` | The backfill updates only `value` (`UPDATE ... WHERE key = $1 AND value = $2`), preserving `modified_at` and not clobbering a concurrent write | Avoids perturbing any CertMagic behavior that reads `Stat.Modified`, and keeps the update race-safe. | y — agent default, no objection raised |
| Rotation of the underlying secret key | **No** | Only the payload is encrypted; per-value random nonces and AES-GCM tags make key rotation unnecessary. (Distinct from `VANE_MASTER_KEY` rotation, which is out of scope.) | y — agent default, no objection raised |
| Process / artifacts | A short feature spec under `.specs/features/tls-key-encryption/` + `AD-030` in `.specs/STATE.md` | `AGENTS.md` §1/§6: architectural decisions are logged as `AD-NNN` with the *why*. | y — agent default, no objection raised |

**Open questions:** none — all resolved or logged above.

## User Stories

### P1: Private TLS key material is encrypted at rest ⭐ MVP

**User Story**: As the operator, I want CertMagic's private keys to be unreadable in a database dump, so a leaked backup/replica does not hand an attacker a valid TLS key for a status-page hostname.

**Why P1**: This is the security fix; without it the last plaintext secret is still exposed.

**Acceptance Criteria**:

1. WHEN the storage layer stores a value whose key ends in `.key` THEN it SHALL persist the value sealed with `crypto.Encrypt(masterKey, plaintext)` prefixed by the versioned envelope marker, never the raw plaintext. <!-- event-driven -->
2. WHEN the storage layer loads a `.key` value that carries the envelope marker THEN it SHALL return the decrypted plaintext. <!-- event-driven -->
3. The storage layer SHALL store and load non-`.key` values (e.g. `.crt`, `.json`) byte-for-byte unchanged, without attempting encryption or decryption. <!-- ubiquitous -->
4. The envelope SHALL be self-describing and versioned (the `vane:tls-secret:v1:` marker), so the format can be changed later from the value alone, without a schema change. <!-- ubiquitous -->
5. IF a `.key` value carries the envelope marker but cannot be decrypted with the current master key THEN the storage layer SHALL return an explicit error (wrapping `crypto.ErrDecryptionFailed`), SHALL NOT return `fs.ErrNotExist`, and SHALL NOT return the ciphertext. <!-- unwanted-behavior -->

**Independent Test**: With a fake storage and a known master key, store a `.key` value and assert the wrapped bytes carry the marker and differ from the plaintext; load it back and assert round-trip equality. Store a `.crt` value and assert the wrapped bytes equal the input exactly. Store a `.key` sealed under a different master key and assert `Load` returns a non-`ErrNotExist` error. Reject a PEM starting with `-----BEGIN`, so there is no ambiguity with legacy plaintext.

### P1: Existing plaintext keys are migrated transparently ⭐ MVP

**User Story**: As the operator upgrading an instance that already issued certificates, I want existing plaintext private keys to keep working and then get sealed automatically, so the upgrade is invisible and the exposure window closes.

**Why P1**: Without pass-through, every existing certificate would fail to load on upgrade; without the backfill, the plaintext would stay on disk indefinitely.

**Acceptance Criteria**:

1. WHEN the storage layer loads a `.key` value that does NOT carry the envelope marker THEN it SHALL return it unchanged (legacy plaintext pass-through). <!-- state-driven -->
2. WHEN the backfill runs THEN it SHALL seal every not-yet-sealed `.key` row (the value gains the marker and becomes decryptable), leaving already-sealed rows and all non-`.key` rows untouched. <!-- event-driven -->
3. WHEN the backfill runs a second time (or concurrently on another replica) THEN it SHALL be idempotent: already-sealed rows are skipped and the outcome is unchanged. <!-- state-driven -->
4. The backfill SHALL update only `value` (preserving `modified_at`), SHALL scope its write to the value it read (`WHERE key = $1 AND value = $2`) so it cannot clobber a concurrent write, and SHALL be guarded by a transaction-scoped advisory lock so concurrent replicas do not duplicate work. <!-- ubiquitous -->
5. IF the backfill errors for any reason THEN the system SHALL log a warning and continue starting; it SHALL NOT abort the boot. <!-- unwanted-behavior -->

**Independent Test**: Against a real Postgres, insert a legacy plaintext `.key` row plus a plaintext `.crt` row; run the backfill; assert the `.key` row now carries the marker and decrypts to the original, the `.crt` row is byte-identical, and `modified_at` is unchanged; run the backfill again and assert no further change.

### P2: The decorator is a complete, transparent `certmagic.Storage`

**User Story**: As a maintainer, I want the encryption confined to a decorator that implements the whole storage interface, so CertMagic sees identical behavior and `PostgresStorage` stays crypto-agnostic.

**Why P2**: A partial interface would fail to compile as `certmagic.Storage`; an intrusive change to `PostgresStorage` would couple persistence and crypto.

**Acceptance Criteria**:

1. The decorator SHALL satisfy `certmagic.Storage` in full, delegating `Delete`/`Exists`/`List`/`Stat`/`Lock`/`Unlock` to the wrapped storage unchanged. <!-- ubiquitous -->
2. WHEN HTTPS is enabled THEN the HTTPS server SHALL wrap `PostgresStorage` in the decorator using the boot-time master key, and the backfill SHALL run before the listener serves. <!-- event-driven -->
3. No new environment variable or config surface SHALL be introduced; the already-required `VANE_MASTER_KEY` is reused. <!-- ubiquitous -->

**Independent Test**: Assert at compile time that the decorator satisfies `certmagic.Storage`; with a fake inner storage, assert `Delete`/`Exists`/`List`/`Stat` calls reach the inner unchanged (including a `.key` key, where `Stat` must not decrypt).

## Edge Cases

- WHEN a `.key` value is legacy plaintext THEN `Load` returns it verbatim; the backfill is what seals it, not the read path (reads never write).
- WHEN a `.key` value carries the marker but is truncated/corrupt THEN `Load` returns the explicit decrypt error (GCM authentication fails closed), never partial bytes.
- WHEN `Stat` is called on a `.key` THEN it reports the sealed size (ciphertext length); CertMagic does not use `Stat.Size` to decide renewal, so this is accepted and documented.
- WHEN a certificate and its key are renewed THEN `Store` seals the new key with a fresh random nonce; the reused key is overwritten (the `ON CONFLICT` upsert is unchanged).
- WHEN two replicas run the backfill at the same instant THEN one holds the advisory lock and the other skips; even without the lock the guarded `UPDATE` is idempotent, so the result is the same.
- WHEN HTTPS is disabled (`VANE_HTTPS_ENABLED=false`) THEN the backfill is not run (no HTTPS listener); any legacy rows remain plaintext until HTTPS is next enabled. Accepted: without HTTPS there is no TLS key traffic.
- IF the master key is wrong/missing THEN every `.key` load fails explicitly and TLS handshakes for affected hostnames fail; no reissue is triggered. The operator must restore the correct `VANE_MASTER_KEY`.

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| TLSKEY-01 | P1: Store `.key` sealed | T1 | Done |
| TLSKEY-02 | P1: Load `.key` decrypted | T1 | Done |
| TLSKEY-03 | P1: Non-`.key` byte-for-byte passthrough | T1 | Done |
| TLSKEY-04 | P1: Versioned self-describing envelope | T1 | Done |
| TLSKEY-05 | P1: Decrypt failure is explicit, not "missing" | T1, T4 | Done (unit; T4 pending) |
| TLSKEY-06 | P1: Legacy plaintext `.key` pass-through | T1 | Done |
| TLSKEY-07 | P1: Idempotent backfill seals legacy keys | T2, T4 | Done (integration; T4 pending) |
| TLSKEY-08 | P1: Backfill advisory-locked, preserves `modified_at`, best-effort | T2, T4 | Done (integration; T4 pending) |
| TLSKEY-09 | P2: Full `certmagic.Storage` delegation | T1 | Done |
| TLSKEY-10 | P2: HTTPS wiring seals and backfills before serving | T3 | Pending |
| TLSKEY-11 | P2: No new config; decision recorded as `AD-030` | T5 | Pending |

**Coverage:** 11 total, 11 mapped to tasks, 0 unmapped

## Success Criteria

- [ ] `SELECT value FROM certmagic_storage WHERE key LIKE '%.key'` returns only sealed blobs (marker-prefixed) after an upgrade; `.crt`/`.json` rows are byte-identical to before.
- [ ] Loading a sealed `.key` returns the original PEM; loading a `.crt`/`.json` is untouched; a wrong master key produces an explicit decrypt error, never `fs.ErrNotExist`.
- [ ] The backfill is idempotent and race-safe (advisory lock + guarded update) and never blocks startup.
- [ ] The decorator satisfies `certmagic.Storage` in full; `PostgresStorage` is unchanged; no new environment variable.
- [ ] `AD-030` records the decision, the trade-off (TLS availability now depends on `VANE_MASTER_KEY` stability; no automatic reissue), and the scope.
