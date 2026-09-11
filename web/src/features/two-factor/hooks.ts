// Two-factor mutations over the auth-2fa-totp backend (design.md).
// useEnroll2FA starts an enrollment and returns the secret + otpauth URI;
// useConfirm2FA validates a code and returns the one-time recovery codes;
// useDisable2FA re-proves the password, turns 2FA off, and refreshes the
// authenticated identity so the card reflects the change (PROFPAGE-17).
// Errors propagate as ApiError (409 enroll / 422 confirm / 401 disable) for
// the components to branch on, same convention as features/sessions/hooks.ts.

import { useMutation } from "@tanstack/react-query";
import { apiFetch } from "../../lib/apiClient";
import { useAuth } from "../../auth/AuthProvider";
import type { ConfirmResponse, EnrollResponse } from "./types";

export function useEnroll2FA() {
  return useMutation({
    mutationFn: () =>
      apiFetch<EnrollResponse>("/api/auth/2fa/enroll", {
        method: "POST",
        body: JSON.stringify({}),
      }),
  });
}

export function useConfirm2FA() {
  return useMutation({
    mutationFn: (code: string) =>
      apiFetch<ConfirmResponse>("/api/auth/2fa/confirm", {
        method: "POST",
        body: JSON.stringify({ code }),
      }),
  });
}

export function useDisable2FA() {
  const { refreshAdmin } = useAuth();
  return useMutation({
    mutationFn: (currentPassword: string) =>
      apiFetch<{ status: string }>("/api/auth/2fa/disable", {
        method: "POST",
        body: JSON.stringify({ current_password: currentPassword }),
      }),
    onSuccess: () => {
      // Best-effort identity refresh (never throws), same as
      // useUpdateProfileName.
      void refreshAdmin();
    },
  });
}
