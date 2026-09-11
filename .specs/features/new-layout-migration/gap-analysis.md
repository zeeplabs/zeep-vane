# Gap Analysis — Migração de layout (handoff-new-layout/)

## Decisões (2026-09-10)

| Tópico | Decisão |
|---|---|
| Poller Status | Redesenhar tela pra realidade: mostrar o poller ativo atual + histórico de failover entre réplicas (dado que já existe via `ha-multi-replica`). Sem fleet multi-região, sem CPU/mem por nó, sem "reiniciar poller" orquestrado. |
| Serviços Monitorados — polling manual | Esconder por ora. Tela nova cobre só monitoramento SLO-based (Datadog), que já existe. Polling manual (HTTP/TCP/Ping) vira feature futura com spec própria, fora deste ciclo. |
| Papéis (owner/operator/viewer) | Mapeamento de rótulo só na UI: `owner`→Admin, `operator`→Membro, `viewer`→Leitura. Nenhuma mudança de modelo, permissão, RLS ou migration. |
| Incidentes — severidade | Adicionar. Migration: coluna `severity` em `incidents` + validação no create/update. |
| Incidentes — timeline de updates | Adicionar. Tabela nova `incident_updates` (autor, texto, timestamp, `is_ai_summary`) — isso muda o fluxo de fechamento com IA atual, que hoje grava só `PendingCloseComment` direto no incidente; o resumo de IA passa a ser mais uma entrada nessa timeline. |
| Dashboard Integrações — webhook inbound | É real, mas especificar depois. Não bloqueia o redesenho das outras telas; card fica fora do escopo deste ciclo até ter spec própria. |
| Meu Perfil (2FA, sessões por dispositivo, preferências de notificação) | Especificar as 3 agora, junto com o resto deste ciclo — ao contrário da recomendação inicial de adiar. |
| Usuários (seat limit por plano) | **Pausado (2026-09-10)**. `Tenant.Plan` é string livre, sem limite definido em lugar nenhum; `zeep-license-server` (`/Users/juliosousa/Projects/ZeepLabs/baas/zeep-license-server`) existe pra centralizar planos/preços/features entre os produtos Zeep, mas hoje não tem nenhum consumidor real (nem Orbit, que o próprio README cita como já integrado — não está). Integrar o Vane com ele é projeto próprio (registrar Vane como produto, definir planos, cliente de verificação offline Ed25519), não um gap pontual desta tela. Decisão do usuário: pausar a spec de seat limit até essa integração cross-repo ser desenhada, em vez de fazer um mapa fixo interino no Vane. Fica como o único item deste ciclo sem spec escrita. |
| Notificações (central in-app) | Adiar. Spec separada, depois que as telas-fonte de evento (incidentes, domínios, billing) estiverem redesenhadas e estáveis. |
| Planos & Faturamento | Fora deste ciclo. Projeto à parte — precisa de decisão de negócio (preços reais) antes de qualquer spec técnica de Stripe/licença. |

---

Levantamento do backend atual (`internal/db`, `internal/api`, `.specs/features/*`) contra as 11 telas descritas em `handoff-new-layout/README.md`. Objetivo: decidir o que falta construir no backend antes de começar o redesenho de frontend, por tela.

**Nota de desatualização do AGENTS.md**: a seção 1 do `AGENTS.md` ainda descreve o projeto como "single-tenant by design (AD-002)" e cita só os papéis `owner/operator/viewer`. `AD-002` foi revertida por `AD-022` em 2026-09-09 (`.specs/STATE.md`) — Vane agora suporta multi-tenancy real (SaaS) além do self-hosted single-tenant. Os papéis `owner/operator/viewer` continuam corretos e valem para as telas do handoff, mas os textos do mock usam nomenclatura diferente (`Admin/Membro/Leitura`) — tratar como mapeamento de rótulo na UI, não como papel novo, a menos que decidam introduzir um 4º papel.

Legenda: ✅ existe · 🟡 parcial · ❌ falta

---

## 1. Login Bootstrap
✅ Maioria já existe: `auth_handler.go` (login), `signup_handler.go`, `password_reset_handler.go`, `bootstrap_handler.go`, `email_verification_repository.go`, `email_verification_tokens` (0026). Self-hosted vs SaaS já distinguido no bootstrap.
✅ **Confirmado, sem gap**: `TenantMembershipRepository.ListForUser` já é usado por `auth_handler.go` no fluxo de login (linhas 127/221/282) para resolver/listar os tenants do usuário autenticado.

**Ação**: nenhuma. Tela pode avançar direto.

---

## 2. Dashboard Integrações
✅ Datadog: `integration_repository.go` (`UpsertDatadog`, `MarkDatadogInvalid`), `integrations_handler.go`.
✅ LLM Provider: `llm_provider_repository.go`, `llm_providers_handler.go`, plan-gate já é tema ativo (`llm-slo-analysis` spec).
✅ Email (SMTP): `email_provider_repository.go`, `email_providers_handler.go` (feature `email-provider-connect` já fechada).
❌ Inbound webhook — confirmado como real pelo usuário, mas fora deste ciclo. Precisa de spec própria (modelo, endpoint de recebimento, autenticação do payload) antes de entrar em qualquer sprint.

**Ação**: tela nova cobre Datadog + LLM + SMTP outbound (já existem). Card de webhook inbound fica marcado como pendente de spec, não aparece nesta rodada.

---

## 3. Serviços Monitorados
✅ `service_repository.go` (Service: ID, Name, SLOID, CurrentStatus, LastStatusChangeAt, StatusAnalysis), `services_handler.go`, `status_interval_repository.go` (histórico status/uptime), `service_status_analysis_repository.go` (tooltip de degradado via LLM).
❌ **Gap estrutural real**: o mock descreve dois modos de monitoramento — "SLO-based" (existe, via Datadog) e **"Polling manual"** (HTTP/TCP/Ping, intervalo configurável 30s/1m/5m) — esse segundo modo **não existe em lugar nenhum do backend**. `Service` não tem `monitor_mode`, `poll_type`, `target`, `poll_interval`; não há scheduler de polling ativo além do poller Datadog-based (`internal/poller`).
❌ Latência exibida na tabela — `Service` não guarda `latency` agregada; teria que vir de `status_intervals` ou de um novo campo.
❌ "Incidentes 30d" no grid de stats do drawer — não há contagem pronta, precisa de query agregando `incidents` por `service_id`+janela.

**Decisão**: esconder "Polling manual" por ora. Tela nova expõe só SLO-based (já existe). Polling manual vira spec futura, fora deste ciclo.

---

## 4. Domínios & Status Pages
✅ `domain_repository.go`, `domains_handler.go`, `domain_verifier.go` (verificação DNS real), feature `status-page-domain-attach`.
❌ **Confirmado, gap real**: `Domain` (`domain_repository.go`) e `domainResponse` (`domains_handler.go`) só têm `{ID, Hostname, CreatedAt}` — é puro registro de hostname root, sem `status`, `tipo` (subdomain/custom), `ssl_status`, `verified_at`, `cname_target`. `domain_verifier.go` existe mas é um verificador **stateless em tempo real** (DNS lookup + handshake TLS ao vivo), consumido hoje só por `StatusPagesHandler.VerifyDomain` — nada disso é persistido na tabela `domains`. `status_page_repository.go` (Name, Subdomain, DomainID, State, TLSLastError, ServiceIDs) cobre o lado da Status Page, não o do Domain.
✅ `status_page_repository.go` (Name, Subdomain, DomainID, State, TLSLastError, ServiceIDs), `status_pages_handler.go` — esse lado já é suficiente pra aba "Status Pages" do mock.

**Ação**: tela "Domínios" precisa de spec própria — migration em `domains` (tipo, status, ssl_status, verified_at, cname_target) + decidir se o "Verificar novamente" do mock persiste o resultado de `domain_verifier` na linha ou só reflete ao vivo a cada leitura. Aba "Status Pages" pode avançar sem gap.

---

## 5. Incidentes
✅ `incident_repository.go` (Status, Description, PendingCloseComment, AutoCreated), `incident_ai_fields_repository.go`, `incidents_handler.go`, feature `llm-slo-analysis` (resumo de fechamento via IA já é real, não mock — `PendingCloseComment` + `ConfirmPendingClose`).
**Correção (2026-09-10, ao especificar)**: leitura mais completa de `incident_repository.go` mostra que a tabela/timeline `incident_updates` **já existe** (`AddUpdate`, `ListUpdatesPaginated`) e que `ConfirmPendingClose` **já insere o resumo de fechamento por IA como uma entrada normal dessa timeline** (`incident_repository.go:737-742`) — a avaliação original abaixo estava errada nesse ponto (não tinha lido o arquivo inteiro). O gap real é bem menor:
❌ Severidade (Menor/Moderado/Crítico) — campo `severity` não existe em `Incident`. **Decisão: adicionar** — migration + coluna + validação no create/update.
❌ `IncidentUpdate` não tem `author`/`is_ai_summary` — hoje é só `{ID, IncidentID, Body, CreatedAt}`, sem atribuição de autor (`AddUpdate` nem lê o usuário autenticado) e sem flag pra diferenciar a entrada de fechamento por IA das manuais. **Decisão: adicionar** essas duas colunas.

**Ação**: spec pequena — 2 colunas em `incidents` (severity) e `incident_updates` (author_id nullable + is_ai_summary), sem redesenho de fluxo (o fluxo de fechamento com IA já funciona e já popula a timeline).

---

## 6. Poller Status
❌ **Maior gap arquitetural do lote**. O mock desenha uma *fleet* de pollers com `Região`, `Verificações/min`, `Fila`, `Latência média`, `Heartbeat`, CPU/mem por nó, e uma ação "Reiniciar poller". A arquitetura real (`internal/poller`, confirmada por `.specs/features/ha-multi-replica/spec.md`) é **um único poller ativo por vez** via leader election em Postgres — não uma fleet distribuída por região. `poller_status.go` hoje só expõe status por *integração* (Datadog: connected/invalid + last_checked_at/last_error), não telemetria de processo.
❌ Não existe pipeline de métricas (CPU/mem/throughput/queue depth) nem conceito de "região" — Vane não tem essa topologia hoje.
❌ "Reiniciar poller" como ação orquestrada (restart de pod/processo) não existe.

**Decisão**: redesenhar a tela pra realidade — 1 poller ativo atual + histórico de failover entre réplicas (dado que já existe via `ha-multi-replica`'s leader election). Sem fleet, sem região, sem CPU/mem por nó, sem "reiniciar poller" orquestrado. Precisa mapear o que `ha-multi-replica` já grava sobre eleição/lease pra saber o que dá pra expor sem trabalho novo de backend.

---

## 7. Usuários
✅ Já bem coberto: `tenant_membership_repository.go` (role, status, invited_at, last_access — incluindo proteção `ErrLastOwner`), `admins.go` (`Invite`, `ResendInvite`, `CancelInvite`, `AcceptInvite`, `UpdateRole`, `Delete`, `List`), feature `admin-invite-resend-cancel`.
❌ **Confirmado, gap real**: `AdminsHandler.Invite` (`admins.go:141`) não faz nenhuma checagem de limite de membros por plano antes de criar o convite — só valida papel/telefone/duplicidade de e-mail. `Tenant.Plan` existe como campo, mas nada o consulta aqui.

**Ação**: gap pequeno e localizado — adicionar contagem de memberships ativos + comparação com limite do plano em `Invite`, retornando 4xx quando estourar (hoje nada bloqueia).

---

## 8. Planos & Faturamento
❌ **Maior gap de negócio do lote**. Não existe nenhuma integração Stripe, nenhum modelo `Subscription`/`License`, nenhuma geração/validação de chave de licença self-hosted. `Tenant.Plan` é hoje um campo texto simples sem billing por trás.
❌ Preços reais (README já sinaliza "TBD com o usuário").
❌ Feature flags por plano (limite de usuários, gate de LLM, limite de domínios/status pages, retenção de histórico) — hoje o único plan-gate real identificado é o do LLM Provider (`llm-slo-analysis`); os demais (domínios, status pages, retenção) não têm enforcement visível.

**Decisão**: fora deste ciclo. Projeto à parte — precisa de decisão de negócio (preços reais) antes de qualquer spec técnica de Stripe/licença. Redesenho das outras 10 telas não espera por isso.

---

## 9. Configurações
✅ Fiscal fields recentemente adicionados (`b31ad74 feat(web): add fiscal profile fields to company settings`): `Tenant.LegalName/TaxID/TaxIDType/BillingAddress`, `company_settings_handler.go` (mas resposta atual `companySettingsResponse` **não inclui** `BillingAddress` nem endereço estruturado completo — só nome/email/logo/fiscais básicos).
🟡 Campos do mock não encontrados na `Tenant`: `site` (website), `timezone`, `language` (existe `Locale`, pode já servir), inscrição estadual separada de `tax_id` para PJ.
🟡 Endereço fiscal completo — existe `BillingAddress []byte` (JSON cru) na tabela, mas não exposto pelo handler ainda (comentário no código diz "SaaS billing feature owns that field's exposure" — ou seja, decisão consciente de não expor ainda).
❌ Exclusão de tenant (danger zone) — não localizei endpoint de delete/soft-delete de tenant com cascata.

**Ação**: falta endpoint de exclusão de tenant + decidir exposição do endereço fiscal completo (hoje deliberadamente oculto) + adicionar `site`/`timezone` se forem realmente necessários.

---

## 10. Meu Perfil
❌ 2FA: nada implementado (`grep` por `two_fa`/`totp` não retornou nada). Precisa: geração de secret TOTP, `otpauth://` URI para QR, verificação de código real, enforcement no login.
❌ Sessões ativas com dispositivo/IP/localização e revogação individual: `User.SessionsRevokedAt` existe mas é **global** (revoga tudo de uma vez) — não há modelo `Session` por dispositivo com listagem/revogação individual.
❌ `NotificationPreference` (toggles por tipo de notificação): não localizado.
✅ Troca de senha — provavelmente via `auth_handler.go`/`password_reset_handler.go`, mas fluxo autenticado "trocar senha sabendo a atual" precisa confirmação separada do fluxo de reset por e-mail.

**Decisão**: especificar as 3 agora, junto com o resto deste ciclo (não adiar). Ao aprofundar (2026-09-10, sessão de specs), confirmado que 2FA e sessões por dispositivo mexem em autenticação de verdade (AGENTS.md §7 marca isso como risco alto) — viram **4 specs técnicas separadas**, não uma:
- `password-change`: troca de senha autenticada (sabendo a senha atual) — hoje só existe reset por email via token. Escopo pequeno, sem risco de auth novo (não muda emissão/verificação de sessão).
- `auth-2fa-totp`: geração de secret TOTP + `otpauth://` URI, verificação real de código, enforcement no login. Mexe em `auth_handler.go`. Scope **Large** (Design formal).
- `user-sessions`: modelo `Session` real no banco (id, user_id, device/user_agent, ip, created_at, last_seen_at, revoked_at) — decisão confirmada com o usuário de substituir a revogação global (`User.SessionsRevokedAt`) por granularidade por sessão de verdade, já que é o que a tela pede. JWT passa a carregar `session_id`; `RequireAuth` valida contra a tabela a cada request. Scope **Large** (Design formal, muda o mecanismo de auth).
- `notification-preferences`: modelo `NotificationPreference` (user_id, type, enabled) + endpoints, **e** o disparo real de email nos eventos (hook em `internal/api/incidents_handler.go` chamando `internal/email/service.go`) — decisão confirmada de não deixar como toggle decorativo sem efeito.

---

## 11. Notificações
❌ Não existe nenhum modelo `Notification`, nenhum fan-out de eventos (incidente aberto/resolvido, domínio verificado, poller degradado, renovação de billing, convite de usuário), nenhuma entrega in-app (poll/websocket).

**Decisão**: adiar. Spec separada, depois que as telas-fonte de evento (incidentes, domínios, billing) estiverem redesenhadas e estáveis — evita especificar fan-out pra eventos que ainda vão mudar de shape (ex.: severidade/timeline nova em Incidentes, "poller degradado" redefinido na tela 6).

---

## Resumo — escopo deste ciclo após as decisões

| Tela | Escopo neste ciclo | Trabalho de backend antes do frontend |
|---|---|---|
| 1. Login Bootstrap | dentro | nenhum — seletor de tenant confirmado (`auth_handler.go` já usa `ListForUser`) |
| 2. Dashboard Integrações | dentro (sem webhook inbound) | nenhum — Datadog/LLM/SMTP já existem |
| 3. Serviços Monitorados | dentro (sem polling manual) | nenhum — só SLO-based, já existe |
| 4. Domínios & Status Pages | dentro | **spec + migration** confirmada na aba Domínios (tipo/status/ssl_status/verified_at/cname_target); aba Status Pages sem gap |
| 5. Incidentes | dentro | **spec + migration**: campo `severity` + tabela `incident_updates` (muda fluxo de fechamento com IA) |
| 6. Poller Status | dentro, redesenhada | mapear o que `ha-multi-replica` já grava (eleição/lease) pra tela de "poller ativo + histórico de failover" |
| 7. Usuários | dentro | **confirmado**: seat limit por plano não existe em `AdminsHandler.Invite`, precisa de checagem nova (pequeno) |
| 8. Planos & Faturamento | **fora deste ciclo** | projeto à parte, aguarda decisão de preços |
| 9. Configurações | dentro | endpoint de exclusão de tenant + decidir exposição do endereço fiscal completo |
| 10. Meu Perfil | dentro | **3 specs novas**: 2FA (TOTP), Session por dispositivo, NotificationPreference |
| 11. Notificações | **fora deste ciclo** | spec separada, depois das telas-fonte de evento estabilizarem |

## Ordem de trabalho
1. ~~Confirmar os itens pequenos pendentes~~ — feito (2026-09-10): seletor de tenant sem gap; Domínios com gap real confirmado; seat limit sem enforcement confirmado.
2. Especificar (`tlc-spec-driven`) em paralelo, pois são independentes entre si:
   - Incidentes: `severity` + `incident_updates`.
   - Poller Status: modelo "poller ativo + histórico de failover" sobre o que `ha-multi-replica` já expõe.
   - Meu Perfil: 2FA, Session por dispositivo, NotificationPreference (podem ser 3 specs ou 1 spec com 3 histórias — decidir ao especificar).
   - Domínios: migration de tipo/status/ssl_status/verified_at/cname_target + decisão de persistência vs. verificação ao vivo.
   - Usuários: seat limit por plano (spec pequena, pode entrar como história dentro de uma spec maior ou sozinha).
3. Depois dos specs acima aprovados e implementados (ou com sign-off explícito de "adiar implementação, seguir com UI mockada temporariamente" — decisão do usuário caso a caso), iniciar o redesenho de frontend tela a tela.
4. Billing (tela 8) e Notificações (tela 11) seguem como iniciativas separadas, sem bloquear o restante.
