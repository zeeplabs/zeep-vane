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

### AD-008
- **Decision**: O endpoint de preview autenticado (`GET /api/status-pages/{id}/public-preview`) deixa de exigir `state == "published"` — passa a compor a resposta pra uma status page em qualquer estado, incluindo sem domínio nenhum anexado (`domain_id: null`).
- **Reason**: Feature `status-page-domain-attach` (2026-08-23) — teste manual do usuário revelou que hoje é impossível visualizar uma status page recém-criada antes do domínio ter DNS resolvido e certificado TLS emitido, porque o preview foi desenhado deliberadamente em `AD-007`/I12 pra espelhar o gate de produção 1:1 ("pra o preview e a página de produção nunca discordarem do que conta como visível"). Esse objetivo original é exatamente a causa do bug: um admin que quer pré-visualizar antes de ir ao ar não tem caminho nenhum se o preview insiste em bater um estado de produção que ainda não existe.
- **Trade-off**: Preview e produção agora podem "discordar" (preview mostra conteúdo de página `draft`/sem domínio que a produção real nunca serviria) — aceito porque o preview já é autenticado/admin-only e nunca foi o caminho público real; fidelidade total a produção não tem valor pra esse caso de uso.
- **Scope**: `internal/api/public_status_preview_handler.go` — não afeta `PublicStatusHandler.Get`/`router.HostRouter` (caminho público real de produção, que continua exigindo `published`).
- **Date**: 2026-08-23
- **Status**: active (supersede parcial do item 2 de `AD-007` — a decisão de ter o endpoint dev/preview continua válida, só a regra de "espelhar produção 1:1" foi revertida)

### AD-009
- **Decision**: Features que tocam deployment (Dockerfile, docker-compose, Makefile build targets, convenções de bootstrap/embed) devem por padrão espelhar as convenções já provadas em produção de `baas/zeep-orbit`, a menos que haja uma razão específica do Vane pra divergir.
- **Reason**: `self-hosted-docker-bootstrap` (2026-08-24) fechou a promessa de `AD-001` ("frontend embedded via `go:embed`") que nunca tinha sido implementada de fato (confirmado por `grep -r "go:embed"` retornando vazio antes desta feature), e o fez espelhando deliberadamente o Dockerfile/docker-compose/Makefile/bootstrap já funcionando de `zeep-orbit` em vez de desenhar um padrão novo — decisão explícita do usuário durante o Design ("preciso que o comando de subida seja igual o que existe hoje no orbit que ja funciona"). O resultado (single-stage `FROM scratch`, `LOCK TABLE ... IN EXCLUSIVE MODE` pro bootstrap race, `go:embed` no mesmo diretório do asset por causa da restrição de `..`) validou que reusar um padrão já testado em produção evita reinventar decisões que `zeep-orbit` já errou e corrigiu uma vez.
- **Trade-off**: Onde Vane tem restrição que Orbit não tem (migrations reais via `golang-migrate`, listener TLS público de status page), o espelhamento não é 1:1 — exige julgamento caso a caso sobre o que é "a mesma convenção adaptada" vs. "um padrão realmente novo". Seguir esse default cegamente sem checar se a diferença é real pode importar uma decisão errada do Orbit pro Vane.
- **Scope**: Qualquer feature futura deste projeto que toque Dockerfile, docker-compose.yml, Makefile de build, ou fluxo de bootstrap/deploy inicial.
- **Date**: 2026-08-24
- **Status**: active

## Handoff

**Feature**: `self-hosted-docker-bootstrap` — **status: PASS ✅** (Verifier iteração 3/3, limite do skill, 2 rodadas de fix→re-verify). Relatório: `.specs/features/self-hosted-docker-bootstrap/validation.md`. 22/22 ACs (SHD-01 a SHD-22), sensor de discriminação 3/3 mortos.

**Completo**: nasceu de análise comparativa contra `zeep-orbit` (pedida pelo usuário) que apontou 2 lacunas reais: `AD-001` prometia frontend embutido via `go:embed` mas isso nunca tinha sido implementado (API e SPA só funcionavam como 2 processos separados, sem Dockerfile/docker-compose nenhum); e criar o primeiro admin exigia um script Go descartável pra gerar hash bcrypt + `INSERT` manual no Postgres. Resolvido com: `web/embed.go` (SPA embutida + fallback de rota client-side + 404 JSON real pra `/api/*` desconhecido), `internal/db/migrations_embed.go` (migrations embutidas, aplicadas automaticamente no boot do `vane serve`), `AdminRepository.BootstrapFirst` (mesma técnica de `zeep-orbit`: `LOCK TABLE admins IN EXCLUSIVE MODE`, não `SELECT ... FOR UPDATE`, que não trava nada numa tabela vazia), `BootstrapHandler` (`GET /api/bootstrap/status` + `POST /api/bootstrap` públicos, mesmo cookie de sessão do login), tela `/bootstrap` no frontend com guarda de redirecionamento nos dois sentidos, e `Dockerfile`/`docker-compose.yml`/`Makefile` mirando a estrutura já funcionando de `zeep-orbit` (multi-stage, `FROM scratch`, `depends_on: service_healthy`). 13 tasks (T1-T13) + 4 commits de fix, todos atômicos, branch `main`.

**Gap real achado pelo Verifier e histórico da correção** (3 iterações, limite do skill): rodada 1 — o gate mandatório (`TEST_DATABASE_URL=<dsn> go test -tags=integration ./...`) era flaky por causa de 3 pacotes (`internal/db`, `internal/api`, `internal/cli`) cada um limpando/restaurando a tabela `admins` compartilhada sem serialização entre processos concorrentes de `go test ./...`. Fix 1 (`df99d5d`) criou `dbtest.LockAdminsTable`, mas só cobriu os pontos que faziam bulk-clear/contagem exata. Rodada 2 — usando `-count=1` real (não `(cached)`), achou 5 pontos que criavam admin `owner` como setup comum sem tomar o lock (`issueRoutesTestToken` em `internal/cli/routes_test.go`, `issueTestSessionTokenWithRole` usado por `internal/api/poller_status_test.go`), quebrando a suposição "sou o único/último owner" de outro teste que tinha o lock. Fix 3 (`526139e`) centralizou `LockAdminsTable` em todo helper compartilhado que cria admin, e ao investigar mais a fundo achou uma race SEPARADA e estruturalmente idêntica na tabela singleton `company_settings` (mesmo problema, 3 pacotes diferentes resetando/lendo a mesma linha `id=1`), corrigida com `dbtest.LockCompanySettings` (`7e401dc`, chave de advisory lock `727100003`, distinta das outras). Rodada 3 (final) — 16 execuções reais e independentes do gate mandatório (`-count=1`, nunca `(cached)`), 16/16 verdes, 0 falhas, com contenção de lock real confirmada ao vivo via `pg_stat_activity` durante a execução (prova de que o fix está de fato serializando, não é um no-op). Lições L-015/L-016/L-017 gravadas.

**Risco conhecido, não bloqueante, não corrigido nesta feature**: `TestPublicStatusPreview_PublishedPage_200Unaffected` (`internal/api/public_status_preview_handler_test.go`, arquivo não tocado por esta feature) foi relatado uma vez como falha isolada (500) sob paralelismo pesado de suíte completa, mas não reproduziu em nenhuma das 16 execuções desta rodada nem em 5 execuções isoladas anteriores — tratado como candidato a flake pré-existente do `internal/api`, registrado como `L-017`, recomendado como item de backlog separado se recorrer.

**AD-009 registrado**: features de deployment devem espelhar as convenções já provadas de `zeep-orbit` por padrão, salvo razão específica do Vane pra divergir.

**Next steps**: nenhum solicitado ainda nesta sessão. Backlog não-bloqueante herdado de rodadas anteriores continua em aberto (resend/cancel de convite de admin, teste 404 em update de incidente, gap residual de concorrência do `PollerManager`) — ver handoffs anteriores.

---

**Feature**: `service-status-intervals` — **status: PASS ✅** (Verifier PASS na primeira passada, sem rodada de fix). Relatório: `.specs/features/service-status-intervals/validation.md`. 20/20 ACs (SHU-01 a SHU-20), sensor de discriminação 3/3 mortos.

**Completo**: nasceu de análise comparativa contra o projeto open source OneUptime (pedida pelo usuário), que apontou 2 problemas reais no Vane: `status_snapshots` cresce 1 linha/poll/serviço sem nenhuma rotina de pruning (vazamento de disco ilimitado), e a barra horária resolvia cada hora pelo último status visto, o que mascara incidente curto dentro da hora. Usuário decidiu fundir os dois numa feature só em vez de corrigir reten­ção isolada e migrar o schema de novo depois. Resolvido com modelo de intervalo: tabela `status_intervals` (migration 0014, substitui `status_snapshots` por completo, sem dual-write, sem backfill — aceito perder 24h de histórico no dia do deploy) com índice único parcial `(service_id) WHERE ends_at IS NULL` garantindo no máximo 1 intervalo aberto por serviço a nível de banco. `StatusIntervalRepository.OpenOrExtend` (transação `SELECT ... FOR UPDATE` + branch, mesmo padrão do `AttachDomain` de `status-page-domain-attach`) substitui o `Create` cego do poller. `internal/history.BuildHourly` mudou de "último vence" pra "pior status vence" (outage > degraded > operational). `internal/history.UptimePercent` novo — só `outage` conta como downtime, denominador clipa pro serviço mais novo que a janela, clamp `[0,100]` antes de floor numa casa decimal. `internal/retention.Pruner` novo — ticker próprio de 1h, deleta intervalos fechados com `ends_at` > 35 dias, nunca toca intervalo aberto. 10 tasks (T1-T10) em 2 batches de sub-agente + Verifier, todos commits atômicos.

**Achado do Verifier** (não bloqueante, não corrigido nesta rodada): falta teste explícito de "primeiro poll de um serviço novo falha → zero intervalos criados" partindo de zero prévio (o código já está correto, só falta o teste começar de zero em vez de já ter um sucesso semeado antes). Registrado como lição `L-014` e como Fix opcional no relatório.

**Decisão de arquitetura tomada durante a análise do OneUptime**: multi-provider (Grafana, New Relic, etc.) fica fora do MVP — Vane continua Datadog-only por decisão explícita do usuário, mas o desenho do `service-status-intervals` já é agnóstico de provedor na camada de armazenamento, então não bloqueia essa evolução futura quando ela vier.

**Next steps**: próximos itens do top-5 (levantados na mesma análise OneUptime, ainda não especificados): Dockerfile+docker-compose+bootstrap de admin sem SQL manual; monitor tipo Manual+heartbeat (desacopla do Datadog); notificações (email/webhook/Slack) com subscribers. Nenhum solicitado ainda para Specify.

---

**Feature**: `public-status-hourly-history` — **status: PASS ✅** (Verifier PASS na primeira passada, 1 rodada de fix→re-verify para 2 gaps de cobertura auto-achados). Relatório: `.specs/features/public-status-hourly-history/validation.md`. 8/8 ACs (UPT-01 a UPT-08), sensor de discriminação 3/3 mortos após a rodada de fix.

**Completo**: achado em teste manual do usuário (2026-08-24) logo após o fix do poller — a barra de uptime da status page pública era decorativa (`history.ts` seedava 45 dias fake por nome hardcoded de serviço). Usuário pivotou o design original do handoff (barras diárias) para: 1 barra por hora, janela de 24h, fuso `America/Sao_Paulo`, último status da hora vence (não o pior — decisão explícita contra minha recomendação padrão), tooltip obrigatório no hover/foco. Implementado via `tlc-spec-driven` completo (Specify→Design→Tasks→Execute, 6 tasks T1-T6): `internal/history` (pacote novo, puro, sem I/O) com `BuildHourly` fazendo bucketing por última-observação-vence; `StatusSnapshotRepository.ListRecentByServices` (nova, reusa índice existente `(service_id, fetched_at)`); `cmd/vane/main.go` ganhou blank import `_ "time/tzdata"` (embute IANA tzdata no binário — `LoadLocation("America/Sao_Paulo")` não pode depender do SO host em containers mínimos, AD-001); `PublicStatusHandler.composeResponse` estendido com `hourly_history` por serviço, compartilhado entre produção e o preview autenticado sem wiring separado (UPT-08 de graça); frontend: `history.ts` (seed fake) deletado, `PublicStatusPage.tsx` renderiza 24 barras reais com tooltip nativo (`title` + `tabIndex`) formatado via `Intl.DateTimeFormat` com timezone explícito. 7 commits atômicos (`a1e22cd` a `08dd7e0`).

**Gaps reais achados pelo Verifier** (ambos corrigidos na rodada de fix, commit `08dd7e0`): (1) todas as asserções de "24 buckets" no lado Go comparavam contra a própria constante `historyWindowHours` em vez de um literal hardcoded — uma regressão futura na constante (`24`→`12`) passaria por todo o build/vet/testes sem ser pega; corrigido trocando as 6 comparações por literal `24`. (2) `ListRecentByServices` não tinha teste dedicado em `internal/db` apesar do T1 exigir explicitamente ≥3 subtestes — a task-3 tinha coberto o comportamento indiretamente via `internal/api`, mas não no nível do repositório; corrigido com `internal/db/status_snapshot_repository_test.go` (5 subtestes) — **nota (L21, 2026-08-25)**: esse arquivo e `StatusSnapshotRepository` existiam quando esta feature foi implementada; a migração posterior `service-status-intervals` (ver acima) substituiu ambos por `internal/db/status_interval_repository_test.go`/`StatusIntervalRepository`. Nome mantido aqui como registro histórico do estado no momento desta feature, não reflete o arquivo atual no repo.

**Next steps**: nenhum solicitado ainda. Backlog aberto não relacionado a esta feature (ver entrada `poller-live-integration-detect` abaixo e handoff anterior): resend/cancel de convite de admin, teste 404 em update de incidente, endpoint de editar serviços de uma status page já criada (gap descoberto em 2026-08-24 durante teste manual, corrigido no banco de dev sem endpoint ainda), gap residual de concorrência do `PollerManager`.

---

**Feature**: `poller-live-integration-detect` — **status: PASS ✅** (Verifier PASS on first pass, 1 fix→re-verify round for 2 self-found coverage gaps, author-applied and self-verified against the same mutants). Relatório: `.specs/features/poller-live-integration-detect/validation.md`. 6/6 ACs (PLD-01 a PLD-06).

**Completo**: bug achado em teste manual do usuário (2026-08-24) — 2 serviços com SLO vinculado e Datadog "Conectado" ficaram permanentemente "Não configurado" porque o poller só lê a integração Datadog uma vez, no boot do `serve` (`newPollerFromStoredIntegration`). Conectar Datadog pela UI depois do processo já de pé persiste a linha, mas o poller em execução nunca via isso — exigia restart manual. Rotação de chave tinha a mesma causa raiz (client do Datadog fixado na construção). Resolvido com `PollerManager` novo (`internal/cli/poller_manager.go`) — mutex-guarded, `Restart(ctx) (started bool, err error)`/`Stop()` — chamado por `IntegrationsHandler.ConnectDatadog` após todo `UpsertDatadog` bem-sucedido (mesmo endpoint cobre connect e rotate) além do boot. 2 commits atômicos (`7f4993c` fix, `c95ca7d` cobertura de gaps).

**Gap real aceito e documentado** (não bloqueante): teste de "duas chamadas concorrentes de `Restart`/`Stop` são serializadas com espera real por `<-m.done`" não é determinístico de provar sem acesso a rede real do Datadog ou refatorar `Poller` para ser injetável (fora do escopo aprovado desta correção) — cancelamento de contexto faz `Run` retornar quase instantaneamente de qualquer forma, mascarando a diferença. Documentado em `validation.md` como risco residual explícito, não reclamado como corrigido.

**Next steps**: nenhum solicitado ainda. Se o gap residual acima for priorizado no futuro, a via mais direta é tornar `newPollerFromStoredIntegration`/`Poller` injetável para permitir um "poller lento" controlável em teste.

---

**Feature**: `status-page-domain-attach` — **status: PASS ✅** (Verifier iteração 3/3, limite do skill, 2 rodadas de fix→re-verify). Relatório: `.specs/features/status-page-domain-attach/validation.md`. 14/14 ACs (SPD-01 a SPD-14), sensor 5/5 mortos.

**Completo**: nasceu de teste manual do usuário (2026-08-23) que descobriu 2 bugs reais em cascata: (1) `POST /api/status-pages` exigia domínio na criação, então uma página nova só era visualizável depois de DNS real apontado + certificado TLS emitido — impossível revisar conteúdo/layout antes disso; (2) o preview autenticado (`public-preview`, I12) espelhava produção 1:1 de propósito (`state=="published"` obrigatório), causa raiz do mesmo bug. Resolvido com fluxo novo: criar página sem domínio (preview libera na hora, ver `AD-008`) → anexar domínio depois via endpoint novo (`PATCH /api/status-pages/{id}/domain`, transação com `SELECT ... FOR UPDATE` — não `UPDATE ... WHERE domain_id IS NULL`, que não distinguiria "não existe" de "já anexado") → índice único parcial (`WHERE domain_id IS NOT NULL`) evita colisão de `(domain_id, subdomain)` sem race. Endpoint novo `GET /api/instance/dns-target` expõe valor configurado pelo operador (`PUBLIC_DNS_TARGET`, sem auto-detecção de IP). 15 tasks (T1-T15) + 3 commits de fix, todos atômicos.

**Gaps reais achados pelo Verifier**: rodada 1 — lista de status pages (`StatusPagesSection.tsx`) nunca recebeu o fix de label distinguível que o detail já tinha, e um teste tinha sido editado durante a implementação original pra FIXAR o label ambíguo proibido pela spec (pin de comportamento errado); mutante sobrevivente em guard de URL nula, código morto sem teste. Rodada 2 — achado mais sério: o teste de concorrência de `AttachDomain` (2 goroutines soltas) passava 20/20 mesmo removendo o `FOR UPDATE` de produção — nunca provava a trava real, só parecia provar. Corrigido com transação "holder" explícita que segura o lock enquanto uma chamada real roda concorrente, forçando contenção de verdade. Lições L-010 a L-013 gravadas.

**AD-008 registrado**: preview autenticado deixa de exigir `published` — supersede parcial do item 2 de `AD-007` (motivo: a decisão original de "espelhar produção" era exatamente a causa do bug).

**Next steps**: restam 3 itens do backlog original (admin invite resend/cancel; teste 404 incident update; validação manual Datadog real — em andamento, poller só lê integração no boot do processo). Backlog novo (não solicitado ainda): auto-discovery de services/SLOs/monitores do Datadog via API (opcional).

---

**Feature**: `company-settings` — **status: PASS ✅** (Verifier iteration 2/3, após 1 rodada de fix→re-verify). Relatório: `.specs/features/company-settings/validation.md`. 16/16 ACs (SET-01 a SET-16) verificados, sensor de discriminação 5/5 mortos, 0 gaps.

**Completo**: backlog AD-007 "company settings GET/PATCH" fechado. Backend Go: migration `0012_company_settings` (tabela singleton via `CHECK (id = 1)`), `CompanySettingsRepository`, `UPLOADS_DIR` config, `internal/uploads.Save` (escrita atômica com overwrite), `CompanySettingsHandler` (`GET`/`PATCH /api/company-settings` + `POST /api/company-settings/logo`, RBAC `ownerOnly`), `logoFileHandler` público sem auth, montado nos dois listeners (admin router e o listener público via `HostRouter` — exigiu wrap em mux, achado real documentado em `design.md` Risks). `PublicStatusHandler.composeResponse` ganhou campo `company` (nome/logo real), cobrindo produção e o preview dev/I12 de uma vez. Frontend: `SettingsPage` conectada a dados reais (nome/e-mail via PATCH, logo via upload multipart separado do form), `public-status/hooks.ts` não importa mais `mockData.companySettings`. 14 tasks (T1-T14) + 3 commits de fix, todos atômicos, branch `main`.

**Gaps reais achados pelo Verifier na rodada 1** (corrigidos na rodada 2): mutante sobrevivente em SET-08 (teste de tamanho de upload derivava do valor sob teste em vez do limite literal do spec — 10 MB nunca fixado); SET-13 sem cobertura nenhuma (falha de escrita em disco → 500 nunca testada); nome do campo multipart `"logo"` não pinado nos dois lados. Lições L-006 a L-009 gravadas em `.specs/LESSONS.md`.

**Desvio técnico documentado como SPEC_DEVIATION no código**: `http.DetectContentType` da stdlib do Go não reconhece assinatura de SVG — adicionada checagem própria de bytes (`isLikelySVG`) além do sniff, nunca confiando no header `Content-Type` do cliente (`internal/api/company_settings_handler.go`).

**Deferred/backlog (não bloqueante)**: multi-réplica sem volume compartilhado (RWX) continua não resolvida em código, documentada como requisito operacional; flake pré-existente de `pg_advisory_lock` em `internal/db`/`internal/api` sob paralelismo (não relacionado a esta feature, já documentado em rodadas anteriores).

**Next steps**: restam 3 itens do backlog original de 4 (ver AD-007/handoff anterior): resend/cancel de convite de admin, teste de 404 em update de incidente inexistente, e a validação manual do conector Datadog real (não MSW) — nenhum solicitado ainda nesta sessão.

---

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
