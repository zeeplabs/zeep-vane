# Admin Dashboard Specification

## Problem Statement

Os 25 requisitos administrativos já especificados em `mvp-core` (conectar Datadog, gerenciar domínio/status page, gerenciar incidente) só são operáveis via chamada direta de API — não existe interface nem controle de acesso por papel. Sem isso, o produto não é usável por uma equipe real, só por 1 pessoa técnica com acesso à API. Este spec adiciona múltiplos admins com papéis fixos, autorização por papel sobre os endpoints do mvp-core, e visão de status do poller.

## Goals

- [ ] Owner convida um novo admin com papel definido e ele consegue logar em menos de 5 minutos (tempo de vida do token de convite)
- [ ] Nenhuma ação de escrita do mvp-core é executável por um papel sem permissão (retorna 403)
- [ ] Remover/rebaixar um admin invalida a sessão dele imediatamente, não espera expiração natural do token

## Out of Scope

Excluído explicitamente desta spec (MVP). Documentado para prevenir scope creep.

| Feature | Reason |
| --- | --- |
| Matriz de permissão configurável por recurso/ação (RBAC granular de fato) | Só 3 papéis fixos no MVP; ver AD-003 em STATE.md |
| SSO/SAML | Mantido fora do MVP — já excluído em `mvp-core` |
| Auto-cadastro de admin sem convite | Só owner pode criar novos admins, via convite |
| UI de auditoria (busca/filtro sobre o audit log) | MVP só registra o log; tela de consulta é fase seguinte |
| Notificação de convite por canal além de email | Email é o único canal no MVP, consistente com reset de senha do mvp-core |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Retenção do audit log | Indefinida (sem expiração automática) | Log de ação de segurança não deve ser descartado por padrão; revisitar se virar problema de armazenamento | n |
| Self-demotion do próprio owner | Permitido, sujeito à mesma regra de proteção contra lockout (ADM-06) | Consistente com o comportamento pedido pelo usuário para remoção/rebaixamento de terceiros | n |
| Limpeza da denylist de JWT revogado | Entrada é removida quando o JWT original já teria expirado naturalmente | Evita crescimento infinito da denylist sem reduzir a garantia de revogação imediata | n |
| Campos exibidos na tela de status do poller | Timestamp da última execução, resultado (sucesso/falha), mensagem de erro se houver, por integração Datadog conectada | Reaproveita o mesmo dado já calculado pro fallback público (SP-09 do mvp-core), sem novo cálculo | n |

**Open questions:** none — todas resolvidas ou registradas acima.

---

## User Stories

### P1: Owner gerencia admins com papéis fixos ⭐ MVP

**User Story**: Como owner, quero convidar, mudar o papel e remover outros admins, para controlar quem acessa o dashboard e o que cada um pode fazer.

**Why P1**: Sem gestão de admin, o produto continua operável por 1 pessoa só via API — não atende o caso de equipe real.

**Acceptance Criteria**:

1. WHEN owner convida um novo admin informando email e papel (`owner`/`operator`/`viewer`) THEN o sistema SHALL enviar um token de convite por email, válido por 1 hora, e criar o registro do admin em estado "pending".
2. IF o email já tiver um convite pendente THEN o sistema SHALL invalidar o convite anterior e gerar um novo token, sem criar registro duplicado.
3. WHEN o admin convidado abre o link de convite e define uma senha THEN o sistema SHALL ativar a conta com o papel definido pelo owner.
4. IF o token de convite estiver expirado ou já usado THEN o sistema SHALL rejeitar a definição de senha.
5. WHEN owner altera o papel de um admin existente THEN o sistema SHALL aplicar o novo papel imediatamente e invalidar todas as sessões JWT ativas daquele admin.
6. IF a remoção ou o rebaixamento de um admin resultar em zero admins com papel `owner` ativo (incluindo o próprio owner tentando se rebaixar/remover) THEN o sistema SHALL rejeitar a ação e exibir mensagem de erro específica.
7. WHEN owner remove um admin THEN o sistema SHALL revogar todas as sessões ativas daquele admin imediatamente e impedir login futuro daquela conta.
8. The system SHALL registrar em log de auditoria append-only cada convite, mudança de papel e remoção de admin, contendo admin que executou a ação, admin afetado, tipo de ação e timestamp.
9. The system SHALL restringir a tela e os endpoints de gestão de admins ao papel `owner` — `operator` e `viewer` recebem 403 ao tentar acessar.

**Independent Test**: Como owner, convidar um admin com papel `operator`, ele define senha e loga; owner rebaixa esse admin para `viewer` e confirma que a sessão anterior dele para de funcionar (401) e que uma ação de escrita retorna 403 na sessão nova.

---

### P1: Autorização por papel nos endpoints do mvp-core ⭐ MVP

**User Story**: Como owner, quero que cada papel só execute as ações que lhe cabem nos endpoints já existentes (Datadog, domínios, status pages, incidentes), para impedir que um `viewer` altere dados por acidente ou má intenção.

**Why P1**: Sem essa checagem, criar papéis é decorativo — qualquer admin autenticado continua podendo fazer qualquer coisa.

**Acceptance Criteria**:

1. The system SHALL permitir que admins com papel `owner` ou `operator` executem todas as ações de escrita já especificadas em `mvp-core` (conectar Datadog, gerenciar domínio/status page, gerenciar incidente).
2. IF um admin com papel `viewer` tentar executar uma ação de escrita (POST/PATCH/DELETE) em qualquer endpoint do dashboard THEN o sistema SHALL rejeitar com 403.
3. WHEN um admin autenticado faz uma requisição a um endpoint do dashboard THEN o sistema SHALL verificar o papel do admin, via extensão do middleware de autenticação já existente, antes de processar a ação.

**Independent Test**: Logar como `viewer`, confirmar que `GET /api/domains` funciona e `POST /api/domains` retorna 403; logar como `operator` e confirmar que ambos funcionam, mas `POST /api/admins` (convite) retorna 403.

---

### P1: Ver status do poller no dashboard ⭐ MVP

**User Story**: Como admin de qualquer papel, quero ver quando o poller do Datadog rodou por último e se falhou, para saber se o status exibido publicamente está atualizado.

**Why P1**: Sem essa visão, um admin só descobre falha de polling quando um cliente reclama que o status público está desatualizado.

**Acceptance Criteria**:

1. WHEN um admin autenticado (qualquer papel) acessa a tela de status do poller THEN o sistema SHALL exibir, por integração Datadog conectada, o timestamp da última execução, o resultado (sucesso/falha) e a mensagem de erro quando houver falha.
2. The system SHALL reaproveitar o mesmo dado de defasagem já calculado para o fallback da página pública (mvp-core), sem duplicar a lógica de fetch.

**Independent Test**: Forçar falha de conexão com o Datadog (revogar API key), confirmar que a tela de status do poller no dashboard mostra "falha" com a mensagem de erro, e que o timestamp da última execução bem-sucedida permanece visível.

---

## Edge Cases

- IF admin removido tentar usar um token JWT emitido antes da remoção THEN o sistema SHALL rejeitar com 401.
- IF dois owners alterarem o papel do mesmo admin simultaneamente THEN o sistema SHALL aplicar a última gravação (last-write-wins), consistente com o comportamento já definido no mvp-core para edição concorrente.
- IF owner tentar convidar um email que já é admin ativo (não pending) THEN o sistema SHALL rejeitar o convite com mensagem específica.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| ADM-01 | P1: Gerenciar admins | Design | Pending |
| ADM-02 | P1: Gerenciar admins | Design | Pending |
| ADM-03 | P1: Gerenciar admins | Design | Pending |
| ADM-04 | P1: Gerenciar admins | Design | Pending |
| ADM-05 | P1: Gerenciar admins | Design | Pending |
| ADM-06 | P1: Gerenciar admins | Design | Pending |
| ADM-07 | P1: Gerenciar admins | Design | Pending |
| ADM-08 | P1: Gerenciar admins | Design | Pending |
| ADM-09 | P1: Gerenciar admins | Design | Pending |
| ADM-10 | P1: Autorização por papel | Design | Pending |
| ADM-11 | P1: Autorização por papel | Design | Pending |
| ADM-12 | P1: Autorização por papel | Design | Pending |
| ADM-13 | P1: Status do poller | Design | Pending |
| ADM-14 | P1: Status do poller | Design | Pending |

**ID format:** `ADM-NN` (Admin Dashboard)

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 14 total, 0 mapped to tasks, 14 unmapped ⚠️ (esperado — Design/Tasks ainda não rodaram)

---

## Success Criteria

- [ ] Owner convida `operator` e `viewer`, cada um loga e confirma seu próprio limite de permissão (403 nas ações vetadas)
- [ ] Remover ou rebaixar um admin invalida a sessão dele na próxima requisição, não só no próximo login
- [ ] Sistema nunca fica sem nenhum `owner` ativo, em nenhum caminho de ação (remoção, rebaixamento, self-demotion)
- [ ] Tela de status do poller reflete uma falha real de conexão com Datadog em até 1 ciclo de polling
