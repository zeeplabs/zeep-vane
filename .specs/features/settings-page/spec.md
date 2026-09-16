# Settings Page Specification

## Problem Statement

`SettingsPage` (tela "Configurações", `gap-analysis.md` item 9 — último item restante do `new-layout-migration`) hoje só cobre nome/e-mail/logo via `company-settings` (SET-01..16). O mock (`handoff-new-layout/Configuracoes.dc.html`) pede 3 coisas que não existem: (1) site + fuso horário do tenant, (2) endereço fiscal completo (hoje deliberadamente oculto — `companySettingsResponse` nunca expõe `BillingAddress`), (3) uma zona de perigo "Excluir conta" que apaga o tenant inteiro — não existe nenhum endpoint de exclusão de tenant hoje.

## Goals

- [ ] Owner visualiza e edita perfil da empresa completo: nome, site, fuso horário, idioma, logo (já existe) — persistido via `GET`/`PATCH /api/company-settings`.
- [ ] Owner visualiza e edita dados fiscais completos: tipo de pessoa, razão social/CNPJ ou nome/CPF, inscrição estadual (PJ), endereço completo (CEP/rua/número/complemento/estado/cidade/país) — persistido nos mesmos endpoints.
- [ ] Owner apaga o tenant atual (soft delete) via um novo endpoint, com confirmação de 2 passos na UI, exceto quando esse for o único tenant do próprio usuário.
- [ ] Tela redesenhada bate visualmente com `handoff-new-layout/Configuracoes.dc.html` (cards, inputs, zona de perigo).

## Out of Scope

| Feature | Reason |
| --- | --- |
| Hard delete / cascade real do tenant e suas linhas | Decisão do usuário: soft delete agora. Apagar de fato os dados fica para uma rotina de expurgo futura (fora deste ciclo). |
| Exclusão de tenant alheio (não é o próprio usuário) | Endpoint sempre opera sobre o tenant ativo da sessão (`ActiveTenantIDFromContext`), nunca aceita um tenant ID arbitrário no corpo. |
| Reativar tenant soft-deleted | Nenhuma tela/endpoint de "desfazer exclusão" pedido; sem UI, sem endpoint. |
| Validação de CEP/endereço contra API dos Correios | Mock só tem inputs de texto livre; nenhuma integração de validação de endereço foi pedida. |
| Seat limit / billing / plano | Fora do escopo deste ciclo (`gap-analysis.md`, item 8, pausado). |
| Multi-tenant company settings (por-org rows) | Sem mudança — `tenants` já é a linha por-tenant desde AD-022; nenhuma tabela nova aqui. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Abordagem de exclusão de tenant | **Soft delete**: `tenants.status = 'deleted'` + `tenants.deleted_at` novo (nullable). Nenhuma linha filha é apagada. | Decisão explícita do usuário (`AskUserQuestion`): "a ideia é ser soft delete nesse momento" — descarta tanto migration com `ON DELETE CASCADE` quanto delete transacional tabela-a-tabela. | y |
| Bloqueio do último tenant | Bloquear a exclusão se o tenant a apagar for o único tenant ativo do usuário que chamou o endpoint (`TenantMembershipRepository.ListForUser` filtrado a `status='active'` retorna ≤1 linha). Responde 409. | Decisão explícita do usuário: cobre o caso self-hosted (1 tenant só) sem precisar detectar modo de deploy — qualquer usuário sem outro tenant fica travado, não só self-hosted. | y |
| Exposição de endereço fiscal + site/timezone | Expor agora: `Tenant` ganha `website`/`timezone` (migration nova) e `BillingAddress` (já existe como `[]byte` JSON cru) passa a ser lido/gravado por este endpoint via struct tipada `TenantBillingAddress{Zip,Street,Number,Complement,State,City,Country}`. Supersede o comentário existente em `company_settings_handler.go` ("SaaS billing feature owns that field's exposure"). | Decisão explícita do usuário — a tela pede exatamente isso; não faz sentido pausar até uma feature de billing que nem tem spec. | y |
| Idioma | Reusa `Tenant.Locale` já existente (não cria campo novo) — `language` no mock mapeia 1:1 pra `locale`. | `Locale` já existe na tabela desde AD-022 e nunca foi exposto por este handler; nenhum motivo pra campo redundante. | y |
| Quem pode apagar o tenant | Só `owner` (mesma regra de toda ação destrutiva já existente — invite/remove admin, rotate de integração). `RequireRole("owner")`. | Consistente com o resto do app; excluir a conta inteira é a ação mais destrutiva que existe, não faz sentido abrir pra `operator`/`viewer`. | y |
| Efeito sobre outros membros do tenant apagado | Nenhum tratamento especial de sessão ativa: a próxima requisição de qualquer membro (inclusive quem apagou) contra esse tenant falha fail-closed (ver CFGPG-09) porque o papel deixa de resolver — igual ao comportamento já existente quando a membership é removida. Se esse membro não tiver outro tenant, fica sem tenant selecionável (mesmo estado de uma conta nova sem convite) — nenhum log-out forçado, nenhum e-mail de aviso. | Menor superfície nova: reusa o mesmo mecanismo fail-closed que já existe pra membership removida (`ErrNotFound` → role vazio → `RequireRole` rejeita), em vez de inventar um mecanismo de revogação de sessão só para isto. Notificar os outros membros por e-mail é feature nova não pedida. | y |
| Confirmação de exclusão na UI | 2 passos, igual ao mock: botão "Excluir conta" na zona de perigo abre modal com texto de aviso + botão "Excluir conta" vermelho — sem exigir digitar o nome do tenant (mock não pede isso). | Seguir o mock literalmente; nenhuma fricção adicional foi pedida. | y |
| Rota do endpoint de exclusão | `DELETE /api/tenants/current` (não `/api/company-settings` — apagar o tenant não é "atualizar configurações", é uma ação distinta e mais destrutiva; separar o endpoint deixa o RBAC e o teste isolados). | Segue o padrão já usado pelo projeto de rotas de ação separadas de rotas de CRUD (ex.: `/logo` separado de `PATCH /api/company-settings`). | y |
| Toast "Alterações salvas" | Front-end mostra o toast (mock) só após um `PATCH` bem-sucedido, 2.2s, igual ao timing do mock. | Comportamento puramente visual, sem ambiguidade — mock já define o timing exato no `setTimeout`. | y |

**Open questions:** none — todas resolvidas ou registradas acima.

---

## User Stories

### P1: Perfil da empresa completo ⭐ MVP

**User Story**: Como owner, quero editar nome, site, fuso horário e idioma da minha empresa, para que a conta reflita os dados reais usados nas páginas de status públicas e nas notificações.

**Why P1**: É a seção principal da tela e a que o resto do app (status pages públicas) já consome parcialmente (nome/logo) — completar os campos que faltam é a menor unidade demonstrável.

**Acceptance Criteria**:

1. WHEN o owner abre `/settings` THEN o sistema SHALL carregar `GET /api/company-settings` e preencher nome, site, fuso horário e idioma com os valores persistidos do tenant ativo.
2. WHEN o owner edita nome/site/fuso horário/idioma e clica "Salvar alterações" THEN o sistema SHALL enviar `PATCH /api/company-settings` com os 4 campos e, em caso de sucesso (200), exibir o toast "Alterações salvas" por 2.2s.
3. IF `website` for uma string vazia ou omitida THEN o sistema SHALL aceitar (campo opcional, sem formato de URL validado no backend — mock não valida).
4. The system SHALL persistir `timezone` como um dos 3 valores literais oferecidos pelo seletor do mock (`"America/Sao_Paulo (GMT-3)"`, `"America/New_York (GMT-5)"`, `"UTC (GMT+0)"`) — qualquer outro valor recebido pelo backend responde 422.
5. The system SHALL manter `/settings` acessível somente a `owner` — `RequireRole(["owner"])` no frontend e `RequireRole(db.RoleOwner)` em toda rota `/api/company-settings*` e `/api/tenants/current`, já existente hoje; esta feature não introduz um modo somente-leitura para `operator`/`viewer` (não pedido pelo mock, e o backend já nega 403 pra esses papéis).

**Independent Test**: abrir `/settings`, mudar nome + site, salvar, recarregar a página e confirmar que os valores persistiram.

---

### P1: Dados fiscais completos ⭐ MVP

**User Story**: Como owner, quero cadastrar o endereço fiscal completo da empresa (além do que já existe: tipo de pessoa, razão social/CNPJ), para que a conta tenha os dados corretos para emissão de notas fiscais.

**Why P1**: É o gap real identificado no `gap-analysis.md` — sem isso a seção "Dados fiscais" do mock fica com metade dos campos sempre vazios e sem persistência.

**Acceptance Criteria**:

1. WHEN o owner preenche CEP/endereço/número/complemento/estado/cidade/país e salva THEN o sistema SHALL persistir esses 7 campos como `billing_address` (JSON) no tenant ativo via `PATCH /api/company-settings`.
2. WHEN o owner abre `/settings` e o tenant já tem `billing_address` persistido THEN o sistema SHALL preencher os 7 campos com os valores salvos.
3. IF `billing_address` nunca foi definido (tenant novo) THEN o sistema SHALL renderizar os 7 campos vazios, sem erro.
4. The system SHALL manter as regras já existentes de `legal_name`/`tax_id`/`tax_id_type` (SET-04/05, validação de dígitos por tipo) inalteradas — esta feature só adiciona campos, não muda validação de CPF/CNPJ.
5. WHERE o tipo de pessoa é "Pessoa Jurídica" o sistema SHALL exibir e aceitar o campo "Inscrição estadual" opcional; WHERE é "Pessoa Física" o sistema SHALL ocultar esse campo (comportamento puramente de frontend, já existente, sem mudança de contrato).

**Independent Test**: preencher os 7 campos de endereço, salvar, recarregar a página e ver os 7 valores de volta.

---

### P1: Excluir conta (zona de perigo) ⭐ MVP

**User Story**: Como owner, quero apagar minha conta (tenant) quando não preciso mais dela, para encerrar o uso do Vane sem depender de suporte.

**Why P1**: É o gap arquitetural real do item 9 — não existe hoje nenhum caminho pra um owner encerrar a própria conta.

**Acceptance Criteria**:

1. WHEN o owner clica "Excluir conta" THEN o sistema SHALL abrir um modal de confirmação com o aviso "Essa ação é permanente..." antes de qualquer chamada de API.
2. WHEN o owner confirma no modal THEN o sistema SHALL chamar `DELETE /api/tenants/current` e, em caso de sucesso (200), redirecionar para a tela de seleção de tenant (ou login, se não houver outro tenant).
3. The system SHALL, ao processar `DELETE /api/tenants/current`, setar `tenants.status = 'deleted'` e `tenants.deleted_at = now()` no tenant ativo da sessão — nenhuma linha de nenhuma outra tabela é apagada ou alterada.
4. IF o tenant ativo for o único tenant com `status = 'active'` do usuário autenticado THEN o sistema SHALL responder 409 e não alterar nenhum dado, exibindo uma mensagem clara na UI ("Esta é sua única conta ativa — não é possível excluí-la").
5. IF o papel do usuário autenticado no tenant ativo não for `owner` THEN o sistema SHALL responder 403 e a UI SHALL nunca exibir o botão/zona de perigo pra esse usuário (consistente com a AC de RBAC da primeira história).
6. WHILE um tenant está com `status = 'deleted'` o sistema SHALL tratá-lo como inexistente para toda resolução de papel (`TenantContext`/`GetRole`) — qualquer requisição tenant-scoped contra ele falha fail-closed (401/403), igual ao caso de uma membership removida.
7. WHILE um tenant está com `status = 'deleted'` o sistema SHALL excluí-lo da lista retornada por `TenantMembershipRepository.ListForUser` (login/seletor de tenant nunca mais o oferece como opção).
8. WHILE `deploymentMode === "self_hosted"` o sistema SHALL nunca exibir a zona de perigo ("Excluir conta") em `/settings`, e `DELETE /api/tenants/current` SHALL responder 404 (mesmo gate `requireSaaSMode`/AD-033 já usado por `/api/signup`) — uma instalação self-hosted é single-tenant por design (AD-002); "apagar o tenant" destruiria a instalação inteira, não encerraria uma conta SaaS.

**Independent Test**: criar um segundo tenant de teste pro mesmo usuário, excluir o tenant ativo, confirmar que ele some da lista de seleção e que qualquer chamada tenant-scoped anterior contra ele responde fail-closed.

---

## Edge Cases

- IF o `PATCH /api/company-settings` receber um `timezone` fora dos 3 valores válidos THEN o sistema SHALL responder 422 (mesmo padrão de erro de validação já usado por SET-04/05).
- IF `DELETE /api/tenants/current` for chamado sem uma sessão com tenant ativo (`active_tenant_id` vazio) THEN o sistema SHALL responder 401/403 (mesma checagem que já existe em toda rota tenant-scoped).
- IF `DELETE /api/tenants/current` for chamado duas vezes seguidas (tenant já `deleted`) THEN o sistema SHALL responder de forma consistente com "tenant não encontrado/sem acesso" (a própria checagem fail-closed do CFGPG-09 já cobre isso, sem necessidade de um branch dedicado).
- WHEN o owner descarta as alterações (botão "Descartar") THEN o sistema SHALL reverter os campos ao último valor persistido (comportamento puramente de frontend — sem chamada de API).

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| CFGPG-01 | P1: Perfil da empresa completo | - | Verified |
| CFGPG-02 | P1: Perfil da empresa completo | - | Verified |
| CFGPG-03 | P1: Perfil da empresa completo | - | Verified |
| CFGPG-04 | P1: Perfil da empresa completo | - | Verified |
| CFGPG-05 | P1: Perfil da empresa completo | - | Verified |
| CFGPG-06 | P1: Dados fiscais completos | - | Verified |
| CFGPG-07 | P1: Dados fiscais completos | - | Verified |
| CFGPG-08 | P1: Dados fiscais completos | - | Verified |
| CFGPG-09 | P1: Excluir conta | - | Verified |
| CFGPG-10 | P1: Excluir conta | - | Verified |
| CFGPG-11 | P1: Excluir conta | - | Verified |
| CFGPG-12 | P1: Excluir conta | - | Verified |
| CFGPG-13 | P1: Excluir conta | - | Verified |
| CFGPG-14 | P1: Excluir conta | - | Verified |

**ID format:** `CFGPG-[NUMBER]`

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 14 total, 14 mapped to tasks (implícitas, escopo Medium), 0 unmapped (CFGPG-14 adicionado em 2026-09-16, achado de bug report ao vivo: zona de perigo/`DELETE /api/tenants/current` não tinha gate `deploymentMode`/AD-033 nem AD-002 no ciclo original)

---

## Success Criteria

- [ ] Tela `/settings` bate visualmente com `handoff-new-layout/Configuracoes.dc.html` (cards, inputs `filled`, zona de perigo).
- [ ] `GET`/`PATCH /api/company-settings` cobrem os 4 campos novos de perfil (site/timezone/idioma já cobertos parcialmente antes — idioma é novo) + os 7 campos de endereço fiscal.
- [ ] `DELETE /api/tenants/current` implementado, testado (owner-only, bloqueio de último tenant, soft delete real, fail-closed pós-delete).
- [ ] `go build`/`go vet`/`go test ./...` limpos; `tsc -b --noEmit` e suíte de frontend verdes.
