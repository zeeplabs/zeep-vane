import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFetch } from "../../lib/apiClient";
import type { CompanySettings } from "../../types/api";

export function useCompanySettings() {
  return useQuery({
    queryKey: ["company-settings"],
    queryFn: () => apiFetch<CompanySettings>("/api/company-settings"),
  });
}

export interface UpdateCompanySettingsInput {
  name: string;
  contact_email: string;
  logo_url?: string | null;
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
