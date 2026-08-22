# STATE

## Decisions

### AD-001
- **Decision**: Backend em Go (chi + pgx/v5 + jwt/v5 + zap + cobra), TLS automático via CertMagic embutido (sem proxy externo), frontend React embutido no binário via `go:embed`.
- **Reason**: Resolve o risco técnico mais crítico do produto (TLS automático por domínio dinâmico cadastrado em runtime) com biblioteca madura da própria equipe do Caddy, entregando binário único de baixo footprint — essencial pra adoção de uma ferramenta self-hosted open source. Stack alinhada à já validada em produção em `baas/zeep-orbit`.
- **Trade-off**: Time trabalha mais com Node/React no dia a dia; Go nesse serviço específico tem menos familiaridade acumulada que Node.
- **Scope**: Todo o backend do projeto (zeep-vane) — API, poller, roteamento por domínio, TLS.
- **Date**: 2026-08-06
- **Status**: active

### AD-002
- **Decision**: Cada instalação self-hosted atende exatamente 1 empresa. Não existe tabela/coluna de tenant (`company_id`) em nenhum schema.
- **Reason**: Isolamento total de dado por deploy é mais simples e mais seguro que multi-tenancy real; bate com o modelo de distribuição (empresa roda a própria infra).
- **Trade-off**: Se o modelo de negócio mudar pra SaaS hospedado multi-cliente no futuro, exige retrofit de tenant em todas as tabelas.
- **Scope**: Todo o schema de dados do projeto.
- **Date**: 2026-08-06
- **Status**: active

### AD-003
- **Decision**: Múltiplos admins com papéis fixos (`owner`, `operator`, `viewer` — sem matriz de permissão configurável) reentram no MVP, via feature própria `admin-dashboard`. Remove RBAC do Out of Scope do `mvp-core` (mantém SSO/SAML fora).
- **Reason**: Sem UI/dashboard, os 25 requisitos admin do `mvp-core` (SP-01 a SP-25) só são operáveis via chamada direta de API. Usuário decidiu que múltiplos admins com papéis diferentes é necessário já na v1, não pós-MVP.
- **Trade-off**: Aumenta escopo do MVP (nova tabela de roles/permissions, checagem de autorização por endpoint, UI de gestão de admins) além do que AD-002 (single-tenant, sem multi-tenant) previa como simplificação.
- **Scope**: Autenticação/autorização do dashboard administrativo e endpoints do `mvp-core` que passam a exigir checagem de papel.
- **Date**: 2026-08-06
- **Status**: active

### AD-004
- **Decision**: Token de sessão do admin no frontend vive em cookie `httpOnly`/`Secure`/`SameSite=Strict` setado pelo backend no login — nunca em `localStorage`/`sessionStorage`, nunca lido ou decodificado por JavaScript no client.
- **Reason**: Decisão explícita do usuário durante o Design de `admin-frontend`. Cookie `httpOnly` elimina por completo a superfície de XSS-rouba-token (JS não tem acesso de leitura), diferente de qualquer storage acessível via script. `SameSite=Strict` mitiga CSRF numa API JSON same-origin. O papel do admin continua vindo sempre de `GET /api/auth/me` (AF-34) — o JWT não carrega claim de role (só `sub`/`iat`).
- **Trade-off**: Exige mudança aditiva no backend (login passa a também setar cookie além do corpo `{token}` já existente; `RequireAuth` passa a aceitar cookie além do header `Authorization`; novo endpoint `POST /api/auth/logout` para limpar o cookie). Mudança é 100% aditiva — nenhum teste/consumidor existente do login ou do middleware quebra. `Secure` exige HTTPS mesmo em desenvolvimento local.
- **Scope**: Qualquer feature de frontend deste projeto que lide com o token de sessão do admin.
- **Date**: 2026-08-19
- **Status**: active (revisada no mesmo dia, antes de Tasks/Execute — versão anterior desta entrada, que propunha `sessionStorage`, nunca chegou a ser implementada)

### AD-005
- **Decision**: O produto se chama **Vane** — nome de marca oficial exibido em toda copy de frontend (sidebar, login, título de página), não só o "working name" do handoff de design recebido em `dashboard-handoff/`.
- **Reason**: Nenhuma feature anterior (`mvp-core`, `admin-dashboard`) travou nome de produto — só o handoff de design (`dashboard-handoff/README.md`) o usa, com a ressalva explícita "rename if the product has an official one". Usuário confirmou que deve ser adotado como definitivo.
- **Trade-off**: Trocar depois exigiria revisitar toda copy de marca já implementada (sidebar, login, possivelmente nome do binário/repo) — decisão feita cedo, antes de qualquer tela existir, pra evitar esse retrabalho.
- **Scope**: Qualquer copy de marca em qualquer frontend deste projeto (admin-frontend e futuras features de frontend, incluindo a status page pública).
- **Date**: 2026-08-20
- **Status**: active

### AD-006
- **Decision**: Frontend `admin-frontend` implementado primeiro contra mock layer (`web/src/lib/mockData.ts` + `apiClient.ts` simulando fetch em memória, sem rede real). Integração real com backend Go (T1-T8 backend + wiring real de T13/T14/T18 + demais hooks) fica para fase posterior, fora do escopo desta rodada de execução.
- **Reason**: Usuário pediu explicitamente "faça só interface navegável com dados fake, depois faremos a fase de integração com o backend" — decisão deliberada de escopo, não esquecimento do plano em `tasks.md` (que descreve ordem backend-primeiro nas Fases 1-2).
- **Trade-off**: Nenhum dos 25 requisitos admin (SP-01 a SP-25) fica operável contra dado real nesta rodada; endpoints T1-T8 (auth/me, list de domains/status-pages/admins com convites, busca de SLO), cookie de sessão (T6-T8) e o wiring de rede real em T13 (`apiClient`)/T14 (`AuthProvider`)/T18 (embed da SPA) continuam pendentes. Risco de retrabalho na integração se o contrato mockado divergir do contrato real do backend — validar payloads exatos com T1-T8 antes de trocar a camada mock por fetch real.
- **Scope**: Frontend `admin-frontend` (`web/`) desta rodada de execução; próxima rodada deve tratar a integração como fase própria.
- **Date**: 2026-08-20
- **Status**: active

### AD-007
- **Decision**: Integração backend real do `admin-frontend` (pós-AD-006) dividida em 6 etapas sequenciais e testáveis isoladamente (Etapa 0 Fundação → 1 Domínios/Status Pages/pública → 2 Integrações/Serviços → 3 Incidentes → 4 Admins → 5 Poller), substituindo T1-T8 e o wiring pendente de T13/T14/T18/T21/T24/T28/T31/T33 em `tasks.md` por tasks novas I1-I20. 3 sub-decisões tomadas com o usuário durante o planejamento:
  1. **Gaps fora de `tasks.md`** descobertos na investigação (admin invite resend/cancel — hooks já existem no frontend sem endpoint; company settings GET/PATCH — feature inteira sem endpoint) ficam como **backlog separado**, fora do escopo desta integração. `AdminsPage` desabilita as ações de reenviar/cancelar convite com aviso explícito em vez de deixá-las quebrar; `SettingsPage` continua 100% mockada.
  2. **Status page pública**: mantém rota por ID no frontend (`/status/:id`) via endpoint novo autenticado-lite de dev/preview (`internal/api/public_status_preview_handler.go`), coexistindo deliberadamente com o endpoint real de produção resolvido por Host header (`public_status_handler.go`, já existente) — SPEC_DEVIATION documentado no código, motivo é a SPA não ter infraestrutura de host-routing em dev.
  3. **Estratégia de teste**: introduzir **MSW** para interceptar `fetch` real nos testes de hook (10 arquivos + `AuthProvider.test.tsx`), substituindo a dependência direta no `handleRoute` mock — primeiro uso de MSW no projeto.
- **Reason**: Investigação (Explore) confirmou que nenhum de T1-T8 existe hoje no backend, CORS não existe em lugar nenhum do código (bloqueante pra qualquer fetch real do Vite dev contra a API), e os testes de hook dependem 100% do roteador mock em memória — trocar `apiClient` pra fetch real sem uma estratégia de teste nova quebraria a suíte inteira. A investigação também achou 2 gaps que o `tasks.md` original nunca cobriu (resend/cancel de convite, company settings); usuário decidiu não expandir escopo agora.
- **Trade-off**: Etapa 0 é bloqueante e relativamente pesada (9 tasks: 5 backend + 4 frontend) antes de qualquer etapa de feature poder começar. Endpoint dev/preview de status page pública (item 2) é superfície extra a manter que não existe em produção — decisão consciente de simplicidade de dev sobre fidelidade total à arquitetura de produção. `AdminsPage`/`SettingsPage` ficam com funcionalidade parcialmente indisponível até rodada futura.
- **Scope**: Integração backend real de `admin-frontend` (`web/` + endpoints Go correspondentes) desta rodada. Backlog (resend/cancel invite, company settings) fica para rodada futura.
- **Date**: 2026-08-21
- **Status**: active

## Handoff

**Feature**: `admin-dashboard` — **status: PASS ✅** (Verifier completou em 1 rodada de fix→re-verify, dentro do limite de 3). Relatório: `.specs/features/admin-dashboard/validation.md`.

**Completo**: T1-T13 (todas as 4 fases) implementadas via 2 batches de sub-agentes (Batch1 T1-T6, Batch2 T7-T13) + 1 rodada de fix (5 de 6 gaps do Verifier — testes de wiring real do router de produção que não cobriam todas as rotas de escrita/admin, TTL de convite não verificado contra valor persistido, revogação de sessão em delete sem teste, poller status sem teste de acesso viewer no router real). Branch `main`, working tree limpa (exceto `.specs/features/admin-dashboard/validation.md`, `.specs/LESSONS.md`, `.specs/lessons.json` ainda não commitados). Último commit: `d719366` (test: assert invite TTL matches persisted expires_at).

**Achado real relevante**: o batch de execução descobriu que o roteamento de produção do `mvp-core` inteiro (domains/services/integrations/incidents/status-pages) nunca tinha sido montado no binário real (`serve.go` só servia `/healthz`) — `internal/cli/routes.go` (`buildAdminRouter`) foi criado nesta feature pra resolver isso e aplicar `RequireRole` a cada rota. Sem essa correção, todo o RBAC desta feature seria código morto, igual o gap achado no `mvp-core`.

**Desvios técnicos documentados como SPEC_DEVIATION no código** (não bloqueantes, backlog):
- Não existe linha de `Admin` "pending" no convite — estado vive só em `admin_invites` até o accept.
- `target_id` do audit log `invited` aponta pro invite, não pro admin (que ainda não existe nesse momento).
- Envio de email loga o token ao invés de enviar de verdade — convenção herdada do mecanismo de reset de senha do mvp-core (`password_reset_handler.go`), não uma decisão nova desta feature.

**Deferred/backlog (não bloqueante)**:
- Flake intermitente `dbtest: pg_advisory_lock failed: timeout` em `integrations_handler_test.go` sob `-p` default (não reproduz com `-p 1`) — infra de teste, não da feature. Recomendação do Verifier: revisar `internal/dbtest` ou fixar `-p 1` no Makefile/CI.
- 4 deferred ideas do `mvp-core` (ver histórico) seguem não resolvidas.

**Next steps**: backend do `zeep-vane` completo (mvp-core + admin-dashboard, ambos PASS). Próximo: specs de frontend (admin React UI) — combinado explicitamente pra ficar pra depois do backend, ainda não começado.

---

**Feature**: `admin-frontend` (integração real com backend, ver AD-007) — **status: 6/6 etapas PASS ✅**. Etapa 0 (Fundação, I1-I9), Etapa 1 (Domínios/Status Pages/pública, I10-I13), Etapa 2 (Integrações/Serviços, I14-I15), Etapa 3 (Incidentes, I16), Etapa 4 (Admins, I17-I19), Etapa 5 (Poller, I20) — todas com Verifier independente PASS. Relatórios em `.specs/features/admin-frontend/validation.md`.

**Completo**: 20 tasks I1-I20 via commits atômicos (branch `main`, working tree limpa). Backend Go: `AuthHandler.Me`, cookie de sessão httpOnly, `DomainRepository.List`/`StatusPageRepository.List`/endpoint dev-preview de status page pública, Datadog `SearchSLOs`+`GET /api/integrations/datadog/slos`, incidentes com `ServiceIDs` e endpoints de lista/timeline, `AdminInviteRepository.List` + `AdminsHandler.List` mesclando admin ativo + convite pendente. Frontend: MSW introduzido (primeiro uso no projeto) e migração completa de todos os hooks (`domains`, `status-pages`, `public-status`, `integrations`, `services`, `incidents`, `admins`, `poller`) do roteador mock em memória para `apiFetch` real interceptado por MSW nos testes. `npm run test` 100% verde (37 arquivos, 129 testes), `tsc --noEmit` limpo.

**Correção pós-Verifier na Etapa 4**: 3 gaps reais (toast de confirmação faltando em AF-28, sem cobertura de teste em happy-paths de troca de papel/remoção/convite) corrigidos em 1 rodada de fix→re-verify.

**Backlog não-bloqueante herdado de AD-007** (fora de escopo desta integração): resend/cancel de convite de admin (hooks existem, endpoint não), company settings GET/PATCH (feature inteira sem endpoint, `SettingsPage` mockada), teste faltante de 404 em update de incidente inexistente (Etapa 3).

**Next steps**: integração real backend↔frontend do `admin-frontend` está completa. Próximo trabalho pendente: os 2 itens de backlog acima (resend/cancel invite; company settings), se decidido priorizá-los — nenhum foi solicitado ainda nesta sessão.
