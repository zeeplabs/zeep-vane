// DisableDialog asks for the current password before disabling 2FA
// (profile-page PROFPAGE-17/18). The password requirement matches the
// backend contract (auth-2fa-totp) and closes the hijacked-session-disables-
// 2FA gap. On success it calls onDisabled (host refreshes the identity) and
// closes; a 401 shows an inline error and leaves 2FA enabled.

import { useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { ApiError } from "../../lib/apiClient";
import { Button } from "../../components/ui/Button";
import { Dialog } from "../../components/ui/Dialog";
import { Field } from "../../components/ui/Field";
import { useDisable2FA } from "./hooks";

export interface DisableDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onDisabled: () => void;
}

export function DisableDialog({ open, onOpenChange, onDisabled }: DisableDialogProps) {
  const { t } = useTranslation();
  const disable = useDisable2FA();
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);

  const handleSubmit = (event: FormEvent) => {
    event.preventDefault();
    disable.mutate(password, {
      onSuccess: () => {
        setPassword("");
        setError(null);
        toast.success(t("twoFactor.disable.success"));
        onDisabled();
        onOpenChange(false);
      },
      onError: (err) => {
        if (err instanceof ApiError && err.status === 401) {
          setError(t("twoFactor.disable.wrongPassword"));
        } else {
          setError(t("twoFactor.disable.error"));
        }
      },
    });
  };

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={t("twoFactor.disable.title")}
      description={t("twoFactor.disable.body")}
      footer={
        <>
          <Button variant="secondary" onClick={() => onOpenChange(false)}>
            {t("twoFactor.disable.cancel")}
          </Button>
          <Button
            type="submit"
            form="disable-2fa-form"
            disabled={disable.isPending}
            data-testid="two-factor-disable-confirm"
          >
            {t("twoFactor.disable.confirm")}
          </Button>
        </>
      }
    >
      <form id="disable-2fa-form" onSubmit={handleSubmit}>
        <Field
          type="password"
          label={t("twoFactor.disable.passwordLabel")}
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          error={error ?? undefined}
          autoComplete="current-password"
        />
      </form>
    </Dialog>
  );
}
