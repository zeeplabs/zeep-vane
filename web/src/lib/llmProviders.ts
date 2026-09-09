// Shape of the LLM provider integration's data (design.md § Data Models,
// mirrors internal/api/llm_providers_handler.go exactly). Sibling of
// features/email-providers/hooks.ts's types - kept in web/src/lib per
// tasks.md T20's Where field, since both PublicStatusPage.tsx (T22) and
// the incidents dashboard (T23) also need the provider-name/model shape
// independent of the settings hook.
export type LLMProviderName = "openai";

// modelAllowlist mirrors internal/llm/service.go's modelAllowlist exactly -
// the fixed set of models Connect/SetModel will accept for "openai". Kept
// here (not fabricated ad hoc in AISettings.tsx) so the model dropdown
// can never drift from the backend's own allowlist.
export const modelAllowlist: Record<LLMProviderName, string[]> = {
  openai: ["gpt-4o-mini", "gpt-4o", "gpt-4.1-mini", "gpt-4.1"],
};

// Mirrors LLMProvidersHandler.List's per-provider response shape
// (internal/api/llm_providers_handler.go's llmProviderResponse) - no
// api_key field ever modeled client-side (AI-01 AC5-equivalent).
export interface LLMProviderStatus {
  provider: LLMProviderName;
  model: string;
  status: "connected" | "invalid";
  last_checked_at: string | null;
  last_error: string | null;
}

// Mirrors LLMProvidersHandler.List's listLLMProvidersResponse - like
// email-providers' EmailProvidersResponse, active_provider/providers sit
// alongside the pagination fields rather than nesting under a generic
// Page<T> envelope (AGENTS.md §4's PAG-08 precedent).
export interface LLMProvidersResponse {
  active_provider: LLMProviderName | null;
  providers: LLMProviderStatus[];
  total: number;
  page: number;
  page_size: number;
}

export interface ConnectLLMProviderInput {
  api_key: string;
  model?: string;
}
