# Admin Frontend Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/admin-frontend/design.md`
**Status**: Draft

> **Nota de execução (2026-08-20, ver AD-006 em `.specs/STATE.md`)**: aquela rodada seguiu ordem invertida — frontend primeiro, contra mock layer (`web/src/lib/mockData.ts` + `apiClient.ts`), sem rede real. UI completa (T9-T34 abaixo, seção "Historical Task Breakdown"), 119 testes, build limpo — mas 100% mockada.
>
> **Nota de execução (2026-08-21, ver AD-007 em `.specs/STATE.md`)**: esta rodada trata a integração real com o backend Go, dividida em 6 etapas sequenciais e testáveis isoladamente (Etapa 0 a 5, ver `## Execution Plan` e `## Integration Task Breakdown` abaixo). Substitui T1-T8 (backend pendente) e a parte de rede real de T13/T14/T18/T21/T24/T28/T31/T33 (que hoje apontam pro mock) por tasks novas I1-I20. As tasks T9-T34 originais (construção de UI) permanecem como registro histórico do que já foi implementado — não precisam ser refeitas, só re-conectadas à API real onde a task de integração correspondente instruir. 2 gaps descobertos durante a investigação desta rodada (admin invite resend/cancel; company settings) ficam fora de escopo, registrados como backlog em AD-007.

---

## Test Coverage Matrix

> Generated from codebase sampling (`internal/api/*_test.go`, `internal/db/*_test.go`, `internal/connectors/datadog/client_test.go`, zeep-orbit's `internal/dashboard/ui`) plus spec ACs. No `AGENTS.md`/lint-config guideline files found in this repo — coverage expectations use the strong default (1:1 to spec ACs, every listed edge case). Frontend layer confirmed with user: Vitest + Testing Library (component/unit), no e2e in this MVP (already Out of Scope in spec.md).

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| Go handlers + repositories (new support endpoints AF-34/35/36/38/42) | integration | All branches; 1:1 to spec ACs; every status code (200/401/403/404/422) listed in Design's contract | `internal/api/*_test.go`, `internal/db/*_test.go` (tag `integration` for DB-backed cases, matching existing convention) | `go test -tags=integration ./...` |
| Go middleware — cookie auth (AF-39/40/41) | integration | Cookie path AND header path both covered; existing header-only tests in `middleware_test.go` must still pass unmodified | `internal/api/middleware_test.go`, `internal/api/auth_handler_test.go` | `go test -tags=integration ./...` |
| Go connector — Datadog SLO search (AF-42) | unit | Success (multiple results), no-match, error paths — same style as existing `FetchSLOStatus` tests | `internal/connectors/datadog/client_test.go` | `go test ./...` |
| Go SPA embed/fallback handler | unit | Static file serve + fallback-to-`index.html` for client-side routing | `internal/webui/*_test.go` | `go test ./...` |
| React hooks (TanStack Query, per resource) | unit | 1:1 to the spec ACs the resource's hooks support; loading/error/success states | `web/src/features/**/*.test.ts(x)` | `cd web && npm run test` |
| React pages/components (forms, lists, RBAC visual, empty states) | unit/component | Happy path + every listed edge case (RBAC-hide, empty state, 409/422 toast) per spec AC | `web/src/**/*.test.tsx` | `cd web && npm run test` |
| React auth/session (`AuthProvider`, route guards, `apiClient`, `SessionExpiredModal`) | unit | Every AC in "P1: Login e sessão" (AF-01 a AF-06, AF-43) | `web/src/auth/*.test.tsx`, `web/src/lib/*.test.ts` | `cd web && npm run test` |
| React design-system layer — tokens + componentes Nocturne (`Button`/`Input`/`Field`/`Tag`/`Card`/`Table`/`Dialog`/`Seg`/`IconRoleSelector`) | unit/component (smoke para tokens) | Cada componente cobre suas variantes/estados descritos no handoff (`dashboard-handoff/README.md`); tokens cobertos por smoke test de computed style | `web/src/styles/*.test.ts`, `web/src/components/ui/*.test.tsx` | `cd web && npm run test` |
| Config/scaffold (`package.json`, `vite.config.ts`, Tailwind config, `types/api.ts`) | none | Build gate only | `web/*.config.*`, `web/src/types/*.ts` | `cd web && npm run build` |
| React hooks contra rede real (integração, 2026-08-21) | unit (via MSW) | Cada hook de feature testado contra handler MSW simulando a resposta HTTP real do backend (status code + shape), não mais `handleRoute` em memória; mesma profundidade que os testes mock já tinham (happy path + erro 401/403/404/409/422 por hook) | `web/src/features/**/*.hooks.test.ts`, `web/src/test/msw/handlers.ts`, `web/src/auth/AuthProvider.test.tsx` | `cd web && npm run test` |
| Go middleware — CORS (integração, 2026-08-21) | integration | Preflight `OPTIONS` aceito pra origem do Vite dev; origem fora da allowlist rejeitada; `Access-Control-Allow-Credentials` nunca combinado com `*` | `internal/api/middleware_test.go` (ou arquivo de teste do novo middleware) | `go test -tags=integration ./...` |

## Gate Check Commands

> Generated from `Makefile` (backend: `go test ./...`, `gofmt -l .`, `go vet ./...`) and the stack decided in context.md/design.md (frontend: Vite + Vitest, `zeep-orbit/internal/dashboard/ui/package.json` as version baseline). Confirm before Execute.

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick (backend, unit only) | After a backend task with no DB-backed test | `go test ./...` |
| Quick (frontend) | After a frontend task | `cd web && npm run test` |
| Full (backend, integration) | After a backend task with a DB-backed/integration test | `go test -tags=integration ./... && gofmt -l . && go vet ./...` |
| Build (backend) | After backend phase completion | `go build ./... && gofmt -l . && go vet ./...` |
| Build (frontend) | After frontend phase completion, or after a config/scaffold-only task | `cd web && npm run build` |

---

## Execution Plan (integração real, 2026-08-21 — ver AD-007)

Esta é a execução ATIVA desta rodada: 6 etapas sequenciais, cada uma uma fatia vertical (backend + wiring de frontend + testes) shippable e testável isoladamente. Etapa 0 é bloqueante para as demais; Etapas 1-5 poderiam em teoria rodar em paralelo entre si depois da Etapa 0, mas esta rodada mantém execução sequencial simples, seguindo a convenção já usada neste arquivo. As tasks T1-T34 originais (abaixo, seção "Historical Task Breakdown") descrevem a rodada mock-first já concluída — a UI que elas descrevem já existe; as tasks I1-I20 abaixo são o trabalho real desta rodada.

### Etapa 0: Fundação — sessão via cookie, CORS, fetch real, MSW (bloqueante)

```
I1 → I2 → I3 → I4 → I5 → I6 → I7 → I8 → I9
```

### Etapa 1: Domínios + Status Pages + status page pública

```
I9 → I10 → I11 → I12 → I13
```

### Etapa 2: Integrações (Datadog) + Serviços

```
I13 → I14 → I15
```

### Etapa 3: Incidentes

```
I15 → I16
```

### Etapa 4: Admins (core, sem resend/cancel — backlog AD-007)

```
I16 → I17 → I18 → I19
```

### Etapa 5: Poller status

```
I19 → I20
```

---

## Task Breakdown (I1-I20 — integração real, esta rodada)

### I1: `GET /api/auth/me` handler

**What**: Novo handler `AuthHandler.Me` que lê o `Admin` já carregado no contexto por `RequireAuth` e retorna `{id,email,role}`; rota registrada atrás de `requireAuth`+`anyRole`.
**Where**: `internal/api/auth_handler.go` (modify), `internal/cli/routes.go` (modify)
**Depends on**: None
**Reuses**: `RequireAuth` (já injeta `Admin` no contexto — `internal/api/middleware.go:50-81`), `anyRole` (`internal/cli/routes.go:76-78`)
**Requirement**: AF-34

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `GET /api/auth/me` retorna 200 `{id,email,role}` para qualquer papel autenticado
- [x] Retorna 401 sem cookie/header válido
- [x] Gate check passes: `go test -tags=integration ./...`
- [x] `gofmt -l .` e `go vet ./...` limpos

**Tests**: integration
**Gate**: full

---

### I2: Login seta cookie de sessão

**What**: `AuthHandler.Login` passa a, além do corpo `{token}` já existente, setar `http.SetCookie` com `Name:"vane_session"`, `HttpOnly:true`, `Secure:true`, `SameSite:http.SameSiteStrictMode`, `Path:"/"`, `MaxAge` igual a `sessionTTL` (AD-004).
**Where**: `internal/api/auth_handler.go` (modify)
**Depends on**: I1
**Reuses**: `sessionTTL` já definida no arquivo
**Requirement**: AF-39

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Login bem-sucedido seta o cookie com todos os atributos corretos, corpo `{token}` inalterado
- [x] Testes existentes de login (`auth_handler_test.go`) continuam passando sem modificação
- [x] Gate check passes: `go test -tags=integration ./...`

**Tests**: integration
**Gate**: full

---

### I3: `RequireAuth` aceita cookie além do header

**What**: `bearerToken`/`RequireAuth` passam a, na ausência do header `Authorization`, ler o token do cookie `vane_session` via `r.Cookie(...)` antes de rejeitar com 401.
**Where**: `internal/api/middleware.go` (modify)
**Depends on**: I2
**Reuses**: `bearerToken`, `RequireAuth` existentes (`internal/api/middleware.go:50-89`)
**Requirement**: AF-40

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Requisição só com cookie (sem header `Authorization`) autentica com sucesso
- [x] Header continua tendo prioridade quando ambos presentes
- [x] Todos os testes existentes de `middleware_test.go` (baseados em header) continuam passando sem alteração
- [x] Gate check passes: `go test -tags=integration ./...`

**Tests**: integration
**Gate**: full

---

### I4: `POST /api/auth/logout`

**What**: Novo handler, atrás de `requireAuth` (qualquer papel), que seta o cookie `vane_session` com `MaxAge:-1` e responde 200; rota registrada.
**Where**: `internal/api/auth_handler.go` (modify), `internal/cli/routes.go` (modify)
**Depends on**: I3
**Reuses**: Mesmos atributos de cookie definidos em I2
**Requirement**: AF-41

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `POST /api/auth/logout` responde 200 e expira o cookie
- [ ] Requisição subsequente com o cookie expirado retorna 401
- [ ] Gate check passes: `go test -tags=integration ./...`

**Tests**: integration
**Gate**: full

---

### I5: Middleware de CORS restrito à origem do Vite dev

**What**: Novo middleware CORS (chi-compatível, ex. `github.com/go-chi/cors`) montado em `buildAdminRouter`, allowlist de origem única (ex. `http://localhost:5173`, configurável por env), `AllowCredentials:true`, métodos/headers necessários para `apiFetch`. Nunca `AllowedOrigins:["*"]` combinado com `AllowCredentials:true` — checar explicitamente essa combinação num teste.
**Where**: `internal/api/cors.go` (new), `internal/cli/routes.go` (modify)
**Depends on**: I4
**Reuses**: Nenhum (primeiro middleware de CORS do projeto)
**Requirement**: N/A — infraestrutura bloqueante para qualquer fetch real do frontend contra a API (ver AD-007)

**Tools**:
- MCP: NONE
- Skill: NONE (consultar doc oficial da lib de CORS escolhida antes de configurar — não inventar opções)

**Done when**:
- [ ] Preflight `OPTIONS` de origem permitida recebe `Access-Control-Allow-Origin`+`Access-Control-Allow-Credentials:true`
- [ ] Origem fora da allowlist não recebe os headers de CORS (requisição falha no browser)
- [ ] Teste explícito confirma que a config nunca combina origem wildcard com credentials
- [ ] Gate check passes: `go test -tags=integration ./...`

**Tests**: integration
**Gate**: full

---

### I6: `apiClient.ts` — troca do mock por `fetch` real

**What**: Substituir o corpo de `handleRoute` (`apiClient.ts:101-402`) por uma implementação real: `fetch(baseUrl+path, {...init, credentials:"include"})`, parse de `response.json()`, `if (!res.ok) throw new ApiError(res.status, body.error)`. Mantém a assinatura pública intacta (`apiFetch<T>`, `ApiError`, `setUnauthorizedHandler`/`triggerUnauthorized`, disparado em `res.status===401`) — nenhum hook muda. `baseUrl` vem de `import.meta.env.VITE_API_BASE_URL`.
**Where**: `web/src/lib/apiClient.ts` (rewrite), `web/vite.config.ts` (modify — expõe env var), `web/.env.development` (new)
**Depends on**: I5
**Reuses**: `ApiError`, `setUnauthorizedHandler`/`triggerUnauthorized` já existentes; assinatura de `apiFetch` já usada por todos os hooks
**Requirement**: AF-03 (401 global), AF-01 (nunca lê/escreve token)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `apiFetch` faz requisição de rede real com `credentials:"include"`, sem nenhum `handleRoute`/roteador em memória restante no arquivo
- [ ] Lança `ApiError` com `status`/`message` corretos em erro real do backend
- [ ] `triggerUnauthorized()` dispara em 401 real
- [ ] `VITE_API_BASE_URL` configurável via env, sem hardcode
- [ ] Gate check passes: `cd web && npm run build` (nenhum teste de hook ainda migrado — ver I7)

**Tests**: none (config/infra — cobertura via I7)
**Gate**: build (frontend)

---

### I7: MSW — setup + migração de `AuthProvider.test.tsx`

**What**: Instalar `msw`, criar `web/src/test/msw/server.ts` (setup do node server) + `web/src/test/msw/handlers.ts` (handlers de `/api/auth/login|logout|me`), religar `AuthProvider.test.tsx` pra rodar contra MSW em vez do `handleRoute` antigo — primeiro arquivo migrado, valida o padrão antes de replicar nos outros 10.
**Where**: `web/src/test/msw/server.ts` (new), `web/src/test/msw/handlers.ts` (new), `web/src/auth/AuthProvider.test.tsx` (modify), `web/package.json` (add devDependency)
**Depends on**: I6
**Reuses**: Nenhum (primeiro uso de MSW no projeto)
**Requirement**: N/A — pré-requisito de teste pra toda task de wiring das etapas seguintes (ver AD-007)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `AuthProvider.test.tsx` passa 100% via handler MSW, sem importar `mockData`/`handleRoute`
- [ ] Handler MSW cobre login sucesso/erro 401, `/me` autenticado/anônimo, logout
- [ ] Gate check passes: `cd web && npm run test`

**Tests**: unit (via MSW)
**Gate**: quick (frontend)

---

### I8: Isolar `setDevRole` atrás de `import.meta.env.DEV`

**What**: `setDevRole` (`AuthProvider.tsx:108-115`) hoje importa `mockData` direto pra trocar de papel sem novo login (recurso "Visualizando como" da sidebar). Envolver toda a função (e o botão que a expõe na sidebar) em `if (import.meta.env.DEV)` explícito, removendo o import de `mockData` em produção.
**Where**: `web/src/auth/AuthProvider.tsx` (modify), `web/src/layout/Sidebar.tsx` (modify — esconder o controle fora de DEV)
**Depends on**: I7
**Reuses**: Nenhum
**Requirement**: N/A — limpeza necessária antes de ligar rede real (ver AD-007)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Build de produção (`npm run build`) não inclui `mockData` no bundle (verificar via análise do output ou remoção do import condicional)
- [ ] `setDevRole`/controle "Visualizando como" só aparecem com `import.meta.env.DEV===true`
- [ ] Gate check passes: `cd web && npm run test`

**Tests**: unit
**Gate**: quick (frontend)

---

### I9: Migrar tipos de `mockData.ts` para `web/src/types/api.ts`

**What**: Extrair as interfaces hoje importadas de `mockData.ts` por hooks/páginas (`Domain`, `Admin`, `AdminInvite`, `Service`, `StatusPage`, `Incident`, `IncidentUpdate`, `PollerStatusEntry`, `CompanySettings`, `SLOSummary`, `IntegrationStatus`) para `web/src/types/api.ts`; atualizar todos os imports; `mockData.ts` deixa de ser fonte de tipos (fica só como fixture de teste, usado pelos handlers MSW das etapas seguintes se conveniente).
**Where**: `web/src/types/api.ts` (new), todos os `hooks.ts`/páginas que hoje importam tipo de `mockData.ts` (modify — só o import)
**Depends on**: I8
**Reuses**: Interfaces já existentes em `mockData.ts` (movidas, não recriadas)
**Requirement**: N/A — infraestrutura de tipos, pré-requisito de todas as etapas seguintes

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Nenhum arquivo de produção (`src/features/**`, `src/auth/**`, `src/layout/**`) importa tipo de `mockData.ts`
- [ ] `tsc -b --force` limpo
- [ ] Gate check passes: `cd web && npm run build`

**Tests**: none (refactor de tipos, sem lógica nova)
**Gate**: build (frontend)

---

### I10: `DomainRepository.List` + `GET /api/domains`

**What**: Novo método `List(ctx) ([]Domain, error)`; handler `anyRole` correspondente; rota registrada.
**Where**: `internal/db/domain_repository.go` (modify), `internal/api/domains_handler.go` (modify), `internal/cli/routes.go` (modify)
**Depends on**: I9
**Reuses**: Mesmo padrão de `ServiceRepository.List`/`IntegrationRepository.List` (`internal/api/services_handler.go:18`, `internal/api/poller_status.go:17`)
**Requirement**: AF-35, AF-37

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `GET /api/domains` retorna 200 com a lista no formato `{id,hostname,created_at}` já usado em `POST /api/domains`
- [ ] Acessível por qualquer papel autenticado, 401 sem sessão válida
- [ ] Gate check passes: `go test -tags=integration ./...`

**Tests**: integration
**Gate**: full

---

### I11: `StatusPageRepository.List` + `GET /api/status-pages`

**What**: Novo método `List(ctx) ([]StatusPage, error)`; handler `anyRole` correspondente; rota registrada.
**Where**: `internal/db/status_page_repository.go` (modify), `internal/api/status_pages_handler.go` (modify), `internal/cli/routes.go` (modify)
**Depends on**: I10
**Reuses**: Mesmo padrão de I10
**Requirement**: AF-36, AF-37

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `GET /api/status-pages` retorna 200 com a lista no formato já usado em `POST /api/status-pages`
- [ ] Acessível por qualquer papel autenticado, 401 sem sessão válida
- [ ] Gate check passes: `go test -tags=integration ./...`

**Tests**: integration
**Gate**: full

---

### I12: Endpoint dev/preview da status page pública por ID

**What**: Novo endpoint autenticado-lite (ex. `GET /api/status-pages/{id}/public-preview`, atrás de `requireAuth`+`anyRole` — não é o endpoint de produção) que reaproveita a lógica de `PublicStatusHandler` mas resolve por ID em vez de Host header, servindo só o preview interno usado por `web/src/features/public-status`. Coexiste deliberadamente com `public_status_handler.go` (produção, resolvido por `router.HostRouter`) — **SPEC_DEVIATION documentado no código**: motivo é a SPA não ter infraestrutura de host-routing em dev (AD-007).
**Where**: `internal/api/public_status_preview_handler.go` (new), `internal/cli/routes.go` (modify)
**Depends on**: I11
**Reuses**: Lógica de composição de resposta de `public_status_handler.go` (extrair função compartilhada em vez de duplicar o corpo)
**Requirement**: N/A — infraestrutura de dev/preview, decisão AD-007

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Endpoint retorna o mesmo shape `{services,incidents}` que `PublicStatusHandler.Get` produz pra hostname, mas resolvido por ID
- [ ] Atrás de `requireAuth` (não é público de verdade — só preview pra admin logado)
- [ ] Comentário `SPEC_DEVIATION` no código aponta pro endpoint real de produção e explica a coexistência
- [ ] Gate check passes: `go test -tags=integration ./...`

**Tests**: integration
**Gate**: full

---

### I13: Wiring — domains, status-pages, public-status

**What**: `domains/hooks.ts` e `status-pages/hooks.ts` já usam `apiFetch` corretamente (nenhuma mudança de lógica) — só migrar `domains/hooks.test.ts` e `status-pages/hooks.test.ts` pra MSW; `public-status/hooks.ts` passa a chamar o endpoint de I12 em vez do mock.
**Where**: `web/src/features/domains/hooks.test.ts` (modify), `web/src/features/status-pages/hooks.test.ts` (modify), `web/src/features/public-status/hooks.ts` (modify), `web/src/features/public-status/PublicStatusPage.test.tsx` (modify), `web/src/test/msw/handlers.ts` (modify — novos handlers)
**Depends on**: I12
**Reuses**: Padrão MSW estabelecido em I7
**Requirement**: AF-12, AF-13, AF-16, AF-17, AF-18

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Fluxo completo (cadastro de domínio → criação de status page → polling de TLS até estado terminal → preview da página pública) funciona contra API real em teste manual no browser
- [ ] Todos os testes de hook desta etapa passam via MSW, sem `handleRoute`
- [ ] Gate check passes: `cd web && npm run test`

**Tests**: unit (via MSW)
**Gate**: quick (frontend)

---

### I14: `SearchSLOs` + `GET /api/integrations/datadog/slos?query=`

**What**: Novo método `SearchSLOs(ctx, query string) ([]SLOSummary, error)` no client Datadog, reaproveitando `sloSearchPath` com filtro de nome livre; handler e rota `writeRoles`.
**Where**: `internal/connectors/datadog/client.go` (modify), `internal/api/integrations_handler.go` (modify), `internal/cli/routes.go` (modify)
**Depends on**: I13
**Reuses**: `sloSearchPath`, `Client.get` (mesma infra HTTP de `FetchSLOStatus`/`ValidateCredentials`)
**Requirement**: AF-42

**Tools**:
- MCP: NONE (consultar doc oficial do Datadog pra sintaxe de busca por nome — não inventar; se a sintaxe exata não estiver clara, usar o mesmo padrão de filtro por texto que `id:<sloID>` já demonstra)
- Skill: NONE

**Done when**:
- [ ] `SearchSLOs` retorna `[]SLOSummary{id,name}` (sucesso, sem resultado, erro 401/5xx)
- [ ] `GET /api/integrations/datadog/slos?query=<termo>` retorna 200 com a lista, 401 sem sessão
- [ ] Gate check passes: `go test ./...` (client) + `go test -tags=integration ./...` (handler)

**Tests**: integration (handler) + unit (client)
**Gate**: full

---

### I15: Wiring — integrações + serviços

**What**: `integrations/hooks.ts` e `services/hooks.ts` já usam `apiFetch` (Connect/Status/Services já existem no backend, sem mudança de lógica) — migrar `integrations/hooks.test.ts` e `services/hooks.test.ts` pra MSW; `useSLOSearch` passa a apontar pro endpoint real de I14.
**Where**: `web/src/features/integrations/hooks.test.ts` (modify), `web/src/features/services/hooks.test.ts` (modify), `web/src/test/msw/handlers.ts` (modify)
**Depends on**: I14
**Reuses**: Padrão MSW de I7
**Requirement**: AF-07, AF-08, AF-09, AF-10, AF-11

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Conectar Datadog (ou stub de credenciais), buscar SLO e vincular serviço funcionam contra API real em teste manual
- [ ] Todos os testes de hook desta etapa passam via MSW
- [ ] Gate check passes: `cd web && npm run test`

**Tests**: unit (via MSW)
**Gate**: quick (frontend)

---

### I16: Wiring — incidentes

**What**: `incidents/hooks.ts` já usa `apiFetch` (Create/List/AddUpdate/Transition já existem no backend, zero endpoint novo) — só migrar `incidents/hooks.test.ts` pra MSW.
**Where**: `web/src/features/incidents/hooks.test.ts` (modify), `web/src/test/msw/handlers.ts` (modify)
**Depends on**: I15
**Reuses**: Padrão MSW de I7
**Requirement**: AF-19, AF-20, AF-21, AF-22, AF-23, AF-24

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Criar incidente, adicionar update, transicionar estado e reabrir funcionam contra API real em teste manual
- [ ] Testes de hook passam via MSW
- [ ] Gate check passes: `cd web && npm run test`

**Tests**: unit (via MSW)
**Gate**: quick (frontend)

---

### I17: `AdminInviteRepository.List`

**What**: Novo método `List(ctx) ([]AdminInvite, error)` retornando convites não usados/não expirados.
**Where**: `internal/db/admin_invites.go` (modify)
**Depends on**: I16
**Reuses**: `AdminInviteRepository` existente (`GetByTokenHash`, `Create`, etc.)
**Requirement**: AF-38

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `List` retorna só convites pendentes e não expirados
- [ ] Gate check passes: `go test -tags=integration ./...`

**Tests**: integration
**Gate**: full

---

### I18: `AdminsHandler.List` — mesclar convites pendentes

**What**: `AdminsHandler.List` passa a mesclar admins ativos (`status:"active"`) com convites pendentes de I17 (`status:"pending"`) na mesma lista.
**Where**: `internal/api/admins.go` (modify)
**Depends on**: I17
**Reuses**: `List` de I17
**Requirement**: AF-38

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `GET /api/admins` retorna admins ativos + convites pendentes na mesma lista, cada item com `status`
- [ ] Convite expirado ou já usado não aparece na lista
- [ ] Rota continua restrita a `owner` (`ownerOnly`, sem mudança de middleware)
- [ ] Gate check passes: `go test -tags=integration ./...`

**Tests**: integration
**Gate**: full

---

### I19: Wiring — admins (core, sem resend/cancel)

**What**: `admins/hooks.ts` — `useAdmins`/`useInviteAdmin`/`useUpdateAdminRole`/`useDeleteAdmin` ligados ao backend real (I18 + endpoints já existentes de Invite/UpdateRole/Delete). `useResendInvite`/`useCancelInvite` **não têm backend** (backlog AD-007) — desabilitar os botões correspondentes em `AdminsPage.tsx` com tooltip/aviso explícito ("Ainda não disponível") em vez de deixá-los quebrar com 404 silencioso.
**Where**: `web/src/features/admins/hooks.test.ts` (modify), `web/src/features/admins/AdminsPage.tsx` (modify — desabilitar 2 ações), `web/src/features/admins/AdminsPage.test.tsx` (modify — cobrir estado desabilitado), `web/src/test/msw/handlers.ts` (modify)
**Depends on**: I18
**Reuses**: Padrão MSW de I7
**Requirement**: AF-25, AF-27, AF-28, AF-29

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Convite, mudança de papel, remoção e lockout de owner funcionam contra API real em teste manual
- [ ] Botões "Reenviar"/"Cancelar" convite aparecem desabilitados com aviso, não disparam requisição
- [ ] Testes de hook e de página cobrem o estado desabilitado
- [ ] Gate check passes: `cd web && npm run test`

**Tests**: unit (via MSW)
**Gate**: quick (frontend)

---

### I20: Wiring — poller status

**What**: `poller/hooks.ts` já usa `apiFetch` contra endpoint já existente (`GET /api/poller/status`) — só migrar `poller/hooks.test.ts` pra MSW.
**Where**: `web/src/features/poller/hooks.test.ts` (modify), `web/src/test/msw/handlers.ts` (modify)
**Depends on**: I19
**Reuses**: Padrão MSW de I7
**Requirement**: AF-31, AF-32, AF-33

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Banner de falha e página de detalhe do poller refletem status real em teste manual
- [ ] Teste de hook passa via MSW
- [ ] Gate check passes: `cd web && npm run test`

**Tests**: unit (via MSW)
**Gate**: quick (frontend)

---

## Historical Task Breakdown — Mock-First UI Round (completed 2026-08-20/21, ver AD-006)

> As tasks T1-T34 abaixo descrevem a rodada anterior (UI navegável contra mock, sem rede real). A UI que elas especificam já existe no código — mantidas aqui só como registro histórico do que foi construído. Não fazem parte da execução ativa desta rodada (ver `## Execution Plan` acima, tasks I1-I20). T1-T8 (backend) e a parte de rede real de T13/T14/T18/T21/T24/T28/T31/T33 foram substituídas pelas tasks I1-I20.

### T1: `GET /api/auth/me` handler

**What**: Novo handler `AuthHandler.Me` (ou handler dedicado) que lê o `Admin` já carregado no contexto por `RequireAuth` e retorna `{ id, email, role }`; monta a rota atrás de `requireAuth` + `anyRole` no arquivo de montagem de rotas do projeto.
**Where**: `internal/api/auth_handler.go` (modify)
**Depends on**: None
**Reuses**: `RequireAuth` (já injeta `Admin` no contexto — `internal/api/middleware.go`), `anyRole` já definido em `routes.go:47`
**Requirement**: AF-34

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `GET /api/auth/me` retorna 200 `{id,email,role}` para qualquer papel autenticado
- [ ] Retorna 401 sem cookie/header válido
- [ ] Gate check passes: `go test -tags=integration ./...`
- [ ] `gofmt -l .` e `go vet ./...` limpos

**Tests**: integration
**Gate**: full

---

### T2: `GET /api/domains` (listagem)

**What**: Novo método `List(ctx) ([]Domain, error)` no repositório de domínio, handler `anyRole` correspondente e rota registrada no arquivo de montagem de rotas.
**Where**: `internal/db/domain_repository.go` (modify)
**Depends on**: T1
**Reuses**: Mesmo padrão do `List` já adicionado em `IntegrationRepository` (feature `admin-dashboard`)
**Requirement**: AF-35, AF-37

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `GET /api/domains` retorna 200 com a lista no formato `{id,hostname,created_at}` já usado em `POST /api/domains`
- [ ] Acessível por qualquer papel autenticado, 401 sem sessão válida
- [ ] Gate check passes: `go test -tags=integration ./...`

**Tests**: integration
**Gate**: full

---

### T3: `GET /api/status-pages` (listagem)

**What**: Novo método `List(ctx) ([]StatusPage, error)` no repositório de status page, handler `anyRole` correspondente e rota registrada no arquivo de montagem de rotas.
**Where**: `internal/db/status_page_repository.go` (modify)
**Depends on**: T2
**Reuses**: Mesmo padrão de T2
**Requirement**: AF-36, AF-37

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `GET /api/status-pages` retorna 200 com a lista no formato já usado em `POST /api/status-pages`
- [ ] Acessível por qualquer papel autenticado, 401 sem sessão válida
- [ ] Gate check passes: `go test -tags=integration ./...`

**Tests**: integration
**Gate**: full

---

### T4: `GET /api/admins` — mesclar convites pendentes

**What**: Novo método `List(ctx) ([]AdminInvite, error)` em `AdminInviteRepository` (retorna convites não usados/não expirados); `AdminsHandler.List` passa a mesclar admins ativos (`status:"active"`) com convites pendentes (`status:"pending"`).
**Where**: `internal/db/admin_invites.go` (modify — inclui a mescla correspondente no handler de admins)
**Depends on**: T3
**Reuses**: `AdminInviteRepository` existente (`GetByTokenHash`, `Create`, etc.)
**Requirement**: AF-38

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `GET /api/admins` retorna admins ativos + convites pendentes na mesma lista, cada item com `status`
- [ ] Convite expirado ou já usado não aparece na lista
- [ ] Rota continua restrita a `owner` (`ownerOnly`, sem mudança de middleware)
- [ ] Gate check passes: `go test -tags=integration ./...`

**Tests**: integration
**Gate**: full

---

### T5: `GET /api/integrations/datadog/slos?query=` (busca de SLO)

**What**: Novo método `SearchSLOs(ctx, query string) ([]SLOSummary, error)` no client Datadog, reaproveitando `sloSearchPath` com filtro de nome livre em vez de `id:`; novo handler e rota `writeRoles` (consistente com quem cria serviço — só quem cria serviço precisa buscar SLO).
**Where**: `internal/connectors/datadog/client.go` (modify)
**Depends on**: T4
**Reuses**: `sloSearchPath`, `Client.get` (mesma infra HTTP já usada por `FetchSLOStatus`/`ValidateCredentials`)
**Requirement**: AF-42

**Tools**:
- MCP: NONE (consultar doc oficial do Datadog pra sintaxe exata de busca por nome, se a API de busca aceitar `query:<termo>` — não inventar; se a sintaxe exata não estiver clara na doc, usar o mesmo padrão de filtro por texto que `id:<sloID>` já demonstra, testado contra o schema de resposta existente)
- Skill: NONE

**Done when**:
- [ ] `SearchSLOs` retorna `[]SLOSummary{id,name}` a partir da resposta de busca do Datadog (sucesso, sem resultado, erro 401/5xx)
- [ ] `GET /api/integrations/datadog/slos?query=<termo>` retorna 200 com a lista, 401 sem sessão
- [ ] Gate check passes: `go test ./...` (client) + `go test -tags=integration ./...` (handler)

**Tests**: integration (handler) + unit (client)
**Gate**: full

---

### T6: Login seta cookie de sessão

**What**: `AuthHandler.Login` passa a, além do corpo `{token}` já existente, setar `http.SetCookie` com `Name: "vane_session"`, `Value: token`, `HttpOnly: true`, `Secure: true`, `SameSite: http.SameSiteStrictMode`, `Path: "/"`, `MaxAge` igual ao TTL do JWT (`sessionTTL`).
**Where**: `internal/api/auth_handler.go` (modify)
**Depends on**: T5
**Reuses**: `sessionTTL` já definida no arquivo
**Requirement**: AF-39

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Login bem-sucedido seta o cookie com todos os atributos corretos, corpo `{token}` inalterado
- [ ] Testes existentes de login (`auth_handler_test.go`) continuam passando sem modificação
- [ ] Gate check passes: `go test -tags=integration ./...`

**Tests**: integration
**Gate**: full

---

### T7: `RequireAuth` aceita cookie além do header

**What**: `bearerToken`/`RequireAuth` passam a, na ausência do header `Authorization`, tentar ler o token do cookie `vane_session` via `r.Cookie(...)` antes de rejeitar com 401.
**Where**: `internal/api/middleware.go` (modify)
**Depends on**: T6 (precisa do cookie já sendo setado pra testar o caminho end-to-end)
**Reuses**: `bearerToken`, `RequireAuth` existentes
**Requirement**: AF-40

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Requisição só com cookie (sem header `Authorization`) autentica com sucesso
- [ ] Header continua tendo prioridade quando ambos presentes
- [ ] Todos os testes existentes de `middleware_test.go` (baseados em header) continuam passando sem alteração
- [ ] Gate check passes: `go test -tags=integration ./...`

**Tests**: integration
**Gate**: full

---

### T8: `POST /api/auth/logout`

**What**: Novo handler, atrás de `requireAuth` (qualquer papel), que seta o cookie `vane_session` com `MaxAge: -1` e responde 200; rota registrada no arquivo de montagem de rotas.
**Where**: `internal/api/auth_handler.go` (modify)
**Depends on**: T7
**Reuses**: Mesmos atributos de cookie definidos em T6
**Requirement**: AF-41

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `POST /api/auth/logout` responde 200 e expira o cookie
- [ ] Requisição subsequente com o cookie expirado retorna 401
- [ ] Gate check passes: `go test -tags=integration ./...`

**Tests**: integration
**Gate**: full

---

### T9: Scaffold `web/` (Vite + React + TS)

**What**: Criar `web/package.json` com as mesmas dependências/versões da referência (Vite, React 18, TS, Radix, Tailwind v4, TanStack Query, react-router-dom, sonner, i18next), mais `vitest`/`@testing-library/react`/`@testing-library/jest-dom` como devDependencies; configuração de build, TypeScript e entrypoints mínimos (placeholder) no mesmo diretório.
**Where**: `web/package.json`
**Depends on**: T8
**Reuses**: `zeep-orbit/internal/dashboard/ui/package.json` como baseline de versões
**Requirement**: N/A (infraestrutura de base, não mapeada a um AF específico)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `npm install` roda sem erro em `web/`
- [ ] `npm run build` gera `web/dist/` sem erro
- [ ] `npm run test` roda (mesmo sem teste real ainda) sem erro de configuração

**Tests**: none
**Gate**: build (frontend)

---

### T10: Tokens Nocturne (Tailwind v4 `@theme` + CSS vars)

**What**: Traduzir os tokens exatos do handoff de design (`dashboard-handoff/README.md`, `dashboard-handoff/nocturne-styles.css`) para `@theme` do Tailwind v4 + CSS vars — cores (`--color-bg #161826`, `--color-surface #232532`, `--color-text #e9e9ed`, `--color-accent #9184d9`, `--color-accent-2 #a7a1db`, ramps neutral e accent 100-900, os 3 semânticos estendidos em OKLCH: `success oklch(0.72 0.135 152)`, `warning oklch(0.78 0.15 80)`, `critical oklch(0.685 0.19 25)`), type scale (Inter, h1 42px…h6 13px uppercase tracked, heading nunca mais pesado que weight 500, body 400), spacing (2.8/5.6/8.4/11.2/16.8/22.4px), radius (sm 4px/md 8px/lg 14px), shadows (sm/md/lg — `shadow-lg` de dialog usa ring `--color-divider`, não neutral-500). Não importar `nocturne-styles.css` no app real — o próprio handoff instrui recriar os valores, não linkar o arquivo.
**Where**: `web/src/styles/tokens.css` (novo) + config Tailwind v4
**Depends on**: T9
**Reuses**: valores (não o arquivo) de `dashboard-handoff/nocturne-styles.css`
**Requirement**: N/A — infraestrutura visual transversal; ver `design.md` § Visual/Design System Layer (Nocturne)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Todas as cores do token list (bg/surface/text/accent/accent-2/ramps 100-900/3 semânticos OKLCH) existem como var CSS/classe Tailwind consumível
- [ ] Type scale, spacing, radius e os 3 níveis de shadow (incluindo a variação de ring do dialog) expostos como token
- [ ] Inter carregada; nenhum heading usa weight > 500
- [ ] Teste de smoke (RTL) renderiza um elemento por token de cor e confirma computed style — garante que nada fica hardcoded fora do arquivo de tokens
- [ ] Gate check passes: `cd web && npm run test`

**Tests**: unit (smoke)
**Gate**: quick (frontend)

---

### T11: Componentes-base primitivos (`Button`, `Input`/`Field`, `Tag`, `Card`)

**What**: `Button` (variantes primary — outline accent, nunca fill sólido —, secondary — outline divider —, ghost — texto accent sem borda —, icon — 36×36 sem label —; hover tint um passo, press tint mais fundo, disabled 45% opacidade), `Input`/`Field` (label acima, 36px min-height, fundo surface, borda divider, borda accent sem outline-offset extra no focus), `Tag` (variantes accent/neutral filled + outline; os 3 tags de status semânticos via `color-mix()` tintado, nunca fill saturado), `Card` (fundo surface, radius 8px, `elev-sm/md/lg`).
**Where**: `web/src/components/ui/{Button,Input,Field,Tag,Card}.tsx`
**Depends on**: T10
**Reuses**: tokens de T10
**Requirement**: N/A — pré-requisito direto de T16 (`SessionExpiredModal`), T17 (App shell) e de toda tela de fase 4+

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `Button` cobre as 4 variantes com hover/press/disabled do handoff
- [ ] `Input`/`Field` mostra label acima e borda accent no focus
- [ ] `Tag` renderiza os 3 status semânticos via `color-mix()`, nunca fill sólido saturado
- [ ] `Card` aplica `elev-sm/md/lg` corretamente
- [ ] Teste de componente cobre cada variante + estado `disabled`
- [ ] Gate check passes: `cd web && npm run test`

**Tests**: unit (component)
**Gate**: quick (frontend)

---

### T12: Componentes-base compostos (`Table`, `Dialog`, `Seg`, `IconRoleSelector`)

**What**: `Table` (header uppercase 11px tracked; hairline rows com fade de 48px em cada borda — assinatura Nocturne, nunca borda sólida full-width), `Dialog`+backdrop sobre `Dialog` do Radix (28px padding interno, `shadow-lg` com ring `--color-divider`, prop para desabilitar dismiss por clique no backdrop — necessário pro modal de sessão expirada), `Seg` (segmented control — segmento ativo com texto accent + ring inset accent), `IconRoleSelector` (3 ícones inline-SVG shield/wrench/eye — papel atual em accent + fundo tintado, os outros 2 a 40% opacidade; dispara só `onSelect(role)`, sem confirmação embutida).
**Where**: `web/src/components/ui/{Table,Dialog,Seg,IconRoleSelector}.tsx`
**Depends on**: T11
**Reuses**: tokens de T10, `Button` de T11 (usado dentro do `Dialog`)
**Requirement**: N/A — pré-requisito direto de T16 (`SessionExpiredModal`), T29/T30 (Seg de Incidentes), T32 (`AdminsPage` — `IconRoleSelector` e `Seg` de papel), e de toda tela com tabela (T23, T25, T26, T32, T34)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `Table` renderiza o fade de 48px nas duas pontas da regra de linha
- [ ] `Dialog` aceita prop que desabilita fechar por clique no backdrop
- [ ] `Seg` mostra segmento ativo com texto accent + ring inset
- [ ] `IconRoleSelector` renderiza papel atual em accent+fundo tintado, os outros 2 a 40% opacidade, dispara `onSelect` correto por ícone
- [ ] Testes cobrem: `Dialog` sem dismiss por backdrop, `IconRoleSelector` clique dispara callback correto por ícone, `Table` renderiza linhas com fade
- [ ] Gate check passes: `cd web && npm run test`

**Tests**: unit (component)
**Gate**: quick (frontend)

---

### T13: `apiClient.ts`

**What**: Implementar `apiFetch<T>(path, init?)` — sempre `credentials:"include"`, parse de erro `{error:string}` em não-2xx lançando `ApiError{status,message}`, callback `onUnauthorized()` registrável disparado em 401.
**Where**: `web/src/lib/apiClient.ts`
**Depends on**: T12
**Reuses**: Nenhum (primeiro client HTTP do projeto)
**Requirement**: AF-03 (base pro 401 global), AF-01 (nunca lê/escreve token)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `apiFetch` inclui `credentials:"include"` em toda chamada
- [ ] Lança `ApiError` com `status`/`message` corretos em 400/401/403/404/409/422
- [ ] `onUnauthorized()` é chamado em 401 antes da rejeição
- [ ] Nenhuma linha do arquivo lê/escreve `localStorage`/`sessionStorage`
- [ ] Gate check passes: `cd web && npm run test`

**Tests**: unit
**Gate**: quick (frontend)

---

### T14: `AuthProvider`

**What**: Context + `useReducer` guardando `{admin, status}`; `login(email,password)` chama `POST /api/auth/login`, extrai só `{id,email,role}` do corpo (descarta `token` explicitamente); `logout()` chama `POST /api/auth/logout`; boot chama `GET /api/auth/me` para hidratar sessão existente via cookie; `hasRole(roles)`.
**Where**: `web/src/auth/AuthProvider.tsx`
**Depends on**: T13
**Reuses**: `apiClient`
**Requirement**: AF-01, AF-05, AF-07(logout via AF-43), AF-43

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `login` bem-sucedido atualiza `admin` só com `id/email/role`; teste confirma que nenhum campo `token` é armazenado em nenhum estado (grep de asserção no teste)
- [ ] Boot com cookie válido (mock de `GET /api/auth/me` 200) resulta em `status:"authenticated"`
- [ ] Boot sem cookie válido (mock 401) resulta em `status:"anonymous"`
- [ ] `logout` limpa `admin` e chama o endpoint
- [ ] `hasRole(["owner"])` retorna corretamente pros 3 papéis
- [ ] Gate check passes: `cd web && npm run test`

**Tests**: unit
**Gate**: quick (frontend)

---

### T15: Route guards (`RequireAuth`/`RequireRole`)

**What**: Componentes de rota `react-router-dom` (`RequireAuth` e `RequireRole`, mesmo diretório) — redireciona pra `/login` se não autenticado; redireciona pra home se papel não permitido (acesso direto por URL).
**Where**: `web/src/routes/RequireRole.tsx`
**Depends on**: T14
**Reuses**: `useAuth` (T14)
**Requirement**: AF-05, AF-30 (edge case de acesso direto por URL)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Não autenticado tentando rota protegida é redirecionado pra `/login`
- [ ] Autenticado com papel fora da lista permitida é redirecionado pra home, não pra `/login`
- [ ] Autenticado com papel permitido renderiza o conteúdo
- [ ] Gate check passes: `cd web && npm run test`

**Tests**: unit
**Gate**: quick (frontend)

---

### T16: `SessionExpiredModal`

**What**: Modal Radix `Dialog` bloqueante, disparado pelo `onUnauthorized` do `apiClient` (registrado no `AuthProvider`); botão "Fazer login novamente" limpa sessão local e navega pra `/login`.
**Where**: `web/src/auth/SessionExpiredModal.tsx`
**Depends on**: T15
**Reuses**: `Dialog` do Radix (já na stack)
**Requirement**: AF-03, AF-04

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] 401 simulado dispara a abertura do modal, mantendo o conteúdo da tela atual visível atrás
- [ ] Confirmar o modal redireciona pra `/login`
- [ ] Modal bloqueante (maior z-index da árvore), ícone de cadeado, CTA único "Ir para o login"
- [ ] Clique no backdrop NÃO fecha o modal (usa a prop de `Dialog` de T12 que desabilita dismiss por backdrop — diferente de todos os outros dialogs do app)
- [ ] Gate check passes: `cd web && npm run test`

**Tests**: unit
**Gate**: quick (frontend)

---

### T17: App shell — Sidebar, EmptyState, i18n base

**What**: Layout autenticado com sidebar (Domínios, Status Pages, Incidentes, Integrações, Admins, Status do Poller — links de Admins ocultos se `!hasRole(["owner"])`), botão de logout, componente `EmptyState` reutilizável (mesmo diretório de componentes), strings base pt/en via i18next.
**Where**: `web/src/layout/Sidebar.tsx`
**Depends on**: T16
**Reuses**: `useAuth`, padrão i18next de `zeep-orbit`
**Requirement**: AF-30 (RBAC visual da navegação), edge case de estado vazio

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Sidebar esconde o link "Admins" para `operator`/`viewer`
- [ ] `EmptyState` renderiza título/CTA customizáveis
- [ ] Troca de idioma pt/en funciona nas strings já cadastradas
- [ ] Sidebar tem 236px de largura, marca "Vane" (AD-005) + ícone estrela/raio no topo
- [ ] Controle "Sair" abre modal de confirmação (título "Sair do painel", corpo "Tem certeza que deseja encerrar sua sessão?", ações Cancelar/secondary e Sair/primary) antes de efetivar o logout
- [ ] Layout reserva um slot fixo acima da área de conteúdo para o banner global do poller (T34), visível em qualquer rota autenticada
- [ ] Gate check passes: `cd web && npm run test`

**Tests**: unit
**Gate**: quick (frontend)

---

### T18: `go:embed` handler da SPA

**What**: Adaptar `zeep-orbit/internal/dashboard/embed.go` para este projeto — `internal/webui/static.go`, servindo `web/dist` embutido com fallback pra `index.html` em rotas de client-side routing.
**Where**: `internal/webui/static.go`
**Depends on**: T17 (precisa de `web/dist` existir, mesmo que mínimo, pra embed compilar)
**Reuses**: `zeep-orbit/internal/dashboard/embed.go` (mesma lógica de `fs.Sub` + fallback)
**Requirement**: N/A (infraestrutura de deploy, AD-001)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Serve arquivo estático existente corretamente
- [ ] Rota inexistente (client-side route) cai no fallback `index.html`
- [ ] `go build ./...` compila com `web/dist` presente
- [ ] Gate check passes: `go test ./...`

**Tests**: unit
**Gate**: build (backend)

---

### T19: `LoginPage`

**What**: Formulário email/senha, chama `useAuth().login`, exibe erro genérico em credenciais inválidas, redireciona pra home em sucesso; inclui "esqueci minha senha" (link pra fluxo de T20).
**Where**: `web/src/features/auth/LoginPage.tsx`
**Depends on**: T18
**Reuses**: `useAuth`
**Requirement**: AF-01, AF-02

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Login correto redireciona pra home
- [ ] Login incorreto exibe erro genérico sem redirecionar
- [ ] Layout: coluna centralizada full-viewport, `card elev-md` max-width 380px
- [ ] Campo de senha tem toggle de visibilidade (ícone de olho)
- [ ] Erro de credencial inválida usa copy exato "E-mail ou senha inválidos." (nunca revela qual campo está errado)
- [ ] Link "Esqueci minha senha" renderizado em accent, 12.5px
- [ ] Botão primário "Entrar" ocupa a largura total do form (block-width)
- [ ] NÃO existe o toggle "Pré-visualizar estado de erro" do protótipo (dev-only, não vai pra produção)
- [ ] Gate check passes: `cd web && npm run test`

**Tests**: unit
**Gate**: quick (frontend)

---

### T20: Reset de senha (request/confirm)

**What**: Página de "esqueci minha senha" (submete email, confirmação genérica) e página de confirmação (token da URL + nova senha, mesmo diretório).
**Where**: `web/src/features/auth/PasswordResetRequestPage.tsx`
**Depends on**: T19
**Reuses**: `apiClient`
**Requirement**: AF-06

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Submissão de email sempre mostra confirmação genérica (sucesso ou não)
- [ ] Token expirado/usado exibe erro específico da API na tela de confirmação
- [ ] Gate check passes: `cd web && npm run test`

**Tests**: unit
**Gate**: quick (frontend)

---

### T21: Hooks de integração Datadog + busca de SLO

**What**: `useIntegrationStatus`, `useConnectDatadog` (mutation), `useSLOSearch(query)` (TanStack Query, `enabled` só com query não vazia).
**Where**: `web/src/features/integrations/hooks.ts`
**Depends on**: T20
**Reuses**: `apiClient`
**Requirement**: AF-07, AF-08, AF-09

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `useConnectDatadog` invalida `useIntegrationStatus` em sucesso
- [ ] `useSLOSearch` não dispara requisição com query vazia
- [ ] Gate check passes: `cd web && npm run test`

**Tests**: unit
**Gate**: quick (frontend)

---

### T22: `IntegrationsPage`

**What**: Formulário de API key/App key, badge de status conectado, mensagem de erro específica em falha, ação escondida para `viewer`.
**Where**: `web/src/features/integrations/IntegrationsPage.tsx`, teste correspondente
**Depends on**: T21
**Reuses**: `useAuth().hasRole`
**Requirement**: AF-07, AF-08, AF-11

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Conexão bem-sucedida mostra "conectada", key nunca reaparece em texto plano
- [ ] Erro da API mantém formulário preenchido
- [ ] `viewer` não vê o formulário de conectar/editar
- [ ] Card conectado mostra ícone+título "Datadog" + caption de chave mascarada no formato "Chave: •••• •••• •••• 8f2a" + timestamp da última verificação + tag "Conectado" (success)
- [ ] "Rotacionar chave" (escondido pra `viewer`) expande form inline de 2 campos (API key/App key, `type=password`) com caption avisando que a chave nunca é reexibida após salvar
- [ ] Estado vazio (nenhuma integração ainda) renderiza como CTA única "Conectar Datadog", sem metadata, abrindo o mesmo form
- [ ] Gate check passes: `cd web && npm run test`

**Tests**: unit
**Gate**: quick (frontend)

---

### T23: Serviços — mapeamento a SLO + `ServicesPage`

**What**: `useServices`/`useCreateService` hooks; formulário de criação de serviço com combobox de busca de SLO (`useSLOSearch`); `ServicesPage` lista serviços, rótulo "not configured" quando sem SLO (tudo no mesmo diretório de feature).
**Where**: `web/src/features/services/hooks.ts`
**Depends on**: T22
**Reuses**: `useSLOSearch` (T21)
**Requirement**: AF-09, AF-10

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Selecionar um SLO da lista de busca e criar o serviço remove o rótulo "not configured"
- [ ] Serviço sem SLO vinculado exibe "not configured"
- [ ] Tabela com colunas Serviço, SLO vinculado, Status (4 estados: Operacional/success, Degradado/warning, Inoperante/critical, Não configurado/neutral), Última mudança
- [ ] "Vincular serviço" (escondido pra `viewer`) abre dialog com nome do serviço + busca de SLO + lista filtrada de resultados
- [ ] Gate check passes: `cd web && npm run test`

**Tests**: unit
**Gate**: quick (frontend)

---

### T24: Hooks de domínios e status pages (com polling)

**What**: `useDomains`/`useCreateDomain`; `useStatusPages`/`useCreateStatusPage` com `refetchInterval` condicional (10s enquanto item em cache não estiver em estado terminal `published`/`tls_failed`) — dois hooks de features irmãs, cada um no seu diretório.
**Where**: `web/src/features/domains/hooks.ts`
**Depends on**: T23
**Reuses**: `apiClient`
**Requirement**: AF-12, AF-13, AF-16

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `useStatusPages` para de fazer polling quando o estado observado é `published` ou `tls_failed`
- [ ] `useCreateDomain` invalida a lista de domínios em sucesso
- [ ] Gate check passes: `cd web && npm run test`

**Tests**: unit
**Gate**: quick (frontend)

---

### T25: `DomainsPage`

**What**: Lista de domínios + formulário de cadastro; erro específico em domínio duplicado; ação escondida para `viewer`.
**Where**: `web/src/features/domains/DomainsPage.tsx`, teste correspondente
**Depends on**: T24
**Reuses**: `useAuth().hasRole`
**Requirement**: AF-16, AF-17, AF-18

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Cadastro duplicado exibe erro da API sem criar linha nova
- [ ] `viewer` não vê o formulário de cadastro
- [ ] Tabela com colunas Hostname, Cadastrado em
- [ ] Erro 409 (duplicado) usa copy exato "Esse hostname já está cadastrado." como erro persistente sob o campo, com ícone de erro
- [ ] Gate check passes: `cd web && npm run test`

**Tests**: unit
**Gate**: quick (frontend)

---

### T26: `StatusPagesPage` (lista)

**What**: Lista de status pages existentes + formulário de criação (seleciona `domain_id` da lista de domínios).
**Where**: `web/src/features/status-pages/StatusPagesPage.tsx`, teste correspondente
**Depends on**: T25
**Reuses**: `useDomains` (T24)
**Requirement**: AF-12, AF-17

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Criação exibe a página nova em estado "emitindo certificado"
- [ ] `viewer` não vê o formulário de criação
- [ ] Tabela com colunas Nome, Subdomínio, Estado; mapeamento de `state` (decisão registrada em `design.md`/Tech Decisions): `draft` → tag accent "Emitindo certificado" (indicador pulsante), `published` → tag success "Publicada" + link pra URL pública, `tls_failed` → tag critical "Falha" + `tls_last_error` exibido como caption sob a tag — sem estado "Rascunho" separado
- [ ] "Criar status page" abre dialog com nome, subdomínio, domain picker (`useDomains`) e multi-select de serviços renderizado como tags outline togláveis
- [ ] Gate check passes: `cd web && npm run test`

**Tests**: unit
**Gate**: quick (frontend)

---

### T27: `StatusPageDetail` (polling de TLS)

**What**: Tela de detalhe — mostra estado atual, URL pública quando `published`, motivo da falha quando `tls_failed`; usa o `refetchInterval` de T24 até estado terminal.
**Where**: `web/src/features/status-pages/StatusPageDetail.tsx`, teste correspondente (mock de timers pra validar o polling)
**Depends on**: T26
**Reuses**: `useStatusPages` (T24)
**Requirement**: AF-13, AF-14, AF-15

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Estado `published` exibe URL e para o polling (teste avança timers e confirma nenhuma nova chamada)
- [ ] Estado `tls_failed` exibe o motivo e para o polling
- [ ] Mantida como rota própria (decisão registrada em `design.md`/Tech Decisions — diverge do handoff, que mostra esse detalhe inline na tabela de T26)
- [ ] Gate check passes: `cd web && npm run test`

**Tests**: unit
**Gate**: quick (frontend)

---

### T28: Hooks de incidentes

**What**: `useIncidents`, `useCreateIncident`, `useAddIncidentUpdate`, `useTransitionIncident` (TanStack Query + mutations com invalidation).
**Where**: `web/src/features/incidents/hooks.ts`, teste correspondente
**Depends on**: T27
**Reuses**: `apiClient`
**Requirement**: AF-19, AF-20, AF-22, AF-23

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `useAddIncidentUpdate` invalida a timeline do incidente em sucesso (resposta é a timeline completa, tratada como lista, não item único — ver design.md)
- [ ] `useTransitionIncident` aceita reabertura (`resolved` → estado anterior)
- [ ] Gate check passes: `cd web && npm run test`

**Tests**: unit
**Gate**: quick (frontend)

---

### T29: `IncidentsPage`

**What**: Lista de incidentes separada em ativos/resolvidos; formulário de criação com seleção de serviços; ação escondida para `viewer`.
**Where**: `web/src/features/incidents/IncidentsPage.tsx`, teste correspondente
**Depends on**: T28
**Reuses**: `useServices` (T23)
**Requirement**: AF-19, AF-21, AF-24

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Incidente não-resolvido aparece em destaque separado dos resolvidos
- [ ] `viewer` não vê o formulário de criação
- [ ] `Seg` (T12) alterna Ativos/Resolvidos
- [ ] Card de incidente ativo: tag accent (Investigando/Identificado/Monitorando — só o label muda), título, tags de serviços afetados, timestamp de criação
- [ ] Estado vazio (Ativos) usa copy exato: ícone check-circle + "Nenhum incidente ativo" + "Todos os serviços monitorados estão operando normalmente."
- [ ] Card resolvido: tag neutral "Resolvido", data de resolução, botão ghost "Reabrir incidente" com ícone reload (escondido pra `viewer`)
- [ ] Gate check passes: `cd web && npm run test`

**Tests**: unit
**Gate**: quick (frontend)

---

### T30: `IncidentDetail` (timeline)

**What**: Timeline ordenada mais recente primeiro, formulário de novo update, controle de transição de estado (incluindo reabertura).
**Where**: `web/src/features/incidents/IncidentDetail.tsx`, teste correspondente
**Depends on**: T29
**Reuses**: `useIncidents`/`useAddIncidentUpdate`/`useTransitionIncident` (T28)
**Requirement**: AF-20, AF-22, AF-23, AF-24

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] 2 updates adicionados aparecem em ordem cronológica reversa
- [ ] Marcar como "resolved" move o incidente pro histórico mantendo a timeline acessível
- [ ] Reabrir um incidente resolvido funciona e aparece de volta em destaque
- [ ] Timeline renderiza dot+corpo+timestamp por update, mais recente primeiro
- [ ] Input de novo update + botão "Publicar"; botões de transição rápida (Identificado/Monitorando/"Marcar como resolvido"); tudo escondido pra `viewer`
- [ ] Gate check passes: `cd web && npm run test`

**Tests**: unit
**Gate**: quick (frontend)

---

### T31: Hooks de gestão de admins

**What**: `useAdmins` (inclui pendentes via AF-38), `useInviteAdmin`, `useUpdateAdminRole`, `useDeleteAdmin` — mutations com invalidation e tratamento de erro 409 (lockout de owner).
**Where**: `web/src/features/admins/hooks.ts`, teste correspondente
**Depends on**: T30
**Reuses**: `apiClient`
**Requirement**: AF-25, AF-27, AF-28, AF-29

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `useAdmins` reflete `status:"active"|"pending"` do backend (T4)
- [ ] Erro 409 de `useUpdateAdminRole`/`useDeleteAdmin` propaga a mensagem da API sem invalidar a lista (mantém estado anterior)
- [ ] Gate check passes: `cd web && npm run test`

**Tests**: unit
**Gate**: quick (frontend)

---

### T32: `AdminsPage`

**What**: Rota restrita a `owner` (via `RequireRole`); lista única com badge "Pendente"; formulário de convite; ações de reenviar/cancelar convite, mudar papel, remover.
**Where**: `web/src/features/admins/AdminsPage.tsx`, teste correspondente
**Depends on**: T31
**Reuses**: `RequireRole` (T15), `useAdmins`/mutations (T31)
**Requirement**: AF-25, AF-26, AF-30

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Convite pendente aparece com badge "Pendente" na mesma lista dos ativos
- [ ] `operator`/`viewer` não acessam a rota (redirecionados, via T15)
- [ ] Rejeição 409 (lockout) exibe erro e mantém a linha anterior visível
- [ ] Coluna Papel usa `IconRoleSelector` (T12) — NÃO é um `<select>` — clique num ícone diferente dispara confirmação antes de `PATCH`
- [ ] Remover admin abre confirmação com copy exato: título "Remover admin", corpo "Remover o acesso de {email}? Esta ação não pode ser desfeita."
- [ ] Tabela de convites pendentes: E-mail, Papel, tag outline "Pendente", ações Reenviar/Cancelar (Cancelar com o mesmo padrão de confirmação destrutiva)
- [ ] Nav "Admins" fica oculta inteiramente pra non-owner (já coberto pelo shell de T17, confirmar aqui que a página em si também redireciona non-owner)
- [ ] Gate check passes: `cd web && npm run test`

**Tests**: unit
**Gate**: quick (frontend)

---

### T33: Hook de status do poller

**What**: `usePollerStatus` (TanStack Query, `refetchInterval` ~30s).
**Where**: `web/src/features/poller/hooks.ts`, teste correspondente
**Depends on**: T32
**Reuses**: `apiClient`
**Requirement**: AF-31

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Hook expõe a lista `[{provider,status,last_checked_at,last_error}]`
- [ ] Gate check passes: `cd web && npm run test`

**Tests**: unit
**Gate**: quick (frontend)

---

### T34: `PollerBanner` + `PollerStatusPage`

**What**: Banner global (montado no layout autenticado, T17) visível quando algum item de `usePollerStatus` tem `status !== "active"`; página dedicada com detalhe por integração (mesmo diretório de feature).
**Where**: `web/src/features/poller/PollerBanner.tsx`
**Depends on**: T33
**Reuses**: `usePollerStatus` (T33) — mesmo hook pro banner e pra página, sem fetch duplicado
**Requirement**: AF-31, AF-32, AF-33

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Falha simulada em qualquer integração exibe o banner em qualquer rota autenticada
- [ ] Correção (próxima consulta bem-sucedida) remove o banner
- [ ] Página dedicada mostra timestamp + mensagem de erro por integração
- [ ] Banner: fundo critical-tinted, ícone warning-triangle, botão ghost "Ver detalhes" navegando pra Status do Poller. Copy de uma linha do handoff não é especificado literalmente — usar texto objetivo (placeholder a confirmar em revisão de copy antes do lançamento, não travado como definitivo)
- [ ] Página dedicada: tabela Integração, Última execução, Resultado (Sucesso=tag success / Falha=tag critical), Mensagem de erro (só populada em falha)
- [ ] Gate check passes: `cd web && npm run test`

**Tests**: unit
**Gate**: quick (frontend)

---

## Phase Execution Map (histórico — T1-T34, rodada mock-first)

Visual representation of task ordering. Phases run in sequence, and tasks within a phase run in order:

```
Phase 1 → Phase 2 → Phase 3 → Phase 4 → Phase 5 → Phase 6 → Phase 7 → Phase 8

Phase 1:  T1 ------→ T2 ------→ T3 ------→ T4 ------→ T5
Phase 2:  T5 ------→ T6 ------→ T7 ------→ T8
Phase 3:  T8 ------→ T9 ------→ T10 ------→ T11 ------→ T12 ------→ T13 ------→ T14 ------→ T15 ------→ T16 ------→ T17 ------→ T18
Phase 4:  T18 -----→ T19 ------→ T20
Phase 5:  T20 -----→ T21 ------→ T22 ------→ T23
Phase 6:  T23 -----→ T24 ------→ T25 ------→ T26 ------→ T27
Phase 7:  T27 -----→ T28 ------→ T29 ------→ T30
Phase 8:  T30 -----→ T31 ------→ T32 ------→ T33 ------→ T34
```

Execution is strictly sequential - there is no intra-phase parallelism. A single agent (or batch worker) works one task at a time, in order.

**Nota**: este mapa e as 3 seções de validação abaixo (Granularity/Diagram-Cross-Check/Test Co-location) descrevem a rodada mock-first já concluída (T1-T34) — mantidas como registro histórico. As seções equivalentes para a rodada ativa (I1-I20) estão logo depois, em "## Phase Execution Map (I1-I20 — integração real)".

---

## Task Granularity Check (histórico — T1-T34)

| Task | Scope | Status |
| --- | --- | --- |
| T1-T8 | 1 endpoint/handler change each | ✅ Granular |
| T9 | 1 scaffold (config files, no logic) | ✅ Granular |
| T10 | 1 config/CSS artifact (design tokens) | ✅ Granular |
| T11 | 4 primitive components, mesmo nível de coesão já usado em T17 (peças pequenas, mesma camada, mesma fase) | ✅ OK — coeso |
| T12 | 4 composed/interactive components — mais pesado que T11 individualmente (Dialog sobre Radix, IconRoleSelector com estado); se o Verifier achar T12 grande demais na prática, o corte natural já está pronto (T11 primitivos vs T12 compostos) | ✅ OK — coeso, com corte de contingência documentado |
| T13-T16 | 1 component/module each | ✅ Granular |
| T17 | 3 small cohesive pieces (sidebar, empty state, i18n base) in the same shell layer | ✅ OK — cohesive (all "app shell" concerns, same phase, small individually) |
| T18 | 1 Go package (embed handler) | ✅ Granular |
| T19-T34 | 1 page/hook-set per task, each mapped to a distinct spec story sub-slice | ✅ Granular |

---

## Diagram-Definition Cross-Check (histórico — T1-T34)

Every task depends on exactly its immediate predecessor (T1←none, T2←T1, T3←T2, ... T34←T33) — a single linear chain across all 8 phases, matching the pattern already used in `admin-dashboard/tasks.md`. Each phase's diagram repeats the last task of the previous phase as its leading node (e.g. Phase 2 opens with `T5 → T6 → ...`, since T5 is Phase 1's last task), so every `Depends on` has a matching arrow and every arrow has a matching `Depends on` — including at phase boundaries.

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | None (first task) | ✅ Match |
| T2..T5 | T(n-1) | T(n-1)→T(n) in Phase 1 | ✅ Match |
| T6 | T5 | T5→T6 in Phase 2 (T5 repeated as Phase 2's leading node) | ✅ Match |
| T7, T8 | T(n-1) | T(n-1)→T(n) in Phase 2 | ✅ Match |
| T9 | T8 | T8→T9 in Phase 3 (T8 repeated as Phase 3's leading node) | ✅ Match |
| T10..T18 | T(n-1) | T(n-1)→T(n) in Phase 3 | ✅ Match |
| T19 | T18 | T18→T19 in Phase 4 (T18 repeated as Phase 4's leading node) | ✅ Match |
| T20 | T19 | T19→T20 in Phase 4 | ✅ Match |
| T21 | T20 | T20→T21 in Phase 5 (T20 repeated as Phase 5's leading node) | ✅ Match |
| T22, T23 | T(n-1) | T(n-1)→T(n) in Phase 5 | ✅ Match |
| T24 | T23 | T23→T24 in Phase 6 (T23 repeated as Phase 6's leading node) | ✅ Match |
| T25..T27 | T(n-1) | T(n-1)→T(n) in Phase 6 | ✅ Match |
| T28 | T27 | T27→T28 in Phase 7 (T27 repeated as Phase 7's leading node) | ✅ Match |
| T29, T30 | T(n-1) | T(n-1)→T(n) in Phase 7 | ✅ Match |
| T31 | T30 | T30→T31 in Phase 8 (T30 repeated as Phase 8's leading node) | ✅ Match |
| T32..T34 | T(n-1) | T(n-1)→T(n) in Phase 8 | ✅ Match |

No dependency points to a later phase — the chain is strictly sequential and backward-only by construction.

---

## Test Co-location Validation (histórico — T1-T34)

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1 | Go handler | integration | integration | ✅ OK |
| T2 | Go handler + repository | integration | integration | ✅ OK |
| T3 | Go handler + repository | integration | integration | ✅ OK |
| T4 | Go handler + repository | integration | integration | ✅ OK |
| T5 | Go connector + handler | unit (connector) + integration (handler) | integration (handler) + unit (connector) | ✅ OK |
| T6 | Go handler (middleware — auth) | integration | integration | ✅ OK |
| T7 | Go middleware | integration | integration | ✅ OK |
| T8 | Go handler | integration | integration | ✅ OK |
| T9 | Config/scaffold | none | none | ✅ OK |
| T10 | React design-system tokens (CSS/config) | unit (smoke) | unit | ✅ OK |
| T11 | React design-system component (primitives) | unit (component) | unit | ✅ OK |
| T12 | React design-system component (composed) | unit (component) | unit | ✅ OK |
| T13 | React lib (apiClient) | unit | unit | ✅ OK |
| T14 | React auth (AuthProvider) | unit | unit | ✅ OK |
| T15 | React routing (guards) | unit | unit | ✅ OK |
| T16 | React auth (modal) | unit | unit | ✅ OK |
| T17 | React components/layout | unit | unit | ✅ OK |
| T18 | Go embed handler | unit | unit | ✅ OK |
| T19 | React page | unit | unit | ✅ OK |
| T20 | React page | unit | unit | ✅ OK |
| T21 | React hooks | unit | unit | ✅ OK |
| T22 | React page | unit | unit | ✅ OK |
| T23 | React hooks + page | unit | unit | ✅ OK |
| T24 | React hooks | unit | unit | ✅ OK |
| T25 | React page | unit | unit | ✅ OK |
| T26 | React page | unit | unit | ✅ OK |
| T27 | React page | unit | unit | ✅ OK |
| T28 | React hooks | unit | unit | ✅ OK |
| T29 | React page | unit | unit | ✅ OK |
| T30 | React page | unit | unit | ✅ OK |
| T31 | React hooks | unit | unit | ✅ OK |
| T32 | React page | unit | unit | ✅ OK |
| T33 | React hooks | unit | unit | ✅ OK |
| T34 | React components | unit | unit | ✅ OK |

No violations — every task's `Tests` field matches its layer's Coverage Expectation from the matrix. `Tests: none` used only for T9 (pure scaffold/config), consistent with the matrix.

---

## Phase Execution Map (I1-I20 — integração real, esta rodada)

```
Etapa 0 → Etapa 1 → Etapa 2 → Etapa 3 → Etapa 4 → Etapa 5

Etapa 0:  I1 ------→ I2 ------→ I3 ------→ I4 ------→ I5 ------→ I6 ------→ I7 ------→ I8 ------→ I9
Etapa 1:  I9 ------→ I10 ------→ I11 ------→ I12 ------→ I13
Etapa 2:  I13 -----→ I14 ------→ I15
Etapa 3:  I15 -----→ I16
Etapa 4:  I16 -----→ I17 ------→ I18 ------→ I19
Etapa 5:  I19 -----→ I20
```

Execução sequencial dentro de cada etapa. 20 tasks totais — acima do budget de ~7-8 por worker de um único batch, então sub-agentes por etapa serão oferecidos no início do Execute (Etapa 0 sozinha tem 9 tasks, levemente acima do budget — dependency chain única, não faz sentido dividir no meio; vira seu próprio batch, absorvendo o excedente). Etapas 1-3 e 5 (4-5 tasks cada) empacotam bem num batch por etapa; Etapa 4 (4 tasks) idem.

---

## Task Granularity Check (I1-I20)

| Task | Scope | Status |
| --- | --- | --- |
| I1-I5 | 1 endpoint/middleware change each | ✅ Granular |
| I6 | 1 arquivo (`apiClient.ts`) + 1 config de env — reescrita coesa de uma única camada | ✅ Granular |
| I7 | Setup de MSW + migração de 1 arquivo de teste — cohesive (infra de teste nova, primeiro uso) | ✅ OK — coeso |
| I8 | 2 arquivos pequenos e diretamente relacionados (isolar 1 feature dev-only) | ✅ OK — coeso |
| I9 | Refactor mecânico de tipos, 1 arquivo novo + imports atualizados | ✅ Granular |
| I10-I12 | 1 endpoint/repository change each | ✅ Granular |
| I13 | Wiring de 3 features irmãs (domains/status-pages/public-status) que já compartilham a mesma migração mecânica (trocar teste de handleRoute pra MSW) — cohesive, mesma fase | ✅ OK — coeso |
| I14 | 1 método de client + 1 endpoint | ✅ Granular |
| I15 | Wiring de 2 features irmãs (integrations/services), mesma migração mecânica | ✅ OK — coeso |
| I16 | Wiring de 1 feature | ✅ Granular |
| I17-I18 | 1 repository method / 1 handler change each | ✅ Granular |
| I19 | Wiring de 1 feature + desabilitar 2 ações órfãs — cohesive (mesma página, mesma decisão AD-007) | ✅ OK — coeso |
| I20 | Wiring de 1 feature | ✅ Granular |

---

## Diagram-Definition Cross-Check (I1-I20)

Cada task depende exatamente do seu predecessor imediato (I1←none, I2←I1, ... I20←I19) — cadeia linear única através das 6 etapas. Cada etapa repete a última task da etapa anterior como nó inicial do diagrama (ex. Etapa 1 abre com `I9 → I10 → ...`, já que I9 é a última task da Etapa 0), então todo `Depends on` tem seta correspondente e toda seta tem `Depends on` correspondente — inclusive nas fronteiras de etapa.

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| I1 | None | None (first task) | ✅ Match |
| I2..I9 | I(n-1) | I(n-1)→I(n) in Etapa 0 | ✅ Match |
| I10 | I9 | I9→I10 in Etapa 1 (I9 repeated as Etapa 1's leading node) | ✅ Match |
| I11..I13 | I(n-1) | I(n-1)→I(n) in Etapa 1 | ✅ Match |
| I14 | I13 | I13→I14 in Etapa 2 (I13 repeated as Etapa 2's leading node) | ✅ Match |
| I15 | I14 | I14→I15 in Etapa 2 | ✅ Match |
| I16 | I15 | I15→I16 in Etapa 3 (I15 repeated as Etapa 3's leading node) | ✅ Match |
| I17 | I16 | I16→I17 in Etapa 4 (I16 repeated as Etapa 4's leading node) | ✅ Match |
| I18, I19 | I(n-1) | I(n-1)→I(n) in Etapa 4 | ✅ Match |
| I20 | I19 | I19→I20 in Etapa 5 (I19 repeated as Etapa 5's leading node) | ✅ Match |

No dependency points to a later stage — the chain is strictly sequential and backward-only by construction.

---

## Test Co-location Validation (I1-I20)

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| I1 | Go handler | integration | integration | ✅ OK |
| I2 | Go handler (auth) | integration | integration | ✅ OK |
| I3 | Go middleware | integration | integration | ✅ OK |
| I4 | Go handler | integration | integration | ✅ OK |
| I5 | Go middleware (CORS) | integration | integration | ✅ OK |
| I6 | React lib (apiClient rewrite) | unit | none (coberto por I7 — ver justificativa abaixo) | ✅ OK |
| I7 | React test infra (MSW) + auth test | unit | unit (via MSW) | ✅ OK |
| I8 | React auth/layout (dev-only feature) | unit | unit | ✅ OK |
| I9 | Config/types refactor | none | none | ✅ OK |
| I10 | Go handler + repository | integration | integration | ✅ OK |
| I11 | Go handler + repository | integration | integration | ✅ OK |
| I12 | Go handler (novo endpoint) | integration | integration | ✅ OK |
| I13 | React hooks (wiring + testes) | unit (via MSW) | unit (via MSW) | ✅ OK |
| I14 | Go connector + handler | unit (connector) + integration (handler) | integration (handler) + unit (connector) | ✅ OK |
| I15 | React hooks (wiring + testes) | unit (via MSW) | unit (via MSW) | ✅ OK |
| I16 | React hooks (wiring + testes) | unit (via MSW) | unit (via MSW) | ✅ OK |
| I17 | Go repository | integration | integration | ✅ OK |
| I18 | Go handler | integration | integration | ✅ OK |
| I19 | React hooks + page (wiring + testes) | unit (via MSW) | unit (via MSW) | ✅ OK |
| I20 | React hooks (wiring + testes) | unit (via MSW) | unit (via MSW) | ✅ OK |

**Justificativa I6 `Tests: none`**: I6 reescreve `apiClient.ts` pra fetch real, mas nenhum teste de hook pode validar essa troca até o MSW existir (I7) — sem MSW, "testar" I6 exigiria rede real contra o backend rodando, não é um teste automatizado isolado. Não é deferral disfarçado: I7 é a PRÓXIMA task, roda imediatamente em seguida na mesma etapa, e migra exatamente o arquivo de teste que cobre o comportamento que I6 introduziu (`AuthProvider.test.tsx`, que exercita `apiFetch` ponta a ponta). Este é o padrão "merge forward" descrito em `tasks.md`/Resolving compilation dependencies — a alternativa (só uma task I6+I7 fundida) quebraria a atomicidade (2 preocupações distintas: reescrever o client vs. montar infra de teste nova). Gate de I6 é `build` (compila e o `AuthProvider` real consegue rodar contra um backend real manualmente) até I7 fechar a cobertura automatizada.

No violations além da já justificada — every task's `Tests` field matches its layer's Coverage Expectation from the matrix.

---

