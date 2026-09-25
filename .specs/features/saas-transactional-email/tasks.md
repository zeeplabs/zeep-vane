# SaaS Transactional Email Delivery Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/saas-transactional-email/design.md`
**Status**: Draft

---

## Test Coverage Matrix

> Gerado a partir do repositório (`AGENTS.md` §3, `Makefile`, amostragem de `internal/connectors/resend/client_test.go`, `internal/config/config_test.go`, `internal/cli/routes_test.go`, `internal/api/signup_handler_test.go`) e da spec. Nenhuma migration nesta feature - camada de dados fora de escopo.

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| `internal/config` (2 env vars novas) | unit | Todo branch: presente+válido, presente+inválido, ausente+`self_hosted` (ok), ausente+`saas` (falha boot) - mesmo piso de profundidade de `config_test.go` já existente pras demais env vars condicionais | `internal/config/config_test.go` | `go test ./internal/config/...` |
| `internal/connectors/notificationservice` (cliente HTTP novo) | unit | Mesma profundidade de `internal/connectors/resend/client_test.go`: envio válido (payload/headers corretos), 401/403→`ErrUnauthorized`, timeout→`ErrTimeout`, 5xx→`ErrServer`, 400/409/413/422→erro genérico logado | `internal/connectors/notificationservice/client_test.go` | `go test ./internal/connectors/notificationservice/...` |
| `internal/email` (contrato + `NotificationServiceSender`, domínio/lógica de negócio) | unit | 1:1 com SAASMAIL-01/03/04/05/06/07/08: as 6 categorias mapeiam `category`/`priority`/`type` corretos (tabela do design), idempotency-key determinístico (mesmo conteúdo → mesma chave, conteúdo diferente → chave diferente), erro do client propagado sem modificação (non-blocking) | `internal/email/notification_service_sender_test.go` | `go test ./internal/email/...` |
| `internal/cli` (middleware `requireSelfHostedMode`, wiring `newEmailSender`/`buildAdminRouter`/`newNotifyService`) | integration (`//go:build integration`, mesma tag de `routes_test.go` - roteador real + Postgres) | Todas as 4 rotas de `EmailProvidersHandler`: 404 em `saas`, comportamento inalterado em `self_hosted` (SAASMAIL-09/10/11); wiring de `newEmailSender` exercitado indiretamente pelos testes de handler já existentes que continuam verdes | `internal/cli/routes_test.go` | `make test-integration` |
| `internal/api` (regressão do bug: signup SaaS sem provider) | unit (fake `email.Sender` injetado direto no handler, sem tocar banco/HTTP real) | Prova direta de SAASMAIL-01/02: `SignupHandler` em modo `saas` nunca consulta `email_providers`/`GetActiveProvider`, envia via `email.Sender` mesmo com zero providers conectados | `internal/api/signup_handler_test.go` | `go test ./internal/api/...` |
| `README.md` (tabela de configuração) | none | Documentação apenas - sem gate de teste, só revisão de conteúdo | `README.md` | build gate only (nenhum comando de teste) |

## Gate Check Commands

> Gerado a partir de `AGENTS.md` §3 e `Makefile`.

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick | Após tasks com só teste unitário (config, connector, email, api) | `go build ./... && go test ./... && go vet ./... && gofmt -l <arquivos alterados>` |
| Full | Após a task que toca `internal/cli` (roteador real + Postgres) | `go build ./... && go test ./... && go vet ./... && gofmt -l <arquivos alterados> && make test-integration` |
| Docs | Após a task de `README.md` | leitura manual - sem comando |

---

## Execution Plan

Phases são ordenadas e rodam em sequência - cada fase completa antes da próxima começar, e as tasks dentro de uma fase rodam em ordem.

### Phase 1: Foundation

```
T1 (sem dependência)
T2 (sem dependência)
```

### Phase 2: Core

```
T2 → T3
T2 → T4
```

### Phase 3: Wiring

```
T5 (sem dependência)
T1 → T6
T3 → T6
T4 → T6
```

### Phase 4: Regression + Docs

```
T6 → T7
T1 → T8
```

---

## Task Breakdown

### T1: Env vars de plataforma em `internal/config`

**What**: Adiciona `NotificationServiceBaseURL`/`NotificationServiceAPIKey` a `Config`; `Load()` lê `VANE_NOTIFICATION_SERVICE_BASE_URL`/`VANE_NOTIFICATION_SERVICE_API_KEY` via `os.Getenv`, exigindo ambas presentes (erro claro no boot) somente quando `deploymentMode == saas`; ignoradas/opcionais em `self_hosted`.
**Where**: `internal/config/config.go`
**Depends on**: None
**Reuses**: Padrão de validação condicional já usado pra `deploymentMode`/`VANE_ADMIN_BASE_URL` (`config.go:83-178`)
**Requirement**: SAASMAIL-01 (edge case: boot falha sem as env vars em modo SaaS)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `Config` ganha os 2 campos novos
- [x] `saas` + ambas setadas → boot ok, campos populados
- [x] `saas` + qualquer uma ausente → `Load()` retorna erro claro
- [x] `self_hosted` + ambas ausentes → boot ok (comportamento inalterado)
- [x] Gate check passes: `go build ./... && go test ./internal/config/... && go vet ./internal/config/... && gofmt -l internal/config/config.go`
- [x] Traceability de SAASMAIL-01 (edge case) atualizada em `spec.md`

**Tests**: unit
**Gate**: quick

**Commit**: `feat(config): add platform notification-service env vars, required in saas mode`

---

### T2: Contrato compartilhado `NotificationServiceRequest`/`NotificationServiceClient`

**What**: Novo arquivo com o struct `NotificationServiceRequest` (TenantKey, Category, Priority, Type, RecipientEmail, Subject, HTMLBody, TextBody, IdempotencyKey) e a interface `NotificationServiceClient` (`Send(ctx, req) error`), ambos em `internal/email` (dono do contrato, mesma direção de dependência de `Message`/`Provider`). Sem lógica - só tipos.
**Where**: `internal/email/notification_service.go`
**Depends on**: None
**Reuses**: `email.ErrUnauthorized`/`ErrTimeout`/`ErrServer` já existentes (`internal/email/provider.go:133-140`) - documentados como os erros que a implementação do client deve retornar, sem redeclarar

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Tipos definidos e exportados, compilam sem erro
- [x] Comentário no arquivo aponta que a implementação concreta vive em `internal/connectors/notificationservice` (T3), nunca o inverso
- [x] Gate check passes: `go build ./... && go vet ./internal/email/... && gofmt -l internal/email/notification_service.go`

**Tests**: none (camada de tipo puro, sem lógica - mesmo tratamento de config/entity da matriz)
**Gate**: quick

**Commit**: `feat(email): define NotificationServiceRequest/Client contract`

---

### T3: `internal/connectors/notificationservice.Client`

**What**: Cliente HTTP que implementa `email.NotificationServiceClient` contra `POST {baseURL}/v1/notifications` (modo `content`, nunca `template`): monta `channel=email`, `tenant_id`, `category`, `priority`, `type`, `recipient.email`, `content.subject`/`content.html_body`/`content.text_body`; header `Authorization: Bearer <apiKey>` + `Idempotency-Key: <req.IdempotencyKey>`; classifica a resposta (`202`→sucesso; `401`/`403`→`email.ErrUnauthorized`; timeout→`email.ErrTimeout`; `5xx`→`email.ErrServer`; `400`/`409`/`413`/`422`→erro genérico com o corpo `application/problem+json` embutido pra log).
**Where**: `internal/connectors/notificationservice/client.go`
**Depends on**: T2
**Reuses**: Forma idêntica de `internal/connectors/resend/client.go` (`do`/`post`/`isTimeout` privados, `defaultTimeout = 10 * time.Second`)
**Requirement**: SAASMAIL-05, SAASMAIL-08 (autenticação de plataforma única)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `NewClient(baseURL, apiKey string) *Client` construído
- [x] `Send` monta o payload de conteúdo inline corretamente (nunca `template.key`)
- [x] Testes cobrindo: envio válido (payload/headers corretos, incluindo `Idempotency-Key`), 401/403, timeout, 5xx, 400/409/413/422 (`httptest.Server` fake, mesmo padrão de `resend/client_test.go`)
- [x] Chave de API nunca aparece em nenhum log/erro retornado
- [x] Gate check passes: `go build ./... && go test ./internal/connectors/notificationservice/... && go vet ./internal/connectors/notificationservice/... && gofmt -l internal/connectors/notificationservice/client.go internal/connectors/notificationservice/client_test.go`
- [x] Test count: >= 6 testes passam (mínimo, um por cenário de classificação) — 7 passam

**Tests**: unit
**Gate**: quick

**Commit**: `feat(connectors): add zeep-notification-service HTTP client`

---

### T4: `email.NotificationServiceSender`

**What**: Novo tipo implementando as 6 assinaturas de `email.Sender`. Cada método: renderiza via `s.templates.renderX(data)` (privado já existente, reaproveitado - `NewNotificationServiceSender` chama `parseTemplates()` internamente, mesmo fail-fast-at-boot de `NewService`), monta `NotificationServiceRequest` com `category`/`priority`/`type` conforme a tabela do design (signup-verification/password-reset → `transactional`/`critical`; admin-invite/incident-opened/incident-resolved → `transactional`/`normal`; weekly-digest → `marketing`/`low`), calcula `IdempotencyKey = hex(sha256(recipient + "|" + subject + "|" + htmlBody))`, chama `s.client.Send(ctx, req)`. Erro do client retornado ao chamador sem modificação (non-blocking, contrato já assumido por todo chamador existente).
**Where**: `internal/email/notification_service_sender.go`
**Depends on**: T2
**Reuses**: `templates.render*` privados (`internal/email/templates.go`), os 6 tipos `*EmailData` existentes (`provider.go`), `parseTemplates()` (mesmo padrão de `NewService`)
**Requirement**: SAASMAIL-01, SAASMAIL-05, SAASMAIL-06, SAASMAIL-07

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `NewNotificationServiceSender(client, tenantKey, logger) (*NotificationServiceSender, error)` construído, erro só se `parseTemplates()` falhar
- [x] As 6 categorias mapeiam `category`/`priority`/`type` exatamente conforme a tabela do design (1 teste por categoria)
- [x] Idempotency-key: mesmo `to`+`subject`+`html` → mesma chave; conteúdo diferente → chave diferente (2 testes mínimo)
- [x] Falha do client (fake retornando erro) é propagada ao chamador sem wrapping que esconda o erro original de classificação (`errors.Is` continua funcionando)
- [x] `NotificationServiceSender` satisfaz `email.Sender` em tempo de compilação (`var _ email.Sender = (*NotificationServiceSender)(nil)`)
- [x] Gate check passes: `go build ./... && go test ./internal/email/... && go vet ./internal/email/... && gofmt -l internal/email/notification_service_sender.go internal/email/notification_service_sender_test.go`
- [x] Test count: >= 10 testes passam (6 categorias + 2 idempotência + falha propagada + assertion de compilação) — 11 passam
- [x] Traceability de SAASMAIL-05/06/07 atualizada em `spec.md`

**Tests**: unit
**Gate**: quick

**Commit**: `feat(email): add NotificationServiceSender implementing email.Sender`

---

### T5: `requireSelfHostedMode` + gate das 4 rotas `EmailProvidersHandler`

**What**: Middleware espelho inverso de `requireSaaSMode` (404 quando `deploymentMode == saas`); aplicado às 4 rotas `POST/POST activate/DELETE/GET /api/integrations/email*`. Ajusta `TestAdminRouter_Viewer_EmailProvidersList_200` (que hoje roda contra o harness compartilhado, deliberadamente `DeploymentMode: saas` - `routes_test.go:36-40`) pra construir seu próprio roteador em `self_hosted`, mesmo padrão local já usado por `TestAdminRouter_DeleteTenant_SelfHostedMode_404` - senão essa asserção de 200 quebra pelo próprio gate que esta task introduz. Adiciona teste novo de 404 em modo SaaS pras 4 rotas, mesmo padrão de `TestAdminRouter_SignupRoutes_SelfHostedMode_404` (invertido: SaaS → 404, não self-hosted → 404).
**Where**: `internal/cli/routes.go`, `internal/cli/routes_test.go`
**Depends on**: None
**Reuses**: `requireSaaSMode` (`routes.go:311-323`) como espelho exato; `TestAdminRouter_DeleteTenant_SelfHostedMode_404`/`TestAdminRouter_SignupRoutes_SelfHostedMode_404` como precedente de construção de roteador dedicado por modo
**Requirement**: SAASMAIL-09, SAASMAIL-10, SAASMAIL-11

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `requireSelfHostedMode(deploymentMode string) func(http.Handler) http.Handler` criado e aplicado às 4 rotas
- [x] `TestAdminRouter_Viewer_EmailProvidersList_200` corrigido pra construir roteador `self_hosted` dedicado (continua verde)
- [x] Novo teste `TestAdminRouter_EmailProvidersRoutes_SaaSMode_404` prova as 4 rotas retornando 404 em `saas`
- [x] Comportamento em `self_hosted` continua idêntico ao de hoje pras 4 rotas (asserção explícita)
- [x] Gate check passes: `go build ./... && go test ./... && go vet ./... && gofmt -l internal/cli/routes.go internal/cli/routes_test.go && make test-integration`
- [x] Traceability de SAASMAIL-09/10/11 atualizada em `spec.md`

**Tests**: integration
**Gate**: full

**Commit**: `feat(cli): 404 EmailProviders routes in saas mode via requireSelfHostedMode`

---

### T6: `newEmailSender` + wiring de `buildAdminRouter`/`newNotifyService`

**What**: Nova função compartilhada `newEmailSender(cfg, pool, logger) (email.Sender, error)` que escolhe `*email.Service` (self-hosted, código idêntico ao de hoje) ou `*email.NotificationServiceSender` (saas, via T3+T4) conforme `cfg.DeploymentMode`. `internal/cli/routes.go`'s `buildAdminRouter` e `internal/cli/serve.go`'s `newNotifyService` passam a chamar essa factory em vez de duplicar `email.NewService(...)`. `AdminsHandler`/`SignupHandler` têm seu campo/parâmetro widened de `*email.Service` pra `email.Sender` (mecânico - `PasswordResetHandler`/`notify.Service` já usavam a interface). `EmailProvidersHandler` continua recebendo especificamente o `*email.Service` self-hosted sempre construído (nunca o `email.Sender` escolhido por modo) - suas rotas já são gated pela T5.
**Where**: `internal/cli/routes.go`, `internal/cli/serve.go`, `internal/api/admins.go`, `internal/api/signup_handler.go`
**Depends on**: T1, T3, T4
**Reuses**: Corpo exato do `email.NewService(...)` de hoje pro branch self-hosted; mesma injeção de dependência já usada nos outros construtores
**Requirement**: SAASMAIL-01, SAASMAIL-02, SAASMAIL-03, SAASMAIL-04, SAASMAIL-05, SAASMAIL-06, SAASMAIL-07, SAASMAIL-08

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `newEmailSender` construído e usado nos 2 pontos de wiring, eliminando a duplicação de `email.NewService(...)`
- [x] `AdminsHandler`/`SignupHandler` compilam contra `email.Sender`, zero mudança de comportamento em `self_hosted` (suíte de testes existente de `internal/api` continua 100% verde sem nenhuma edição de asserção)
- [x] `saas`: `buildAdminRouter`/`newNotifyService` recebem um `*NotificationServiceSender` de fato quando `VANE_NOTIFICATION_SERVICE_BASE_URL`/`_API_KEY` estão setadas
- [x] `EmailProvidersHandler` inalterado (continua só com o `*email.Service` self-hosted)
- [x] Gate check passes: `go build ./... && go test ./... && go vet ./... && gofmt -l internal/cli/routes.go internal/cli/serve.go internal/api/admins.go internal/api/signup_handler.go && make test-integration`
- [x] Test count: suíte completa de `internal/api`/`internal/cli` sem nenhum teste removido, todos verdes (flake pré-existente e não relacionado — `TestPublicStatusGet_HourlyHistory_KnownHourStatusAppearsAsSingleBucket`, teste dependente de wall-clock cruzando fronteira de hora — confirmado 3x verde isolado e verde na re-corrida completa)

**Tests**: integration
**Gate**: full

**Commit**: `feat(cli): wire deployment-mode-aware email sender via newEmailSender`

---

### T7: Regressão - signup SaaS sem provider deixa de travar

**What**: Novo teste em `internal/api/signup_handler_test.go` que constrói `SignupHandler` com um `email.Sender` fake (implementando as 6 assinaturas, sem depender de `email_providers`/`GetActiveProvider` de forma alguma) e prova que `POST /api/signup` envia a verificação e que `GET /api/signup/verify/{token}` libera o login - reprodução direta do bug fechado por esta feature, num tenant que nunca teve nenhum provider conectado. Prova explicitamente que `TestSignup_NewEmail_201_...` (fixture com provider já ativo) não é mais o único caminho testado.
**Where**: `internal/api/signup_handler_test.go`
**Depends on**: T6
**Reuses**: Helpers existentes de `signup_handler_test.go` (`newSignupRouterWithEmail` ou equivalente, adaptado pro tipo de interface)
**Requirement**: SAASMAIL-01, SAASMAIL-02

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Novo teste prova envio + verificação + login liberado, com um `email.Sender` fake que nunca consulta `email_providers`
- [ ] Teste falharia se o bug original (dependência de `GetActiveProvider`) fosse reintroduzido
- [ ] Gate check passes: `go build ./... && go test ./internal/api/... && go vet ./internal/api/... && gofmt -l internal/api/signup_handler_test.go`
- [ ] Traceability de SAASMAIL-01/02 marcada "Verified" em `spec.md`

**Tests**: unit
**Gate**: quick

**Commit**: `test(api): prove saas signup verification never depends on email_providers`

---

### T8: Documentação - `README.md`

**What**: Adiciona `VANE_NOTIFICATION_SERVICE_BASE_URL`/`VANE_NOTIFICATION_SERVICE_API_KEY` à tabela de [Configuration](README.md#-configuration), com nota de que são obrigatórias apenas quando `VANE_DEPLOYMENT_MODE=saas`.
**Where**: `README.md`
**Depends on**: T1
**Reuses**: Formato existente da tabela de configuração (mesma linha por env var, coluna de default/obrigatoriedade)
**Requirement**: N/A (documentação, `AGENTS.md` §6)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Tabela atualizada com as 2 env vars novas, obrigatoriedade condicional documentada
- [ ] Nenhuma outra seção do README fica desatualizada por esta mudança

**Tests**: none
**Gate**: docs (leitura manual, sem comando)

**Commit**: `docs(readme): document notification-service platform env vars`

---

## Phase Execution Map

```
Phase 1: T1 (sem dependência), T2 (sem dependência)
Phase 2:
T2 → T3
T2 → T4
Phase 3: T5 (sem dependência)
T1 → T6
T3 → T6
T4 → T6
Phase 4:
T6 → T7
T1 → T8
```

Execução é estritamente sequencial - sem paralelismo intra-fase. Um agente (ou batch worker) trabalha uma task por vez, em ordem.

Com 8 tasks, tudo cabe num único batch (≤ ~8 tasks) - execução inline, sem oferta de sub-agentes.

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1: Env vars de plataforma | 1 arquivo (`config.go`) | ✅ Granular |
| T2: Contrato `NotificationServiceRequest`/`Client` | 1 arquivo novo, só tipos | ✅ Granular |
| T3: `notificationservice.Client` | 1 arquivo novo (+ teste) | ✅ Granular |
| T4: `NotificationServiceSender` | 1 arquivo novo (+ teste) | ✅ Granular |
| T5: `requireSelfHostedMode` + gate de rotas | 2 arquivos (`routes.go` + seu próprio `routes_test.go`), mesmo par testado-e-teste de toda outra task de rota nesta base | ✅ Granular (par cohesivo, mesmo padrão de `requireSaaSMode`) |
| T6: `newEmailSender` + wiring | 4 arquivos (`routes.go`, `serve.go`, `admins.go`, `signup_handler.go`) | ⚠️ Aceito como task única - é uma mudança de wiring atômica por natureza (Go exige o pacote inteiro compilando; dividir produziria um estado intermediário que não compila, exatamente o caso que a regra de "merge forward/merge backward" deste skill recomenda unir em vez de fragmentar) |
| T7: Regressão signup SaaS | 1 arquivo (`signup_handler_test.go`) | ✅ Granular |
| T8: Documentação README | 1 arquivo (`README.md`) | ✅ Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | Nenhuma seta de entrada | ✅ Match |
| T2 | None | Nenhuma seta de entrada | ✅ Match |
| T3 | T2 | `T2 → T3` (Fase 2) | ✅ Match |
| T4 | T2 | `T2 → T4` (Fase 2) | ✅ Match |
| T5 | None | Nenhuma seta de entrada (Fase 3) | ✅ Match |
| T6 | T1, T3, T4 | `T1 → T6`, `T3 → T6`, `T4 → T6` (Fase 3) | ✅ Match |
| T7 | T6 | `T6 → T7` (Fase 4) | ✅ Match |
| T8 | T1 | `T1 → T8` (Fase 4) | ✅ Match |

**Nota**: T5 não depende de nenhuma task anterior - roda na Fase 3 por proximidade de escopo (rotas/`internal/cli`), não por dependência de dado; a execução continua estritamente sequencial (uma task por vez), a fase é só agrupamento semântico. Nenhuma task depende de uma task de fase posterior (regra respeitada).

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1: Env vars | `internal/config` | unit | unit | ✅ OK |
| T2: Contrato | `internal/email` (tipos puros) | none | none | ✅ OK |
| T3: Client HTTP | `internal/connectors/notificationservice` | unit | unit | ✅ OK |
| T4: Sender | `internal/email` (domínio) | unit | unit | ✅ OK |
| T5: Middleware + gate | `internal/cli` (roteador) | integration | integration | ✅ OK |
| T6: Wiring | `internal/cli` (roteador) | integration | integration | ✅ OK |
| T7: Regressão | `internal/api` | unit | unit | ✅ OK |
| T8: README | docs | none | none | ✅ OK |

---

## Tips

- **Task 6 é a única com múltiplos arquivos** - deliberado, é uma mudança de wiring que não compila fatiada; justificado na Granularity Check.
- **Nenhuma migration** - toda a feature é código de aplicação, credencial de plataforma nunca persistida em banco.
- **T5 antes de T6 na sequência** - T5 é independente de T1/T3/T4, mas roda antes porque é menor e não bloqueia T6; poderia rodar em paralelo com Fases 1-2 se este skill suportasse paralelismo real entre fases (não suporta - execução é sempre sequencial).
