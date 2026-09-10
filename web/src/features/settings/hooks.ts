import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFetch } from "../../lib/apiClient";
import type { CompanySettings, TaxIDType } from "../../types/api";
import type { ConnectLLMProviderInput, LLMProviderName, LLMProvidersResponse } from "../../lib/llmProviders";

export function useCompanySettings() {
  return useQuery({
    queryKey: ["company-settings"],
    queryFn: () => apiFetch<CompanySettings>("/api/company-settings"),
  });
}

export interface UpdateCompanySettingsInput {
  name: string;
  contact_email: string;
  // legal_name/tax_id/tax_id_type are all optional (TENANT-22) - omitting
  // a key leaves it unchanged server-side (db.TenantUpdate's own "nil = no
  // change" semantics), so this only sends them when the caller (T19's
  // form) actually provides a value.
  legal_name?: string;
  tax_id?: string;
  tax_id_type?: TaxIDType;
}

export function useUpdateCompanySettings() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: UpdateCompanySettingsInput) =>
      apiFetch<CompanySettings>("/api/company-settings", {
        method: "PATCH",
        body: JSON.stringify(input),
      }),
    onSuccess: (data) => {
      queryClient.setQueryData(["company-settings"], data);
    },
  });
}

// useUploadCompanyLogo posts the logo file as multipart/form-data to the
// dedicated upload endpoint (SET-07), immediately and independently of the
// name/e-mail PATCH above - the logo is no longer sent as a data: URL
// inside UpdateCompanySettingsInput.
export function useUploadCompanyLogo() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (file: File) => {
      const formData = new FormData();
      formData.append("logo", file);
      return apiFetch<CompanySettings>("/api/company-settings/logo", {
        method: "POST",
        body: formData,
      });
    },
    onSuccess: (data) => {
      queryClient.setQueryData(["company-settings"], data);
    },
  });
}

// useLLMProviders/useConnect.../useSetModel.../useActivate... follow the
// same shape as email-providers/hooks.ts's useEmailProviders family
// (T20's direct template) - queryKey includes the page number per
// AGENTS.md §5, and every mutation invalidates the ["integrations", "llm"]
// list on success.
export function useLLMProviders(page: number) {
  return useQuery({
    queryKey: ["integrations", "llm", page],
    queryFn: () => apiFetch<LLMProvidersResponse>(`/api/integrations/llm?page=${page}`),
  });
}

// The real backend's POST /api/integrations/llm/{provider} only ever
// returns {"status":"connected"} (mirrors ConnectEmailProviderResponse -
// never api_key back).
interface ConnectLLMProviderResponse {
  status: "connected";
}

export function useConnectLLMProvider(provider: LLMProviderName) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: ConnectLLMProviderInput) =>
      apiFetch<ConnectLLMProviderResponse>(`/api/integrations/llm/${provider}`, {
        method: "POST",
        body: JSON.stringify(input),
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["integrations", "llm"] });
    },
  });
}

interface SetLLMProviderModelResponse {
  status: "updated";
}

// useSetLLMProviderModel lets an already-connected provider switch models
// without re-supplying the API key (AI-04) - POST
// /api/integrations/llm/{provider}/model.
export function useSetLLMProviderModel(provider: LLMProviderName) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (model: string) =>
      apiFetch<SetLLMProviderModelResponse>(`/api/integrations/llm/${provider}/model`, {
        method: "POST",
        body: JSON.stringify({ model }),
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["integrations", "llm"] });
    },
  });
}

interface ActivateLLMProviderResponse {
  status: "active";
}

export function useActivateLLMProvider() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (provider: LLMProviderName) =>
      apiFetch<ActivateLLMProviderResponse>(`/api/integrations/llm/${provider}/activate`, {
        method: "POST",
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["integrations", "llm"] });
    },
  });
}
