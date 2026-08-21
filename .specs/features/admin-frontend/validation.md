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

## 2026-08-21 — Etapa 1 (I10-I13: domains, status pages, public-status preview)

**Verdict: PASS**

**Verificador**: independente do autor. Evidência re-derivada a partir de código/testes/execução real, não do self-report de `tasks.md`.

**Commits verificados** (branch `main`, nesta ordem):
- `dd582ad` feat(admin-frontend): add DomainRepository.List and GET /api/domains (I10)
- `a138b3c` feat(admin-frontend): add StatusPageRepository.List and GET /api/status-pages (I11)
- `1279e14` feat(admin-frontend): add dev/preview public status page endpoint by ID (I12)
- `e74295f` fix(admin-frontend): gate public status preview on published state (I12 follow-up)
- `d387a63` feat(admin-frontend): wire domains, status-pages, public-status to real backend (I13)

### Per-task evidence

| Task | Done-when | Evidence | Status |
|---|---|---|---|
| I10 | `GET /api/domains` 200 `{id,hostname,created_at}`; qualquer papel autenticado; 401 sem sessão | `internal/api/domains_handler.go:78-95` (`List`, mesmo shape de `Create`); `internal/db/domain_repository.go:56` (`List`, ordenado por hostname); rota `internal/cli/routes.go:81` (`protected.With(anyRole).Get("/api/domains", ...)`); `TestListDomains_AnyRole_200IncludesCreated` e `TestListDomains_NoAuth_401` em `internal/api/domains_handler_test.go:141-181` — round-trip HTTP real contra Postgres real, cria via POST e confirma presença via GET, e confirma 401 sem `Authorization` | PASS |
| I11 | `GET /api/status-pages` 200 no formato de `POST`; qualquer papel; 401 sem sessão | `internal/api/status_pages_handler.go:87-108` (`List`); `internal/db/status_page_repository.go:72` (`List`); rota `internal/cli/routes.go:83`; `TestListStatusPages_AnyRole_200IncludesCreated`/`TestListStatusPages_NoAuth_401` em `internal/api/status_pages_handler_test.go:207-256` — mesmo padrão de I10, cria via POST real e confirma no GET | PASS |
| I12 | Preview por ID retorna mesmo shape `{services,incidents}` de `PublicStatusHandler.Get`; atrás de `requireAuth`; comentário `SPEC_DEVIATION`; gate de estado `published` | `internal/api/public_status_preview_handler.go:32-83` (comentário `SPEC_DEVIATION` linhas 24-31 aponta pro handler de produção via `router.HostRouter`; `Get` chama `h.inner.composeResponse` — função extraída e compartilhada com `PublicStatusHandler.Get`, `internal/api/public_status_handler.go:123-165`); gate de estado em `public_status_preview_handler.go:68-71` (`if statusPage.State != "published" { http.NotFound }`), lido fresco via `StatusPageRepository.GetByID` a cada request (`internal/db/status_page_repository.go:97-110`, sem cache); rota atrás de `RequireAuth` em `internal/cli/routes.go:84`; testes `TestPublicStatusPreview_AuthenticatedByID_200SameShapeAsProduction`, `TestPublicStatusPreview_NoAuth_401`, `TestPublicStatusPreview_DraftPage_404`, `TestPublicStatusPreview_UnknownID_404` em `internal/api/public_status_preview_handler_test.go:64-133` | PASS |
| I13 | Hooks de domains/status-pages/public-status migrados para MSW, sem `handleRoute`; testes desta etapa passam; gate `npm run test` (escopado) | `web/src/features/public-status/hooks.ts` (rewrite, chama `/api/status-pages/{id}/public-preview` e adapta pra `PublicStatusPageData`); `web/src/test/msw/handlers.ts:83-188` (handlers novos p/ GET/POST domains, GET/POST status-pages, GET public-preview); `web/src/lib/publicStatus.ts` (só tipos, `getPublicStatusPageData` removida); ver execução isolada abaixo — 6 arquivos / 17 testes 100% verdes | PASS |

### Discrimination sensor

**I12 preview gate — cache/campo errado**: uma implementação plausível-e-errada checaria um valor de estado obtido antes (ex. reusar um `StatusPage` já carregado por outra query, ou checar `TLSLastError == nil` como proxy de "publicado"). Aqui `Get` chama `h.statusPages.GetByID(r.Context(), statusPageID)` a cada requisição (sem cache) e testa exatamente `statusPage.State != "published"` (`public_status_preview_handler.go:58-71`) — o mesmo campo que `router.HostRouter` usa em produção (confirmado por leitura, não apenas por nome de teste). `TestPublicStatusPreview_DraftPage_404` cria a página sem tocar o estado (fica no default `draft` do schema) e espera 404; isso pegaria uma implementação que checasse o campo errado (ex. `tls_last_error IS NULL`) ou que tratasse "draft" como visível por engano — o teste falharia porque o handler retornaria 200 nesse caso. `TestPublicStatusPreview_UnknownID_404` cobre separadamente o caminho `ErrNotFound` do `GetByID`. Sensor não achou brecha.

**MSW handlers vs shape real do backend**: comparação campo a campo:
- Domains: Go `domainResponse{ID,Hostname,CreatedAt}` (`domains_handler.go:37-41`, JSON `id,hostname,created_at`) vs MSW `toDomainResponse` (`handlers.ts:43-45`, mesmos 3 campos, mesma ordem de chaves conceitual). Idêntico.
- Status pages: Go `statusPageResponse{ID,Name,Subdomain,DomainID,State,TLSLastError,CreatedAt}` (`status_pages_handler.go:39-46`) vs MSW `toStatusPageResponse` (`handlers.ts:47-57`) — mesmos 7 campos, incluindo `tls_last_error` nullable. MSW corretamente omite `service_ids` (comentário `handlers.ts:39-42` documenta que é um campo só de fixture, nunca devolvido pelo backend real) — confirmado: `statusPageResponse` no Go de fato não tem esse campo.
- Public preview: Go `publicStatusResponse{Services []publicServiceResponse, Incidents publicIncidentsResponse{Active,Resolved []publicIncidentResponse}}` com `publicIncidentResponse{ID,Title,Status,CreatedAt,ResolvedAt,Updates}` e `publicServiceResponse{Name,Status,LastUpdatedAt}` (`public_status_handler.go:61-89`) vs MSW `handlers.ts:180-187` — mesma árvore de chaves (`services[].{name,status,last_updated_at}`, `incidents.{active,resolved}[].{id,title,status,created_at,resolved_at,updates[].{body,created_at}}`). Uma diferença real notada e já documentada no comentário do handler (`handlers.ts:140-142`): nem o MSW nem o Go real incluem `service_names` por incidente — condiz com o gap documentado em `public-status/hooks.ts:31-34`. Nenhuma divergência estrutural não-documentada encontrada.

Um MSW handler sutilmente errado que, por exemplo, incluísse `service_ids` no corpo de `GET /api/status-pages` ou omitisse o gate de `state == "published"` no preview teria passado despercebido pelos testes de frontend (eles só reagem ao que o MSW devolve) — mas nesse caso a comparação campo-a-campo contra o Go real (feita aqui) é exatamente o que pegaria a divergência, e não achei nenhuma.

### Regressões — backend

Comandos executados:
```
go build ./...            # limpo
gofmt -l .                 # sem output (limpo)
go vet ./...                # limpo
TEST_DATABASE_URL=postgres://zeep:zeep@localhost:5434/zeep?sslmode=disable go test -tags=integration -p 1 -count=1 ./...
```
Resultado: todos os pacotes `ok` (`internal/api` 5.65s, `internal/audit`, `internal/auth`, `internal/cli`, `internal/config`, `internal/connectors/datadog`, `internal/crypto`, `internal/db`, `internal/poller` 12.75s, `internal/router`, `internal/tls`) — 100% verde, sem os flakes conhecidos (`TestDeleteAdmin_SelfRemovalAsLastOwner_409`/`TestUpdateAdminRole_SelfDemotionAsLastOwner_409`/`TestConnectDatadog_*`) surgindo sob `-p 1`, condição do prompt satisfeita.

Wiring das rotas novas confirmado em `internal/cli/routes.go:71,77,81,83,84` — `POST /api/domains`/`POST /api/status-pages` sob `writeRoles`, `GET /api/domains`/`GET /api/status-pages`/`GET /api/status-pages/{id}/public-preview` sob `anyRole`, todas atrás de `protected` (RequireAuth).

### Regressões — frontend

```
cd web && npx tsc -b --force   # limpo
cd web && npm run build         # limpo, 200 módulos
cd web && npm run test -- --run
```
Resultado: **33 falhas / 92 passando** (12 arquivos falhos de 37), reduzido de 49 na Etapa 0 — confere com o número documentado no SPEC_DEVIATION de I13. Lista completa das 33 falhas confirmada restrita a 5 áreas: `admins` (9), `incidents` (7), `integrations` (5), `poller` (4), `services` (5) — nenhuma falha fora dessas 5, todas de escopo I14-I20. Nenhuma falha em `domains`, `status-pages` ou `public-status`.

Execução isolada dos 3 diretórios desta etapa (`npx vitest run src/features/domains src/features/status-pages src/features/public-status`): **6 arquivos, 17 testes, 100% verde** — confirma que a migração p/ MSW é completa e funcional, não apenas "sem regressão no agregado".

`npm run build` também expõe evidência independente do SPEC_DEVIATION #2 (abaixo): warning do Vite — `mockData.ts is dynamically imported by AuthProvider.tsx but also statically imported by features/public-status/hooks.ts` — confirma em runtime de build que I13 reintroduziu `mockData` como import estático de produção.

### Atomicity check

Todos os 5 commits auditados com `git show --stat`:
- `dd582ad` (I10): `tasks.md`, `domains_handler.go`, `domains_handler_test.go`, `routes.go`, `domain_repository.go` — só escopo de I10.
- `a138b3c` (I11): `tasks.md`, `status_pages_handler.go`, `status_pages_handler_test.go`, `routes.go`, `status_page_repository.go` — só escopo de I11.
- `1279e14` (I12): `tasks.md`, `public_status_handler.go` (extração de `composeResponse`, prevista no "Reuses" da task), `public_status_preview_handler.go` (novo), `public_status_preview_handler_test.go` (novo), `routes.go` — só escopo de I12.
- `e74295f` (fix I12 follow-up): `public_status_preview_handler.go`, `public_status_preview_handler_test.go`, `routes.go`, `status_page_repository.go` (`GetByID` novo) — só escopo do fix, mensagem do commit descreve exatamente esse diff.
- `d387a63` (I13): `tasks.md`, `DomainsPage.test.tsx`, `PublicStatusPage.test.tsx`, `public-status/hooks.ts`, `lib/publicStatus.ts`, `test/msw/handlers.ts`, `test/setup.ts` — todos batem com o "Where" da task, exceto `test/setup.ts` que não está listado explicitamente mas é uma adição mecânica de 2 linhas (`resetDomainsAndStatusPages` no `afterEach`) diretamente necessária para os novos handlers MSW funcionarem isolados por teste — não é scope creep.

Nenhum commit varreu arquivo não relacionado. Nenhuma repetição do problema de hygiene encontrado na Etapa 0 (I9).

### SPEC_DEVIATION audit (I13, 5 itens declarados)

| # | Declarado | Confirmado no código |
|---|---|---|
| 1 | Gate `state=="published"` faltando no preview, corrigido em commit separado antes de I13 | Confirmado: `e74295f` antecede `d387a63` no log, e o gate existe em `public_status_preview_handler.go:68-71` com testes `TestPublicStatusPreview_DraftPage_404`/`_UnknownID_404` |
| 2 | `service_names` vazio por incidente (backend não expõe vínculo publicamente) | Confirmado: `public-status/hooks.ts:35-45` (`toPublicIncidentEntry`, `service_names: []` com comentário explícito) |
| 3 | `company_name`/`logo_url` ainda vêm de `mockData.companySettings`, reintroduzindo import estático de `mockData` no bundle de produção (superando parcialmente a garantia de I8) | Confirmado: `public-status/hooks.ts:3,69-70` (`import { companySettings } from "../../lib/mockData"`, import estático top-level); confirmado independentemente pelo warning do Vite no `npm run build` ("statically imported by features/public-status/hooks.ts") |
| 4 | `lib/publicStatus.ts` teve `getPublicStatusPageData` (roteador mock morto) removida, ficando só com tipos | Confirmado: arquivo atual (40 linhas) contém somente `export type`/`export interface`, nenhuma função | 
| 5 | `DomainsPage.test.tsx` ajustada pro texto real do backend em inglês (`"hostname already registered"`), gap de i18n já identificado em I7 | Confirmado: diff do commit `d387a63` mostra a troca exata de string, com comentário no próprio teste referenciando o gap de I7 |

Nenhum item encontrado como falso. Nenhuma divergência não-documentada encontrada durante a auditoria (a comparação campo-a-campo do sensor de discriminação não achou nada além do que já está listado aqui).

### Gaps encontrados

Nenhum bloqueante. Um ponto de atenção não-bloqueante, não declarado como SPEC_DEVIATION mas também não é regressão desta etapa:

1. **[Não-bloqueante]** `StatusPagesPage.test.tsx` gera warnings do MSW (`intercepted a request without a matching request handler: GET /api/services`) durante 3 dos seus testes — a página de status pages já faz fetch de serviços (provavelmente para o seletor de vínculo), mas não há handler MSW pra `/api/services` ainda (escopo de I15+). Os testes passam porque a página degrada bem (a chamada falha silenciosamente sem quebrar a asserção), mas o warning indica uma dependência cross-feature que só vai ficar 100% limpa quando I15 (services) também migrar para MSW. Recomendação: nenhuma ação agora — apenas registrar para não ser confundido com regressão nas próximas etapas.

## 2026-08-21 — Etapa 2 (I14-I15: Datadog SearchSLOs + wiring integrations/services)

**Verdict: PASS**

**Verificador**: independente do autor. Evidência re-derivada de código/testes/execução real (build, testes Go e frontend, mutação de comportamento), não do self-report de `tasks.md`.

**Commits verificados**:
- `79be5dc` feat(admin-frontend): add Datadog SearchSLOs + GET /api/integrations/datadog/slos (I14)
- `f6a5ec3` feat(admin-frontend): wire integrations + services to real Datadog/services API (I15)

### Per-task evidence

| Task | Done-when | Evidence | Status |
|---|---|---|---|
| I14 | `SearchSLOs` retorna `[]SLOSummary{id,name}`; reaproveita `sloSearchPath` | `internal/connectors/datadog/client.go:175-196` — `func (c *Client) SearchSLOs(ctx, query string) ([]SLOSummary, error)`, monta `endpoint` com `sloSearchPath` (linha 176, mesma constante de `FetchSLOStatus`), decodifica `sloSearchResponse` e retorna slice vazio (não erro) quando não há match | PASS |
| I14 | `GET /api/integrations/datadog/slos?query=` 200 com lista, 401 sem sessão | `internal/api/integrations_handler.go:149-188` — `SearchSLOs` handler decripta `EncryptedAPIKey`/`EncryptedAppKey` via `crypto.Decrypt` (linhas 163-174), chama `h.search` injetado, escreve 200 com `[]sloSummaryResponse` (nunca `null`, ver `writeSLOSummaries` linha 190-197); rota em `internal/cli/routes.go:92` sob `protected` (que já exige `requireAuth` → 401 sem sessão) | PASS |
| I14 | Rota gated `writeRoles`, não `anyRole` | `internal/cli/routes.go:87-92` — comentário explícito + `protected.With(writeRoles).Get("/api/integrations/datadog/slos", ...)`. Confere exatamente com o que a nota do autor em `tasks.md:426` (SPEC_DEVIATION #2) declara | PASS |
| I14 | Gate `go test ./...` + `go test -tags=integration ./...` | Executado nesta verificação (ver Regressões) — todos os pacotes `ok`, incluindo `internal/connectors/datadog` e `internal/api` | PASS |
| I15 | `integrations/hooks.ts`/`services/hooks.ts` sem mockData para dados live | `grep -n "mockData" web/src/features/integrations/hooks.ts web/src/features/services/hooks.ts web/src/features/services/ServicesSection.tsx web/src/features/integrations/IntegrationsPage.tsx` → nenhuma ocorrência. `services/hooks.ts:1-36` e `integrations/hooks.ts:1-87` usam só `apiFetch` contra paths reais (`/api/integrations/datadog/status`, `/api/integrations/datadog/slos`, `/api/services`) | PASS |
| I15 | Todos os testes de hook desta etapa passam via MSW | `npx vitest run src/features/integrations src/features/services` (ver Regressões) — 100% verde | PASS |
| I15 | Gate `npm run test` | Executado (ver Regressões) — 23 falhas, todas fora do escopo desta etapa (admins/incidents/poller, I16-I20) | PASS |

### Discrimination sensor

Executado em cópia real dos arquivos de trabalho (backup em `/private/tmp/.../scratchpad/sensor/*.bak`, restaurado após cada mutação — nunca `git stash`). `git status --porcelain` antes, entre e depois das 3 mutações mostrou sempre apenas `.specs/features/admin-frontend/validation.md` modificado (pré-existente, de uma verificação anterior não commitada) — nenhum resíduo da sessão de mutação ficou no working tree.

1. **Mutante — role gate da rota SLO search**: `internal/cli/routes.go:92` trocado de `writeRoles` para `anyRole`. Rodei `go test -tags=integration -run TestAdminRouter -p 1 ./internal/cli/...`: `TestAdminRouter_Viewer_AllWriteRoutes_403/GET_/api/integrations/datadog/slos` **falhou** (`status = 200, want 403`). Confirma que `writeRouteCases()` (`routes_test.go:216-220`) de fato exercita essa rota contra um token viewer real através do router de produção, não um router de teste isolado. Revertido; `go build ./...` e `git diff` confirmaram reversão exata.
2. **Mutante — resolução `id:` no MSW SLO search**: removido o branch `if (query.startsWith("id:"))` de `web/src/test/msw/handlers.ts:204-207` (fallback pro filtro substring, que não bate `slo-1` contra nome). Rodei `npx vitest run src/features/services/hooks.test.ts`: o teste `"serviço criado com slo_id nasce not_configured e resolve slo_name via busca por id"` **falhou** (`expected null not to be null`). Confirma que a resolução de `slo_name` via `fetchSLOName`/`id:` (SPEC_DEVIATION I15 #2) é de fato exercitada e não um efeito colateral incidental. Revertido; arquivo restaurado byte-a-byte (diff void).
3. **Mutante — enforcement de `slo_id` obrigatório no POST /api/services (MSW)**: removida a condição `|| !body.slo_id` de `handlers.ts:220`, deixando passar corpo sem `slo_id`. Rodei o mesmo arquivo de teste: `"POST /api/services sem slo_id retorna 422"` **falhou** (promise resolveu em vez de rejeitar com `ApiError`). Confirma que o teste de fato depende do enforcement declarado em SPEC_DEVIATION I15 #3, e não passaria por acaso. Revertido.

Todos os 3 sensores discriminaram corretamente (mutação → falha do teste correspondente; reversão → tree limpo).

### Regressões

```
gofmt -l .                                    # limpo, sem output
go build ./...                                # limpo
go vet ./...                                  # limpo
go test -tags=integration -p 1 -count=1 ./...  # todos ok: internal/api 5.725s, audit 0.501s,
                                                # auth 0.673s, cli 2.803s, config 0.337s,
                                                # connectors/datadog 0.481s, crypto 0.351s,
                                                # db 1.312s, poller 13.720s, router 0.516s, tls 0.498s
```

```
cd web && npx tsc -b --force     # limpo
cd web && npm run build           # limpo, 200 módulos, mesmo warning pré-existente de mockData
                                   # (estático em public-status/hooks.ts, documentado em I13 —
                                   # não é regressão nova de I14/I15)
cd web && npm run test -- --run   # 23 falhas / 103 passando (37 arquivos, 8 falhos)
```

As 23 falhas, listadas por arquivo:
- `admins/AdminsPage.test.tsx` (6), `admins/hooks.test.ts` (3) — escopo I17-I18 (admin management ainda não migrado)
- `incidents/IncidentDetail.test.tsx` (4), `incidents/IncidentsPage.test.tsx` (4), `incidents/hooks.test.ts` (2) — escopo I16
- `poller/PollerBanner.test.tsx` (1), `poller/PollerStatusPage.test.tsx` (2), `poller/hooks.test.ts` (1) — escopo I19/I20

Nenhuma falha em `integrations/*`, `services/*`, `domains/*`, `status-pages/*` ou `public-status/*` — confirma que I14/I15 não introduziram regressão fora do próprio escopo. Execução isolada `npx vitest run src/features/integrations src/features/services` — 100% verde, confirmando a migração MSW é completa e funcional nessas duas features.

### Atomicity check

`git show --stat 79be5dc` (I14): `tasks.md`, `internal/api/integrations_handler.go`, `internal/api/integrations_handler_test.go`, `internal/cli/routes.go`, `internal/cli/routes_test.go`, `internal/connectors/datadog/client.go`, `internal/connectors/datadog/client_test.go` — bate exatamente com o "Where" da task (`client.go`, `integrations_handler.go`, `routes.go`, modify) mais os arquivos de teste correspondentes e `routes_test.go` (adição mecânica dos novos casos em `writeRouteCases()`, não listada no "Where" mas diretamente necessária). Nenhum arquivo alheio.

`git show --stat f6a5ec3` (I15): `tasks.md`, `web/src/features/integrations/IntegrationsPage.test.tsx`, `web/src/features/integrations/hooks.ts`, `web/src/features/services/ServicesSection.tsx`, `web/src/features/services/hooks.test.ts`, `web/src/features/services/hooks.ts`, `web/src/lib/mockData.ts`, `web/src/test/msw/handlers.ts`, `web/src/test/setup.ts` — **divergência do "Where" declarado**: a task lista só `integrations/hooks.test.ts`, `services/hooks.test.ts`, `test/msw/handlers.ts` (modify), mas o commit real também toca `integrations/hooks.ts`, `services/hooks.ts`, `ServicesSection.tsx`, `mockData.ts`, `IntegrationsPage.test.tsx` e `test/setup.ts`. Isso **não é scope creep nem hygiene issue** — o "What" da própria task já descrevia essas mudanças (adaptar `useSLOSearch` pro endpoint real, resolver `slo_name`, exigir `slo_id`), e cada arquivo extra é rastreável a um dos 5 SPEC_DEVIATION points que o autor documentou. É o campo "Where" do `tasks.md` que ficou desatualizado/incompleto em relação ao "What" e ao SPEC_DEVIATION, não o commit que varreu algo indevido.

### SPEC_DEVIATION audit

**I14** (`tasks.md:426`, 3 pontos): (1) shape de `name` não validado contra Datadog real (sem credenciais) — confirmado, comentário `[Provável]` existe em `client.go:170-174`; (2) rota `writeRoles` em vez de `anyRole` — confirmado em `routes.go:87-92`; (3) integração não conectada retorna 200 com lista vazia — confirmado em `integrations_handler.go:152-156` (`errors.Is(err, db.ErrNotFound)` → `writeSLOSummaries(w, nil)`, que normaliza pra `[]`). Todos os 3 reais.

**I15** (`tasks.md:450`, 5 pontos), todos confirmados por leitura direta do diff/código:
1. Teste manual contra Datadog real não executado — sem contra-evidência possível (é uma omissão, não uma afirmação verificável), consistente com o resto da sessão.
2. `slo_name` resolvido via `id:` search — confirmado em `integrations/hooks.ts:76-86` (`fetchSLOName`) + `services/hooks.ts:17-26` (`toService`), e exercitado pelo sensor #2 acima.
3. `slo_id` obrigatório na criação — confirmado em `services/hooks.ts:38-41` (`CreateServiceInput.slo_id: string`, não opcional) + `ServicesSection.tsx` (validação client-side, não lida linha-a-linha nesta rodada mas presente no diff do commit) + MSW 422, exercitado pelo sensor #3.
4. `masked_key` sempre `undefined`, path corrigido para `/api/integrations/datadog/status` — confirmado em `integrations/hooks.ts:5-38` (comentário explícito + adaptação 404→`{connected:false}`).
5. Fixtures `svc-3`/`svc-4` corrigidas de `slo_id: null` pra SLOs reais do catálogo — confirmado via `git show f6a5ec3 -- web/src/lib/mockData.ts` (diff mostra exatamente a troca `null`→`"slo-4"`/`"slo-3"` com `slo_name` preenchido).

Nenhum item encontrado como falso ou parcialmente descrito.

### Gaps encontrados

Nenhum bloqueante.

1. **[Não-bloqueante, hygiene de spec]** O campo "Where" de I15 em `tasks.md:433` está desatualizado em relação ao que o commit `f6a5ec3` de fato tocou (falta `integrations/hooks.ts`, `services/hooks.ts`, `ServicesSection.tsx`, `mockData.ts`, `IntegrationsPage.test.tsx`, `test/setup.ts`). O "What" da task e o SPEC_DEVIATION já cobrem essas mudanças em prosa, então não é um problema de execução — é um problema de a lista estruturada "Where" não ter sido atualizada para refletir o "What" real. Recomendação: ajustar o "Where" de I15 em `tasks.md` para listar os arquivos reais, para que auditorias futuras baseadas só no campo estruturado não subestimem o escopo do commit.
