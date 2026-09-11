// PersonalInfoCard shows the caller's display name (editable), email
// (read-only) and an initials avatar, and submits name changes through
// PATCH /api/auth/me (profile-page PROFPAGE-04/05/06). The email and avatar
// are display-only: email change and avatar upload are out of scope
// (spec.md Out of Scope). On success the hook refreshes the identity and the
// field re-syncs to the server's value, so the card never shows a stale
// name.

import { useEffect, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { ApiError } from "../../lib/apiClient";
import { useAuth } from "../../auth/AuthProvider";
import { Button } from "../../components/ui/Button";
import { Card } from "../../components/ui/Card";
import { Field } from "../../components/ui/Field";
import { useUpdateProfileName } from "./hooks";

// initialsOf derives a 1-2 letter avatar from the name, falling back to the
// email when no name is set. Avatar upload is out of scope, so initials are
// the whole avatar.
function initialsOf(name: string | undefined, email: string): string {
  const source = (name ?? "").trim();
  if (source) {
    const parts = source.split(/\s+/);
    const first = parts[0]?.[0] ?? "";
    const last = parts.length > 1 ? parts[parts.length - 1][0] : "";
    return (first + last).toUpperCase();
  }
  return email.slice(0, 2).toUpperCase();
}

export function PersonalInfoCard() {
  const { t } = useTranslation();
  const { admin } = useAuth();
  const update = useUpdateProfileName();
  const [name, setName] = useState(admin?.name ?? "");
  const [error, setError] = useState<string | null>(null);

  // Keep the field in step with the refreshed identity (PROFPAGE-05): after
  // a successful PATCH the context carries the server's value. A local edit
  // does not change admin.name, so typing is never clobbered.
  useEffect(() => {
    setName(admin?.name ?? "");
  }, [admin?.name]);

  if (!admin) return null;

  const handleSubmit = (event: FormEvent) => {
    event.preventDefault();
    const trimmed = name.trim();
    if (!trimmed) {
      setError(t("profile.personalInfo.nameRequired"));
      return;
    }
    setError(null);
    update.mutate(trimmed, {
      onSuccess: () => toast.success(t("profile.personalInfo.updateSuccess")),
      onError: (err) => {
        if (err instanceof ApiError && err.status === 422) {
          setError(t("profile.personalInfo.nameRequired"));
        } else {
          toast.error(t("profile.personalInfo.updateError"));
        }
      },
    });
  };

  return (
    <Card elevation="elev-sm" className="p-6">
      <form onSubmit={handleSubmit} className="flex flex-col gap-4">
        <div className="flex items-center gap-4">
          <div
            data-testid="profile-avatar"
            aria-hidden="true"
            className="flex h-12 w-12 items-center justify-center rounded-full bg-accent-900 text-base font-medium text-accent-200"
          >
            {initialsOf(admin.name, admin.email)}
          </div>
          <div>
            <h2 className="text-text">{t("profile.personalInfo.title")}</h2>
            <p className="m-0 text-[13.5px] text-neutral-400">{t("profile.personalInfo.subtitle")}</p>
          </div>
        </div>
        <Field
          label={t("profile.personalInfo.nameLabel")}
          value={name}
          onChange={(e) => setName(e.target.value)}
          error={error ?? undefined}
        />
        <Field label={t("profile.personalInfo.emailLabel")} value={admin.email} readOnly />
        <div>
          <Button type="submit" disabled={update.isPending}>
            {t("profile.personalInfo.saveButton")}
          </Button>
        </div>
      </form>
    </Card>
  );
}
