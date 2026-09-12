// Notification preference hooks (notification-preferences NOTIFPREF-13/14).
//
// useNotificationPreferences reads the caller's own three toggles from GET
// /api/auth/notification-preferences; useUpdateNotificationPreference sends a
// partial PATCH for a single key with an optimistic update, rolling the
// optimistic change back on failure so the switch never lies about persisted
// state (the section surfaces the error toast).

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFetch } from "../../lib/apiClient";

export interface NotificationPreferences {
  incident_opened: boolean;
  incident_resolved: boolean;
  weekly_digest: boolean;
}

export type NotificationType = keyof NotificationPreferences;

export const notificationPreferencesQueryKey = ["notification-preferences"];

export function useNotificationPreferences() {
  return useQuery({
    queryKey: notificationPreferencesQueryKey,
    queryFn: () =>
      apiFetch<NotificationPreferences>("/api/auth/notification-preferences"),
  });
}

export function useUpdateNotificationPreference() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (input: Partial<NotificationPreferences>) =>
      apiFetch<NotificationPreferences>("/api/auth/notification-preferences", {
        method: "PATCH",
        body: JSON.stringify(input),
      }),
    onMutate: async (input) => {
      await queryClient.cancelQueries({ queryKey: notificationPreferencesQueryKey });
      const previous = queryClient.getQueryData<NotificationPreferences>(
        notificationPreferencesQueryKey,
      );
      if (previous) {
        queryClient.setQueryData<NotificationPreferences>(
          notificationPreferencesQueryKey,
          { ...previous, ...input },
        );
      }
      return { previous };
    },
    onError: (_error, _input, context) => {
      if (context?.previous) {
        queryClient.setQueryData(notificationPreferencesQueryKey, context.previous);
      }
    },
    onSuccess: (data) => {
      queryClient.setQueryData(notificationPreferencesQueryKey, data);
    },
  });
}
