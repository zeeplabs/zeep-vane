package llm

import (
	"strings"
	"testing"
)

func testAnalysisInput() AnalysisInput {
	return AnalysisInput{
		ServiceName:          "checkout-api",
		SLOState:             "breached",
		SLI:                  0.982,
		Target:               0.995,
		Timeframe:            "30d",
		ErrorBudgetRemaining: -12.5,
	}
}

// TestBuildDegradedTooltipPrompt_IncludesServiceNameAndSLOState guards
// against silently dropping a field: the prompt is useless to the model
// without both.
func TestBuildDegradedTooltipPrompt_IncludesServiceNameAndSLOState(t *testing.T) {
	in := testAnalysisInput()
	systemPrompt, userPrompt := buildDegradedTooltipPrompt(in)

	if systemPrompt == "" {
		t.Fatal("systemPrompt is empty")
	}
	if !containsAll(userPrompt, in.ServiceName, in.SLOState) {
		t.Errorf("userPrompt = %q, want it to include service name %q and SLO state %q", userPrompt, in.ServiceName, in.SLOState)
	}
}

func TestBuildOutageDescriptionPrompt_IncludesServiceNameAndSLOState(t *testing.T) {
	in := testAnalysisInput()
	systemPrompt, userPrompt := buildOutageDescriptionPrompt(in)

	if systemPrompt == "" {
		t.Fatal("systemPrompt is empty")
	}
	if !containsAll(userPrompt, in.ServiceName, in.SLOState) {
		t.Errorf("userPrompt = %q, want it to include service name %q and SLO state %q", userPrompt, in.ServiceName, in.SLOState)
	}
}

func TestBuildClosingCommentPrompt_IncludesServiceNameAndSLOState(t *testing.T) {
	in := testAnalysisInput()
	systemPrompt, userPrompt := buildClosingCommentPrompt(in)

	if systemPrompt == "" {
		t.Fatal("systemPrompt is empty")
	}
	if !containsAll(userPrompt, in.ServiceName, in.SLOState) {
		t.Errorf("userPrompt = %q, want it to include service name %q and SLO state %q", userPrompt, in.ServiceName, in.SLOState)
	}
}

// TestPromptBuilders_SystemPromptsAreShortFactualNonAlarmist confirms all
// three builders share the same instruction, per design's requirement
// that the system prompt instructs a short (1-2 sentence), factual,
// non-alarmist output.
func TestPromptBuilders_SystemPromptsAreShortFactualNonAlarmist(t *testing.T) {
	in := testAnalysisInput()
	builders := []func(AnalysisInput) (string, string){
		buildDegradedTooltipPrompt, buildOutageDescriptionPrompt, buildClosingCommentPrompt,
	}

	for _, build := range builders {
		systemPrompt, _ := build(in)
		if !containsAll(systemPrompt, "1-2 sentence", "factual", "non-alarmist") {
			t.Errorf("systemPrompt = %q, want it to instruct a short, factual, non-alarmist response", systemPrompt)
		}
	}
}

func containsAll(haystack string, needles ...string) bool {
	for _, n := range needles {
		if !strings.Contains(haystack, n) {
			return false
		}
	}
	return true
}
