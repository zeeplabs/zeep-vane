package llm

import (
	"math"
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

// preBurnSystemPrompt is analysisSystemPrompt exactly as it was before
// BURN-15, pinned so adding the burn numbers provably leaves the system
// prompt (tone and vocabulary rules) byte-identical.
const preBurnSystemPrompt = "Você está escrevendo uma atualização curta para uma página pública de status, " +
	"em português do Brasil, para um leitor sem conhecimento técnico (ex: RH, gestor). " +
	"Responda em 1-2 frases, em tom calmo e factual, não alarmista. Pode usar um termo técnico simples quando " +
	"ajudar a explicar o que está acontecendo (ex: \"tempo de resposta\", \"taxa de erro\", \"lentidão intermitente\"), " +
	"mas sempre explique o efeito prático logo em seguida - nunca use as siglas internas \"SLO\", \"SLI\" ou " +
	"\"error budget\", nem nomes internos de serviço. Não especule sobre causas que você não pode confirmar a " +
	"partir dos dados fornecidos, e não use pontos de exclamação ou linguagem dramática."

func burnTestInput() AnalysisInput {
	in := testAnalysisInput()
	in.BurnRateShort = 40
	in.BurnRateLong = 18.5
	in.WindowErrors = 20
	in.WindowRequests = 100
	return in
}

var promptBuilders = map[string]func(AnalysisInput) (string, string){
	"degraded tooltip":   buildDegradedTooltipPrompt,
	"outage description": buildOutageDescriptionPrompt,
	"closing comment":    buildClosingCommentPrompt,
}

// TestPromptBuilders_WithBurnNumbers_IncludeBurnSentence covers BURN-15:
// each of the three user prompts carries the cycle's error rate and burn
// multipliers in plain words.
func TestPromptBuilders_WithBurnNumbers_IncludeBurnSentence(t *testing.T) {
	for name, build := range promptBuilders {
		t.Run(name, func(t *testing.T) {
			_, userPrompt := build(burnTestInput())
			want := " Nos últimos 5 minutos, 20 de 100 requisições falharam (taxa de erro de 20.0%). " +
				"Nesse ritmo, as falhas consomem a margem de falhas permitida 40.0 vezes mais rápido " +
				"que o aceitável nos últimos 5 minutos, e 18.5 vezes na última hora."
			if !strings.Contains(userPrompt, want) {
				t.Errorf("userPrompt = %q, want it to contain %q", userPrompt, want)
			}
		})
	}
}

// TestPromptBuilders_WithoutWindowRequests_IdenticalToBaseline covers the
// "only when WindowRequests > 0" half: no requests means no sentence, even
// when the burn fields carry values, and the prompt is byte-identical to a
// zero-value input's.
func TestPromptBuilders_WithoutWindowRequests_IdenticalToBaseline(t *testing.T) {
	for name, build := range promptBuilders {
		t.Run(name, func(t *testing.T) {
			_, baseline := build(testAnalysisInput())
			in := burnTestInput()
			in.WindowRequests = 0
			_, userPrompt := build(in)

			if userPrompt != baseline {
				t.Errorf("userPrompt = %q, want unchanged %q", userPrompt, baseline)
			}
			if strings.Contains(userPrompt, "Nos últimos 5 minutos") {
				t.Errorf("userPrompt = %q, want no burn sentence without requests", userPrompt)
			}
		})
	}
}

// TestPromptBuilders_InfiniteBurn_NeverPrintsInf covers a 100% target: the
// burn rate is +Inf, which must never reach the text.
func TestPromptBuilders_InfiniteBurn_NeverPrintsInf(t *testing.T) {
	for name, build := range promptBuilders {
		t.Run(name, func(t *testing.T) {
			in := burnTestInput()
			in.BurnRateShort = math.Inf(1)
			in.BurnRateLong = math.Inf(1)
			_, userPrompt := build(in)

			if strings.Contains(userPrompt, "Inf") {
				t.Errorf("userPrompt = %q, want no \"Inf\" text", userPrompt)
			}
			if !containsAll(userPrompt, "20 de 100 requisições falharam", "não tolera nenhuma falha") {
				t.Errorf("userPrompt = %q, want the error count and the no-tolerance sentence", userPrompt)
			}
			if strings.Contains(userPrompt, "vezes mais rápido") {
				t.Errorf("userPrompt = %q, want the multiplier omitted", userPrompt)
			}
		})
	}
}

func TestBurnSentence_NoErrorsInWindow(t *testing.T) {
	in := burnTestInput()
	in.WindowErrors = 0
	in.BurnRateShort = 0

	got := burnSentence(in)
	if want := " Nos últimos 5 minutos, nenhuma das 100 requisições falhou."; got != want {
		t.Errorf("burnSentence() = %q, want %q", got, want)
	}
}

// TestPromptBuilders_SystemPromptUnchangedByBurnNumbers pins every
// builder's system prompt to its pre-BURN-15 text, with and without burn
// numbers.
func TestPromptBuilders_SystemPromptUnchangedByBurnNumbers(t *testing.T) {
	for name, build := range promptBuilders {
		t.Run(name, func(t *testing.T) {
			for _, in := range []AnalysisInput{testAnalysisInput(), burnTestInput()} {
				if systemPrompt, _ := build(in); systemPrompt != preBurnSystemPrompt {
					t.Errorf("systemPrompt = %q, want byte-identical to the pre-BURN-15 prompt %q", systemPrompt, preBurnSystemPrompt)
				}
			}
		})
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

// TestPromptBuilders_DefaultUserPromptsUnchanged pins the full user prompt
// each builder produced before NoRecentTraffic existed, so moving the SLO
// fields into sloContext provably leaves every existing prompt
// byte-identical.
func TestPromptBuilders_DefaultUserPromptsUnchanged(t *testing.T) {
	want := map[string]string{
		"degraded tooltip": "O serviço \"checkout-api\" está atualmente em estado degradado. Estado do SLO: breached. SLI atual: 0.9820. " +
			"Meta: 0.9950. Período: 30d. Error budget restante: -12.50%. " +
			"Escreva um tooltip curto explicando o estado degradado para quem visita a página de status.",
		"outage description": "O serviço \"checkout-api\" acabou de entrar em indisponibilidade. Estado do SLO: breached. SLI atual: 0.9820. " +
			"Meta: 0.9950. Período: 30d. Error budget restante: -12.50%. " +
			"Escreva uma descrição curta do incidente explicando o que está acontecendo para quem visita a página de status.",
		"closing comment": "O serviço \"checkout-api\" se recuperou e voltou ao estado operacional. Estado do SLO: breached. SLI atual: 0.9820. " +
			"Meta: 0.9950. Período: 30d. Error budget restante: -12.50%. " +
			"Escreva um comentário curto de encerramento do incidente, confirmando a recuperação para quem visita a página de status.",
	}
	for name, build := range promptBuilders {
		t.Run(name, func(t *testing.T) {
			if _, userPrompt := build(testAnalysisInput()); userPrompt != want[name] {
				t.Errorf("userPrompt = %q, want byte-identical %q", userPrompt, want[name])
			}
		})
	}
}

// noTrafficTestInput is a burn-path input whose 5-minute window was empty:
// SLOState/SLI/ErrorBudgetRemaining hold the hour's Datadog figures, as
// pollService passes them on a recovery after the zero-traffic grace.
func noTrafficTestInput() AnalysisInput {
	in := testAnalysisInput()
	in.NoRecentTraffic = true
	in.BurnRateLong = 16
	return in
}

// TestPromptBuilders_NoRecentTraffic_OmitsHourStateAndSaysNoTraffic covers
// the pre-release review of BURN-15: with an empty 5-minute window, no
// builder presents the hour's Datadog state/SLI/budget as current; each
// keeps the target and timeframe, says there was no traffic, and labels
// the 1-hour burn as the last hour.
func TestPromptBuilders_NoRecentTraffic_OmitsHourStateAndSaysNoTraffic(t *testing.T) {
	for name, build := range promptBuilders {
		t.Run(name, func(t *testing.T) {
			_, userPrompt := build(noTrafficTestInput())

			for _, stale := range []string{"Estado do SLO", "breached", "SLI atual", "0.9820", "Error budget restante", "-12.50"} {
				if strings.Contains(userPrompt, stale) {
					t.Errorf("userPrompt = %q, want no %q (the hour's figures, not the current state)", userPrompt, stale)
				}
			}
			want := "Meta: 0.9950. Período: 30d. Nos últimos 5 minutos, o serviço não recebeu requisições, " +
				"portanto não há medição recente de falhas. Na última hora, as falhas consumiram a margem de falhas " +
				"permitida 16.0 vezes mais rápido que o aceitável."
			if !strings.Contains(userPrompt, want) {
				t.Errorf("userPrompt = %q, want it to contain %q", userPrompt, want)
			}
		})
	}
}

func TestBurnSentence_NoRecentTraffic(t *testing.T) {
	tests := []struct {
		name      string
		burnLong  float64
		wantExtra string
	}{
		{name: "no failures in the last hour", burnLong: 0, wantExtra: ""},
		{name: "failures in the last hour", burnLong: 2.5, wantExtra: " Na última hora, as falhas consumiram a margem de falhas permitida 2.5 vezes mais rápido que o aceitável."},
		{name: "100% target with failures", burnLong: math.Inf(1), wantExtra: " Houve falhas na última hora, e a meta deste serviço não tolera nenhuma falha."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := testAnalysisInput()
			in.NoRecentTraffic = true
			in.BurnRateLong = tt.burnLong
			want := " Nos últimos 5 minutos, o serviço não recebeu requisições, portanto não há medição recente de falhas." + tt.wantExtra
			if got := burnSentence(in); got != want {
				t.Errorf("burnSentence() = %q, want %q", got, want)
			}
		})
	}
}

// TestPromptBuilders_NoRecentTraffic_SystemPromptUnchanged extends the
// system-prompt pin to the no-traffic input.
func TestPromptBuilders_NoRecentTraffic_SystemPromptUnchanged(t *testing.T) {
	for name, build := range promptBuilders {
		t.Run(name, func(t *testing.T) {
			if systemPrompt, _ := build(noTrafficTestInput()); systemPrompt != preBurnSystemPrompt {
				t.Errorf("systemPrompt = %q, want byte-identical %q", systemPrompt, preBurnSystemPrompt)
			}
		})
	}
}
