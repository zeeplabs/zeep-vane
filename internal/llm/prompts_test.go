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
// non-alarmist, non-technical output in Portuguese.
func TestPromptBuilders_SystemPromptsAreShortFactualNonAlarmist(t *testing.T) {
	in := testAnalysisInput()
	builders := []func(AnalysisInput) (string, string){
		buildDegradedTooltipPrompt, buildOutageDescriptionPrompt, buildClosingCommentPrompt,
	}

	for _, build := range builders {
		systemPrompt, _ := build(in)
		if !containsAll(systemPrompt, "1-2 frases", "factual", "não alarmista", "sem conhecimento técnico") {
			t.Errorf("systemPrompt = %q, want it to instruct a short, factual, non-alarmist, non-technical response in Portuguese", systemPrompt)
		}
	}
}

// TestBuildDegradedTooltipPrompt_WithCause_IncludesCauseText covers RCA-06:
// a non-empty CauseType/CauseMessage must surface in the user prompt so the
// model can reflect the practical impact of the actual cause.
func TestBuildDegradedTooltipPrompt_WithCause_IncludesCauseText(t *testing.T) {
	in := testAnalysisInput()
	in.CauseType = "MongooseError"
	in.CauseMessage = "Operation buffering timed out after 10000ms"
	_, userPrompt := buildDegradedTooltipPrompt(in)

	if !containsAll(userPrompt, in.CauseType, in.CauseMessage) {
		t.Errorf("userPrompt = %q, want it to include cause type %q and cause message %q", userPrompt, in.CauseType, in.CauseMessage)
	}
}

// TestBuildDegradedTooltipPrompt_WithoutCause_IdenticalToBaseline covers
// RCA-06's "no behavior change when the cause fields are empty" half: the
// default zero-value AnalysisInput (every existing caller/test) must
// produce byte-identical output to before this feature existed.
func TestBuildDegradedTooltipPrompt_WithoutCause_IdenticalToBaseline(t *testing.T) {
	in := testAnalysisInput()
	_, withEmptyCause := buildDegradedTooltipPrompt(in)

	in.CauseType = "MongooseError"
	_, withOnlyType := buildDegradedTooltipPrompt(in)

	if withEmptyCause != withOnlyType {
		t.Errorf("userPrompt changed with only CauseType set (CauseMessage still empty): got %q, want unchanged %q", withOnlyType, withEmptyCause)
	}
	if strings.Contains(withEmptyCause, "Causa técnica identificada") {
		t.Errorf("userPrompt = %q, want no cause sentence when both cause fields are empty", withEmptyCause)
	}
}

// TestBuildOutageDescriptionPrompt_WithCause_IncludesCauseText mirrors
// TestBuildDegradedTooltipPrompt_WithCause_IncludesCauseText for the outage
// builder (RCA-06 applies to both degraded and outage, not closing).
func TestBuildOutageDescriptionPrompt_WithCause_IncludesCauseText(t *testing.T) {
	in := testAnalysisInput()
	in.CauseType = "MongooseError"
	in.CauseMessage = "Operation buffering timed out after 10000ms"
	_, userPrompt := buildOutageDescriptionPrompt(in)

	if !containsAll(userPrompt, in.CauseType, in.CauseMessage) {
		t.Errorf("userPrompt = %q, want it to include cause type %q and cause message %q", userPrompt, in.CauseType, in.CauseMessage)
	}
}

// TestBuildOutageDescriptionPrompt_WithoutCause_IdenticalToBaseline mirrors
// TestBuildDegradedTooltipPrompt_WithoutCause_IdenticalToBaseline for the
// outage builder.
func TestBuildOutageDescriptionPrompt_WithoutCause_IdenticalToBaseline(t *testing.T) {
	in := testAnalysisInput()
	_, withEmptyCause := buildOutageDescriptionPrompt(in)

	in.CauseMessage = "Operation buffering timed out after 10000ms"
	_, withOnlyMessage := buildOutageDescriptionPrompt(in)

	if withEmptyCause != withOnlyMessage {
		t.Errorf("userPrompt changed with only CauseMessage set (CauseType still empty): got %q, want unchanged %q", withOnlyMessage, withEmptyCause)
	}
	if strings.Contains(withEmptyCause, "Causa técnica identificada") {
		t.Errorf("userPrompt = %q, want no cause sentence when both cause fields are empty", withEmptyCause)
	}
}

// TestBuildClosingCommentPrompt_IgnoresCauseFields guards RCA-06's scope
// boundary: the closing-comment builder must never surface cause data, even
// when the caller populated it (design.md scopes cause data to
// degraded/outage only).
func TestBuildClosingCommentPrompt_IgnoresCauseFields(t *testing.T) {
	in := testAnalysisInput()
	in.CauseType = "MongooseError"
	in.CauseMessage = "Operation buffering timed out after 10000ms"
	_, userPrompt := buildClosingCommentPrompt(in)

	if strings.Contains(userPrompt, in.CauseType) || strings.Contains(userPrompt, in.CauseMessage) {
		t.Errorf("userPrompt = %q, want it to never include cause data (closing-comment builder is out of RCA-06's scope)", userPrompt)
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
