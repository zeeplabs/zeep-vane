**Result**: PASS

# Validation — domain-verification-state

Independent Verifier report. Diff range: `e73d600..b9f6240` (commits `a2e1d25`, `ac58e32`, `5912f07`, `b9f6240`).

This is a fix→re-verify cycle (1 iteration). The prior run (this file, diff range `e73d600..5912f07`) returned FAIL because DOMVER-08's "DNS resolves but mismatches configured target → error" branch had no test coverage — a mutant deleting that branch survived the full suite. Commit `b9f6240` (`test(api): cover DNS-mismatch verification failure path`) added `TestVerifyDomain_DNSMismatch_ErrorWithLastError` to close the gap; this re-verification confirms it.

## Per-AC evidence

| AC | File:line | Assertion | Spec outcome | Covered? |
| --- | --- | --- | --- | --- |
| DOMVER-01 (list includes fields) | `internal/api/domains_handler_test.go` (List tests, `toDomainResponse`) | response includes `domain_type/status/ssl_status/verified_at/last_error` | exact fields | Yes |
| DOMVER-02 (new domain defaults) | `internal/api/domains_handler_test.go:383-395`; `internal/db/domain_repository_test.go:151` | `DomainType=custom`, `Status=pending`, `SSLStatus=pending`, `VerifiedAt=nil` | matches | Yes |
| DOMVER-03 (CNAME target) | `domains_handler_test.go:399-434` (`TestListDomains_DNSTargetConfigured_IncludedInResponse`, `...Unconfigured_NullInResponse`) | configured target returned; unset → `null` | matches | Yes |
| DOMVER-04 (verify persists real result, 200) | `domains_handler_test.go:467-497` | 200, status/ssl_status/verified_at/last_error persisted | matches | Yes |
| DOMVER-05 (unknown id → 404, no network call) | `domains_handler_test.go:526-536` | 404 for nonexistent id | matches; test doesn't explicitly assert verifier.Verify() wasn't called, but `GetByID` fails before `h.verifier.Verify` is reached in code (`domains_handler.go` Verify — confirmed by reading control flow) | Yes (code-confirmed) |
| DOMVER-06 (cooldown → 200 existing state, no fresh call) | `domains_handler_test.go:554-581` (`counter.calls != 1` assertion) | 200 with existing state, verifier called once total | matches exactly; genuinely distinguishes from `StatusPagesHandler`'s 429 | Yes |
| DOMVER-07 (success mapping) | `domains_handler_test.go:467-497` | DNS resolves + TLS valid → verified/active | matches | Yes |
| DOMVER-08 (failure mapping, DNS not resolved) | `domains_handler_test.go:501-523` | DNS unresolved → error + last_error populated | matches | Yes |
| DOMVER-08 (failure mapping, DNS resolves but mismatches target) | `domains_handler_test.go:528-559` (`TestVerifyDomain_DNSMismatch_ErrorWithLastError`) | `fakeDomainVerifier{result: domainVerificationResult{DNSResolved: true, DNSMatchesTarget: &mismatch}}` (mismatch=false, lines 531-533) → `status == "error"`, populated `last_error`, and `ssl_status == "active"` asserted independently (TLS success not blanked by DNS failure) | matches spec's Assumptions table exactly; exercises `mapDomainVerificationResult`'s `case result.DNSMatchesTarget != nil && !*result.DNSMatchesTarget:` (`domains_handler.go:194`), distinct from the `!result.DNSResolved` branch (line 190) already covered above | **Yes — gap closed** |

## Discrimination sensor (scratch worktree, discarded after)

Prior run (diff `e73d600..5912f07`):
1. Removed the `DNSMatchesTarget` mismatch case from `mapDomainVerificationResult` (always "verified" once DNS resolves) → full `internal/api` integration suite still **PASSED**. **Mutant survived** — confirmed the DOMVER-08 gap, not just a documentation nit.
2. Made `checkVerifyCooldown` a no-op (always allow + always record) → `TestVerifyDomain_WithinCooldown_ReturnsExistingStateNoNewCheck` **FAILED**. Caught.
3. Swapped `status`/`ssl_status` positional params in `SetVerificationResult`'s UPDATE SQL → `TestDomainRepository_SetVerificationResult_Success_UpdatesAllFields` and `TestVerifyDomain_Success_VerifiedActive`/`WithinCooldown` **FAILED**. Caught.

Prior sensor result: 2/3 caught, 1/3 survived (mutant 1).

Re-verification of this cycle (in-place file edit, reverted from backup after, never staged/committed/stashed):
1. Re-applied mutant 1 — `case result.DNSMatchesTarget != nil && !*result.DNSMatchesTarget:` in `mapDomainVerificationResult` (`domains_handler.go:194`) changed to `case false && result.DNSMatchesTarget != nil && !*result.DNSMatchesTarget:` (dead branch, always falls to `default: status = "verified"`). Ran `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/... -run TestVerifyDomain -v` against a disposable Postgres (`verifier-test-pg5`, port 5439). Result: `TestVerifyDomain_DNSMismatch_ErrorWithLastError` **FAILED** (`Status = "verified", want "error"`; `LastError is nil/empty`). **Mutant now caught.**
2. File restored from a pre-edit backup; `git diff --stat internal/api/domains_handler.go` confirmed zero diff afterward.

Sensor result (this cycle): 1/1 targeted re-run caught. Combined with the prior run's mutants 2/3, all 3 discrimination-sensor mutants are now caught.

## Other checks

- `go build ./...`, `go vet ./...`: clean (re-run this cycle).
- `gofmt -l` on changed `.go` files (`domains_handler.go`, `domains_handler_test.go`): clean.
- Integration gate (disposable Postgres `verifier-test-pg5`, port 5439, `max_connections=300`, `-p 1`, `./internal/db/... ./internal/api/... ./internal/cli/...`): all packages **PASS** (`internal/db` 10.1s, `internal/api` 27.1s, `internal/cli` 8.0s). Container stopped and removed afterward.
- CHECK constraint on `domain_type`: confirmed via `\d domains` (`CHECK (domain_type = 'custom'::text)`) and by directly attempting `INSERT ... domain_type = 'subdomain'` against a live disposable DB → rejected with `violates check constraint "domains_domain_type_check"`.
- No regression to `StatusPagesHandler.VerifyDomain`: its existing tests (`TestVerifyDomain_ValidRequest_200ReturnsCheckResult`, `TestVerifyDomain_SecondCallWithinCooldown_429`, etc.) still pass unchanged, and the two surfaces use the shared `verifyDomainCooldown` constant without redefinition — no tangling found.
- Cooldown map key space: `DomainsHandler.lastVerifyAt` is per-handler-instance and keyed by domain id, separate from `StatusPagesHandler.lastVerifyAt` (separate struct, separate map) — no shared-state collision between the two surfaces.

## Verdict

**PASS** — all DOMVER-01..08 acceptance criteria have direct test evidence, including the previously-missing DOMVER-08 DNS-mismatch branch (`TestVerifyDomain_DNSMismatch_ErrorWithLastError`, `domains_handler_test.go:528-559`). The discrimination sensor's mutant 1 (deleting the DNS-mismatch case) is now caught. Build/vet/fmt clean, full integration gate green, no regression to `StatusPagesHandler.VerifyDomain`. This was a fix→re-verify cycle: 1 iteration (FAIL on `5912f07` → fix in `b9f6240` → PASS).
