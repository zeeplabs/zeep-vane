// useUpdateProfileName updates the caller's own display name via PATCH
// /api/auth/me (profile-self-service PROFSS-01..03) and, on success,
// refreshes the authenticated identity so the page and topbar show the new
// name without a reload (profile-page PROFPAGE-04/05).
//
// useChangePassword rotates the caller's own password via POST
// /api/auth/change-password (PROFSS-04..06). Both surface ApiError (status
// 401/422) to callers for inline error branching, the same convention
// features/sessions/hooks.ts follows.

import { useMutation } from "@tanstack/react-query";
import { apiFetch } from "../../lib/apiClient";
import { useAuth, type AuthenticatedAdmin } from "../../auth/AuthProvider";

export interface ChangePasswordInput {
  current_password: string;
  new_password: string;
}

export function useUpdateProfileName() {
  const { refreshAdmin } = useAuth();
  return useMutation({
    mutationFn: (name: string) =>
      apiFetch<AuthenticatedAdmin>("/api/auth/me", {
        method: "PATCH",
        body: JSON.stringify({ name }),
      }),
    onSuccess: () => {
      // Best-effort identity refresh (never throws): the PATCH already
      // succeeded, so a failed refresh must not surface as a mutation error.
      void refreshAdmin();
    },
  });
}

export function useChangePassword() {
  return useMutation({
    mutationFn: (input: ChangePasswordInput) =>
      apiFetch<{ status: string }>("/api/auth/change-password", {
        method: "POST",
        body: JSON.stringify(input),
      }),
  });
}
