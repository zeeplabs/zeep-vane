// TwoFactorCard shows the current 2FA state from the authenticated identity
// and routes to enable (a <EnrollDrawer/>) or disable (a <DisableDialog/>).
// The card itself holds no server state: after either flow completes it
// refreshes the identity through the auth context, and the badge + action
// re-derive from admin.two_factor_enabled (profile-page PROFPAGE-12/13/17).

import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useAuth } from "../../auth/AuthProvider";
import { Button } from "../../components/ui/Button";
import { Tag } from "../../components/ui/Tag";
import { EnrollDrawer } from "./EnrollDrawer";
import { DisableDialog } from "./DisableDialog";

export function TwoFactorCard() {
  const { t } = useTranslation();
  const { admin, refreshAdmin } = useAuth();
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [dialogOpen, setDialogOpen] = useState(false);

  if (!admin) return null;
  const enabled = admin.two_factor_enabled;

  return (
    <div
      data-testid="two-factor-card"
      data-enabled={enabled ? "true" : "false"}
      className="flex items-center justify-between gap-4"
    >
      <div>
        <div className="flex items-center gap-2">
          <h3 className="m-0 text-[15px] font-medium text-text">{t("twoFactor.title")}</h3>
          <Tag variant={enabled ? "success" : "neutral"} data-testid="two-factor-badge">
            {enabled ? t("twoFactor.enabledBadge") : t("twoFactor.disabledBadge")}
          </Tag>
        </div>
        <p className="m-0 mt-0.5 text-[13.5px] text-neutral-400">{t("twoFactor.subtitle")}</p>
      </div>

      {enabled ? (
        <Button
          variant="secondary"
          onClick={() => setDialogOpen(true)}
          data-testid="two-factor-disable"
        >
          {t("twoFactor.disableButton")}
        </Button>
      ) : (
        <Button onClick={() => setDrawerOpen(true)} data-testid="two-factor-enable">
          {t("twoFactor.enableButton")}
        </Button>
      )}

      <EnrollDrawer
        open={drawerOpen}
        onOpenChange={setDrawerOpen}
        onEnabled={() => void refreshAdmin()}
      />
      <DisableDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        onDisabled={() => void refreshAdmin()}
      />
    </div>
  );
}
