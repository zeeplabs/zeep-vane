# Poller Zero-Request False-Outage Specification

## Problem Statement

A service whose Datadog SLO genuinely receives zero requests in a poll window (`RequestCount <= 0`) can be classified as `"outage"` even though no request ever failed — because no request happened at all. Root cause: `breachBound(target, requestCount, sigmas)` (`internal/poller/breach_threshold.go:42-45`) returns the raw `target` (e.g. 99.5) when `requestCount <= 0`, and `pollService` (`internal/poller/poller.go:339`) then compares `status.SLI < breachBound(...)` — with no data, `SLI` reads `0`, so `0 < 99.5` is always true regardless of real health. This feeds `breachStreak`, and after `breachHysteresisCycles = 2` consecutive zero-request cycles, `current_status` latches to `"outage"`.

This gap was introduced by `086d678` (`slo-low-traffic-not-configured`, SLOTRAF-01), which deliberately skips the low-volume carry-forward guard (`poller.go:323`) for a service's first-ever classification (`CurrentStatus == "not_configured"`) so it doesn't carry a placeholder forever. That fix's own rationale — "any real reading beats a placeholder backed by zero data" — is self-contradicting for `RequestCount == 0`: a window with zero requests is not a reading at all, it is exactly the "zero data" the rationale says shouldn't count. `breach_threshold.go`'s own doc comment already assumed this couldn't happen ("Callers still apply their own low-volume guards before relying on the result") — SLOTRAF-01 broke that assumption for the `not_configured` case without noticing.

Confirmed live: SLO `flow:profissional:fluxo-autenticacao-profissional` (doctors-ms login, 13 requests total in 30 days) shows `"outage"` (admin: "Inativo", public page: "Interrupção em andamento") despite the SLO reporting 100% healthy in Datadog — the service simply never crossed 1 request in its most recent 5-minute polling windows.

## Goals

- [ ] A poll window with `RequestCount <= 0` never counts as a breach signal and never advances `breachStreak`, whether it's a service's first-ever classification or a later one.
- [ ] A service currently latched into `"outage"`/`"degraded"` by this exact bug self-corrects on the first poll after the fix ships, without manual intervention.
- [ ] Every other classification path (`RequestCount` between 1 and `minRecentWindowRequests - 1`, `RequestCount >= minRecentWindowRequests`, `Target <= 0`) is untouched — this is a narrow fix to the zero-request boundary only.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Reclassificação retroativa de `status_intervals` já gravados como `outage`/`degraded` para os serviços afetados | Mesma decisão já tomada em `slo-low-traffic-not-configured`: histórico reflete o que o sistema achava na época; o fix vale daqui pra frente (incluindo a correção do `current_status` ao vivo, que é o que a página pública mostra agora — ver AC ZEROREQ-02). |
| Calibração de `minRecentWindowRequests`, `recentWindowWidth`, `recentWindowLag`, `breachThresholdSigmas`, `breachHysteresisCycles` | Já documentados como pisos não calibrados (AD-019 lineage) e ortogonais a este bug — este spec não muda nenhum desses valores. |
| Novo estado explícito distinto de `not_configured`/`operational`/`degraded`/`outage` | Mesma decisão já tomada em `slo-low-traffic-not-configured`: manter os 4 valores existentes, sem novo contrato de frontend/filtros/página pública. |
| Mudar o comportamento para `RequestCount` entre 1 e `minRecentWindowRequests - 1` | Fora do escopo — esse caso já funciona corretamente hoje (carry-forward via guard existente para serviço já classificado; classificação normal via `086d678` para o primeiro poll). O bug é especificamente `RequestCount <= 0`, não "baixo volume" em geral. |
| Mudar o comportamento para `CurrentStatus == "operational"` com `RequestCount <= 0` | Já correto hoje (carry-forward via guard existente) — uma janela sem requests nunca pôde ter produzido `"operational"` por engano, então não há nada a corrigir nesse caminho. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Escopo do fix | Só o limite `RequestCount <= 0`, tanto na primeira classificação quanto como recuperação para serviços já presos em `outage`/`degraded` por essa mesma causa. Caso já classificado como `operational`, ou `RequestCount` entre 1 e `minRecentWindowRequests-1`, permanece exatamente como hoje. | Decisão do usuário (`AskUserQuestion`): menor blast radius, consistente com a decisão já tomada em `slo-low-traffic-not-configured`. | y |
| Recuperação automática de serviços já presos | Sim — ao processar um poll com `RequestCount <= 0` para um serviço cujo `CurrentStatus` já é `"outage"` ou `"degraded"`, reclassificar via `normalizeStatus(status.State)` em vez de carregar o status atual pra frente cegamente. Nunca mexe em `breachStreak`. | Decisão do usuário (`AskUserQuestion`) — recomendação explícita para evitar exigir ação manual (ex: UPDATE via admin) para desprender serviços já afetados, já que o bug está ativo em produção agora. | y |
| `RequestCount <= 0` nunca pode, por si só, indicar um outage real | Verdadeiro por construção do SLI usado (`trace.express.request.hits`/`trace.express.request.errors`, contagem de requisições que efetivamente chegaram ao serviço) — uma falha real (5xx) ainda conta como uma requisição no denominador, então `RequestCount > 0` sempre que houve qualquer tentativa, com sucesso ou falha. `RequestCount == 0` significa literalmente "ninguém tentou", nunca "todo mundo falhou". | Justifica por que reclassificar via `normalizeStatus(status.State)` em vez de assumir breach é seguro — não existe outage real medível com `RequestCount == 0` nesta SLI. | y |
| Trade-off aceito da recuperação automática (ZEROREQ-02) | Um outage real que **também** zera o tráfego de entrada no mesmo momento (ex: load balancer remove o pod doente da rota, e nenhuma requisição chega a ele) seria mostrado como `"degraded"` em vez de `"outage"` congelado, na primeira janela pós-recuperação em que isso ocorrer. Aceito como consequência direta da mesma lógica de ZEROREQ-01 (zero requests nunca é evidência de outage) — não é um caso novo introduzido por este fix, é o mesmo raciocínio já aceito para a primeira classificação, agora também aplicado à recuperação. | Doc explícito do trade-off, no mesmo estilo de honestidade das decisões AD-019 anteriores — não é um requisito quebrado, é uma consequência conhecida e aceita. | y |
| `status.State` para uma janela com `RequestCount <= 0` | Assume-se que o Datadog retorna um `state` coerente com "sem dado" (ex: `"no_data"`, ou vazio/outro valor não reconhecido) para uma janela sem requests, o que `normalizeStatus` já mapeia para `"degraded"` (`poller.go:432-438`, caminho default). Nenhuma mudança necessária em `normalizeStatus`. | Consistente com o comportamento documentado da própria função (SPEC_DEVIATION já registrado no código) — não é um valor novo a tratar, apenas um caminho que passa a ser alcançado em mais um cenário (`RequestCount<=0`) além do já existente (`Target<=0`, via `classifyByState`). | y |

**Open questions:** none — todas resolvidas ou registradas acima.

---

## User Stories

### P1: Janela sem requests nunca é tratada como breach ⭐ MVP

**User Story**: Como owner, quero que um serviço de baixíssimo tráfego não seja marcado como "outage" só porque uma janela de poll não teve nenhuma requisição, para que a página de status pública reflita a saúde real do serviço em vez de um falso positivo derivado da ausência de dado.

**Why P1**: É o próprio bug relatado, ativo em produção agora (`flow:profissional:fluxo-autenticacao-profissional`).

**Acceptance Criteria**:

1. **ZEROREQ-01**: WHEN `pollService` recebe uma resposta de `FetchSLOStatus` sem erro com `status.RequestCount <= 0` E `svc.CurrentStatus == "not_configured"` THEN o sistema SHALL classificar `current_status` via `normalizeStatus(status.State)`, sem avaliar `status.SLI` contra `breachBound(...)` e sem incrementar `p.breachStreak[svc.ID]`.
2. **ZEROREQ-02**: WHEN `pollService` recebe uma resposta sem erro com `status.RequestCount <= 0` E `svc.CurrentStatus` é `"outage"` OU `"degraded"` THEN o sistema SHALL reclassificar `current_status` via `normalizeStatus(status.State)` em vez de carregar `svc.CurrentStatus` pra frente, sem alterar `p.breachStreak[svc.ID]`.
3. **ZEROREQ-03**: WHEN `pollService` recebe uma resposta sem erro com `status.RequestCount <= 0` E `svc.CurrentStatus == "operational"` THEN o sistema SHALL manter o comportamento atual inalterado (`current = svc.CurrentStatus`, via o guard de baixo volume já existente).
4. **ZEROREQ-04**: WHILE `0 < status.RequestCount < minRecentWindowRequests` THEN o sistema SHALL manter o comportamento atual inalterado para todo `CurrentStatus` (carry-forward se já classificado; classificação normal via `086d678` se `not_configured`) — este spec não muda nada nessa faixa.
5. **ZEROREQ-05**: WHEN uma reclassificação sob ZEROREQ-01 ou ZEROREQ-02 resulta em `current != svc.CurrentStatus` THEN o sistema SHALL disparar `p.analyzer.HandleTransition` exatamente como já dispara hoje para qualquer transição — nenhum mecanismo novo.
6. **ZEROREQ-06**: IF a chamada a `FetchWithRetry`/`FetchSLOStatus` retornar erro THEN `pollService` SHALL continuar retornando erro sem tocar `current_status`, exatamente como hoje — este fix vale somente para uma resposta bem-sucedida.

**Independent Test**: Simular (via `fakeProvider` de teste) uma resposta `RequestCount: 0` para um serviço `not_configured` e para um serviço já `outage`/`degraded`/`operational`, e verificar `current_status` resultante em cada caso, mais um caso de `RequestCount: 5` (dentro de 1..9) confirmando que o comportamento antigo (guard existente / classificação normal do 086d678) permanece intacto.

---

## Edge Cases

- IF `status.RequestCount == 0` E `status.State` vier vazio/valor não reconhecido pelo Datadog THEN o sistema SHALL cair no branch default de `normalizeStatus` (`"degraded"`), nunca em `"operational"` ou `"outage"` — comportamento já existente, sem mudança de código necessária.
- IF um serviço real tiver um outage genuíno que também zera o tráfego de entrada no mesmo ciclo THEN o sistema SHALL mostrar `"degraded"` em vez de manter `"outage"` congelado nesse ciclo — trade-off aceito, documentado na tabela de Assumptions acima.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| ZEROREQ-01 | P1: Janela sem requests nunca é tratada como breach | - | Verified |
| ZEROREQ-02 | P1: Janela sem requests nunca é tratada como breach | - | Verified |
| ZEROREQ-03 | P1: Janela sem requests nunca é tratada como breach | - | Verified |
| ZEROREQ-04 | P1: Janela sem requests nunca é tratada como breach | - | Verified |
| ZEROREQ-05 | P1: Janela sem requests nunca é tratada como breach | - | Verified |
| ZEROREQ-06 | P1: Janela sem requests nunca é tratada como breach | - | Verified (unchanged code path, covered by pre-existing tests) |

**Coverage:** 6 total, 6 mapped to tests, 0 unmapped

---

## Success Criteria

- [ ] `flow:profissional:fluxo-autenticacao-profissional` (e qualquer outro serviço com o mesmo padrão de baixíssimo tráfego) para de mostrar `"outage"`/"Inativo"/"Interrupção" sem uma falha real por trás.
- [ ] Nenhum teste existente de `poller_recent_window_test.go`/`breach_threshold_test.go` quebra.
- [ ] `go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l` limpos.
