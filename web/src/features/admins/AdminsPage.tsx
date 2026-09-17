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
const roleFilters: RoleFilter[] = ["all", "owner", "operator", "viewer"];
const filterLabel: Record<RoleFilter, string> = { all: "Todos", ...adminRoleLabel };

const roleOptions: { value: Role; label: string; description: string }[] = [
  { value: "owner", label: "Admin", description: "Acesso total, inclusive faturamento e integrações" },
  { value: "operator", label: "Membro", description: "Gerencia serviços e incidentes, sem acesso à cobrança" },
  { value: "viewer", label: "Somente leitura", description: "Visualiza painéis e incidentes, sem editar nada" },
];

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

function RoleBoxes({ value, onChange }: { value: Role; onChange: (r: Role) => void }) {
  return (
    <div role="radiogroup" aria-label="Papel" className="flex flex-col gap-2">
      {roleOptions.map((opt) => {
        const active = value === opt.value;
        return (
          <button
            key={opt.value}
            type="button"
            role="radio"
            aria-checked={active}
            onClick={() => onChange(opt.value)}
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
            <div className="mb-0.5 text-[13px] font-bold">{opt.label}</div>
            <div className="text-[11.5px] leading-snug opacity-80">{opt.description}</div>
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
      setInviteError(err instanceof ApiError ? err.message : "Não foi possível enviar o convite.");
    }
  }

  async function handleRoleChange(a: AdminRow, role: Role) {
    if (role === a.role) return;
    setRoleError(null);
    try {
      await updateRole.mutateAsync({ id: a.id, role });
    } catch (err) {
      setRoleError(err instanceof ApiError ? err.message : "Não foi possível alterar o papel.");
    }
  }

  async function confirmRemove() {
    if (!removeTarget) return;
    setRemoveError(null);
    try {
      if (removeTarget.status === "pending") {
        await cancelInvite.mutateAsync(removeTarget.id);
        toast.success(`Convite de ${removeTarget.email} cancelado.`);
      } else {
        await deleteAdmin.mutateAsync(removeTarget.id);
        toast.success(`Acesso de ${removeTarget.email} removido.`);
      }
      setRemoveTarget(null);
      setSelectedId(null);
    } catch (err) {
      setRemoveError(err instanceof ApiError ? err.message : "Não foi possível remover o usuário.");
    }
  }

  async function handleResend(a: AdminRow) {
    try {
      const result = await resendInvite.mutateAsync(a.id);
      toast.success(
        result.email_sent
          ? `Convite reenviado para ${a.email}.`
          : `Convite reenviado para ${a.email}, mas o e-mail não pôde ser entregue.`
      );
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "Não foi possível reenviar o convite.");
    }
  }

  return (
    <div className="mx-auto flex w-full max-w-[1280px] flex-col gap-5">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h2 className="text-text">Usuários</h2>
          <p className="m-0 text-[13.5px] text-neutral-400">Gerencie quem tem acesso ao tenant e o papel de cada pessoa.</p>
        </div>
        <Button variant="solid" onClick={() => setInviteOpen(true)}>
          <PlusIcon />
          Convidar usuário
        </Button>
      </div>

      <div className="flex flex-wrap items-center justify-between gap-3">
        <div role="group" aria-label="Filtrar por papel" className="flex flex-wrap items-center gap-2">
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
                {filterLabel[value]}
                <span className="opacity-70">{counts[value]}</span>
              </button>
            );
          })}
        </div>
        <input
          type="text"
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
          placeholder="Buscar por nome ou email"
          aria-label="Buscar por nome ou email"
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
            <span className="text-[11px] font-bold uppercase tracking-wide text-neutral-400">Usuário</span>
            <span className="text-[11px] font-bold uppercase tracking-wide text-neutral-400">Papel</span>
            <span className="text-[11px] font-bold uppercase tracking-wide text-neutral-400">Status</span>
            <span className="text-[11px] font-bold uppercase tracking-wide text-neutral-400">Último acesso</span>
            <span />
          </div>
          {filtered.length === 0 ? (
            <p className="px-5 py-12 text-center text-[13.5px] text-neutral-400">
              Nenhum usuário encontrado com esses filtros.
            </p>
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
                  {adminRoleLabel[a.role]}
                </Tag>
                <div className="flex flex-wrap items-center gap-1.5">
                  <AdminStatusTag status={a.status} />
                  {a.expired ? <Tag variant="critical">Expirado</Tag> : null}
                </div>
                <div className="text-[12.5px] text-neutral-400">{formatLastAccess(a.last_access)}</div>
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
                    {selected.expired ? <Tag variant="critical">Expirado</Tag> : null}
                  </div>
                  <RadixDialog.Close asChild>
                    <button type="button" aria-label="Fechar" className="cursor-pointer text-neutral-400 hover:text-text">
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
                  <span className="text-[12.5px] font-semibold text-neutral-400">Papel</span>
                  <RoleBoxes value={selected.role} onChange={(role) => handleRoleChange(selected, role)} />
                  {roleError ? (
                    <p role="alert" className="text-xs text-critical">
                      {roleError}
                    </p>
                  ) : null}
                </div>

                <div>
                  <div className="mb-1 text-[10.5px] font-bold uppercase tracking-wide text-neutral-400">
                    Último acesso
                  </div>
                  <div className="text-sm font-semibold text-text">{formatLastAccess(selected.last_access)}</div>
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
                      Reenviar convite
                    </Button>
                  ) : null}
                  <Button
                    type="button"
                    variant="secondary"
                    className="!flex-1 !border-critical !text-critical hover:!bg-critical/10"
                    onClick={() => setRemoveTarget(selected)}
                  >
                    Remover usuário
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
        title="Convidar usuário"
        description="Enviaremos um convite por email com um link de acesso a este tenant."
        closeLabel="Fechar"
        footer={
          <>
            <Button
              type="button"
              variant="secondary"
              style={drawerFooterSecondaryStyle}
              onClick={() => setInviteOpen(false)}
            >
              Cancelar
            </Button>
            <Button
              type="submit"
              form="invite-user-form"
              variant="solid"
              style={drawerFooterPrimaryStyle}
              disabled={inviteAdmin.isPending}
            >
              Enviar convite
            </Button>
          </>
        }
      >
        <form id="invite-user-form" onSubmit={handleInvite} className="flex flex-col gap-3">
          <Field variant="filled" label="Nome" value={name} onChange={(e) => setName(e.target.value)} required />
          <Field
            variant="filled"
            label="Email"
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            placeholder="nome@empresa.com"
            required
          />
          <PhoneField label="Celular (opcional)" variant="filled" onChange={setPhone} />
          <div className="flex flex-col gap-1.5">
            <span className="text-sm font-medium text-text">Papel</span>
            <RoleBoxes value={newRole} onChange={setNewRole} />
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
        title="Remover usuário"
        description={
          removeTarget
            ? `Remover o acesso de ${removeTarget.email}? Esta ação não pode ser desfeita.`
            : undefined
        }
        footer={
          <>
            <Button variant="secondary" onClick={() => setRemoveTarget(null)}>
              Cancelar
            </Button>
            <Button
              variant="solid"
              className="!border-critical !bg-critical hover:!bg-critical"
              onClick={confirmRemove}
              disabled={deleteAdmin.isPending || cancelInvite.isPending}
            >
              Remover
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
