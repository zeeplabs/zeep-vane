# Poller Status (new-layout-migration) Specification

## Problem Statement

A tela atual (`PollerStatusPage.tsx`) é uma lista simples de integrações com última execução — não usa nenhum dos campos que `GET /api/poller/status` já retorna desde a feature `poller-status-real-state` (`leader_elected`, `poller_running`, `replica`, `checks_last_minute`): o tipo frontend (`PollerStatusEntry`, `types/api.ts:160`) nem os declara. O mock (`handoff-new-layout/Poller Status.dc.html`) desenha uma fleet multi-região fictícia (CPU/mem por nó, fila, "Reiniciar poller") que `poller-status-real-state`'s spec já decidiu não construir (Out of Scope: sem fleet, sem região, sem CPU/mem/fila, sem restart, sem histórico de failover persistido — estado é lido ao vivo, nunca cacheado). Esta spec cobre só o frontend: consumir os campos reais já existentes e alinhar o visual ao design system já migrado (`domains-status-pages-page`, `dashboard-integrations-page`).

## Goals

- [ ] Frontend consome `leader_elected`/`poller_running`/`replica`/`checks_last_minute`, hoje ignorados.
- [ ] Layout usa grid de stat cards (padrão já migrado) com dados reais, sem inventar região/CPU/mem/fila/latência.
- [ ] Lista de integrações conectadas segue o padrão visual dot+pill já usado em `IntegrationCard`/`DomainStatusTag`.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Fleet multi-região, CPU/mem/fila/throughput/latência por nó | Decidido em `poller-status-real-state` (Out of Scope) — arquitetura real não tem esses conceitos; nenhum dado existe pra exibir. |
| Botão "Reiniciar poller" | Decidido em `poller-status-real-state` — sem restart genérico seguro (request pode cair em réplica não-líder); poller já se autorrecupera via leader election. |
| Histórico de failover entre réplicas / drawer por nó | Decidido em `poller-status-real-state` — estado é live-only, sem tabela nova, sem log de eventos persistido. Esta tela mostra o estado atual, não histórico. |
| Novo endpoint ou mudança de contrato em `GET /api/poller/status` | Endpoint já expõe tudo que esta tela precisa; mudança é só o frontend passar a consumir. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Composição visual (já que o mock é fleet fictícia, não dá pra seguir 1:1) | Grid de 3 stat cards no topo — "Poller" (Ativo/Parado + nome da réplica líder), "Verificações/min" (`checks_last_minute`), "Integrações conectadas" (`total`) — seguido da lista de integrações já existente, restilizada com badge dot+pill (`Tag`) igual `IntegrationCard` | Reaproveita exatamente os 4 campos reais do endpoint sem inventar métricas; segue o padrão stat-card + lista já estabelecido nas outras telas migradas (`domains-status-pages-page`) | n — decisão de composição, baixo risco, sem impacto de dado/produto |
| Banner de degradação (mock tem banner de alerta quando há nó degradado) | Mostrar banner de alerta quando `poller_running: false` (com `leader_elected: true`, ou seja, réplica líder existe mas sem integração Datadog conectada) OU quando qualquer integração da lista tem `status !== "active"` | Sinaliza exatamente os dois estados reais de atenção que o endpoint já expõe, sem inventar "grau de degradação" | n — inferido do dado real disponível |
| `leader_elected: false` (nenhuma réplica líder no momento da chamada) | Estado card "Poller: Sem líder no momento" — não é erro, é estado transitório legítimo (já documentado em `poller-status-real-state` Edge Cases) | Spec de origem já trata isso como estado válido; frontend só precisa renderizar sem quebrar | y — já confirmado na spec de origem |
| Rota da tela | Mantém `/poller-status`, componente reescrito internamente | Nenhuma indicação de mudança de rota | y |
| Paginação da lista de integrações | Mantém `Pager` existente (já usa `Page<T>` com `total`/`page`/`page_size`) | Endpoint já pagina; convenção do projeto (AGENTS.md §5) exige manter `Pager` compartilhado | y |

**Open questions:** none — todas resolvidas ou registradas acima.

---

## User Stories

### P1: Ver estado real do poller e das integrações ⭐ MVP

**User Story**: Como owner/operator/viewer, quero ver se o poller está rodando, qual réplica lidera, quantas verificações ocorreram no último minuto e o status de cada integração conectada, para confiar no painel de operação em vez de ver uma fleet inventada.

**Why P1**: É o conteúdo real da tela — sem isso não há redesenho, só placeholder.

**Acceptance Criteria**:

1. WHEN `/poller-status` carrega e `leader_elected: true` e `poller_running: true` THEN o sistema SHALL exibir o card "Poller" com badge "Ativo" e o nome da réplica líder (`replica.application_name`).
2. IF `leader_elected: true` e `poller_running: false` THEN o card "Poller" SHALL exibir badge "Aguardando integração" com o nome da réplica líder, e um banner de alerta indicando que a réplica líder existe mas nenhuma integração Datadog está conectada.
3. IF `leader_elected: false` THEN o card "Poller" SHALL exibir badge "Sem líder no momento" e nenhum nome de réplica — sem erro, sem quebrar a tela.
4. The system SHALL exibir o card "Verificações/min" com o valor de `checks_last_minute`, incluindo `0` como valor válido (sem tratar como erro ou estado vazio; verificado no Execute — gap fechado após o Verifier apontar ausência de teste pro caso `0`).
5. The system SHALL exibir o card "Integrações conectadas" com o valor de `total`.
6. WHEN a lista de integrações (`items`) carrega THEN cada linha SHALL exibir o badge dot+pill de status ("Sucesso" verde quando `status === "active"`, "Falha" vermelho caso contrário) e a data/hora de `last_checked_at` formatada, igual ao comportamento atual.
7. IF uma integração tem `status !== "active"` THEN a linha SHALL exibir `last_error` abaixo do nome do provider, igual ao comportamento atual.

**Independent Test**: Logar como owner com Datadog conectado e ativo (poller rodando); conferir que os 3 stat cards e a linha do Datadog mostram os valores reais do endpoint. Simular `status: "invalid"` no Datadog e conferir badge "Falha" + `last_error` visível.

---

### P2: Alerta de atenção quando o poller precisa de ação

**User Story**: Como owner/operator, quero ver um banner de alerta quando o poller está sem integração conectada ou alguma integração está falhando, para não precisar abrir cada linha pra descobrir.

**Why P2**: Reaproveita o padrão de banner já existente em `PollerBanner.tsx` (usado no shell da aplicação) dentro da própria tela de detalhe, evitando que o operador precise cruzar informação entre o banner global e a tabela.

**Acceptance Criteria**:

1. WHEN `poller_running: false` e `leader_elected: true` THEN o sistema SHALL exibir um banner de alerta com a mensagem "Réplica líder ativa, mas nenhuma integração Datadog conectada."
2. WHEN pelo menos uma integração da lista tem `status !== "active"` THEN o sistema SHALL exibir um banner de alerta nomeando os providers afetados, reaproveitando o texto já usado por `PollerBanner.tsx` (`failureMessage`).
3. IF ambas as condições acima são verdadeiras simultaneamente THEN o sistema SHALL exibir os dois banners, um por condição, sem sobrepor ou omitir nenhum.
4. WHILE nenhuma das duas condições é verdadeira, o sistema SHALL NOT exibir nenhum banner de alerta.

**Independent Test**: Simular resposta com `poller_running: false` e um item `status: "invalid"` simultaneamente; conferir que os 2 banners aparecem.

---

### P3: Visual alinhado ao design system migrado

**User Story**: Como usuário de qualquer papel, quero que a tela de Poller Status pareça parte do mesmo produto que `Domínios & Status Pages`/`Dashboard Integrações`.

**Why P3**: Consistência visual pura.

**Acceptance Criteria**:

1. The system SHALL renderizar os stat cards e a lista sem sombra (`elevation="none"`) e com `border-divider`, igual à convenção pós-remoção de shadow já aplicada nas outras telas migradas.
2. The system SHALL usar o badge dot+pill (`Tag` + span de dot) para status de integração, igual ao padrão já usado em `IntegrationCard`/`DomainStatusTag`.
3. The system SHALL usar tokens de texto semânticos (`text-text-muted`, não `text-neutral-400`) em todo texto secundário da tela — evita repetir o bug de contraste já corrigido em `Tag.tsx`/`Button.tsx` nesta sessão.

**Independent Test**: Screenshot da tela renderizada no tema claro e escuro — sem sombra, badges dot+pill, texto secundário legível nos dois temas.

---

## Edge Cases

- IF a chamada a `GET /api/poller/status` falha (erro de rede/5xx) THEN a tela SHALL exibir um estado de erro isolado (mesmo padrão de `isError` já usado em `dashboard-integrations-page`), sem quebrar a navegação.
- IF `items` está vazio (nenhuma integração conectada) THEN a tela SHALL exibir "Nenhuma integração conectada." no lugar da lista, igual ao comportamento atual — os stat cards continuam renderizando normalmente (`checks_last_minute: 0`, `total: 0`).
- WHEN `replica` é `null` mas `leader_elected: true` (não deveria ocorrer segundo o contrato do backend, mas o tipo frontend permite) THEN o sistema SHALL tratar como "Sem líder no momento" (mesmo fallback do AC P1.3), nunca acessar campo de objeto nulo.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| POLLPG-01 | P1: Ver estado real do poller | Execute | Verified |
| POLLPG-02 | P1: Ver estado real do poller | Execute | Verified |
| POLLPG-03 | P1: Ver estado real do poller | Execute | Verified |
| POLLPG-04 | P1: Ver estado real do poller | Execute | Verified |
| POLLPG-05 | P1: Ver estado real do poller | Execute | Verified |
| POLLPG-06 | P1: Ver estado real do poller | Execute | Verified |
| POLLPG-07 | P1: Ver estado real do poller | Execute | Verified |
| POLLPG-08 | P2: Banner de alerta | Execute | Verified |
| POLLPG-09 | P2: Banner de alerta | Execute | Verified |
| POLLPG-10 | P2: Banner de alerta | Execute | Verified |
| POLLPG-11 | P2: Banner de alerta | Execute | Verified |
| POLLPG-12 | P3: Sem sombra | Execute | Verified |
| POLLPG-13 | P3: Badge dot+pill | Execute | Verified |
| POLLPG-14 | P3: Tokens semânticos | Execute | Verified |

**Coverage:** 14 total, 14 verified (Execute inline — Medium scope, sem `tasks.md` formal), 0 unmapped.

---

## Success Criteria

- [ ] `/poller-status` renderiza os 3 stat cards reais + lista de integrações com badges dot+pill corretos para os 4 estados combinados (líder ativo/aguardando/sem líder × integrações ok/falhando).
- [ ] Nenhum campo fictício (região, CPU, mem, fila, throughput, latência, restart) aparece na tela.
- [ ] `tsc -b --noEmit` limpo e suíte de frontend verde após a implementação.
