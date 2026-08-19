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

**Feature**: `mvp-core` — **status: PASS ✅** (Verifier completou em 2 rodadas de fix→re-verify, dentro do limite de 3). Relatório: `.specs/features/mvp-core/validation.md`.

**Completo**: T1-T40 (todas as 7 fases) implementadas via 6 batches de sub-agentes + 2 rodadas de fix (dead-code wiring do router público/handler em `serve.go`, e scoping de SP-15 pra `status_page_services`/`incident_services` — antes write-only, agora lido e testado). Branch `main`, working tree limpa. Último commit: `9efdcef` (test: disjoint-incidents SP-15 scoping).

**Deferred ideas (não resolvidas, sem context.md ainda pra feature)**:
- Cliente Datadog é construído uma vez no startup do `serve`; se admin reconectar com chave nova em runtime, poller continua usando client antigo até restart (surgiu no batch de Phase 4).
- `internal/poller` nunca é chamado por request pública — verificado estruturalmente (nenhum import cruzado), não por teste de request-path, já que handler público só existiu a partir da Phase 6.
- 3 spec-precision gaps não-bloqueantes flagados pelo Verifier final: shape de log da SP-05 não travado, SP-08 ("nunca chama Datadog a partir de request pública") sem teste ativo dedicado, e label de estado `tls_failed` (spec usa texto "pendente de publicação").
- Tabela de Requirement Traceability em `spec.md` não foi atualizada linha a linha durante os fixes — vale uma passada separada se o time quiser ela 100% current.

**Next steps**: `admin-dashboard` (13 tasks, 4 fases) ainda não iniciado — depende de tabela `admins` (já existe, T8/T9 do mvp-core) e das rotas já registradas (T18/T20/T27/T29/T37/T38/T39, todas prontas). Pronto pra rodar Execute quando o usuário autorizar. Depois: frontend specs (admin React UI) — combinado explicitamente pra ficar pra depois do backend, ainda não começado.
