# Design: Multi-Tenancy Core

## Contexto

Vane hoje é single-tenant por design (AD-002): 1 instalação self-hosted = 1 empresa, sem coluna de tenant em nenhuma tabela. Decisão de negócio: passar a suportar dois modelos de distribuição sobre a **mesma base de código**:

- **Self-hosted** (como hoje): gestor de infra roda o próprio deploy, 1 tenant único auto-provisionado, sem billing, licença anual validada contra o servidor de licença Zeep, LLM/features controladas por licença.
- **SaaS**: multi-tenant real, signup público, plano free + 2 planos pagos, LLM liberado só em plano pago com limites de uso, email transacional via notification-service da Zeep (não o cliente que configura).

Este documento cobre só o subsistema **fundação de multi-tenancy** — o que precisa existir antes de licença (próximo subsistema), billing e notificação-service ficarem em cima. Decomposição completa e ordem de dependência registradas na conversa de brainstorming; ver AD-022 em `.specs/STATE.md`.

Não há hoje instalação self-hosted externa em produção (só o ambiente de teste da Starbem, controlado pela própria equipe) — logo este subsistema **não precisa de migração retrocompatível**: schema é recriado do zero. Esse pressuposto deixa de valer no dia em que existir o primeiro cliente self-hosted externo real (ver AD-022).

## Modelo de dado

### Tabela `tenants`

| coluna | tipo | notas |
|---|---|---|
| `id` | uuid PK | |
| `name` | text | nome de exibição, obrigatório — **substitui `company_settings.name`** (tabela removida, era singleton fixo `id=1`, virava 1 linha por tenant naturalmente) |
| `slug` | text unique | reservado para uso futuro de roteamento por subdomínio (não usado neste subsistema — decisão foi domínio único + seleção por conta) |
| `plan` | text | `free` \| `paid_*` — valor por enquanto sem enforcement real (subsistema de billing cuida disso); self-hosted não usa |
| `status` | text | `active` \| `suspended` etc. |
| `contact_email` | text | migrado de `company_settings.contact_email` |
| `logo_data` / `logo_content_type` | bytea / text nullable | migrado de `company_settings` (`0015_company_settings_logo_storage`) — logo servido via `/uploads/logo` já resolvido por tenant ativo |
| `legal_name` | text nullable | razão social ou nome completo (pessoa física também pode contratar) |
| `tax_id` | text nullable | CNPJ ou CPF, sem formatação forçada na coluna |
| `tax_id_type` | text nullable | enum `cnpj` \| `cpf` |
| `billing_address` | jsonb nullable | usado só no SaaS (subsistema de billing); self-hosted nunca preenche, sem UI pra isso |
| `locale` | text | default `pt-BR`; groundwork para tenant escolher idioma padrão — sem motor de aplicação neste subsistema |
| `primary_color` / `secondary_color` | text nullable | hex; groundwork para white-label/tema — sem motor de aplicação neste subsistema |
| `created_at` | timestamptz | |

Campos `legal_name`/`tax_id`/`tax_id_type` editáveis via tela de "dados da empresa" (settings do tenant, role `owner`) em ambos os modelos. `billing_address` só aparece na UI do SaaS.

### Tabela `users` (substitui `admins`)

Identidade global — 1 email = 1 conta, independente de quantos tenants participa. Sem `tenant_id` na própria tabela.

### Tabela `tenant_memberships` (substitui o vínculo implícito de `admins`)

| coluna | tipo | notas |
|---|---|---|
| `user_id` | FK `users` | |
| `tenant_id` | FK `tenants` | |
| `role` | text | `owner` \| `operator` \| `viewer` (papéis do AD-003, agora escopados por tenant em vez de globais) |

PK composta `(user_id, tenant_id)`.

### Tabela `tenant_invites` (substitui `admin_invites`)

Mesmo mecanismo de hoje (`internal/api/admins.go`, `0010_admin_invites.up.sql`), com `tenant_id` adicionado. Token hash + TTL de 1h inalterados.

### Tabelas de domínio existentes

`services`, `incidents`, `status_pages`, `llm_provider_config`, `email_provider_config`, `domains`, `admin_audit_log` (e qualquer outra tabela de dado por instalação) ganham `tenant_id NOT NULL` desde a criação. `company_settings` é removida — suas colunas migram para `tenants` (ver acima); handler/repository (`internal/api` company-settings, `internal/db` company_settings) passam a ler/escrever direto em `tenants` pelo tenant ativo da sessão.

### Isolamento: RLS obrigatório

Toda tabela com `tenant_id` recebe policy RLS baseada em `current_setting('app.tenant_id')`. Middleware seta `SET LOCAL app.tenant_id = '<uuid>'` no início de cada request/transação. Sem esse setting, RLS bloqueia por padrão (fail-closed) — proteção mesmo se um repository esquecer o `WHERE tenant_id = ?`.

## Provisionamento

### Self-hosted

`/bootstrap` (`internal/api/bootstrap_handler.go`) passa a, no mesmo fluxo que já cria o primeiro owner hoje, criar também a única row de `tenants` (sem billing/plan real) e a `tenant_membership` do owner para esse tenant. UI nunca mostra seletor de tenant — sempre exatamente 1 membership por usuário nesse modelo.

### SaaS — signup público

Novo endpoint `POST /api/signup`:
1. Cria `tenants` (`plan = free`)
2. Cria `users` se o email não existir, ou reaproveita conta existente
3. Cria `tenant_memberships` (role `owner`)
4. Dispara email de verificação (`email_verified_at` em `users` fica null até confirmar)
5. Login bloqueado até verificação — decisão explícita: prioriza evitar tenant fantasma/spam sobre reduzir atrito de onboarding

### Convite de membros do time (self-hosted e SaaS)

Reaproveita o mecanismo já existente em `internal/api/admins.go` (`Invite`/`AcceptInvite`/`ResendInvite`/`CancelInvite`/`UpdateRole`/`Delete`/`List`), com dois ajustes:
- Todas as operações passam a filtrar pelo tenant ativo da sessão (um owner de um tenant não pode listar/gerenciar membros de outro)
- `AcceptInvite` ramifica: se o email do convite já é um `user` existente (ex.: consultor que atende múltiplos tenants), não pede senha — só cria a `tenant_membership` e redireciona pro login. Se é email novo, mantém o fluxo atual (define senha na aceitação) e cria `user` + `tenant_membership`

## Sessão e autenticação

Cookie de sessão (extensão aditiva do AD-004, continua `httpOnly`/`Secure`/`SameSite=Strict`) passa a guardar o `tenant_id` ativo além do `user_id`. Pós-login: se o usuário tem 1 `tenant_membership`, entra direto (caso de todo self-hosted); se tem mais de 1, tela de seleção de tenant antes do dashboard. Endpoint `POST /api/auth/switch-tenant` troca o tenant ativo sem novo login. Papel efetivo (`owner`/`operator`/`viewer`) sempre resolvido via `GET /api/auth/me` no contexto do tenant ativo, nunca decodificado client-side (mantém AD-004).

## Migração

Sem compatibilidade retroativa (ver Contexto). Migrations recriam o schema do zero: dropam `admins`/`admin_invites` e sobem `tenants`, `users`, `tenant_memberships`, `tenant_invites`, `tenant_id` + policies RLS em toda tabela de domínio, tudo já no formato final. Ambiente de teste da Starbem no `eks-starbem-dev` é reprovisionado do zero (`helm uninstall` + reinstall, ou drop schema + reaplica migrations) — sem preocupação de downtime ou backfill.

## Testes

- Suíte de integração já roda contra Postgres descartável (`TEST_DATABASE_URL`, container efêmero) — sem mudança de processo, só passam a exercitar tenant desde a criação
- Teste obrigatório de vazamento cross-tenant: tenant A não enxerga dado de tenant B mesmo simulando um repository que "esquece" o filtro — validação direta de que a RLS barra, não só o caminho feliz

## Fora de escopo deste subsistema (fica para os seguintes)

- Licença self-hosted (servidor de licença Zeep, código de assinatura anual, renovação) — subsistema seguinte, usa o tenant já existente
- Billing/subscription real (Stripe, planos pagos, limites de uso de LLM) — usa `tenants.plan` já modelado aqui
- Connector do notification-service Zeep para o email service já existente (`internal/email/service.go`) — **bloqueia especificamente o signup público do SaaS**, já que verificação de email é obrigatória antes de liberar acesso. Não bloqueia self-hosted (provider configurado pelo gestor já funciona hoje)
- Aplicação real de `locale`/`primary_color`/`secondary_color` na UI (motor de tema/white-label) — colunas existem, sem consumo ainda
- Roteamento por subdomínio (`slug`) — coluna reservada, não usada neste subsistema (decisão foi domínio único + seleção de tenant por conta)
