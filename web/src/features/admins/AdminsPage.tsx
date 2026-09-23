import { useMemo, useState, type FormEvent } from "react";
import { toast } from "sonner";
import { useTranslation } from "react-i18next";
import * as RadixDialog from "@radix-ui/react-dialog";
import { MdOutlineAdd, MdChevronRight, MdClose } from "react-icons/md";
import { Card } from "../../components/ui/Card";
import { Dialog } from "../../components/ui/Dialog";
import { Button } from "../../components/ui/Button";
import { Field } from "../../components/ui/Field";
import { PhoneField } from "../../components/ui/PhoneField";
import { Tag } from "../../components/ui/Tag";
import { Pager } from "../../components/ui/Pager";
import { Skeleton } from "../../components/ui/Skeleton";
import { Drawer, drawerFooterPrimaryStyle, drawerFooterSecondaryStyle } from "../../components/ui/Drawer";
import { ApiError } from "../../lib/apiClient";
import type { Role } from "../../types/api";
import { adminRoleLabel, formatLastAccess } from "./adminMeta";
import { AdminStatusTag } from "./AdminStatusTag";
import {
  useAdmins,
  useCancelInvite,
  useDeleteAdmin,
  useInviteAdmin,
  useResendInvite,
  useUpdateAdminRole,
  type AdminRow,
} from "./hooks";

type RoleFilter = "all" | Role;
type Translator = (key: string, options?: Record<string, unknown>) => string;
const roleFilters: RoleFilter[] = ["all", "owner", "operator", "viewer"];
const roles: Role[] = ["owner", "operator", "viewer"];

function filterLabelFor(t: Translator, value: RoleFilter): string {
  return value === "all" ? t("admins.filters.all") : adminRoleLabel(t, value);
}

function PlusIcon() {
  return <MdOutlineAdd size={14} aria-hidden="true" />;
}

function initials(a: AdminRow): string {
  const source = a.name || a.email;
  return source
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((part) => part.charAt(0).toUpperCase())
    .join("");
}

function RoleBoxes({ value, onChange, t }: { value: Role; onChange: (r: Role) => void; t: Translator }) {
  return (
    <div role="radiogroup" aria-label={t("admins.roleFieldLabel")} className="flex flex-col gap-2">
      {roles.map((role) => {
        const active = value === role;
        return (
          <button
            key={role}
            type="button"
            role="radio"
            aria-checked={active}
            onClick={() => onChange(role)}
            className="cursor-pointer rounded-md border-[1.5px] px-3.5 py-3 text-left transition-colors"
            style={
              active
                ? {
                    borderColor: "var(--color-accent)",
                    backgroundColor: "color-mix(in oklch, var(--color-accent) 6%, transparent)",
                    color: "var(--color-text)",
                  }
                : {
                    borderColor: "var(--color-divider)",
                    backgroundColor: "var(--color-surface)",
                    color: "var(--color-text-muted)",
                  }
            }
          >
            <div className="mb-0.5 text-[13px] font-bold">{t(`admins.roleOptions.${role}.label`)}</div>
            <div className="text-[11.5px] leading-snug opacity-80">{t(`admins.roleOptions.${role}.description`)}</div>
          </button>
        );
      })}
    </div>
  );
}

export function AdminsPage() {
  const { t } = useTranslation();
  const [page, setPage] = useState(1);
  const { data: adminsPage, isLoading } = useAdmins(page);
  const admins = useMemo(() => adminsPage?.items ?? [], [adminsPage]);
  const totalPages = Math.max(1, Math.ceil((adminsPage?.total ?? 0) / (adminsPage?.page_size ?? 20)));
  const inviteAdmin = useInviteAdmin();
  const updateRole = useUpdateAdminRole();
  const deleteAdmin = useDeleteAdmin();
  const resendInvite = useResendInvite();
  const cancelInvite = useCancelInvite();

  const [roleFilter, setRoleFilter] = useState<RoleFilter>("all");
  const [searchQuery, setSearchQuery] = useState("");
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [roleError, setRoleError] = useState<string | null>(null);
  const [removeTarget, setRemoveTarget] = useState<AdminRow | null>(null);
  const [removeError, setRemoveError] = useState<string | null>(null);

  const [inviteOpen, setInviteOpen] = useState(false);
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [phone, setPhone] = useState("");
  const [newRole, setNewRole] = useState<Role>("viewer");
  const [inviteError, setInviteError] = useState<string | null>(null);

  const counts = useMemo(() => {
    const base: Record<RoleFilter, number> = { all: admins.length, owner: 0, operator: 0, viewer: 0 };
    for (const a of admins) base[a.role] += 1;
    return base;
  }, [admins]);

  const q = searchQuery.trim().toLowerCase();
  const filtered = useMemo(
    () =>
      admins.filter((a) => {
        const matchesRole = roleFilter === "all" || a.role === roleFilter;
        const matchesQuery = !q || (a.name ?? "").toLowerCase().includes(q) || a.email.toLowerCase().includes(q);
        return matchesRole && matchesQuery;
      }),
    [admins, roleFilter, q]
  );

  const selected = admins.find((a) => a.id === selectedId) ?? null;

  async function handleInvite(e: FormEvent) {
    e.preventDefault();
    setInviteError(null);
    try {
      await inviteAdmin.mutateAsync({ name, email, phone: phone || undefined, role: newRole });
      setName("");
      setEmail("");
      setPhone("");
      setNewRole("viewer");
      setInviteOpen(false);
    } catch (err) {
      setInviteError(err instanceof ApiError ? err.message : t("admins.invite.error"));
    }
  }

  async function handleRoleChange(a: AdminRow, role: Role) {
    if (role === a.role) return;
    setRoleError(null);
    try {
      await updateRole.mutateAsync({ id: a.id, role });
    } catch (err) {
      setRoleError(err instanceof ApiError ? err.message : t("admins.roleChangeError"));
    }
  }

  async function confirmRemove() {
    if (!removeTarget) return;
    setRemoveError(null);
    try {
      if (removeTarget.status === "pending") {
        await cancelInvite.mutateAsync(removeTarget.id);
        toast.success(t("admins.remove.inviteCancelledToast", { email: removeTarget.email }));
      } else {
        await deleteAdmin.mutateAsync(removeTarget.id);
        toast.success(t("admins.remove.accessRemovedToast", { email: removeTarget.email }));
      }
      setRemoveTarget(null);
      setSelectedId(null);
    } catch (err) {
      setRemoveError(err instanceof ApiError ? err.message : t("admins.remove.error"));
    }
  }

  async function handleResend(a: AdminRow) {
    try {
      const result = await resendInvite.mutateAsync(a.id);
      toast.success(
        result.email_sent
          ? t("admins.resend.successToast", { email: a.email })
          : t("admins.resend.failedDeliveryToast", { email: a.email })
      );
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : t("admins.resend.error"));
    }
  }

  return (
    <div className="mx-auto flex w-full max-w-[1280px] flex-col gap-5">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h2 className="text-text">{t("admins.title")}</h2>
          <p className="m-0 text-[13.5px] text-neutral-400">{t("admins.subtitle")}</p>
        </div>
        <Button variant="solid" onClick={() => setInviteOpen(true)}>
          <PlusIcon />
          {t("admins.inviteButton")}
        </Button>
      </div>

      <div className="flex flex-wrap items-center justify-between gap-3">
        <div role="group" aria-label={t("admins.filters.ariaLabel")} className="flex flex-wrap items-center gap-2">
          {roleFilters.map((value) => {
            const active = roleFilter === value;
            return (
              <button
                key={value}
                type="button"
                aria-pressed={active}
                onClick={() => setRoleFilter(value)}
                className={
                  "inline-flex cursor-pointer items-center gap-1.5 rounded-full border px-3.5 py-1.5 text-xs font-semibold transition-colors " +
                  (active
                    ? "border-accent bg-accent-100 text-accent"
                    : "border-divider bg-surface text-neutral-400 hover:text-text")
                }
              >
                {filterLabelFor(t, value)}
                <span className="opacity-70">{counts[value]}</span>
              </button>
            );
          })}
        </div>
        <input
          type="text"
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
          placeholder={t("admins.searchPlaceholder")}
          aria-label={t("admins.searchPlaceholder")}
          className="w-60 rounded-md border border-transparent bg-card-header-bg px-3 py-2 text-[13px] text-text outline-none transition-colors focus:border-accent focus:bg-surface"
        />
      </div>

      {isLoading ? (
        <div aria-busy="true" className="overflow-hidden rounded-md border border-divider">
          <span className="sr-only">{t("admins.loading")}</span>
          {Array.from({ length: 5 }).map((_, i) => (
            <div
              key={i}
              className="grid grid-cols-[minmax(220px,1fr)_110px_130px_130px_20px] items-center gap-3 border-b border-divider px-5 py-3 last:border-b-0"
            >
              <div className="flex min-w-0 items-center gap-2.5">
                <Skeleton width={32} height={32} radius={8} />
                <Skeleton width={140} height={14} />
              </div>
              <Skeleton width={70} height={20} radius={999} />
              <Skeleton width={80} height={20} radius={999} />
              <Skeleton width={90} height={14} />
              <Skeleton width={16} height={16} />
            </div>
          ))}
        </div>
      ) : (
        <Card elevation="none" className="overflow-hidden border border-divider">
          <div className="grid grid-cols-[minmax(220px,1fr)_110px_130px_130px_20px] items-center gap-3 border-b border-divider bg-card-header-bg px-5 py-2.5">
            <span className="text-[11px] font-bold uppercase tracking-wide text-neutral-400">{t("admins.table.user")}</span>
            <span className="text-[11px] font-bold uppercase tracking-wide text-neutral-400">{t("admins.table.role")}</span>
            <span className="text-[11px] font-bold uppercase tracking-wide text-neutral-400">{t("common.status")}</span>
            <span className="text-[11px] font-bold uppercase tracking-wide text-neutral-400">{t("admins.table.lastAccess")}</span>
            <span />
          </div>
          {filtered.length === 0 ? (
            <p className="px-5 py-12 text-center text-[13.5px] text-neutral-400">{t("admins.empty")}</p>
          ) : (
            filtered.map((a) => (
              <div
                key={a.id}
                data-testid="admin-row"
                onClick={() => setSelectedId(a.id)}
                className="grid cursor-pointer grid-cols-[minmax(220px,1fr)_110px_130px_130px_20px] items-center gap-3 border-b border-divider px-5 py-3 last:border-b-0 hover:bg-card-header-bg"
              >
                <div className="flex min-w-0 items-center gap-2.5">
                  <div className="grid h-8 w-8 flex-none place-items-center rounded-md bg-accent-100 text-xs font-bold text-accent">
                    {initials(a)}
                  </div>
                  <div className="min-w-0">
                    <div className="truncate text-[13.5px] font-bold text-text">{a.name || a.email}</div>
                    {a.name ? <div className="truncate text-xs text-neutral-400">{a.email}</div> : null}
                  </div>
                </div>
                <Tag variant={a.role === "owner" ? "accent" : "neutral"} className="w-fit rounded-full">
                  {adminRoleLabel(t, a.role)}
                </Tag>
                <div className="flex flex-wrap items-center gap-1.5">
                  <AdminStatusTag status={a.status} />
                  {a.expired ? <Tag variant="critical">{t("admins.expiredTag")}</Tag> : null}
                </div>
                <div className="text-[12.5px] text-neutral-400">{formatLastAccess(a.last_access, t)}</div>
                <MdChevronRight size={16} className="text-neutral-500" aria-hidden="true" />
              </div>
            ))
          )}
        </Card>
      )}

      <Pager page={page} totalPages={totalPages} onChange={setPage} />

      <RadixDialog.Root
        open={selected !== null && removeTarget === null}
        onOpenChange={(open) => {
          if (!open) setSelectedId(null);
        }}
      >
        <RadixDialog.Portal>
          <RadixDialog.Overlay className="fixed inset-0 z-40 bg-black/60" />
          <RadixDialog.Content className="fixed right-0 top-0 z-50 flex h-full w-full max-w-md flex-col overflow-y-auto bg-surface p-6 shadow-lg outline-none">
            {selected ? (
              <div className="flex flex-1 flex-col gap-4">
                <div className="flex items-start justify-between">
                  <div className="flex flex-wrap items-center gap-1.5">
                    <AdminStatusTag status={selected.status} />
                    {selected.expired ? <Tag variant="critical">{t("admins.expiredTag")}</Tag> : null}
                  </div>
                  <RadixDialog.Close asChild>
                    <button type="button" aria-label={t("common.close")} className="cursor-pointer text-neutral-400 hover:text-text">
                      <MdClose size={18} aria-hidden="true" />
                    </button>
                  </RadixDialog.Close>
                </div>

                <div className="flex items-center gap-3">
                  <div className="grid h-11 w-11 flex-none place-items-center rounded-xl bg-accent-100 text-[15px] font-bold text-accent">
                    {initials(selected)}
                  </div>
                  <div className="min-w-0">
                    <RadixDialog.Title className="m-0 truncate text-[17px] font-bold text-text">
                      {selected.name || selected.email}
                    </RadixDialog.Title>
                    {selected.name ? (
                      <div className="truncate text-[13px] text-neutral-400">{selected.email}</div>
                    ) : null}
                  </div>
                </div>

                <div className="flex flex-col gap-1.5">
                  <span className="text-[12.5px] font-semibold text-neutral-400">{t("admins.roleFieldLabel")}</span>
                  <RoleBoxes value={selected.role} onChange={(role) => handleRoleChange(selected, role)} t={t} />
                  {roleError ? (
                    <p role="alert" className="text-xs text-critical">
                      {roleError}
                    </p>
                  ) : null}
                </div>

                <div>
                  <div className="mb-1 text-[10.5px] font-bold uppercase tracking-wide text-neutral-400">
                    {t("admins.table.lastAccess")}
                  </div>
                  <div className="text-sm font-semibold text-text">{formatLastAccess(selected.last_access, t)}</div>
                </div>

                <div className="flex-1" />

                <div className="flex gap-2.5">
                  {selected.status === "pending" ? (
                    <Button
                      type="button"
                      variant="secondary"
                      className="flex-1"
                      onClick={() => handleResend(selected)}
                      disabled={resendInvite.isPending}
                    >
                      {t("admins.resendInviteButton")}
                    </Button>
                  ) : null}
                  <Button
                    type="button"
                    variant="secondary"
                    className="!flex-1 !border-critical !text-critical hover:!bg-critical/10"
                    onClick={() => setRemoveTarget(selected)}
                  >
                    {t("admins.removeUserButton")}
                  </Button>
                </div>
              </div>
            ) : null}
          </RadixDialog.Content>
        </RadixDialog.Portal>
      </RadixDialog.Root>

      <Drawer
        open={inviteOpen}
        onOpenChange={setInviteOpen}
        title={t("admins.invite.title")}
        description={t("admins.invite.description")}
        closeLabel={t("common.close")}
        footer={
          <>
            <Button
              type="button"
              variant="secondary"
              style={drawerFooterSecondaryStyle}
              onClick={() => setInviteOpen(false)}
            >
              {t("common.cancel")}
            </Button>
            <Button
              type="submit"
              form="invite-user-form"
              variant="solid"
              style={drawerFooterPrimaryStyle}
              disabled={inviteAdmin.isPending}
            >
              {t("admins.invite.submitButton")}
            </Button>
          </>
        }
      >
        <form id="invite-user-form" onSubmit={handleInvite} className="flex flex-col gap-3">
          <Field
            variant="filled"
            label={t("admins.invite.nameLabel")}
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
          />
          <Field
            variant="filled"
            label={t("admins.invite.emailLabel")}
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            placeholder={t("admins.invite.emailPlaceholder")}
            required
          />
          <PhoneField label={t("admins.invite.phoneLabel")} variant="filled" onChange={setPhone} />
          <div className="flex flex-col gap-1.5">
            <span className="text-sm font-medium text-text">{t("admins.roleFieldLabel")}</span>
            <RoleBoxes value={newRole} onChange={setNewRole} t={t} />
          </div>
          {inviteError ? (
            <p role="alert" className="text-xs text-critical">
              {inviteError}
            </p>
          ) : null}
        </form>
      </Drawer>

      <Dialog
        open={removeTarget !== null}
        onOpenChange={(open) => {
          if (!open) setRemoveTarget(null);
        }}
        title={t("admins.remove.title")}
        description={
          removeTarget ? t("admins.remove.description", { email: removeTarget.email }) : undefined
        }
        footer={
          <>
            <Button variant="secondary" onClick={() => setRemoveTarget(null)}>
              {t("common.cancel")}
            </Button>
            <Button
              variant="solid"
              className="!border-critical !bg-critical hover:!bg-critical"
              onClick={confirmRemove}
              disabled={deleteAdmin.isPending || cancelInvite.isPending}
            >
              {t("admins.remove.confirmButton")}
            </Button>
          </>
        }
      >
        {removeError ? (
          <p role="alert" className="text-xs text-critical">
            {removeError}
          </p>
        ) : null}
      </Dialog>
    </div>
  );
}
