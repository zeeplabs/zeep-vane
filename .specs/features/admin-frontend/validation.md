# Validation — admin-frontend

## 2026-08-21 — Etapa 0 (Fundação, I1-I9, ver AD-007)

**Verdict: PASS** (o achado de hygiene de commit em I9, registrado abaixo, foi corrigido após esta verificação — ver nota de correção antes de "Achado real")

**Diff range original verificado**: `f48b1b9` (feat(auth): add GET /api/auth/me identity endpoint) .. `b02d362` (refactor(web): move API types out of mockData.ts into types/api.ts) — 9 commits, branch `main`.

**Correção pós-verificação (2026-08-21)**: `b02d362` foi desfeito (`git reset --soft`) e recriado como 2 commits — `cd10b28` (mesma mensagem, agora só a migração de tipos, +159/-107) e `099f3d1` (`feat(web): carry forward uncommitted UI polish from the mock-first round`, +272/-75, cobrindo exatamente os 4 arquivos apontados no achado abaixo). `npm run build`/`npm run test` reconfirmados idênticos após o split (mesmo bundle, mesmas 49 falhas esperadas). Nenhum outro commit da lista foi tocado.

**Escopo revisado**: apenas os 9 commits acima (I1-I9). A pilha de arquivos não commitados/untracked em `web/` de uma rodada anterior (mock-first, UI-only) foi ignorada, conforme instrução.

### Per-task evidence

| Task | Done-when | Evidence | Status |
|---|---|---|---|
| I1 | `GET /api/auth/me` 200 `{id,email,role}` p/ qualquer papel; 401 sem sessão | `internal/api/auth_handler.go:84-104` (`Me` handler, lê `AdminFromContext`); `TestMe_ValidSession_200WithIdentity`, `TestMe_NoSession_401` em `auth_handler_test.go:132-208`; rota em `internal/cli/routes.go:59` (`protected.With(anyRole).Get("/api/auth/me", ...)`) | PASS |
| I2 | Login seta cookie `vane_session` c/ atributos corretos; corpo `{token}` inalterado; testes de login existentes intactos | `internal/api/auth_handler.go:79-99` (`sessionCookie` helper: `HttpOnly:true`,`Secure:true`,`SameSite:Strict`,`Path:"/"`,`MaxAge=SessionTTL`); `TestLogin_CorrectCredentials_SetsSessionCookie` assevera cada atributo individualmente + corpo `{token}` não-vazio | PASS |
| I3 | Cookie-only autentica; header tem prioridade sobre cookie; testes de header existentes intactos | `internal/api/middleware.go:53-56` (fallback só quando `bearerToken` vazio); `TestRequireAuth_CookieOnly_PassesThrough`, `TestRequireAuth_HeaderTakesPriorityOverCookie` (usa 2 admins diferentes p/ header vs cookie e confirma que o admin do contexto é o do header) | PASS |
| I4 | Logout 200 expira cookie; requisição subsequente c/ cookie expirado → 401 | `internal/api/auth_handler.go:126-131` (`Logout` seta `sessionCookie("", -1)`); `TestLogout_ExpiresCookie_SubsequentRequestRejected` confirma `MaxAge<0` e replay do cookie expirado → 401 | PASS |
| I5 | Preflight de origem permitida recebe Allow-Origin+Allow-Credentials; origem fora da allowlist não recebe headers; teste explícito nunca-wildcard-com-credentials | `internal/api/cors.go` (`corsOptions` — origem única, nunca `"*"`); `TestCORS_PreflightFromAllowedOrigin_GrantsCredentials`, `TestCORS_PreflightFromDisallowedOrigin_NoOriginHeaderGranted`, `TestCORS_NeverCombinesWildcardOriginWithCredentials` (asserção direta na struct `cors.Options`, não só comportamento observado) | PASS |
| I6 | `apiFetch` faz fetch real c/ `credentials:"include"`; `ApiError` correto; `triggerUnauthorized` em 401 real; base URL via env | `web/src/lib/apiClient.ts` (rewrite completo, zero `handleRoute` remanescente); `web/.env.development` (`VITE_API_BASE_URL`); sem teste próprio nesta task (coberto por I7) — conforme planejado | PASS |
| I7 | `AuthProvider.test.tsx` 100% via MSW; handler cobre login 200/401, `/me` auth/anon, logout; gate `npm run test` (escopado) | `web/src/test/msw/{server,handlers}.ts` (novo); `web/src/auth/AuthProvider.test.tsx` sem import de `mockData`/`handleRoute`; `apiClient.ts` ganhou fix `res.text()` p/ corpo vazio (confirmado no diff, condiz com o SPEC_DEVIATION documentado); suíte cheia com 49 falhas nos arquivos ainda não migrados (ver "Known issues") | PASS |
| I8 | Build de produção sem `mockData`; controle "Visualizando como" só em DEV | `AuthProvider.tsx` — `setDevRole` faz `import("../lib/mockData")` dinâmico só sob `import.meta.env.DEV`; `Sidebar.tsx` — bloco `{import.meta.env.DEV ? (...) : null}`; `grep` em `dist/assets/*.js` do build real por `owner@vane.app`/`correct-horse`/`demo1234` (strings de fixture) → 0 ocorrências | PASS |
| I9 | Nenhum arquivo de produção importa tipo de `mockData.ts`; `tsc -b --force` limpo; build passa | `web/src/types/api.ts` (novo, 92 linhas, todas as interfaces listadas); todos os imports de tipo atualizados; `npm run build` rodou limpo (tsc -b && vite build, 200 módulos, sem erro) | PASS (ver achado de hygiene abaixo) |

### Discrimination sensor — raciocínio sobre testes shallow

**CORS (`internal/api/cors.go` / `cors_test.go`)**: uma implementação errada plausível seria esquecer `AllowCredentials:true` no `cors.Options` ou deixar `AllowedOrigins` vazio por engano. `TestCORS_PreflightFromAllowedOrigin_GrantsCredentials` pega ambos os casos (falha se `Access-Control-Allow-Credentials` não for `"true"` ou se o header de origem vier vazio). Uma implementação que combinasse wildcard+credentials passaria despercebida pelos 2 primeiros testes (eles usam só 1 origem concreta), mas `TestCORS_NeverCombinesWildcardOriginWithCredentials` verifica a struct diretamente para 2 origens diferentes — pega esse caso especificamente. Não achei brecha óbvia aqui.

**RequireAuth cookie fallback (`internal/api/middleware.go`)**: uma implementação errada plausível seria inverter a prioridade (checar cookie antes do header) ou aceitar cookie mesmo com header presente mas inválido. `TestRequireAuth_HeaderTakesPriorityOverCookie` usa **dois admins distintos** para header vs cookie e verifica que o admin no contexto é exatamente o do header — isso pegaria uma inversão de prioridade ou um "cookie ganha se ambos presentes". Teste é discriminativo, não shallow.

**Cookie attributes no login (`auth_handler.go`/`sessionCookie`)**: uma implementação errada plausível seria esquecer `Secure`, usar `SameSite=Lax` em vez de `Strict`, ou não persistir `MaxAge` corretamente. `TestLogin_CorrectCredentials_SetsSessionCookie` assevera cada atributo individualmente (`HttpOnly`, `Secure`, `SameSite`, `Path`, `MaxAge`) em vez de só checar presença do cookie — pegaria qualquer regressão em atributo isolado. Não achei brecha.

Nenhum dos 3 sensores encontrou teste raso o suficiente para deixar passar uma implementação quebrada plausível.

### Regressões — backend

`TEST_DATABASE_URL=postgres://zeep:zeep@localhost:5434/zeep?sslmode=disable go test -tags=integration -count=1 ./...`:
- Com paralelismo padrão (`-p` default): 2 falhas por `dbtest: pg_advisory_lock failed: timeout` em `internal/api` (`TestPollerStatus_PersistedFailure_ReflectsInvalidStatusAndError`, `TestPollerStatus_Owner_200`) — **não** as mesmas 2 mencionadas no prompt (`TestUpdateAdminRole_SelfDemotionAsLastOwner_409`/`TestDeleteAdmin_SelfRemovalAsLastOwner_409`), mas mesma categoria/causa raiz (flake de infra de teste sob paralelismo, documentado em STATE.md do `admin-dashboard`).
- Com `-p 1`: 100% verde, confirma que é o flake conhecido de contenção de advisory lock, não regressão desta rodada.
- `gofmt -l .`: limpo. `go vet ./...`: limpo.

### Regressões — frontend

`cd web && npm run build`: limpo (tsc -b + vite build, 200 módulos).
`cd web && npm run test`: **49 falhas / 76 passando** (18 arquivos falhos de 37). Todas as 49 falhas estão nos arquivos ainda não migrados p/ MSW (`domains`, `status-pages`, `incidents`, `integrations`, `services`, `poller`, `admins`, `public-status` — hooks e páginas), exatamente o conjunto e a contagem previstos no SPEC_DEVIATION de I7 (migração dessas suítes é escopo de I13/I15/I16/I19/I20). Nenhuma categoria nova de falha — todas batem na API real sem handler MSW (erro de rede/timeout ou hook nunca resolve).

### Achado real (não-bloqueante, mas reportável)

**I9 — commit não-atômico, mensagem enganosa**: o commit `b02d362` se descreve como "Pure type migration, no behavior change" e o task I9 declara `Tests: none (refactor de tipos, sem lógica nova)`. Na prática, o diff inclui redesign substancial de UI já commitada anteriormente, não relacionado a migração de tipos:
- `web/src/features/incidents/IncidentsPage.tsx`: +219/-38 linhas — novo componente `ActiveIncidentCard` com timeline expansível, formulário de update, botões de transição de status, novo ícone `PlusIcon`. Usa hooks (`useIncidentUpdates`, `useAddIncidentUpdate`) que já existiam em `hooks.ts` antes deste commit, mas nunca estavam conectados a esta página.
- `web/src/features/admins/AdminsPage.tsx`: +42/-16 — troca de botão texto por ícone+tooltip, novo componente `Tooltip` importado, reflow de layout (título "Admins"→"Equipe", nova seção "Ativos").
- `web/src/features/poller/PollerStatusPage.tsx` e `web/src/features/services/ServicesSection.tsx`: diffs de 33 e 26 linhas, mesma natureza (nenhum é só troca de import).

Confirmado via `git log --follow -- web/src/features/incidents/IncidentsPage.tsx`: o arquivo só tem 2 commits em toda a história (`33767cb` inicial e `b02d362`) — ou seja, todo esse redesign nunca passou por um commit próprio; foi varrido para dentro do commit de I9 (provavelmente arquivos tracked já modificados no working tree da rodada mock-first anterior, capturados por `git add <file>` ao aplicar o fix de import de 1 linha no mesmo arquivo).

**Risco**: nenhuma quebra funcional detectada (build e testes relevantes passam), mas (a) a mensagem do commit é factualmente incorreta sobre seu próprio escopo, (b) esse redesign de UI não tem task/Done-when/Tests correspondente em `tasks.md` — não há teste dedicado à nova `ActiveIncidentCard` ou ao redesign de `AdminsPage`/`PollerStatusPage`, e (c) quebra a rastreabilidade requisito↔commit que o skill `tlc-spec-driven` exige (atomic commits, 1:1 com task). Recomendação: nas próximas etapas, revisar `git status`/`git diff --stat` antes de `git add` em tasks que tocam arquivos com histórico de dirtiness conhecido, e considerar um commit `fix(web): reconcile UI redesign carried from mock-first round` separado e retroativamente documentado, ou ao menos registrar como SPEC_DEVIATION explícito em `tasks.md` (o que não foi feito).

### Known issues (esperados, não bloqueantes)

- 2 testes Go flaky sob paralelismo padrão (`pg_advisory_lock` timeout) — infra de teste pré-existente, mesma causa raiz documentada em `admin-dashboard`'s validation; não reproduz com `-p 1`.
- 49 testes frontend falhando em arquivos ainda não migrados para MSW (domains/status-pages/incidents/integrações/serviços/poller/admins/public-status) — escopo explícito de I13/I15/I16/I19/I20, documentado em I7's SPEC_DEVIATION.
- Gap de i18n em `LoginPage`: mensagem de erro de login vem em inglês direto do backend (`"invalid email or password"`), sem tradução — identificado em I7, não corrigido (fora de escopo).
- Config: `defaultCORSAllowedOrigin` cai silenciosamente para `http://localhost:5173` quando `CORS_ALLOWED_ORIGIN` não está setada — aceitável para Etapa 0 (dev), mas vale confirmar que o deploy de produção sempre define essa env var explicitamente (não travado por teste algum nesta rodada).
