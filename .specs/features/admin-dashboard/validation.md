# Admin Dashboard Validation

**Date**: 2026-08-19
**Spec**: `.specs/features/admin-dashboard/spec.md`
**Diff range**: `4f1e70a..d719366` (16 commits; T1 = `508b6e1` → T13 = `8d0396f`, fixes `4a4ef81`, `545820d`, `d719366`)
**Verifier**: independent sub-agent, round 2 of 3 (author ≠ verifier), evidence-or-zero
**Round 1 verdict**: ❌ FAIL (5 of 12 mutants survived) — this round re-derived everything from source, it did not accept the fix agent's report.

**Verdict**: ✅ **PASS** — 23 mutations injected, 23 killed. All 14 requirements traced to `file:line` + assertion. Gate green.

---

## Task Completion

| Task | Status | Notes |
| --- | --- | --- |
| T1 migration role/revogação | ✅ Done | `internal/db/migrations/0009_admin_role_and_revocation.up.sql` |
| T2 admin_invites + repo | ✅ Done | `internal/db/admin_invites.go` |
| T3 admin_audit_log + repo | ✅ Done | `internal/audit/log.go` |
| T4 repo Admin (role/revoke/count/delete) | ✅ Done | `internal/db/admin_repository.go` |
| T5 RequireAuth carrega Admin + revogação | ✅ Done | `internal/api/middleware.go:66-75` |
| T6 RequireRole | ✅ Done | `internal/api/middleware.go` |
| T7 POST /api/admins | ✅ Done | TTL de 1h agora asserido a partir da linha persistida (`internal/api/admins_test.go:159-164`) |
| T8 POST /api/admins/invite/{token}/accept | ✅ Done | `internal/api/admins.go` |
| T9 PATCH /api/admins/{id}/role | ✅ Done | revogação de sessão asserida |
| T10 DELETE /api/admins/{id} | ✅ Done | revogação + JWT antigo → 401 agora asseridos (`admins_test.go:655-701`) |
| T11 GET /api/admins | ✅ Done | - |
| T12 RequireRole nas rotas do mvp-core | ✅ Done | todas as 7 rotas de escrita + 4 de gestão exercidas via `buildAdminRouter` |
| T13 GET /api/poller/status | ✅ Done | acesso de `viewer` no router real coberto |

**Montagem real (re-verificada por leitura direta nesta rodada):** `internal/cli/routes.go:28` `buildAdminRouter` é o handler usado pelo binário (`internal/cli/serve.go`). Rotas de escrita do mvp-core em `routes.go:66-72` com `writeRoles` (`:46`), gestão de admins em `:60-63` com `ownerOnly` (`:48`), leituras + poller status em `:76-78` com `anyRole` (`:47`). Nenhum handler de escrita fora do router real.

---

## Spec-Anchored Acceptance Criteria

| Requisito / Critério | Outcome definido na spec | `file:line` + assertion | Resultado |
| --- | --- | --- | --- |
| **ADM-01** convite: token válido 1h, registro pending, envio por email | TTL de **1 hora**; registro pending; envio por email | `internal/api/admins_test.go:159-164` — `gotTTL := invite.expiresAt.Sub(invite.createdAt)`, `wantTTL = 1h`, tolerância 2s, lendo `expires_at`/`created_at` da linha que o **handler** persistiu (`latestInviteForEmail`, `:110-121`); `:146` — `invite.role != db.RoleOperator`; `:152` — `invite.usedAt != nil` | ✅ PASS (TTL) + ⚠️ 2 SPEC_DEVIATION declarados (ver nota abaixo) |
| **ADM-02** convite duplicado invalida o anterior, sem duplicar | invite anterior invalidado, exatamente 1 pendente | `internal/api/admins_test.go:224` — `firstUsedAt == nil` → falha; `:234` — `pendingCount != 1` | ✅ PASS |
| **ADM-03** accept ativa conta com papel do convite | conta ativa com `role` do convite; convite marcado usado | `internal/api/admins_test.go:333/341` — `gotRole != db.RoleOperator`; `invite.UsedAt == nil` | ✅ PASS |
| **ADM-04** token expirado/usado rejeita definição de senha | 401, sem alterar estado | `internal/api/admins_test.go:359/363/386` — `rec.Code != 401`; `GetByEmail` → `ErrNotFound`; segundo accept `!= 401` | ✅ PASS |
| **ADM-05** mudança de papel aplica imediato + invalida sessões | novo role persistido; `sessions_revoked_at` ≥ `iat`; token antigo → 401 | `internal/api/admins_test.go:476/479` — `gotRole != db.RoleViewer`, `revokedAt == nil \|\| revokedAt.Before(...IssuedAt)`; `internal/api/middleware_test.go:189` — `rec.Code != 401` | ✅ PASS |
| **ADM-06** ação que zeraria owners ativos → rejeita | 409 + estado inalterado, inclusive self-demotion/self-removal | `internal/api/admins_test.go:557/565/568` (PATCH), `:665/673` (DELETE); `internal/api/admins_logic_test.go` — `TestWouldLeaveZeroOwners` (tabela de fronteira) | ✅ PASS |
| **ADM-07** remoção revoga sessões + impede login futuro | sessões revogadas; JWT anterior → 401 | `internal/api/admins_test.go:655` `TestDeleteAdmin_ValidRemoval_OldJWTRejected_401`; `:696` — `protectedRec.Code != 401`; `:699` — `gotAdmin != nil` (admin removido não chega ao handler) | ✅ PASS (fechado na rodada 2) |
| **ADM-08** audit log append-only com actor/target/ação/timestamp | linha com actor/target/action + timestamp | `internal/api/admins_test.go:169/172` (`invited`), `:489` (`role_changed`), `:643` (`removed`); `internal/audit/log_test.go` — `TestLog_Record_InsertsRowWithTimestamp`, `TestLog_Record_SurvivesReferencedAdminRemoval` | ✅ PASS |
| **ADM-09** gestão de admins restrita a `owner`; operator/viewer → 403 | 403 para operator e viewer nas 4 rotas | `internal/cli/routes_test.go:362` `TestAdminRouter_OperatorAndViewer_AdminManagementRoutes_403` → `:372` `rec.Code != 403`, tabela `adminManagementRouteCases()` (`:266-302`) cobrindo POST/GET/PATCH/DELETE `/api/admins` **através de `buildAdminRouter`**; `:384` owner passa autorização | ✅ PASS (fechado na rodada 2) |
| **ADM-10** owner/operator executam todas as ações de escrita do mvp-core | nenhuma rota de escrita retorna 401/403 para owner/operator | `internal/cli/routes_test.go:339` `TestAdminRouter_OwnerAndOperator_AllWriteRoutes_PassAuthorization` → `:349` `rec.Code == 401 \|\| 403` → falha, sobre `writeRouteCases()` (`:178-259`, as 7 rotas de `routes.go:66-72`) | ✅ PASS |
| **ADM-11** viewer em escrita em **qualquer** endpoint → 403 | 403 em todas as rotas de escrita | `internal/cli/routes_test.go:319` `TestAdminRouter_Viewer_AllWriteRoutes_403` → `:326` `rec.Code != http.StatusForbidden` para cada uma das 7 rotas | ✅ PASS (fechado na rodada 2) |
| **ADM-12** papel verificado no middleware antes do handler | 403 antes do handler rodar | `internal/api/middleware_role_test.go` — `TestRequireRole_{Owner,Operator,Viewer}{Allowed,Disallowed}`, `TestRequireRole_NoAdminInContext_403` | ✅ PASS |
| **ADM-13** qualquer papel vê timestamp, resultado e erro por integração | `status`, `last_checked_at`, `last_error`; 200 para os 3 papéis | `internal/api/poller_status_test.go:132/135/138` — `Status != "invalid"`, `*LastError != failureReason`, `LastCheckedAt == nil`; `:149/160/171` — 200 por papel; `internal/cli/routes_test.go:401` — `viewer` → `/api/poller/status` = 200 **no router real** | ✅ PASS (fechado na rodada 2) |
| **ADM-14** reaproveita dado persistido, sem duplicar fetch | nenhuma chamada nova ao Datadog | `internal/api/poller_status.go:48` — única dependência é `integrations.List`; o arquivo não importa `internal/connectors/datadog`. Verificação estrutural (leitura + import graph); sem teste que asserte ausência de fetch | ✅ PASS (estrutural) |

**Status**: ✅ 14/14 requisitos com evidência `file:line` e outcome conforme a spec.

**Nota ADM-01 / ADM-08 — 2 `SPEC_DEVIATION` declarados, carregados adiante (não bloqueantes):**

1. `internal/api/admins.go:83-89` — não existe linha de `Admin` em estado "pending"; o estado pending é modelado pela linha em `admin_invites`, e o `Admin` só é criado no accept. Divergência do texto literal de ADM-01, mas consistente com o schema de `design.md` já commitado em T1-T4, e todos os comportamentos observáveis (ADM-02/03/04, edge case de email já ativo) passam.
2. `internal/api/admins.go:91-93` — `admin_audit_log.target_id` do evento `invited` aponta pro ID do invite, porque não há `Admin` ainda e a coluna é `NOT NULL`.
3. **Envio de email**: `internal/api/admins.go:125-129` loga o token em vez de despachar por provider. Isto **é** "o mesmo mecanismo do reset de senha do mvp-core" que `design.md:76` especifica — `internal/api/password_reset_handler.go:102` faz exatamente o mesmo. Convenção do projeto, não regressão desta feature.

Nenhum dos três é falha silenciosa; os três estão declarados no código. Recomendação: virar item explícito de backlog (provider de email real) em vez de gap de validação.

---

## Discrimination Sensor

Scratch isolado: `git worktree add --detach <scratch> HEAD`, mutação, `go test`, `git checkout -- .`, `git worktree remove --force`. **Nunca** foi usado `git stash`. Baseline `git status --porcelain` da árvore real capturado antes do sensor e reconferido depois — idêntico (3 arquivos untracked em `.specs/`). Profundidade: **P0-full** (caminho de autenticação/autorização).

### Bloco A — re-verificação independente dos 5 gaps da rodada 1

| # | Arquivo:linha | Mutação | Morta? | Teste que matou |
| --- | --- | --- | --- | --- |
| M1 | `internal/cli/routes.go:72` | remove `RequireRole` de `POST /api/status-pages` | ✅ Killed | `TestAdminRouter_Viewer_AllWriteRoutes_403/POST_/api/status-pages` |
| M2 | `internal/cli/routes.go:69` | remove `RequireRole` de `POST /api/incidents` | ✅ Killed | `.../POST_/api/incidents` |
| M3 | `internal/cli/routes.go:60` | remove `ownerOnly` de `POST /api/admins` | ✅ Killed | `TestAdminRouter_OperatorAndViewer_AdminManagementRoutes_403` (operator e viewer) |
| M4 | `internal/api/admins.go:22` | `adminInviteTTL` `1h` → `24h` | ✅ Killed | `TestInviteAdmin_Owner_201_CreatesInviteAndAuditEntry` |
| M5 | `internal/api/middleware.go:66-70` | `RequireAuth` ignora erro de `GetByID` (admin removido vira owner sintético) | ✅ Killed | `TestDeleteAdmin_ValidRemoval_OldJWTRejected_401` |
| M6 | `internal/cli/routes.go:78` | `GET /api/poller/status`: `anyRole` → `writeRoles` | ✅ Killed | `TestAdminRouter_Viewer_PollerStatus_200` |

**Os 5 gaps da rodada 1 (M5/M6/M8/M10/M12 de então) estão empiricamente fechados.**

### Bloco B — varredura nova, escolhida nesta rodada a partir de `spec.md`/`design.md`

| # | Arquivo:linha | Mutação | Morta? | Teste que matou |
| --- | --- | --- | --- | --- |
| M7 | `internal/api/admins.go:148-150` | remove `audit.Record(..., "invited")` (ADM-08) | ✅ Killed | `TestInviteAdmin_Owner_201_CreatesInviteAndAuditEntry` |
| M8 | `internal/api/admins.go:326-328` | remove `audit.Record(..., "role_changed")` (ADM-08) | ✅ Killed | `TestUpdateAdminRole_ValidChange_200_AppliesRoleRevokesSessionsAndAudits` |
| M9 | `internal/api/admins.go:400-402` | remove `audit.Record(..., "removed")` (ADM-08) | ✅ Killed | `TestDeleteAdmin_ValidRemoval_200_RevokesSessionsDeletesAndAudits` |
| M10 | `internal/api/admins.go:115-119` | remove `InvalidatePendingForEmail` (ADM-02: convite duplicado deixa de invalidar o anterior) | ✅ Killed | `TestInviteAdmin_DuplicatePendingInvite_InvalidatesPreviousWithoutDuplicateRow` |
| M11 | `internal/api/admins.go:199-202` | remove a checagem `UsedAt != nil \|\| now > ExpiresAt` no accept (ADM-04) | ✅ Killed | `TestAcceptInvite_ExpiredToken_401_NoStateChange`, `TestAcceptInvite_AlreadyUsedToken_401` |
| M12 | `migrations/0009_...up.sql:2` | backfill/default de `role`: `'owner'` → `'viewer'` | ✅ Killed | `TestAdminRepository_CountActiveOwners_CountsOnlyOwners` |
| M13 | `internal/api/poller_status.go:60` | `LastCheckedAt` sempre `nil` (ADM-13) | ✅ Killed | `TestPollerStatus_PersistedFailure_ReflectsInvalidStatusAndError` |
| M14 | `internal/api/poller_status.go:59` | `Status` sempre `"active"` (ADM-13) | ✅ Killed | idem |
| M15 | `internal/api/admins.go:314-318` | `UpdateRole` deixa de gravar `sessions_revoked_at` (ADM-05) | ✅ Killed | `TestUpdateAdminRole_ValidChange_200_...` |
| M16 | `internal/api/admins.go:58` | off-by-one em `wouldLeaveZeroOwners`: `ownerCount <= 1` → `<= 0` (ADM-06) | ✅ Killed | `TestWouldLeaveZeroOwners/owner_demoted,_last_owner`, `TestUpdateAdminRole_SelfDemotionAsLastOwner_409`, `TestDeleteAdmin_SelfRemovalAsLastOwner_409` |
| M17 | `internal/api/middleware.go:71` | `claims.IssuedAt.Before(...)` → `.After(...)` (ADM-05/07) | ✅ Killed | `TestRequireAuth_TokenIssuedBeforeRevocation_401`, `TestRequireAuth_TokenIssuedAfterRevocation_PassesThrough` |

### Bloco C — cobertura exaustiva do wiring restante (`routes.go`), rota a rota

| # | Rota mutada (remoção do `RequireRole`) | Morta? |
| --- | --- | --- |
| M1b | `POST /api/domains` (`routes.go:66`) | ✅ Killed |
| M1c | `POST /api/services` (`:67`) | ✅ Killed |
| M1d | `POST /api/integrations/datadog` (`:68`) | ✅ Killed |
| M1e | `POST /api/incidents/{id}/updates` (`:70`) | ✅ Killed |
| M1f | `PATCH /api/incidents/{id}` (`:71`) | ✅ Killed |
| M3b | `GET /api/admins` (`:61`) | ✅ Killed |
| M3c | `PATCH /api/admins/{id}/role` (`:62`) | ✅ Killed |
| M3d | `DELETE /api/admins/{id}` (`:63`) | ✅ Killed |

**Result**: **23 injetadas, 23 mortas, 0 sobreviventes** — ✅ PASS.
As 11 rotas protegidas de `routes.go` (7 de escrita mvp-core + 4 de gestão de admin) e a rota `anyRole` do poller status foram mutadas **individualmente**; nenhuma passa despercebida. Duas mutações intermediárias (M8/M9 na primeira formulação) não compilaram e foram reescritas até serem semanticamente válidas antes de contar — mutantes que não compilam não são contados nem como mortos nem como sobreviventes.

---

## Code Quality

| Princípio | Status |
| --- | --- |
| Minimum code | ✅ |
| Surgical changes | ✅ (as 3 correções mexeram só em `_test.go`) |
| No scope creep | ✅ nenhum código de produção alterado pela rodada de fix (`git show --stat 4a4ef81 545820d d719366` → só arquivos de teste) |
| Matches patterns | ✅ |
| Spec-anchored outcome check | ✅ 14/14 |
| Per-layer Coverage Expectation | ✅ rotas: happy + negado por papel em todas as rotas protegidas |
| Todo teste mapeia a um requisito | ✅ nenhum teste órfão |
| Guidelines documentados seguidos | ✅ `Test Coverage Matrix` de `tasks.md` |

---

## Edge Cases

- [x] `spec.md:97` — JWT emitido antes da remoção → 401: `internal/api/admins_test.go:655-701` (M5 confirma que é discriminante).
- [x] `spec.md:98` — last-write-wins em alteração concorrente: transação com `SELECT ... FOR UPDATE` (`admins.go:286`, `:359`); sem teste de concorrência (aceitável para o MVP).
- [x] `spec.md:99` — convite pra email já ativo → 409: `internal/api/admins_test.go:251`.

---

## Gate Check

- **Gate command**: `go build ./... && gofmt -l . && go test ./... -tags=integration -p 1 && go vet ./...`
- **Result**: build ✅ · `gofmt -l` vazio ✅ · `go vet ./...` ✅ · `go vet -tags=integration ./...` ✅ · testes **164 passed, 0 failed, 0 skipped** (+45 subtests nomeados → 209 casos executados)
- **Contagem de funções `func Test`**: `4f1e70a` (pré-feature) = **100** → `8d0396f` (fim da rodada 1) = **158** → `d719366` (HEAD) = **164**. Delta da feature: **+64**. A rodada 1 reportou "166"; recontagem independente nesta rodada dá **164** — a rodada 1 contou errado, nenhum teste foi apagado (`158 → 164`, só adições).
- **Skipped**: nenhum.
- **Flake de infraestrutura conhecido (não relacionado à feature, não corrigido)**: sob `-p` default (paralelo entre pacotes), corridas ocasionais falham com `dbtest: pg_advisory_lock failed: timeout: context deadline exceeded`. Não reproduz com `-p 1`, que é o modo do gate. É contenção do lock de teste no Postgres compartilhado, não comportamento da feature. Recomendação: aumentar o timeout do advisory lock em `internal/dbtest/lock.go` ou fixar `-p 1` no `Makefile`; fora do escopo desta spec.

---

## Fix Plans

Nenhum. Zero gaps bloqueantes nesta rodada.

Itens carregados adiante (backlog, não gates):
1. Provider de email real para convite e reset de senha (hoje ambos logam o token) — decisão de projeto herdada do mvp-core.
2. Flake de `pg_advisory_lock` sob execução paralela — infra de teste.

---

## Requirement Traceability Update

| Requisito | Status anterior (rodada 1) | Novo status |
| --- | --- | --- |
| ADM-01 | ⚠️ Needs Fix (spec-precision) | ✅ Verified (TTL asserido; deviations declarados) |
| ADM-02 | ✅ Verified | ✅ Verified |
| ADM-03 | ✅ Verified | ✅ Verified |
| ADM-04 | ✅ Verified | ✅ Verified |
| ADM-05 | ✅ Verified | ✅ Verified |
| ADM-06 | ✅ Verified | ✅ Verified |
| ADM-07 | ❌ Needs Fix | ✅ Verified |
| ADM-08 | ✅ Verified | ✅ Verified |
| ADM-09 | ❌ Needs Fix | ✅ Verified |
| ADM-10 | ⚠️ Needs Fix | ✅ Verified |
| ADM-11 | ❌ Needs Fix | ✅ Verified |
| ADM-12 | ✅ Verified | ✅ Verified |
| ADM-13 | ⚠️ Needs Fix | ✅ Verified |
| ADM-14 | ✅ Verified | ✅ Verified |

---

## Summary

**Overall**: ✅ Ready

**Spec-anchored check**: 14/14 requisitos com outcome asserido conforme a spec; 3 `SPEC_DEVIATION` declarados e justificados (nenhum silencioso)
**Sensor**: 23/23 mutações mortas (0 sobreviventes), profundidade P0-full
**Gate**: 164 passed, 0 failed, 0 skipped (`-p 1`)

**O que funciona**: revogação de sessão em `RequireAuth` (M5, M17 mortos), `RequireRole` em **todas** as 11 rotas protegidas do router de produção mutadas uma a uma (M1-M3, M1b-M1f, M3b-M3d mortos), TTL de 1h do convite lido da linha persistida pelo handler (M4 morto), invalidação de convite duplicado (M10), rejeição de convite expirado/usado (M11), audit log nas 3 ações (M7-M9), proteção contra lockout de owner incluindo a fronteira `ownerCount <= 1` (M16), JWT pré-remoção rejeitado com 401 (M5), status do poller sem novo fetch ao Datadog (M13/M14), backfill de `role='owner'` na migration (M12).

**Diferença em relação à rodada 1**: a suíte agora discrimina o wiring de produção. `internal/cli/routes_test.go` dirige `buildAdminRouter` — o mesmo handler que `serve.go` monta — por tabela, cobrindo cada rota individualmente em vez de usar `POST /api/domains` como proxy. Qualquer remoção futura de `RequireRole` quebra teste.

**Next steps**: feature pronta. Levar os 2 itens de backlog (provider de email, flake do advisory lock) pro planejamento seguinte.
