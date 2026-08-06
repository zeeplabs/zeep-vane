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

## Handoff

[none yet - Design phase recém concluída]
