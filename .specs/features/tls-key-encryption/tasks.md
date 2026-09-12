# TLS Key Encryption Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/tls-key-encryption/design.md`
**Spec**: `.specs/features/tls-key-encryption/spec.md`
**Status**: Approved (2026-09-12).

---

## Test Coverage Matrix

> Generated from codebase (`AGENTS.md` §3, sampled `internal/tls/postgres_storage.go`, `internal/crypto/secretbox.go`, `internal/cli/serve.go`) and spec ACs - confirm before Execute. No schema, no frontend, no new config. The decorator/envelope are pure and unit-testable against an in-memory `certmagic.Storage` fake; the backfill and end-to-end round trip need the disposable Postgres.

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| `EncryptedStorage` + envelope helpers (pure) | unit | 1:1 to TLSKEY-01..06, TLSKEY-09; wrong-key error identity; delegation to inner for every non-Store/Load method; `.key` predicate shapes | `internal/tls/encrypted_storage_test.go` | `go test ./internal/tls/...` |
| `EncryptLegacyKeys` (Postgres) | integration | Legacy `.key` sealed + decryptable; `.crt` untouched; `modified_at` preserved; idempotent re-run; advisory lock single-run | `internal/tls/key_backfill_integration_test.go` (`//go:build integration`) | `TEST_DATABASE_URL=... go test -tags=integration ./internal/tls/...` |
| End-to-end decorator over Postgres | integration | Round-trip real PEM; wrong master key fails explicitly (not `fs.ErrNotExist`) | `internal/tls/encrypted_storage_integration_test.go` (`//go:build integration`) | `TEST_DATABASE_URL=... go test -tags=integration ./internal/tls/...` |
| Wiring | build | `newHTTPSServer` seals + backfills before serving; no new env var | `internal/cli/serve.go` | `go build ./... && go vet ./...` |
| Decision record + docs | none | Build/format only | `.specs/STATE.md`, `README.md` | `go build ./...` |

## Gate Check Commands

> Generated from `AGENTS.md` §3 - confirm before Execute.

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick (Go, no DB) | After T1, T3 | `go build ./... && go vet ./... && gofmt -l <changed files> && go test ./internal/tls/...` |
| Full (Go, DB-touching) | After T2, T4 | Spin disposable Postgres per `AGENTS.md` §3, then `TEST_DATABASE_URL=... go test -tags=integration -count=1 -p 1 ./internal/tls/...`, then destroy the container |
| Build (phase completion) | End of every phase | Quick + Full, both green |

---

## Execution Plan

Phases are ordered and run sequentially - each phase completes before the next begins, and tasks within a phase execute in order.

### Phase 1: Storage decorator

```
T1
```

### Phase 2: Legacy-key backfill

```
T1 → T2
```

### Phase 3: HTTPS wiring

```
T2 → T3
```

### Phase 4: End-to-end verification

```
T1 → T4
T2 → T4
```

### Phase 5: Decision record and docs

```
T3 → T5
```

---

## Task Breakdown

### T1: `EncryptedStorage` decorator and envelope

**What**: New `certmagic.Storage` decorator that seals `.key` values with `crypto.Encrypt` under a versioned envelope and opens them on `Load`, passing everything else (including legacy plaintext `.key`) through; delegate every other method to the inner storage.
**Where**: `internal/tls/encrypted_storage.go`
**Depends on**: None
**Reuses**: `internal/crypto.Encrypt`/`Decrypt`, the `certmagic.Storage` interface, `PostgresStorage` as the inner implementation.
**Requirement**: TLSKEY-01, TLSKEY-02, TLSKEY-03, TLSKEY-04, TLSKEY-05, TLSKEY-06, TLSKEY-09

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `NewEncryptedStorage(inner, masterKey)` + `var _ certmagic.Storage = (*EncryptedStorage)(nil)`
- [x] `Store` seals only `.key` values; non-`.key` stored byte-for-byte
- [x] `Load` decrypts sealed `.key`; returns legacy plaintext `.key` unchanged; passes other keys through
- [x] Decrypt failure → error wrapping `crypto.ErrDecryptionFailed`, never `fs.ErrNotExist`, never ciphertext
- [x] Envelope is `vane:tls-secret:v1:` + `crypto.Encrypt` output; `isSecretKey` matches real CertMagic `.key` shapes
- [x] `Delete`/`Exists`/`List`/`Stat`/`Lock`/`Unlock` delegate unchanged (incl. `.key` `Stat` without decryption)
- [x] Gate check passes: `go build ./... && go vet ./... && gofmt -l internal/tls/encrypted_storage.go && go test ./internal/tls/...`
- [x] Test count: ≥8 new subtests

**Tests**: unit
**Gate**: quick

**Commit**: `feat(tls): encrypt private keys at rest with a storage decorator`

---

### T2: Legacy plaintext key backfill

**What**: `EncryptLegacyKeys(ctx, pool, masterKey, logger)` sealing every not-yet-sealed `.key` row in `certmagic_storage`, idempotently, preserving `modified_at`, guarded by a transaction-scoped advisory lock and a value-scoped `UPDATE`, best-effort at the call site.
**Where**: `internal/tls/key_backfill.go` (+ `internal/tls/key_backfill_integration_test.go`)
**Depends on**: T1
**Reuses**: `sealSecret`/`isSealed` from T1, `internal/db.Pool`, the existing `certmagic_storage` table (`0018`).
**Requirement**: TLSKEY-07, TLSKEY-08

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Selects only `key LIKE '%.key'`; skips values already carrying the marker
- [x] `UPDATE ... SET value = $2 WHERE key = $1 AND value = $3` — never touches `modified_at`, never clobbers a concurrent write
- [x] Guarded by `pg_try_advisory_xact_lock`; a replica that misses the lock skips without error
- [x] Returns the count of rows sealed (or is log-only) and never panics on zero rows
- [x] Integration test: legacy `.key` sealed + decrypts to original; sibling `.crt` byte-identical; `modified_at` unchanged; second run is a no-op
- [x] Gate check passes on disposable Postgres: `TEST_DATABASE_URL=... go test -tags=integration -count=1 -p 1 ./internal/tls/...`
- [x] Test count: ≥3 new integration tests

**Tests**: integration
**Gate**: full

**Commit**: `feat(tls): backfill legacy plaintext private keys at startup`

---

### T3: HTTPS wiring

**What**: `newHTTPSServer` gains `masterKey`, wraps the `PostgresStorage` in `EncryptedStorage`, and runs `EncryptLegacyKeys` (best-effort, bounded context) before the CertMagic manager is built; `RunE` passes `cfg.MasterKey`.
**Where**: `internal/cli/serve.go`
**Depends on**: T2
**Reuses**: existing `cfg.MasterKey` (already required at boot), `newHTTPSServer` construction path.
**Requirement**: TLSKEY-10, TLSKEY-11

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `newHTTPSServer` signature updated; the storage handed to `vanetls.NewManager` is the decorator
- [x] Backfill called before `NewManager` with a timeout; on error logs a warning and continues
- [x] No new environment variable or config surface
- [x] Gate check passes: `go build ./... && go vet ./... && gofmt -l internal/cli/serve.go`
- [x] Callers updated (`RunE`)

**Tests**: none (compile/wiring)
**Gate**: quick

**Commit**: `feat(tls): wire key encryption and backfill into the HTTPS server`

---

### T4: End-to-end round trip against Postgres

**What**: Integration test proving the decorator round-trips a real PEM through Postgres and that a wrong master key fails explicitly.
**Where**: `internal/tls/encrypted_storage_integration_test.go`
**Depends on**: T1, T2
**Reuses**: Disposable-Postgres integration harness, `EncryptedStorage`, `PostgresStorage`.
**Requirement**: TLSKEY-05, TLSKEY-07, TLSKEY-08

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `Store` a PEM under a `.key` key → row is sealed (marker present, no `-----BEGIN` in the raw column) → `Load` returns the exact PEM
- [x] A decorator built with a different master key → `Load` error `Is(crypto.ErrDecryptionFailed)` and **not** `Is(fs.ErrNotExist)`
- [x] Gate check passes on disposable Postgres: `TEST_DATABASE_URL=... go test -tags=integration -count=1 -p 1 ./internal/tls/...`
- [x] Test count: ≥3 new integration tests

**Tests**: integration
**Gate**: full

**Commit**: `test(tls): cover key encryption round trip and wrong-key failure`

---

### T5: Decision record and README

**What**: Add `AD-030` to `.specs/STATE.md` (decision, reason, trade-off, scope, status) and update the `VANE_MASTER_KEY` description in `README.md` to note it also protects TLS/ACME private keys.
**Where**: `.specs/STATE.md`, `README.md`
**Depends on**: T3
**Reuses**: `AD-NNN` entry format.
**Requirement**: TLSKEY-11

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `AD-030` recorded: decorator + envelope + backfill; trade-off (TLS availability depends on master-key stability; no automatic reissue); status active
- [ ] `README.md` `VANE_MASTER_KEY` line mentions TLS/ACME private keys
- [ ] No new env var documented (none added)
- [ ] Gate check passes: `go build ./...`

**Tests**: none
**Gate**: build

**Commit**: `docs(tls): record private-key encryption decision and config note`
