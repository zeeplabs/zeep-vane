# Deployment Mode Specification

## Problem Statement

Vane suporta dois modelos de distribuição sobre a mesma base de código desde `AD-022` (self-hosted 1-tenant via `/bootstrap`; SaaS multi-tenant real via `/signup`), mas hoje **nenhum sinal de runtime distingue os dois**. As rotas `/api/signup`, `/api/signup/verify/{token}` e `/api/signup/resend-verification` (`internal/api/signup_handler.go`) estão sempre montadas e acessíveis em qualquer instalação (`internal/cli/routes.go`), inclusive uma instalação self-hosted que deveria ter exatamente 1 tenant (o próprio propósito do `/bootstrap`). O frontend também não tem como decidir se mostra o link "Criar conta"/badge "Self-hosted" (gap identificado durante `auth-pages-redesign`) — foi deliberadamente omitido por falta desse sinal.

Decisão de negócio (Julio, 2026-09-15): criar um env var de modo de distribuição. Default (env ausente) é `self_hosted` — a Zeep distribui Vane como open source pra qualquer infra de terceiro. O valor `saas` só é setado no próprio servidor da Zeep, onde cada conta criada via `/signup` é um tenant novo pagante.

## Goals

- [ ] Nova env var `VANE_DEPLOYMENT_MODE` (valores `self_hosted` | `saas`, default `self_hosted` quando ausente/vazio), validada no boot (`internal/config`) — valor desconhecido falha o boot com erro claro, mesmo padrão de `AD-022`'s outras validações de env.
- [ ] Em modo `self_hosted`: `POST /api/signup`, `GET /api/signup/verify/{token}` e `POST /api/signup/resend-verification` retornam 404 (rotas não expostas de fato, não só escondidas na UI).
- [ ] Em modo `saas`: as 3 rotas de signup continuam funcionando como hoje, sem mudança de comportamento.
- [ ] `GET /api/bootstrap/status` (já público, já consultado no boot do frontend) passa a expor `deployment_mode` na resposta, pra o frontend decidir o que mostrar sem precisar de um novo round-trip.
- [ ] Frontend: link "Criar conta"/"Já tem uma conta?" (`LoginPage`, `SignupPage`) e o texto/estado de acesso restrito só aparecem/mudam conforme o modo real da instância — nunca mais uma escolha assumida ou fabricada.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Badge visual "Self-hosted" no canto da tela de login (do mock) | Fora do pedido explícito desta feature (só link de criar conta + gate de rota); pode ser adicionado depois trivialmente já que o sinal (`deployment_mode`) passa a existir — decisão de UI separada, não travada aqui. |
| Bloquear `/bootstrap` por modo | `/bootstrap` já se autolimita (só funciona 1 vez, `bootstrapped=false`→`true`, 409 depois) independente do modo — SaaS também precisa rodar `/bootstrap` uma única vez pra criar o admin da própria Zeep. Nenhuma mudança necessária aqui. |
| Migração de instalações já rodando | Nenhuma instalação self-hosted externa real em produção hoje (mesma premissa de `AD-022`) — sem preocupação de compatibilidade retroativa. |
| Enforcement de billing/plano por modo | Já modelado como fora de escopo em `multi-tenancy-core`'s design.md; `deployment_mode` não mexe em `tenants.plan`. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Gate de `/signup` em self-hosted | Bloqueio real no backend (404), não só ocultar link na UI | Decisão explícita do usuário via `AskUserQuestion` — chamada direta de API contornaria um esconder-só-na-UI, contradizendo o próprio modelo self-hosted (1 tenant). | y |
| Nome/valores da env var | `VANE_DEPLOYMENT_MODE`, `self_hosted` \| `saas`, default `self_hosted` | Mesmo padrão de nomenclatura (`VANE_*`) e validação estrita (rejeita valor desconhecido no boot) já usado em `internal/config/config.go` pras outras env vars. Default self-hosted é a decisão explícita do usuário: "a ideia desse sistema é poder ser subido na infra de qualquer pessoa". | y |
| Onde expor o modo pro frontend | Campo novo `deployment_mode` na resposta já existente de `GET /api/bootstrap/status` | Endpoint já é público, pré-auth, e já consultado uma vez no boot do `AuthProvider` (`needsBootstrap`) — reaproveita o mesmo round-trip em vez de criar um endpoint novo só pra isso. | y |
| Status HTTP do bloqueio de signup | 404 (rota não existe), não 403 | Não vazar a existência da capacidade de signup pra quem não deveria usá-la é mais consistente com "essa rota não existe nesta instalação" do que "existe mas você não pode". | y |

**Open questions:** none — todas resolvidas ou registradas acima.

---

## User Stories

### P1: Env var + gate de backend das rotas de signup ⭐ MVP

**User Story**: Como operador de uma instalação self-hosted, quero que as rotas de criação de novo tenant estejam de fato desligadas (não só escondidas), para que minha instalação continue sendo garantidamente 1-tenant mesmo contra uma chamada de API direta.

**Why P1**: É a peça de segurança real — sem ela, o resto (frontend escondendo o link) é só cosmético.

**Acceptance Criteria**:

1. The system SHALL ler `VANE_DEPLOYMENT_MODE` no boot (`internal/config.Load`), aceitando somente `""` (ausente, equivale a `self_hosted`), `"self_hosted"` ou `"saas"`; qualquer outro valor SHALL falhar o boot com um erro claro (mesmo padrão das demais validações de `config.go`).
2. WHILE `deployment_mode == self_hosted` (incluindo o default), `POST /api/signup`, `GET /api/signup/verify/{token}` e `POST /api/signup/resend-verification` SHALL responder 404, sem tocar o handler/repositório de signup.
3. WHILE `deployment_mode == saas`, as 3 rotas acima SHALL se comportar exatamente como hoje, sem nenhuma mudança de contrato.
4. `POST /api/bootstrap` SHALL continuar funcionando de forma idêntica em ambos os modos (sem gate novo) — sua própria checagem de `bootstrapped` já é a única proteção necessária.

**Independent Test**: sem `VANE_DEPLOYMENT_MODE` setado, `curl -X POST /api/signup` retorna 404; com `VANE_DEPLOYMENT_MODE=saas`, o mesmo `curl` funciona como hoje (cria tenant/envia verificação).

---

### P1: Frontend consome o modo real (sem link fabricado) ⭐ MVP

**User Story**: Como usuário anônimo acessando o login, quero ver "Criar conta" só quando essa instalação realmente aceita signups, para não ser levado a um formulário que vai falhar.

**Why P1**: Fecha o gap identificado em `auth-pages-redesign` — hoje o link aparece sempre, mesmo quando o backend vai recusar em self-hosted.

**Acceptance Criteria**:

1. `GET /api/bootstrap/status` SHALL incluir `deployment_mode: "self_hosted" | "saas"` no corpo da resposta, ao lado do `bootstrapped` já existente.
2. `AuthProvider` SHALL expor esse valor (`deploymentMode`) no mesmo contexto que já expõe `needsBootstrap`, consultado no mesmo boot fetch existente (sem round-trip novo).
3. WHILE `deploymentMode === "self_hosted"`, `LoginPage` SHALL ocultar o link "Não tem uma conta? Criar conta" (adicionado em `auth-pages-redesign`) e `SignupPage`/`VerifyEmailPage` continuam existindo como rotas (sem remoção de código), mas `SignupPage` SHALL exibir uma mensagem de acesso restrito em vez do formulário quando a instalação for self-hosted, refletindo o 404 real do backend.
4. WHILE `deploymentMode === "saas"`, o comportamento SHALL ser idêntico ao que `auth-pages-redesign` já entregou (link visível, formulário funcional).

**Independent Test**: rodar o backend sem `VANE_DEPLOYMENT_MODE`, abrir `/login`, confirmar que "Criar conta" não aparece; setar `VANE_DEPLOYMENT_MODE=saas`, reiniciar, confirmar que aparece e funciona.

---

## Edge Cases

- IF `VANE_DEPLOYMENT_MODE` está setado mas vazio (`VANE_DEPLOYMENT_MODE=`) THEN o sistema SHALL tratar como ausente (`self_hosted`), mesmo padrão de outras env vars opcionais deste arquivo (`os.Getenv` retorna `""` tanto pra ausente quanto pra vazio).
- IF o frontend acessa `/signup` diretamente pela URL numa instalação self-hosted (sem passar pelo link oculto) THEN a tela SHALL mostrar a mensagem de acesso restrito, nunca um formulário que vai falhar com 404 silencioso ao submeter.
- WHILE o boot fetch de `deployment_mode` ainda está em voo, `AuthProvider` SHALL usar `"saas"` como valor otimista (mesmo princípio de `needsBootstrap` default `false`) — evita mostrar por um instante a mensagem de acesso restrito numa instância SaaS real enquanto o fetch resolve.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| DEPMODE-01 | P1: Env var + gate de backend | - | Implementing |
| DEPMODE-02 | P1: Env var + gate de backend | - | Implementing |
| DEPMODE-03 | P1: Env var + gate de backend | - | Implementing |
| DEPMODE-04 | P1: Env var + gate de backend | - | Implementing |
| DEPMODE-05 | P1: Frontend consome o modo | - | Implementing |
| DEPMODE-06 | P1: Frontend consome o modo | - | Implementing |
| DEPMODE-07 | P1: Frontend consome o modo | - | Implementing |
| DEPMODE-08 | P1: Frontend consome o modo | - | Implementing |

**ID format:** `DEPMODE-[NUMBER]`

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 8 total, 8 mapeados a tarefas (implícitas, escopo Medium), 0 unmapped

---

## Success Criteria

- [ ] `VANE_DEPLOYMENT_MODE` documentada no README (tabela de Configuração, AGENTS.md §6).
- [ ] Nova entrada `AD-033` em `.specs/STATE.md` registrando a decisão (env var, default, gate real vs. cosmético).
- [ ] `go build`/`go vet`/`gofmt` limpos; suíte de integração backend verde (Postgres descartável); `tsc -b --noEmit` e suíte de frontend verdes.
