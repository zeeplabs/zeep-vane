package llm

import "fmt"

// AnalysisInput is the SLO/incident context handed to each prompt builder,
// built directly from db.Service + datadog.SLOStatus by the caller
// (SLOAnalyzer) - no new external call is made to gather it.
type AnalysisInput struct {
	ServiceName          string
	SLOState             string
	SLI                  float64
	Target               float64
	Timeframe            string
	ErrorBudgetRemaining float64
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
			"SLI atual: %.4f. Meta: %.4f. Período: %s. Error budget restante: %.2f%%. "+
			"Escreva um tooltip curto explicando o estado degradado para quem visita a página de status.",
		in.ServiceName, in.SLOState, in.SLI, in.Target, in.Timeframe, in.ErrorBudgetRemaining,
	)
	return analysisSystemPrompt, userPrompt
}

// buildOutageDescriptionPrompt builds the prompt for an incident's
// description when a service transitions into an outage (breached SLO).
func buildOutageDescriptionPrompt(in AnalysisInput) (systemPrompt, userPrompt string) {
	userPrompt = fmt.Sprintf(
		"O serviço %q acabou de entrar em indisponibilidade. Estado do SLO: %s. "+
			"SLI atual: %.4f. Meta: %.4f. Período: %s. Error budget restante: %.2f%%. "+
			"Escreva uma descrição curta do incidente explicando o que está acontecendo para quem visita a página de status.",
		in.ServiceName, in.SLOState, in.SLI, in.Target, in.Timeframe, in.ErrorBudgetRemaining,
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
