# public-status-domain-health

**Size**: Medium (tlc-spec-driven auto-sizing) — spec breve, sem design.md/tasks.md. Implementação direta com TDD, tasks atômicas listadas inline na Execute.

## Contexto

Feature 3 do batch de 4 melhorias pedidas pelo usuário em 2026-10-06 (feature 1 `service-groups-and-dependencies` já shipada; feature 2 `service-environment-tag` especificada em paralelo). Pedido original: "monitoramento de DNS na status page".

**Overlap real identificado antes do design** (pesquisa de codebase, confirmada pelo usuário): `domain-health-monitoring` (AD-039, shipado) já roda RDAP + NS drift diariamente contra toda a tabela `domains`, já persiste `ExpiresAt`/`Registrar`/`CurrentNS`/`NSDriftDetected`/`LastRDAPCheckAt`/`LastRDAPSuccessAt`/`RDAPLastError` (`internal/db/domain_repository.go:53-68`), e já expõe isso no admin (domain detail view + lista de domínios). O que falta — e o escopo real desta feature — é só **expor esse dado já existente na status page pública**. Reimplementar RDAP/NS checking duplicaria AD-039 e violaria seu design single-source-of-truth; não é o que esta feature faz.

## Decisões (via `AskUserQuestion`, confirmadas pelo usuário)

- **Visibilidade**: pública (visível ao visitante externo), não só admin — admin já está coberto por AD-039.
- **Granularidade exposta**: só status de saúde (`ok`/`attention`), não data de expiração/registrar/NS literal — menos superfície de informação sensível exposta publicamente.
- **Ativação**: sempre visível quando a status page tem domínio anexado, sem toggle/config nova em `status_pages` — mesmo padrão passivo do AD-039.
- **Relação com incidentes**: nenhuma. Badge isolado, não cria/fecha incidente, não toca `incidents`.

## Requisitos (ACs)

- **PSDH-01**: `PublicStatusHandler` (`internal/api/public_status_handler.go`) ganha campo aditivo `domain_health: {status: "ok" | "attention"}` no response, presente **só** quando a status page tem domínio anexado (`status_pages.domain_id != NULL`) e esse domínio já teve pelo menos uma checagem RDAP bem-sucedida (`domains.LastRDAPSuccessAt` não-zero).
- **PSDH-02**: `status = "attention"` quando `domains.NSDriftDetected == true` **OU** dias até `domains.ExpiresAt` ≤ 30 (reaproveita o primeiro threshold de alerta já definido em AD-039, não introduz número novo). Caso contrário `"ok"`.
- **PSDH-03**: Se o domínio nunca teve checagem RDAP bem-sucedida (`LastRDAPSuccessAt` zero), o campo `domain_health` é omitido do response inteiro — não mostra `"ok"` otimista nem `"attention"` por falta de dado.
- **PSDH-04**: Status page sem domínio anexado nunca tem o campo `domain_health` no response (comportamento atual, sem mudança).
- **PSDH-05**: `PublicStatusPage.tsx` (frontend) mostra um indicador pequeno (ícone + tooltip `t()`-roteado, en + pt-BR) só quando `domain_health` está presente no response — `"ok"` discreto, `"attention"` com destaque visual. Não expõe data/registrar/NS literal na UI.
- **PSDH-06**: MSW fixtures (`web/src/test/msw/handlers.ts`) ganham o campo opcional, mirror 1:1 das 3 regras acima.

## Fora de escopo

- Qualquer novo RDAP/NS lookup — reaproveita 100% a checagem diária do AD-039.
- Exposição pública de data de expiração, registrar ou NS literal.
- Toggle de ativação por status page.
- Integração com `incidents` (criação/fechamento automático).
- Histórico/timeline de drift (AD-039 já rejeitou isso explicitamente — não reabrir aqui).

## Reuso

- `domains` table e os 7 campos de saúde já existentes (AD-039) — única fonte de verdade, sem duplicação.
- Padrão de campo aditivo no response público já estabelecido por AD-043 (`groups: []` adicionado sem reestruturar o shape existente) — `domain_health` segue o mesmo princípio.
- `PublicStatusHandler`'s regra de nunca chamar Datadog/serviço externo ao vivo numa request de visitante (`AGENTS.md` §4) — `domain_health` só lê o que o scheduler do AD-039 já persistiu, mesma garantia.
