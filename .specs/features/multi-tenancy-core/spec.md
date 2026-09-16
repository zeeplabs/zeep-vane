# Multi-Tenancy Core Specification

## Problem Statement

Vane é hoje single-tenant por design (AD-002, superseded por AD-022): 1 instalação self-hosted atende exatamente 1 empresa, sem coluna de tenant em nenhuma tabela. O negócio decidiu suportar dois modelos de distribuição sobre a mesma base de código — self-hosted (como hoje, 1 tenant único auto-provisionado, licença anual) e SaaS (multi-tenant real, signup público, planos pagos). Nenhum dos dois funciona sem uma fundação de tenant isolando dado corretamente. Esta é essa fundação.

## Goals

- [ ] Toda tabela de domínio isola dado por tenant via RLS do Postgres, fail-closed (query sem `app.tenant_id` setado não retorna linha nenhuma)
- [ ] Self-hosted continua funcionando com zero fricção nova: bootstrap cria tenant único automaticamente, sem UI de seleção de tenant
- [ ] SaaS tem signup público funcional: cria tenant + owner + membership, bloqueia acesso até verificação de email
- [ ] Convite de membros do time (mecanismo já existente em `internal/api/admins.go`) funciona corretamente escopado por tenant, incluindo o caso de um usuário que já existe em outro tenant

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
| --- | --- |
| Licença self-hosted (servidor de licença Zeep, código de assinatura, renovação) | Subsistema seguinte, depende do tenant já existir — ver AD-022 |
| Billing/subscription real (Stripe, planos pagos, limites de uso de LLM) | Subsistema seguinte; usa `tenants.plan` já modelado aqui mas sem enforcement |
| Connector do notification-service Zeep | Subsistema de notificação; bloqueia especificamente o signup público do SaaS até existir (ver Assumptions) |
| Aplicação real de `locale`/`primary_color`/`secondary_color` na UI (motor de tema/white-label) | Colunas existem (groundwork), sem consumo nesta feature |
| Roteamento por subdomínio (`tenants.slug`) | Coluna reservada; decisão foi domínio único + seleção de tenant por conta |
| Migração retrocompatível de instalação self-hosted existente com dado real | Não há cliente self-hosted externo em produção hoje (AD-022); schema é recriado do zero |
| Suspensão/reativação de tenant (`status = suspended`) | Coluna existe para uso futuro (billing inadimplente, moderação); sem fluxo/endpoint nesta feature |
| Consultor com múltiplos tenants sendo removido de um deles enquanto sessão ativa nesse tenant | Edge case de segunda ordem; tratado como Deferred — sessão expira/rejeita no próximo request via RLS (owner sem membership não vê nada), sem necessidade de invalidação ativa |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Poller e outros jobs de background acessam dado de qual tenant? | Poller itera tenant-por-tenant explicitamente, setando `app.tenant_id` a cada iteração (nunca um role com `BYPASSRLS`) | Mantém um único caminho de enforcement (RLS sempre ligado); um role que ignora RLS reintroduz exatamente o risco de vazamento que a RLS existe pra eliminar | n |
| Quem pode convidar/gerenciar membros do tenant? | Só role `owner` (mesma regra hoje aplicada a ações administrativas, `Sidebar.tsx:143` `hasRole(["owner"])`) | Consistente com o padrão já existente no dashboard; convite/remoção de membro é ação sensível o bastante pra não abrir pra `operator` | y (inferido do padrão já shipado) |
| Formato de validação de `tax_id` | Validação de comprimento/dígitos básica na camada de aplicação (11 dígitos CPF / 14 dígitos CNPJ), sem validação de dígito verificador nesta feature | Campo é opcional e informativo (usado por billing futuro para nota fiscal); validação completa de CPF/CNPJ é regra de negócio de billing, não desta fundação | n |
| Formato de `primary_color`/`secondary_color` | Hex de 6 dígitos (`#RRGGBB`), validado só se preenchido | Sem motor de tema consumindo ainda; validação mínima evita lixo óbvio no dado sem inventar regra de negócio de tema | n |
| Falha ao enviar email de verificação durante signup | Tenant/user/membership já commitados no banco (signup não é revertido); endpoint de reenvio (mesmo padrão de `ResendInvite`) permite tentar de novo | Reverter o signup inteiro por falha transitória de envio de email cria um estado pior (usuário não sabe se deve tentar de novo do zero); reenviar é mais simples e já tem precedente no código | n |
| Rate limit / anti-abuso no signup público | Rate limit por IP na origem, reaproveitando o mesmo mecanismo de rate limiting já existente no projeto (`RemoteAddr`, nunca `X-Forwarded-For`, por AGENTS.md §4) | Endpoint público sem auth é superfície natural de abuso (criar tenants em massa); projeto já tem convenção de rate limit por IP de conexão pra outros endpoints públicos | n |
| Token de convite de tenant reaproveita TTL/hash de hoje? | Sim, 1h de TTL, hash do token armazenado — sem mudança no mecanismo, só adiciona `tenant_id` | Mecanismo já validado em produção (`admin_invites`); mudar TTL/hashing não foi discutido nem tem motivo novo | y |
| `company_settings` unificada em `tenants` | Colunas (`name`, `contact_email`, `logo_data`, `logo_content_type`) migram para `tenants`; tabela `company_settings` é removida | Descoberto durante Specify (Knowledge Verification Chain step 1): `company_settings` já era exatamente o caso "1 linha por instalação" que vira "1 linha por tenant" — duplicar como tabela separada 1:1 seria redundante | y (correção técnica, não decisão de produto — sem necessidade de confirmação do usuário) |

**Open questions:** none — todas resolvidas ou registradas acima.

---

## User Stories

### P1: Isolamento de dado por tenant via RLS ⭐ MVP

**User Story**: Como operador da plataforma (self-hosted ou SaaS), quero que dado de um tenant nunca seja visível/gravável por outro tenant, mesmo se um repository esquecer o filtro, para que um bug de código nunca vire um incidente de vazamento de dado entre clientes.

**Why P1**: Sem isolamento garantido no banco, multi-tenancy real é uma promessa vazia — qualquer feature construída em cima herda o risco.

**Acceptance Criteria**:

1. The system SHALL aplicar policy RLS em toda tabela com coluna `tenant_id` (services, incidents, status_pages, llm_provider_config, email_provider_config, domains, admin_audit_log, tenants, tenant_memberships, tenant_invites). <!-- ubiquitous -->
2. WHEN uma request autenticada chega THEN o middleware SHALL executar `SET LOCAL app.tenant_id` com o tenant ativo da sessão antes de qualquer query de domínio na mesma transação. <!-- event-driven -->
3. IF uma query roda numa transação sem `app.tenant_id` setado THEN o Postgres SHALL retornar zero linhas para qualquer tabela com RLS (fail-closed), nunca erro de permissão nem linhas de outro tenant. <!-- unwanted-behavior -->
4. WHILE o poller executa fora do contexto de uma request HTTP, o sistema SHALL setar `app.tenant_id` explicitamente antes de processar cada tenant, iterando um de cada vez (nunca com `BYPASSRLS`). <!-- state-driven -->

**Independent Test**: Criar 2 tenants via seed de teste, popular `services` em cada um, autenticar como membro do tenant A, chamar `GET /api/services` e confirmar que só os serviços do tenant A retornam — mesmo com uma query de teste que deliberadamente omite `WHERE tenant_id`.

---

### P1: Self-hosted provisiona tenant único no bootstrap ⭐ MVP

**User Story**: Como gestor de infra rodando Vane self-hosted, quero que o primeiro `/bootstrap` já crie meu tenant automaticamente, para que eu nunca precise entender ou interagir com o conceito de "tenant" — a experiência continua sendo "eu instalo, eu uso".

**Why P1**: Self-hosted é o modelo que já roda em produção (teste Starbem); não pode regredir.

**Acceptance Criteria**:

1. WHEN `POST /api/bootstrap` é chamado com zero admins existentes THEN o sistema SHALL criar, na mesma transação, 1 row em `tenants` e 1 `tenant_membership` (role `owner`) ligando o novo `user` a esse tenant. <!-- event-driven -->
2. The system SHALL nunca exibir seletor de tenant na UI quando o usuário logado tem exatamente 1 `tenant_membership`. <!-- ubiquitous -->
3. IF `/bootstrap` é chamado quando já existe ao menos 1 admin THEN o sistema SHALL recusar com o mesmo comportamento 4xx já existente hoje, sem criar tenant duplicado. <!-- unwanted-behavior -->

**Independent Test**: Subir instância nova (banco vazio), chamar `/bootstrap`, confirmar 1 tenant + 1 membership criados, logar e confirmar ausência de UI de troca de tenant.

---

### P1: Signup público SaaS com verificação de email obrigatória ⭐ MVP

**User Story**: Como visitante do site do Vane SaaS, quero criar minha conta e meu tenant sozinho, para começar a usar o produto sem depender de alguém da Zeep me provisionar manualmente.

**Why P1**: É o motion de aquisição self-serve decidido para o v1 do SaaS.

**Acceptance Criteria**:

1. WHEN `POST /api/signup` recebe email + senha + nome do tenant válidos THEN o sistema SHALL criar `tenants` (`plan = free`), criar ou reaproveitar `users` pelo email, criar `tenant_memberships` (role `owner`) e disparar email de verificação, tudo na mesma transação exceto o envio de email. <!-- event-driven -->
2. WHILE `users.email_verified_at` é nulo, o sistema SHALL recusar login para esse usuário com uma mensagem indicando que a verificação de email é necessária. <!-- state-driven -->
3. WHEN o usuário clica no link de verificação de email dentro do prazo THEN o sistema SHALL marcar `email_verified_at` e permitir login normalmente a partir desse ponto. <!-- event-driven -->
4. IF o email informado no signup já pertence a um `user` verificado THEN o sistema SHALL criar o novo tenant e uma nova `tenant_membership` (role `owner`) para esse `user` existente, sem pedir senha nova nem duplicar `users`. <!-- unwanted-behavior -->
5. IF o envio do email de verificação falhar (provider indisponível) THEN o sistema SHALL manter o tenant/user/membership já criados e permitir reenvio via endpoint dedicado, análogo ao `ResendInvite` já existente. <!-- unwanted-behavior -->
6. IF a mesma origem (IP de conexão) exceder o rate limit de signups num período THEN o sistema SHALL recusar novas tentativas com 429, sem criar tenant. <!-- unwanted-behavior -->

**Independent Test**: Chamar `/api/signup` com email novo, confirmar tenant+user+membership criados e login bloqueado; clicar no link de verificação (via token capturado em teste) e confirmar login liberado.

---

### P1: Convite de membro do time escopado por tenant ⭐ MVP

**User Story**: Como owner de um tenant, quero convidar colegas com papéis diferentes (operator/viewer), incluindo alguém que já tem conta em outro tenant, para montar meu time sem cada pessoa precisar de conta nova.

**Why P1**: Multi-usuário por tenant já era requisito (AD-003); sem isso o tenant fica preso a 1 pessoa só.

**Acceptance Criteria**:

1. WHEN um `owner` chama `POST /api/admins/invite` THEN o sistema SHALL criar `tenant_invites` com o `tenant_id` do tenant ativo da sessão do owner. <!-- event-driven -->
2. IF o email convidado já corresponde a um `user` existente (verificado) THEN `AcceptInvite` SHALL criar apenas a `tenant_membership` (sem pedir senha) e redirecionar para login. <!-- unwanted-behavior -->
3. IF o email convidado não existe como `user` THEN `AcceptInvite` SHALL manter o fluxo atual (define senha na aceitação), criando `user` + `tenant_membership`. <!-- unwanted-behavior -->
4. The system SHALL restringir `Invite`/`List`/`UpdateRole`/`Delete`/`ResendInvite`/`CancelInvite` para operarem apenas sobre membros/convites do tenant ativo da sessão de quem chama. <!-- ubiquitous -->
5. IF o token de convite estiver expirado (> 1h) THEN `AcceptInvite` SHALL recusar com o mesmo comportamento já existente hoje. <!-- unwanted-behavior -->

**Independent Test**: Owner do tenant A convida email X (novo) — aceita, vira membro do tenant A. Owner do tenant B convida o mesmo email X — aceita sem pedir senha, agora X tem membership em A e B; login de X mostra seletor de tenant.

---

### P2: Troca de tenant ativo na sessão

**User Story**: Como usuário com acesso a mais de 1 tenant (ex.: consultor), quero trocar de tenant ativo sem deslogar, para alternar de contexto rapidamente.

**Why P2**: Só é relevante para o caso (esperado ser raro no v1) de conta com múltiplas memberships; não bloqueia o motion principal de nenhum dos dois modelos.

**Acceptance Criteria**:

1. WHEN o login é bem-sucedido e o usuário tem mais de 1 `tenant_membership` THEN o sistema SHALL apresentar a tela de seleção de tenant antes de liberar o dashboard. <!-- event-driven -->
2. WHEN `POST /api/auth/switch-tenant` é chamado com um `tenant_id` que o usuário tem membership THEN o sistema SHALL atualizar o cookie de sessão para esse tenant sem exigir novo login. <!-- event-driven -->
3. IF `switch-tenant` é chamado com um `tenant_id` sem membership do usuário THEN o sistema SHALL recusar com 403, sem alterar a sessão atual. <!-- unwanted-behavior -->

**Independent Test**: Usuário com membership em tenant A e B loga, seleciona A, chama switch-tenant para B, confirma que requests seguintes enxergam dado de B.

---

### P2: Perfil da empresa por tenant (dados fiscais)

**User Story**: Como owner (self-hosted ou SaaS), quero preencher razão social/CNPJ-CPF opcionalmente, para ter esse dado pronto quando billing/nota fiscal precisar dele.

**Why P2**: Não bloqueia nenhum fluxo do v1 — é groundwork de schema com uma tela simples de edição.

**Acceptance Criteria**:

1. WHEN um `owner` chama `PATCH` na tela de dados da empresa com `legal_name`/`tax_id`/`tax_id_type` THEN o sistema SHALL persistir os campos em `tenants` (todos opcionais/nullable). <!-- event-driven -->
2. IF `tax_id_type = cpf` e `tax_id` não tem 11 dígitos, ou `tax_id_type = cnpj` e `tax_id` não tem 14 dígitos THEN o sistema SHALL recusar o PATCH com erro de validação, sem persistir. <!-- unwanted-behavior -->
3. WHERE o modelo é self-hosted, `billing_address` SHALL nunca ser exposto na UI de edição (campo existe no schema, sem tela). <!-- optional-feature -->

**Independent Test**: Owner self-hosted preenche CNPJ válido, confirma persistência; tenta CPF de 5 dígitos, confirma rejeição.

---

### P3: Groundwork de locale e branding (schema apenas)

**User Story**: Como futuro consumidor de tema/idioma por tenant, quero que as colunas já existam, para não precisar de nova migration quando essa feature for construída.

**Why P3**: Zero UI consome isso nesta feature; puro schema.

**Acceptance Criteria**:

1. The system SHALL ter as colunas `locale` (default `pt-BR`), `primary_color` e `secondary_color` (nullable) em `tenants`, sem nenhuma tela ou lógica de aplicação as consumindo nesta feature. <!-- ubiquitous -->
2. IF `primary_color` ou `secondary_color` forem preenchidos via API futura fora desta feature THEN o formato esperado SHALL ser hex de 6 dígitos (`#RRGGBB`) — documentado aqui para a feature que vier a validar. <!-- unwanted-behavior -->

**Independent Test**: Migration aplicada, `\d tenants` no Postgres confirma as 3 colunas com os defaults corretos.

---

## Edge Cases

- IF o mesmo email chama `/api/signup` duas vezes antes de verificar o primeiro THEN o sistema SHALL recusar a segunda tentativa com 409 (tenant/membership não duplicados), orientando a verificar o email já enviado.
- IF um `owner` é o único membro de um tenant e tenta remover a si mesmo (`Delete` em si próprio) THEN o sistema SHALL recusar, para nunca deixar um tenant sem owner.
- IF a migration de RLS falhar no meio (policy criada em algumas tabelas, não em todas) THEN a migration inteira SHALL rodar dentro de 1 transação, revertendo tudo em caso de erro parcial.
- WHEN um usuário sem nenhuma `tenant_membership` tenta logar (ex.: removido de todos os tenants) THEN o sistema SHALL recusar login com mensagem clara, nunca um dashboard vazio.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| TENANT-01 | P1: Isolamento RLS | T1, T2 | Verified |
| TENANT-02 | P1: Isolamento RLS | T1, T2, T3 | Verified |
| TENANT-03 | P1: Isolamento RLS | T1, T2, AD-023 | Verified |
| TENANT-04 | P1: Isolamento RLS (poller) | T15, AD-024 | Verified |
| TENANT-05 | P1: Bootstrap self-hosted | T4, T6 | Verified |
| TENANT-06 | P1: Bootstrap self-hosted | T5, T6 | Verified |
| TENANT-07 | P1: Bootstrap self-hosted | T6 | Verified |
| TENANT-08 | P1: Signup SaaS | T9, T18 | Verified |
| TENANT-09 | P1: Signup SaaS | T10, T18 | Verified |
| TENANT-10 | P1: Signup SaaS | T10, T18 | Verified |
| TENANT-11 | P1: Signup SaaS | T11, T18 | Verified |
| TENANT-12 | P1: Signup SaaS | T9 | Verified |
| TENANT-13 | P1: Signup SaaS (rate limit) | T12 | Verified |
| TENANT-14 | P1: Convite por tenant | T13 | Verified |
| TENANT-15 | P1: Convite por tenant | T14 | Verified |
| TENANT-16 | P1: Convite por tenant | T14 | Verified |
| TENANT-17 | P1: Convite por tenant | T13 | Verified |
| TENANT-18 | P1: Convite por tenant | T14 | Verified |
| TENANT-19 | P2: Switch tenant | T7, T8, T17 | Verified |
| TENANT-20 | P2: Switch tenant | T8, T17 | Verified |
| TENANT-21 | P2: Switch tenant | T8, T17 | Verified |
| TENANT-22 | P2: Perfil fiscal | T4, T16, T19 | Verified |
| TENANT-23 | P2: Perfil fiscal | T4, T16, T19 | Verified |
| TENANT-24 | P2: Perfil fiscal | T16, T19 | Verified |
| TENANT-25 | P3: Locale/branding schema | T1 | Verified |
| TENANT-26 | P3: Locale/branding schema | T1 | Verified (validação de formato hex adiada — spec-precision gap registrado em validation.md, deferido a feature futura) |

**ID format:** `TENANT-NN`

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 26 total, 0 mapped to tasks, 26 unmapped ⚠️ (Tasks phase mapeia)

---

## Success Criteria

- [ ] 2 tenants seedados em teste de integração nunca enxergam dado um do outro, mesmo com repository de teste que omite `WHERE tenant_id` deliberadamente
- [ ] Bootstrap self-hosted de ponta a ponta (banco vazio → login) funciona sem nenhuma tela nova visível ao gestor de infra
- [ ] Signup público SaaS de ponta a ponta (signup → email → verificação → login) funciona em ambiente de teste com provider de email fake/sandbox
- [ ] Convite de membro funciona nos dois casos (email novo e email já existente em outro tenant)
