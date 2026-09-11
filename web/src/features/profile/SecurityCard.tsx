// SecurityCard hosts the password-change form and, below it,
// <TwoFactorCard/> (profile-page PROFPAGE-07..11). The confirmation field is
// pure UX: mismatch/empty block the request client-side, while the
// current-password and policy checks are the backend's (401/422), surfaced
// inline. On success the form clears and a toast confirms; the current
// session stays authenticated (only other sessions are revoked server-side).

import { useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { ApiError } from "../../lib/apiClient";
import { Button } from "../../components/ui/Button";
import { Card } from "../../components/ui/Card";
import { Field } from "../../components/ui/Field";
import { TwoFactorCard } from "../two-factor/TwoFactorCard";
import { useChangePassword } from "./hooks";

export function SecurityCard() {
  const { t } = useTranslation();
  const change = useChangePassword();
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [error, setError] = useState<string | null>(null);

  const handleSubmit = (event: FormEvent) => {
    event.preventDefault();
    if (!currentPassword || !newPassword || !confirmPassword) {
      setError(t("profile.security.required"));
      return;
    }
    if (newPassword !== confirmPassword) {
      setError(t("profile.security.mismatch"));
      return;
    }
    setError(null);
    change.mutate(
      { current_password: currentPassword, new_password: newPassword },
      {
        onSuccess: () => {
          setCurrentPassword("");
          setNewPassword("");
          setConfirmPassword("");
          toast.success(t("profile.security.updateSuccess"));
        },
        onError: (err) => {
          if (err instanceof ApiError && err.status === 401) {
            setError(t("profile.security.wrongCurrent"));
          } else if (err instanceof ApiError && err.status === 422) {
            setError(t("profile.security.policyError"));
          } else {
            setError(t("profile.security.updateError"));
          }
        },
      }
    );
  };

  return (
    <Card elevation="elev-sm" className="flex flex-col gap-6 p-6">
      <div>
        <h2 className="text-text">{t("profile.security.title")}</h2>
        <p className="m-0 text-[13.5px] text-neutral-400">{t("profile.security.subtitle")}</p>
      </div>

      <form onSubmit={handleSubmit} className="flex flex-col gap-4">
        <Field
          type="password"
          label={t("profile.security.currentPassword")}
          value={currentPassword}
          onChange={(e) => setCurrentPassword(e.target.value)}
          autoComplete="current-password"
        />
        <Field
          type="password"
          label={t("profile.security.newPassword")}
          value={newPassword}
          onChange={(e) => setNewPassword(e.target.value)}
          autoComplete="new-password"
        />
        <Field
          type="password"
          label={t("profile.security.confirmPassword")}
          value={confirmPassword}
          onChange={(e) => setConfirmPassword(e.target.value)}
          autoComplete="new-password"
        />
        {error ? (
          <p role="alert" className="m-0 text-xs text-critical">
            {error}
          </p>
        ) : null}
        <div>
          <Button type="submit" disabled={change.isPending}>
            {t("profile.security.updatePassword")}
          </Button>
        </div>
      </form>

      <div className="border-t border-divider pt-6">
        <TwoFactorCard />
      </div>
    </Card>
  );
}
