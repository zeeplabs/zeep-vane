# status-page-environment-banner

**Size**: Medium (tlc-spec-driven auto-sizing) — spec breve, sem design.md/tasks.md. Implementação direta com TDD, tasks atômicas listadas inline na Execute.

## Contexto

Feature 4 (última) do batch de 4 melhorias pedidas pelo usuário em 2026-10-06. Pedido original: "tag de ambiente + faixa na própria status page". Independente da feature 2 (`service-environment-tag` — tag por serviço, admin-only) e da feature 3 (`public-status-domain-health` — saúde DNS, não ambiente). Esta feature tagueia a **página** inteira, não um serviço, e é a única das 3 que expõe uma faixa visual na página pública.

## Decisões (via `AskUserQuestion`, confirmadas pelo usuário)

- **Modelo**: campo enum em `status_pages`, mesmo padrão enum-com-""-default da feature 2, nível diferente (página, não serviço).
- **Valores**: `''` (default, produção implícita — sem faixa), `staging`, `development`. Sem valor explícito `production` — redundante com o default.
- **Quando mostrar faixa**: só quando `environment != ''`. Página sem tag (comportamento atual, imensa maioria das páginas) nunca muda visualmente.
- **Conteúdo da faixa**: menciona qual ambiente é (ex: "Esta é uma página de staging — não reflete produção"), não texto genérico.
- **Dispensabilidade**: fixa, sem botão de fechar, sem estado de sessão/localStorage — o propósito é avisar sempre; permitir esconder contradiz isso.

## Requisitos (ACs)

- **SPEB-01**: `status_pages` ganha coluna `environment` (migration `0048`, após a `0047` da feature 2): `TEXT NOT NULL DEFAULT ''` + `CHECK (environment IN ('', 'staging', 'development'))`.
- **SPEB-02**: `CreateStatusPage`/`UpdateStatusPage` (repo + handler) aceitam `environment` opcional; valor fora do enum retorna 422.
- **SPEB-03**: Admin — formulário de criar/editar status page (`AddStatusPageDrawer.tsx`/`EditStatusPageDrawer.tsx`) ganha select de ambiente, com opção "nenhum" (= `''`).
- **SPEB-04**: `PublicStatusHandler` expõe `environment` no response, campo aditivo, presente **só** quando `!= ''` (omitido quando `''`, não serializa string vazia).
- **SPEB-05**: `PublicStatusPage.tsx` renderiza faixa fixa no topo da página quando `environment` está presente no response. Texto via `react-i18next` (en + pt-BR), interpolando o valor do ambiente (ex: chave `publicStatus.environmentBanner` com `{{environment}}`). Sem botão de fechar, sem `localStorage`.
- **SPEB-06**: MSW fixtures (`web/src/test/msw/handlers.ts`) ganham o campo opcional nos endpoints de status page (admin CRUD + preview + público).
- **SPEB-07**: i18n parity check (`node scripts/check-i18n-parity.mjs`) permanece verde com as chaves novas.

## Fora de escopo

- Qualquer efeito funcional do ambiente além da faixa visual (sem lógica de alerta/permissão/roteamento).
- Valor explícito `production` no enum (default `''` já cobre).
- Faixa dispensável/com estado de sessão.
- Qualquer relação com a tag de ambiente por serviço (feature 2) ou com o indicador de saúde DNS (feature 3) — três conceitos independentes, sem campo compartilhado.

## Reuso

- Padrão de enum `""`-default já estabelecido por `MonitorMode`/`SLOID` (`internal/db/service_repository.go`) e pela feature 2 (`service-environment-tag`) — mesmo estilo de validação 422 e mesma convenção de campo.
- Padrão de campo aditivo no response público já estabelecido por AD-043 (`groups: []`) e pela feature 3 (`domain_health`) — `environment` segue o mesmo princípio: presente só quando relevante, nunca reestrutura o shape existente.
