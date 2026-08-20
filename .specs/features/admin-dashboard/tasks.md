# Admin Dashboard Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/admin-dashboard/design.md`
**Status**: Draft

---

**Scope note:** este breakdown cobre backend (Go, testável via `go test`/`httptest`) das 3 stories da spec: gestão de admin (papel, convite, revogação, audit log), autorização por papel sobre os endpoints do `mvp-core`, e leitura de status do poller. UI (React) fica pra passada de Tasks separada, mesmo padrão adotado em `mvp-core` pra tratar backend e frontend como lotes distintos.

---

## Test Coverage Matrix

> Mesmo padrão já confirmado em `mvp-core/tasks.md` — mesmo repo, mesma stack, sem necessidade de reconfirmar.

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| Domain/business logic (`internal/auth` middleware, lockout check) | unit ou integration quando depende de DB | Todos os branches; 1:1 com ACs da spec; todo edge case listado tem teste | `internal/**/*_test.go` | `go test ./...` |
| API handlers (`internal/api`) | integration (via `httptest`) | Toda rota: happy path + edge + erro | `internal/api/**/*_test.go` | `go test ./...` |
| Repository/data-access (`internal/db`) | integration | Caminhos de query principais + tratamento de erro | `internal/db/**/*_test.go` | `go test ./... -tags=integration` (requer `TEST_DATABASE_URL`) |
| Migrations/config | none | build gate apenas | - | build gate apenas |

## Gate Check Commands

> Mesmo de `mvp-core/tasks.md`.

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick | Após tasks só com teste unitário | `go test ./... && go vet ./...` |
| Full | Após tasks com integração (API/DB) | `go test ./... -tags=integration && go vet ./...` |
| Build | Fim de fase ou task só de config/migration | `go build ./... && gofmt -l . && go test ./... -tags=integration && go vet ./...` |

---

## Execution Plan

Fases são ordenadas e rodam sequencialmente - cada fase completa antes da próxima começar, e tasks dentro de uma fase rodam em ordem.

### Phase 1: Foundation (dados de papel, convite, auditoria)

```
T1 → T2 → T3 → T4
```

### Phase 2: Auth Middleware (papel + revogação de sessão)

```
T4 → T5 → T6
```

### Phase 3: Admin Management Endpoints

```
T6 → T7 → T8 → T9 → T10 → T11
```

### Phase 4: Authorization Wiring & Poller Status

```
T11 → T12 → T13
```

---

## Task Breakdown

### T1: Migration `role` + `sessions_revoked_at` em `admins`

**What**: Adiciona coluna `role` (`text`, `CHECK IN ('owner','operator','viewer')`, default `'owner'`) e `sessions_revoked_at` (`timestamptz`, nullable) na tabela `admins`; backfill explícito `role = 'owner'` pra qualquer linha já existente.
**Where**: `internal/db/migrations/NNNN_admin_role_and_revocation.sql`
**Depends on**: None
**Reuses**: convenção de migration versionada já usada em T8/T26/T36 do mvp-core
**Requirement**: ADM-08 (base de dado pra role/revogação usados nos demais requisitos)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Migration roda limpa em banco vazio e em banco com admin pré-existente (backfill correto)
- [x] Constraint `CHECK` rejeita valor de role fora dos 3 permitidos
- [x] Gate check passa: `go build ./... && gofmt -l . && go test ./... -tags=integration && go vet ./...`

**Tests**: none
**Gate**: build

---

### T2: Migration `admin_invites` + repositório

**What**: Tabela `admin_invites (id, email, role, token_hash, invited_by_id, expires_at, used_at, created_at)` + repositório com `Create`, `GetByTokenHash`, `MarkUsed`, `InvalidatePendingForEmail`.
**Where**: `internal/db/admin_invites.go`
**Depends on**: T1
**Reuses**: padrão de `password_reset_tokens` (T13 do mvp-core) - mesmo shape de token com hash + expiração
**Requirement**: ADM-01, ADM-02

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `Create` grava convite com token hasheado (nunca cru)
- [x] `InvalidatePendingForEmail` marca convites pendentes anteriores como usados antes de criar o novo
- [x] Testes de integração cobrem create, lookup por hash, e dedup de convite pendente
- [x] Gate check passa: `go test ./... -tags=integration && go vet ./...`

**Tests**: integration
**Gate**: full

---

### T3: Migration `admin_audit_log` + repositório

**What**: Tabela `admin_audit_log (id, actor_id, target_id, action, created_at)` + repositório com `Record(ctx, actorID, targetID, action string) error`.
**Where**: `internal/audit/log.go`
**Depends on**: T2
**Reuses**: nenhum - componente novo, mas trivial (1 insert)
**Requirement**: ADM-08

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `Record` insere linha com timestamp correto
- [x] Sem cascade delete - remover um `Admin` não remove linhas de `admin_audit_log` que o citam
- [x] Teste de integração cobre insert e sobrevivência do registro após remoção do admin referenciado
- [x] Gate check passa: `go test ./... -tags=integration && go vet ./...`

**Tests**: integration
**Gate**: full

---

### T4: Repositório de Admin - papel, revogação e contagem de owners

**What**: Estende o repositório de `Admin` (já existente em T9 do mvp-core) com `UpdateRole(ctx, id, role string) error`, `RevokeSessions(ctx, id string) error` (seta `sessions_revoked_at = now()`), `CountActiveOwners(ctx, tx) (int, error)` usando `SELECT ... FOR UPDATE` pra suportar a checagem atômica de lockout, e `Delete(ctx, id string) error`.
**Where**: `internal/db/admins.go` (modifica)
**Depends on**: T3
**Reuses**: repositório de Admin já existente (T9 do mvp-core)
**Requirement**: ADM-05, ADM-06, ADM-07

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `UpdateRole` e `RevokeSessions` persistem corretamente
- [x] `CountActiveOwners` usa `FOR UPDATE` e é chamado dentro de transação pelos handlers que o consomem (T9/T10)
- [x] Testes de integração cobrem update de role, revogação, contagem de owners, e delete
- [x] Gate check passa: `go test ./... -tags=integration && go vet ./...`

**Tests**: integration
**Gate**: full

---

### T5: `RequireAuth` carrega `Admin` do banco e checa revogação

**What**: Estende o middleware `RequireAuth` (T12 do mvp-core) pra, após validar o JWT, carregar o `Admin` atual do Postgres por `admin_id` do claim, rejeitar com 401 se `token.iat < admin.SessionsRevokedAt`, e injetar o `Admin` carregado no `context` da requisição.
**Where**: `internal/api/middleware.go` (modifica)
**Depends on**: T4
**Reuses**: `RequireAuth` já existente (T12 do mvp-core)
**Requirement**: ADM-05, ADM-07

**Tools**:
- MCP: NONE
- Skill: `security-best-practices`, `best-practices`

**Done when**:
- [x] Token com `iat` anterior a `sessions_revoked_at` é rejeitado com 401
- [x] Token válido e não revogado segue com `Admin` disponível no `context`
- [x] Testes de integração cobrem os 2 casos + o caso já existente de token ausente/inválido/expirado (regressão)
- [x] Gate check passa: `go test ./... -tags=integration && go vet ./...`

**Tests**: integration
**Gate**: full

---

### T6: Middleware `RequireRole`

**What**: Novo middleware `RequireRole(roles ...string) func(http.Handler) http.Handler` que lê o `Admin` do `context` (setado por `RequireAuth`) e rejeita com 403 se `Admin.Role` não estiver entre os papéis permitidos pra rota.
**Where**: `internal/api/middleware.go` (modifica)
**Depends on**: T5
**Reuses**: `context` já populado por T5
**Requirement**: ADM-09, ADM-10, ADM-11, ADM-12

**Tools**:
- MCP: NONE
- Skill: `security-best-practices`, `best-practices`

**Done when**:
- [x] Papel presente na lista permitida segue pro handler
- [x] Papel fora da lista permitida retorna 403 sem executar o handler
- [x] Testes unitários cobrem os 2 casos pra cada um dos 3 papéis
- [x] Gate check passa: `go test ./... && go vet ./...`

**Tests**: unit
**Gate**: quick

---

### T7: Endpoint `POST /api/admins` (convidar admin)

**What**: Handler restrito a `owner` que recebe email + role, invalida convite pendente existente pro mesmo email (T2), cria novo convite, envia email com link de token (reaproveita mecanismo de envio do reset de senha do mvp-core), e registra `admin_audit_log` com action `invited`.
**Where**: `internal/api/admins.go`
**Depends on**: T6
**Reuses**: mecanismo de envio de email do fluxo de reset de senha (T14 do mvp-core)
**Requirement**: ADM-01, ADM-02, ADM-08, ADM-09

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `owner` consegue convidar; `operator`/`viewer` recebem 403
- [x] Convite duplicado pro mesmo email invalida o anterior sem criar registro duplicado
- [x] `admin_audit_log` recebe entrada `invited` com actor e target corretos
- [x] Testes de integração cobrem os 3 casos acima
- [x] Gate check passa: `go test ./... -tags=integration && go vet ./...`

**Tests**: integration
**Gate**: full

**Commit**: `feat(admin-dashboard): add admin invite endpoint`

---

### T8: Endpoint `POST /api/admins/invite/{token}/accept`

**What**: Handler público que valida o token de convite (existe, não expirado, não usado), define a senha submetida, e ativa a conta `Admin` com o papel definido no convite.
**Where**: `internal/api/admins.go` (modifica)
**Depends on**: T7
**Reuses**: padrão de validação de token de T14 (reset de senha) do mvp-core
**Requirement**: ADM-03, ADM-04

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Token válido ativa a conta com o papel correto e marca o convite como usado
- [x] Token expirado ou já usado retorna 401 sem alterar estado
- [x] Testes de integração cobrem os 2 casos
- [x] Gate check passa: `go test ./... -tags=integration && go vet ./...`

**Tests**: integration
**Gate**: full

**Commit**: `feat(admin-dashboard): add invite acceptance endpoint`

---

### T9: Endpoint `PATCH /api/admins/{id}/role`

**What**: Handler restrito a `owner` que, dentro de uma transação com `CountActiveOwners` (`FOR UPDATE`), rejeita a mudança com 409 se resultaria em zero owners ativos (incluindo o próprio owner se rebaixando), senão aplica o novo papel, revoga as sessões do admin afetado (T4), e registra `admin_audit_log` com action `role_changed`.
**Where**: `internal/api/admins.go` (modifica)
**Depends on**: T8
**Reuses**: `UpdateRole`/`RevokeSessions`/`CountActiveOwners` (T4)
**Requirement**: ADM-05, ADM-06, ADM-08, ADM-09

**Tools**:
- MCP: NONE
- Skill: `security-best-practices`, `best-practices`

**Done when**:
- [x] Mudança de papel válida aplica o novo role e revoga sessões do afetado
- [x] Tentativa que zeraria owners ativos é rejeitada com 409, estado não muda
- [x] Sessão antiga do admin afetado passa a ser rejeitada pelo `RequireAuth` (T5) após a mudança
- [x] `admin_audit_log` recebe entrada `role_changed`
- [x] Testes de integração cobrem os 3 casos acima, incluindo tentativa de self-demotion como último owner
- [x] Gate check passa: `go test ./... -tags=integration && go vet ./...`

**Tests**: integration
**Gate**: full

**Commit**: `feat(admin-dashboard): add admin role change endpoint`

---

### T10: Endpoint `DELETE /api/admins/{id}`

**What**: Handler restrito a `owner` que, dentro de transação com `CountActiveOwners` (`FOR UPDATE`), rejeita a remoção com 409 se resultaria em zero owners ativos, senão revoga sessões (T4), remove o admin, e registra `admin_audit_log` com action `removed`.
**Where**: `internal/api/admins.go` (modifica)
**Depends on**: T9
**Reuses**: `RevokeSessions`/`CountActiveOwners`/`Delete` (T4)
**Requirement**: ADM-06, ADM-07, ADM-08, ADM-09

**Tools**:
- MCP: NONE
- Skill: `security-best-practices`, `best-practices`

**Done when**:
- [x] Remoção válida revoga sessões e remove a conta
- [x] Tentativa que zeraria owners ativos é rejeitada com 409
- [x] `admin_audit_log` recebe entrada `removed`
- [x] Testes de integração cobrem os 2 casos, incluindo self-removal como último owner
- [x] Gate check passa: `go test ./... -tags=integration && go vet ./...`

**Tests**: integration
**Gate**: full

**Commit**: `feat(admin-dashboard): add admin removal endpoint`

---

### T11: Endpoint `GET /api/admins`

**What**: Handler restrito a `owner` que lista admins com email e papel atual.
**Where**: `internal/api/admins.go` (modifica)
**Depends on**: T10
**Reuses**: nenhum - listagem simples
**Requirement**: ADM-09

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `owner` recebe lista completa; `operator`/`viewer` recebem 403
- [x] Testes de integração cobrem os 2 casos
- [x] Gate check passa: `go test ./... -tags=integration && go vet ./...`

**Tests**: integration
**Gate**: full

**Commit**: `feat(admin-dashboard): add admin listing endpoint`

---

### T12: Aplicar `RequireRole` nas rotas de escrita do `mvp-core`

**What**: No arquivo central de registro de rotas, adiciona `RequireRole("owner", "operator")` em toda rota de escrita já especificada no mvp-core (integrations, services, domains, status-pages, incidents); rotas de leitura seguem acessíveis a `owner`/`operator`/`viewer`.
**Where**: `internal/api/router.go` (modifica)
**Depends on**: T11
**Reuses**: rotas já registradas pelo mvp-core (T18, T20, T27, T29, T37, T38, T39)
**Requirement**: ADM-10, ADM-11, ADM-12

**Tools**:
- MCP: NONE
- Skill: `security-best-practices`, `best-practices`

**Done when**:
- [x] `viewer` recebe 403 em toda rota de escrita listada no design; recebe 200 em rotas de leitura
- [x] `operator` e `owner` continuam funcionando em todas as rotas como antes
- [x] Testes de integração cobrem pelo menos 1 rota de escrita e 1 de leitura por papel (3 papéis × 2 tipos de rota)
- [x] Gate check passa: `go test ./... -tags=integration && go vet ./...`

**Tests**: integration
**Gate**: full

**Commit**: `feat(admin-dashboard): enforce role-based authorization on mvp-core routes`

---

### T13: Endpoint `GET /api/poller/status`

**What**: Handler acessível a `owner`/`operator`/`viewer` que retorna, por `Integration`, `last_checked_at`, `status` e `last_error`, lendo diretamente os campos já persistidos pelo poller do mvp-core.
**Where**: `internal/api/poller_status.go`
**Depends on**: T12
**Reuses**: `Integration.LastCheckedAt`/`Status`/`LastError` já persistidos pelo poller (T24 do mvp-core) - sem nova lógica de fetch
**Requirement**: ADM-13, ADM-14

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Resposta reflete o último estado persistido pelo poller, sem chamada nova ao Datadog
- [ ] Falha de conexão registrada pelo poller aparece com `status = "invalid"` e `last_error` preenchido
- [ ] Todos os 3 papéis conseguem acessar (200)
- [ ] Testes de integração cobrem sucesso e cenário de falha persistida
- [ ] Gate check passa: `go test ./... -tags=integration && go vet ./...`

**Tests**: integration
**Gate**: full

**Commit**: `feat(admin-dashboard): add poller status endpoint`

---

## Phase Execution Map

```
Phase 1 → Phase 2 → Phase 3 → Phase 4

Phase 1:  T1 ------→ T2 ------→ T3 ------→ T4
Phase 2:  T4 ------→ T5 ------→ T6
Phase 3:  T6 ------→ T7 ------→ T8 ------→ T9 ------→ T10 ------→ T11
Phase 4:  T11 -----→ T12 -----→ T13
```

Execução é estritamente sequencial - sem paralelismo intra-fase. Um único agente (ou batch worker) trabalha uma task por vez, em ordem.

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1: Migration role/revogação | 1 arquivo de migration | ✅ Granular |
| T2: Migration + repo admin_invites | 1 arquivo (migration+repo colocados) | ✅ Granular |
| T3: Migration + repo audit log | 1 arquivo | ✅ Granular |
| T4: Extensão repo Admin | 1 arquivo (modifica) | ✅ Granular |
| T5: RequireAuth estendido | 1 arquivo (modifica) | ✅ Granular |
| T6: RequireRole novo | 1 arquivo (modifica, mesmo de T5 mas função nova e independente) | ✅ Granular |
| T7: POST /api/admins | 1 endpoint | ✅ Granular |
| T8: POST /api/admins/invite/{token}/accept | 1 endpoint | ✅ Granular |
| T9: PATCH /api/admins/{id}/role | 1 endpoint | ✅ Granular |
| T10: DELETE /api/admins/{id} | 1 endpoint | ✅ Granular |
| T11: GET /api/admins | 1 endpoint | ✅ Granular |
| T12: Aplicar RequireRole nas rotas do mvp-core | 1 arquivo (registro de rotas) | ✅ Granular |
| T13: GET /api/poller/status | 1 endpoint | ✅ Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | None | ✅ Match |
| T2 | T1 | T1 → T2 | ✅ Match |
| T3 | T2 | T2 → T3 | ✅ Match |
| T4 | T3 | T3 → T4 | ✅ Match |
| T5 | T4 | T4 → T5 | ✅ Match |
| T6 | T5 | T5 → T6 | ✅ Match |
| T7 | T6 | T6 → T7 | ✅ Match |
| T8 | T7 | T7 → T8 | ✅ Match |
| T9 | T8 | T8 → T9 | ✅ Match |
| T10 | T9 | T9 → T10 | ✅ Match |
| T11 | T10 | T10 → T11 | ✅ Match |
| T12 | T11 | T11 → T12 | ✅ Match |
| T13 | T12 | T12 → T13 | ✅ Match |

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1: Migration role/revogação | Migrations/config | none | none | ✅ OK |
| T2: admin_invites | Repository | integration | integration | ✅ OK |
| T3: admin_audit_log | Repository | integration | integration | ✅ OK |
| T4: Repo Admin extension | Repository | integration | integration | ✅ OK |
| T5: RequireAuth estendido | API/auth (depende de DB) | integration | integration | ✅ OK |
| T6: RequireRole | Domain logic (sem DB) | unit | unit | ✅ OK |
| T7: POST /api/admins | API handler | integration | integration | ✅ OK |
| T8: accept invite | API handler | integration | integration | ✅ OK |
| T9: PATCH role | API handler | integration | integration | ✅ OK |
| T10: DELETE admin | API handler | integration | integration | ✅ OK |
| T11: GET /api/admins | API handler | integration | integration | ✅ OK |
| T12: RequireRole nas rotas mvp-core | API handler (roteamento) | integration | integration | ✅ OK |
| T13: GET /api/poller/status | API handler | integration | integration | ✅ OK |

---

## Tips

(referência - ver `tasks.md` reference da skill para regras completas)
