package llm

import (
	"fmt"
	"math"
)

// AnalysisInput is the SLO/incident context handed to each prompt builder,
// built by the caller (SLOAnalyzer) from db.Service + datadog.SLOStatus.
// internal/llm itself still makes no external call to gather any of this -
// but for a degraded/outage transition with root-cause enrichment enabled,
// SLOAnalyzer may populate CauseType/CauseMessage from a Datadog Error
// Tracking lookup it makes itself before calling in (slo-root-cause-
// enrichment RCA-06), which this package's own Generate* methods never
// trigger.
type AnalysisInput struct {
	ServiceName          string
	SLOState             string
	SLI                  float64
	Target               float64
	Timeframe            string
	ErrorBudgetRemaining float64
	// CauseType/CauseMessage carry a Datadog Error Tracking issue's
	// error_type/error_message (datadog.CauseHint). Both "" (the default,
	// and every existing caller/test) unless the caller opted into and
	// found root-cause enrichment data for a degraded/outage transition
	// (RCA-01..04); buildClosingCommentPrompt ignores both fields.
	CauseType    string
	CauseMessage string
	// BurnRateShort/BurnRateLong are the cycle's 5-minute/1-hour burn
	// rates (how many times faster than the SLO's error budget allows the
	// service is failing), and WindowErrors/WindowRequests the 5-minute
	// window's counts (poller-burn-rate-classification BURN-15). All zero
	// when the transition came from the state-based fallback (no counts);
	// burnSentence then adds nothing. A burn rate is +Inf when the target
	// is 100% and the window had errors.
	BurnRateShort  float64
	BurnRateLong   float64
	WindowErrors   int64
	WindowRequests int64
}

// causeSentence returns the extra user-prompt sentence surfacing in's cause
// data, or "" when either field is empty. This is RCA-06's "extend the user
// prompt" reading of "allow the model to reflect the practical impact of
// the cause" (design.md's Integration Points, T9's Reuses note): the system
// prompt already forbids speculating beyond the data provided, so folding
// the cause into the user prompt is what lets the model use it, without
// needing to reword analysisSystemPrompt's jargon/tone constraints.
func causeSentence(in AnalysisInput) string {
	if in.CauseType == "" || in.CauseMessage == "" {
		return ""
	}
	return fmt.Sprintf(" Causa técnica identificada: %s - %s.", in.CauseType, in.CauseMessage)
}

// burnSentence returns the extra user-prompt sentence surfacing in's
// burn-rate numbers (BURN-15), or "" when the 5-minute window had no
// requests (no reading, or no traffic to describe). Same "optional extra
// sentence" pattern as causeSentence: the numbers go into the user prompt
// in plain words - "margem de falhas permitida", never "burn rate"/"error
// budget" - so the model can cite them under analysisSystemPrompt's
// unchanged vocabulary rules. A burn rate of +Inf (100% target) never
// reaches the text: the multiplier is replaced by saying the target
// tolerates no failures.
func burnSentence(in AnalysisInput) string {
	if in.WindowRequests <= 0 {
		return ""
	}
	errs := min(max(in.WindowErrors, 0), in.WindowRequests)
	if errs == 0 {
		return fmt.Sprintf(" Nos últimos 5 minutos, nenhuma das %d requisições falhou.", in.WindowRequests)
	}

	sentence := fmt.Sprintf(" Nos últimos 5 minutos, %d de %d requisições falharam (taxa de erro de %.1f%%).",
		errs, in.WindowRequests, 100*float64(errs)/float64(in.WindowRequests))
	if math.IsInf(in.BurnRateShort, 0) || math.IsInf(in.BurnRateLong, 0) ||
		math.IsNaN(in.BurnRateShort) || math.IsNaN(in.BurnRateLong) {
		return sentence + " A meta deste serviço não tolera nenhuma falha."
	}
	return sentence + fmt.Sprintf(" Nesse ritmo, as falhas consomem a margem de falhas permitida %.1f vezes mais rápido "+
		"que o aceitável nos últimos 5 minutos, e %.1f vezes na última hora.", in.BurnRateShort, in.BurnRateLong)
}

// analysisSystemPrompt is shared by all three builders (2026-10-07
// feedback): the fully jargon-free wording read as too vague to be useful -
// "algo está lento" with no detail at all. It still targets a non-technical
// reader and still bans the internal SLO/SLI/error-budget vocabulary
// (meaningless outside engineering), but now allows a plain-language
// technical term (response time, error rate, intermittent slowness) when it
// helps describe what's actually happening, as long as the model explains
// its practical effect right after using it - a reader should come away
// understanding *what kind* of problem this is, not just that "something"
// is wrong.
const analysisSystemPrompt = "Você está escrevendo uma atualização curta para uma página pública de status, " +
	"em português do Brasil, para um leitor sem conhecimento técnico (ex: RH, gestor). " +
	"Responda em 1-2 frases, em tom calmo e factual, não alarmista. Pode usar um termo técnico simples quando " +
	"ajudar a explicar o que está acontecendo (ex: \"tempo de resposta\", \"taxa de erro\", \"lentidão intermitente\"), " +
	"mas sempre explique o efeito prático logo em seguida - nunca use as siglas internas \"SLO\", \"SLI\" ou " +
	"\"error budget\", nem nomes internos de serviço. Não especule sobre causas que você não pode confirmar a " +
	"partir dos dados fornecidos, e não use pontos de exclamação ou linguagem dramática."

// buildDegradedTooltipPrompt builds the prompt for the short tooltip shown
// when a service's SLO is degraded (approaching its error budget limit but
// not yet breached).
func buildDegradedTooltipPrompt(in AnalysisInput) (systemPrompt, userPrompt string) {
	userPrompt = fmt.Sprintf(
		"O serviço %q está atualmente em estado degradado. Estado do SLO: %s. "+
			"SLI atual: %.4f. Meta: %.4f. Período: %s. Error budget restante: %.2f%%.%s%s "+
			"Escreva um tooltip curto explicando o estado degradado para quem visita a página de status.",
		in.ServiceName, in.SLOState, in.SLI, in.Target, in.Timeframe, in.ErrorBudgetRemaining, causeSentence(in), burnSentence(in),
	)
	return analysisSystemPrompt, userPrompt
}

// buildOutageDescriptionPrompt builds the prompt for an incident's
// description when a service transitions into an outage (breached SLO).
func buildOutageDescriptionPrompt(in AnalysisInput) (systemPrompt, userPrompt string) {
	userPrompt = fmt.Sprintf(
		"O serviço %q acabou de entrar em indisponibilidade. Estado do SLO: %s. "+
			"SLI atual: %.4f. Meta: %.4f. Período: %s. Error budget restante: %.2f%%.%s%s "+
			"Escreva uma descrição curta do incidente explicando o que está acontecendo para quem visita a página de status.",
		in.ServiceName, in.SLOState, in.SLI, in.Target, in.Timeframe, in.ErrorBudgetRemaining, causeSentence(in), burnSentence(in),
	)
	return analysisSystemPrompt, userPrompt
}

// buildClosingCommentPrompt builds the prompt proposing a closing comment
// when a service recovers (returns to operational) while an incident is
// still open for it.
func buildClosingCommentPrompt(in AnalysisInput) (systemPrompt, userPrompt string) {
	userPrompt = fmt.Sprintf(
		"O serviço %q se recuperou e voltou ao estado operacional. Estado do SLO: %s. "+
			"SLI atual: %.4f. Meta: %.4f. Período: %s. Error budget restante: %.2f%%.%s "+
			"Escreva um comentário curto de encerramento do incidente, confirmando a recuperação para quem visita a página de status.",
		in.ServiceName, in.SLOState, in.SLI, in.Target, in.Timeframe, in.ErrorBudgetRemaining, burnSentence(in),
	)
	return analysisSystemPrompt, userPrompt
}
