package llm

import "fmt"

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

// analysisSystemPrompt is shared by all three builders: it instructs a
// short, factual, non-alarmist response in plain Portuguese aimed at a
// non-technical reader, since every caller (a status page tooltip, an
// incident description, a closing comment) is visitor-facing text read by
// people outside engineering (HR, managers) - jargon or an alarmist/verbose
// response would be actively worse than no analysis at all.
const analysisSystemPrompt = "Você está escrevendo uma atualização curta e factual para uma página pública de status, " +
	"em português do Brasil, para um leitor sem conhecimento técnico (ex: RH, gestor). " +
	"Responda em 1-2 frases, em tom calmo e não alarmista, evitando jargão técnico (não use termos como " +
	"\"SLO\", \"SLI\", \"error budget\", \"latência\" ou nomes internos de serviço) - descreva o impacto prático " +
	"para quem está usando o produto. Não especule sobre causas que você não pode confirmar a partir dos dados " +
	"fornecidos, e não use pontos de exclamação ou linguagem dramática."

// buildDegradedTooltipPrompt builds the prompt for the short tooltip shown
// when a service's SLO is degraded (approaching its error budget limit but
// not yet breached).
func buildDegradedTooltipPrompt(in AnalysisInput) (systemPrompt, userPrompt string) {
	userPrompt = fmt.Sprintf(
		"O serviço %q está atualmente em estado degradado. Estado do SLO: %s. "+
			"SLI atual: %.4f. Meta: %.4f. Período: %s. Error budget restante: %.2f%%.%s "+
			"Escreva um tooltip curto explicando o estado degradado para quem visita a página de status.",
		in.ServiceName, in.SLOState, in.SLI, in.Target, in.Timeframe, in.ErrorBudgetRemaining, causeSentence(in),
	)
	return analysisSystemPrompt, userPrompt
}

// buildOutageDescriptionPrompt builds the prompt for an incident's
// description when a service transitions into an outage (breached SLO).
func buildOutageDescriptionPrompt(in AnalysisInput) (systemPrompt, userPrompt string) {
	userPrompt = fmt.Sprintf(
		"O serviço %q acabou de entrar em indisponibilidade. Estado do SLO: %s. "+
			"SLI atual: %.4f. Meta: %.4f. Período: %s. Error budget restante: %.2f%%.%s "+
			"Escreva uma descrição curta do incidente explicando o que está acontecendo para quem visita a página de status.",
		in.ServiceName, in.SLOState, in.SLI, in.Target, in.Timeframe, in.ErrorBudgetRemaining, causeSentence(in),
	)
	return analysisSystemPrompt, userPrompt
}

// buildClosingCommentPrompt builds the prompt proposing a closing comment
// when a service recovers (returns to operational) while an incident is
// still open for it.
func buildClosingCommentPrompt(in AnalysisInput) (systemPrompt, userPrompt string) {
	userPrompt = fmt.Sprintf(
		"O serviço %q se recuperou e voltou ao estado operacional. Estado do SLO: %s. "+
			"SLI atual: %.4f. Meta: %.4f. Período: %s. Error budget restante: %.2f%%. "+
			"Escreva um comentário curto de encerramento do incidente, confirmando a recuperação para quem visita a página de status.",
		in.ServiceName, in.SLOState, in.SLI, in.Target, in.Timeframe, in.ErrorBudgetRemaining,
	)
	return analysisSystemPrompt, userPrompt
}
