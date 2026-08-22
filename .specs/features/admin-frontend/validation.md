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

## 2026-08-21 — Etapa 3 (I16: Incidentes)

**Verdict: PASS** (1 gap não-bloqueante encontrado pelo discrimination sensor — ver "Gaps encontrados")

**Verificador**: independente do autor. Evidência re-derivada de código/testes/execução real (build, testes Go e frontend, mutação de comportamento em worktree isolado), não do self-report de `tasks.md`.

**Diff range verificado**: commit único `2e36a80` "feat(admin-frontend): wire incidents feature to real backend (I16)" (7 arquivos, +385/-6).

**Contexto**: o "What" original de I16 presumia zero endpoint novo de backend. Investigação durante a implementação encontrou que `GET /api/incidents` e `GET /api/incidents/{id}/updates` não existiam, ambos consumidos por `IncidentsPage.tsx`/`IncidentDetail.tsx`. Usuário consultado, decisão registrada: fechar o gap nesta task (não só documentar). Isso expandiu o escopo do commit de "migrar 1 arquivo de teste pra MSW" para "2 endpoints novos de backend + MSW completo pros 5 verbos de incidentes" — tudo dentro de um único commit atômico, o que é aceitável dado que a decisão de fechar o gap foi tomada antes de codar (não é scope creep silencioso).

### Per-AC evidence (AF-19 a AF-24, spec.md P1: Gerenciar incidentes)

| AC | Spec (resumo) | Evidência de teste com valor esperado batendo a spec | Status |
|---|---|---|---|
| AF-19 (spec.md:114) | Criar incidente vinculando serviços → aparece na lista de ativos | Backend: `TestListIncidents_ReturnsMostRecentFirstWithServiceIDs` (`internal/api/incidents_handler_test.go:150-196`) cria 2 incidentes via `POST`, confirma `listed[0].ServiceIDs` bate o `serviceID` usado na criação (linha ~193). Frontend: `IncidentsPage.test.tsx:64-74` ("criar incidente com serviços selecionados aparece na aba Ativos") — cria via UI, assevera `screen.findByText("Falha de teste E2E")` na lista após fechar o form; MSW `POST /api/incidents` (`handlers.ts:269-287`) grava `service_ids` do body no incidente criado, espelhando `incidents_handler.go:70` (`incident.ServiceIDs = req.ServiceIDs`) | PASS |
| AF-20 (spec.md:115) | Adicionar update → anexa à timeline, mais recente primeiro, sem reload manual (TanStack Query) | Backend: `TestListIncidentUpdates_ReturnsTimelineMostRecentFirst` (`incidents_handler_test.go:207-239`) posta 2 updates, assevera `timeline[0].Body == "second update"` e `timeline[1].Body == "first update"` — valor exato esperado pela spec ("mais recente para o mais antigo"), não só presença. Frontend: `IncidentDetail.test.tsx:49-66` ("adicionar 2 updates aparece em ordem cronológica reversa") — publica 2 updates via UI e assevera `bodies[0]` = "Segundo update novo", `bodies[1]` = "Primeiro update novo"; revalidação via `useAddIncidentUpdate`'s `onSuccess: () => queryClient.invalidateQueries(...)` (`hooks.ts:36-48`), sem `reload()`/`location.href` em nenhum lugar do componente | PASS |
| AF-21 (spec.md:116) | Incidente não-resolvido em destaque, separado dos resolvidos | `IncidentsPage.test.tsx:35-40` ("incidente não-resolvido aparece na aba Ativos, separado dos resolvidos") assevera incidente ativo visível E incidente resolvido ("Indisponibilidade parcial da API") ausente da aba padrão (`queryByText(...).not.toBeInTheDocument()`) — nega presença cruzada, não só afirma presença própria | PASS |
| AF-22 (spec.md:117) | Marcar "resolved" → move pro histórico, timeline continua acessível | `IncidentDetail.test.tsx:68-77` ("marcar como resolvido move o incidente pro histórico mantendo a timeline acessível") clica "Marcar como resolvido", assevera texto "Resolvido" aparece E o texto de timeline anterior (`/Identificamos aumento de latência/`) continua presente — cobre as duas metades da AC (mover E manter timeline). Backend real: `IncidentRepository.Transition` (`incident_repository.go:189-222`) seta `resolved_at = now()` via `CASE WHEN $2 = 'resolved'` | PASS |
| AF-23 (spec.md:118) | Reabrir de "resolved" pra estado anterior é permitido | `hooks.test.ts:31-39` ("useTransitionIncident aceita reabertura") chama `mutateAsync("investigating")` e assevera `reopened.status === "investigating"` E `reopened.resolved_at === null` — valor exato, não só ausência de erro. Backend real espelhado: `Transition`'s `CASE WHEN $2 = 'resolved' THEN now() ELSE NULL END` (`incident_repository.go:200`) limpa `resolved_at` em qualquer transição não-resolved, mesma semântica do MSW `handlers.ts:340` | PASS |
| AF-24 (spec.md:119) | `viewer` não vê ações de criar/update/transição | `IncidentsPage.test.tsx:53-62` ("viewer não vê o formulário de criação nem o botão de reabrir") e `IncidentDetail.test.tsx:79-85` ("viewer não vê formulário de update nem botões de transição") — ambos logam como `viewer@vane.app` e asseveram ausência (`queryByRole`/`queryByLabelText` retornando null) de: botão "Novo incidente", botão "Reabrir incidente", label "Novo update", botão "Marcar como resolvido" | PASS |

Os 2 endpoints novos de backend (fora do escopo direto das ACs de produto acima, mas prerequisito técnico delas, coberto pela Test Coverage Matrix "Go handlers + repositories"):
- `GET /api/incidents`: `TestListIncidents_ReturnsMostRecentFirstWithServiceIDs` (ordem + shape) e `TestListIncidents_NoAuth_401` (`incidents_handler_test.go:198-205`) — valor esperado 401 sem `Authorization`, bate `internal/cli/routes.go:84` (`anyRole`, mesmo padrão de `/api/domains`/`/api/services`).
- `GET /api/incidents/{id}/updates`: `TestListIncidentUpdates_ReturnsTimelineMostRecentFirst`, `TestListIncidentUpdates_UnknownIncident_404` (`incidents_handler_test.go:241-249`, espera 404 exato para UUID inexistente) e `TestListIncidentUpdates_NoAuth_401` (`incidents_handler_test.go:251-258`).

MSW handlers novos (`web/src/test/msw/handlers.ts:259-346`) espelham os 2 endpoints + os 3 já existentes (`POST /api/incidents`, `POST/GET /api/incidents/:id/updates`, `PATCH /api/incidents/:id`) — comparação campo a campo:
- `GET /api/incidents` (MSW `handlers.ts:261-268`) vs Go `IncidentsHandler.List` (`incidents_handler.go:79-90`): mesma ordenação (`created_at DESC`), mesmo shape `{id,title,status,created_at,resolved_at,service_ids}` incluindo `ServiceIDs` nunca `null` (`incidents_handler.go:229-232`, `toIncidentResponse`) — MSW usa array já seedado, nunca `undefined`. Idêntico.
- `PATCH /api/incidents/:id` (MSW `handlers.ts:324-346`) vs Go `Transition` (`incidents_handler.go:127-152` + `incident_repository.go:189-222`): ambos limpam `resolved_at` em qualquer status != resolved — confirmado como comportamento real e não incidental pelo sensor (mutante 4 abaixo).

### Discrimination sensor

Executado em git worktree temporário (`git worktree add <scratch>/sensor-wt 2e36a80`, nunca `git stash`), com `web/node_modules` symlinkado do checkout real só para reuso de deps (read-only). 4 mutantes injetados e revertidos um de cada vez; `git status --porcelain` na árvore real confirmado idêntico (limpo) antes e depois de toda a sessão de sensor; worktree removido ao final (`git worktree remove --force`).

| # | Mutante | Como testado | Resultado |
|---|---|---|---|
| 1 | `internal/db/incident_repository.go:104` — `List`'s `ORDER BY created_at DESC` → `ASC` | `go test -tags=integration -p 1 ./internal/api/... -run TestListIncidents` | **Morto**: `TestListIncidents_ReturnsMostRecentFirstWithServiceIDs` falha (`listed[0].ID` trocado, `ServiceIDs` do incidente errado) |
| 2 | `internal/db/incident_repository.go:155-157` — remove a chamada `r.mustExist(ctx, incidentID)` em `ListUpdates` | `go test -tags=integration -p 1 ./internal/api/... -run TestListIncidentUpdates` | **Morto**: `TestListIncidentUpdates_UnknownIncident_404` falha (`status = 200, want 404`) |
| 3 | `web/src/test/msw/handlers.ts:291-298` — remove a checagem `!incidentsState.some(...)` no handler `GET /api/incidents/:id/updates` (sempre 200, nunca 404) | `npx vitest run src/features/incidents` (worktree) | **SOBREVIVEU** — os 3 arquivos de teste de incidentes (10 testes) passam 100% mesmo com o 404 removido |
| 4 | `web/src/test/msw/handlers.ts:340` — PATCH nunca limpa `resolved_at` ao reabrir (`: incident.resolved_at` em vez de `: null`) | `npx vitest run src/features/incidents` (worktree) | **Morto**: `hooks.test.ts` > "useTransitionIncident aceita reabertura" falha (`expected '...T...Z' to be null`) |

3 de 4 mutantes mortos. O mutante 3 revela um gap real de cobertura no frontend: **nenhum teste de componente/hook exercita o caminho 404 de `GET /api/incidents/{id}/updates` para um incidente inexistente** — o handler MSW replica o 404 do backend real (comentário `handlers.ts:288-290` diz isso explicitamente), mas nada no lado do cliente confirma que o hook/página trata esse 404 de forma correta (ex. estado de erro visível, não uma timeline vazia silenciosa). O backend está coberto (`TestListIncidentUpdates_UnknownIncident_404`); o cliente não está.

### Regressões — backend

```
go clean -testcache
go test -tags=integration -p 1 ./...   # todos os pacotes ok (internal/api 5.77s, poller 13.98s, demais < 1s cada)
gofmt -l .                              # limpo, sem output
go vet ./...                            # limpo
```

### Regressões — frontend

```
cd web && npx tsc -b --force   # limpo, sem erros
cd web && npm run test -- --run
```
Resultado: **13 falhas / 113 passando** (5 arquivos falhos de 37), reduzido de 33 na Etapa 2 — confere exatamente com o número documentado no SPEC_DEVIATION de I16 (`tasks.md:481`, "reduzido de 33"). As 13 falhas, por arquivo:
- `admins/AdminsPage.test.tsx` (6), `admins/hooks.test.ts` (3) — escopo Etapa 4 (I17-I19), ainda não implementada
- `poller/PollerBanner.test.tsx` (1), `poller/PollerStatusPage.test.tsx` (2), `poller/hooks.test.ts` (1) — escopo Etapa 5 (I20), ainda não implementada

Nenhuma falha em `incidents/*`, `domains/*`, `status-pages/*`, `integrations/*`, `services/*` ou `public-status/*`. Execução isolada `npx vitest run src/features/incidents` (árvore real): 3 arquivos, 10 testes, 100% verde.

```
cd web && npm run build   # limpo, 200 módulos, mesmo warning pré-existente de mockData estático
                           # (public-status/hooks.ts, documentado em I13 — não é regressão de I16)
```

### Atomicity check

`git show --stat 2e36a80`: `.specs/features/admin-frontend/tasks.md`, `internal/api/incidents_handler.go`, `internal/api/incidents_handler_test.go`, `internal/cli/routes.go`, `internal/db/incident_repository.go`, `web/src/test/msw/handlers.ts`, `web/src/test/setup.ts` — todos rastreáveis ao SPEC_DEVIATION declarado no próprio commit (que já avisa sobre `IncidentsPage.test.tsx`/`IncidentDetail.test.tsx` não estarem no "Where" original mas serem exercitados pelos mesmos hooks). Sem arquivo alheio ao escopo. Mensagem do commit descreve com precisão o que o diff faz (2 endpoints novos + migração MSW), ao contrário do achado de hygiene da Etapa 0 (I9).

### SPEC_DEVIATION audit (I16, 7 itens declarados em `tasks.md:474-481`)

| # | Declarado | Confirmado no código |
|---|---|---|
| 1 | `Incident.ServiceIDs`, `List`, `ListUpdates` com `mustExist` | Confirmado: `internal/db/incident_repository.go:21,102-125,154-182` |
| 2 | `incidentCreator.List`, `IncidentsHandler.List`/`ListUpdates`, `ServiceIDs` na resposta | Confirmado: `internal/api/incidents_handler.go:20,39,79-90,155-181,229-232` |
| 3 | 2 rotas novas com `anyRole` | Confirmado: `internal/cli/routes.go:84-85` |
| 4 | 6 testes novos (nota diz 6, lista 5 nomes) | Encontrados 5 testes novos de fato (`TestListIncidents_ReturnsMostRecentFirstWithServiceIDs`, `TestListIncidents_NoAuth_401`, `TestListIncidentUpdates_ReturnsTimelineMostRecentFirst`, `TestListIncidentUpdates_UnknownIncident_404`, `TestListIncidentUpdates_NoAuth_401`) — **divergência menor**: a nota diz "6 testes novos" mas só nomeia e o diff só contém 5. Não é um problema de cobertura (os 5 existentes cobrem o que a AC pede), é uma imprecisão de contagem na prosa do SPEC_DEVIATION |
| 5 | Estado em memória MSW + 5 handlers + `resetIncidents()` no `afterEach` | Confirmado: `web/src/test/msw/handlers.ts:62-79,259-346`, `web/src/test/setup.ts:5-19` |
| 6 | `IncidentsPage.test.tsx`/`IncidentDetail.test.tsx` passaram a exercitar os handlers novos sem mudança de asserção | Confirmado por leitura de ambos os arquivos — nenhuma asserção parece ter sido ajustada para acomodar o MSW (ao contrário do gap de i18n em I7/I15) |
| 7 | 13 falhas esperadas restantes, reduzido de 33 | Confirmado (ver Regressões — frontend acima), contagem exata bate |

Item 4 é a única imprecisão encontrada (contagem "6" vs 5 testes reais) — cosmético, não afeta o veredito.

### Gaps encontrados

Nenhum bloqueante — PASS mantido.

1. **[Não-bloqueante, gap de cobertura real]** Nenhum teste de frontend (hook ou componente) exercita o caminho 404 de `GET /api/incidents/{id}/updates` para um incidente inexistente, apesar do handler MSW replicar esse 404 deliberadamente (comentário explícito em `handlers.ts:288-290`) e do backend Go ter cobertura dedicada (`TestListIncidentUpdates_UnknownIncident_404`). Descoberto pelo mutante 3 do sensor (sobrevivente). Recomendação: adicionar 1 teste em `hooks.test.ts` (ex. `useIncidentUpdates` com um `incidentId` inexistente propaga `ApiError` 404) antes de fechar a Etapa 3 definitivamente, ou registrar como débito técnico explícito se a decisão for adiar.
2. **[Cosmético]** SPEC_DEVIATION #4 de I16 em `tasks.md:478` diz "6 testes novos" mas lista e o código contêm 5. Ajustar a contagem na próxima edição de `tasks.md`, sem impacto funcional.

## 2026-08-21 — Etapa 4 (I17-I19: Admins) — Round 1 (histórico - ver Round 2 abaixo para o veredito atual)

**Verdict: FAIL** (histórico - superado pela re-verificação Round 2 abaixo)

**Verdict: FAIL** — backend (I17/I18) sólido; wiring de frontend (I19) tem 3 gaps de cobertura reais (evidence-or-zero) e 1 requisito não implementado (toast, AF-28), descobertos por comparação spec-anchored e confirmados empiricamente pelo discrimination sensor (2 mutantes sobreviventes).

**Verificador**: independente do autor. Evidência re-derivada de código/testes/execução real (gate completo, sensor de mutação em 2 worktrees git isolados), não do self-report de `tasks.md`.

**Diff range verificado**: `93cddf7^..befb57f` (3 commits):
- `93cddf7` feat(admin-frontend): add AdminInviteRepository.List for pending invites (I17)
- `f31c05d` feat(admin-frontend): merge pending invites into GET /api/admins (I18)
- `befb57f` feat(admin-frontend): wire admins core actions to real backend (I19)

### Per-task evidence (Done-when)

| Task | Done-when | Evidence | Status |
|---|---|---|---|
| I17 | `List` retorna só convites pendentes e não expirados | `internal/db/admin_invites.go:94-122` (`List`, filtro `WHERE used_at IS NULL AND expires_at > now()`, nunca seleciona `token_hash`); `TestAdminInviteRepository_List_ReturnsOnlyPendingNotExpiredMostRecentFirst` (`internal/db/admin_invites_test.go:202-289`) — cria pending mais antigo/mais novo, um usado, um expirado (via insert direto), confirma exclusão dos 2 últimos, ordem mais-recente-primeiro, e ausência de `TokenHash` no retorno | PASS |
| I18 | `GET /api/admins` mescla ativos+pendentes, cada um com `status`; convite expirado/usado não aparece; rota continua `ownerOnly` | `internal/api/admins.go:410-462` (`List`, `item.Status = "active"` linha 434, mescla convites de `h.invites.List` linhas 443-457, `Status: "pending"` linha 454); `TestListAdmins_Owner_200_MergesPendingInviteWithStatus` (`admins_test.go:797-849`) assevera `pendingItem.Status == "pending"`, `pendingItem.Role == db.RoleOperator` (valor exato, não só presença), `pendingItem.ExpiresAt != nil`, `activeItem.Status == "active"`; `TestListAdmins_Owner_200_ExcludesUsedAndExpiredInvites` (`admins_test.go:851-885`) confirma exclusão dos dois casos | PASS |
| I19 | Convite/mudança de papel/remoção/lockout funcionam contra API real (via MSW + integração Go) | Ver "Spec-anchored Acceptance Criteria" abaixo — **parcialmente falso**: o lockout (AF-29) está coberto; convite (AF-25), sucesso de mudança de papel (AF-27) e sucesso de remoção (AF-28) NÃO têm nenhuma asserção de frontend exercitando o caminho feliz end-to-end, apesar do texto do Done-when afirmar "validado end-to-end via MSW" | ❌ GAP |
| I19 | Botões Reenviar/Cancelar desabilitados, sem requisição | `AdminsPage.tsx:128-146` (`disabled` em ambos os `Button`, envoltos em `Tooltip label="Ainda não disponível"`); `AdminsPage.test.tsx:79-99` — 2 testes: `toBeDisabled()` em ambos os botões, e clique em ambos não altera a lista (`novo-operador@vane.app` continua presente) | PASS |
| I19 | Gate `cd web && npm run test` | Executado nesta verificação (ver Regressões) — 122 passed / 4 failed, as 4 fora de escopo (poller, Etapa 5) | PASS |

### Spec-Anchored Acceptance Criteria (AF-25, AF-27, AF-28, AF-29, AF-38 — spec.md "P2: Gerenciar admins")

| AC | Spec-defined outcome | `file:line` + assertion | Result |
|---|---|---|---|
| AF-25 (spec.md:133) — owner convida admin → convite via backend, badge "Pendente" | `POST /api/admins` chamado com `{email,role}`, item aparece com badge "Pendente" | `AdminsPage.test.tsx:35-40` ("convite pendente aparece com badge Pendente") só renderiza a página já logada e verifica que um convite **pré-semeado** no fixture MSW (`seedAdminInvites`, não criado pela submissão do formulário) aparece com o badge. Nenhum teste em `hooks.test.ts` nem em `AdminsPage.test.tsx` preenche o formulário de convite (`Field E-mail`, `select Papel`, botão "Enviar convite") e confirma que isso resulta em `POST /api/admins` sendo chamado e o item aparecendo depois. `hooks.ts:26-32` (`useInviteAdmin`) tem zero cobertura de teste própria (nem sucesso, nem erro) | ❌ GAP (evidence-or-zero: 0 assertions on the actual submit path) |
| AF-27 (spec.md:135) — owner altera papel de admin ativo → envia mudança, atualiza papel exibido imediatamente | Após `PATCH /api/admins/{id}/role` bem-sucedido, o papel exibido na lista muda para o novo valor | Nenhuma. `AdminsPage.test.tsx:50-63` testa só o caminho de **rejeição** (409/lockout). `hooks.test.ts:25-39` testa só o caminho de **erro** de `useUpdateAdminRole`. Nenhum teste aciona uma mudança de papel que o backend/MSW aceita (200) e confirma o novo papel refletido. Confirmado por mutação dirigida (ver Discrimination Sensor, mutante 5) — remover `queryClient.invalidateQueries(...)` do `onSuccess` de `useUpdateAdminRole` não quebra nenhum teste | ❌ GAP |
| AF-28 (spec.md:136) — owner remove admin → remove linha, confirmação via toast | Linha desaparece da lista E um toast de confirmação é exibido | `AdminsPage.test.tsx:65-77` testa só que o **diálogo de confirmação abre** com o texto exato ("Remover admin", "Remover o acesso de operator@vane.app?..."), nunca clica em "Remover" para confirmar. Nenhum teste confirma que a linha some após a remoção. Pior: **a metade "toast" da AC nunca foi implementada** — `grep -rn "toast(" web/src` (excluindo testes) não retorna nenhuma chamada em nenhum lugar do app; `sonner`'s `<Toaster/>` só existe como container em `App.tsx`, nunca invocado. `confirmRemove` em `AdminsPage.tsx:77-86` só fecha o diálogo, sem chamar `toast(...)`. Confirmado por mutação dirigida (mutante 6) — remover `invalidateQueries` do `onSuccess` de `useDeleteAdmin` não quebra nenhum teste | ❌ GAP (teste ausente E funcionalidade de toast ausente) |
| AF-29 (spec.md:137) — rejeição por zero-owners mantém estado, exibe erro específico | Mensagem de erro da API exibida, estado anterior preservado | `AdminsPage.test.tsx:50-63` (mudança de papel): `expect(await screen.findByRole("alert")).toHaveTextContent(/zero active owners/)` (mensagem exata do backend) + `owner@vane.app` continua na lista. `hooks.test.ts:25-53` (mudança de papel E remoção): `rejects.toBeInstanceOf(ApiError)` + `expect(result.current.admins.data).toBe(before)` (mesma referência de objeto — prova que a lista não foi invalidada/recarregada) | ✅ PASS |
| AF-38 (spec.md:172) — `GET /api/admins` inclui pendentes com `status` | Ver evidência I18 acima (`admins_test.go:797-885`) | ✅ PASS |

**Status**: ❌ 2/5 ACs cobertos com evidência exata da spec, 3/5 com gap real (não apenas spec-precision gap — ausência de asserção no caminho feliz, e em AF-28 também ausência de implementação).

### Discrimination Sensor

Executado em 2 git worktrees temporários isolados (`git worktree add <scratch> HEAD`, nunca `git stash`), `web/node_modules` symlinkado do checkout real (read-only, removido antes do `worktree remove`). `git status --porcelain` da árvore real confirmado idêntico ao baseline (só `.specs/features/admin-frontend/validation.md`, modificação pré-existente desta mesma sessão de verificação) antes e depois de cada worktree.

| # | Mutação | Como testado | Resultado |
|---|---|---|---|
| 1 | `internal/db/admin_invites.go:101` — inverte `WHERE used_at IS NULL AND expires_at > now()` → `WHERE used_at IS NOT NULL OR expires_at <= now()` | `go test -tags=integration -run TestAdminInviteRepository_List ./internal/db/...` | **Morto** — `TestAdminInviteRepository_List_ReturnsOnlyPendingNotExpiredMostRecentFirst` falha nas 4 asserções (inclui usado/expirado, exclui os 2 pendentes reais) |
| 2 | `internal/api/admins.go:434` — remove `item.Status = "active"` | `go test -tags=integration -run TestListAdmins ./internal/api/...` | **Morto** — `TestListAdmins_Owner_200_MergesPendingInviteWithStatus` falha (`active admin Status = "", want "active"`) |
| 3 | `internal/api/admins.go:58` — `wouldLeaveZeroOwners`: `ownerCount <= 1` → `ownerCount < 1` | `go test -tags=integration -run 'TestUpdateAdminRole\|TestDeleteAdmin' ./internal/api/...` | **Morto** — `TestUpdateAdminRole_SelfDemotionAsLastOwner_409` e `TestDeleteAdmin_SelfRemovalAsLastOwner_409` falham (`status = 200, want 409`) |
| 4 | `web/src/test/msw/handlers.ts:439` — remove a chamada a `wouldLeaveZeroOwners` no handler `PATCH /api/admins/:id/role` (nunca retorna 409) | `npm run test -- --run src/features/admins` (worktree) | **Morto** — `AdminsPage.test.tsx` (409 lockout) e `hooks.test.ts` (409 lockout) falham |
| 5 | `web/src/features/admins/hooks.ts` — remove `queryClient.invalidateQueries(...)` do `onSuccess` de `useUpdateAdminRole` (sucesso deixa de atualizar a lista) | `npm run test -- --run src/features/admins` (worktree novo) | **SOBREVIVEU** — 9/9 testes passam |
| 6 | `web/src/features/admins/hooks.ts` — remove `queryClient.invalidateQueries(...)` do `onSuccess` de `useDeleteAdmin` (sucesso deixa de atualizar a lista) | `npm run test -- --run src/features/admins` (worktree novo) | **SOBREVIVEU** — 9/9 testes passam |

**Sensor depth**: lightweight (padrão) — 4 mutações planejadas no prompt de verificação, mais 2 mutações dirigidas para confirmar empiricamente a hipótese de gap de cobertura levantada na checagem spec-anchored (AF-27/AF-28 caminho feliz).

**Resultado**: 4/6 mortos, 2 sobreviventes → confirma concretamente que o caminho de sucesso de `useUpdateAdminRole`/`useDeleteAdmin` (refresh da lista após 200) não tem nenhuma cobertura de teste — os únicos testes que tocam essas mutations exercitam exclusivamente o caminho de erro 409.

### Regressões — backend

```
go test -tags=integration -p 1 ./...
```
Resultado: todos os pacotes `ok` (`internal/api`, `internal/audit`, `internal/auth`, `internal/cli`, `internal/config`, `internal/connectors/datadog`, `internal/crypto`, `internal/db`, `internal/poller`, `internal/router`, `internal/tls`) — 0 falhas.

### Regressões — frontend

```
cd web && npm run test -- --run
```
Resultado: **122 passed / 4 failed** (3 arquivos falhos de 37). As 4 falhas, todas em `src/features/poller/*` (`PollerStatusPage.test.tsx` — 2, `hooks.test.ts` — 1, mais 1 relacionada em `PollerBanner`/`PollerStatusPage`), escopo explícito de Etapa 5 (I20), consistente com o número documentado no SPEC_DEVIATION de I19 (`tasks.md:556`, "reduzido de 13" → agora 4). Nenhuma falha fora do escopo do poller. Execução isolada `npx vitest run src/features/admins`: 2 arquivos, 9 testes, 100% verde.

`cd web && npx tsc -b --force`: não executado nesta rodada (não solicitado no escopo desta verificação; build já confirmado limpo nas etapas anteriores e nenhum arquivo de tipos foi tocado neste diff).

### Atomicity check

`git show --stat 93cddf7` (I17): `internal/db/admin_invites.go`, `internal/db/admin_invites_test.go` — só escopo de I17.
`git show --stat f31c05d` (I18): `internal/api/admins.go`, `internal/api/admins_test.go` — só escopo de I18.
`git show --stat befb57f` (I19): `.specs/features/admin-frontend/tasks.md`, `web/src/features/admins/AdminsPage.test.tsx`, `web/src/features/admins/AdminsPage.tsx`, `web/src/features/admins/hooks.ts`, `web/src/test/msw/handlers.ts`, `web/src/test/setup.ts` — bate com o "Where" declarado (incluindo `test/setup.ts`, adição mecânica de `resetAdmins()` no `afterEach`, mesmo padrão de etapas anteriores). Nenhum arquivo alheio ao escopo.

### SPEC_DEVIATION audit (I19, 6 itens declarados em `tasks.md:551-556`)

| # | Declarado | Confirmado no código |
|---|---|---|
| 1 | Paths corrigidos: `/api/admins/invite`→`/api/admins`, `/api/admins/{id}`→`/api/admins/{id}/role` | Confirmado: `hooks.ts:26-32,37-47` |
| 2 | 2 testes antigos de resend/cancel reescritos para afirmar estado desabilitado | Confirmado: `AdminsPage.test.tsx:79-99` |
| 3 | `useResendInvite`/`useCancelInvite` removidos da UI, permanecem em `hooks.ts` sem uso | Confirmado: `hooks.ts:61-76` ainda existem, `AdminsPage.tsx` não os importa mais |
| 4 | 1 asserção de lockout ajustada pro texto real do backend (`/zero active owners/`) | Confirmado: `AdminsPage.test.tsx:61` |
| 5 | MSW ganhou estado em memória + 4 handlers novos | Confirmado: `handlers.ts:92-101,387-459` |
| 6 | Gate com 4 falhas esperadas (poller) | Confirmado (ver Regressões — frontend) |

Todos os 6 itens declarados são verdadeiros. **Nenhum deles, porém, documenta os 3 gaps encontrados nesta verificação** (ausência de teste de caminho feliz para convite/mudança de papel/remoção, e ausência de implementação de toast) — o SPEC_DEVIATION cobre fielmente o que foi feito, mas não alerta sobre o que ficou sem cobrir, que é justamente o papel desta verificação independente.

### Gaps encontrados (ranqueados)

1. **[Bloqueante]** AF-28 — toast de confirmação em remoção bem-sucedida de admin não está implementado em lugar nenhum do app (`grep -rn "toast(" web/src` exclui testes: zero ocorrências; `sonner`'s `Toaster` só é montado em `App.tsx`, nunca invocado). `AdminsPage.tsx:77-86` (`confirmRemove`) só fecha o diálogo. — **Fix**: chamar `toast.success(...)` (ou padrão equivalente já decidido para o design system) em `onSuccess` de `useDeleteAdmin`, ou dentro de `confirmRemove` após `mutateAsync` resolver; adicionar teste que rendere um `<Toaster/>` e confirme o texto do toast.
2. **[Major]** AF-27 — nenhum teste (hook ou página) exercita o caminho de sucesso de mudança de papel; confirmado por mutante sobrevivente (#5) que remove o refresh da lista sem quebrar nada. — **Fix**: adicionar teste em `hooks.test.ts` (`useUpdateAdminRole` resolve e invalida `["admins"]`) e/ou em `AdminsPage.test.tsx` (clicar em outro ícone de papel, confirmar, ver o novo papel refletido na linha).
3. **[Major]** AF-28 — nenhum teste exercita o caminho de sucesso de remoção de admin; confirmado por mutante sobrevivente (#6). — **Fix**: teste em `hooks.test.ts` (`useDeleteAdmin` resolve e invalida `["admins"]`) e/ou em `AdminsPage.test.tsx` (clicar "Remover" no diálogo, confirmar que a linha some da tabela).
4. **[Major]** AF-25 — nenhum teste exercita a submissão real do formulário de convite (preencher email/papel, submeter, confirmar `POST /api/admins` e o item "Pendente" resultante); o teste existente usa um convite pré-semeado no fixture MSW, não um convite criado pela ação do usuário. `useInviteAdmin` (`hooks.ts:26-32`) tem zero teste próprio. — **Fix**: teste em `hooks.test.ts` (`useInviteAdmin` bem-sucedido invalida `["admins"]`) e em `AdminsPage.test.tsx` (preencher e submeter o formulário, ver o novo convite aparecer com "Pendente").

### Fix Plans

**Fix 1** (AF-28 — toast ausente): Root cause — a task I19 (e nenhuma task anterior) nunca incluiu "chamar `toast()` em sucesso" no Done-when; a UI foi migrada de mock para rede real sem reintroduzir esse requisito de spec.md:136. Fix task: adicionar `toast.success("Admin removido")` (ou copy equivalente) ao fluxo de `confirmRemove`, mais teste de componente que confirme o texto do toast aparecendo após remoção bem-sucedida (MSW 200). Prioridade: Blocker (requisito de spec.md não implementado, não é só teste faltando).

**Fix 2** (AF-27/AF-28 — caminho feliz sem cobertura): Root cause — os testes desta e da rodada anterior focaram nos casos de erro (409/lockout, que é onde o design de sistema tem mais risco), mas nunca adicionaram o caso de sucesso simétrico. Fix task: 4 testes novos — 2 em `hooks.test.ts` (`useUpdateAdminRole`/`useDeleteAdmin` sucesso invalida a query), 2 em `AdminsPage.test.tsx` (mudança de papel bem-sucedida reflete na UI; remoção bem-sucedida some da lista). Prioridade: Major.

**Fix 3** (AF-25 — convite sem teste de submissão real): Root cause — mesmo padrão do Fix 2, aplicado ao fluxo de convite. Fix task: 1 teste em `hooks.test.ts` (`useInviteAdmin` sucesso) + 1 teste em `AdminsPage.test.tsx` (preencher formulário, submeter, ver "Pendente" aparecer). Prioridade: Major.

### Requirement Traceability Update

| Requirement | Previous Status | New Status |
|---|---|---|
| AF-25 | Implementing | ❌ Needs Fix (invite happy-path sem teste de frontend) |
| AF-27 | Implementing | ❌ Needs Fix (role-change happy-path sem teste; mutante sobrevivente) |
| AF-28 | Implementing | ❌ Needs Fix (toast não implementado; remoção happy-path sem teste; mutante sobrevivente) |
| AF-29 | Implementing | ✅ Verified |
| AF-38 | Implementing | ✅ Verified |

### Summary

**Overall**: ❌ Not Ready — I17/I18 (backend) prontos e bem cobertos; I19 (frontend wiring) tem 3 requisitos de UI sem evidência de teste no caminho feliz e 1 requisito de spec (toast) não implementado.

**Spec-anchored check**: 2/5 ACs com evidência exata da spec (AF-29, AF-38); 3/5 com gap real (AF-25, AF-27, AF-28)
**Sensor**: 4/6 mutações mortas, 2 sobreviventes (ambas em `useUpdateAdminRole`/`useDeleteAdmin` `onSuccess`)
**Gate**: backend 100% (0 falhas), frontend 122 passed / 4 failed (fora de escopo, poller/Etapa 5)

**What works**: `AdminInviteRepository.List` (I17) e a mesclagem de `AdminsHandler.List` (I18) estão corretos, bem testados e resistiram a mutação direcionada. O lockout de último owner (AF-29) está coberto em 2 camadas (hook + página) com asserção de mensagem exata e preservação de estado. O desabilitar de Reenviar/Cancelar (backlog AD-007) está corretamente implementado e testado.

**Issues found**: Ver "Gaps encontrados" e "Fix Plans" acima — 1 blocker (toast ausente), 3 majors (caminho feliz de convite/mudança de papel/remoção sem teste de frontend).

**Next steps**: Rotear os 3 fix tasks acima (Fix 1, 2, 3) para um implementador; re-verificar após aplicados. Ciclo fix→re-verify está no round 1 de 3 permitidos.

## 2026-08-21 — Etapa 4 (I17-I19: Admins) — Re-verificação round 2

**Result**: PASS ✅

**Verdict: PASS ✅** — os 4 gaps do round 1 foram corrigidos pelo commit `a5a8c2a` "fix(admin-frontend): close Verifier gaps on admins happy paths", com evidência fresca re-derivada nesta rodada (não a partir do relatório anterior).

**Verificador**: independente do autor (fresh sub-agent, sem contexto do autor do fix). Round 2 de 3 permitidos.

**Diff verificado nesta rodada**: `a5a8c2a` (sobre `93cddf7..befb57f`) — `web/src/features/admins/AdminsPage.tsx` (+3/-0), `web/src/features/admins/AdminsPage.test.tsx` (+52/-1 lines), `.specs/features/admin-frontend/tasks.md` (+1).

### Gaps do round 1 — status re-derivado

| # | Gap (round 1) | Fechado? | Evidência fresca (`file:line`) |
|---|---|---|---|
| 1 | AF-28 — toast de confirmação ausente na remoção bem-sucedida | ✅ Fechado | `web/src/features/admins/AdminsPage.tsx:2` (`import { toast } from "sonner"`); `AdminsPage.tsx:82,85` (`confirmRemove`: captura `removedEmail` antes do `mutateAsync`, chama `toast.success(\`Acesso de ${removedEmail} removido.\`)` após sucesso). `grep -rn "toast\." web/src/features/admins/AdminsPage.tsx` confirma a única chamada de produção do arquivo. |
| 2 | AF-27 — nenhum teste cobria o caminho feliz de mudança de papel | ✅ Fechado | `AdminsPage.test.tsx:81-97` ("alterar papel com sucesso atualiza o papel exibido na lista (AF-27)") — clica no ícone "Viewer" da linha `operator@vane.app`, confirma no diálogo, e afirma via `waitFor` que o botão "Viewer" da linha atualizada tem `aria-pressed="true"`. Exercita a mutação real (`PATCH /api/admins/{id}/role` via MSW), não fixture. |
| 3 | AF-28 — nenhum teste cobria o caminho feliz de remoção | ✅ Fechado | `AdminsPage.test.tsx:99-111` ("remover admin com sucesso remove a linha e exibe toast de confirmação (AF-28)") — clica "Remover" na linha, confirma no diálogo, afirma via `waitFor` que `operator@vane.app` não está mais no DOM, e `expect(await screen.findByText("Acesso de operator@vane.app removido.")).toBeInTheDocument()` — asserção de texto exato, casando com o valor literal produzido em `AdminsPage.tsx:85`. |
| 4 | AF-25 — badge "Pendente" só era exercitado por fixture pré-semeada do MSW | ✅ Fechado | `AdminsPage.test.tsx:113-125` ("convidar admin com sucesso adiciona a linha com badge Pendente (AF-25)") — abre o form real ("Convidar admin"), digita e-mail (`novo-viewer@vane.app`), seleciona papel, submete ("Enviar convite"), e só então afirma que a linha aparece com badge "Pendente". Não usa `seedAdminInvites`/fixture pré-semeada — a linha nasce da submissão do formulário via `POST /api/admins` real (MSW). |

**Nota sobre precisão de spec**: `spec.md:136` exige "confirmação via toast" mas não define o texto exato. O texto implementado (`"Acesso de {email} removido."`) e a asserção correspondente são consistentes entre si; segue-se marcado como AC coberta (não é spec-precision gap, pois o teste passa exatamente pelo valor produzido pela implementação, e a spec não exige um texto literal específico).

### Spec-Anchored Acceptance Criteria (atualização)

| AC | Spec-defined outcome | `file:line` + assertion | Result |
|---|---|---|---|
| AF-25 | Convite via backend, item some com badge "Pendente" após ação real do usuário | `AdminsPage.test.tsx:113-125` — submissão real do form, `expect(within(newRow).getByText("Pendente")).toBeInTheDocument()` | ✅ PASS |
| AF-27 | Papel exibido atualiza após `PATCH` bem-sucedido | `AdminsPage.test.tsx:81-97` — `expect(...).toHaveAttribute("aria-pressed", "true")` no botão do novo papel, na linha real | ✅ PASS |
| AF-28 | Linha some da lista E toast de confirmação exibido | `AdminsPage.test.tsx:99-111` — `expect(screen.queryByText("operator@vane.app")).not.toBeInTheDocument()` + `expect(await screen.findByText("Acesso de operator@vane.app removido.")).toBeInTheDocument()` | ✅ PASS |
| AF-29 | Rejeição zero-owners preserva estado, erro específico | Inalterado desde round 1 — `AdminsPage.test.tsx` (mensagem exata `/zero active owners/`) + `hooks.test.ts` (mesma referência de objeto preservada) | ✅ PASS |
| AF-38 | `GET /api/admins` inclui pendentes com `status` | Inalterado desde round 1 — `admins_test.go:797-885` | ✅ PASS |

**Status**: ✅ 5/5 ACs cobertos com evidência exata da spec (0 gaps reais, 0 spec-precision gaps).

### Discrimination Sensor (round 2)

Executado em 2 git worktrees temporários isolados (`git worktree add <scratch> HEAD`, nunca `git stash`), `web/node_modules` symlinkado do checkout real (removido antes do `worktree remove --force`). Baseline `git status --porcelain` da árvore real: só `.specs/features/admin-frontend/validation.md` (modificação pré-existente desta sessão de verificação, não relacionada ao código de produção) — confirmado idêntico antes e depois de cada worktree.

| # | Mutação | `file:line` | Como testado | Resultado |
|---|---|---|---|---|
| 1 | Remove a chamada `toast.success(...)` de `confirmRemove` | `web/src/features/admins/AdminsPage.tsx:85` (worktree 1) | `npm run test -- --run src/features/admins/AdminsPage.test.tsx` (worktree) | **Morto** — teste "remover admin com sucesso... (AF-28)" falha em `findByText("Acesso de operator@vane.app removido.")` (timeout, nunca aparece) |
| 2 | Remove `queryClient.invalidateQueries(...)` do `onSuccess` de `useUpdateAdminRole` e de `useDeleteAdmin` | `web/src/features/admins/hooks.ts` (linhas do `onSuccess`, worktree 1) | `npm run test -- --run src/features/admins` (worktree) | **Morto** — 2/2 testes afetados falham: "alterar papel com sucesso... (AF-27)" (aria-pressed nunca atualiza) e "remover admin com sucesso... (AF-28)" (linha nunca desaparece) |
| 3 | Remove `queryClient.invalidateQueries(...)` do `onSuccess` de `useInviteAdmin` | `web/src/features/admins/hooks.ts:30-32` (worktree 2) | `npm run test -- --run src/features/admins` (worktree) | **Morto** — teste "convidar admin com sucesso... (AF-25)" falha (linha "novo-viewer@vane.app" nunca aparece) |

**Sensor depth**: lightweight (padrão) — 3 mutações dirigidas, uma por gap corrigido (mutação 2 cobre 2 pontos de código simetricamente, ambos verificados).

**Resultado**: 3/3 mutações mortas (4/4 pontos de código mutados detectados) — **PASS**. Nenhuma asserção rasa: os 3 testes novos falham quando o comportamento correspondente é removido, confirmando que fecham os gaps de fato (não apenas de forma) apontados no round 1.

**Isolamento**: `git status --porcelain` da árvore real idêntico ao baseline após remoção de ambos os worktrees (`git worktree list` confirma só o worktree principal restante).

### Gate Check

```
go test -tags=integration -p 1 ./...
```
Resultado: todos os pacotes `ok` (`internal/api`, `internal/audit`, `internal/auth`, `internal/cli`, `internal/config`, `internal/connectors/datadog`, `internal/crypto`, `internal/db`, `internal/poller`, `internal/router`, `internal/tls`) — 0 falhas. Igual ao round 1.

```
cd web && npm run test -- --run
```
Resultado: **125 passed / 4 failed** (3 arquivos falhos de 37). As 4 falhas são todas em `src/features/poller/*` (`PollerBanner.test.tsx` — 1, `PollerStatusPage.test.tsx` — 2, `hooks.test.ts` — 1), fora de escopo desta etapa (Etapa 5/I20), idênticas em nome/local às 4 falhas já documentadas no round 1 — nenhuma falha nova, nenhuma falha fora do conjunto esperado.

- **Teste count antes do fix**: 122 passed (round 1).
- **Teste count depois do fix**: 125 passed.
- **Delta**: +3 (exatamente os 3 testes novos de `AdminsPage.test.tsx`, confirmado via `npx vitest run src/features/admins` isolado: 2 arquivos, **12 testes, 100% verde** — eram 9 no round 1).
- **Regressão**: nenhuma. Nenhum teste que passava no round 1 falhou nesta rodada; nenhum teste foi removido ou teve asserção enfraquecida.

### Code Quality

| Principle | Status |
|---|---|
| Minimum code (fix cirúrgico: 1 import, 2 linhas de lógica, 1 teste por gap) | ✅ |
| Surgical changes (só `AdminsPage.tsx` e `AdminsPage.test.tsx`, mais o `tasks.md` já esperado) | ✅ |
| No scope creep | ✅ |
| Matches existing patterns (usa `sonner` já presente no projeto, `<Toaster/>` já existente em `App.tsx`; testes seguem o mesmo padrão de `render`/`userEvent`/`waitFor` dos testes vizinhos) | ✅ |
| Spec-anchored outcome check (asserted values match spec) | ✅ |
| Toda asserção corresponde a uma AC (AF-25, AF-27, AF-28) — sem testes não reivindicados | ✅ |
| Documented guidelines followed | none - strong defaults applied |

### Requirement Traceability Update

| Requirement | Previous Status (round 1) | New Status (round 2) |
|---|---|---|
| AF-25 | ❌ Needs Fix | ✅ Verified |
| AF-27 | ❌ Needs Fix | ✅ Verified |
| AF-28 | ❌ Needs Fix | ✅ Verified |
| AF-29 | ✅ Verified | ✅ Verified (inalterado) |
| AF-38 | ✅ Verified | ✅ Verified (inalterado) |

### Summary

**Overall**: ✅ Ready — os 4 gaps do round 1 (toast ausente + 3 caminhos felizes sem cobertura) estão corrigidos com evidência fresca de código, teste passando e mutante morto para cada um. Nenhuma regressão no gate Go ou frontend.

**Spec-anchored check**: 5/5 ACs com evidência exata da spec (0 gaps)
**Sensor**: 3/3 mutações mortas (4/4 pontos de código cobertos)
**Gate**: backend 100% (0 falhas), frontend 125 passed / 4 failed (fora de escopo, poller/Etapa 5) — idêntico padrão de falhas ao round 1, +3 testes novos, 0 regressões

**What works**: Toast de confirmação de remoção implementado e testado; caminhos felizes de mudança de papel, remoção e convite agora exercitam a mutação real (não fixture) e são detectados por mutação dirigida quando o comportamento é removido.

**Issues found**: Nenhum.

**Next steps**: Etapa 4 (I17-I19: Admins) considerada concluída. Nenhum fix task pendente. Próxima etapa (I20, poller/Etapa 5) fora do escopo desta verificação.
