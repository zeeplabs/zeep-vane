import { useQuery } from "@tanstack/react-query";
import { apiFetch, resolveAssetUrl } from "./apiClient";

interface BrandingResponse {
  logo_url: string | null;
}

// useBrandLogoUrl backs the login screen and the sidebar - both must show
// the company's custom logo when one was uploaded (SettingsPage) or fall
// back to Vane's own mark otherwise. GET /api/instance/branding is public
// (no auth), unlike /api/company-settings (owner-only): the login screen
// has no session yet, and viewer/operator (not just owner) also see the
// sidebar.
export function useBrandLogoUrl(): string | null {
  const { data } = useQuery({
    queryKey: ["instance-branding"],
    queryFn: () => apiFetch<BrandingResponse>("/api/instance/branding"),
    staleTime: 5 * 60_000,
    retry: false,
  });
  return resolveAssetUrl(data?.logo_url ?? null);
}
