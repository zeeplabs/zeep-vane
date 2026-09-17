# Degraded-Interval Analysis Specification

## Problem Statement

The public status page's hourly bars show a service's status per bucket (operational/degraded/outage/no_data, `internal/history/hourly.go`), but no explanation. Today the only AI-generated analysis text (`services.status_analysis`) is scoped to the service's *current* state, is overwritten on every transition (`internal/poller/analyzer.go` `HandleTransition`), and is shown only as a hover title on the live status badge - a past degraded episode's reason is never persisted anywhere durable, and a bucket spanning multiple episodes has no way to show more than one.

The user wants: clicking a degraded hourly bar opens a popover listing the AI-generated reason(s) for every degraded episode that bucket overlaps, most recent first.

## Goals

- [ ] Every transition into `"degraded"` persists its AI-generated analysis text against the specific `status_intervals` row it describes, not just the service's current-state column.
- [ ] `services.status_analysis` (current-state badge tooltip) keeps working exactly as it does today - untouched code path, parallel mechanism.
- [ ] The public status page's hourly-history endpoint exposes, per bucket, every degraded episode that overlaps it (start, end, analysis text or null), most recent first.
- [ ] Clicking a degraded bar opens an accessible popover (click to open, Escape/outside-click to close, keyboard-focusable) listing those episodes; a bucket with no degraded episodes is not interactive.
- [ ] A bucket status stays computed by outage > degraded > operational priority exactly as today (`history.BuildBuckets`) - this feature only adds data alongside that, never changes bucket status resolution.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Outage-interval reasons on the hourly bar | Decisão explícita do usuário. Outage already has its own mechanism (incident description/timeline via `dispatchOutageEnrichment`/`incidents.SetDescription`) - duplicating it into the bar would be a second, inconsistent source of truth for the same information. |
| Retroactive backfill of analysis text for degraded episodes that already happened before this feature ships | Same "history reflects what the system knew at the time" convention already used by `audit-log-expansion` and the poller `not_configured` fix this session - no migration rewrites old `status_intervals` rows. |
| A new HTTP endpoint | The existing hourly-history endpoint (`GET` serving `publicServiceResponse.History`, `internal/api/public_status_handler.go`) already returns per-bucket data for the same range the popover needs - extending its response shape is strictly less surface than adding a parallel endpoint. |
| Changing which providers can produce this text (Datadog vs a future New Relic connector, etc.) | `llm.AnalysisInput` (`internal/llm/prompts.go`) is already provider-agnostic (`ServiceName`/`SLOState`/`SLI`/`Target`/`Timeframe`/`ErrorBudgetRemaining`, never raw Datadog fields) - nothing here is Datadog-specific, so no connector work is implied or needed. |
| A generic reusable "Popover" design-system component beyond what this feature needs | Built for this one use case (click-triggered, positioned under a bar, list content); a fully generalized `Popover.tsx` API is not requested and would be speculative design. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Onde persistir o texto por episódio | `status_intervals` ganha coluna nova `analysis TEXT NULL`. `services.status_analysis` continua existindo e funcionando exatamente como hoje - dois mecanismos paralelos, nenhum lê/escreve o outro. | Decisão do usuário via `AskUserQuestion`: "Mantém os dois". | y |
| Race do enrichment assíncrono | `StatusIntervalRepository.OpenOrExtend` passa a retornar o ID do intervalo (novo ou estendido) que ficou aberto após a chamada. `dispatchDegradedEnrichment` captura esse ID no momento do dispatch (antes de disparar a goroutine) e a goroutine grava o texto gerado via `UPDATE status_intervals SET analysis = $1 WHERE id = $2` - por ID, nunca "o intervalo aberto agora" - então funciona corretamente mesmo se esse intervalo já tiver fechado (novo status) ou um segundo episódio já tiver começado antes da IA terminar. | Decisão do usuário via `AskUserQuestion`: "Só no intervalo que disparou". | y |
| Popover em bucket sem texto (pendente ou falho) | Clique sempre abre o popover em qualquer bucket com pelo menos 1 episódio degradado. Um episódio sem `analysis` (ainda gerando, ou geração falhou/expirou) aparece só com o range de horário, sem frase - nunca esconde a interatividade do bucket inteiro por causa de 1 episódio sem texto. | Decisão do usuário via `AskUserQuestion`: "Popover abre, mostra range sem motivo". | y |
| Múltiplos episódios no mesmo bucket | Lista todos os episódios que o bucket overlap, ordenados do mais recente pro mais antigo (mesma ordem que `status_intervals` naturalmente tem por `starts_at DESC`). Não há um limite de episódios por bucket - um bucket de 24h (range 90d) que teve muitos episódios curtos lista todos. | Decisão do usuário via `AskUserQuestion` (pergunta original desta feature). | y |
| Sem backfill retroativo | Episódios degradados anteriores a esta feature aparecem no bucket (contribuem pro status "degraded" do bucket, como já acontece hoje), mas sem `analysis` (coluna não existia, valor NULL) - tratados como "geração pendente/falha" pelo mesmo Assumption acima, não como erro. | Mesmo padrão já estabelecido nesta sessão (`audit-log-expansion`, poller `not_configured` fix): histórico reflete o que o sistema sabia na época, sem reescrita retroativa. | y |
| Mesmo endpoint, sem endpoint novo | `publicHistoryBucketResponse` (`internal/api/public_status_handler.go`) ganha um campo novo `episodes` (array, omitido/vazio quando o bucket não é degraded ou não tem nenhum episódio), em vez de um endpoint `GET` separado. | Investigação de código confirma que o endpoint atual já entrega os buckets pro range certo (24h/7d/30d/90d) - replicar isso num endpoint novo seria puro retrabalho. | y |
| Componente de popover novo no frontend | Novo componente (`web/src/components/ui/Popover.tsx` ou dentro do próprio `public-status`), usando `@radix-ui/react-popover` (dependência nova, mesma família do `@radix-ui/react-dialog` já usado por `Dialog.tsx`/`Drawer.tsx`) - dá foco/clique-fora/Escape acessíveis de graça, em vez de reimplementar isso à mão. `Tooltip.tsx` existente (CSS puro, hover-only, `pointer-events-none`, texto de 1 linha) não serve pra clique-e-fica-aberto com lista de itens - fica intocado, usado só pra ícones sem label visível como hoje. | Investigação de código: `Tooltip.tsx` read in full, confirmado incompatível com o requisito de interação (clique, não hover). `@radix-ui/react-dialog` já é dependência aprovada no projeto. | y |
| Escopo do popover: pop-over posicionado, não modal/drawer de tela cheia | Popover pequeno ancorado na barra clicada (como um dropdown), nunca um `Dialog`/`Drawer` de tela cheia - a página de status pública é visitor-facing, informação é curta (lista de frases + horários), não justifica um modal. | Consistente com o que "popover" significa no pedido original do usuário, e com o padrão de UI leve já usado nesta página (tooltips no hover do badge). | y |

**Open questions:** none — todas resolvidas ou registradas acima.

---

## User Stories

### P1: Episódio degradado grava seu próprio motivo, sobrevivendo à transição seguinte ⭐ MVP

**User Story**: Como sistema, quero que o texto de análise gerado pela IA no início de um episódio degradado fique associado a esse episódio específico (não à linha "atual" do serviço), para que o motivo não se perca quando o serviço sair do estado degradado ou entrar em um segundo episódio antes do primeiro texto terminar de gerar.

**Why P1**: Sem isso, não existe dado nenhum pra popover nenhum mostrar - é a fundação de toda a feature.

**Acceptance Criteria**:

1. **DEGINT-01**: The system SHALL have a nullable `analysis` text column on `status_intervals`, added via a new migration with a matching `.down.sql`.
2. **DEGINT-02**: WHEN `StatusIntervalRepository.OpenOrExtend` successfully opens a new interval OR extends the currently open one THEN the system SHALL return that interval's ID alongside its existing `error` return value.
3. **DEGINT-03**: WHEN a service transitions into `"degraded"` and `dispatchDegradedEnrichment` is invoked THEN the system SHALL capture the open interval's ID (from AC2) at dispatch time, before starting the detached goroutine.
4. **DEGINT-04**: WHEN the detached goroutine's LLM call succeeds and returns non-empty text THEN the system SHALL persist that text to the captured interval ID's `status_intervals.analysis` column (`UPDATE ... WHERE id = $1`) - never to whichever interval happens to be open at the time the goroutine finishes.
5. **DEGINT-05**: IF the interval captured in AC3 has already closed (a newer status transition happened) by the time the goroutine finishes THEN the system SHALL still persist the text to that same (now-closed) interval's row - a closed interval remains a valid `UPDATE` target by ID.
6. **DEGINT-06**: WHEN the detached goroutine's LLM call fails, times out, or returns empty text THEN the system SHALL leave that interval's `analysis` column NULL, exactly as `services.status_analysis` already does today for the same failure modes.
7. **DEGINT-07**: The system SHALL NOT alter any existing read/write of `services.status_analysis` - `HandleTransition`'s clear-on-transition behavior and the public status page's current-state badge tooltip stay exactly as they are today.

### P2: Histórico exposto por bucket na página de status pública

**User Story**: Como visitante da status page pública, quero que cada bucket do histórico (barra) que teve algum episódio degradado carregue o(s) motivo(s) daquele episódio, para que eu possa ver o motivo ao clicar.

**Why P2**: Depende de P1 já existir gravando o dado; é o que torna esse dado visível.

**Acceptance Criteria**:

8. **DEGINT-08**: WHEN `history.BuildBuckets` resolves a bucket's status THEN the system SHALL also attach, to that bucket, every interval with `Status == "degraded"` that overlaps the bucket's span - identical overlap rule already used for status resolution (an interval spanning multiple buckets contributes to every bucket it overlaps).
9. **DEGINT-09**: The system SHALL order a bucket's attached episodes most-recent-first (by `StartsAt` descending).
10. **DEGINT-10**: WHEN a bucket's resolved status is NOT `"degraded"` THEN the system SHALL attach zero episodes to it, even if a degraded interval technically overlaps a wider span that includes part of this bucket's time range but not the priority-winning status (matches AC in the sense that only degraded-status intervals ever contribute episodes, regardless of what "wins" priority in a bucket - clarify in Design if a degraded interval can overlap a bucket whose winning status is "outage": in that case the bucket is still attached its degraded episode(s) even though its displayed color is the outage's, since the popover's data source is "which degraded intervals overlap this bucket", independent of bucket-level color priority).
11. **DEGINT-11**: `publicHistoryBucketResponse` (the JSON DTO already served by the existing hourly-history endpoint) SHALL include an `episodes` array field, one entry per attached episode with its start time, end time (null if still open as of the response's `asOf` clamp), and analysis text (null if not yet generated/failed) - omitted or empty when the bucket has none.
12. **DEGINT-12**: The system SHALL NOT create any new HTTP endpoint - this data rides on the existing response already serving `History`.

### P3: Popover de clique na barra degradada

**User Story**: Como visitante da status page pública, quero clicar numa barra que teve degradação para ver o(s) motivo(s) daquele período, sem precisar de hover (funciona em touch/mobile) e sem o popover fechar sozinho.

**Why P3**: É a superfície visível do pedido original - sem isso as duas stories anteriores não têm efeito percebido pelo usuário final.

**Acceptance Criteria**:

13. **DEGINT-13**: WHEN a visitor clicks (or activates via keyboard) an hourly bar whose bucket has at least one attached episode THEN the system SHALL open a popover listing every episode's time range and analysis text (or a "sem motivo registrado" placeholder when the text is null), most-recent-first.
14. **DEGINT-14**: WHEN a visitor clicks outside the open popover, or presses Escape, or clicks the same bar again THEN the system SHALL close it.
15. **DEGINT-15**: WHEN an hourly bar's bucket has zero attached episodes (operational, outage, no_data, or a degraded bucket somehow carrying no episode data) THEN the system SHALL NOT make that bar interactive - it keeps its current hover-title-only behavior (`hourlyTooltip`), unchanged.
16. **DEGINT-16**: The popover SHALL be reachable and dismissible by keyboard alone (Tab to focus the bar, Enter/Space to open, Escape to close) - no interaction is mouse-only.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| DEGINT-01 | P1: Episódio degradado grava seu próprio motivo | - | Draft |
| DEGINT-02 | P1: Episódio degradado grava seu próprio motivo | - | Draft |
| DEGINT-03 | P1: Episódio degradado grava seu próprio motivo | - | Draft |
| DEGINT-04 | P1: Episódio degradado grava seu próprio motivo | - | Draft |
| DEGINT-05 | P1: Episódio degradado grava seu próprio motivo | - | Draft |
| DEGINT-06 | P1: Episódio degradado grava seu próprio motivo | - | Draft |
| DEGINT-07 | P1: Episódio degradado grava seu próprio motivo | - | Draft |
| DEGINT-08 | P2: Histórico exposto por bucket na página de status pública | - | Draft |
| DEGINT-09 | P2: Histórico exposto por bucket na página de status pública | - | Draft |
| DEGINT-10 | P2: Histórico exposto por bucket na página de status pública | - | Draft |
| DEGINT-11 | P2: Histórico exposto por bucket na página de status pública | - | Draft |
| DEGINT-12 | P2: Histórico exposto por bucket na página de status pública | - | Draft |
| DEGINT-13 | P3: Popover de clique na barra degradada | - | Draft |
| DEGINT-14 | P3: Popover de clique na barra degradada | - | Draft |
| DEGINT-15 | P3: Popover de clique na barra degradada | - | Draft |
| DEGINT-16 | P3: Popover de clique na barra degradada | - | Draft |
