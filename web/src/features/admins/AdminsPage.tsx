import { useState, type FormEvent } from "react";
import { toast } from "sonner";
import { Card } from "../../components/ui/Card";
import { Dialog } from "../../components/ui/Dialog";
import { Button } from "../../components/ui/Button";
import { Field } from "../../components/ui/Field";
import { PhoneField } from "../../components/ui/PhoneField";
import { Tag } from "../../components/ui/Tag";
import { IconRoleSelector } from "../../components/ui/IconRoleSelector";
import { Pager } from "../../components/ui/Pager";
import { Seg } from "../../components/ui/Seg";
import { Tooltip } from "../../components/ui/Tooltip";
import { ApiError } from "../../lib/apiClient";
import type { Role } from "../../types/api";
import {
  useAdmins,
  useCancelInvite,
  useDeleteAdmin,
  useInviteAdmin,
  useResendInvite,
  useUpdateAdminRole,
  type AdminRow,
} from "./hooks";

const roleOptions: Role[] = ["owner", "operator", "viewer"];

function PlusIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
      <path d="M12 5v14M5 12h14" />
    </svg>
  );
}

function TrashIcon() {
  return (
    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13" />
    </svg>
  );
}

function EnvelopeIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <rect x="3" y="5" width="18" height="14" rx="2" />
      <path d="M3 7l9 6 9-6" />
    </svg>
  );
}

function initial(a: AdminRow): string {
  return (a.name || a.email).charAt(0).toUpperCase();
}

const roleLabel: Record<Role, string> = { owner: "Owner", operator: "Operator", viewer: "Viewer" };

export function AdminsPage() {
  const [page, setPage] = useState(1);
  const { data: adminsPage, isLoading } = useAdmins(page);
  const admins = adminsPage?.items;
  const totalPages = Math.max(1, Math.ceil((adminsPage?.total ?? 0) / (adminsPage?.page_size ?? 20)));
  const inviteAdmin = useInviteAdmin();
  const updateRole = useUpdateAdminRole();
  const deleteAdmin = useDeleteAdmin();
  const resendInvite = useResendInvite();
  const cancelInvite = useCancelInvite();

  const [inviteOpen, setInviteOpen] = useState(false);
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [phone, setPhone] = useState("");
  const [role, setRole] = useState<Role>("viewer");
  const [inviteError, setInviteError] = useState<string | null>(null);

  const [pendingRoleChange, setPendingRoleChange] = useState<{ id: string; role: Role } | null>(
    null
  );
  const [roleError, setRoleError] = useState<string | null>(null);

  const [removeTarget, setRemoveTarget] = useState<AdminRow | null>(null);
  const [removeError, setRemoveError] = useState<string | null>(null);

  const active = (admins ?? []).filter((a) => a.status === "active");
  const pending = (admins ?? []).filter((a) => a.status === "pending");

  async function handleInvite(e: FormEvent) {
    e.preventDefault();
    setInviteError(null);
    try {
      await inviteAdmin.mutateAsync({ name, email, phone: phone || undefined, role });
      setName("");
      setEmail("");
      setPhone("");
      setRole("viewer");
      setInviteOpen(false);
    } catch (err) {
      setInviteError(err instanceof ApiError ? err.message : "Não foi possível enviar o convite.");
    }
  }

  async function confirmRoleChange() {
    if (!pendingRoleChange) return;
    setRoleError(null);
    try {
      await updateRole.mutateAsync(pendingRoleChange);
      setPendingRoleChange(null);
    } catch (err) {
      setRoleError(err instanceof ApiError ? err.message : "Não foi possível alterar o papel.");
    }
  }

  async function confirmRemove() {
    if (!removeTarget) return;
    setRemoveError(null);
    try {
      const removedEmail = removeTarget.email;
      await deleteAdmin.mutateAsync(removeTarget.id);
      setRemoveTarget(null);
      toast.success(`Acesso de ${removedEmail} removido.`);
    } catch (err) {
      setRemoveError(err instanceof ApiError ? err.message : "Não foi possível remover o admin.");
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

  async function handleCancel(a: AdminRow) {
    try {
      await cancelInvite.mutateAsync(a.id);
      toast.success(`Convite de ${a.email} cancelado.`);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "Não foi possível cancelar o convite.");
    }
  }

  return (
    <div className="mx-auto flex w-full max-w-[1280px] flex-col gap-6">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-text">Admins</h2>
          <p className="m-0 text-[13.5px] text-neutral-400">
            Gerencie quem tem acesso ao painel e com qual papel.
          </p>
        </div>
        <Button variant="primary" onClick={() => setInviteOpen(true)}>
          <PlusIcon />
          Convidar admin
        </Button>
      </div>

      <div className="flex flex-col gap-3">
        <h4 className="text-text">Ativos</h4>
        {isLoading ? (
          <p className="text-neutral-400">Carregando…</p>
        ) : (
          <Card elevation="elev-sm" className="divide-y divide-divider overflow-hidden">
            {active.length === 0 ? (
              <p className="px-4 py-6 text-center text-neutral-400">Nenhum admin cadastrado.</p>
            ) : (
              active.map((a) => (
                <div key={a.id} data-testid="admin-row" className="flex items-center gap-3 px-4 py-3.5">
                  <div className="grid h-9 w-9 flex-none place-items-center rounded-full bg-neutral-800 text-sm font-medium text-neutral-300">
                    {initial(a)}
                  </div>
                  <div className="flex-1 text-[15px] font-medium text-text">{a.email}</div>
                  <IconRoleSelector
                    role={a.role}
                    onSelect={(newRole) => {
                      if (newRole === a.role) return;
                      setRoleError(null);
                      setPendingRoleChange({ id: a.id, role: newRole });
                    }}
                  />
                  <Tooltip label="Remover">
                    <Button
                      variant="ghost"
                      aria-label="Remover"
                      className="text-neutral-400 hover:text-critical"
                      onClick={() => setRemoveTarget(a)}
                    >
                      <TrashIcon />
                    </Button>
                  </Tooltip>
                </div>
              ))
            )}
          </Card>
        )}
        <Pager page={page} totalPages={totalPages} onChange={setPage} />
      </div>

      {pending.length > 0 ? (
        <div className="flex flex-col gap-3">
          <h4 className="text-text">Convites pendentes</h4>
          <Card elevation="elev-sm" className="divide-y divide-divider overflow-hidden">
            {pending.map((a) => (
              <div key={a.id} data-testid="invite-row" className="flex items-center gap-3 px-4 py-3.5">
                <div className="grid h-9 w-9 flex-none place-items-center rounded-full bg-neutral-800 text-neutral-300">
                  <EnvelopeIcon />
                </div>
                <div className="flex-1">
                  <div className="text-[15px] font-medium text-text">{a.email}</div>
                  <div className="mt-0.5 text-xs text-neutral-400">{roleLabel[a.role]}</div>
                </div>
                <Tag variant="accent-outline">Pendente</Tag>
                {a.expired ? <Tag variant="critical">Expirado</Tag> : null}
                <Button variant="ghost" onClick={() => handleResend(a)} disabled={resendInvite.isPending}>
                  Reenviar
                </Button>
                <Button variant="ghost" onClick={() => handleCancel(a)} disabled={cancelInvite.isPending}>
                  Cancelar
                </Button>
              </div>
            ))}
          </Card>
          <Pager page={page} totalPages={totalPages} onChange={setPage} />
        </div>
      ) : null}

      <Dialog
        open={inviteOpen}
        onOpenChange={setInviteOpen}
        title="Convidar admin"
        footer={
          <>
            <Button type="button" variant="secondary" onClick={() => setInviteOpen(false)}>
              Cancelar
            </Button>
            <Button type="submit" form="invite-admin-form" variant="primary" disabled={inviteAdmin.isPending}>
              Enviar convite
            </Button>
          </>
        }
      >
        <form id="invite-admin-form" onSubmit={handleInvite} className="flex flex-col gap-3">
          <Field label="Nome" value={name} onChange={(e) => setName(e.target.value)} required />
          <Field label="E-mail" type="email" value={email} onChange={(e) => setEmail(e.target.value)} required />
          <PhoneField label="Celular (opcional)" onChange={setPhone} />
          <div className="flex flex-col gap-1">
            <span className="text-sm font-medium text-text">Papel</span>
            <Seg
              aria-label="Papel"
              options={roleOptions.map((r) => ({ value: r, label: roleLabel[r] }))}
              value={role}
              onChange={(v) => setRole(v as Role)}
            />
          </div>
          {inviteError ? (
            <p role="alert" className="text-xs text-critical">
              {inviteError}
            </p>
          ) : null}
        </form>
      </Dialog>

      <Dialog
        open={pendingRoleChange !== null}
        onOpenChange={(open) => {
          if (!open) setPendingRoleChange(null);
        }}
        title="Alterar papel"
        description={
          pendingRoleChange
            ? `Alterar o papel deste admin para ${pendingRoleChange.role}?`
            : undefined
        }
        footer={
          <>
            <Button variant="secondary" onClick={() => setPendingRoleChange(null)}>
              Cancelar
            </Button>
            <Button variant="primary" onClick={confirmRoleChange} disabled={updateRole.isPending}>
              Confirmar
            </Button>
          </>
        }
      >
        {roleError ? (
          <p role="alert" className="text-xs text-critical">
            {roleError}
          </p>
        ) : null}
      </Dialog>

      <Dialog
        open={removeTarget !== null}
        onOpenChange={(open) => {
          if (!open) setRemoveTarget(null);
        }}
        title="Remover admin"
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
            <Button variant="primary" onClick={confirmRemove} disabled={deleteAdmin.isPending}>
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
