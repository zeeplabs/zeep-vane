# Admin Dashboard Context

**Gathered:** 2026-08-06
**Spec:** `.specs/features/admin-dashboard/spec.md`
**Status:** Ready for design

---

## Feature Boundary

Dashboard administrativo que dá interface humana e controle de acesso por papel aos 25 requisitos já especificados em `mvp-core` (conectar Datadog, gerenciar domínio/status page, gerenciar incidente, login). Adiciona: gestão de múltiplos admins com papéis fixos, revogação de sessão, audit log de ações sensíveis, e visão de status do poller.

---

## Implementation Decisions

### Papéis

- 3 papéis fixos: `owner`, `operator`, `viewer` — sem matriz de permissão configurável.
- `owner`: tudo que `operator` faz + gerenciar admins (convidar, mudar papel, remover).
- `operator`: todas as ações de escrita já especificadas no mvp-core (Datadog, domínios, status pages, incidentes) — mas não gerencia outros admins.
- `viewer`: somente leitura em todo o dashboard, incluindo status do poller. Nenhuma ação de escrita.

### Convite de admin

- Owner cadastra email + papel. Sistema envia link de convite com token, mesmo padrão do fluxo de reset de senha do mvp-core (expira em 1 hora).
- Novo admin define a própria senha ao abrir o link — conta ativa só depois disso.
- Convite duplicado pro mesmo email invalida o anterior e gera novo token (sem registro duplicado).

### Proteção contra lockout

- Sistema nunca permite que a última conta com papel `owner` seja removida ou rebaixada — bloqueia a ação com erro específico, mesmo se for o próprio owner tentando se rebaixar/remover.

### Revogação de sessão

- Remover ou rebaixar um admin invalida imediatamente todas as sessões JWT ativas dele (denylist mínima por admin_id), não espera expiração natural do token.
- Entradas da denylist são limpas quando o JWT original já teria expirado naturalmente (evita crescimento infinito).

### Audit log

- Toda ação de convite, mudança de papel e remoção de admin é registrada (quem fez, quem foi afetado, ação, timestamp), append-only.
- Sem UI de auditoria detalhada no MVP — só o registro existe, é dado pra fase seguinte expor.

### Status do poller no dashboard

- Qualquer papel autenticado vê, por integração Datadog conectada: timestamp da última execução, resultado (sucesso/falha), mensagem de erro se houver.
- Reaproveita o mesmo dado de defasagem já exposto na página pública (mvp-core) — sem duplicar lógica de fetch.

### Agent's Discretion

- Layout visual e fluxo de tela (onde cada ação fica na UI) — fica pro design/UI, esta spec cobre só o comportamento funcional.

### Declined / Undiscussed Gray Areas → Assumptions

- Retenção do audit log: sem prazo definido explicitamente pelo usuário → assumido indefinida (ver spec.md).
- Self-demotion fora do caso de lockout (owner mudando o próprio papel quando existe outro owner) → assumido permitido, sujeito à mesma regra de proteção.

---

## Specific References

Nenhuma referência visual/produto específica trazida pelo usuário — comportamento segue os padrões já usados no mvp-core (fluxo de token por email, last-write-wins em edição concorrente, log estruturado).

---

## Deferred Ideas

- Matriz de permissão configurável por recurso/ação (RBAC granular de fato) — ficou fora, só 3 papéis fixos no MVP.
- UI de auditoria com filtro/busca sobre o audit log — log existe, tela de consulta fica pra fase seguinte.
