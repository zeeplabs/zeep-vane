# Users Page Specification

## Problem Statement

`AdminsPage` (tela "Usuários", `gap-analysis.md` item 7) hoje é pré-redesenho: 2 listas separadas (Ativos/Convites pendentes) sem chips de filtro, sem busca, sem drawer de detalhe, e sem rótulo em português (`Owner/Operator/Viewer` em vez do mapeamento decidido `Admin/Membro/Leitura`). O mock (`handoff-new-layout/Usuarios.dc.html`) unifica tudo numa única tabela com chips por papel, busca, drawer de detalhe (papel + último acesso + remover/reenviar) e um banner de limite de seat por plano que não existe no backend real.

## Goals

- [ ] Tela bate visualmente com `handoff-new-layout/Usuarios.dc.html`: header, chips de filtro por papel com contagem, busca por nome/email, tabela única (ativos + pendentes), drawer de detalhe.
- [ ] Rótulos de papel em português: `owner`→"Admin", `operator`→"Membro", `viewer`→"Leitura" (mapeamento de UI já decidido em `gap-analysis.md`, nenhuma mudança de modelo/permissão).
- [ ] Coluna "Último acesso" reflete dado real (`sessions.last_seen_at` do usuário), não texto decorativo.
- [ ] Convite de usuário continua exigindo nome (obrigatório no backend) e papel — telefone opcional — mesmo o mock não pedindo nome.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Banner de limite de seats por plano ("Você atingiu o limite de X usuários") + botão "Fazer upgrade" | Decisão do usuário (`AskUserQuestion`): seat-limit por plano está pausado (AD-025) — `Tenant.Plan` é texto livre sem limite definido em lugar nenhum, sem integração com `zeep-license-server`. Mostrar isso exigiria inventar um limite. Fica de fora até essa integração existir. |
| Bloqueio de "Convidar usuário" por limite de plano | Consequência direta do item acima — convite nunca é bloqueado por seat count nesta tela. |
| Reenviar/cancelar convite pendente | Já existe (`admin-invite-resend-cancel`), reaproveitado sem mudança de contrato — só reposicionado visualmente dentro do drawer/tabela unificada. |
| Novo 4º papel ou matriz de permissão configurável | `gap-analysis.md`: mapeamento é só rótulo de UI, sem mudança de modelo. |
| Rastreamento de sessão por dispositivo nesta tela | Já coberto por `user-sessions`/Meu Perfil — "Último acesso" aqui é só o `last_seen_at` mais recente do usuário, agregado, sem lista de dispositivos. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Banner de seat-limit | Omitir inteiramente (banner + botão upgrade) | Decisão explícita do usuário via `AskUserQuestion` — não fabricar limite inexistente. | y |
| Campos do drawer "Convidar usuário" | Nome (obrigatório) + Telefone (opcional) + Email (obrigatório) + Papel — mock só tem Email+Papel | Backend (`POST /api/admins`) rejeita sem `name`; mock é protótipo simplificado. Visual do drawer (radio-boxes de papel, copy) segue o mock; só adiciona os 2 campos que faltam. | y |
| "Último acesso" | Novo campo `last_access` (nullable) no `adminResponse`, calculado como o maior `sessions.last_seen_at` (ou `created_at` se `last_seen_at` nunca foi tocado) entre as sessões do usuário; `null` quando o usuário nunca teve sessão (convite pendente) — frontend renderiza "—" nesse caso, igual ao mock. | Dado real e derivável (tabela `sessions` já existe de `user-sessions`), evita render decorativo. Não inclui dispositivo/IP — só o agregado. | y |
| Mapeamento de rótulo de papel | `owner`→"Admin", `operator`→"Membro", `viewer`→"Leitura" — só na camada de apresentação (label/i18n), nenhuma mudança de valor persistido/contrato de API (`role` continua `"owner"/"operator"/"viewer"`). | Decisão já registrada em `gap-analysis.md`, nunca implementada em nenhuma tela ainda. | y |
| Papel do usuário logado no drawer de detalhe | Segue o mock: qualquer usuário listado (inclusive o próprio ator) pode ter o papel alterado ou ser removido pelo owner, exceto quando isso deixaria o tenant sem nenhum owner — `ErrLastOwner`/409 já existente (`ADM-06`), sem mudança de comportamento, só de camada visual. | Comportamento já implementado (`TenantMembershipRepository.UpdateRole`/`Delete`); esta feature só redesenha a UI que o consome. | y |
| Filtro "Todos/Admin/Membro/Leitura" e busca | Client-side sobre a página carregada (mesma paginação já existente, `Page[T]`) — sem novo parâmetro de query no backend. | Escopo pequeno; lista de usuários de um tenant tende a ser curta (dezenas, não milhares); nenhum outro filtro client-side desta migração precisou de suporte de backend. | y |

**Open questions:** none — todas resolvidas ou registradas acima.

---

## User Stories

### P1: Tabela unificada com chips de papel e busca ⭐ MVP

**User Story**: Como owner, quero ver todos os usuários (ativos e convites pendentes) numa única tabela filtrável por papel e pesquisável por nome/e-mail, para gerenciar acesso ao tenant rapidamente.

**Why P1**: É a mudança visual central do redesenho — o resto (drawer, convite) depende da tabela existir primeiro.

**Acceptance Criteria**:

1. WHEN a tela `/admins` carrega THEN o sistema SHALL exibir uma única tabela com todos os usuários da página atual (`GET /api/admins?page=N`), ativos e pendentes juntos, ordenados como o backend já retorna.
2. The system SHALL exibir 4 chips de filtro ("Todos", "Admin", "Membro", "Leitura") cada um com a contagem de usuários daquele papel na página carregada; clicar um chip filtra a tabela client-side.
3. WHEN o owner digita na busca THEN o sistema SHALL filtrar a tabela (client-side) por substring case-insensitive em nome OU e-mail.
4. IF a combinação de filtro+busca não retornar nenhuma linha THEN o sistema SHALL exibir "Nenhum usuário encontrado com esses filtros." (mock).
5. The system SHALL exibir o rótulo de papel traduzido (Admin/Membro/Leitura) em toda a tela — tabela, chips, drawer — nunca os valores crus `owner/operator/viewer`.

**Independent Test**: abrir `/admins`, ver a tabela com todos os usuários da seed; clicar em "Admin" e ver só o(s) admin(s); digitar parte de um e-mail e ver o filtro combinado.

---

### P1: Drawer de detalhe (papel + último acesso + remover/reenviar) ⭐ MVP

**User Story**: Como owner, quero clicar num usuário e ver/alterar o papel dele, ver o último acesso e removê-lo (ou reenviar convite se pendente), num painel de detalhe.

**Why P1**: É a ação de gestão real da tela — sem isso a tabela é só leitura.

**Acceptance Criteria**:

1. WHEN o owner clica numa linha THEN o sistema SHALL abrir um drawer de detalhe com badge de status, nome/e-mail, seletor de papel (3 opções em caixas, como o mock) e "Último acesso".
2. WHEN o owner seleciona um papel diferente no drawer THEN o sistema SHALL chamar `PATCH /api/admins/{id}/role` e refletir o novo papel; IF a mudança deixaria o tenant sem nenhum owner THEN o sistema SHALL manter o papel anterior e exibir a mensagem de erro do backend (409, `ADM-06`).
3. WHILE o usuário selecionado está com status "Pendente" o sistema SHALL exibir o botão "Reenviar convite" no drawer; WHILE está "Ativo" o sistema SHALL ocultá-lo.
4. WHEN o owner clica "Remover usuário" (ativo) ou o equivalente para convite pendente THEN o sistema SHALL chamar o endpoint de remoção/cancelamento já existente e fechar o drawer em caso de sucesso.
5. The system SHALL exibir "Último acesso" formatado (ex.: "há 2 min"/"há 3 horas"/"há 1 dia") quando `last_access` não for nulo, e "—" quando for nulo.

**Independent Test**: abrir o drawer de um usuário ativo, trocar o papel, ver refletido na tabela; abrir o drawer de um convite pendente, ver "Reenviar convite" visível e "Último acesso" = "—".

---

### P2: Convidar usuário via drawer

**User Story**: Como owner, quero convidar um novo usuário informando nome, e-mail, telefone opcional e papel, através de um drawer lateral (em vez do modal atual).

**Why P2**: Reaproveita fluxo já existente (`useInviteAdmin`), só muda o container visual — menor risco que as histórias P1.

**Acceptance Criteria**:

1. WHEN o owner clica "Convidar usuário" THEN o sistema SHALL abrir um drawer com campos Nome, E-mail, Telefone (opcional) e um seletor de papel em 3 caixas (Admin/Membro/Leitura).
2. WHEN o formulário é submetido com nome e e-mail válidos THEN o sistema SHALL chamar `POST /api/admins` e fechar o drawer em caso de sucesso, exibindo a nova linha (pendente) na tabela.
3. IF o backend rejeitar (e-mail duplicado, papel inválido, etc.) THEN o sistema SHALL manter o drawer aberto e exibir a mensagem de erro inline.

**Independent Test**: abrir o drawer de convite, preencher nome+e-mail+papel, enviar, ver a nova linha "Pendente" na tabela.

---

## Edge Cases

- IF um usuário nunca teve nenhuma sessão (`sessions` sem linha para esse `user_id`) THEN `last_access` SHALL ser `null`, renderizado como "—" (nunca erro, nunca data fabricada).
- IF a busca não corresponder a nenhum papel do chip ativo THEN o sistema SHALL mostrar o estado vazio (não um erro).
- IF o owner tentar remover a si mesmo sendo o único owner THEN o sistema SHALL manter o comportamento já existente (`ErrLastOwner`, 409, sem mudança nesta feature).

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| USRPG-01 | P1: Tabela unificada | - | Pending |
| USRPG-02 | P1: Tabela unificada | - | Pending |
| USRPG-03 | P1: Tabela unificada | - | Pending |
| USRPG-04 | P1: Tabela unificada | - | Pending |
| USRPG-05 | P1: Tabela unificada | - | Pending |
| USRPG-06 | P1: Drawer de detalhe | - | Pending |
| USRPG-07 | P1: Drawer de detalhe | - | Pending |
| USRPG-08 | P1: Drawer de detalhe | - | Pending |
| USRPG-09 | P1: Drawer de detalhe | - | Pending |
| USRPG-10 | P1: Drawer de detalhe | - | Pending |
| USRPG-11 | P2: Convidar via drawer | - | Pending |
| USRPG-12 | P2: Convidar via drawer | - | Pending |
| USRPG-13 | P2: Convidar via drawer | - | Pending |

**ID format:** `USRPG-[NUMBER]`

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 13 total, 13 mapped to tasks (implícitas, escopo Medium), 0 unmapped

---

## Success Criteria

- [ ] Tela `/admins` bate visualmente com `handoff-new-layout/Usuarios.dc.html` (menos o banner de seat-limit, explicitamente fora de escopo).
- [ ] `GET /api/admins` retorna `last_access` real (derivado de `sessions`), sem migration nova.
- [ ] `go build`/`go vet`/`gofmt` limpos; `tsc -b --noEmit` e suíte de frontend verdes.
