// NotificationsSection is the Meu Perfil "Notificações" card
// (notification-preferences NOTIFPREF-13..16). It owns no layout beyond its
// own card: it reads the caller's three toggles, renders one Switch each, and
// writes a single-key PATCH per toggle. The optimistic update and its rollback
// live in the hook; the error toast is surfaced here.

import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { Card } from "../../components/ui/Card";
import { Switch } from "../../components/ui/Switch";
import {
  useNotificationPreferences,
  useUpdateNotificationPreference,
  type NotificationPreferences,
  type NotificationType,
} from "./hooks";

interface ToggleDef {
  type: NotificationType;
  labelKey: string;
  hintKey: string;
}

const TOGGLES: ToggleDef[] = [
  {
    type: "incident_opened",
    labelKey: "profile.notifications.incidentOpened",
    hintKey: "profile.notifications.incidentOpenedHint",
  },
  {
    type: "incident_resolved",
    labelKey: "profile.notifications.incidentResolved",
    hintKey: "profile.notifications.incidentResolvedHint",
  },
  {
    type: "weekly_digest",
    labelKey: "profile.notifications.weeklyDigest",
    hintKey: "profile.notifications.weeklyDigestHint",
  },
];

export function NotificationsSection() {
  const { t } = useTranslation();
  const query = useNotificationPreferences();
  const update = useUpdateNotificationPreference();

  const disabled = query.isLoading || query.isError;

  const handleToggle = (type: NotificationType, checked: boolean) => {
    update.mutate(
      { [type]: checked } as Partial<NotificationPreferences>,
      { onError: () => toast.error(t("profile.notifications.updateError")) },
    );
  };

  return (
    <Card elevation="elev-sm" className="flex flex-col gap-6 p-6">
      <div className="flex flex-col gap-1">
        <h2 className="text-text">{t("profile.notifications.title")}</h2>
        <p className="text-sm text-neutral-300">{t("profile.notifications.subtitle")}</p>
      </div>

      {query.isError && (
        <p role="alert" className="text-sm text-critical">
          {t("profile.notifications.loadError")}
        </p>
      )}

      <ul className="flex flex-col gap-4">
        {TOGGLES.map(({ type, labelKey, hintKey }) => (
          <li key={type} className="flex items-center justify-between gap-4">
            <div className="flex flex-col">
              <span className="text-text">{t(labelKey)}</span>
              <span className="text-sm text-neutral-400">{t(hintKey)}</span>
            </div>
            <Switch
              checked={query.data?.[type] ?? false}
              disabled={disabled}
              aria-label={t(labelKey)}
              onChange={(checked) => handleToggle(type, checked)}
            />
          </li>
        ))}
      </ul>
    </Card>
  );
}
