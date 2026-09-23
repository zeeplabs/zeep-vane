# SaaS Transactional Email Delivery Design

**Spec**: `.specs/features/saas-transactional-email/spec.md`
**Status**: Draft

---

## Architecture Overview

Uma segunda implementação de `email.Sender` (interface já existente, `internal/email/provider.go:43-50`) entra ao lado da que já existe (`*email.Service`, self-hosted). Um fator de decisão único, no boot, escolhe qual implementação cada consumidor recebe — nenhum consumidor (`SignupHandler`, `PasswordResetHandler`, `AdminsHandler`, `notify.Service`) muda de comportamento além de trocar o tipo do campo que guarda de `*email.Service` pra `email.Sender` (2 deles já usam a interface hoje; só `AdminsHandler`/`SignupHandler` seguem no tipo concreto).

```mermaid
graph TD
    Boot["internal/cli.newEmailSender(cfg, pool, logger)"] -->|deployment_mode=self_hosted| SelfHosted["*email.Service (inalterado)"]
    Boot -->|deployment_mode=saas| SaaSSender["*email.NotificationServiceSender (novo)"]

    SelfHosted -->|Provider ativo do tenant| ProviderFactory["email.ProviderFactory (Resend/SendGrid)"]
    SaaSSender -->|HTTP: POST /v1/notifications, modo content| NSClient["internal/connectors/notificationservice.Client (novo)"]
    NSClient -->|Bearer zns_live_...| NS["zeep-notification-service"]

    SignupHandler["SignupHandler"] -->|email.Sender| Boot
    PasswordResetHandler["PasswordResetHandler"] -->|email.Sender, já era interface| Boot
    AdminsHandler["AdminsHandler"] -->|email.Sender| Boot
    NotifyService["notify.Service"] -->|email.Sender, já era interface local| Boot

    EmailProvidersHandler["EmailProvidersHandler (self-hosted only)"] -->|sempre o *email.Service self-hosted, nunca o Sender de SaaS| SelfHosted
    Routes["4 rotas /api/integrations/email/*"] -->|requireSelfHostedMode (novo, inverso de requireSaaSMode)| EmailProvidersHandler
```

---

## Code Reuse Analysis

### Existing Components to Leverage

| Component | Location | How to Use |
| --- | --- | --- |
| `email.Sender` interface | `internal/email/provider.go:43-50` | Já existe com as 6 assinaturas exatas necessárias — nenhuma mudança nesse arquivo. `PasswordResetHandler` já depende dela; `AdminsHandler`/`SignupHandler` passam a depender também (troca de `*email.Service` pro tipo de interface, sem mudança de comportamento). |
| `internal/email/templates.go`'s `parseTemplates()`/`render*` privados | `internal/email/templates.go` | `NotificationServiceSender` (novo) parseia seu próprio `*templates` via `parseTemplates()` (mesmo fail-fast-at-boot de `NewService`) e chama os mesmos métodos privados `renderAdminInvite`/`renderSignupVerification`/etc. — zero duplicação de conteúdo, já que o novo tipo vive no mesmo pacote `email`. |
| Padrão de conector dedicado (`internal/connectors/resend/client.go`) | `internal/connectors/resend/client.go` | `internal/connectors/notificationservice` (novo) espelha exatamente essa forma: `Client{baseURL, apiKey, httpClient}`, `do`/`post` privados que classificam timeout/401/403/5xx nos erros tipados já existentes de `internal/email` (`ErrTimeout`, `ErrUnauthorized`, `ErrServer`) — reaproveita os mesmos 3 valores em vez de inventar um quarto conjunto de erros. |
| Padrão de injeção por factory/interface estreita (`ProviderFactory`, `EmailProviderStore`) | `internal/email/provider.go:39`, `internal/email/service.go:35-42` | `NotificationServiceSender` depende de uma interface estreita nova (`NotificationServiceClient`, definida em `internal/email`, não no pacote do conector) — mesmo motivo de desacoplamento: `internal/email` nunca importa `internal/connectors/*` diretamente; é o conector que importa `internal/email` pros tipos compartilhados (exatamente como `internal/connectors/resend` já importa `email.Message`/`email.Provider` hoje). |
| `requireSaaSMode` | `internal/cli/routes.go:311-323` | Novo `requireSelfHostedMode` é o espelho exato (inverte a condição), mesma assinatura `func(deploymentMode string) func(http.Handler) http.Handler`. |
| `config.Load()`'s padrão de validação estrita no boot | `internal/config/config.go:83-178` | As 2 env vars novas seguem o mesmo estilo: leitura com `os.Getenv`, validação condicionada a `deploymentMode == saas`, erro claro no boot em vez de falha silenciosa em runtime. |

### Integration Points

| Sistema | Método de integração |
| --- | --- |
| `zeep-notification-service` | HTTP `POST /v1/notifications`, modo `content` (não `template`), autenticado com `Authorization: Bearer <VANE_NOTIFICATION_SERVICE_API_KEY>`, header `Idempotency-Key` obrigatório por request. |
| `internal/cli/routes.go` (`buildAdminRouter`) e `internal/cli/serve.go` (`newNotifyService`) | Ambos os pontos de construção hoje duplicam `email.NewService(...)` — passam a chamar uma função compartilhada nova `newEmailSender(cfg, pool, logger) (email.Sender, error)` em vez de repetir a lógica de escolha em dois lugares. |

---

## Components

### `internal/connectors/notificationservice.Client` (novo)

- **Purpose**: cliente HTTP de baixo nível pro `zeep-notification-service`, sem lógica de negócio — só monta a request, autentica, classifica a resposta.
- **Location**: `internal/connectors/notificationservice/client.go`
- **Interfaces**:
  - `NewClient(baseURL, apiKey string) *Client` — mesma forma de `resend.NewClient`.
  - `Send(ctx context.Context, req email.NotificationServiceRequest) error` — monta o payload (`channel: "email"`, `content.subject/html_body/text_body`, `category`, `priority`, `type`, `recipient.email`), envia `POST {baseURL}/v1/notifications` com `Authorization: Bearer` + `Idempotency-Key`, classifica a resposta: `202` sucesso; `401`/`403` → `email.ErrUnauthorized`; timeout de rede/contexto → `email.ErrTimeout`; `5xx` → `email.ErrServer`; qualquer outro código (`400`/`409`/`413`/`422` — payload malformado ou idempotency-key reaproveitada com payload diferente) → erro genérico envolvendo o corpo `application/problem+json` pra log, nunca repassado ao usuário final.
- **Dependencies**: `net/http`, timeout fixo (`defaultTimeout = 10 * time.Second`, mesmo valor de `resend.Client`).
- **Reuses**: forma idêntica de `internal/connectors/resend/client.go` (`do`/`post`/`isTimeout` privados); importa `internal/email` só pelos tipos compartilhados (`NotificationServiceRequest`, os 3 erros tipados) — nunca o inverso.

### `internal/email.NotificationServiceRequest` + `NotificationServiceClient` (novo, em `internal/email`)

- **Purpose**: contrato compartilhado entre `internal/email` (dono) e `internal/connectors/notificationservice` (implementador), mesma forma de `Message`/`Provider` hoje.
- **Location**: novo arquivo `internal/email/notification_service.go`
- **Interfaces**:
  ```go
  type NotificationServiceRequest struct {
      TenantKey      string
      Category       string // "transactional" | "marketing"
      Priority       string // "critical" | "normal" | "low"
      Type           string // ex.: "SIGNUP_VERIFICATION"
      RecipientEmail string
      Subject        string
      HTMLBody       string
      TextBody       string
      IdempotencyKey string
  }

  type NotificationServiceClient interface {
      Send(ctx context.Context, req NotificationServiceRequest) error
  }
  ```
- **Dependencies**: nenhuma nova — só tipos primitivos.
- **Reuses**: `email.ErrUnauthorized`/`ErrTimeout`/`ErrServer` já existentes (`provider.go:133-140`) continuam sendo os únicos erros tipados que atravessam a fronteira do conector.

### `internal/email.NotificationServiceSender` (novo, implementa `email.Sender`)

- **Purpose**: implementação de `email.Sender` pro modo SaaS — renderiza os mesmos templates locais e entrega via `zeep-notification-service`, nunca consultando `email_providers`.
- **Location**: novo arquivo `internal/email/notification_service_sender.go`
- **Interfaces**:
  - `NewNotificationServiceSender(client NotificationServiceClient, tenantKey string, logger *zap.Logger) (*NotificationServiceSender, error)` — erro só se `parseTemplates()` falhar (mesmo fail-fast-at-boot de `NewService`).
  - As 6 assinaturas de `email.Sender` (`SendAdminInvite`, `SendPasswordReset`, `SendSignupVerification`, `SendIncidentOpened`, `SendIncidentResolved`, `SendWeeklyDigest`) — cada uma: renderiza via `s.templates.renderX(data)` (método privado já existente, reaproveitado tal como `Service` usa), monta `NotificationServiceRequest` com a categoria/prioridade/tipo mapeados (tabela abaixo, `spec.md` Assumptions), calcula `IdempotencyKey` (ver Tech Decisions), chama `s.client.Send(ctx, req)`. Erro é retornado ao chamador sem modificação — o chamador (SignupHandler etc.) já trata falha de envio como não-fatal, comportamento inalterado.
- **Dependencies**: `NotificationServiceClient` (injetado), `*templates` (parseado internamente).
- **Reuses**: `templates.render*` privados, os 6 tipos `*EmailData` de `provider.go` (sem nenhuma mudança neles).

### `internal/cli.newEmailSender` (novo, factory compartilhada)

- **Purpose**: ponto único de decisão self-hosted vs SaaS, chamado pelos 2 lugares que hoje duplicam `email.NewService(...)`.
- **Location**: novo arquivo `internal/cli/email_sender.go` (ou inline em `routes.go`, decisão de Tasks)
- **Interfaces**: `newEmailSender(cfg config.Config, pool *db.Pool, logger *zap.Logger) (email.Sender, error)`.
  - `self_hosted`: constrói e retorna `email.NewService(db.NewEmailProviderRepository(pool), emailProviderFactory, cfg.MasterKey, logger)` — idêntico ao código atual.
  - `saas`: constrói `notificationservice.NewClient(cfg.NotificationServiceBaseURL, cfg.NotificationServiceAPIKey)`, depois `email.NewNotificationServiceSender(client, "vane-saas", logger)`.
- **Dependencies**: `config.Config` (2 campos novos), `db.Pool`, `zap.Logger`.
- **Reuses**: substitui a duplicação hoje existente entre `routes.go`'s `buildAdminRouter` e `serve.go`'s `newNotifyService`, sem mudar o resultado do caso self-hosted.

### `internal/cli.requireSelfHostedMode` (novo middleware)

- **Purpose**: 404 real nas 4 rotas de `EmailProvidersHandler` quando `deployment_mode == saas`.
- **Location**: `internal/cli/routes.go`, ao lado de `requireSaaSMode`
- **Interfaces**: `requireSelfHostedMode(deploymentMode string) func(http.Handler) http.Handler` — mesma forma de `requireSaaSMode`, condição invertida (`deploymentMode == config.DeploymentModeSaaS` → 404).
- **Reuses**: espelho direto de `requireSaaSMode` (`routes.go:311-323`), zero lógica nova.

---

## Data Models

### `internal/email.NotificationServiceRequest` (ver Components acima)

Sem persistência — struct transitória, construída e descartada por chamada. Nenhuma migration, nenhuma tabela nova.

### `internal/config.Config` (2 campos novos)

```go
NotificationServiceBaseURL string
NotificationServiceAPIKey  string
```

`VANE_NOTIFICATION_SERVICE_BASE_URL` / `VANE_NOTIFICATION_SERVICE_API_KEY`, lidas via `os.Getenv` (mesmo padrão de `VANE_ADMIN_BASE_URL`), validadas como obrigatórias **somente quando** `deploymentMode == saas` (boot falha com erro claro se ausentes nesse modo); ignoradas/opcionais em `self_hosted` (mesmo se setadas por engano, nunca usadas nesse modo).

**Relacionamento**: nenhuma persistência em `tenants`/`email_providers` — a credencial vive só em memória, nunca criptografada/armazenada como as chaves por-tenant (`email_providers.encrypted_api_key`), porque não é uma credencial de tenant.

---

## Mapeamento categoria/prioridade/tipo (spec.md Assumptions, confirmado como default técnico)

| Categoria Vane | `category` | `priority` | `type` |
| --- | --- | --- | --- |
| Signup verification | `transactional` | `critical` | `SIGNUP_VERIFICATION` |
| Password reset | `transactional` | `critical` | `PASSWORD_RESET` |
| Admin invite | `transactional` | `normal` | `ADMIN_INVITE` |
| Incident opened | `transactional` | `normal` | `INCIDENT_OPENED` |
| Incident resolved | `transactional` | `normal` | `INCIDENT_RESOLVED` |
| Weekly digest | `marketing` | `low` | `WEEKLY_DIGEST` |

---

## Error Handling Strategy

| Cenário de erro | Tratamento | Impacto pro usuário |
| --- | --- | --- |
| `zeep-notification-service` indisponível/timeout | `NotificationServiceSender.SendX` retorna erro; chamador (goroutine destacada em `SignupHandler`/`PasswordResetHandler`/`AdminsHandler`/`notify.Service`, já non-blocking hoje) loga e segue — mesmo contrato de hoje. | Ação de negócio (signup/reset/convite/incidente) completa normalmente; email pode atrasar (outbox do outro serviço garante entrega eventual quando ele se recuperar). |
| `401`/`403` (credencial de plataforma inválida/revogada) | Classificado como `email.ErrUnauthorized`, logado com nível de erro (nunca a chave em si). | Nenhum email SaaS sai enquanto a credencial não for corrigida — falha visível só nos logs/observabilidade do Vane, não pro usuário final imediatamente (mesma característica non-blocking; requer alerta operacional externo a esta spec). |
| `400`/`409`/`413`/`422` (payload malformado, idempotency-key reusada com payload diferente, corpo grande demais, recipient suprimido) | Logado com o `Problem` retornado; tratado como qualquer outro erro de envio (non-blocking). | Nenhum, mesmo caminho non-blocking. Nenhum destes é esperado em operação normal (todos os 6 payloads são gerados internamente pelo Vane, não por input de usuário direto no corpo). |
| `VANE_NOTIFICATION_SERVICE_BASE_URL`/`_API_KEY` ausentes com `deployment_mode=saas` | Boot falha com erro claro (`config.Load`), mesmo padrão de `VANE_DEPLOYMENT_MODE` inválido. | Instalação SaaS nunca sobe incapaz de enviar email — falha rápida, não silenciosa. |

---

## Risks & Concerns

| Concern | Location (file:line) | Impact | Mitigation |
| --- | --- | --- | --- |
| `TestSignup_NewEmail_201_...` usa um fixture com provider já ativo — não reproduz o bug real (tenant SaaS zero-provider) | `internal/api/signup_handler_test.go:138` (aprox., conforme citado em `validation.md`/investigação desta sessão) | O teste atual passaria mesmo que o bug voltasse a existir — falso positivo de cobertura. | Tasks deve incluir um teste novo especificamente em modo SaaS usando um `NotificationServiceClient` fake, provando envio sem nenhuma linha em `email_providers`. |
| `zeep-notification-service` tem pendências operacionais reais não resolvidas por código (credenciais OneSignal/Mailjet nunca validadas contra ambiente real, quotas do seed são exemplo, nenhum cluster k8s real usado) — `.specs/STATE.md` daquele repo | repositório externo, fora deste código | Uma instalação SaaS real pode enfrentar limite de quota mal calibrado ou falha de provider secundário não testada em produção antes do go-live. | Fora do escopo desta spec (é responsabilidade operacional do outro repo/time) — mitigado do lado Vane pelo fato de que o Vane é inteiramente agnóstico a qual provider aquele serviço escolhe (roteamento/fallback já são passados adiante integralmente); nenhuma mudança de código no Vane resolveria isso. |
| Duas chamadas independentes a `parseTemplates()` (uma em `email.NewService`, outra em `email.NewNotificationServiceSender`) quando só uma implementação é usada por boot | `internal/email/service.go:80-88`, novo `notification_service_sender.go` | Nenhum custo real (parsing de `go:embed` na inicialização, uma vez) além de uma pequena duplicação de trabalho no boot. | Aceito deliberadamente — extrair um `*templates` compartilhado exigiria acoplar as duas implementações ou reestruturar `NewService`, mais complexidade do que o ganho justifica pra um custo de boot desprezível. |
| Credencial de plataforma (`VANE_NOTIFICATION_SERVICE_API_KEY`) fica em memória sem a mesma criptografia-em-repouso que `email_providers.encrypted_api_key` tem hoje | novo `internal/config/config.go` | Não é uma regressão de segurança real (a chave nunca é persistida em banco, só vive em variável de ambiente/memória do processo, igual `VANE_MASTER_KEY`/`VANE_SESSION_SECRET` já vivem hoje) — mas deve nunca ser logada. | `Client`/`NotificationServiceSender` nunca logam o valor da chave em nenhum campo, mesma regra já seguida por `resend.Client`/`openai` connectors. Validar em Tasks com um teste de log-redaction se o padrão de teste existente pra isso for reaproveitável. |

> Nenhum outro concern de fragilidade/tech debt/performance identificado nas áreas tocadas.

---

## Tech Decisions (only non-obvious ones)

| Decision | Choice | Rationale |
| --- | --- | --- |
| Como gerar `Idempotency-Key` por envio | Hash determinístico do conteúdo: `sha256(recipient + "\|" + subject + "\|" + htmlBody)`, truncado/hex | Evita threading de um novo parâmetro de ID por 6 assinaturas de método (nenhum `*EmailData` hoje carrega um ID de evento natural). Uma chamada verdadeiramente duplicada (mesmo destinatário+assunto+corpo) dedupe; um reenvio legítimo e intencional (`ResendInvite`, `resend-verification`) naturalmente gera um corpo diferente (token novo embutido na URL) e portanto uma chave diferente — sem precisar de lógica extra de deduplicação do lado Vane. |
| Onde vive o contrato compartilhado (`NotificationServiceRequest`/`NotificationServiceClient`) | Em `internal/email`, não em `internal/connectors/notificationservice` | Mantém a mesma direção de dependência já estabelecida por `Message`/`Provider`/`ProviderFactory`: `internal/email` nunca importa um pacote de conector concreto; é o conector que importa `internal/email`. |
| Granularidade de tenant no `zeep-notification-service` | Uma única `TenantKey` constante (`"vane-saas"`) pra todas as empresas hospedadas pelo Vane SaaS | Já registrado como assumption em `spec.md` — tenants daquele serviço isolam produtos/clientes da Zeep entre si, não precisam espelhar cada empresa-cliente do Vane. |
| `EmailProvidersHandler` continua ligado ao `*email.Service` self-hosted sempre construído, nunca ao `email.Sender` escolhido por modo | Duas variáveis de boot distintas: a self-hosted `*email.Service` (sempre construída, barata, sem I/O) e o `email.Sender` de fato injetado nos handlers de envio (escolhido por modo) | Evita qualquer mudança em `EmailProvidersHandler`/seu construtor — suas 4 rotas continuam gated por `requireSelfHostedMode` no nível de middleware, nunca alcançáveis em SaaS de qualquer forma; menor blast radius que redesenhar esse handler pra aceitar ausência de provider. |
| Nome dos 2 campos novos de env var | `VANE_NOTIFICATION_SERVICE_BASE_URL`, `VANE_NOTIFICATION_SERVICE_API_KEY` | Segue a convenção `VANE_*` já usada por toda env var opcional/condicional de `internal/config/config.go` (`VANE_ADMIN_BASE_URL`, `VANE_DEPLOYMENT_MODE`, etc.). |

> **Decisão de nível de projeto**: a divisão `email.Sender` self-hosted vs SaaS por `deployment_mode`, escolhida em um ponto único de boot (`newEmailSender`), estabelece o padrão que qualquer integração externa futura condicionada a modo de distribuição deve seguir — será registrada como `AD-034` em `.specs/STATE.md` ao final desta fase de Design.
