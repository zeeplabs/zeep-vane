# service-environment-tag

**Size**: Medium (tlc-spec-driven auto-sizing) — spec breve, sem design.md/tasks.md. Implementação direta com TDD, tasks atômicas listadas inline na Execute.

## Contexto

Parte do batch de 4 melhorias pedidas pelo usuário em 2026-10-06 (feature 1, `service-groups-and-dependencies`, já shipada em `develop`). Esta é a feature 2: tag de ambiente visual por serviço, só para o admin.

## Decisões (via `AskUserQuestion`, confirmadas pelo usuário)

- **Escopo**: campo global em `services`, não por status-page-placement. O mesmo serviço mostra sempre a mesma tag em qualquer lugar do admin onde aparece.
- **Efeito**: puramente visual (badge). Sem lógica de alerta, filtro ou permissão associada — YAGNI, nada pedido além da exibição.
- **Valores**: enum fixo, não texto livre.
- **Visibilidade**: admin-only. Não aparece na status page pública (distingue esta feature da feature 4, que é tag+faixa na própria status page, escopo de página — não de serviço).

## Requisitos (ACs)

- **SET-01**: `Service` ganha campo `Environment` (`""` = sem tag, default; outros valores: `"development"`, `"staging"`, `"production"`), seguindo a convenção `""`-means-absent já usada em `SLOID`/`SLOName`/`DatadogServiceTag` (`internal/db/service_repository.go:19-44`).
- **SET-02**: Migration nova (`0047`, após `0046_status_page_service_groups`): `ALTER TABLE services ADD COLUMN environment TEXT NOT NULL DEFAULT ''` + `CHECK (environment IN ('', 'development', 'staging', 'production'))`.
- **SET-03**: `CreateService`/`UpdateService` (repo + handler) aceitam `environment` opcional no payload; valor inválido (fora do enum) retorna 422.
- **SET-04**: Endpoints de listagem/detalhe de serviço (já existentes) devolvem `environment` sem endpoint novo — é campo a mais no `Service` já serializado.
- **SET-05**: Frontend admin (`web/src/features/services/*` — nome exato a confirmar na Execute) ganha badge colorido ao lado do nome do serviço: `development`=cinza, `staging`=amarelo, `production`=verde, `""`=sem badge. Select de ambiente no formulário de criar/editar serviço, com opção "sem tag".
- **SET-06**: Strings do select/badge via `react-i18next` (en + pt-BR), parity-checked.

## Fora de escopo

- Status page pública (admin-only, confirmado).
- Qualquer lógica de alerta/filtro/permissão baseada no ambiente.
- Ambiente por status-page-placement (rejeitado — ver Decisões).

## Reuso

- Padrão de enum fixo + badge já existe no repo para `MonitorMode` (`"slo"`/`"polling"`) — mesmo estilo de validação server-side (422 com mensagem clara, mirror no `AGENTS.md` §5 de erro mapeado) e de UI (ver como `ServiceDrawer`/`ServicesTable` já exibem `MonitorMode`, se houver badge análogo).
