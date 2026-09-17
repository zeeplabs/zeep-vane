# Audit Log Expansion Specification

## Problem Statement

`GET /api/audit-log` ("Atividade recente do time" no Overview) hoje grava só 9 ações, todas em `admins.go`/`domains_handler.go`/`status_pages_handler.go`: `invited`, `resent`, `canceled`, `role_changed`, `removed`, `domain_verified`, `domain_deleted`, `status_page_deleted`, `status_page_domain_verified`. Toda outra ação de criação/edição/remoção no app — serviços, criação de status page, ativação de integrações, configurações da empresa, domínios custom — não aparece ali. O usuário pediu para cobrir essas ações.

`internal/audit.Log.Record(ctx, actorID, targetID, targetLabel, action string) error` já existe e já é usado pelos 9 casos atuais — esta feature só adiciona novas chamadas a `Record` nos handlers que hoje não a chamam, mais o campo `audit *audit.Log` (e sua injeção via `New*Handler`) nos handlers que ainda não o têm.

## Goals

- [ ] `ServicesHandler.Create/Update/Delete` gravam `service_created`/`service_updated`/`service_deleted`.
- [ ] `StatusPagesHandler.Create/AttachDomain/SetServices` gravam `status_page_created`/`status_page_domain_attached`/`status_page_services_updated` (Delete e VerifyDomain já gravam, inalterados).
- [ ] `DomainsHandler.Create` grava `domain_created` (Delete e Verify já gravam, inalterados).
- [ ] `IntegrationsHandler.ConnectDatadog` grava `datadog_connected`.
- [ ] `EmailProvidersHandler.Connect/Activate` gravam `email_provider_connected`/`email_provider_activated`.
- [ ] `LLMProvidersHandler.Connect/Activate` gravam `llm_provider_connected`/`llm_provider_activated` (`SetModel` fora de escopo - ver Out of Scope).
- [ ] `CompanySettingsHandler.Update/UploadLogo` gravam `company_settings_updated`/`company_logo_updated`.
- [ ] `TenantHandler.Delete` grava `tenant_deleted` antes do soft-delete responder 200.
- [ ] Toda nova ação aparece no card "Atividade recente do time" com uma frase pt-BR/en legível (i18n), no mesmo formato das 9 ações existentes.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Incidentes (Create/Transition/ConfirmClose/SetSeverity/AddUpdate/DiscardCloseProposal) | Decisão explícita do usuário. Incidentes já têm timeline própria (mecanismo separado); boa parte é criada pela IA/poller sem actor humano (`admin_audit_log.actor_id` é `NOT NULL`, confirmado em `migrations/0011_admin_audit_log.up.sql:5` - não daria pra gravar essas mesmo se quisesse). |
| Ações de auto-serviço na própria conta (trocar senha, ativar/desativar 2FA, editar nome/e-mail do próprio perfil) | Decisão explícita do usuário. Não é "atividade do time" sobre um recurso compartilhado, é ação sobre a própria conta. |
| `LLMProvidersHandler.SetModel` | Troca de modelo dentro de um provider já ativo é um ajuste fino demais para o nível de granularidade do audit log (mesmo raciocínio que já vale para outros ajustes finos não auditados, como reordenar campos). Connect/Activate (que representam a decisão real de qual provider usar) já cobrem o sinal relevante. |
| Reclassificação/backfill de atividade antiga (ações que já aconteceram antes desta feature, sem entrada) | Fora de escopo - card mostra atividade daqui pra frente, sem histórico retroativo. |
| Mudar `auditLogDefaultLimit`/`auditLogMaxLimit` ou adicionar paginação ao card | Não pedido; mantém o comportamento atual (5 por padrão, máx. 20, sem paginação) mesmo com mais tipos de ação gerando entradas. |
| Diff de valores antigo/novo em ações de "update" (ex: nome anterior vs novo) | Mesmo padrão já usado por `role_changed` hoje - só o identificador atual do alvo, sem diff. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Status pages: escopo de edição | Cobre `AttachDomain` e `SetServices` além de `Create` (não só criação/remoção). | Decisão do usuário via `AskUserQuestion`: "Criação + edições". | y |
| Domínios custom + Company Settings/Logo | Ambos entram no escopo. | Decisão do usuário via `AskUserQuestion`: "Sim, os dois". | y |
| Integrações: Connect vs Activate | Ambos os passos geram entrada (`_connected` e `_activated` distintos para email/LLM; Datadog só tem o passo `ConnectDatadog`, que é ao mesmo tempo conectar e o único evento existente hoje). | Decisão do usuário via `AskUserQuestion`: "Connect + Activate" - reforçada pela confusão real já registrada nesta sessão (usuário achava que salvar credencial já ativava o provider). | y |
| Nomenclatura de ação | Mantém o padrão ad-hoc existente `entidade_verbo` (`service_created`, `status_page_domain_attached`, `datadog_connected`, etc.), nunca um verbo genérico sozinho (`created`/`updated`). | Consistente com as 9 ações já existentes (`domain_verified`, `role_changed`, etc.) - nenhuma delas usa um verbo genérico sem prefixo de entidade. | y |
| `target_label` de cada nova ação | Mesmo padrão das ações existentes: nome/identificador legível do alvo no momento da ação (nome do serviço, nome da status page, hostname do domínio, nome do provider conectado/ativado, `"Configurações da empresa"` fixo para company settings/logo, nome do tenant para `tenant_deleted`). Nunca um diff de antes/depois. | Decisão do usuário via `AskUserQuestion` (pergunta 4 do Discuss anterior, respondida implicitamente ao confirmar o padrão de nomenclatura) - mesmo mecanismo de `role_changed`, que já não guarda diff. | y |
| Handlers sem `audit *audit.Log` hoje | `ServicesHandler`, `IntegrationsHandler`, `EmailProvidersHandler`, `LLMProvidersHandler`, `CompanySettingsHandler`, `TenantHandler` ganham o campo `audit *audit.Log` e um parâmetro novo em `New*Handler`, seguindo exatamente o padrão já usado por `StatusPagesHandler`/`DomainsHandler`. `internal/cli/routes.go` já constrói um `auditLog` compartilhado (usado por `domainsHandler`/`statusPagesHandler`) - só precisa passar a mesma instância pros novos construtores. | Investigação de código (`routes.go:92-106`) - nenhuma decisão nova, só extensão do padrão existente. | y |
| Falha ao gravar audit (`Record` retorna erro) | Mesmo padrão dos 9 casos existentes: loga o erro (`h.logger.Error(...)`) mas não falha nem reverte a operação principal - a ação de negócio (criar serviço, ativar provider, etc.) já foi bem-sucedida e responde normalmente ao cliente. | Consistente com todo `audit.Record` existente hoje (ex.: `status_pages_handler.go:280-282`) - audit é best-effort, nunca bloqueante. | y |
| `TenantHandler.Delete`: ordem de operações | `audit.Record` é chamado ANTES do soft-delete responder 200 ao cliente, mas depois de `SoftDelete` já ter sido persistido com sucesso (mesma ordem "ação primeiro, audit depois, resposta por último" dos casos existentes). Uma falha em `Record` aqui não desfaz o soft-delete (mesma regra do item acima). | Consistente com o padrão geral de best-effort audit. | y |

**Open questions:** none — todas resolvidas ou registradas acima.

---

## User Stories

### P1: Ações de recurso compartilhado (serviço, status page, domínio, integração, configurações) aparecem na Atividade recente do time ⭐ MVP

**User Story**: Como owner/operator, quero ver no card "Atividade recente do time" quando alguém cria, edita ou remove um serviço, cria/edita uma status page, conecta ou ativa uma integração, ou edita as configurações/domínios da empresa, para acompanhar o que o time mudou sem precisar perguntar.

**Why P1**: É o próprio pedido do usuário - hoje esse card mostra só uma fração pequena da atividade real do tenant.

**Acceptance Criteria**:

1. **AUDITEXP-01**: WHEN um admin cria um serviço com sucesso (`POST /api/services`) THEN o sistema SHALL gravar uma entrada `service_created` com `target_label` = nome do serviço.
2. **AUDITEXP-02**: WHEN um admin edita um serviço com sucesso (`PATCH /api/services/{id}`) THEN o sistema SHALL gravar uma entrada `service_updated` com `target_label` = nome atual do serviço (pós-edição).
3. **AUDITEXP-03**: WHEN um admin remove um serviço com sucesso (`DELETE /api/services/{id}`) THEN o sistema SHALL gravar uma entrada `service_deleted` com `target_label` = nome do serviço removido.
4. **AUDITEXP-04**: WHEN um admin cria uma status page com sucesso THEN o sistema SHALL gravar `status_page_created` com `target_label` = nome da status page.
5. **AUDITEXP-05**: WHEN um admin anexa um domínio a uma status page com sucesso (`AttachDomain`) THEN o sistema SHALL gravar `status_page_domain_attached` com `target_label` = nome da status page.
6. **AUDITEXP-06**: WHEN um admin altera os serviços exibidos em uma status page com sucesso (`SetServices`) THEN o sistema SHALL gravar `status_page_services_updated` com `target_label` = nome da status page.
7. **AUDITEXP-07**: WHEN um admin cria um domínio custom com sucesso (`DomainsHandler.Create`) THEN o sistema SHALL gravar `domain_created` com `target_label` = hostname do domínio.
8. **AUDITEXP-08**: WHEN um admin conecta a integração Datadog com sucesso (`ConnectDatadog`) THEN o sistema SHALL gravar `datadog_connected`.
9. **AUDITEXP-09**: WHEN um admin salva credenciais de um provider de e-mail com sucesso (`EmailProvidersHandler.Connect`) THEN o sistema SHALL gravar `email_provider_connected` com `target_label` = nome do provider.
10. **AUDITEXP-10**: WHEN um admin ativa um provider de e-mail com sucesso (`Activate`) THEN o sistema SHALL gravar `email_provider_activated` com `target_label` = nome do provider.
11. **AUDITEXP-11**: WHEN um admin salva credenciais de um provider de LLM com sucesso (`LLMProvidersHandler.Connect`) THEN o sistema SHALL gravar `llm_provider_connected` com `target_label` = nome do provider.
12. **AUDITEXP-12**: WHEN um admin ativa um provider de LLM com sucesso (`Activate`) THEN o sistema SHALL gravar `llm_provider_activated` com `target_label` = nome do provider.
13. **AUDITEXP-13**: WHEN um owner atualiza as configurações da empresa com sucesso (`CompanySettingsHandler.Update`) THEN o sistema SHALL gravar `company_settings_updated`.
14. **AUDITEXP-14**: WHEN um owner atualiza o logo da empresa com sucesso (`UploadLogo`) THEN o sistema SHALL gravar `company_logo_updated`.
15. **AUDITEXP-15**: WHEN um owner exclui o tenant atual com sucesso (`TenantHandler.Delete`, após passar pela checagem de "último tenant ativo") THEN o sistema SHALL gravar `tenant_deleted` com `target_label` = nome do tenant, antes de responder 200.
16. **AUDITEXP-16**: IF qualquer operação acima falhar (validação, 404, 409, 500) THEN o sistema SHALL NOT gravar nenhuma entrada de audit para essa tentativa - consistente com todo `audit.Record` já existente, que só é chamado após a operação de negócio ter sido confirmada bem-sucedida.
17. **AUDITEXP-17**: IF `audit.Record` retornar erro para qualquer uma das novas chamadas THEN o sistema SHALL logar o erro (`logger.Error`) e responder normalmente ao cliente como se a gravação de audit tivesse sucedido - nunca reverter ou falhar a operação de negócio por causa disso.
18. **AUDITEXP-18**: WHEN o frontend renderiza uma entrada do card "Atividade recente do time" para qualquer uma das novas ações THEN o sistema SHALL exibir uma frase legível em pt-BR e en (chave de i18n dedicada por ação), no mesmo padrão visual das 9 ações já existentes - nunca o valor cru de `action` (ex.: `"service_created"`) exposto ao usuário.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| AUDITEXP-01 | P1: Ações de recurso compartilhado aparecem na Atividade recente do time | - | Implementing |
| AUDITEXP-02 | P1: Ações de recurso compartilhado aparecem na Atividade recente do time | - | Implementing |
| AUDITEXP-03 | P1: Ações de recurso compartilhado aparecem na Atividade recente do time | - | Implementing |
| AUDITEXP-04 | P1: Ações de recurso compartilhado aparecem na Atividade recente do time | - | Implementing |
| AUDITEXP-05 | P1: Ações de recurso compartilhado aparecem na Atividade recente do time | - | Implementing |
| AUDITEXP-06 | P1: Ações de recurso compartilhado aparecem na Atividade recente do time | - | Implementing |
| AUDITEXP-07 | P1: Ações de recurso compartilhado aparecem na Atividade recente do time | - | Implementing |
| AUDITEXP-08 | P1: Ações de recurso compartilhado aparecem na Atividade recente do time | - | Implementing |
| AUDITEXP-09 | P1: Ações de recurso compartilhado aparecem na Atividade recente do time | - | Implementing |
| AUDITEXP-10 | P1: Ações de recurso compartilhado aparecem na Atividade recente do time | - | Implementing |
| AUDITEXP-11 | P1: Ações de recurso compartilhado aparecem na Atividade recente do time | - | Implementing |
| AUDITEXP-12 | P1: Ações de recurso compartilhado aparecem na Atividade recente do time | - | Implementing |
| AUDITEXP-13 | P1: Ações de recurso compartilhado aparecem na Atividade recente do time | - | Implementing |
| AUDITEXP-14 | P1: Ações de recurso compartilhado aparecem na Atividade recente do time | - | Implementing |
| AUDITEXP-15 | P1: Ações de recurso compartilhado aparecem na Atividade recente do time | - | Implementing |
| AUDITEXP-16 | P1: Ações de recurso compartilhado aparecem na Atividade recente do time | - | Implementing |
| AUDITEXP-17 | P1: Ações de recurso compartilhado aparecem na Atividade recente do time | - | Implementing |
| AUDITEXP-18 | P1: Ações de recurso compartilhado aparecem na Atividade recente do time | - | Implementing |
