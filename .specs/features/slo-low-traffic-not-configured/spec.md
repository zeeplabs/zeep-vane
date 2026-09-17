# SLO Low-Traffic Stuck-on-not_configured Specification

## Problem Statement

A service linked to a real Datadog SLO (`slo_id` set, confirmed correct, SLO healthy in Datadog) can stay `current_status = "not_configured"` forever in Vane. Root cause: `internal/db/service_repository.go` seeds every new service's `current_status` at `"not_configured"`, and `internal/poller/poller.go`'s `pollService` only overwrites it when the polled window has at least `minRecentWindowRequests = 10` requests (AD-019) — below that, `current = svc.CurrentStatus` (carry-forward, unchanged). A service whose 5-minute polling window (`recentWindowWidth`) never reaches 10 requests (low-volume flows: login, cadastro, prontuário — as opposed to high-volume flows like agendamento) never gets a first real classification, so it carries `"not_configured"` forward every cycle, indefinitely, with no error and no log line — confirmed live against 4 such services (`flow:rh:fluxo-login-portal`, `flow:profissional:fluxo-autenticacao-profissional`, `flow:parceiro:fluxo-cadastro-empresa`, `flow:profissional:fluxo-prontuario`) whose Datadog SLOs are green and whose `slo_id` was confirmed correct in the database.

`not_configured` today conflates two different situations that look identical to a user: (a) a service with no `slo_id` at all (never linked), and (b) a service with a correct `slo_id` that has simply never received a first real classification because its traffic never crossed the floor. This spec fixes (b) only.

## Goals

- [ ] A service with `slo_id` set gets a real first classification (`operational`/`degraded`/`outage`, per the existing classification logic) on the first poll cycle whose SLO response is otherwise usable, even if `RequestCount < minRecentWindowRequests` — the traffic floor stops applying only for that first-ever classification.
- [ ] Once a service has left `not_configured` via a real classification, every subsequent low-traffic cycle (`RequestCount < minRecentWindowRequests`) behaves exactly as today: carry the previous status forward unchanged (AD-019's noise-avoidance intent is preserved for all classifications after the first).
- [ ] A service that genuinely has no `slo_id` (`monitor_mode = "polling"` with no SLO link, or an `slo` service never given an `slo_id`) is unaffected — this spec does not change how or when such a service reaches `not_configured`, only how a service that already has a valid link escapes it.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Reclassificação retroativa de `status_intervals` já gravados como `not_configured` para os serviços afetados | Decisão explícita do usuário: deixar como está. Histórico reflete o que o sistema achava na época; o fix vale só daqui pra frente. |
| Novo estado explícito (`insufficient_data`/similar) distinto de `not_configured` | Decisão explícita do usuário: manter `current_status` com os 4 valores existentes (`not_configured`/`operational`/`degraded`/`outage`), sem novo contrato de frontend/filtros/public status page. |
| Seed inicial diferente (`operational` em vez de `not_configured`) para serviço recém-linkado | Rejeitado — assumiria saúde sem nenhum dado real por trás, podendo mascarar um outage genuíno em serviço de baixo tráfego. |
| Calibração do piso `minRecentWindowRequests = 10` ou da janela `recentWindowWidth = 5min` | Já documentado como piso não calibrado (AD-019/addendum 3) e fora do escopo deste bug — este spec não muda os valores, só quando o piso se aplica. |
| Qualquer mudança em `breachHysteresisCycles`/`breachThresholdSigmas` (lógica de detecção de outage) | Ortogonal — este bug é sobre a *primeira* classificação nunca acontecer, não sobre a lógica de breach em si. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Abordagem de fix | Primeira classificação roda mesmo com `RequestCount < minRecentWindowRequests`, mas **somente quando `svc.CurrentStatus == "not_configured"`** (nunca houve classificação real ainda). Depois da primeira classificação real, o piso de tráfego volta a valer normal para toda atualização subsequente. | Decisão do usuário após recomendação explícita (`AskUserQuestion`): menor blast radius (só `pollService`), preserva intenção do AD-019 para classificações subsequentes, e qualquer dado real é estritamente melhor que um placeholder sem nenhuma medição por trás. | y |
| Histórico já gravado indevidamente como `not_configured` | Deixar como está — fora de escopo, sem migração/reclassificação retroativa. | Decisão explícita do usuário. | y |
| O que conta como "resposta do SLO utilizável" para disparar a primeira classificação | Mesma condição que já existe hoje para as demais branches de `pollService` — a chamada a `FetchWithRetry`/`FetchSLOStatus` deve ter retornado sem erro (`err == nil`). Se a chamada falhar, o comportamento é inalterado: `pollService` retorna erro, `current_status` não é tocado, serviço permanece `not_configured`. | Consistente com o restante da função — este fix só remove o piso de `RequestCount`, nunca contorna uma falha real de fetch. | y |
| `status.Target <= 0` (branch `classifyByState`, sem threshold utilizável) durante a primeira classificação | Também sai do `not_configured` normalmente via `classifyByState` (usa `status.State`), já que essa branch não depende de `RequestCount` hoje — nenhuma mudança necessária ali, só documentado para deixar claro que o fix cobre apenas a branch `RequestCount < minRecentWindowRequests`. | A branch `Target <= 0` já ignora o piso de tráfego hoje (não checa `RequestCount`) — não é o caminho que trava esses 4 serviços, mas vale registrar que ela já se comporta como o Goal pede, sem mudança. | y |
| Efeito em `breachStreak` na primeira classificação com poucos requests | Segue a mesma lógica já existente da branch escolhida (breach → incrementa/latch conforme hysteresis; não-breach → reseta a 0) — nenhuma regra nova de hysteresis introduzida para o caso de poucos requests. | Reusa o mecanismo existente sem inventar um caminho paralelo; a primeira classificação com poucos requests se comporta, para fins de hysteresis, exatamente como uma classificação normal seria hoje se o piso não existisse. | y |

**Open questions:** none — todas resolvidas ou registradas acima.

---

## User Stories

### P1: Serviço de baixo tráfego sai de not_configured na primeira medição válida ⭐ MVP

**User Story**: Como owner, quero que um serviço com `slo_id` correto e SLO saudável no Datadog deixe de aparecer como "não configurado" assim que o poller conseguir uma leitura válida, mesmo que o tráfego do serviço seja baixo, para que a página de status pública reflita a saúde real do serviço em vez de um estado padrão que nunca é atualizado.

**Why P1**: É o próprio bug relatado — sem isso, o serviço fica preso indefinidamente, indistinguível de "nunca configurado".

**Acceptance Criteria**:

1. **SLOTRAF-01**: WHEN um serviço tem `MonitorMode == "slo"`, `SLOID` não vazio, `CurrentStatus == "not_configured"`, e o poller recebe uma resposta de `FetchSLOStatus` sem erro com `status.Target > 0` e `status.RequestCount < minRecentWindowRequests` THEN o sistema SHALL classificar `current_status` usando a mesma lógica de comparação já existente para uma classificação normal (`status.SLI < breachBound(...)` → hysteresis de breach; caso contrário → `degraded` ou `normalizeStatus(status.State)`), em vez de simplesmente carregar `"not_configured"` para frente.
2. **SLOTRAF-02**: WHEN um serviço já saiu de `"not_configured"` (isto é, `CurrentStatus` já é `"operational"`, `"degraded"`, ou `"outage"`) e uma nova leitura chega com `status.RequestCount < minRecentWindowRequests` THEN o sistema SHALL manter o comportamento atual inalterado — `current = svc.CurrentStatus` (carry-forward), sem nenhuma reclassificação.
3. **SLOTRAF-03**: WHEN um serviço tem `MonitorMode == "slo"` mas `SLOID == ""`, OR `MonitorMode == "polling"` THEN o comportamento do poller SHALL permanecer exatamente como hoje — este AC não introduz nenhum caminho novo de classificação para serviço sem `slo_id` vinculado.
4. **SLOTRAF-04**: IF a chamada a `FetchWithRetry`/`FetchSLOStatus` retornar erro (qualquer motivo — credencial inválida, SLO não encontrado, timeout) THEN `pollService` SHALL continuar retornando erro sem tocar `current_status`, exatamente como hoje — a remoção do piso de tráfego vale somente para uma resposta bem-sucedida.
5. **SLOTRAF-05**: WHEN a primeira classificação de um serviço `not_configured` resulta em transição real (`current != "not_configured"`) THEN o sistema SHALL disparar `p.analyzer.HandleTransition` exatamente como já dispara hoje para qualquer transição — nenhuma mudança nesse mecanismo.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| SLOTRAF-01 | P1: Serviço de baixo tráfego sai de not_configured na primeira medição válida | - | Implementing |
| SLOTRAF-02 | P1: Serviço de baixo tráfego sai de not_configured na primeira medição válida | - | Implementing |
| SLOTRAF-03 | P1: Serviço de baixo tráfego sai de not_configured na primeira medição válida | - | Implementing |
| SLOTRAF-04 | P1: Serviço de baixo tráfego sai de not_configured na primeira medição válida | - | Implementing |
| SLOTRAF-05 | P1: Serviço de baixo tráfego sai de not_configured na primeira medição válida | - | Implementing |
