// useSessions fetches the current user's per-device active sessions
// from GET /api/auth/sessions (user-sessions spec). The real backend
// returns a flat `SessionView[]` (NOT wrapped in Page<T> - device
// count is bounded for a single user, so pagination would just add
// noise). The "current" flag on each row is set server-side by
// comparing the row's id against the JWT's sid claim.
//
// useRevokeSession calls DELETE /api/auth/sessions/{id} and
// invalidates the list on success so the revoked row disappears
// from the next render. Errors propagate to react-query's standard
// `error` field on the mutation result; the component layer is
// responsible for user-facing messaging (toast/alert - matches the
// pattern used by the admins/delete-admin flow).

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFetch } from "../../lib/apiClient";
import type { SessionView } from "../../types/api";

export function useSessions() {
  return useQuery({
    queryKey: ["sessions"],
    queryFn: () => apiFetch<SessionView[]>("/api/auth/sessions"),
  });
}

export function useRevokeSession() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      apiFetch<void>(`/api/auth/sessions/${id}`, { method: "DELETE" }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["sessions"] });
    },
  });
}
