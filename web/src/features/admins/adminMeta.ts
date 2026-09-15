import type { Role } from "../../types/api";
import type { TagVariant } from "../../components/ui/Tag";

// Users-page USRPG-01/05 role/status label mapping - gap-analysis.md's
// decision: owner->Admin, operator->Membro, viewer->Leitura is a
// presentation-only mapping, never a change to the persisted role value.
export const adminRoleLabel: Record<Role, string> = {
  owner: "Admin",
  operator: "Membro",
  viewer: "Leitura",
};

export const adminRoleVariant: Record<Role, TagVariant> = {
  owner: "accent",
  operator: "neutral",
  viewer: "neutral",
};

export type AdminStatus = "active" | "pending";

export const adminStatusLabel: Record<AdminStatus, string> = {
  active: "Ativo",
  pending: "Pendente",
};

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
export function formatLastAccess(iso: string | null | undefined): string {
  if (!iso) return "—";
  const diffMs = Date.now() - new Date(iso).getTime();
  const minutes = Math.max(0, Math.round(diffMs / 60000));
  if (minutes < 1) return "agora";
  if (minutes < 60) return `há ${minutes} min`;
  const hours = Math.round(minutes / 60);
  if (hours < 24) return `há ${hours} hora${hours === 1 ? "" : "s"}`;
  const days = Math.round(hours / 24);
  return `há ${days} dia${days === 1 ? "" : "s"}`;
}
