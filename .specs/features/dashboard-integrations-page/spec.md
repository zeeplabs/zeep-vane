# Dashboard Integrações (new-layout-migration) Specification

## Problem Statement

A tela de integrações atual (`IntegrationsPage.tsx`) empilha Datadog, provedores de e-mail e LLM Provider em cards soltos, sem categorização nem o visual já estabelecido nas telas migradas (`domains-status-pages-page`, `manual-polling-monitoring`). O handoff (`handoff-new-layout/Dashboard Integracoes.dc.html`) organiza as mesmas integrações em grid por categoria (APM & Observabilidade, IA, E-mail), com badge de status consistente por card. Gap-analysis (item 2) confirmou que as 3 integrações reais já existem por completo no backend — o trabalho aqui é só de frontend.

## Goals

- [ ] Reorganizar `/integrations` (ou rota equivalente) em grid por categoria, igual ao mock, consumindo os endpoints já existentes (Datadog, LLM Provider, Email).
- [ ] Badge de status por card seguindo o padrão dot+pill já usado em `DomainStatusTag`/`services/StatusTag`.
- [ ] Preservar RBAC existente: `owner`/`operator` podem conectar/editar; `viewer` só visualiza.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Webhook inbound (Integrações & Automação) | Card real mas sem spec própria ainda — decisão registrada em `gap-analysis.md` item 2, fora deste ciclo. Nem sequer aparece no mock desta tela. |
| Botão "Testar conexões" (topo da página) | Sem `onClick` real no mock (`support.js` não implementa ação) e sem endpoint de bulk-test no backend hoje. Renderizado como visual estático não-funcional nesta rodada, ou omitido — ver Assumptions. |
| Card New Relic — conectar de verdade | Sem repository/handler no backend (nenhum suporte a outro provedor de APM). Card aparece só como vitrine ("Em breve"), decisão do usuário. |
| Gate Free/Pro no card LLM Provider (badge "Plano Free" + "Fazer upgrade") | `POST /api/integrations/llm/{provider}` não tem enforcement de plano hoje — só a geração de resumo de fechamento por IA é plan-gated (endpoint diferente). Billing é item 8 do gap-analysis, fora deste ciclo. Reproduzir o paywall visual do mock sem enforcement real seria enganoso — decisão do usuário via AskUserQuestion. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Card New Relic (APM sem backend) | Aparece desabilitado com badge "Em breve" substituindo o pill de status do mock; botão "Conectar" removido/desabilitado | Decisão do usuário via AskUserQuestion — mantém a vitrine visual do handoff sem simular funcionalidade real | y |
| Seção "E-mail" atrás de `isSelfHosted` no mock | Seção sempre visível, sem gate por modo de instalação | Não existe hoje nenhum sinal de runtime (config/flag) que distinga self-hosted de SaaS (confirmado por grep em `internal/config`, `internal/api`) — Resend/SendGrid já são genéricos e não amarrados a modo de instalação. Decisão do usuário via AskUserQuestion. | y |
| Botão "Testar conexões" (topo) | Omitido nesta rodada — não existe endpoint de bulk-test e o mock não conecta o botão a nenhuma ação real | Renderizar um botão que não faz nada seria UI morta; melhor não incluir até existir spec/endpoint pra ele | n — assumido, não perguntado (baixo risco, sem ação de backend implicada) |
| "Sincronizado há N min" (card Datadog) | Calculado no cliente a partir de `last_checked_at` (já retornado por `GET /api/integrations/datadog/status`) | Backend não expõe um texto relativo pronto; é o mesmo padrão já usado em outras telas (ex.: domínios) | y — inferido do backend existente, sem ambiguidade de produto |
| Rota da tela | Mantém `/integrations`, componente reescrito internamente | Nenhuma indicação de mudança de rota no handoff nem pedido do usuário | y |

**Open questions:** none — todas resolvidas ou registradas acima.

---

## User Stories

### P1: Ver integrações reais por categoria, com status correto ⭐ MVP

**User Story**: Como owner/operator, quero ver Datadog, LLM Provider e provedores de e-mail agrupados por categoria com status atualizado, para entender rapidamente o que está conectado sem abrir cada configuração.

**Why P1**: É o conteúdo real da tela — sem isso não há redesenho, só placeholder.

**Acceptance Criteria**:

1. WHEN a tela `/integrations` carrega THEN o sistema SHALL renderizar 3 seções — "APM & Observabilidade" (Datadog + New Relic), "IA" (LLM Provider), "E-mail" (Resend + SendGrid) — cada uma com contagem de integrações no cabeçalho (`N integrações`/`N integração`).
2. WHEN o Datadog está conectado (`GET /api/integrations/datadog/status` retorna 200, `useIntegrationStatus` resolve `connected: true`) THEN o card SHALL exibir o badge verde "Conectado" e o texto "Sincronizado há {N} min" calculado a partir de `last_checked_at`.
3. IF o Datadog nunca foi conectado (`GET /api/integrations/datadog/status` retorna 404, `connected: false`) THEN o card SHALL exibir o badge neutro "Não conectado" e o texto "Não configurado", sem quebrar a tela.
4. WHEN o LLM Provider tem `active_provider` não-nulo (`GET /api/integrations/llm`) THEN o card SHALL exibir o badge verde "Conectado" e o texto "{Provider} · {Model}" do provider ativo.
5. IF `active_provider` é nulo (nenhum LLM conectado ainda) THEN o card SHALL exibir o badge neutro "Não conectado" e o botão "Conectar" — sem badge Free/Pro nem CTA de upgrade, independente do `plan_tier` do tenant.
6. WHEN a lista de e-mail (`GET /api/integrations/email`) retorna providers com `status: "connected"` THEN cada card (Resend, SendGrid) SHALL exibir seu próprio badge de status e ação — "Conectado" com botão "Editar conexão" quando presente na lista e ativo/verificado, "Não conectado" com botão "Conectar" quando ausente da lista.
7. The system SHALL exibir o card New Relic sempre com o badge "Em breve" e sem ação clicável, independente de qualquer resposta de backend (não existe endpoint para esta integração).

**Independent Test**: Logar como owner com Datadog conectado + LLM não conectado + Resend conectado + SendGrid não conectado; conferir que os 5 cards (Datadog, New Relic, LLM, Resend, SendGrid) renderizam o status e ação corretos.

---

### P2: Ações de conectar/editar abrem via drawer padrão

**User Story**: Como owner/operator, quero clicar em "Conectar"/"Editar conexão" em qualquer card e preencher a conexão num drawer lateral, igual ao padrão já usado em "Anexar domínio"/"Criar status page", sem duplicar a lógica de conectar/validar/persistir que já existe.

**Why P2**: A tela nova é composição visual; a lógica de conectar/validar/persistir já existe (hooks) e não deve ser reescrita — só a superfície de UI muda, de formulário inline pra drawer, por decisão do usuário durante o Execute (consistência com toda tela já migrada).

**Acceptance Criteria**:

1. WHEN um owner/operator clica em "Conectar"/"Editar conexão" no card Datadog THEN o sistema SHALL abrir um `Drawer` (mesmo chrome padrão: X de fechar, padding 24px, footer com border-top) com o formulário de API key/App key, reusando `useConnectDatadog` — sem navegação para outra rota.
2. WHEN um owner/operator clica em "Conectar"/"Editar conexão" no card Resend ou SendGrid THEN o sistema SHALL abrir o mesmo padrão de `Drawer` com o formulário de conexão daquele provider específico, reusando os hooks já existentes em `email-providers/hooks`.
3. WHEN um owner/operator clica em "Conectar"/"Editar conexão" no card LLM Provider THEN o sistema SHALL abrir o mesmo padrão de `Drawer` com o formulário de API key/modelo, reusando `useConnectLLMProvider` de `settings/hooks`.
4. IF o usuário autenticado é `viewer` THEN nenhum botão de ação (Conectar/Editar conexão) SHALL aparecer nos cards — apenas os badges de status, mesmo padrão de RBAC já usado em `IntegrationsPage.tsx` (`hasRole(["owner","operator"])`).

**Independent Test**: Logar como `viewer`, confirmar que nenhum card mostra botão de ação; logar como `owner`, clicar em "Editar conexão" no Datadog e confirmar que o formulário existente abre.

---

### P3: Visual alinhado ao design system migrado

**User Story**: Como usuário de qualquer papel, quero que a tela de integrações pareça parte do mesmo produto que `Domínios & Status Pages`/`Serviços Monitorados`, não uma tela antiga isolada.

**Why P3**: Consistência visual pura — não muda comportamento, só estilo.

**Acceptance Criteria**:

1. The system SHALL renderizar os cards com `Card` `elevation="none"` + `border border-divider` (convenção pós-remoção de shadow, AGENTS.md/sessão anterior), nunca com sombra.
2. The system SHALL usar o badge dot+pill (`Tag` + span de dot) para todo status de card, igual ao padrão `DomainStatusTag`.

**Independent Test**: Screenshot da tela renderizada lado a lado com o mock — sem sombra em nenhum card, badges no formato dot+pill.

---

## Edge Cases

- IF o Datadog está conectado mas com `status: "invalid"` (chave rejeitada em uma verificação anterior) THEN o card SHALL continuar exibindo o badge "Conectado" (binário, igual ao mock — não há terceiro estado visual nesta tela) — divergência de credencial inválida não é tratada aqui, é escopo de outra tela.
- IF `GET /api/integrations/email` retorna lista vazia (nenhum provider conectado) THEN ambos os cards Resend/SendGrid SHALL aparecer como "Não conectado" — nunca 404 nem seção oculta.
- IF `GET /api/integrations/llm` retorna `active_provider: null` mesmo em plano Pro (nenhum provider configurado ainda) THEN o card LLM SHALL tratar como "Não conectado" com botão "Conectar", não como erro.
- WHEN a tela carrega e alguma das 3 chamadas (Datadog status, LLM list, Email list) falha (erro de rede/5xx) THEN a seção correspondente SHALL exibir estado de erro isolado (não derruba as outras 2 seções nem a página inteira).

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| INTG-01 | P1: Ver integrações por categoria | Execute | Verified |
| INTG-02 | P1: Ver integrações por categoria | Execute | Verified |
| INTG-03 | P1: Ver integrações por categoria | Execute | Verified |
| INTG-04 | P1: Ver integrações por categoria | Execute | Verified |
| INTG-05 | P1: Ver integrações por categoria | Execute | Verified |
| INTG-06 | P1: Ver integrações por categoria | Execute | Verified |
| INTG-07 | P1: Ver integrações por categoria (New Relic) | Execute | Verified |
| INTG-08 | P2: Ações abrem via drawer padrão | Execute | Verified |
| INTG-09 | P2: Ações abrem via drawer padrão | Execute | Verified |
| INTG-10 | P2: Ações abrem via drawer padrão | Execute | Verified |
| INTG-11 | P2: RBAC viewer sem ações | Execute | Verified |
| INTG-12 | P3: Sem sombra nos cards | Execute | Verified |
| INTG-13 | P3: Badge dot+pill | Execute | Verified |

**Coverage:** 13 total, 13 verified (Execute inline — Medium scope, sem `tasks.md` formal), 0 unmapped.

---

## Success Criteria

- [ ] `/integrations` renderiza as 3 categorias do mock com contagem, status e ações corretas para os 5 cards (Datadog, New Relic, LLM Provider, Resend, SendGrid).
- [ ] `viewer` não vê nenhum botão de ação; `owner`/`operator` veem e conseguem abrir os formulários já existentes.
- [ ] Nenhum card usa `elevation="elev-sm"`; todos com `border-divider`.
- [ ] `tsc -b --noEmit` limpo e suíte de frontend verde após a implementação.
