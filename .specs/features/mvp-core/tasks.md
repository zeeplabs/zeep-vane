# MVP Core (Zeep Vane) Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/mvp-core/design.md`
**Status**: Draft

---

**Scope note:** este breakdown cobre o backend (API Go, testável via HTTP/`go test`) de todas as stories P1 e a story P2 (login/conta). O admin React (P1-adjacent UI) e a story P3 (histórico de uptime) ficam para uma passada de Tasks separada depois que o backend estiver verificado — evita um único lote gigante e entrega o núcleo de valor (dado real fluindo do Datadog pra status page) primeiro. Todas as Independent Tests da spec são satisfeitas via chamada HTTP direta, sem exigir UI.

---

## Test Coverage Matrix

> Gerado por decisão do usuário (mesmo padrão de `baas/zeep-orbit`) - projeto novo, sem testes existentes ainda. Confirmar antes de Execute.

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| Domain/business logic (`internal/auth`, `internal/connectors`, `internal/poller`, `internal/tls`, `internal/crypto`) | unit | Todos os branches; 1:1 com ACs da spec; todo edge case listado tem teste | `internal/**/*_test.go` | `go test ./...` |
| API handlers (`internal/api`) | integration (via `httptest`) | Toda rota: happy path + edge + erro | `internal/api/**/*_test.go` | `go test ./...` |
| Repository/data-access (`internal/db`) | integration | Caminhos de query principais + tratamento de erro | `internal/db/**/*_test.go` | `go test ./... -tags=integration` (requer `TEST_DATABASE_URL`) |
| Migrations/config/entities | none | build gate apenas | - | build gate apenas |

## Gate Check Commands

> Confirmar antes de Execute.

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick | Após tasks só com teste unitário | `go test ./... && go vet ./...` |
| Full | Após tasks com integração (API/DB) | `go test ./... -tags=integration && go vet ./...` |
| Build | Fim de fase ou task só de config/migration | `go build ./... && gofmt -l . && go test ./... -tags=integration && go vet ./...` |

---

## Execution Plan

Fases são ordenadas e rodam sequencialmente - cada fase completa antes da próxima começar, e tasks dentro de uma fase rodam em ordem.

### Phase 1: Foundation

```
T1 → T2 → T3 → T4 → T5 → T6 → T7
```

### Phase 2: Auth (P2 - login/conta)

```
T7 → T8 → T9 → T10 → T11 → T12 → T13 → T14
```

### Phase 3: Datadog Integration (P1 - conectar Datadog)

```
T14 → T15 → T16 → T17 → T18 → T19 → T20
```

### Phase 4: Poller (P1 - status público, fonte)

```
T20 → T21 → T22 → T23 → T24 → T25
```

### Phase 5: Domains & TLS (P1 - domínio/TLS)

```
T25 → T26 → T27 → T28 → T29 → T30 → T31
```

### Phase 6: Public Status Page Rendering (P1 - status público, saída)

```
T31 → T32 → T33 → T34 → T35
```

### Phase 7: Incidents (P1 - incidentes manuais)

```
T35 → T36 → T37 → T38 → T39 → T40
```

---

## Task Breakdown

### T1: Scaffold do repositório Go

**What**: `go.mod` (`github.com/zeeplabs/zeep-vane`), `.gitignore`, `Makefile` com targets `build`/`test`/`lint`/`vet`, `cmd/vane/main.go` vazio que só compila.
**Where**: raiz do projeto, `cmd/vane/main.go`, `Makefile`, `go.mod`
**Depends on**: None
**Reuses**: layout de `baas/zeep-orbit` (`cmd/zeep/main.go`) como referência de estrutura
**Requirement**: N/A (fundação)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `go build ./...` compila sem erro
- [x] `Makefile` tem targets `build`, `test`, `lint`, `vet` funcionais

**Commit**: `chore(scaffold): initialize go module and project layout`
**Tests**: none
**Gate**: build

---

### T2: CLI skeleton com cobra

**What**: Comando raiz + subcomando `serve` (ainda sem lógica, só imprime "not implemented").
**Where**: `cmd/vane/main.go`, `internal/cli/root.go`, `internal/cli/serve.go`
**Depends on**: T1
**Reuses**: padrão `cobra` de `zeep-orbit`
**Requirement**: N/A

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `go run ./cmd/vane serve` executa sem erro
- [x] `--help` lista o subcomando `serve`

**Commit**: `feat(cli): add cobra root and serve command skeleton`
**Tests**: none
**Gate**: build

---

### T3: Config loader (env vars)

**What**: Carrega e valida `DATABASE_URL`, `VANE_MASTER_KEY`, `PORT`, `POLL_INTERVAL_SECONDS` via `godotenv` + validação de presença/formato.
**Where**: `internal/config/config.go`
**Depends on**: T2
**Reuses**: padrão de config de `zeep-orbit` (`godotenv`)
**Requirement**: N/A

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `Load()` retorna erro claro se variável obrigatória ausente
- [x] Testes cobrem: todas variáveis presentes (sucesso), uma ausente (erro), formato inválido de `POLL_INTERVAL_SECONDS` (erro)
- [x] Gate check passa: `go test ./... && go vet ./...`

**Commit**: `feat(config): add environment config loader`
**Tests**: unit
**Gate**: quick

---

### T4: Logger estruturado (zap)

**What**: Inicializa `zap.Logger` com nível configurável via `config.LogLevel`, injetável via context.
**Where**: `internal/logging/logger.go`
**Depends on**: T3
**Reuses**: padrão `zap` de `zeep-orbit`
**Requirement**: N/A

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Logger emite JSON estruturado
- [x] Nível configurável via env (`debug`/`info`/`warn`/`error`)
- [x] Gate check passa: `go build ./...`

**Commit**: `feat(logging): add structured zap logger`
**Tests**: none
**Gate**: build

---

### T5: Conexão Postgres (pgx/v5) + health check

**What**: Pool de conexão `pgxpool.Pool` a partir de `DATABASE_URL`, função `Ping(ctx)`.
**Where**: `internal/db/pool.go`
**Depends on**: T4
**Reuses**: `pgx/v5` (mesma lib de `zeep-orbit`)
**Requirement**: N/A

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `NewPool(ctx, dsn)` retorna erro claro se DSN inválido
- [x] `Ping(ctx)` detecta banco fora do ar
- [x] Teste de integração conecta a `TEST_DATABASE_URL` e confirma ping OK
- [x] Gate check passa: `go test ./... -tags=integration`

**Commit**: `feat(db): add postgres connection pool with health check`
**Tests**: integration
**Gate**: full

---

### T6: Estrutura de migrations

**What**: Diretório `internal/db/migrations` com runner (`golang-migrate` ou SQL sequencial próprio) + comando `vane migrate up`.
**Where**: `internal/db/migrations/`, `internal/cli/migrate.go`
**Depends on**: T5
**Reuses**: nenhum
**Requirement**: N/A

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `vane migrate up` roda contra banco vazio sem erro (0 migrations ainda)
- [x] Runner é idempotente (rodar 2x não duplica nada)
- [x] Gate check passa: `go test ./... -tags=integration`

**Commit**: `feat(db): add migration runner and migrate command`
**Tests**: integration
**Gate**: full

---

### T7: Router chi + healthz + wiring no `serve`

**What**: `chi.Router` básico com `GET /healthz` (retorna 200 + status do Postgres), comando `serve` sobe HTTP na porta configurada.
**Where**: `internal/router/router.go`, `internal/cli/serve.go` (completar)
**Depends on**: T6
**Reuses**: `chi` (mesma lib de `zeep-orbit`)
**Requirement**: N/A

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `GET /healthz` retorna 200 quando Postgres acessível, 503 quando não
- [x] Teste de integração cobre os dois casos
- [x] Gate check passa: `go test ./... -tags=integration`

**Commit**: `feat(http): add chi router with healthz endpoint`
**Tests**: integration
**Gate**: full

---

### T8: Migration `admins`

**What**: Tabela `admins (id, email unique, password_hash, created_at)`.
**Where**: `internal/db/migrations/0001_admins.sql`
**Depends on**: T7
**Reuses**: nenhum
**Requirement**: SP-21

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Migration aplica limpo em banco vazio
- [x] Constraint unique em `email` confirmada
- [x] Gate check passa: `go test ./... -tags=integration`

**Commit**: `feat(db): add admins migration`
**Tests**: integration
**Gate**: full

---

### T9: Repositório de Admin

**What**: `AdminRepository` com `Create(ctx, admin) error` e `GetByEmail(ctx, email) (*Admin, error)`.
**Where**: `internal/db/admin_repository.go`
**Depends on**: T8
**Reuses**: pool de T5
**Requirement**: SP-21

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `Create` rejeita email duplicado com erro tipado
- [x] `GetByEmail` retorna `ErrNotFound` tipado quando não existe
- [x] Testes de integração cobrem: create sucesso, create duplicado, get existente, get inexistente
- [x] Gate check passa: `go test ./... -tags=integration`

**Commit**: `feat(auth): add admin repository`
**Tests**: integration
**Gate**: full

---

### T10: Hash de senha (bcrypt)

**What**: `HashPassword(plain string) (string, error)` e `VerifyPassword(hash, plain string) bool`.
**Where**: `internal/auth/password.go`
**Depends on**: T9
**Reuses**: `golang.org/x/crypto/bcrypt` (já dependência transitiva compatível com stack `zeep-orbit`)
**Requirement**: SP-21

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Hash nunca é igual ao texto plano
- [x] Verify retorna true pra senha correta, false pra incorreta
- [x] Testes unitários cobrem os dois casos
- [x] Gate check passa: `go test ./...`

**Commit**: `feat(auth): add bcrypt password hashing`
**Tests**: unit
**Gate**: quick

---

### T11: Endpoint `POST /api/auth/login`

**What**: Valida email+senha contra `AdminRepository`, retorna 401 genérico se inválido (sem indicar se email existe).
**Where**: `internal/api/auth_handler.go`
**Depends on**: T10
**Reuses**: `AdminRepository` (T9), `VerifyPassword` (T10)
**Requirement**: SP-21, SP-22

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Credenciais corretas → 200 + sessão criada
- [x] Senha errada → 401 com mensagem genérica idêntica à de email inexistente (SP-22, anti user-enumeration)
- [x] Email inexistente → mesma mensagem/401
- [x] Testes de integração cobrem os 3 casos
- [x] Gate check passa: `go test ./... -tags=integration`

**Commit**: `feat(auth): add login endpoint`
**Tests**: integration
**Gate**: full

---

### T12: Sessão JWT + middleware de autenticação

**What**: Emite JWT (`golang-jwt/jwt/v5`) no login bem-sucedido, middleware `RequireAuth` que valida token em rotas protegidas.
**Where**: `internal/auth/session.go`, `internal/api/middleware.go`
**Depends on**: T11
**Reuses**: `jwt/v5` (mesma lib de `zeep-orbit`)
**Requirement**: SP-25

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Token válido passa pelo middleware
- [x] Token ausente/inválido/expirado → 401
- [x] Testes de integração cobrem os 3 casos
- [x] Gate check passa: `go test ./... -tags=integration`

**Commit**: `feat(auth): add jwt session and auth middleware`
**Tests**: integration
**Gate**: full

---

### T13: Migration `password_reset_tokens` + repositório

**What**: Tabela `password_reset_tokens (id, admin_id, token_hash, expires_at, used_at)` + repositório (`Create`, `GetByTokenHash`, `MarkUsed`).
**Where**: `internal/db/migrations/0002_password_reset_tokens.sql`, `internal/db/password_reset_repository.go`
**Depends on**: T12
**Reuses**: padrão de repositório de T9
**Requirement**: SP-23, SP-24

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Migration aplica limpo
- [x] Repositório nunca persiste o token em texto plano, só o hash
- [x] Testes de integração cobrem create, get por hash, marcar usado
- [x] Gate check passa: `go test ./... -tags=integration`

**Commit**: `feat(auth): add password reset token migration and repository`
**Tests**: integration
**Gate**: full

---

### T14: Endpoints de reset de senha

**What**: `POST /api/auth/password-reset/request` (gera token, expira em 1h — envio de email fica como stub logado, sem provider real no MVP) e `POST /api/auth/password-reset/confirm` (valida token, seta nova senha).
**Where**: `internal/api/password_reset_handler.go`
**Depends on**: T13
**Reuses**: `PasswordResetRepository` (T13), `HashPassword` (T10)
**Requirement**: SP-23, SP-24

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Request gera token com expiração de 1h
- [x] Confirm com token válido e não expirado troca a senha
- [x] Confirm com token expirado ou já usado → rejeitado (SP-24)
- [x] Testes de integração cobrem os 3 casos
- [x] Gate check passa: `go test ./... -tags=integration`

**Commit**: `feat(auth): password reset request/confirm flow`
**Tests**: integration
**Gate**: full

---

### T15: Migration `integrations`

**What**: Tabela `integrations (id, provider unique, encrypted_api_key, encrypted_app_key, status, last_checked_at, last_error)`.
**Where**: `internal/db/migrations/0003_integrations.sql`
**Depends on**: T14
**Reuses**: nenhum
**Requirement**: SP-01

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Migration aplica limpo
- [x] Constraint unique em `provider` confirmada
- [x] Gate check passa: `go test ./... -tags=integration`

**Commit**: `feat(db): add integrations migration`
**Tests**: integration
**Gate**: full

---

### T16: Criptografia de API key (AES-256-GCM)

**What**: `Encrypt(plain []byte) ([]byte, error)` / `Decrypt(cipher []byte) ([]byte, error)` usando `VANE_MASTER_KEY` (config de T3).
**Where**: `internal/crypto/secretbox.go`
**Depends on**: T15
**Reuses**: `config.MasterKey` (T3)
**Requirement**: SP-04 (nunca expor chave em texto plano)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `Decrypt(Encrypt(x)) == x` pra qualquer input
- [x] Chave mestra errada falha a decriptação com erro claro, nunca retorna dado corrompido silenciosamente
- [x] Testes unitários cobrem round-trip e chave errada
- [x] Gate check passa: `go test ./...`

**Commit**: `feat(crypto): add AES-256-GCM secret encryption`
**Tests**: unit
**Gate**: quick

---

### T17: Cliente Datadog SLO

**What**: `internal/connectors/datadog.Client` implementando `SLOProvider` (`FetchSLOStatus(ctx, sloID) (SLOStatus, error)`), chamando a API real de SLO do Datadog.
**Where**: `internal/connectors/datadog/client.go`
**Depends on**: T16
**Reuses**: interface `SLOProvider` definida no design
**Requirement**: SP-01, SP-06, SP-07

**Tools**:
- MCP: `mcp__claude_ai_Datadog__get_datadog_metric` / ferramentas de SLO do Datadog MCP disponíveis nesta sessão (usar pra inspecionar um SLO real e confirmar o shape de resposta antes de fixar o struct — item marcado `[Incerto]` no design, resolver aqui, não fabricar)
- Skill: NONE

**Done when**:
- [x] Shape da resposta confirmado contra API/MCP real antes de implementar o parser (não assumido)
- [x] `FetchSLOStatus` retorna erro tipado em 401/timeout/5xx (pra retry na Phase 4 diferenciar)
- [x] Testes unitários com servidor HTTP mock cobrem: resposta válida, 401, timeout, 5xx
- [x] Gate check passa: `go test ./...`

**Commit**: `feat(datadog): add SLO status client`
**Tests**: unit
**Gate**: quick

---

### T18: Endpoint `POST /api/integrations/datadog`

**What**: Recebe API key + App key, valida chamando `FetchSLOStatus` num SLO de teste (ou endpoint de validação do Datadog), criptografa e salva só se válido.
**Where**: `internal/api/integrations_handler.go`
**Depends on**: T17
**Reuses**: `crypto.Encrypt` (T16), `datadog.Client` (T17), middleware `RequireAuth` (T12)
**Requirement**: SP-01, SP-02, SP-04

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Key válida → 201, salva criptografado
- [x] Key inválida/sem permissão → 422, nada é salvo (SP-02)
- [x] Resposta do endpoint (e qualquer log) nunca inclui a key em texto plano (SP-04)
- [x] Rota exige autenticação (401 sem sessão)
- [x] Testes de integração cobrem os 4 casos
- [x] Gate check passa: `go test ./... -tags=integration`

**Commit**: `feat(datadog): add connect integration endpoint`
**Tests**: integration
**Gate**: full

---

### T19: Migration `services`

**What**: Tabela `services (id, name, slo_id, current_status default 'not_configured', last_status_change_at)`.
**Where**: `internal/db/migrations/0004_services.sql`
**Depends on**: T18
**Reuses**: nenhum
**Requirement**: SP-03

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Migration aplica limpo
- [x] Default `current_status = 'not_configured'` confirmado
- [x] Gate check passa: `go test ./... -tags=integration`

**Commit**: `feat(db): add services migration`
**Tests**: integration
**Gate**: full

---

### T20: Endpoints de serviço (`POST`/`GET /api/services`)

**What**: `POST /api/services` (cria serviço vinculado a um SLO), `GET /api/services` (lista).
**Where**: `internal/api/services_handler.go`
**Depends on**: T19
**Reuses**: middleware `RequireAuth` (T12)
**Requirement**: SP-03

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Criar serviço com `slo_id` salva vínculo
- [x] Listar retorna todos os serviços com `current_status` atual
- [x] Rotas exigem autenticação
- [x] Testes de integração cobrem os 2 fluxos
- [x] Gate check passa: `go test ./... -tags=integration`

**Commit**: `feat(datadog): connect integration and link services to SLOs`
**Tests**: integration
**Gate**: full

---

### T21: Migration `status_snapshots`

**What**: Tabela `status_snapshots (id, service_id, status, error_budget_remaining, fetched_at)`.
**Where**: `internal/db/migrations/0005_status_snapshots.sql`
**Depends on**: T20
**Reuses**: nenhum
**Requirement**: SP-06

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Migration aplica limpo
- [x] Índice em `(service_id, fetched_at)` pra consulta de histórico
- [x] Gate check passa: `go test ./... -tags=integration`

**Commit**: `feat(db): add status snapshots migration`
**Tests**: integration
**Gate**: full

---

### T22: Retry com backoff pro fetch de SLO

**What**: Wrapper `FetchWithRetry(ctx, provider, sloID, maxAttempts=3)` com backoff exponencial, só re-tenta erros transitórios (timeout/5xx), não erros de auth (401).
**Where**: `internal/poller/retry.go`
**Depends on**: T21
**Reuses**: `SLOProvider` (T17), erros tipados de T17
**Requirement**: SP-05

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] 3 tentativas em erro transitório antes de desistir
- [ ] Erro 401 não gera retry (falha imediata)
- [ ] Testes unitários cobrem: sucesso na 1ª tentativa, sucesso na 3ª, esgotamento das tentativas, 401 sem retry
- [ ] Gate check passa: `go test ./...`

**Commit**: `feat(poller): add retry with backoff for SLO fetch`
**Tests**: unit
**Gate**: quick

---

### T23: Loop do poller

**What**: `Poller.Run(ctx)` com `time.Ticker` (intervalo de `config.PollIntervalSeconds`), itera serviços com `slo_id` configurado, chama `FetchWithRetry`, persiste snapshot e atualiza `current_status`.
**Where**: `internal/poller/poller.go`
**Depends on**: T22
**Reuses**: `FetchWithRetry` (T22), repositórios de serviço/snapshot
**Requirement**: SP-06, SP-07, SP-08

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Ciclo atualiza `current_status` conforme error budget (operational/degraded/outage)
- [ ] Nunca é chamado a partir de uma requisição pública (só pelo próprio ticker)
- [ ] Cancelamento via context para o loop de forma limpa
- [ ] Testes de integração cobrem uma iteração completa com Datadog mockado
- [ ] Gate check passa: `go test ./... -tags=integration`

**Commit**: `feat(poller): add periodic SLO polling loop`
**Tests**: integration
**Gate**: full

---

### T24: Alerta de falha de conexão pro admin

**What**: Ao esgotar retries, marca `Integration.status = 'invalid'` + `last_error`, expõe via `GET /api/integrations/datadog/status`.
**Where**: `internal/poller/poller.go` (completar), `internal/api/integrations_handler.go` (adicionar rota)
**Depends on**: T23
**Reuses**: `Integration` repository
**Requirement**: SP-09 (lado admin)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Falha esgotada marca integração como inválida com motivo
- [ ] `GET /api/integrations/datadog/status` retorna o estado + último erro pro admin autenticado
- [ ] Último `current_status` do serviço NÃO muda quando a falha é de conexão (mantém último válido)
- [ ] Testes de integração cobrem o fluxo completo de falha
- [ ] Gate check passa: `go test ./... -tags=integration`

**Commit**: `feat(poller): add connection failure alerting`
**Tests**: integration
**Gate**: full

---

### T25: Wiring do poller no `serve`

**What**: `serve` sobe o poller em goroutine própria, cancela via context no shutdown (`SIGTERM`/`SIGINT`).
**Where**: `internal/cli/serve.go`
**Depends on**: T24
**Reuses**: `Poller.Run` (T23)
**Requirement**: SP-06

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `serve` inicia HTTP + poller juntos
- [ ] `SIGTERM` encerra os dois de forma limpa (sem goroutine leak)
- [ ] Gate check passa: `go build ./... && go test ./... -tags=integration`

**Commit**: `feat(poller): periodic SLO polling with retry and failure alerting`
**Tests**: integration
**Gate**: build

---

### T26: Migration `domains`

**What**: Tabela `domains (id, hostname unique, created_at)`.
**Where**: `internal/db/migrations/0006_domains.sql`
**Depends on**: T25
**Reuses**: nenhum
**Requirement**: SP-14

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Migration aplica limpo
- [ ] Constraint unique em `hostname` confirmada
- [ ] Gate check passa: `go test ./... -tags=integration`

**Commit**: `feat(db): add domains migration`
**Tests**: integration
**Gate**: full

---

### T27: Endpoint `POST /api/domains`

**What**: Cadastra domínio raiz, rejeita duplicado com 409.
**Where**: `internal/api/domains_handler.go`
**Depends on**: T26
**Reuses**: middleware `RequireAuth` (T12)
**Requirement**: SP-14 (edge case de duplicado)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Domínio novo → 201
- [ ] Domínio duplicado → 409
- [ ] Rota exige autenticação
- [ ] Testes de integração cobrem os 2 casos
- [ ] Gate check passa: `go test ./... -tags=integration`

**Commit**: `feat(domains): add domain registration endpoint`
**Tests**: integration
**Gate**: full

---

### T28: Migrations `status_pages` + `status_page_services`

**What**: Tabela `status_pages (id, name, subdomain, domain_id, state, tls_last_error, created_at)` + tabela de junção `status_page_services (status_page_id, service_id)`.
**Where**: `internal/db/migrations/0007_status_pages.sql`
**Depends on**: T27
**Reuses**: nenhum
**Requirement**: SP-15

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Migrations aplicam limpo
- [ ] FK `domain_id → domains.id` e `status_page_services` com FKs pros dois lados confirmadas
- [ ] Gate check passa: `go test ./... -tags=integration`

**Commit**: `feat(db): add status pages migrations`
**Tests**: integration
**Gate**: full

---

### T29: Endpoint `POST /api/status-pages`

**What**: Cria status page (nome, subdomínio, domínio, lista de serviços), estado inicial `draft`. Permite múltiplas status pages e múltiplos domínios sem limite técnico.
**Where**: `internal/api/status_pages_handler.go`
**Depends on**: T28
**Reuses**: middleware `RequireAuth` (T12)
**Requirement**: SP-15

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Cria status page vinculada a domínio + serviços
- [ ] Criar uma 2ª status page pra mesma empresa não é bloqueado (sem limite técnico no MVP)
- [ ] Criar um 2º domínio raiz não é bloqueado
- [ ] Testes de integração cobrem os 3 casos
- [ ] Gate check passa: `go test ./... -tags=integration`

**Commit**: `feat(status-pages): add status page creation endpoint`
**Tests**: integration
**Gate**: full

---

### T30: CertMagic manager + HostPolicy

**What**: `internal/tls.NewManager(store)` configura `certmagic.Config` com `OnDemand` + `HostPolicy` que só permite ACME pra hostnames com `StatusPage.State != 'draft'` no Postgres.
**Where**: `internal/tls/manager.go`
**Depends on**: T29
**Reuses**: `StatusPage` repository (T29)
**Requirement**: SP-11, SP-13

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Hostname não cadastrado → `HostPolicy` rejeita (nunca chama ACME pra hostname arbitrário — risco de abuso do design)
- [ ] Hostname cadastrado com state válido → `HostPolicy` permite
- [ ] Storage do CertMagic aponta pra path configurável via env (persistência entre restarts)
- [ ] Testes unitários cobrem os 2 casos de `HostPolicy` (com store fake)
- [ ] Gate check passa: `go test ./...`

**Commit**: `feat(tls): add certmagic manager with host policy`
**Tests**: unit
**Gate**: quick

---

### T31: Listener HTTPS on-demand + transição de estado

**What**: `serve` sobe listener HTTPS via CertMagic; ao emitir certificado com sucesso marca `StatusPage.State = 'published'`; ao falhar marca `'tls_failed'` + `tls_last_error`.
**Where**: `internal/cli/serve.go` (completar), `internal/tls/manager.go` (completar)
**Depends on**: T30
**Reuses**: `HostPolicy` (T30)
**Requirement**: SP-12, SP-13

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Emissão bem-sucedida → estado `published`
- [ ] Emissão falha → estado `tls_failed` com motivo salvo
- [ ] Teste de integração cobre os 2 fluxos (com ACME de teste/staging ou fake)
- [ ] Gate check passa: `go test ./... -tags=integration`

**Commit**: `feat(domains): custom domain registration with automatic TLS provisioning`
**Tests**: integration
**Gate**: full

---

### T32: Router por Host header

**What**: Middleware/dispatcher que decide, pelo `Host` da requisição, se serve API admin, SPA admin (placeholder por enquanto) ou status page pública.
**Where**: `internal/router/host_router.go`
**Depends on**: T31
**Reuses**: `StatusPage` repository (busca por hostname)
**Requirement**: SP-11 (roteamento subjacente)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Host de status page publicada → roteia pro handler público
- [ ] Host não reconhecido → 404
- [ ] Testes de integração cobrem os 2 casos
- [ ] Gate check passa: `go test ./... -tags=integration`

**Commit**: `feat(router): add host-based request routing`
**Tests**: integration
**Gate**: full

---

### T33: Handler público da status page

**What**: `GET /` (no domínio da status page) retorna JSON com serviços + `current_status` + timestamp da última atualização bem-sucedida. Sem autenticação.
**Where**: `internal/api/public_status_handler.go`
**Depends on**: T32
**Reuses**: `Service` repository, `router.Handler` (T32)
**Requirement**: SP-10

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Acessível sem header de autenticação
- [ ] Retorna status + timestamp de última atualização por serviço
- [ ] Teste de integração cobre acesso anônimo
- [ ] Gate check passa: `go test ./... -tags=integration`

**Commit**: `feat(public): add public status page handler`
**Tests**: integration
**Gate**: full

---

### T34: Fallback de cache em falha do Datadog (lado público)

**What**: Handler público nunca chama Datadog direto; em falha de conexão continua servindo o último snapshot válido com timestamp visível de defasagem.
**Where**: `internal/api/public_status_handler.go` (completar)
**Depends on**: T33
**Reuses**: `status_snapshots` (T21), estado de falha de T24
**Requirement**: SP-08, SP-09 (lado público)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Integração marcada como `invalid` não derruba a resposta pública
- [ ] Timestamp exposto reflete a última atualização bem-sucedida real, nunca "agora" fingido
- [ ] Teste de integração simula integração inválida e confirma último status + timestamp corretos
- [ ] Gate check passa: `go test ./... -tags=integration`

**Commit**: `feat(public): add cached fallback on datadog failure`
**Tests**: integration
**Gate**: full

---

### T35: Ocultar serviço `not_configured` da página pública

**What**: Handler público filtra serviços com `current_status = 'not_configured'`.
**Where**: `internal/api/public_status_handler.go` (completar)
**Depends on**: T34
**Reuses**: nenhum novo
**Requirement**: Edge case (serviço sem SLO vinculado)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Serviço `not_configured` nunca aparece na resposta pública
- [ ] Serviço com status válido aparece normalmente
- [ ] Teste de integração cobre os 2 casos
- [ ] Gate check passa: `go test ./... -tags=integration`

**Commit**: `feat(public): serve public status page from cache with host-based routing`
**Tests**: integration
**Gate**: full

---

### T36: Migrations `incidents` + `incident_services` + `incident_updates`

**What**: Tabelas `incidents (id, title, status, created_at, resolved_at)`, `incident_services (incident_id, service_id)`, `incident_updates (id, incident_id, body, created_at)`.
**Where**: `internal/db/migrations/0008_incidents.sql`
**Depends on**: T35
**Reuses**: nenhum
**Requirement**: SP-16

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Migrations aplicam limpo
- [ ] FKs confirmadas
- [ ] Gate check passa: `go test ./... -tags=integration`

**Commit**: `feat(db): add incidents migrations`
**Tests**: integration
**Gate**: full

---

### T37: Endpoint `POST /api/incidents`

**What**: Cria incidente vinculado a 1+ serviços.
**Where**: `internal/api/incidents_handler.go`
**Depends on**: T36
**Reuses**: middleware `RequireAuth` (T12)
**Requirement**: SP-16

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Cria incidente com vínculo a serviços
- [ ] Rota exige autenticação
- [ ] Teste de integração cobre o fluxo
- [ ] Gate check passa: `go test ./... -tags=integration`

**Commit**: `feat(incidents): add incident creation endpoint`
**Tests**: integration
**Gate**: full

---

### T38: Endpoint `POST /api/incidents/{id}/updates`

**What**: Anexa update à timeline do incidente.
**Where**: `internal/api/incidents_handler.go` (completar)
**Depends on**: T37
**Reuses**: `Incident` repository
**Requirement**: SP-17

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Update anexado aparece na timeline ordenado do mais recente pro mais antigo
- [ ] Incidente inexistente → 404
- [ ] Teste de integração cobre os 2 casos
- [ ] Gate check passa: `go test ./... -tags=integration`

**Commit**: `feat(incidents): add incident update timeline endpoint`
**Tests**: integration
**Gate**: full

---

### T39: Endpoint `PATCH /api/incidents/{id}` (transição de estado)

**What**: Muda `status` do incidente (`investigating`/`identified`/`monitoring`/`resolved`), permite reabertura (voltar de `resolved` pra estado anterior).
**Where**: `internal/api/incidents_handler.go` (completar)
**Depends on**: T38
**Reuses**: `Incident` repository
**Requirement**: SP-19, SP-20

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Transição pra `resolved` seta `resolved_at`
- [ ] Reabertura (`resolved` → `investigating`) é permitida e registrada na timeline com timestamp (SP-20)
- [ ] Teste de integração cobre os 2 fluxos
- [ ] Gate check passa: `go test ./... -tags=integration`

**Commit**: `feat(incidents): add incident status transition endpoint`
**Tests**: integration
**Gate**: full

---

### T40: Expor incidentes na página pública

**What**: Handler público (T33/T34) inclui incidentes não resolvidos em destaque no topo e histórico de resolvidos dentro da janela de retenção de 90 dias.
**Where**: `internal/api/public_status_handler.go` (completar)
**Depends on**: T39
**Reuses**: `Incident` repository
**Requirement**: SP-18, retenção (assumption da spec)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Incidente não resolvido aparece em destaque na resposta pública
- [ ] Incidente resolvido há menos de 90 dias aparece no histórico
- [ ] Incidente resolvido há mais de 90 dias não aparece
- [ ] Teste de integração cobre os 3 casos
- [ ] Gate check passa: `go test ./... -tags=integration`

**Commit**: `feat(incidents): manual incident management with public timeline`
**Tests**: integration
**Gate**: full

---

## Phase Execution Map

```
Phase 1 → Phase 2 → Phase 3 → Phase 4 → Phase 5 → Phase 6 → Phase 7

Phase 1:  T1 → T2 → T3 → T4 → T5 → T6 → T7
Phase 2:              T7 → T8 → T9 → T10 → T11 → T12 → T13 → T14
Phase 3:                                                  T14 → T15 → T16 → T17 → T18 → T19 → T20
Phase 4:                                                                              T20 → T21 → T22 → T23 → T24 → T25
Phase 5:                                                                                                  T25 → T26 → T27 → T28 → T29 → T30 → T31
Phase 6:                                                                                                                          T31 → T32 → T33 → T34 → T35
Phase 7:                                                                                                                                      T35 → T36 → T37 → T38 → T39 → T40
```

Execução é estritamente sequencial dentro de cada fase - sem paralelismo intra-fase.

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1-T40 | 1 arquivo/deliverable por task (migration, repositório, endpoint, ou componente único) | ✅ Granular |

Nenhuma task cria mais de 1-2 arquivos cohesivos (ex: migration + índice no mesmo arquivo SQL, handler + rota no mesmo arquivo).

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | início da Phase 1 | ✅ Match |
| T2 | T1 | T1→T2 | ✅ Match |
| T3 | T2 | T2→T3 | ✅ Match |
| T4 | T3 | T3→T4 | ✅ Match |
| T5 | T4 | T4→T5 | ✅ Match |
| T6 | T5 | T5→T6 | ✅ Match |
| T7 | T6 | T6→T7 | ✅ Match |
| T8 | T7 | T7→T8 (cross-fase) | ✅ Match |
| T9 | T8 | T8→T9 | ✅ Match |
| T10 | T9 | T9→T10 | ✅ Match |
| T11 | T10 | T10→T11 | ✅ Match |
| T12 | T11 | T11→T12 | ✅ Match |
| T13 | T12 | T12→T13 | ✅ Match |
| T14 | T13 | T13→T14 | ✅ Match |
| T15 | T14 | T14→T15 (cross-fase) | ✅ Match |
| T16 | T15 | T15→T16 | ✅ Match |
| T17 | T16 | T16→T17 | ✅ Match |
| T18 | T17 | T17→T18 | ✅ Match |
| T19 | T18 | T18→T19 | ✅ Match |
| T20 | T19 | T19→T20 | ✅ Match |
| T21 | T20 | T20→T21 (cross-fase) | ✅ Match |
| T22 | T21 | T21→T22 | ✅ Match |
| T23 | T22 | T22→T23 | ✅ Match |
| T24 | T23 | T23→T24 | ✅ Match |
| T25 | T24 | T24→T25 | ✅ Match |
| T26 | T25 | T25→T26 (cross-fase) | ✅ Match |
| T27 | T26 | T26→T27 | ✅ Match |
| T28 | T27 | T27→T28 | ✅ Match |
| T29 | T28 | T28→T29 | ✅ Match |
| T30 | T29 | T29→T30 | ✅ Match |
| T31 | T30 | T30→T31 | ✅ Match |
| T32 | T31 | T31→T32 (cross-fase) | ✅ Match |
| T33 | T32 | T32→T33 | ✅ Match |
| T34 | T33 | T33→T34 | ✅ Match |
| T35 | T34 | T34→T35 | ✅ Match |
| T36 | T35 | T35→T36 (cross-fase) | ✅ Match |
| T37 | T36 | T36→T37 | ✅ Match |
| T38 | T37 | T37→T38 | ✅ Match |
| T39 | T38 | T38→T39 | ✅ Match |
| T40 | T39 | T39→T40 | ✅ Match |

Nenhuma dependência aponta pra fase futura - todas apontam pra trás ou dentro da mesma fase.

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1, T2 | scaffold/CLI (config/entity-like) | none | none | ✅ OK |
| T3 | domain logic (config) | unit | unit | ✅ OK |
| T4 | domain logic (logger) | unit | none (wrapper fino sobre zap, sem branch lógico próprio) | ✅ OK |
| T5, T6, T7 | repository/data-access + API handler | integration | integration | ✅ OK |
| T8, T15, T19, T21, T26, T28, T36 | migrations | none | integration (aplica migration + confirma constraint) | ✅ OK — nível acima do mínimo, aceitável |
| T9, T13 | repository | integration | integration | ✅ OK |
| T10, T16, T22 | domain logic (crypto/retry) | unit | unit | ✅ OK |
| T11, T12, T14, T18, T20, T24, T27, T29, T37, T38, T39 | API handler | integration | integration | ✅ OK |
| T17 | domain logic (conector) | unit | unit | ✅ OK |
| T23 | domain logic (poller) | integration (cruza com repositório real) | integration | ✅ OK — nível acima do mínimo, aceitável |
| T30 | domain logic (TLS HostPolicy) | unit | unit | ✅ OK |
| T31, T32, T33, T34, T35, T40 | API handler/router | integration | integration | ✅ OK |

Nenhuma violação - todas as tasks que criam camada com teste exigido incluem o teste no mesmo task.

---

## Tips

- Cada task tem seu próprio commit atômico (`implement → gate → commit`, um por task, nunca em lote).
- P3 (histórico de uptime) e o frontend React admin ficam para uma passada de Tasks separada após este backend ser verificado.
