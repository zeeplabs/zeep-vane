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
// short, factual, non-alarmist response, since every caller (a status
// page tooltip, an incident description, a closing comment) is
// visitor-facing text where an alarmist or verbose response would be
// actively worse than no analysis at all.
const analysisSystemPrompt = "You are writing a short, factual status update for a public status page. " +
	"Respond in 1-2 sentences, in a calm and non-alarmist tone. Do not speculate about causes you cannot " +
	"confirm from the data given, and do not use exclamation points or dramatic language."

// buildDegradedTooltipPrompt builds the prompt for the short tooltip shown
// when a service's SLO is degraded (approaching its error budget limit but
// not yet breached).
func buildDegradedTooltipPrompt(in AnalysisInput) (systemPrompt, userPrompt string) {
	userPrompt = fmt.Sprintf(
		"Service %q is currently in a degraded state. SLO state: %s. "+
			"Current SLI: %.4f. Target: %.4f. Timeframe: %s. Error budget remaining: %.2f%%. "+
			"Write a short tooltip explaining the degraded state to a status page visitor.",
		in.ServiceName, in.SLOState, in.SLI, in.Target, in.Timeframe, in.ErrorBudgetRemaining,
	)
	return analysisSystemPrompt, userPrompt
}

// buildOutageDescriptionPrompt builds the prompt for an incident's
// description when a service transitions into an outage (breached SLO).
func buildOutageDescriptionPrompt(in AnalysisInput) (systemPrompt, userPrompt string) {
	userPrompt = fmt.Sprintf(
		"Service %q just transitioned to an outage. SLO state: %s. "+
			"Current SLI: %.4f. Target: %.4f. Timeframe: %s. Error budget remaining: %.2f%%. "+
			"Write a short incident description explaining what is happening to a status page visitor.",
		in.ServiceName, in.SLOState, in.SLI, in.Target, in.Timeframe, in.ErrorBudgetRemaining,
	)
	return analysisSystemPrompt, userPrompt
}

// buildClosingCommentPrompt builds the prompt proposing a closing comment
// when a service recovers (returns to operational) while an incident is
// still open for it.
func buildClosingCommentPrompt(in AnalysisInput) (systemPrompt, userPrompt string) {
	userPrompt = fmt.Sprintf(
		"Service %q has recovered and returned to an operational state. SLO state: %s. "+
			"Current SLI: %.4f. Target: %.4f. Timeframe: %s. Error budget remaining: %.2f%%. "+
			"Write a short closing comment for the open incident, confirming the recovery to a status page visitor.",
		in.ServiceName, in.SLOState, in.SLI, in.Target, in.Timeframe, in.ErrorBudgetRemaining,
	)
	return analysisSystemPrompt, userPrompt
}
