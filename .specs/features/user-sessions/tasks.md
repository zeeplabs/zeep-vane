# Per-Device User Sessions Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Spec**: `.specs/features/user-sessions/spec.md`
**Context**: `.specs/features/user-sessions/context.md`
**Design**: `.specs/features/user-sessions/design.md`
**Status**: Draft

> Decisões de design-phase já fixadas em `context.md` (híbrido `SessionsRevokedAt`, quebra de JWTs pré-deploy, helper de captura UA/IP, assinatura nova de `RequireAuth`, `Logout` lê `sid` do context, `SwitchTenant` reusa `sid`, TTL único 24h, race trivial, sem cleanup job). Tasks abaixo materializam essas decisões sem reabrir.

---

## Test Coverage Matrix

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| Migration (`0031_sessions.up.sql` / `.down.sql`) | integration (reversão limpa) | up + down em DB descartável; `gen_random_uuid()` e tipo `INET` aceitos; FK CASCADE ativo | `internal/db/migrations/*_test.go` (test carrega `.up.sql` e aplica) | `go test -tags=integration ./internal/db` |
| `SessionRepository` (8 métodos) | integration | 1:1 com ACs SESS-01, SESS-04, SESS-05, SESS-06, SESS-07, SESS-10; cobre throttle 5min, exclusão de revoked e >24h, anti-enumeration em `GetByIDAndUser` | `internal/db/session_repository_test.go` | mesmo |
| `auth.IssueSessionWithTenant` + `VerifySessionClaims` (mudança de claim `sid`) | unit | token emitido carrega `sid` parseável; token sem `sid` ou com `sid` malformado é rejeitado; challenge 2FA (audience `2fa_challenge`) continua rejeitado por `VerifySessionClaims` | `internal/auth/session_test.go` | `go test ./internal/auth` |
| `middleware.RequireAuth` (warm path com sid) | integration | 401 sem sid / sid inválido / sid revogado / sid inexistente; 200 + `TouchLastSeen` throttled quando válido; `SessionsRevokedAt` global continua sendo checado (regressão) | `internal/api/middleware_test.go` | `go test -tags=integration ./internal/api` |
| `AuthHandler` (issueSessionForUser, SwitchTenant, Logout) | integration | Login cria row; Verify-2FA cria row; AcceptInvite cria row; Bootstrap cria row; SwitchTenant NÃO cria row (mesmo sid, novo tid); Logout revoga + limpa cookie | `internal/api/auth_handler_test.go` | mesmo |
| `AdminsHandler` (UpdateRole, Delete — per-session) | integration | `RevokeAllForUser` chamado no lugar de `RevokeSessions` global; mesmo efeito observável (request autenticada com sid antigo ⇒ 401) | `internal/api/admins_test.go` | mesmo |
| `SessionsHandler.List` | integration | 200 com `SessionView[]`; exclui `revoked_at` não-nulo; exclui `created_at > 24h`; marca exatamente 1 row como `current: true` (a do `ctx.sid`); rejeita com 401 sem auth | `internal/api/sessions_handler_test.go` | mesmo |
| `SessionsHandler.Revoke` | integration | matriz 200/404/409; UUID malformado no path ⇒ 404; `GetByIDAndUser` retorna 404 pra row de outro user (anti-enumeration) | mesmo | mesmo |
| Routes (wiring completo de `RequireAuth` nova assinatura) | integration | toda rota protegida continua 401 sem sid válido; `routes.go` compila com assinatura nova | `internal/cli/routes_test.go` | mesmo |
| Frontend — types/MSW/hook | unit (via MSW) | 200/401/404 por hook; `current: true` parseado corretamente; revoke mutation invalida query | `web/src/features/sessions/hooks.test.ts` + MSW handlers | `cd web && npm run test` |
| Frontend — SessionsSection | component (Testing Library) | empty state (só current); lista com current + outras; revoke flow com optimistic update + toast | `web/src/features/sessions/SessionsSection.test.tsx` | mesmo |
| i18n | smoke | 2 strings (`Sessão atual`, `Encerrar`) em pt-BR + en | `web/src/lib/i18n.test.ts` ou equivalente | mesmo |

## Gate Check Commands

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick | Após task só com teste unitário (T3 — auth/session.go) | `go test ./internal/auth && go vet ./...` |
| Full | Após task com integração (T1, T2, T4–T10) | `go test -tags=integration ./... && go vet ./...` |
| Build | Fim de fase backend (depois de T10) ou task só de config/migration (T1) | `go build ./... && gofmt -l . && go test -tags=integration ./... && go vet ./...` |
| Frontend quick | Após task de frontend (T11–T14) | `cd web && npm run test && npx tsc -b --noEmit` |
| Frontend build | Fim da fase frontend (depois de T14) | `cd web && npm run build` |
| Release gate (Verificador) | Fim do Execute (depois de T14) | tudo acima, com `git status --porcelain` confirmando zero diff residual depois do scratch do sensor |

> **Regra de DB descartável** (AGENTS.md §3): nunca rodar `-tags=integration` contra `vane-dev-pg` ou banco com dados reais. Sempre container Postgres descartável em `localhost:5433` com `max_connections=300`.

---

## Execution Plan

Fases ordenadas e estritamente sequenciais — cada fase completa antes da próxima. Sem paralelismo intra-fase (mudanças tocam tipos compartilhados, blast radius precisa ser cumulativo).

```
Phase 1 → Phase 2 → Phase 3 → Phase 4 → Phase 5 → Phase 6

Phase 1: T1 → T2                      (migration + repo)
Phase 2: T2 → T3 → T4                 (auth + middleware)
Phase 3: T4 → T5 → T6                 (issue + SwitchTenant)
Phase 4: T6 → T7 → T8 → T9 → T10      (revoke + new endpoints + wiring)
Phase 5: T10 → T11 → T12 → T13 → T14  (frontend)
Phase 6: T14 → T15                    (Verifier independente)
```

---

## Task Breakdown

### T1: Migration `0031_sessions.up.sql` + reverso

**What**: Cria tabela `sessions` (id UUID PK, user_id UUID FK → users ON DELETE CASCADE, user_agent TEXT, ip INET, created_at TIMESTAMPTZ DEFAULT now(), last_seen_at TIMESTAMPTZ NULL, revoked_at TIMESTAMPTZ NULL) + 2 índices: `(user_id, created_at DESC)` e `(user_id) WHERE revoked_at IS NULL`. `.down.sql` derruba índices e tabela. Sem RLS (escopo vem do handler, mesma postura de `tenant_memberships`).
**Where**: `internal/db/migrations/0031_sessions.{up,down}.sql`
**Depends on**: None
**Reuses**: convenção de migrations versionadas (`0009`, `0024`, `0030`); padrão "sem RLS, escopo via app.user_id" herdado de `tenant_memberships`
**Requirement**: SESS-01 (storage), SESS-05 (lista exclui >24h), SESS-04 (last_seen_at)

**Tools**: MCP NONE · Skill NONE

**Done when**:
- [ ] `up.sql` aplica sem erro em DB vazio e em DB com `users` já populado
- [ ] `down.sql` reverte sem erro, deixando o DB no estado anterior
- [ ] `gen_random_uuid()` está habilitado (PG 13+) — sem dependência de extensão
- [ ] FK CASCADE ativo: tentar `DELETE FROM users WHERE id = ...` com `sessions` existente para esse user remove as rows
- [ ] Gate check passa: `go test -tags=integration ./internal/db && go vet ./...`

**Tests**: integration
**Gate**: full

---

### T2: `SessionRepository` com 8 métodos + integration tests

**What**: Struct `SessionRepository{ pool *Pool }` com `Create`, `GetByID`, `ListForUser`, `GetByIDAndUser`, `Revoke`, `TouchLastSeen`, `RevokeAllForUser` (+ constructor `NewSessionRepository`). Cada método tem teste integration cobrindo happy path + edge case documentado no design.md §3.2.
**Where**: `internal/db/session_repository.go` (novo) + `internal/db/session_repository_test.go` (novo)
**Depends on**: T1
**Reuses**: padrão de outros repos em `internal/db/*_repository.go` (pgx, pool, ErrNotFound, sem ORM)
**Requirement**: SESS-01, SESS-03, SESS-04, SESS-05, SESS-06, SESS-07, SESS-10

**Tools**: MCP NONE · Skill NONE

**Done when**:
- [ ] `Create` retorna UUID; round-trip com `GetByID` bate byte-a-byte (campos `user_agent`/`ip` viram `sql.NullString` corretamente, inclusive NULL)
- [ ] `ListForUser` exclui rows com `revoked_at IS NOT NULL` E rows com `created_at < now - 24h` (testado com 3 rows: uma revoked, uma >24h, uma válida — só a última aparece)
- [ ] `GetByIDAndUser` retorna `ErrNotFound` (não outro erro) quando row existe mas pertence a outro user
- [ ] `Revoke` é idempotente (chamar 2x não dá erro, segundo call afeta 0 rows)
- [ ] `TouchLastSeen` faz no-op quando `last_seen_at < now - 5min` é falso (testado com 2 chamadas em sequência rápida: 2ª não muda timestamp)
- [ ] `RevokeAllForUser` afeta N rows ativas e não afeta rows já revogadas
- [ ] Gate check passa: `go test -tags=integration ./internal/db && go vet ./...`

**Tests**: integration
**Gate**: full

---

### T3: `auth/session.go` — claim `sid` + rejeição sem sid

**What**: `sessionClaims` ganha campo `SessionID string \`json:"sid"\``. `IssueSessionWithTenant` recebe `sessionID uuid.UUID` adicional e popula o claim. `VerifySessionClaims` rejeita tokens sem `sid` parseável como UUID (cobre JWTs pré-deploy — decision #4 do context).
**Where**: `internal/auth/session.go` (modify) + `internal/auth/session_test.go` (modify)
**Depends on**: T2 (apenas pra teste; o código não importa o repo)
**Reuses**: `IssueTwoFactorChallenge` / `VerifyTwoFactorChallenge` (`internal/auth/two_factor.go`) — `VerifySessionClaims` continua rejeitando challenge tokens via audience check
**Requirement**: SESS-01, SESS-03 (parte JWT)

**Tools**: MCP NONE · Skill NONE

**Done when**:
- [ ] `IssueSessionWithTenant(userID, tenantID, sessionID, secret)` compila com nova assinatura
- [ ] JWT emitido contém `"sid": "<uuid>"` válido (verificado por parsing do token em teste)
- [ ] `VerifySessionClaims` rejeita token sem campo `sid` com erro tipado
- [ ] `VerifySessionClaims` rejeita token com `sid` malformado (string que não parseia como UUID) com erro tipado
- [ ] `VerifySessionClaims` **continua rejeitando** challenge 2FA (`audience = "2fa_challenge"`) — regressão coberta por teste explícito
- [ ] Gate check passa: `go test ./internal/auth && go vet ./...`

**Tests**: unit
**Gate**: quick

---

### T4: `middleware.RequireAuth` — nova assinatura + lookup de sid + touch

**What**: `RequireAuth` ganha dependência `sessions sessionGetter` (interface mínima) + `*zap.Logger`. Após validar JWT e checar `users.SessionsRevokedAt`, faz `sessions.GetByID(sid)`, rejeita 401 se inexistente ou revogado, chama `TouchLastSeen` throttled (fire-and-forget), e popula `ctx.Set("sid", sid)`.
**Where**: `internal/api/middleware.go` (modify) + `internal/api/middleware_test.go` (modify)
**Depends on**: T3
**Reuses**: `UserGetter` existente; padrão de fail-open em `TouchLastSeen` (mesmo que `ratelimit` Postgre fail-open, AD-010/HA-10); T2 (SessionRepository) implícito via cadeia
**Requirement**: SESS-03, SESS-04

**Tools**: MCP NONE · Skill `security-best-practices`, `best-practices`

**Done when**:
- [ ] Assinatura nova `RequireAuth(secret, users, sessions, logger)` compila
- [ ] Token com `sid` válido + row ativa ⇒ 200, `sid` no context
- [ ] Token sem `sid` ⇒ 401 (cobre JWTs pré-deploy)
- [ ] Token com `sid` malformado ⇒ 401
- [ ] Token com `sid` válido mas row revogada (`revoked_at` não-nulo) ⇒ 401
- [ ] Token com `sid` válido mas row inexistente (UUID nunca emitido) ⇒ 401
- [ ] `TouchLastSeen` chamado uma vez por request autenticada; falhas logged mas não bloqueiam (fail-open)
- [ ] Throttle verificado: 2 requests em <5min não mudam `last_seen_at`
- [ ] `users.SessionsRevokedAt` global ainda sendo checado (regressão) — token com `iat < SessionsRevokedAt` mas sid válido ⇒ 401
- [ ] Gate check passa: `go test -tags=integration ./internal/api && go vet ./...`

**Tests**: integration
**Gate**: full

---

### T5: `AuthHandler` — `captureSessionContext` + `issueSessionForUser` cria row

**What**: Helper privado `captureSessionContext(r)` lê `r.UserAgent()` + IP via `r.RemoteAddr` (mesma lógica de `internal/ratelimit/ip_limiter.go:126`). `issueSessionForUser` ganha row creation: cria `sessions` row antes de emitir JWT. `NewAuthHandler` recebe `*db.SessionRepository` na assinatura.
**Where**: `internal/api/auth_handler.go` (modify)
**Depends on**: T4
**Reuses**: `issueSessionForUser` já é helper compartilhado por `Login` (L171) e `VerifyTwoFactor` (L324); nenhum call site muda de assinatura; T2 (SessionRepository) + T3 (sid claim) implícitos via cadeia
**Requirement**: SESS-01 (Login, Verify-2FA, AcceptInvite, Bootstrap — os 4 cobertos via helper)

**Tools**: MCP NONE · Skill NONE

**Done when**:
- [ ] `captureSessionContext` retorna UA + IP parseado de `RemoteAddr`; UA ausente ⇒ string vazia (vira NULL no banco)
- [ ] `issueSessionForUser` cria 1 row em `sessions` e emite JWT com `sid = row.id`
- [ ] Login correto (sem 2FA) cria row; JWT carrega `sid` parseável
- [ ] Verify-2FA correto cria row; JWT carrega `sid` parseável
- [ ] AcceptInvite (`admins.go:382` via `issueSessionForUser`) cria row
- [ ] Bootstrap (`bootstrap_handler.go:190`) cria row
- [ ] `NewAuthHandler` recebe `*db.SessionRepository`; wiring em `routes.go` ainda não muda aqui (T10)
- [ ] Gate check passa: `go test -tags=integration ./internal/api && go vet ./...`

**Tests**: integration
**Gate**: full

**Commit**: `feat(user-sessions): add sid claim and row-backed session issuance`

---

### T6: `SwitchTenant` reusa sid (mesmo row, novo tid)

**What**: `AuthHandler.SwitchTenant` (`auth_handler.go:801`) lê `sid` do context (populado por `RequireAuth` em T4) e chama `IssueSessionWithTenant(user.ID, newTenantID, sid, secret)` — **não** cria nova row. JWT resultante tem `tid` novo, `iat` novo, `sid` igual.
**Where**: `internal/api/auth_handler.go` (modify — `SwitchTenant`)
**Depends on**: T5
**Reuses**: `IssueSessionWithTenant` atualizado em T3; `sidFromContext` (helper de uma linha)
**Requirement**: SESS-02

**Tools**: MCP NONE · Skill NONE

**Done when**:
- [ ] `SwitchTenant` chamado com sid A ⇒ emite JWT com `sid == A` (verificado por parsing)
- [ ] `SELECT COUNT(*) FROM sessions WHERE user_id = ?` é 1 antes e depois do `SwitchTenant` (não criou row nova)
- [ ] JWT antigo continua rejeitado se `users.SessionsRevokedAt` foi atualizado entre os dois (regressão)
- [ ] Gate check passa: `go test -tags=integration ./internal/api && go vet ./...`

**Tests**: integration
**Gate**: full

**Commit**: `feat(user-sessions): reuse sid on tenant switch`

---

### T7: `Logout` revoga row + limpa cookie

**What**: `AuthHandler.Logout` (`auth_handler.go:777`) lê `sid` do context, chama `sessions.Revoke(ctx, sid)`, depois expira cookie (igual hoje). Falha de DB é logged mas não bloqueia response (fail-open, mesma postura do rate limiter).
**Where**: `internal/api/auth_handler.go` (modify — `Logout`)
**Depends on**: T6
**Reuses**: `clearSessionCookie` (helper existente)
**Requirement**: SESS-11

**Tools**: MCP NONE · Skill NONE

**Done when**:
- [ ] `Logout` chamado com sid válido ⇒ row em `sessions` ganha `revoked_at = now()`
- [ ] Cookie `vane_session` é expirado (`MaxAge = -1`) — preservado
- [ ] Após logout, request autenticada com o **token antigo** (não o cookie, o JWT cru copiado pré-logout) ⇒ 401 em `RequireAuth`
- [ ] Falha de `sessions.Revoke` (ex: pool exhausted) logged como warning; response ainda 200 + cookie expirado
- [ ] Gate check passa: `go test -tags=integration ./internal/api && go vet ./...`

**Tests**: integration
**Gate**: full

**Commit**: `feat(user-sessions): server-side revoke on logout`

---

### T8: `AdminsHandler.UpdateRole`/`Delete` migram pra per-session

**What**: Substitui `users.RevokeSessions(targetID)` por `sessions.RevokeAllForUser(targetID)` em `admins.go:571` (UpdateRole) e `admins.go:619` (Delete). `UserRepository.RevokeSessions` continua existindo (ChangePassword/reset mantêm — decision #1 do context).
**Where**: `internal/api/admins.go` (modify)
**Depends on**: T7
**Reuses**: nenhuma — `RevokeAllForUser` substitui chamada, assinatura similar
**Requirement**: per-session decision #1 do context (não-AC explícito, mas implícito no comportamento de UpdateRole/Delete manter "derruba tudo")

**Tools**: MCP NONE · Skill `security-best-practices`, `best-practices`

**Done when**:
- [ ] `UpdateRole` chama `sessions.RevokeAllForUser` no lugar de `users.RevokeSessions`
- [ ] `Delete` chama `sessions.RevokeAllForUser` no lugar de `users.RevokeSessions`
- [ ] `users.RevokeSessions` continua sendo chamado por `AuthHandler.ChangePassword` e `PasswordResetHandler.Confirm` (não tocado aqui)
- [ ] Após `UpdateRole`, request autenticada do admin rebaixado com sid pré-update ⇒ 401
- [ ] Após `Delete`, request autenticada do admin removido com sid pré-delete ⇒ 401
- [ ] Audit log (`admin_audit_log`) continua gravado (regressão)
- [ ] Gate check passa: `go test -tags=integration ./internal/api && go vet ./...`

**Tests**: integration
**Gate**: full

**Commit**: `feat(user-sessions): switch admin revoke from global to per-session`

---

### T9: `SessionsHandler` + 2 endpoints novos

**What**: Novo handler em `internal/api/sessions_handler.go` com `List` (GET `/api/auth/sessions`, retorna `SessionView[]` com `current: true` na row do próprio sid) e `Revoke` (DELETE `/api/auth/sessions/{id}`, matriz 200/404/409). UUID malformado no path ⇒ 404. Anti-enumeration: `GetByIDAndUser` retorna 404 (não 403) quando row não pertence ao user.
**Where**: `internal/api/sessions_handler.go` (novo) + `internal/api/sessions_handler_test.go` (novo)
**Depends on**: T8
**Reuses**: padrão de outros handlers em `internal/api/*_handler.go`; `SessionView` espelha o tipo Go 1:1 com JSON
**Requirement**: SESS-05, SESS-06, SESS-07, SESS-08, SESS-09, SESS-10

**Tools**: MCP NONE · Skill NONE

**Done when**:
- [ ] `GET /api/auth/sessions` 200 retorna `SessionView[]` com shape `{id, user_agent, ip, created_at, last_seen_at, current}`
- [ ] Lista exclui rows revogadas (testado com row revoked criada direto no DB)
- [ ] Lista exclui rows com `created_at > 24h` (testado via manipulação de clock ou injeção direta)
- [ ] Lista marca exatamente 1 row como `current: true` (a do `ctx.sid`); teste com 2 logins do mesmo user confirma
- [ ] `DELETE /api/auth/sessions/{id}` para row de outro user ⇒ 404
- [ ] `DELETE /api/auth/sessions/{id}` para o próprio `ctx.sid` ⇒ 409
- [ ] `DELETE /api/auth/sessions/{id}` para row válida do user ⇒ 200 + `revoked_at` setado; request subsequente com esse sid ⇒ 401
- [ ] UUID malformado no path (`/api/auth/sessions/not-a-uuid`) ⇒ 404
- [ ] Sem auth ⇒ 401 (regressão de wiring)
- [ ] Gate check passa: `go test -tags=integration ./internal/api && go vet ./...`

**Tests**: integration
**Gate**: full

**Commit**: `feat(user-sessions): add list and revoke endpoints`

---

### T10: `routes.go` wiring completo

**What**: `internal/cli/routes.go` instancia `db.NewSessionRepository(pool)`, atualiza assinatura de `RequireAuth` em todas as chamadas, instancia `NewSessionsHandler`, registra `GET /api/auth/sessions` e `DELETE /api/auth/sessions/{id}` sob `protected.With(anyRole)`.
**Where**: `internal/cli/routes.go` (modify) + `internal/cli/routes_test.go` (modify)
**Depends on**: T9
**Reuses**: padrão existente de registro de rotas; `protected` group já com `RequireAuth`
**Requirement**: SESS-05, SESS-07 (wiring)

**Tools**: MCP NONE · Skill NONE

**Done when**:
- [ ] `routes.go` compila com nova assinatura de `RequireAuth` (todos os call sites atualizados — só `routes.go:95` na verdade)
- [ ] `GET /api/auth/sessions` retorna 200 com auth válida e 401 sem auth (testado em `routes_test.go` com `buildAdminRouter`)
- [ ] `DELETE /api/auth/sessions/{id}` retorna 200 com auth + id válido e 401 sem auth
- [ ] Gate check passa: `go test -tags=integration ./... && go vet ./... && go build ./... && gofmt -l .`

**Tests**: integration
**Gate**: full

**Commit**: `feat(user-sessions): wire new endpoints and middleware signature`

---

### T11: Frontend types + MSW handlers + mockData

**What**: Adiciona `SessionView` type em `web/src/types/api.ts` mirror do Go shape. Atualiza MSW handler em `web/src/test/msw/handlers.ts` para `GET /api/auth/sessions` retornar `SessionView[]` e `DELETE /api/auth/sessions/{id}` retornar 200. Adiciona seed em `web/src/lib/mockData.ts` com 2 sessões (1 current, 1 outra) pra cobrir UI vazia e UI populada.
**Where**: `web/src/types/api.ts`, `web/src/test/msw/handlers.ts`, `web/src/lib/mockData.ts`
**Depends on**: T10 (backend contrato definido)
**Reuses**: padrão de outros types em `types/api.ts`; `paginatedPage()` helper do `admin-frontend` se aplicável (mas aqui não há paginação — array simples)
**Requirement**: contratos de frontend espelhando SESS-05/07

**Tools**: MCP NONE · Skill NONE

**Done when**:
- [ ] `SessionView` em `types/api.ts` com todos os campos do shape Go
- [ ] MSW handler `GET /api/auth/sessions` retorna array; smoke test via fetch em teste confirma shape
- [ ] MSW handler `DELETE /api/auth/sessions/{id}` retorna 200
- [ ] `mockData.ts` tem `seedSessions` com 2 entries (1 com `current: true`)
- [ ] `cd web && npm run test && npx tsc -b --noEmit` passa limpo

**Tests**: unit (via MSW smoke)
**Gate**: frontend quick

**Commit**: `feat(user-sessions): add frontend types and MSW handlers`

---

### T12: Hook `useSessions` + revoke mutation

**What**: `web/src/features/sessions/hooks.ts` com `useSessions()` (TanStack Query, queryKey `['sessions']`, refetch a cada 30s) e `useRevokeSession()` (mutation que chama DELETE e invalida `['sessions']` no success).
**Where**: `web/src/features/sessions/hooks.ts` (novo) + `web/src/features/sessions/hooks.test.ts` (novo)
**Depends on**: T11
**Reuses**: padrão de outros hooks em `web/src/features/**/hooks.ts` (TanStack Query); padrão de mutation + invalidate já estabelecido
**Requirement**: SESS-05 (lista), SESS-07 (revoke)

**Tools**: MCP NONE · Skill NONE

**Done when**:
- [ ] `useSessions()` retorna `{data: SessionView[] | undefined, isLoading, error}`
- [ ] Hook busca via `apiFetch('/api/auth/sessions')` (não `mockData` em memória)
- [ ] `useRevokeSession()` mutation: 200 → toast sucesso + invalida query; 401 → toast erro; 409 → toast "não pode revogar sessão atual"
- [ ] Testes cobrem 200/401/404/409 por hook (mesma profundidade de `admin-frontend` I13)
- [ ] `cd web && npm run test` passa

**Tests**: unit (via MSW)
**Gate**: frontend quick

**Commit**: `feat(user-sessions): add useSessions hook with revoke mutation`

---

### T13: Página `SessionsSection` + i18n

**What**: `web/src/features/sessions/SessionsSection.tsx` reescreve a seção "Sessões ativas" dentro de "Meu Perfil". Lista `SessionView[]` com 1 row por sessão; row com `current: true` marcada visualmente (badge "Sessão atual", sem botão); outras rows com botão "Encerrar" → confirmação → `revoke.mutate` → toast. Empty state tratado (1 sessão só, a current). i18n: 2 strings (`Sessão atual`, `Encerrar`) em pt-BR + en.
**Where**: `web/src/features/sessions/SessionsSection.tsx` (novo) + `web/src/features/sessions/SessionsSection.test.tsx` (novo) + `web/src/lib/i18n.ts` ou equivalente
**Depends on**: T12
**Reuses**: componentes UI do design system (`Card`, `Button`, `Dialog`/`ConfirmDialog`) já estabelecidos em `new-layout-migration`; `sonner` para toast
**Requirement**: SESS-05, SESS-06, SESS-07, SESS-08

**Tools**: MCP NONE · Skill NONE

**Done when**:
- [ ] Lista renderiza todas as rows retornadas
- [ ] Row `current: true` tem badge distinto e **sem** botão "Encerrar"
- [ ] Row `current: false` tem botão "Encerrar" que abre confirmação
- [ ] Após confirmar + mutation success → toast "Sessão encerrada" + lista refetched (row some)
- [ ] Mutation 409 → toast "Não é possível encerrar a sessão atual"
- [ ] Strings em pt-BR + en; testes checam ambas as línguas
- [ ] `cd web && npm run test && npm run build` passa

**Tests**: component (Testing Library)
**Gate**: frontend build

**Commit**: `feat(user-sessions): add SessionsSection page with i18n`

---

### T14: Frontend integration tests (hook + página em conjunto)

**What**: Teste que exercita o fluxo "user vê lista, encerra sessão de outro device, lista atualiza" end-to-end via MSW — sem servidor Go real, mas com handlers MSW refletindo o contrato do backend.
**Where**: `web/src/features/sessions/SessionsSection.test.tsx` (modify, adiciona testes integration)
**Depends on**: T13
**Reuses**: padrão de MSW handlers já estabelecido
**Requirement**: cobertura final dos ACs SESS-05/06/07 do ponto de vista do usuário

**Tools**: MCP NONE · Skill NONE

**Done when**:
- [ ] Teste renderiza componente com seed de 2 sessões, confirma que ambas aparecem, current marcada
- [ ] Teste clica "Encerrar" na sessão não-current, confirma toast e que lista agora tem só 1 row
- [ ] Teste verifica que `current: true` não tem botão Encerrar (regressão)
- [ ] `cd web && npm run test && npm run build` passa

**Tests**: component (integration via MSW)
**Gate**: frontend build

**Commit**: `test(user-sessions): add end-to-end section tests`

---

### T15: Verifier independente (discrimination sensor + spec-anchored check)

**What**: Sub-agente independente (author ≠ verifier) roda:
1. Spec-anchored outcome check — confirma que cada teste acima assere exatamente o outcome definido em `spec.md` (não implementação espelhada)
2. Discrimination sensor — injeta mutações no scratch worktree (nunca no real), confirma que cada uma é morta por algum teste
3. Escreve `.specs/features/user-sessions/validation.md` com verdict PASS/FAIL + lista de gaps
4. Retorna veredito compacto pro orquestrador

**Where**: scratch worktree (criado e destruído pelo verifier)
**Depends on**: T14
**Reuses**: skill `tlc-spec-driven` (Verificador); T1–T13 implícitos via cadeia
**Requirement**: closure gate antes de feature ser considerada Done

**Tools**: Skill `tlc-spec-driven`

**Done when**:
- [ ] `validation.md` escrito com verdict PASS, diff range, e `file:line` evidence por AC
- [ ] Sensor cobriu pelo menos: remoção do check `revoked_at IS NOT NULL` no `ListForUser`, remoção do check `revoked_at` no `GetByID`, `VerifySessionClaims` parando de exigir `sid`, `Revoke` deixando de ser idempotente, `SwitchTenant` criando nova row em vez de reusar, `Logout` parando de chamar `Revoke`, `UpdateRole` voltando a chamar `RevokeSessions` global, `current: true` mal-marcado na listagem
- [ ] `git status --porcelain` confirma zero diff residual no real tree após sensor
- [ ] Se verdict FAIL: gaps viram fix tasks (T16, T17...) até PASS ou escalação (max 3 iterações)

**Tests**: N/A (verifica os outros)
**Gate**: release (tudo verde antes de declarar Done)

**Commit**: `docs(user-sessions): add Verifier validation report (PASS)`

---

## Phase Execution Map

```
Phase 1: T1 → T2
Phase 2: T2 → T3 → T4
Phase 3: T4 → T5 → T6
Phase 4: T6 → T7 → T8 → T9 → T10
Phase 5: T10 → T11 → T12 → T13 → T14
Phase 6: T14 → T15
```

Execução é estritamente sequencial — sem paralelismo intra-fase. T1–T10 são backend, T11–T14 são frontend, T15 fecha com o Verifier.

> **Nota sobre sub-agentes**: 15 tasks > 1 batch de ~7-8 tasks. Phase 1–2 (T1–T4, 4 tasks) cabem em 1 batch; Phase 3–4 (T5–T10, 6 tasks) cabem em 1 batch; Phase 5 (T11–T14, 4 tasks) cabem em 1 batch. **3 batches totais**, sequenciais. Oferecer ao usuário antes de Execute.

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1 Migration `0031_sessions` | 2 arquivos (up/down) | ✅ Granular |
| T2 SessionRepository | 2 arquivos (repo + test) | ✅ Granular |
| T3 auth/session.go sid claim | 1 arquivo modify | ✅ Granular |
| T4 middleware.RequireAuth nova assinatura | 1 arquivo modify + test | ✅ Granular |
| T5 issueSessionForUser + captureSessionContext | 1 arquivo modify (helper em `auth_handler.go`) | ✅ Granular |
| T6 SwitchTenant reusa sid | 1 arquivo modify | ✅ Granular |
| T7 Logout revoga row | 1 arquivo modify | ✅ Granular |
| T8 UpdateRole/Delete per-session | 1 arquivo modify | ✅ Granular |
| T9 SessionsHandler + 2 endpoints | 1 arquivo novo + test | ✅ Granular |
| T10 routes.go wiring | 1 arquivo modify | ✅ Granular |
| T11 frontend types/MSW/mockData | 3 arquivos modify | ⚠️ borderline (3 arquivos, mas todos no mesmo padrão de "espelhar contrato" — coesos, não dá pra splitar sem repetir MSW handler) |
| T12 hook useSessions | 2 arquivos (hook + test) | ✅ Granular |
| T13 página SessionsSection + i18n | 2 arquivos modify (page + i18n) + test | ✅ Granular |
| T14 frontend integration tests | 1 arquivo modify | ✅ Granular |
| T15 Verifier | scratch externo | ✅ Granular (não toca repo) |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | None | ✅ |
| T2 | T1 | T1 → T2 | ✅ |
| T3 | T2 | T2 → T3 | ✅ |
| T4 | T2, T3 | T3 → T4 | ✅ |
| T5 | T2, T3, T4 | T4 → T5 | ✅ |
| T6 | T5 | T5 → T6 | ✅ |
| T7 | T6 | T6 → T7 | ✅ |
| T8 | T7 | T7 → T8 | ✅ |
| T9 | T8 | T8 → T9 | ✅ |
| T10 | T9 | T9 → T10 | ✅ |
| T11 | T10 | T10 → T11 | ✅ |
| T12 | T11 | T11 → T12 | ✅ |
| T13 | T12 | T12 → T13 | ✅ |
| T14 | T13 | T13 → T14 | ✅ |
| T15 | T14 | T14 → T15 | ✅ |

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1 migration | Migrations | integration | integration | ✅ |
| T2 SessionRepository | Repository | integration | integration | ✅ |
| T3 auth/session.go | Domain (JWT) | unit | unit | ✅ |
| T4 middleware | API middleware | integration | integration | ✅ |
| T5 AuthHandler helpers | API handler | integration | integration | ✅ |
| T6 SwitchTenant | API handler | integration | integration | ✅ |
| T7 Logout | API handler | integration | integration | ✅ |
| T8 admins handlers | API handler | integration | integration | ✅ |
| T9 SessionsHandler | API handler | integration | integration | ✅ |
| T10 routes.go | API wiring | integration | integration | ✅ |
| T11 types/MSW | Config (tipos) | unit | unit (smoke) | ✅ |
| T12 hook | React hook | unit | unit (MSW) | ✅ |
| T13 page | React component | component | component | ✅ |
| T14 integration tests | React component | component | component | ✅ |
| T15 Verifier | — | — | — | ✅ |

---

## Tips

(Referência — ver `tasks.md` reference da skill para regras completas.)

- **Não pular T3 só porque é "só JWT"** — é o ponto onde a defesa contra JWTs pré-deploy fica. Sem T3, deploy não quebra sessões antigas (decisão #4 do context).
- **Não agrupar T5+T6 num único task** — `SwitchTenant` tem AC separado (SESS-02) com teste de contagem de rows. Mesclado, fica ambíguo o que falhou.
- **T8 é traiçoeiro** — `RevokeAllForUser` tem mesmo blast radius observável que `RevokeSessions` (derruba tudo do user), mas o teste tem que provar que **não chama** `users.RevokeSessions` mais (sensor de mutação).
- **T15 não tem commit no repo real** — scratch worktree, `git checkout -- .` no fim. Confirmar `git status --porcelain == ""` no real antes de fechar a feature.
