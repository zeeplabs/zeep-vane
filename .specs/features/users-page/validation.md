# Validation: users-page

**Result**: PASS

**Verifier**: independent (fresh agent, no authorship of the reviewed commits)
**Diff range**: `67c1ca6~1..bc80494` (67c1ca6 backend last_access, bc80494 frontend redesign) + follow-up `ab49ed2` (PhoneField `variant` prop, shared component touching the same invite drawer)
**Date**: 2026-09-15

---

## Real gate (current tree)

| Check | Result |
| --- | --- |
| `go build ./...` | PASS |
| `go vet ./...` | PASS |
| `gofmt -l internal/api/admins.go internal/api/admins_test.go` | clean (no output) |
| `TEST_DATABASE_URL=... go test -tags=integration -p 1 -run 'Admin\|LastAccess' ./internal/api/...` (disposable Postgres, port 5436, destroyed after) | PASS |
| `npx tsc -b --noEmit` (web/) | PASS (clean) |
| `npm run test -- src/features/admins/AdminsPage.test.tsx` | PASS — 16/16 tests |

No integration test run touched `vane-dev-pg`; the disposable container (`users-page-realgate-pg`) was stopped/removed after the run.

---

## Per-AC evidence table

| AC | Requirement (spec.md summary) | Test(s) | file:line | Assertion vs spec | Verdict |
| --- | --- | --- | --- | --- | --- |
| USRPG-01 | Tabela única, ativos+pendentes, ordenados como o backend retorna | `lista todos os usuários...` | `AdminsPage.test.tsx:47-56` | Asserts all 4 seeded users present, pending row shows "Pendente" | Covered |
| USRPG-01 (rótulo) | Rótulos traduzidos, nunca owner/operator/viewer crus | `rótulos de papel são Admin/Membro/Leitura...` | `AdminsPage.test.tsx:58-67` | Asserts Admin/Membro/Leitura per row + absence of raw "owner" text | Covered |
| USRPG-02 | 4 chips com contagem; clique filtra client-side | `chips de papel filtram a tabela com contagem correta` | `AdminsPage.test.tsx:69-79` | Asserts filtering behavior (only owner row visible after clicking "Admin") | **Spec-precision gap**: test name claims "com contagem correta" but never asserts the numeric badge text next to each chip (`counts[value]` rendered at `AdminsPage.tsx:218`). With the seed data (1 owner/2 operator[1 active+1 pending]/1 viewer) the expected badges would be Todos=4, Admin=1, Membro=2, Leitura=1 — none of this is asserted anywhere in the suite. The filtering itself is real and covered; the count-display requirement is not. |
| USRPG-03 | Busca filtra por nome OU e-mail, case-insensitive substring | `busca filtra por nome ou e-mail` | `AdminsPage.test.tsx:81-90` | Asserts substring match on email finds the row and excludes others | Covered for email; no dedicated test exercises name-substring matching or case-insensitivity explicitly (lowercase `q` in a test only), but the implementation (`AdminsPage.tsx:118-127`) is straightforward and shared with the email path. Minor gap, not spec-blocking. |
| USRPG-04 | Sem resultado → estado vazio, texto exato do mock | `filtro sem resultado mostra o estado vazio` | `AdminsPage.test.tsx:92-100` | Asserts exact string "Nenhum usuário encontrado com esses filtros." | Covered, exact match |
| USRPG-05 | `last_access` real (`sessions`), "—" quando nulo | Backend: `TestListAdmins_Owner_200_LastAccessFromMostRecentSession`, `TestListAdmins_Owner_200_LastAccessNilWithoutSession` (`internal/api/admins_test.go:2008-2056`); Frontend: `último acesso ausente mostra travessão` (`AdminsPage.test.tsx:186-192`) | Backend asserts exact timestamp equality and nil; frontend asserts "—" render | Covered, but see sensor findings below — the backend test fixtures only ever seed a **single** session per user with `last_seen_at` explicitly non-null, so the "most recent of several sessions" and "falls back to `created_at`" halves of the spec's own last_access definition are unexercised (see Sensor section). |
| USRPG-06 | Drawer abre com badge status, nome/e-mail, papel (3 radio), último acesso | `clicar numa linha abre o drawer de detalhe...` | `AdminsPage.test.tsx:102-113` | Asserts dialog role, email text, role radio `aria-checked=true`, and a "há N" last-access string | Covered |
| USRPG-07 | Troca de papel → `PATCH /role`; refletido; 409 (`ADM-06`) mantém papel + mostra erro | `trocar papel no drawer chama a API...` + `troca de papel rejeitada (409...)` | `AdminsPage.test.tsx:115-140` | Success case asserts table reflects new label; failure case asserts alert text `/zero active owners/` and unchanged label in table | Covered, both branches |
| USRPG-08 | Pendente → "Reenviar convite" visível; Ativo → oculto | `drawer de convite pendente mostra Reenviar convite; ativo não mostra` | `AdminsPage.test.tsx:142-155` | Asserts button present for pending row's drawer, absent for active row's drawer | Covered |
| USRPG-09 | Remover (ativo) / cancelar (pendente) → chama endpoint existente, fecha drawer em sucesso | `remover usuário ativo...` + `cancelar convite pendente...` | `AdminsPage.test.tsx:157-184` | Both assert row disappears + specific success toast text | Covered |
| USRPG-10 | "Último acesso" formatado (há N min/horas/dias) ou "—" | `clicar numa linha abre...` (regex `/há \d+/`) + `último acesso ausente mostra travessão` | `AdminsPage.test.tsx:112`, `186-192` | Covered for both non-null (regex, not exact string) and null ("—" exact) | Covered. Non-null assertion uses a loose regex rather than pinning exact minute/hour/day wording, but that's reasonable given real elapsed time in a live test. |
| USRPG-11 | Drawer de convite: Nome, Email, Telefone (opcional), papel em 3 caixas | `convidar usuário via drawer exige nome, telefone opcional e papel` | `AdminsPage.test.tsx:194-207` | Fills name+email+role, leaves phone blank, submits successfully — implicitly proves phone is optional | Covered |
| USRPG-12 | Submit válido → `POST /api/admins`, fecha drawer, nova linha pendente aparece | same test, `:203-207` | Asserts new email row appears with "Pendente" tag after submit | Covered |
| USRPG-13 | Rejeição do backend → drawer permanece aberto, erro inline | `convite rejeitado (409, e-mail já ativo)...` | `AdminsPage.test.tsx:209-221` | Asserts alert text `/an active admin already exists/` and that "Enviar convite" button (i.e. the drawer) is still present | Covered |

**Coverage summary**: 13/13 ACs have at least one test whose asserted value matches the spec's expected outcome. One genuine spec-precision gap (USRPG-02's count badges are never asserted) and one exact-value nuance for USRPG-05 (fixtures never exercise "several sessions, pick the most recent" or "fall back to created_at" — see Sensor).

---

## Discrimination sensor

Isolated git worktree at `/tmp/users-page-sensor` (checked out at `bc80494`, `ab49ed2` cherry-picked on top since it touches the same invite drawer). Backend tests run against a disposable Postgres container (`users-page-sensor-pg`, port 5435, stopped/removed after); frontend tests run via `npm run test -- src/features/admins/AdminsPage.test.tsx`. The real working tree was never touched by any mutation (`git status --porcelain` on the real repo confirmed clean before, during, and after); the worktree was removed with `git worktree remove --force` when done.

| # | Mutation | Layer | Result | Notes |
| --- | --- | --- | --- | --- |
| 1 | Drop the `LEFT JOIN LATERAL` on `sessions`; `last_access` always `NULL` | Backend | **Killed** | `TestListAdmins_Owner_200_LastAccessFromMostRecentSession` failed ("LastAccess = nil, want a timestamp") |
| 2 | Swap `MAX(...)` → `MIN(...)` in the lateral subquery | Backend | **Survived** | Both backend tests seed exactly one session row per user, so `MAX` and `MIN` return the same value. The spec's own definition ("o maior `sessions.last_seen_at`... entre as sessões do usuário") is not actually pinned by any test — a real gap for a user with 2+ sessions. |
| 3 | Drop the `COALESCE(last_seen_at, created_at)` fallback, use `last_seen_at` alone | Backend | **Survived** | The one test that exercises a non-null last_access seeds `last_seen_at` explicitly equal to `created_at` (never seeds a session with only `created_at` set and `last_seen_at` NULL), so the fallback path documented in spec.md's Assumptions table is unexercised. |
| 4 | Role-filter chips: `matchesRole = true` (chips stop filtering) | Frontend | **Killed** | `chips de papel filtram a tabela com contagem correta` failed — operator/viewer rows still visible after clicking "Admin" |
| 5 | Swap role label map: `operator: "Leitura", viewer: "Membro"` | Frontend | **Killed** | Both `rótulos de papel...` and `trocar papel no drawer...` failed |
| 6 | "Reenviar convite" shown unconditionally (`{true ? ... : null}` instead of `{selected.status === "pending" ? ...}`) | Frontend | **Killed** | `drawer de convite pendente mostra Reenviar convite; ativo não mostra` failed — button present for the active user's drawer |
| 7 | Remove/cancel handlers become no-ops (drop the `mutateAsync` calls, keep the success toasts) | Frontend | **Killed** | Both `remover usuário ativo...` and `cancelar convite pendente...` failed — row still present after "removal" |

**Kill rate**: 5/7 (71%). The two survivors are both on the backend `last_access` computation and share a root cause: the integration test fixtures never seed a user with (a) more than one session, or (b) a session whose `last_seen_at` is NULL. Both are real, spec-relevant gaps — the spec text explicitly calls out "o maior... entre as sessões" (implying multiple sessions matter) and the NULL→`created_at` fallback as a named case in the Assumptions table, but neither is pinned by a test that would fail if the SQL regressed to `MIN` or dropped the fallback.

---

## Skeptical checks requested

**1. Detail-drawer `open` prop (`selected !== null && removeTarget === null`) and the remove-confirmation interplay.**
Reviewed `AdminsPage.tsx:282-363` (RadixDialog.Root for the detail drawer) and `:417-449` (the separate `Dialog` for remove confirmation). Since `open` is a controlled prop driven by `selectedId`/`removeTarget` state (not by the dialog's own internal open/close request), setting `removeTarget` via "Remover usuário" flips the detail drawer's `open` to `false` without going through `onOpenChange` (Radix only invokes `onOpenChange` for its own internally-triggered close events — Escape, overlay click, `Dialog.Close` — not for externally-driven prop changes), so `selectedId` is not cleared. Clicking "Cancelar" on the remove-confirmation dialog sets `removeTarget` back to `null`, which flips the detail drawer's `open` back to `true` (since `selectedId` was never cleared), so the drawer reappears in a defined, non-stuck state. This reasoning holds up against the code, but **no test in the suite exercises this exact "open detail → click Remover → click Cancelar → drawer reappears" sequence** — it is inferred from the state wiring, not proven by a passing test. Recommend adding one before this logic is touched again, since it depends on Radix's internal "controlled prop change doesn't fire onOpenChange" contract, which is not this codebase's contract to enforce.

**2. Pending-invite rows suppressing the secondary email line.**
`AdminsPage.tsx:261-263` and `:311-316`: when `a.name` is falsy, the primary line renders `a.email` and the secondary line is omitted entirely (rather than rendering email/email). Since `email` is the tenant's uniqueness key for a membership/invite, no two distinct rows can carry the same primary text this way — the suppressed line is a literal duplicate of what's already shown, not a distinguishing field being hidden. This does not create an accessibility indistinguishability issue for two different pending invites (their emails necessarily differ), so the fix is legitimate — it removes a duplicate render, not a differentiator. No change recommended.

---

## Known-context items (not flagged as gaps)

- Seat-limit banner ("Você atingiu o limite de X usuários" + "Fazer upgrade") is intentionally absent — documented Out of Scope in spec.md, AD-025.
- Role label mapping (`owner`→Admin, `operator`→Membro, `viewer`→Leitura) is presentation-only; the raw API/persisted values remain `owner/operator/viewer` by design.

---

## Ranked gap list

1. **(Spec-precision, Backend, Medium)** USRPG-05's own definition of `last_access` ("maior... entre as sessões", "ou created_at se last_seen_at nunca foi tocado") has two branches — multi-session recency and the NULL-fallback — that are implemented correctly (verified by reading `internal/api/admins.go:707-712`) but are not distinguished by any test; both `MAX→MIN` and "drop COALESCE" mutations survive the suite. A regression on either would ship silently. Recommend: add one test seeding 2+ sessions per user (older `last_seen_at` on one, newer on another) asserting the newer wins, and one test seeding a session with `last_seen_at = NULL` asserting `created_at` is used.
2. **(Spec-precision, Frontend, Low)** USRPG-02 requires each role chip to show a count; the suite proves the *filtering* works but never asserts the *displayed count number* is correct. A mutation that renders a wrong/static count next to each chip would pass all current tests. Recommend a test asserting exact `Todos 4 / Admin 1 / Membro 2 / Leitura 1` badge text against the seeded fixture.
3. **(Untested interaction, Frontend, Low)** The detail-drawer/remove-confirmation dialog interplay (open→Remover→Cancelar→drawer reappears) is correct by code inspection but has no regression test pinning it — flagged above under skeptical checks.

None of these are release blockers; all 13 ACs have real, spec-matching coverage, the real gate is green, and 5/7 sensor mutations were killed (the two survivors are additive test-coverage gaps in already-correct code, not implementation defects).
