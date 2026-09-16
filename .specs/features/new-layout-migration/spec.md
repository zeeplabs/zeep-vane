# New Layout Migration — Fundação (Design System + App Shell) Specification

## Problem Statement

O redesenho completo do Vane (`handoff-new-layout/`) usa um design system e um shell de aplicação totalmente diferentes dos atuais (`web/src/styles/tokens.css`, "Nocturne" — dark-only, fonte Inter, paleta `#9184d9`; `web/src/layout/Sidebar.tsx` — sidebar fixa 236px, sem topbar, sem collapse). Migrar as 11 telas do handoff tela a tela exige primeiro trocar essa fundação (tokens + shell), senão cada tela migrada fica uma ilha visual dentro do app antigo. O usuário confirmou que não há rollout incremental em produção — a versão nova só sobe quando todo o redesenho estiver pronto — então a fundação pode substituir tokens/shell atuais sem se preocupar em manter as telas hoje existentes visualmente intactas durante a transição.

## Goals

- [ ] Tokens de design (cores light+dark, fonte, paleta, raio, sombra) do handoff aplicados globalmente, substituindo Nocturne.
- [ ] Shell de aplicação novo (sidebar colapsável + topbar + content area) envolvendo toda rota autenticada, substituindo `Sidebar.tsx` atual.
- [ ] Toggle de tema (light/dark) funcional com persistência client-side.

## Out of Scope

Explicitamente excluído. Documentado para prevenir scope creep.

| Item | Motivo |
| --- | --- |
| Migração de conteúdo das 11 telas individuais | Trabalho futuro, tela a tela, depois desta fundação — telas hoje existentes continuam com seus componentes/estilo atuais dentro do shell novo até serem portadas. |
| Central de notificações (rota real, contagem de não-lidas, modelo `Notification`) | Backlog adiado (`.specs/features/new-layout-migration/gap-analysis.md`) — depende das telas-fonte de evento estabilizarem. |
| Página "Meu Perfil" | Feature futura própria (2FA já feito via `auth-2fa-totp`; sessões e preferências de notificação têm specs próprias em Draft: `user-sessions`, `notification-preferences`). |
| Planos & Faturamento (item de nav, página) | Fora deste ciclo por decisão de negócio pendente (preços reais) — item de nav correspondente não aparece nesta fundação. |
| Persistência de tema/pin no servidor (por usuário) | Nesta fundação a persistência é só client-side (`localStorage`) — persistência server-side, se decidida no futuro, é feature separada. |
| Seat limit por plano, RBAC de item de nav além do já existente | `hasRole(["owner"])` já usado hoje pelo `Sidebar.tsx` atual é reaproveitado como está; nenhuma regra de autorização nova nesta fundação. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Tenant switcher precisa de nome/plano do tenant, que `/api/auth/me` hoje não devolve (`TenantMembership` só tem `tenant_id`/`role`) | Estender `/api/auth/me` (memberships) com `name` e `plan_tier` do tenant | Decisão do usuário — sem isso o switcher (e o `TenantSelector.tsx` existente, que hoje mostra `tenant_id` cru) fica com apresentação quebrada | y |
| Sino de notificação no topbar, sem feature de Notificações pronta | Ícone estático, sem badge, sem link funcional | Decisão do usuário — evita rota quebrada ou contagem falsa | y |
| Tema padrão no primeiro load (app é dark-only hoje) | Light | Decisão do usuário — segue o padrão do handoff | y |
| Persistência de tema e de "Fixar menu" (pin do sidebar) | `localStorage`, client-side apenas | Mesma convenção que o próprio handoff já recomenda ("should be persisted per-user server-side or in localStorage in production") combinada com a decisão explícita de não fazer persistência server-side nesta fundação | y |
| Item "Meu perfil" no menu do avatar (página ainda não existe) | Omitido até a página existir — menu do avatar mostra só nome/email, "Configurações" (owner-only, mesma gate atual), "Sair" | Evita link morto; consistente com a decisão do sino | y |
| Item "Planos & Faturamento" no grupo "Organização" (fora de escopo deste ciclo) | Omitido do nav até a feature existir | Mesma lógica dos itens acima — nenhum link morto nesta fundação | y |
| Flash de tema errado no load (tema salvo aplicado antes do primeiro paint) | Aplicar o atributo de tema via script síncrono antes da hidratação do React, evitando flash perceptível | Prática padrão de SPA para evitar FOUC de tema; testável observando o atributo already-set no `document` antes do primeiro render | y |

**Open questions:** none — todas resolvidas ou registradas acima.

---

## User Stories

### P1: Tokens de design novos aplicados globalmente ⭐ MVP

**User Story**: Como usuário do Vane, quero que o app use a paleta, tipografia e temas (claro/escuro) do novo design, para que a experiência visual já reflita a marca redesenhada.

**Why P1**: Sem os tokens trocados, nenhuma tela nova nem o shell novo têm onde se apoiar — é a base de tudo que vem depois.

**Acceptance Criteria**:

1. The system SHALL definir tokens de tema claro e escuro (background de página, texto primário/mudo, background/borda de sidebar, background/hover de card, hover de sidebar, cor de ícone do topbar) com os valores hexadecimais documentados em `handoff-new-layout/README.md`.
2. The system SHALL carregar a fonte Manrope (pesos 400/500/600/700) como fonte da aplicação, substituindo Inter.
3. The system SHALL usar `#5A46C7` como cor de marca (hover/active `#4C3AAE`) em ambos os temas.
4. The system SHALL manter as cores de status/severidade/papel (verde `#1A9E6B`, âmbar `#B45309`, vermelho `#D6395B`, roxo `#5A46C7`) idênticas nos dois temas — só o tom de fundo claro dessas cores muda entre os temas.
5. WHEN o app carrega sem preferência de tema salva THEN o system SHALL renderizar o tema claro por padrão.
6. WHEN o usuário aciona o toggle de tema THEN o system SHALL trocar todos os tokens temáticos imediatamente e persistir a escolha em `localStorage`.
7. WHEN o app carrega com uma preferência de tema salva THEN o system SHALL aplicar esse tema via atributo síncrono antes da primeira renderização do React, sem exibir o tema oposto primeiro.

**Independent Test**: Abrir o app sem `localStorage` prévio → tema claro. Acionar o toggle → tema escuro aplicado e persistido; recarregar a página → tema escuro reaparece sem flash do claro.

---

### P2: Shell de aplicação novo (sidebar + topbar) ⭐ MVP

**User Story**: Como usuário autenticado, quero navegar pelo Vane através do novo shell (sidebar colapsável + topbar), para que a navegação siga o layout redesenhado antes mesmo de cada tela de conteúdo ser portada.

**Why P1/P2**: É o elemento estrutural presente em toda página autenticada — sem ele, nenhuma tela migrada teria onde viver.

**Acceptance Criteria**:

1. WHEN uma rota autenticada é renderizada THEN o system SHALL envolvê-la no shell novo (sidebar + topbar + área de conteúdo), substituindo o `Sidebar.tsx` atual.
2. The system SHALL renderizar a sidebar com `72px` de largura no estado colapsado e `240px` no estado expandido.
3. WHEN o ponteiro entra na sidebar colapsada THEN o system SHALL expandi-la; WHEN o ponteiro sai THEN o system SHALL colapsá-la de volta — EXCETO enquanto o pin ("Fixar menu") estiver ativo, caso em que o system SHALL mantê-la expandida independentemente do hover.
4. WHEN o usuário aciona "Fixar menu" THEN o system SHALL persistir essa escolha em `localStorage` e mantê-la entre recarregamentos.
5. The system SHALL agrupar os itens de navegação em três grupos rotulados (Monitoramento: Serviços monitorados/Domínios & Status/Incidentes; Plataforma: Integrações/Poller Status; Organização: Usuários), omitindo "Planos & Faturamento" enquanto essa feature não existir (ver Out of Scope).
6. The system SHALL aplicar o mesmo gate de papel que o `Sidebar.tsx` atual já usa (`hasRole(["owner"])`) aos itens equivalentes (Usuários, Configurações) no shell novo.
7. WHILE o item de navegação corresponde à rota atual, the system SHALL destacá-lo com fundo `rgba(90,70,199,0.08)` e texto/ícone `#5A46C7`.
8. The system SHALL renderizar uma topbar de `60px` de altura contendo: título da página atual à esquerda; à direita, nessa ordem, toggle de tema, sino de notificação (estático, sem badge, sem link — ver Assumptions), e menu de avatar.
9. WHEN o usuário abre o menu de avatar THEN o system SHALL mostrar nome + email do usuário autenticado, o link "Configurações" (mesma gate de papel do item de nav) e "Sair" (abre o modal de confirmação de logout já existente).
10. IF o usuário autenticado tem exatamente 1 `tenant_membership` THEN o system SHALL exibir avatar+nome do tenant sem popover de troca (sem seletor).
11. WHEN um usuário com 2+ `tenant_membership` abre o seletor de tenant THEN o system SHALL listar todos os seus tenants (avatar de iniciais, nome, badge de plano), marcar o tenant ativo com indicador visual, e selecionar outro tenant SHALL chamar o `switchTenant` já existente (`AuthProvider`).
12. The system SHALL renderizar a área de conteúdo com scroll próprio, padding `32px 40px`, e max-width `1200px` centrado.

**Independent Test**: Logar com um usuário de 1 tenant → sidebar/topbar novos aparecem, sem popover de troca; navegar entre 2 páginas existentes → título do topbar muda, item ativo destacado. Logar com um usuário de 2+ tenants → popover de troca lista os tenants com nome real (não IDs crus) e badge de plano; trocar de tenant funciona como hoje.

---

### P3: `/api/auth/me` passa a incluir nome/plano do tenant em cada membership

**User Story**: Como frontend, quero que cada `tenant_membership` retornada já traga nome e plano do tenant, para que o seletor de tenant (e o `TenantSelector.tsx` de tela cheia já existente) mostrem dado real em vez do `tenant_id` cru.

**Why P3**: É um gap pequeno e localizado (2 campos), mas bloqueante para a AC 11 da P2 acima — sem isso o switcher mostra UUID em vez de nome.

**Acceptance Criteria**:

1. WHEN `GET /api/auth/me` é chamado por um usuário autenticado THEN cada item de `memberships` SHALL incluir `name` (nome do tenant) e `plan_tier` (plano do tenant), além dos campos `tenant_id`/`role` já existentes.
2. IF um tenant referenciado por uma membership não tiver `plan_tier` definido (campo de texto livre, `AD-022`) THEN o system SHALL devolver o valor armazenado tal como está, sem inventar um default no backend — a ausência de rótulo amigável fica a cargo do frontend (ex.: pill cinza "Free" já é o comportamento visual do handoff para plano vazio/"free").

**Independent Test**: Logar com um usuário de 2+ memberships em tenants com nomes/planos diferentes; `GET /api/auth/me` retorna `name`/`plan_tier` reais para cada um.

---

## Edge Cases

- IF `localStorage` estiver indisponível (modo privado restritivo, quota) THEN o system SHALL degradar para o padrão (tema claro, sidebar não fixada) sem lançar erro visível ao usuário.
- IF um tenant em `memberships` não tiver `name` (linha de dado legado/edge case) THEN o system SHALL usar `tenant_id` como fallback de exibição no seletor, em vez de quebrar a renderização.
- WHEN a janela é redimensionada com a sidebar expandida por hover (não fixada) THEN o system SHALL manter o comportamento de colapso ao mouse-leave inalterado (sem estado extra de breakpoint nesta fundação — responsividade mobile fica fora de escopo, não solicitada).

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| SHELL-01 | P1: Tokens novos | Design | Verified |
| SHELL-02 | P1: Tokens novos | Design | Verified |
| SHELL-03 | P1: Tokens novos | Design | Verified |
| SHELL-04 | P1: Tokens novos | Design | Verified |
| SHELL-05 | P1: Tokens novos | Design | Verified |
| SHELL-06 | P1: Tokens novos | Design | Verified |
| SHELL-07 | P1: Tokens novos | Design | Verified |
| SHELL-08 | P2: Shell novo | Design | Verified |
| SHELL-09 | P2: Shell novo | Design | Verified |
| SHELL-10 | P2: Shell novo | Design | Verified |
| SHELL-11 | P2: Shell novo | Design | Verified |
| SHELL-12 | P2: Shell novo | Design | Verified |
| SHELL-13 | P2: Shell novo | Design | Verified |
| SHELL-14 | P2: Shell novo | Design | Verified |
| SHELL-15 | P2: Shell novo | Design | Verified |
| SHELL-16 | P2: Shell novo | Design | Verified |
| SHELL-17 | P2: Shell novo | Design | Verified |
| SHELL-18 | P2: Shell novo | Design | Verified |
| SHELL-19 | P2: Shell novo | Design | Verified |
| SHELL-20 | P3: /api/auth/me | Design | Verified |
| SHELL-21 | P3: /api/auth/me | Design | Verified |

**Coverage:** 21 total, 21 verified (`validation.md`). SHELL-01/02/03/07/19 were spec-precision gaps (exact literals untested) closed by the 2026-09-12 hygiene pass; the rest were already test-backed.

---

## Success Criteria

- [ ] `web/src/styles/tokens.css` substituído — nenhum token Nocturne (paleta antiga, Inter) restante.
- [ ] Toda rota autenticada renderiza dentro do shell novo (sidebar colapsável + topbar), sem regressão de navegação existente.
- [ ] Toggle de tema funciona e persiste; sem flash perceptível de tema errado no reload.
- [ ] Seletor de tenant mostra nome/plano real (não `tenant_id` cru), tanto no popover novo quanto no `TenantSelector.tsx` de tela cheia existente.
- [ ] Gates de papel (owner-only) preservados nos itens equivalentes do novo shell.
