# Admin Frontend Specification

## Problem Statement

O backend do zeep-vane (`mvp-core` + `admin-dashboard`) está completo e validado (ambos PASS), mas só é operável via chamada direta de API — não existe interface. Sem uma SPA administrativa, o produto não é usável por uma equipe real de operação; é utilizável apenas por quem sabe montar requisições HTTP manualmente. Esta spec cobre a interface React que expõe todo o backend já construído: autenticação, conexão Datadog, gestão de domínios/status pages com TLS, gestão de incidentes, gestão de admins com papéis (RBAC) e visão de status do poller.

## Goals

- [ ] Um admin recém-convidado consegue, sem ajuda, conectar o Datadog, mapear um serviço a um SLO, cadastrar um domínio e publicar uma status page usando só a SPA (nenhuma chamada de API manual)
- [ ] Um `viewer` nunca vê, na interface, um botão de ação que ele não tem permissão de executar
- [ ] Uma falha de polling do Datadog fica visível para qualquer admin logado em até 1 ciclo de polling, sem precisar recarregar a página manualmente

## Out of Scope

Excluído explicitamente desta spec. Documentado para prevenir scope creep.

| Feature | Reason |
| --- | --- |
| Renderização da status page pública | Já é responsabilidade do "Public status page renderer" no backend (`mvp-core/design.md`), servido por Host header — não é parte desta SPA administrativa |
| Novas regras de negócio no backend, ou remoção/quebra de contrato já existente | Esta spec consome a API já validada (PASS) como base; as únicas adições de backend permitidas são endpoints de leitura pura (AF-34 a AF-38) e a extensão aditiva de login/autenticação para cookie httpOnly + endpoint de logout (AF-39 a AF-41, ver User Stories) — nenhuma delas remove ou quebra um contrato de resposta já existente |
| Tela de consulta/filtro do audit log | Já fora de escopo do `admin-dashboard` (backend); consistente aqui |
| Telemetria/error tracking de frontend (Sentry ou equivalente) | Não decidido — ver Assumptions |
| Notificação de mudança de status por email/webhook (subscription) | Já fora de escopo do `mvp-core` |
| Gráfico de histórico de uptime (SP-26, P3 do mvp-core) | P3 no backend, ainda não implementado; sem UI enquanto o dado não existir |
| Testes E2E automatizados (Playwright) | Reservado para uma fase de hardening após a UI estar funcionalmente completa; ver Assumptions |

---

## Assumptions & Open Questions

Toda ambiguidade foi resolvida ou registrada aqui — nada fica silenciosamente indefinido.

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Observabilidade de erro no frontend | Log em console apenas no MVP, sem serviço de error tracking | Não discutido como decisão de produto; nenhum requisito de telemetria foi levantado | n |
| Concorrência/edição simultânea na UI | Sem lock otimista nem aviso de "outro admin editou"; revalida via TanStack Query após cada mutação | Backend já resolve com last-write-wins; UI não precisa duplicar essa lógica | y |
| Validação client-side de formulário | Replica client-side as regras óbvias do backend (formato, campo obrigatório) para feedback imediato; API continua fonte de verdade | Reduz round-trip para erros triviais sem inventar regra nova | y |
| Expiração de convite/token (visualização) | UI só exibe o estado retornado pela API (expirado, pendente); nenhuma lógica de expiração calculada no cliente | Evita divergência entre relógio do cliente e do servidor | y |
| Testes E2E | Fora do MVP desta spec; cobertura via testes de componente/integração é suficiente por ora | Escopo já grande (2 features de backend inteiras expostas); E2E completo é investimento futuro | n |
| Estrutura de rotas e organização de pastas em `web/` | Deferida para Design | Detalhe de implementação, não requisito de produto | n |

**Open questions:** none — todas resolvidas ou registradas acima.

---

## User Stories

### P1: Login e sessão do admin ⭐ MVP

**User Story**: Como admin, quero autenticar na SPA com email e senha, permanecer logado durante o uso, e ser avisado de forma clara se minha sessão cair, para usar o dashboard com segurança e sem perder contexto de onde parei.

**Why P1**: Toda outra tela do dashboard depende de uma sessão autenticada — é a base de acesso.

**Acceptance Criteria**:

1. WHEN o admin submete email e senha corretos na tela de login THEN o sistema SHALL autenticar via `POST` de login do backend, receber um cookie de sessão `httpOnly`/`Secure`/`SameSite=Strict` setado pelo backend, e redirecionar para a tela inicial do dashboard. The system SHALL nunca persistir o token de sessão em `localStorage`, `sessionStorage`, variável JS ou qualquer outro estado acessível ao client-side JavaScript — o único lugar onde o token existe no browser é o cookie `httpOnly` (que o próprio JS não consegue ler); o campo `token` ainda presente no corpo da resposta de login (mantido por retrocompatibilidade com o contrato já testado no backend) SHALL ser ignorado e descartado pelo frontend, nunca armazenado ou repassado a nenhum outro código.
2. IF as credenciais forem inválidas THEN o sistema SHALL exibir mensagem de erro genérica (não revela se o email existe) sem redirecionar.
3. WHEN o backend responder 401 durante qualquer requisição autenticada (sessão expirada, cookie ausente/inválido, ou admin removido/rebaixado por outro owner) THEN o sistema SHALL abrir um modal bloqueante "Sessão expirada" sobre a tela atual, sem descartar o estado visual de onde o admin estava.
4. WHEN o admin confirma o modal de sessão expirada THEN o sistema SHALL redirecionar para a tela de login.
5. The system SHALL exigir sessão autenticada (cookie válido) para acessar qualquer rota do dashboard além de `/login`; visitante não autenticado que tentar acessar uma rota protegida SHALL ser redirecionado para `/login`. Ao carregar a SPA, o sistema SHALL chamar `GET /api/auth/me` para descobrir se já existe uma sessão válida via cookie, sem exigir novo login se o cookie ainda for válido.
6. WHEN o admin aciona "esqueci minha senha" e submete um email THEN o sistema SHALL disparar o fluxo de reset já existente no backend e exibir confirmação genérica (não revela se o email existe).
7. WHEN o admin aciona "sair" (logout) THEN o sistema SHALL chamar o endpoint de logout, que SHALL invalidar o cookie de sessão no browser, e redirecionar para a tela de login.

**Independent Test**: Logar com credenciais corretas, confirmar (via inspeção de DevTools) que o token não está acessível em `localStorage`/`sessionStorage` nem em variável JS, e ver o dashboard; recarregar a página e continuar autenticado (cookie mantém a sessão); logar com senha errada e ver erro sem redirecionamento; forçar um 401 (ex: revogar sessão no backend) e confirmar que o modal aparece; clicar em "sair" e confirmar que uma rota protegida volta a exigir login.

---

### P1: Conectar Datadog e mapear serviço a SLO ⭐ MVP

**User Story**: Como admin (`owner`/`operator`), quero conectar minha conta Datadog pela interface e mapear serviços a SLOs existentes, para que o status seja monitorado sem eu precisar chamar a API manualmente.

**Why P1**: É a base de dado de todo o produto (SP-01 a SP-05 do mvp-core já validam o backend); sem UI, só é operável via API direta.

**Acceptance Criteria**:

1. WHEN o admin submete uma API key do Datadog no formulário de integração THEN o sistema SHALL enviar ao backend e, em caso de sucesso, exibir a integração como "conectada" sem nunca re-exibir a key em texto plano após o envio.
2. IF o backend rejeitar a API key (inválida ou sem permissão) THEN o sistema SHALL exibir a mensagem de erro específica retornada pela API, mantendo o formulário preenchido para correção.
3. WHEN o admin digita um termo de busca e seleciona um SLO existente na lista retornada por `GET /api/integrations/datadog/slos?query=` (AF-42) para vincular a um serviço THEN o sistema SHALL salvar o vínculo e refletir o serviço como "configurado" na lista.
4. WHILE um serviço não tiver SLO vinculado THEN o sistema SHALL exibi-lo com o rótulo "not configured" na lista de serviços, consistente com o estado já definido no backend (mvp-core edge case).
5. IF `owner`/`operator` estiverem logados THEN o sistema SHALL mostrar as ações de conectar/editar integração; IF o papel for `viewer` THEN o sistema SHALL esconder essas ações (RBAC visual).

**Independent Test**: Conectar uma API key de teste, ver a integração como "conectada", vincular 1 serviço a um SLO real e confirmar que ele deixa de aparecer como "not configured".

---

### P1: Gerenciar domínios e status pages com TLS ⭐ MVP

**User Story**: Como admin (`owner`/`operator`), quero cadastrar um domínio, criar uma status page em um subdomínio e acompanhar a emissão do certificado TLS pela interface, para publicar a página sem intervenção manual nem acompanhamento por chamada de API.

**Why P1**: É o entregável final visível ao cliente da empresa (status page pública); sem UI, o fluxo de domínio/TLS (SP-11 a SP-15) só é operável via API.

**Acceptance Criteria**:

1. WHEN o admin cadastra um domínio raiz e um subdomínio para uma status page THEN o sistema SHALL enviar o cadastro ao backend e exibir a página em estado "emitindo certificado".
2. WHILE uma status page estiver em estado "emitindo certificado" THEN o sistema SHALL fazer polling do status a cada 10 segundos, sem exigir recarregar a página, até o estado mudar para "publicada" ou "falha".
3. WHEN o estado mudar para "publicada" THEN o sistema SHALL exibir a URL pública HTTPS da status page e encerrar o polling.
4. IF o estado mudar para "falha" (emissão de TLS falhou) THEN o sistema SHALL exibir o motivo retornado pelo backend e encerrar o polling, mantendo a página em destaque como "pendente de publicação".
5. IF o admin tentar cadastrar um domínio raiz já existente THEN o sistema SHALL exibir a mensagem de erro específica do backend sem criar duplicata.
6. The system SHALL permitir listar múltiplos domínios raiz e múltiplas status pages por domínio, sem limite aplicado na UI (espelhando o backend).
7. IF o papel for `viewer` THEN o sistema SHALL esconder as ações de cadastrar domínio/status page, mantendo apenas visualização.

**Independent Test**: Cadastrar um domínio de teste, ver o estado "emitindo certificado" mudar automaticamente (via polling) sem interação manual, até published ou failed.

---

### P1: Gerenciar incidentes ⭐ MVP

**User Story**: Como admin (`owner`/`operator`), quero criar um incidente, adicionar updates e mudar seu estado pela interface, para comunicar contexto durante um problema sem usar a API diretamente.

**Why P1**: Comunicação de incidente é esperada em qualquer status page (SP-16 a SP-20 já validados no backend); sem UI só é operável via API.

**Acceptance Criteria**:

1. WHEN o admin cria um incidente vinculando um ou mais serviços THEN o sistema SHALL enviá-lo ao backend e exibi-lo na lista de incidentes ativos.
2. WHEN o admin adiciona um update a um incidente existente THEN o sistema SHALL anexar o update à timeline exibida, ordenado do mais recente para o mais antigo, sem recarregar a página manualmente (revalidação via TanStack Query).
3. WHILE um incidente estiver em estado diferente de "resolved" THEN o sistema SHALL exibi-lo em destaque na lista de incidentes ativos, separado dos resolvidos.
4. WHEN o admin marca um incidente como "resolved" THEN o sistema SHALL mover o incidente para o histórico, mantendo a timeline completa acessível.
5. The system SHALL permitir que o admin mova um incidente de "resolved" de volta para um estado anterior (reabertura), refletindo a mesma permissão já garantida no backend.
6. IF o papel for `viewer` THEN o sistema SHALL esconder as ações de criar incidente/adicionar update/mudar estado, mantendo apenas visualização da timeline.

**Independent Test**: Criar um incidente, adicionar 2 updates, marcar como resolvido, reabrir, e confirmar que a timeline completa aparece em ordem cronológica reversa em cada etapa.

---

### P2: Gerenciar admins com papéis (RBAC)

**User Story**: Como `owner`, quero convidar, mudar o papel e remover outros admins pela interface, para controlar acesso ao dashboard sem usar a API diretamente.

**Why P2**: Depende do login (P1) e não bloqueia o valor central de status/incidente; mas é essencial para operação em equipe (ADM-01 a ADM-09 já validados no backend).

**Acceptance Criteria**:

1. WHEN o `owner` convida um admin informando email e papel THEN o sistema SHALL enviar o convite via backend e exibir o convidado na lista de admins com badge "Pendente".
2. WHEN um convite pendente é reenviado ou cancelado pelo `owner` THEN o sistema SHALL refletir a ação imediatamente na lista (reenviar gera novo prazo; cancelar remove a linha).
3. WHEN o `owner` altera o papel de um admin ativo THEN o sistema SHALL enviar a mudança e atualizar o papel exibido na lista imediatamente.
4. WHEN o `owner` remove um admin THEN o sistema SHALL remover a linha da lista e exibir confirmação via toast.
5. IF a ação de remover/rebaixar resultar em rejeição do backend (zero owners ativos) THEN o sistema SHALL exibir a mensagem de erro específica retornada pela API e manter o estado anterior na lista.
6. The system SHALL restringir o acesso à tela de gestão de admins ao papel `owner`; `operator` e `viewer` SHALL ser redirecionados ou ver a rota oculta na navegação.

**Independent Test**: Como `owner`, convidar um admin `operator`, ver badge "Pendente" na lista; simular aceite (fora da UI, via backend) e ver a linha virar "Ativo" após revalidação; rebaixar esse admin para `viewer` e confirmar que a lista atualiza o papel.

---

### P2: Ver status do poller

**User Story**: Como admin de qualquer papel, quero ver quando o poller do Datadog rodou por último e se falhou, direto na interface, para saber se o status publicado está atualizado sem precisar consultar a API.

**Why P2**: Depende do login (P1); é informação de suporte operacional, não bloqueia o fluxo central de conectar/publicar (ADM-13/14 já validados no backend).

**Acceptance Criteria**:

1. WHEN qualquer admin autenticado acessa a página "Status do Poller" THEN o sistema SHALL exibir, por integração Datadog conectada, o timestamp da última execução, o resultado (sucesso/falha) e a mensagem de erro quando houver falha.
2. WHILE houver uma falha ativa de polling em qualquer integração conectada THEN o sistema SHALL exibir um banner de alerta persistente no topo de toda a SPA (não só na página dedicada).
3. WHEN a falha for corrigida (próxima execução bem-sucedida) THEN o sistema SHALL remover o banner global automaticamente na próxima consulta de status.

**Independent Test**: Forçar falha de conexão Datadog (revogar API key no backend), confirmar que o banner global aparece em qualquer tela do dashboard e que a página dedicada mostra a mensagem de erro e o timestamp da última execução bem-sucedida.

---

### P1: Endpoints de suporte da API ⭐ MVP (gap de backend descoberto no Design)

**User Story**: Como frontend administrativo, preciso de endpoints que hoje não existem no backend (`mvp-core`/`admin-dashboard`, ambos já PASS) — leitura do próprio papel autenticado, listagem de domínios/status pages já cadastrados, e um mecanismo de sessão via cookie httpOnly (login passa a setar cookie, e um endpoint de logout passa a existir para limpá-lo).

**Why P1**: Sem eles, a SPA não consegue implementar RBAC visual (não sabe o próprio papel), popular os selects de domínio/status page, nem gerenciar sessão sem expor o token ao JavaScript — bloqueia todas as outras stories P1 desta spec.

**Acceptance Criteria**:

1. WHEN um admin autenticado chama `GET /api/auth/me` THEN o sistema SHALL retornar `{ id, email, role }` do admin da sessão corrente, com o mesmo middleware `RequireAuth` já usado nas demais rotas protegidas.
2. WHEN um admin autenticado (qualquer papel) chama `GET /api/domains` THEN o sistema SHALL retornar a lista de domínios cadastrados no formato já usado em `POST /api/domains` (`{ id, hostname, created_at }`).
3. WHEN um admin autenticado (qualquer papel) chama `GET /api/status-pages` THEN o sistema SHALL retornar a lista de status pages cadastradas no formato já usado em `POST /api/status-pages` (`{ id, name, subdomain, domain_id, state, tls_last_error, created_at }`).
4. The system SHALL restringir os 3 endpoints acima a admins autenticados (qualquer papel — `owner`/`operator`/`viewer`), consistente com o padrão de leitura já aplicado a `GET /api/services` e `GET /api/poller/status`.
5. WHEN `owner` chama `GET /api/admins` THEN o sistema SHALL incluir, junto aos admins ativos, os convites pendentes (não aceitos e não expirados) do mesmo owner, cada um com um campo `status` (`"active"` ou `"pending"`) — hoje o endpoint retorna só admins ativos e não existe nenhuma forma de listar convite pendente (`AdminInviteRepository` não tem método de listagem).
6. WHEN `POST /api/auth/login` autenticar com sucesso THEN o sistema SHALL, além do corpo de resposta já existente, setar um cookie `httpOnly`, `Secure`, `SameSite=Strict` contendo o token de sessão — aditivo ao contrato atual, sem remover nada do corpo de resposta existente (não quebra nenhum teste/consumidor atual do endpoint).
7. The system SHALL aceitar o token de sessão tanto via cookie quanto via header `Authorization: Bearer` nas rotas protegidas (o middleware de autenticação já existente passa a checar ambas as origens) — mantém retrocompatibilidade com qualquer consumidor de API que já use o header.
8. WHEN o admin chama `POST /api/auth/logout` (autenticado) THEN o sistema SHALL responder 200 e setar o cookie de sessão com expiração imediata (`Max-Age=0`), efetivamente invalidando-o no browser.
9. WHEN um admin autenticado (owner/operator, já que é usado no formulário de criação de serviço) chama `GET /api/integrations/datadog/slos?query=<termo>` THEN o sistema SHALL buscar no Datadog (reaproveitando o mesmo endpoint de busca de SLO já integrado, sem o filtro de ID exato) e retornar uma lista `[{ id, name }]` de SLOs cujo nome contém o termo — sem esse endpoint não existe forma de o admin descobrir o `slo_id` a não ser copiando manualmente do Datadog.

**Independent Test**: Logar como `viewer`, chamar os 3 endpoints de leitura e confirmar 200 com os campos esperados; chamar sem token/cookie e confirmar 401. Como `owner`, convidar um admin e confirmar que `GET /api/admins` retorna a linha com `status: "pending"` antes do aceite. Fazer login, confirmar cookie setado com os atributos corretos (via inspeção de resposta HTTP), chamar uma rota protegida só com o cookie (sem header `Authorization`) e confirmar 200; chamar `POST /api/auth/logout` e confirmar que o cookie subsequente não autentica mais.

---

## Edge Cases

- IF a API do backend estiver indisponível (erro de rede, timeout) em qualquer requisição THEN o sistema SHALL exibir toast de erro genérico e manter a última visualização válida em tela (sem tela em branco).
- IF o admin abrir a SPA sem nenhum domínio/admin/integração cadastrado (instalação nova) THEN o sistema SHALL exibir tela de estado vazio com CTA direto para a primeira ação relevante daquela seção, não uma tabela vazia genérica.
- WHEN o admin está no meio de um formulário e o backend retorna 401 THEN o sistema SHALL preservar os dados digitados no formulário durante o modal de sessão expirada, quando tecnicamente viável (estado local do formulário não é descartado antes do redirect).
- IF um `viewer` tentar acessar diretamente (via URL) uma rota restrita a `owner`/`operator` THEN o sistema SHALL redirecionar para a tela inicial do dashboard, consistente com a ausência do link na navegação.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| AF-01 | P1: Login e sessão | Design | Pending |
| AF-02 | P1: Login e sessão | Design | Pending |
| AF-03 | P1: Login e sessão | Design | Pending |
| AF-04 | P1: Login e sessão | Design | Pending |
| AF-05 | P1: Login e sessão | Design | Pending |
| AF-06 | P1: Login e sessão | Design | Pending |
| AF-07 | P1: Conectar Datadog | Design | Pending |
| AF-08 | P1: Conectar Datadog | Design | Pending |
| AF-09 | P1: Conectar Datadog | Design | Pending |
| AF-10 | P1: Conectar Datadog | Design | Pending |
| AF-11 | P1: Conectar Datadog | Design | Pending |
| AF-12 | P1: Domínios/TLS | Design | Pending |
| AF-13 | P1: Domínios/TLS | Design | Pending |
| AF-14 | P1: Domínios/TLS | Design | Pending |
| AF-15 | P1: Domínios/TLS | Design | Pending |
| AF-16 | P1: Domínios/TLS | Design | Pending |
| AF-17 | P1: Domínios/TLS | Design | Pending |
| AF-18 | P1: Domínios/TLS | Design | Pending |
| AF-19 | P1: Incidentes | Design | Pending |
| AF-20 | P1: Incidentes | Design | Pending |
| AF-21 | P1: Incidentes | Design | Pending |
| AF-22 | P1: Incidentes | Design | Pending |
| AF-23 | P1: Incidentes | Design | Pending |
| AF-24 | P1: Incidentes | Design | Pending |
| AF-25 | P2: Gestão de admins | Design | Pending |
| AF-26 | P2: Gestão de admins | Design | Pending |
| AF-27 | P2: Gestão de admins | Design | Pending |
| AF-28 | P2: Gestão de admins | Design | Pending |
| AF-29 | P2: Gestão de admins | Design | Pending |
| AF-30 | P2: Gestão de admins | Design | Pending |
| AF-31 | P2: Status do poller | Design | Pending |
| AF-32 | P2: Status do poller | Design | Pending |
| AF-33 | P2: Status do poller | Design | Pending |
| AF-34 | P1: Endpoints de suporte da API | Design | Pending |
| AF-35 | P1: Endpoints de suporte da API | Design | Pending |
| AF-36 | P1: Endpoints de suporte da API | Design | Pending |
| AF-37 | P1: Endpoints de suporte da API | Design | Pending |
| AF-38 | P1: Endpoints de suporte da API | Design | Pending |
| AF-39 | P1: Endpoints de suporte da API | Design | Pending |
| AF-40 | P1: Endpoints de suporte da API | Design | Pending |
| AF-41 | P1: Endpoints de suporte da API | Design | Pending |
| AF-42 | P1: Endpoints de suporte da API | Design | Pending |
| AF-43 | P1: Login e sessão (AC7 — logout) | Design | Pending |

**ID format:** `AF-NN` (Admin Frontend)

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 43 total, 0 mapped to tasks, 43 unmapped ⚠️ (esperado — Design/Tasks ainda não rodaram)

---

## Success Criteria

- [ ] Admin convidado consegue logar, conectar Datadog, cadastrar domínio e ver status page publicada usando só a SPA
- [ ] `viewer` nunca vê ação de escrita na interface, em nenhuma tela
- [ ] Emissão de TLS acompanhada por polling automático até estado final, sem recarregar página
- [ ] Falha de polling do Datadog vira banner global visível em qualquer tela dentro de 1 ciclo de polling
- [ ] `owner` convida, muda papel e remove admin, tudo pela interface, sem chamada de API manual
