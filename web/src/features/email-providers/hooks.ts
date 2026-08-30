import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFetch } from "../../lib/apiClient";

export type EmailProviderName = "sendgrid" | "resend";

// Mirrors EmailProvidersHandler.List's response shape
// (internal/api/email_providers_handler.go) - no api_key in any form
// (EMAIL-06 AC1).
export interface EmailProviderStatus {
  provider: EmailProviderName;
  status: "connected" | "invalid";
  from_email: string;
  from_name: string;
  last_checked_at: string | null;
  last_error: string | null;
}

// Mirrors EmailProvidersHandler.List's paginated response shape (PAG-08) -
// active_provider/providers sit alongside the pagination fields rather
// than nesting providers under a generic Page<T> envelope, since
// active_provider is not itself a list item.
export interface EmailProvidersResponse {
  active_provider: EmailProviderName | null;
  providers: EmailProviderStatus[];
  total: number;
  page: number;
  page_size: number;
}

export function useEmailProviders(page: number) {
  return useQuery({
    queryKey: ["integrations", "email", page],
    queryFn: () => apiFetch<EmailProvidersResponse>(`/api/integrations/email?page=${page}`),
  });
}

export interface ConnectEmailProviderInput {
  api_key: string;
  from_email: string;
  from_name: string;
}

// The real backend's POST /api/integrations/email/{provider} only ever
// returns {"status":"connected"} (EMAIL-01 AC5 - never api_key back).
interface ConnectEmailProviderResponse {
  status: "connected";
}

export function useConnectEmailProvider(provider: EmailProviderName) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: ConnectEmailProviderInput) =>
      apiFetch<ConnectEmailProviderResponse>(`/api/integrations/email/${provider}`, {
        method: "POST",
        body: JSON.stringify(input),
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["integrations", "email"] });
    },
  });
}

interface ActivateEmailProviderResponse {
  status: "active";
}

export function useActivateEmailProvider() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (provider: EmailProviderName) =>
      apiFetch<ActivateEmailProviderResponse>(`/api/integrations/email/${provider}/activate`, {
        method: "POST",
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["integrations", "email"] });
    },
  });
}
