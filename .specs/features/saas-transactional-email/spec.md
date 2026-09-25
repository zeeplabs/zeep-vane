# SaaS Transactional Email Delivery Specification

## Problem Statement

Hoje `internal/email.Service` só sabe enviar contra um provider (Resend/SendGrid) conectado pelo próprio tenant (tabela `email_providers`, escopo por `tenant_id`, RLS via `AD-022`). Isso funciona pra self-hosted (1 tenant, dono conecta sua própria chave), mas quebra o modo SaaS de um jeito real e bloqueante: um tenant SaaS recém-criado via `/signup` não tem nenhum provider conectado ainda, então `SignupHandler.issueAndSendVerification` → `email.Service.SendSignupVerification` → `GetActiveProvider` (escopo por `tenant_id`) sempre retorna `ErrNoActiveProvider` (`internal/api/signup_handler.go:264`). O email de verificação nunca sai; login fica bloqueado enquanto `email_verified_at` é nulo (`internal/api/auth_handler.go:162`); e não existe como o próprio owner recém-criado entrar pra conectar um provider, porque login exige a verificação que nunca chegou. Beco sem saída — confirmado nesta sessão, não pego pelos testes existentes porque `TestSignup_NewEmail_201_...` usa um fixture com provider já ativo, não reproduz o estado real de um tenant zero-provider.

Decisão de negócio (Julio, nesta sessão): em modo SaaS, tenants contratantes do Vane **nunca** conectam provider próprio — o Vane garante o envio de todo email transacional (verificação de signup, reset de senha, convite de admin, incidente aberto, incidente resolvido, resumo semanal) através do `zeep-notification-service` (serviço interno da Zeep, repositório separado), autenticado com uma única credencial de plataforma configurada via env var na infra SaaS da própria Zeep — nunca por tenant. Modo self-hosted continua exatamente como hoje: 1 instalação, 1 tenant, owner conecta seu próprio provider via `EmailProvidersPage`.

`zeep-notification-service` foi verificado diretamente nesta sessão (repo `/Users/juliosousa/Projects/ZeepLabs/zeep/zeep-notification-service`, branch `feat/notifications-service`): todas as 9 fases do seu próprio ciclo de tasks completas (T1-T70), testado (unit + integração via Testcontainers Postgres/RabbitMQ + suíte e2e cenários A-G), com outbox durável, retry/backoff/circuit-breaker/dead-letter, quota preventiva com reserva crítica, e — decisão recente lá, `AD-002` desse repo, adicionada a pedido do usuário nesta mesma sessão — um segundo modo de conteúdo (`content.subject`+`content.html_body`+`content.text_body`, mutuamente exclusivo com `template.key`/`template.locale`, validado via `CHECK` no banco) que aceita HTML já renderizado pelo chamador, sem exigir cadastro prévio de template no serviço. Isso elimina o acoplamento de gerenciar templates em outro repositório: o Vane continua dono de `internal/email/templates.go` como hoje, só troca a entrega final.

## Goals

- [ ] Em modo `saas`, todo envio de email transacional do Vane (as 6 categorias que `internal/email.Service` já produz hoje: signup verification, password reset, admin invite, incident opened, incident resolved, weekly digest) passa a sair via `zeep-notification-service`'s `POST /v1/notifications` (modo `content`, HTML/texto já renderizados localmente pelo Vane), autenticado com uma credencial de plataforma única (env var), nunca por-tenant.
- [ ] Em modo `self_hosted`, nenhuma mudança de comportamento: `email_providers` por-tenant, `EmailProvidersPage`, conexão de chave própria — tudo idêntico a hoje.
- [ ] O bug bloqueante do signup SaaS (verificação nunca enviada, login travado permanentemente) deixa de existir: um tenant SaaS recém-criado consegue completar `/signup` → verificação → login sem nenhuma configuração manual de provider.
- [ ] Em modo `saas`, as rotas `POST /api/integrations/email/{provider}`, `POST /api/integrations/email/{provider}/activate`, `DELETE /api/integrations/email/{provider}` e `GET /api/integrations/email` retornam 404 de verdade (mesmo padrão de `requireSaaSMode`/`AD-033`, mas invertido) — um tenant SaaS nunca consegue conectar/ativar/desconectar um provider próprio, nem via chamada direta de API.
- [ ] Falha de rede/timeout ao chamar `zeep-notification-service` nunca bloqueia a ação de negócio (signup, convite, transição de incidente) — mesmo padrão non-blocking (goroutine destacada, log e segue) já usado hoje em `AdminsHandler`/`PasswordResetHandler`.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Provisionar templates no `zeep-notification-service` | Resolvido pelo modo `content` (`AD-002` daquele repo) — o Vane manda HTML/texto já renderizado, nunca precisa cadastrar template lá. |
| Mudar o conteúdo/visual dos 6 templates existentes (`internal/email/templates.go`) | Fora do escopo — esta feature troca só a camada de transporte/entrega, não o conteúdo do email. |
| Retry/fila própria do lado Vane pra reenviar em caso de falha de rede | O outbox do `zeep-notification-service` já garante entrega eventual (durável, com retry/circuit-breaker/dead-letter reais e testados) — duplicar essa garantia no Vane seria retrabalho e uma segunda fonte de verdade sobre estado de entrega. |
| Migrar `SendIncidentOpened`/`SendIncidentResolved`/`SendWeeklyDigest`/`SendAdminInvite` de self-hosted pra usar `zeep-notification-service` | Self-hosted continua 100% no fluxo `email_providers` por-tenant, sem exceção — nenhuma dessas 4 categorias muda de comportamento fora do modo `saas`. |
| Seat limit / enforcement de plano | Bloqueador cross-repo separado, já registrado em `AD-025` — não relacionado a entrega de email. |
| Provisionamento operacional real do `zeep-notification-service` (deploy, credenciais reais OneSignal/Mailjet, cluster k8s real) | Pendência conhecida e documentada no próprio repo daquele serviço (`.specs/STATE.md` de lá) — fora do escopo de código do Vane; é pré-requisito operacional pra ir live, não uma tarefa desta spec. |
| Badge/aviso no `EmailProvidersPage`/telas de organização explicando por que a tela sumiu em SaaS | Fora do pedido explícito; a tela simplesmente não existe nesse modo (mesmo tratamento dado às rotas de signup em self-hosted). |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Comportamento em falha de rede/timeout do `zeep-notification-service` | Fire-and-forget: goroutine destacada, loga erro, segue sem bloquear a ação de negócio nem duplicar retry | Confirmado via `AskUserQuestion` nesta sessão, à luz do outbox real e testado daquele serviço — mesmo padrão non-blocking já estabelecido em `AdminsHandler.sendAdminInviteEmail`/`PasswordResetHandler.Request`. | y |
| Gate de `EmailProvidersPage`/rotas de integração de email em modo SaaS | 404 real no backend (não só ocultar na UI) | Confirmado via `AskUserQuestion` — mesmo padrão de `requireSaaSMode`/`AD-033` já usado pras rotas de signup em self-hosted; uma chamada direta de API não pode contornar a garantia de "Vane sempre garante o envio". | y |
| Provisionamento de template no `zeep-notification-service` | Fora de escopo — Vane usa exclusivamente o modo `content` (HTML/texto inline) | Resolvido por `AD-002` daquele repo, adicionada nesta mesma sessão a pedido do usuário especificamente pra remover esse acoplamento. | y |
| Nome/forma exata da credencial de plataforma (env vars) | A decidir em Design — provavelmente `VANE_NOTIFICATION_SERVICE_BASE_URL` + `VANE_NOTIFICATION_SERVICE_API_KEY`, mesmo padrão `VANE_*` de `internal/config/config.go`, exigidos apenas quando `VANE_DEPLOYMENT_MODE=saas` | Não é uma decisão de produto, é um detalhe de nomenclatura técnica — não vale gastar uma pergunta ao usuário; Design confirma o nome final contra a convenção real do arquivo. | n — default técnico, revisitar em Design |
| Mapeamento de `category`/`priority` das 6 categorias de email pro contrato do `zeep-notification-service` (`category: transactional\|marketing`, `priority: critical\|normal\|low`) | signup-verification e password-reset (gatilhos de login) → `category=transactional`, `priority=critical`; admin-invite/incident-opened/incident-resolved → `category=transactional`, `priority=normal`; weekly-digest → `category=marketing`, `priority=low` | Signup/reset são os dois casos que travam login se não chegarem — merecem a reserva crítica de quota do roteador daquele serviço; os demais são importantes mas não bloqueiam acesso; digest é o único não-urgente. | n — default técnico razoável, revisitar em Design se o time de operação da Zeep quiser outro mapeamento |
| Abstração de código pra alternar entre a implementação self-hosted (`email_providers` por-tenant) e a implementação SaaS (`zeep-notification-service`) | A decidir em Design — hoje todo handler (`AdminsHandler`, `SignupHandler`, `PasswordResetHandler`, etc.) guarda um `*email.Service` concreto, não uma interface; provavelmente precisa de uma interface nova (`email.Sender` ou similar) que ambas implementações satisfaçam, escolhida no boot conforme `cfg.DeploymentMode` | Puramente arquitetural (COMO), não WHAT — fica pro Design decidir o shape exato sem prejudicar esta spec. | n/a — decisão de Design, não de Specify |
| Granularidade de tenant no `zeep-notification-service` | O Vane inteiro (todas as empresas SaaS que ele hospeda) é UM único tenant lá (ex.: `tenant_id=vane-saas`), não um tenant por empresa cliente do Vane | Tenants do `zeep-notification-service` existem pra isolar produtos/clientes da própria Zeep entre si (quota, suppression, credencial) — não faz sentido nem é necessário espelhar cada empresa-cliente do Vane como um tenant separado lá; suppression/routing daquele serviço já operam por destinatário (email), que já é único por natureza. | n — default técnico razoável, revisitar em Design se o time de operação da Zeep discordar |

**Open questions:** none — todas resolvidas ou registradas acima.

---

## User Stories

### P1: Signup SaaS deixa de travar por falta de provider ⭐ MVP

**User Story**: Como pessoa criando uma conta nova no Vane SaaS, quero receber o email de verificação mesmo sem nenhum provider de email configurado no meu tenant recém-criado, para conseguir completar o cadastro e fazer login.

**Why P1**: É o bug bloqueante real — sem isso, `/signup` em modo SaaS é uma funcionalidade morta (cria a conta, mas ninguém nunca consegue entrar nela).

**Acceptance Criteria**:

1. WHILE `deployment_mode == saas`, `email.Service.SendSignupVerification` (ou seu equivalente pós-Design) SHALL enviar o email de verificação através do `zeep-notification-service`, nunca consultando `email_providers`/`GetActiveProvider` por-tenant. <!-- state-driven -->
2. WHEN o envio via `zeep-notification-service` retorna sucesso (2xx) THEN o sistema SHALL registrar o envio como concluído, mesmo comportamento de log/observabilidade já existente pra outros envios de email. <!-- event-driven -->
3. IF a chamada ao `zeep-notification-service` falhar (erro de rede, timeout, resposta não-2xx) THEN o sistema SHALL logar o erro e retornar da função sem bloquear a criação do tenant/user/membership já commitados, exatamente como o comportamento non-blocking de hoje. <!-- unwanted-behavior -->
4. WHILE `deployment_mode == self_hosted`, `SendSignupVerification` SHALL continuar usando exclusivamente `email_providers` por-tenant, sem nenhuma chamada ao `zeep-notification-service`. <!-- state-driven -->

**Independent Test**: em ambiente SaaS de teste (sem nenhum `email_providers` conectado pro tenant novo), completar `POST /api/signup` e confirmar que o `zeep-notification-service` recebeu uma notificação `content` com o assunto/corpo de verificação, e que `GET /api/signup/verify/{token}` com o token emitido libera o login.

---

### P1: As outras 5 categorias de email seguem o mesmo roteamento por modo ⭐ MVP

**User Story**: Como operação da Zeep rodando o Vane em modo SaaS, quero que todo email transacional (não só signup) saia de forma garantida pelo `zeep-notification-service`, para nunca depender de um tenant configurar algo que ele não tem permissão de configurar.

**Why P1**: Signup é o caso mais grave (trava login), mas os outros 5 (password reset, admin invite, incident opened/resolved, weekly digest) têm exatamente o mesmo problema estrutural — sem provider por-tenant em SaaS, todos falhariam do mesmo jeito silencioso hoje.

**Acceptance Criteria**:

1. WHILE `deployment_mode == saas`, `SendPasswordReset`, `SendAdminInvite`, `SendIncidentOpened`, `SendIncidentResolved` e `SendWeeklyDigest` SHALL todos enviar através do `zeep-notification-service`, com o mesmo tratamento non-blocking de falha do AC3 da story anterior. <!-- state-driven -->
2. WHILE `deployment_mode == self_hosted`, essas 5 categorias SHALL continuar exatamente como hoje (`email_providers` por-tenant, sem mudança de comportamento). <!-- state-driven -->
3. The system SHALL usar, para cada categoria, o `content.subject`/`content.html_body`/`content.text_body` já renderizado pelo `internal/email/templates.go` existente, sem introduzir um segundo motor de renderização nem depender de template cadastrado no `zeep-notification-service`. <!-- ubiquitous -->
4. The system SHALL autenticar toda chamada ao `zeep-notification-service` com uma única credencial de plataforma (env var, carregada no boot), nunca com uma credencial por-tenant. <!-- ubiquitous -->

**Independent Test**: em modo SaaS, disparar cada uma das 5 ações (reset de senha, convite de admin, abrir incidente, resolver incidente, disparo do digest semanal) e confirmar que todas chegam como notificações `content` no `zeep-notification-service`, com o `category`/`priority` correspondentes.

---

### P1: Tenant SaaS nunca consegue conectar provider próprio ⭐ MVP

**User Story**: Como operação da Zeep, quero que um tenant SaaS nunca tenha como conectar/ativar/desconectar um provider de email próprio, para que a garantia "Vane sempre garante o envio" não possa ser contornada por uma chamada de API direta.

**Why P1**: Sem esse gate, o backend continua tecnicamente aceitando `POST /api/integrations/email/{provider}` em SaaS — um tenant poderia conectar sua própria chave e criar uma segunda fonte de verdade sobre como seus emails são enviados, contradizendo a decisão de negócio.

**Acceptance Criteria**:

1. WHILE `deployment_mode == saas`, `POST /api/integrations/email/{provider}`, `POST /api/integrations/email/{provider}/activate`, `DELETE /api/integrations/email/{provider}` e `GET /api/integrations/email` SHALL responder 404, sem tocar `EmailProvidersHandler`. <!-- state-driven -->
2. WHILE `deployment_mode == self_hosted`, essas 4 rotas SHALL se comportar exatamente como hoje, sem nenhuma mudança de contrato. <!-- state-driven -->
3. WHILE `deployment_mode == saas`, o frontend (`EmailProvidersPage`/item de navegação correspondente) SHALL deixar de ser exibido, mesmo padrão de sinal (`deployment_mode` já exposto por `GET /api/bootstrap/status`, feature `deployment-mode`) já usado pro link de "Criar conta". <!-- state-driven -->

**Independent Test**: com `VANE_DEPLOYMENT_MODE=saas`, `curl -X POST /api/integrations/email/resend` retorna 404; com `VANE_DEPLOYMENT_MODE=self_hosted` (ou ausente), o mesmo `curl` funciona como hoje.

---

## Edge Cases

- IF o `zeep-notification-service` estiver configurado (env vars presentes) mas a URL/credencial for inválida THEN o sistema SHALL falhar o boot com um erro claro, mesmo padrão de validação estrita já usado pras demais env vars de `internal/config/config.go` — nunca subir em modo SaaS silenciosamente incapaz de enviar email.
- IF `deployment_mode == saas` e as env vars do `zeep-notification-service` estiverem ausentes THEN o sistema SHALL falhar o boot (não faz sentido rodar SaaS sem o único canal de envio disponível nesse modo).
- WHEN o mesmo evento é reenviado de propósito pelo usuário (ex.: `POST /api/signup/resend-verification`, `AdminsHandler.ResendInvite`) THEN o sistema SHALL gerar uma nova chamada com sua própria identidade de conteúdo (novo token/nova instância do evento) — não é responsabilidade desta feature deduplicar reenvios intencionais, só evitar que uma falha de rede vire reenvio automático duplicado do lado Vane (que não existe, já que não há retry no lado Vane).
- IF o corpo HTML/texto de algum dos 6 templates ultrapassar o limite de tamanho combinado que o `zeep-notification-service` aceita pro modo `content` (1 MB, `AD-002`/P1b item 5 daquele repo) THEN a chamada SHALL falhar com um erro do lado do `zeep-notification-service` — tratado pelo mesmo caminho non-blocking do AC3 da primeira story (log e segue), já que nenhum dos 6 templates atuais do Vane se aproxima desse tamanho.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| SAASMAIL-01 | P1: Signup SaaS deixa de travar | Execute (T1: boot fails without env vars in saas mode) | Partial - edge case only, envio real ainda pendente (T4/T6/T7) |
| SAASMAIL-02 | P1: Signup SaaS deixa de travar | Design | Pending |
| SAASMAIL-03 | P1: Signup SaaS deixa de travar | Design | Pending |
| SAASMAIL-04 | P1: Signup SaaS deixa de travar | Design | Pending |
| SAASMAIL-05 | P1: As outras 5 categorias | Execute (T4: NotificationServiceSender, todas as 6 categorias) | Partial - Sender implementado e testado, wiring de boot pendente (T6) |
| SAASMAIL-06 | P1: As outras 5 categorias | Execute (T4: NotificationServiceSender, todas as 6 categorias) | Partial - Sender implementado e testado, wiring de boot pendente (T6) |
| SAASMAIL-07 | P1: As outras 5 categorias | Execute (T4: NotificationServiceSender, todas as 6 categorias) | Partial - Sender implementado e testado, wiring de boot pendente (T6) |
| SAASMAIL-08 | P1: As outras 5 categorias | Design | Pending |
| SAASMAIL-09 | P1: Tenant SaaS nunca conecta provider próprio | Execute (T5: requireSelfHostedMode) | Verified |
| SAASMAIL-10 | P1: Tenant SaaS nunca conecta provider próprio | Execute (T5: requireSelfHostedMode) | Verified |
| SAASMAIL-11 | P1: Tenant SaaS nunca conecta provider próprio | Execute (T5: requireSelfHostedMode) | Verified |

**ID format:** `SAASMAIL-NN`

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 11 total, 0 mapped to tasks, 11 unmapped ⚠️ (Tasks phase ainda não rodou)

---

## Success Criteria

- [ ] Um tenant SaaS novo, criado via `/signup` sem nenhum provider de email conectado, recebe o email de verificação e consegue fazer login — reprodução direta do bug fechado por esta feature.
- [ ] Nenhuma das 6 categorias de email em modo self-hosted muda de comportamento (mesma suíte de testes de `internal/email`/`internal/api` de hoje continua verde sem alteração).
- [ ] `curl` direto contra as 4 rotas de `EmailProvidersHandler` em modo SaaS retorna 404.
- [ ] Falha simulada do `zeep-notification-service` (endpoint indisponível) não impede a criação de tenant/user/membership em `/signup`, nem a transição de um incidente, nem a criação de um convite de admin.
