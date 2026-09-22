import type { Role } from "../../types/api";
import type { TagVariant } from "../../components/ui/Tag";

type Translator = (key: string, options?: Record<string, unknown>) => string;

// Users-page USRPG-01/05 role/status label mapping - gap-analysis.md's
// decision: owner->Admin, operator->Membro, viewer->Leitura is a
// presentation-only mapping, never a change to the persisted role value.
export function adminRoleLabel(t: Translator, role: Role): string {
  return t(`admins.roleLabel.${role}`);
}

export const adminRoleVariant: Record<Role, TagVariant> = {
  owner: "accent",
  operator: "neutral",
  viewer: "neutral",
};

export type AdminStatus = "active" | "pending";

export function adminStatusLabel(t: Translator, status: AdminStatus): string {
  return t(`admins.statusLabel.${status}`);
}

export const adminStatusVariant: Record<AdminStatus, TagVariant> = {
  active: "success",
  pending: "warning",
};

export const adminStatusDotColor: Record<AdminStatus, string> = {
  active: "var(--color-success)",
  pending: "var(--color-warning)",
};

// formatLastAccess renders `iso` (an Admin.last_access value) as the
// mock's relative-time copy ("há 2 min"/"há 3 horas"/"há 1 dia"...), or
// "—" when there's no timestamp (pending invite, or never logged in).
export function formatLastAccess(iso: string | null | undefined, t: Translator): string {
  if (!iso) return "—";
  const diffMs = Date.now() - new Date(iso).getTime();
  const minutes = Math.max(0, Math.round(diffMs / 60000));
  if (minutes < 1) return t("admins.relativeTime.now");
  if (minutes < 60) return t("admins.relativeTime.minute", { count: minutes });
  const hours = Math.round(minutes / 60);
  if (hours < 24) return t("admins.relativeTime.hour", { count: hours });
  const days = Math.round(hours / 24);
  return t("admins.relativeTime.day", { count: days });
}
