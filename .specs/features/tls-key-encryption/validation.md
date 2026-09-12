# TLS Key Encryption Validation

**Date**: 2026-09-12
**Spec**: `.specs/features/tls-key-encryption/spec.md`
**Diff range**: `167f88d..13ea26d` (feature artifacts + code; code surface `57a7986..13ea26d`)
**Verifier**: fresh-eyes standalone pass. This harness exposes no worker sub-agent (only read-only `cavecrew-*`), so the Verifier ran as an independent pass over the committed tree rather than a separate agent. **Author = verifier is a known limitation of this run**; it is mitigated by the spec-anchored check, the full gate on a fresh disposable database, and the discrimination sensor.

---

## Task Completion

| Task | Status | Notes |
| --- | --- | --- |
| T1 | ✅ Done | `EncryptedStorage` + envelope; 9 unit test functions |
| T2 | ✅ Done | `EncryptLegacyKeys` backfill; 3 integration test functions |
| T3 | ✅ Done | HTTPS wiring. **Gap found in validation**: the integration-tagged `internal/cli/serve_test.go` callers were missed by the non-integration quick gate → fixed in `13ea26d` |
| T4 | ✅ Done | End-to-end round trip + wrong key; 3 integration test functions |
| T5 | ✅ Done | `AD-030` (bundled with artifacts in `167f88d`) + `README.md` |

---

## Spec-Anchored Acceptance Criteria

| Criterion (WHEN X THEN Y) | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| TLSKEY-01 store `.key` sealed | marker present, plaintext absent | `internal/tls/encrypted_storage_test.go:128` - `if !bytes.HasPrefix(raw, secretEnvelopePrefix)` (+`:130` no-plaintext); integration `encrypted_storage_integration_test.go:38` - `if !isSealed(raw)` | ✅ PASS |
| TLSKEY-02 load sealed `.key` decrypted | original PEM returned | `internal/tls/encrypted_storage_test.go:152` - `if !bytes.Equal(got, plaintext)`; integration `encrypted_storage_integration_test.go:50` - `if !bytes.Equal(got, plaintext)` | ✅ PASS |
| TLSKEY-03 non-`.key` byte-for-byte | raw == input for `.crt`/`.json` | `encrypted_storage_test.go:158` (`.crt`/`.json` raw + load equality); `:292` marker-bearing non-`.key` unchanged; `:285` `Stat` reports sealed size (no decrypt); integration `encrypted_storage_integration_test.go:98` | ✅ PASS |
| TLSKEY-04 versioned self-describing envelope | `vane:tls-secret:v1:` prefix | `encrypted_storage.go:22` constant + `encrypted_storage_test.go:128` asserts its presence | ✅ PASS |
| TLSKEY-05 decrypt failure explicit, never missing | `Is(crypto.ErrDecryptionFailed)`, not `Is(fs.ErrNotExist)` | `encrypted_storage_test.go:220` - `if !errors.Is(err, crypto.ErrDecryptionFailed)`; `:223` - `if errors.Is(err, fs.ErrNotExist)`; integration `encrypted_storage_integration_test.go:88`,`:91` | ✅ PASS |
| TLSKEY-06 legacy plaintext `.key` pass-through | legacy bytes returned unchanged | `encrypted_storage_test.go:193` - asserts `bytes.Equal(got, want)` | ✅ PASS |
| TLSKEY-07 idempotent backfill seals legacy keys | run 1 seals, run 2 no-op | `key_backfill_integration_test.go:70` - `if sealed != 1`; `:123` - second run `n != 0`; e2e legacy load `encrypted_storage_integration_test.go` | ✅ PASS |
| TLSKEY-08 backfill advisory-locked, `modified_at` preserved, best-effort | lock → skip; `modified_at` unchanged | `key_backfill_integration_test.go:92` - `if !keyModified.Equal(modified)`; `:161` - `if n != 0` while locked; `:170` - not sealed while locked. Best-effort warn-and-continue is inspection-only: `internal/cli/serve.go:227-228` | ⚠️ Test-backed except best-effort (inspection) |
| TLSKEY-09 full `certmagic.Storage` delegation | compile assertion + delegated calls | `encrypted_storage.go:33` - `var _ certmagic.Storage = (*EncryptedStorage)(nil)`; `encrypted_storage_test.go:229` delegates Delete/Exists/List/Stat/Lock/Unlock | ✅ PASS |
| TLSKEY-10 HTTPS wiring seals + backfills before serving | decorator handed to manager; backfill before serve | inspection-only: `internal/cli/serve.go:221` wrap, `:227` backfill, `:232` manager; `serve_test.go` callers updated (`13ea26d`) | ⚠️ No automated assertion (wiring) |
| TLSKEY-11 no new config; `AD-030` recorded | no new env var; AD present | `README.md:337` updated; `.specs/STATE.md` `AD-030`; `internal/config/config.go` unchanged in the diff | ✅ PASS |

**Status**: ⚠️ 9/11 ACs test-backed; 2 verified by inspection only (TLSKEY-08 best-effort branch, TLSKEY-10 wiring). No AC failed.

---

## Discrimination Sensor

Scratch `git worktree add --detach /tmp/vane-sensor HEAD` (`13ea26d`); mutations applied to the scratch only, then reverted. Real-worktree porcelain was identical before and after.

| Mutation | File:line | Description | Killed? |
| --- | --- | --- | --- |
| M1 | `internal/tls/encrypted_storage.go:16` | `secretKeySuffix = ".key"` → `""` (predicate always true) | ✅ Killed |
| M2 | `internal/tls/encrypted_storage.go:46` | Inverted `Store` guard (`!isSecretKey` → `isSecretKey`) | ✅ Killed |
| M3 | `internal/tls/encrypted_storage.go:63` | `Load` guard `!isSealed` → `isSealed` (sealed returned raw) | ✅ Killed |
| M4 | `internal/tls/encrypted_storage.go:70` | Decrypt failure returns `nil, nil` instead of an explicit error | ✅ Killed |
| M5 | `internal/tls/encrypted_storage.go:105` | `sealSecret` drops the envelope prefix | ✅ Killed |
| M6 | `internal/tls/key_backfill.go:64` | Removed the already-sealed skip (`!isSealed` → `true`) | ✅ Killed |
| M7 | `internal/tls/key_backfill.go:92` | `UPDATE` also sets `modified_at = now()` | ✅ Killed |

**Sensor depth**: lightweight (7 targeted behavior-level mutations across the highest-risk new code: sealing, opening, failure identity, predicate, backfill idempotency and `modified_at`).
**Result**: **7/7 killed** - PASS ✅.

---

## Edge Cases

- [x] Legacy plaintext `.key` returned verbatim on read (TLSKEY-06 test).
- [x] Corrupt/wrong-key sealed value fails closed via GCM (wrong-key test; `crypto.Decrypt` returns `ErrDecryptionFailed`).
- [x] `Stat` on a sealed `.key` reports the ciphertext size and does not decrypt (`encrypted_storage_test.go:285`).
- [x] Two replicas backfilling concurrently: advisory lock makes one skip (`key_backfill_integration_test.go:137`).
- [ ] Renewal overwrites a key with a fresh nonce: not explicitly asserted (the `ON CONFLICT` upsert is unchanged from `PostgresStorage`). Inspection-only.
- [ ] HTTPS disabled: backfill not called: inspection-only (`newHTTPSServer` is only built when `cfg.HTTPSEnabled`).

---

## Code Quality

| Principle | Status |
| --- | --- |
| Minimum code | ✅ (decorator ~110 lines, backfill ~110 lines) |
| Surgical changes | ✅ (`PostgresStorage`, `crypto`, `config` untouched) |
| No scope creep | ✅ |
| Matches patterns | ✅ (same `certmagic.Storage` idiom, same `crypto` reuse, `db.Pool`/tx usage as elsewhere) |
| Spec-anchored outcome check | ✅ (asserted values match spec; 2 inspection-only items flagged) |
| Per-layer Coverage Expectation met | ✅ (pure decorator unit-tested 1:1 to ACs; Postgres paths integration-tested) |
| Every test maps to a spec requirement - no unclaimed tests | ✅ |
| Documented guidelines followed | ✅ `AGENTS.md` §3 gates; `docs`/`internal/crypto` conventions |

---

## Gate Check

- **Gate command**: Quick (`go build ./... && go vet ./... && gofmt -l <changed>`) + Full (`TEST_DATABASE_URL=... go test -tags=integration -count=1 -p 1 ./...` on a **fresh** disposable Postgres, `max_connections=300`).
- **Result**: **1142 passed, 0 failed** across 25 packages (fresh container). Quick gate clean.
- **Test count before feature**: `internal/tls` 3 unit test functions / 20 integration test functions.
- **Test count after feature**: 12 unit / 26 integration.
- **Delta**: **+9 unit, +6 integration test functions** (no test removed or weakened).
- **Skipped tests**: 0 (in the fresh-container run).
- **Failures**: 0. (An intermediate run against a *reused* container showed 2 `internal/db` singleton failures - the documented stale-singleton artifact, not a regression; a fresh container cleared them.)

---

## Fix Plans (issues found)

### Fix 1: `internal/cli/serve_test.go` callers not updated for the new `newHTTPSServer` signature

- **Root cause**: the T3 quick gate ran `go build ./...`/`go test ./...` without `-tags=integration`, so the integration-tagged `serve_test.go` call sites were never compiled; the full integration gate exposed 6 compile errors.
- **Fix task**: applied in `13ea26d` - pass `"cli-serve-test-master-key"` to all six `newHTTPSServer` calls.
- **Priority**: Blocker (integration build broken).

---

## Requirement Traceability Update

| Requirement | Previous Status | New Status |
| --- | --- | --- |
| TLSKEY-01..07, 09, 11 | Done | ✅ Verified |
| TLSKEY-08 | Done | ✅ Verified (best-effort branch inspection-only) |
| TLSKEY-10 | Done | ✅ Verified (wiring; no automated assertion) |

---

## Summary

**Overall**: ✅ Ready

**Spec-anchored check**: 9/11 ACs test-backed and matching the spec outcome; 2 (TLSKEY-08 best-effort, TLSKEY-10 wiring) verified by inspection, flagged, not failed.
**Sensor**: 7/7 mutations killed.
**Gate**: 1142 passed, 0 failed, 25 packages (fresh disposable DB); quick gate clean.

**What works**: private `.key` values are sealed with `VANE_MASTER_KEY` under a versioned envelope; `.crt`/`.json` pass through byte-for-byte; legacy plaintext keys keep loading and are sealed by an idempotent, advisory-locked backfill that preserves `modified_at`; a wrong master key fails explicitly instead of triggering reissue; the decorator satisfies `certmagic.Storage` in full with `PostgresStorage` untouched.

**Issues found**: one real gap (T3's missed integration test callers), fixed in `13ea26d`. Two ACs lack an automated assertion (wiring and the best-effort branch); both are low-risk and inspection-verified.

**Next steps**: feature is complete and ready to log in the vault. `develop` is ahead of `origin/develop`; push only on explicit request.
