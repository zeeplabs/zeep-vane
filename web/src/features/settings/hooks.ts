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
