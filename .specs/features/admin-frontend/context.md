# Admin Frontend Context

**Gathered:** 2026-08-19
**Spec:** `.specs/features/admin-frontend/spec.md`
**Status:** Ready for design

---

## Feature Boundary

Uma SPA React única, embutida no binário Go via `go:embed` (AD-001), cobrindo toda a superfície administrativa do zeep-vane: as telas do `mvp-core` (conectar Datadog, gerenciar domínios/status pages com TLS, gerenciar incidentes) e as telas do `admin-dashboard` (gestão de admins com convite/papéis, status do poller). Login e sessão são compartilhados por toda a SPA. Não cobre a status page pública (essa já é servida separadamente pelo backend, sem necessidade de SPA React).

---

## Implementation Decisions

### Escopo e stack

- Spec única cobrindo mvp-core + admin-dashboard, não specs separadas — é uma SPA só, com shell/nav/auth compartilhados.
- Stack: Vite + React 18 + TypeScript + Radix UI + Tailwind v4 + TanStack Query + react-router-dom + sonner (toast) + i18next — mesmo padrão já usado em `zeep-orbit/internal/dashboard/ui` (referência confirmada em `mvp-core/design.md`).
- Idioma: português + inglês via i18next desde o MVP (não só português).

### Priorização (P1/P2)

- P1: login/sessão + telas do mvp-core (Datadog, domínios/status pages com TLS, incidentes).
- P2: gestão de admins (convite/papéis) + tela de status do poller.
- Reflete a ordem em que o backend foi construído (mvp-core validado antes de admin-dashboard).

### Navegação e layout

- Sidebar lateral fixa com seções: Domínios, Status Pages, Incidentes, Integrações (Datadog), Admins, Status do Poller.
- Estado vazio (instalação nova, zero domínios/admins): tela com CTA direto para a ação principal, não tabela vazia genérica.

### Feedback de ações e erros

- Toast (sonner) para confirmação/erro de ações (criar, editar, remover, etc).
- Banner inline apenas para erro de validação campo-a-campo em formulário.

### RBAC visual

- `viewer` não vê botões/ações de escrita (esconder, não desabilitar) — UI reflete a permissão real. Backend continua retornando 403 como rede de segurança caso a ação seja disparada por outra via (ex: chamada direta).

### Sessão e 401

- 401 durante uso (sessão expirada, ou admin removido/rebaixado por outro owner em tempo real) abre modal bloqueante "Sessão expirada" sobre a tela atual, com botão "Fazer login novamente" — preserva contexto visual de onde o admin parou.

### Fluxo de convite de admin

- Lista única de admins (não duas seções separadas); convite pendente aparece na mesma tabela com badge "Pendente" e ações de reenviar/cancelar.

### Status do poller e falha de conexão Datadog

- Banner global de alerta persiste no topo de toda a SPA quando há falha ativa de polling, além de página dedicada "Status do Poller" com detalhe por integração conectada.

### Fluxo de domínio + emissão de TLS

- Após cadastrar domínio, a tela de detalhe faz polling automático de status (a cada 10s) enquanto o certificado está "emitindo", até chegar a publicado ou falho — sem precisar recarregar a página manualmente. Mesmo princípio de polling se aplica a qualquer estado assíncrono de longa duração no dashboard.

### Agent's Discretion

- Detalhes visuais finos (espaçamento, paleta exata, ícones específicos) ficam a critério do Design/implementação, desde que sigam os componentes Radix/Tailwind já usados em `zeep-orbit`.
- Estrutura exata de rotas (`react-router-dom`) e organização de pastas dentro de `web/` ficam a critério do Design.

### Declined / Undiscussed Gray Areas → Assumptions

- **Observabilidade de erro no frontend**: nenhum serviço de error tracking (Sentry etc) foi discutido. Assumido: log em console apenas no MVP; erros de rede/backend seguem tratados via toast/modal já definidos. Revisitar se o time quiser telemetria de erro de cliente.
- **Concorrência/edição simultânea na UI**: backend já resolve com last-write-wins (mvp-core e admin-dashboard). Assumido: a UI não implementa lock otimista nem aviso de "outro admin editou" — apenas revalida dado via TanStack Query após cada mutação bem-sucedida.
- **Validação client-side de formulário**: assumido que os formulários replicam client-side as mesmas regras óbvias já impostas pelo backend (formato de email, domínio, senha não vazia) para feedback imediato, mas a fonte de verdade continua sendo a resposta da API — nenhuma regra nova é inventada no frontend.
- **Limpeza de denylist / expiração de convite/token na UI**: a UI apenas exibe o estado retornado pela API (expirado, pendente, etc); não há lógica de expiração calculada no cliente.

---

## Specific References

Nenhuma referência visual externa citada. Referência de stack e padrão de embed: `zeep-orbit/internal/dashboard/ui` (Vite + React + Radix + Tailwind + TanStack Query + react-router-dom + sonner + i18next) e `zeep-orbit/internal/dashboard/embed.go` (padrão `go:embed` + fallback SPA).

---

## Deferred Ideas

- Tela de consulta/filtro sobre o audit log (já fora de escopo do `admin-dashboard` backend) — segue fora de escopo aqui também.
- Telemetria/error tracking de frontend (Sentry ou equivalente) — não decidido, ver Assumptions.
- Notificação de mudança de status por email/webhook (subscription) — já fora de escopo do `mvp-core`, segue fora aqui.
