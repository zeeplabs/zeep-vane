# MVP Core (Zeep Vane) Specification

## Problem Statement

Empresas que usam Datadog não têm forma simples e self-hosted de publicar uma status page pública conectada ao SLO real dos seus serviços. Ferramentas existentes (Cachet, Gatus, Upptime) monitoram por ping próprio, não leem SLO de um APM já configurado. O objetivo é permitir que uma empresa conecte sua conta Datadog, defina serviços mapeados a SLOs existentes, e publique status pages em subdomínios próprios — rodando inteiramente na infra da empresa.

## Goals

- [ ] Admin conecta conta Datadog e mapeia ao menos 1 serviço a um SLO em menos de 10 minutos
- [ ] Status page pública reflete o status real do SLO (operational/degraded/outage) com defasagem máxima de 1 ciclo de polling
- [ ] Admin publica status page em subdomínio próprio com TLS válido sem intervenção manual de certificado

## Out of Scope

Excluído explicitamente desta spec (MVP). Documentado para prevenir scope creep.

| Feature | Reason |
| --- | --- |
| Conectores New Relic e outros APMs | Datadog é o único conector do MVP; outros ficam para fase seguinte |
| Status derivado de Monitors/Alerts do Datadog | MVP lê só SLO; Monitors é P3/fase seguinte |
| Enforcement técnico de license key (gate enterprise) | Estrutura de pasta `ee/` existe, mas sem checagem de license key ativa no MVP |
| Multi-cliente numa única instalação (multi-tenant SaaS) | Cada instalação atende 1 empresa; isolamento é por deploy, não por linha de banco |
| Notificação por email/webhook de mudança de status (subscription) | Feature comum de status page, mas não essencial pro caso de teste inicial |
| SSO/SAML | Reservado para tier enterprise, fora do MVP |

Múltiplos admins com papéis fixos (owner/operator/viewer) reentraram no MVP — ver AD-003 e feature `admin-dashboard`.

---

## Assumptions & Open Questions

Toda ambiguidade foi resolvida ou registrada aqui — nada fica silenciosamente indefinido.

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Nome do projeto | `zeep-vane` — repo GitHub `zeeplabs/zeep-vane`, sem domínio próprio (produto referenciado no site da Zeep Labs) | Nome escolhido pelo usuário | y |
| Mecanismo de provisionamento TLS/domínio custom | Deferido para Design (Let's Encrypt DNS-01 ou proxy tipo Caddy/Traefik) | É decisão de arquitetura, não de requisito; requisito só define o resultado (TLS automático) | n |
| Método de verificação de posse de domínio | Deferido para Design (provável DNS TXT record) | Implementação, não requisito de produto | n |
| Mecanismo de criptografia da API key do Datadog | Deferido para Design (ex: AES-256 com chave local/KMS) | Implementação de storage, não requisito funcional | n |
| Intervalo de polling do SLO | 2 minutos | Equilíbrio entre atualidade e limite de rate do Datadog API; ajustável depois sem mudar contrato | y (aceito via pergunta) |
| Janela de retenção de histórico de incidentes/uptime | 90 dias | Padrão de mercado (Statuspage.io e afins); revisitar se cliente enterprise pedir mais | n |
| Fluxo de recuperação de senha do admin | Reset via token de email, expira em 1 hora | Auth sem recovery é inviável para produção; escopo mínimo de segurança | n |
| Rate limit do endpoint público da status page | Deferido para Design (nível de proxy/reverse proxy) | Detalhe de infraestrutura, não requisito funcional | n |
| Ordem de exibição de updates de incidente | Cronológica, mais recente no topo | Padrão de UX de status pages; sem preferência contrária expressa | n |

**Open questions:** none — todas resolvidas ou registradas acima.

---

## User Stories

### P1: Conectar Datadog e mapear serviço a SLO ⭐ MVP

**User Story**: Como admin da empresa, quero conectar minha conta Datadog e mapear um serviço a um SLO existente, para que o status desse serviço seja monitorado automaticamente.

**Why P1**: Sem essa conexão não existe fonte de dado — é a base de tudo.

**Acceptance Criteria**:

1. WHEN admin submete uma API key do Datadog válida THEN o sistema SHALL armazená-la criptografada e marcar a conexão como ativa.
2. IF a API key submetida for inválida ou sem permissão de leitura de SLO THEN o sistema SHALL rejeitar a conexão e exibir mensagem de erro específica sem salvar a key.
3. WHEN admin seleciona um SLO existente do Datadog para vincular a um serviço THEN o sistema SHALL salvar o vínculo serviço↔SLO.
4. The system SHALL nunca expor a API key do Datadog em texto plano em nenhuma tela ou log após o salvamento inicial.
5. IF a chamada de leitura de SLO ao Datadog falhar (timeout, 401, 5xx) THEN o sistema SHALL tentar novamente com backoff antes de marcar a conexão como falha, e SHALL registrar a falha em log estruturado.

**Independent Test**: Cadastrar API key de uma conta Datadog de teste, vincular 1 serviço a um SLO real, confirmar que o vínculo aparece salvo no dashboard.

---

### P1: Ver status público derivado do SLO ⭐ MVP

**User Story**: Como visitante público, quero ver o status atual de cada serviço na status page, para saber se há algum problema em andamento.

**Why P1**: É o valor central entregue ao usuário final da empresa cliente.

**Acceptance Criteria**:

1. WHILE o error budget do SLO estiver dentro do limite configurado THEN o sistema SHALL exibir o serviço como "operational".
2. WHEN o error budget do SLO ultrapassar o limite configurado THEN o sistema SHALL atualizar o serviço para "degraded" ou "outage" conforme o nível de violação, no próximo ciclo de polling.
3. The system SHALL buscar o status do SLO a cada 2 minutos via job agendado e nunca chamar o Datadog diretamente a partir de uma requisição da página pública.
4. IF a busca de status ao Datadog falhar THEN o sistema SHALL continuar exibindo publicamente o último status válido em cache, com timestamp da última atualização bem-sucedida, e SHALL notificar o admin no dashboard sobre a falha de conexão.
5. The system SHALL exibir a página de status pública sem exigir autenticação do visitante.

**Independent Test**: Forçar (via sandbox/mock) violação de SLO e confirmar que o status público muda de "operational" para "degraded"/"outage" dentro de 1 ciclo de polling; revogar a API key e confirmar que o último status em cache permanece visível com timestamp.

---

### P1: Publicar status page em subdomínio próprio com TLS automático ⭐ MVP

**User Story**: Como admin, quero apontar minha status page para um subdomínio do meu domínio principal, para que meus usuários acessem no meu próprio domínio com HTTPS válido.

**Why P1**: Sem domínio próprio + TLS, a página não é utilizável em produção por uma empresa real.

**Acceptance Criteria**:

1. WHEN admin cadastra um domínio raiz e escolhe um subdomínio para uma status page THEN o sistema SHALL iniciar o processo de emissão de certificado TLS para aquele subdomínio.
2. WHEN o certificado TLS for emitido com sucesso THEN o sistema SHALL marcar a status page como publicada e acessível via HTTPS naquele subdomínio.
3. IF a emissão do certificado falhar (domínio não aponta corretamente, DNS não propagado, etc.) THEN o sistema SHALL manter a status page em estado "pendente de publicação" e exibir ao admin o motivo da falha.
4. The system SHALL permitir múltiplos domínios raiz cadastrados pela mesma empresa, sem limite técnico aplicado no MVP.
5. The system SHALL permitir múltiplas status pages por empresa, cada uma com seu próprio subdomínio, sem limite técnico aplicado no MVP.

**Independent Test**: Cadastrar um domínio de teste com DNS configurado, apontar 1 status page para um subdomínio, confirmar acesso HTTPS válido sem intervenção manual de certificado.

---

### P1: Gerenciar incidentes manuais ⭐ MVP

**User Story**: Como admin, quero criar e atualizar um post de incidente com linha do tempo, para comunicar contexto humano durante um problema além do status automático.

**Why P1**: Status automático sozinho não explica causa/ETA; incidente manual é esperado em qualquer status page (padrão Statuspage.io).

**Acceptance Criteria**:

1. WHEN admin cria um incidente vinculado a um ou mais serviços THEN o sistema SHALL publicá-lo na status page pública correspondente.
2. WHEN admin adiciona um update a um incidente existente THEN o sistema SHALL anexar o update à timeline do incidente, ordenado do mais recente para o mais antigo.
3. WHILE um incidente estiver em estado diferente de "resolved" THEN o sistema SHALL exibi-lo em destaque no topo da status page pública.
4. WHEN admin marca um incidente como "resolved" THEN o sistema SHALL manter o incidente visível no histórico da página pelo período de retenção configurado (90 dias, ver Assumptions).
5. IF admin tentar mover um incidente para um estado anterior ao estado atual (ex: de "resolved" de volta para "investigating") THEN o sistema SHALL permitir a transição (reabertura é caso legítimo) e SHALL registrar a mudança na timeline com timestamp.

**Independent Test**: Criar um incidente, adicionar 2 updates, marcar como resolvido, confirmar que a timeline completa aparece na página pública em ordem cronológica reversa.

---

### P2: Login e conta do admin

**User Story**: Como admin, quero autenticar com email e senha e recuperar acesso se esquecer a senha, para gerenciar a plataforma com segurança.

**Why P2**: Necessário para qualquer uso real, mas é infraestrutura de suporte às stories P1, não o valor central — pode ser testado isoladamente do fluxo de status.

**Acceptance Criteria**:

1. WHEN um admin submete email e senha corretos THEN o sistema SHALL autenticar e criar uma sessão.
2. IF as credenciais forem inválidas THEN o sistema SHALL rejeitar o login sem indicar se o email existe ou não (evita user enumeration).
3. WHEN admin solicita reset de senha THEN o sistema SHALL enviar um token de reset por email, válido por 1 hora.
4. IF o token de reset estiver expirado ou já usado THEN o sistema SHALL rejeitar a tentativa de redefinição de senha.
5. The system SHALL exigir autenticação para todas as ações de dashboard (conectar Datadog, criar status page, gerenciar incidente, cadastrar domínio).

**Independent Test**: Criar conta, fazer logout, solicitar reset de senha, redefinir com o token recebido, logar com a nova senha.

---

### P3: Histórico de uptime em gráfico

**User Story**: Como visitante, quero ver um gráfico dos últimos 90 dias de uptime por serviço, para avaliar confiabilidade histórica.

**Why P3**: Valor incremental sobre o status atual; não bloqueia o teste inicial com a empresa piloto.

**Acceptance Criteria**:

1. WHEN um visitante acessa a status page THEN o sistema SHALL exibir um gráfico de uptime dos últimos 90 dias por serviço.

---

## Edge Cases

- IF admin cadastra o mesmo domínio raiz duas vezes THEN o sistema SHALL rejeitar o cadastro duplicado.
- IF o job de polling do SLO estiver atrasado por falha de infraestrutura interna (não do Datadog) THEN o sistema SHALL expor essa defasagem no timestamp de "última atualização" da página pública, nunca fingir dado fresco.
- IF dois admins editam a mesma status page simultaneamente THEN o sistema SHALL aplicar a última gravação (last-write-wins), sem lock otimista no MVP.
- WHEN um serviço não tem nenhum SLO vinculado ainda THEN o sistema SHALL exibi-lo como "not configured" na tela de admin, e SHALL omiti-lo da página pública até ter um vínculo.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| SP-01 | P1: Conectar Datadog | Design | Pending |
| SP-02 | P1: Conectar Datadog | Design | Pending |
| SP-03 | P1: Conectar Datadog | Design | Pending |
| SP-04 | P1: Conectar Datadog | Design | Pending |
| SP-05 | P1: Conectar Datadog | Design | Pending |
| SP-06 | P1: Status público | Design | Pending |
| SP-07 | P1: Status público | Design | Pending |
| SP-08 | P1: Status público | Design | Pending |
| SP-09 | P1: Status público | Design | Pending |
| SP-10 | P1: Status público | Design | Pending |
| SP-11 | P1: Domínio/TLS | Design | Pending |
| SP-12 | P1: Domínio/TLS | Design | Pending |
| SP-13 | P1: Domínio/TLS | Design | Pending |
| SP-14 | P1: Domínio/TLS | Design | Pending |
| SP-15 | P1: Domínio/TLS | Design | Pending |
| SP-16 | P1: Incidentes | Design | Pending |
| SP-17 | P1: Incidentes | Design | Pending |
| SP-18 | P1: Incidentes | Design | Pending |
| SP-19 | P1: Incidentes | Design | Pending |
| SP-20 | P1: Incidentes | Design | Pending |
| SP-21 | P2: Login/conta | Design | Implementing |
| SP-22 | P2: Login/conta | Design | Implementing |
| SP-23 | P2: Login/conta | Design | Implementing |
| SP-24 | P2: Login/conta | Design | Pending |
| SP-25 | P2: Login/conta | Design | Implementing |
| SP-26 | P3: Uptime histórico | Design | Pending |

**ID format:** `SP-NN` (Status Page)

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 26 total, 0 mapped to tasks, 26 unmapped ⚠️ (esperado — Design/Tasks ainda não rodaram)

---

## Success Criteria

- [ ] Admin conecta Datadog, mapeia 1 serviço a SLO, e vê status refletido publicamente em até 2 minutos
- [ ] Status page publica em subdomínio com HTTPS válido sem intervenção manual de certificado
- [ ] Incidente manual criado aparece na página pública com timeline correta
- [ ] Revogar/invalidar API key do Datadog não derruba a página pública (mostra último status + timestamp)
