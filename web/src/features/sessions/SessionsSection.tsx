// <SessionsSection /> renders the current user's per-device active
// sessions (user-sessions spec). It's a self-contained feature module
// in `web/src/features/sessions/` - the future "Meu Perfil" page
// (its own spec, deferred - see new-layout-migration/spec.md:21) will
// import this component and slot it into its layout. No host-page
// assumptions are made here.
//
// Mutation UX: clicking "Encerrar" immediately calls
// DELETE /api/auth/sessions/{id} - no confirmation dialog. The action
// is reversible (the user just signs in again on that device) and
// requires no destructive side effects on the local browser, so a
// confirm step would just add friction. The mutation is disabled
// while in-flight (revoke.isPending) so a double-click can't double-
// fire; success/error feedback comes through a sonner toast.

import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { ApiError } from "../../lib/apiClient";
import { Button } from "../../components/ui/Button";
import { Card } from "../../components/ui/Card";
import { Tag } from "../../components/ui/Tag";
import { useRevokeSession, useSessions } from "./hooks";

function formatTimestamp(iso: string | null, locale: string): string {
  if (!iso) return "-";
  return new Date(iso).toLocaleString(locale);
}

// deviceLabel shortens a User-Agent to a one-word OS/browser label
// for display. Real UA parsing is out of scope (context.md, "Declined
// / Undiscussed Gray Areas") so this is a coarse substring match on
// well-known fingerprints - good enough for the "what device is this"
// callout in a list of 1-5 rows, not a real UA parser.
function deviceLabel(userAgent: string | null, unknownLabel: string): string {
  if (!userAgent) return unknownLabel;
  if (/iPhone/.test(userAgent)) return "iPhone";
  if (/iPad/.test(userAgent)) return "iPad";
  if (/Android/.test(userAgent)) return "Android";
  if (/Macintosh/.test(userAgent)) return "Mac";
  if (/Windows/.test(userAgent)) return "Windows";
  if (/Linux/.test(userAgent)) return "Linux";
  return unknownLabel;
}

export function SessionsSection() {
  const { t, i18n } = useTranslation();
  const { data, isLoading, isError } = useSessions();
  const revoke = useRevokeSession();
  const sessions = data ?? [];

  const handleRevoke = (id: string) => {
    revoke.mutate(id, {
      onSuccess: () => {
        toast.success(t("sessions.revokeSuccess"));
      },
      onError: (err) => {
        if (err instanceof ApiError && err.status === 409) {
          // Defensive: the section doesn't render a button on the
          // current session, so 409 is unexpected here. Surface the
          // same message the real backend would have, in case a
          // future change accidentally opens that path.
          toast.error(t("sessions.revokeCurrentError"));
        } else {
          toast.error(t("sessions.genericError"));
        }
      },
    });
  };

  return (
    <div className="flex flex-col gap-4">
      <div>
        <h2 className="text-text">{t("sessions.title")}</h2>
        <p className="m-0 text-[13.5px] text-neutral-400">{t("sessions.subtitle")}</p>
      </div>
      <Card elevation="elev-sm" className="divide-y divide-divider overflow-hidden">
        {isLoading ? (
          <p className="px-4 py-6 text-center text-neutral-400" data-testid="sessions-loading">
            {t("sessions.loading")}
          </p>
        ) : isError ? (
          <p className="px-4 py-6 text-center text-neutral-400" data-testid="sessions-error">
            {t("sessions.loadError")}
          </p>
        ) : sessions.length === 0 ? (
          <p className="px-4 py-6 text-center text-neutral-400" data-testid="sessions-empty">
            {t("sessions.empty")}
          </p>
        ) : (
          sessions.map((s) => (
            <div
              key={s.id}
              data-testid="session-row"
              data-session-id={s.id}
              data-current={s.current ? "true" : "false"}
              className="flex items-center gap-3 px-4 py-3.5"
            >
              <div className="flex-1">
                <div className="flex items-center gap-2">
                  <span className="text-[15px] font-medium text-text">
                    {deviceLabel(s.user_agent, t("sessions.unknownDevice"))}
                  </span>
                  {s.current ? (
                    <Tag variant="accent" data-testid="session-current-badge">
                      {t("sessions.currentBadge")}
                    </Tag>
                  ) : null}
                </div>
                <div className="mt-0.5 text-xs text-neutral-400">
                  {t("sessions.columns.ip")}: {s.ip ?? t("sessions.unknownIp")}
                </div>
              </div>
              <div className="text-right text-xs text-neutral-400">
                <div>{t("sessions.columns.lastSeen")}</div>
                <div className="mt-0.5 text-[13px] text-text">{formatTimestamp(s.last_seen_at, i18n.language)}</div>
              </div>
              {s.current ? null : (
                <Button
                  variant="secondary"
                  onClick={() => handleRevoke(s.id)}
                  disabled={revoke.isPending}
                  data-testid="revoke-button"
                >
                  {t("sessions.revokeButton")}
                </Button>
              )}
            </div>
          ))
        )}
      </Card>
    </div>
  );
}
