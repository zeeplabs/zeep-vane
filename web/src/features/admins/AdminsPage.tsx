import { useState, type FormEvent } from "react";
import { toast } from "sonner";
import { Table, type TableColumn } from "../../components/ui/Table";
import { Dialog } from "../../components/ui/Dialog";
import { Button } from "../../components/ui/Button";
import { Field } from "../../components/ui/Field";
import { Tag } from "../../components/ui/Tag";
import { IconRoleSelector } from "../../components/ui/IconRoleSelector";
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

export function AdminsPage() {
  const { data: admins, isLoading } = useAdmins();
  const inviteAdmin = useInviteAdmin();
  const updateRole = useUpdateAdminRole();
  const deleteAdmin = useDeleteAdmin();
  const resendInvite = useResendInvite();
  const cancelInvite = useCancelInvite();

  const [inviteOpen, setInviteOpen] = useState(false);
  const [email, setEmail] = useState("");
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
      await inviteAdmin.mutateAsync({ email, role });
      setEmail("");
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

  const activeColumns: TableColumn<AdminRow>[] = [
    { key: "email", header: "E-mail", render: (a) => a.email },
    {
      key: "role",
      header: "Papel",
      render: (a) => (
        <IconRoleSelector
          role={a.role}
          onSelect={(newRole) => {
            if (newRole === a.role) return;
            setRoleError(null);
            setPendingRoleChange({ id: a.id, role: newRole });
          }}
        />
      ),
    },
    {
      key: "actions",
      header: "",
      render: (a) => (
        <div className="flex justify-end">
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
      ),
    },
  ];

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

  const pendingColumns: TableColumn<AdminRow>[] = [
    { key: "email", header: "E-mail", render: (a) => a.email },
    { key: "role", header: "Papel", render: (a) => a.role },
    {
      key: "status",
      header: "",
      render: (a) => (
        <div className="flex justify-end gap-1">
          <Tag variant="accent-outline">Pendente</Tag>
          {a.expired ? <Tag variant="critical">Expirado</Tag> : null}
        </div>
      ),
    },
    {
      key: "actions",
      header: "",
      render: (a) => (
        <div className="flex justify-end gap-2">
          <Button variant="ghost" onClick={() => handleResend(a)} disabled={resendInvite.isPending}>
            Reenviar
          </Button>
          <Button variant="ghost" onClick={() => handleCancel(a)} disabled={cancelInvite.isPending}>
            Cancelar
          </Button>
        </div>
      ),
    },
  ];

  return (
    <div className="mx-auto flex w-full max-w-[1280px] flex-col gap-6">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-text">Equipe</h2>
          <p className="m-0 text-[13.5px] text-neutral-400">
            Gerencie quem tem acesso ao painel e com qual papel.
          </p>
        </div>
        <Button variant="primary" onClick={() => setInviteOpen(true)}>
          <PlusIcon />
          Convidar admin
        </Button>
      </div>

      <div className="flex flex-col gap-4">
        <h4 className="text-text">Ativos</h4>
        <div className="mt-2">
          {isLoading ? (
            <p className="text-neutral-400">Carregando…</p>
          ) : (
            <Table
              columns={activeColumns}
              rows={active}
              rowKey={(a) => a.id}
              emptyMessage="Nenhum admin cadastrado."
            />
          )}
        </div>
      </div>

      {pending.length > 0 ? (
        <div className="flex flex-col gap-4">
          <h4 className="text-text">Convites pendentes</h4>
          <div className="mt-2">
            <Table columns={pendingColumns} rows={pending} rowKey={(a) => a.id} />
          </div>
        </div>
      ) : null}

      <Dialog open={inviteOpen} onOpenChange={setInviteOpen} title="Convidar admin">
        <form onSubmit={handleInvite} className="flex flex-col gap-3">
          <Field label="E-mail" type="email" value={email} onChange={(e) => setEmail(e.target.value)} required />
          <div className="flex flex-col gap-1">
            <label htmlFor="invite-role" className="text-sm font-medium text-text">
              Papel
            </label>
            <select
              id="invite-role"
              value={role}
              onChange={(e) => setRole(e.target.value as Role)}
              className="min-h-9 rounded-md border border-divider bg-surface px-3 text-sm text-text"
            >
              {roleOptions.map((r) => (
                <option key={r} value={r}>
                  {r}
                </option>
              ))}
            </select>
          </div>
          {inviteError ? (
            <p role="alert" className="text-xs text-critical">
              {inviteError}
            </p>
          ) : null}
          <div className="flex gap-2">
            <Button type="submit" variant="primary" disabled={inviteAdmin.isPending}>
              Enviar convite
            </Button>
            <Button type="button" variant="secondary" onClick={() => setInviteOpen(false)}>
              Cancelar
            </Button>
          </div>
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
      >
        {roleError ? (
          <p role="alert" className="mb-2 text-xs text-critical">
            {roleError}
          </p>
        ) : null}
        <div className="flex justify-end gap-2">
          <Button variant="secondary" onClick={() => setPendingRoleChange(null)}>
            Cancelar
          </Button>
          <Button variant="primary" onClick={confirmRoleChange} disabled={updateRole.isPending}>
            Confirmar
          </Button>
        </div>
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
      >
        {removeError ? (
          <p role="alert" className="mb-2 text-xs text-critical">
            {removeError}
          </p>
        ) : null}
        <div className="flex justify-end gap-2">
          <Button variant="secondary" onClick={() => setRemoveTarget(null)}>
            Cancelar
          </Button>
          <Button variant="primary" onClick={confirmRemove} disabled={deleteAdmin.isPending}>
            Remover
          </Button>
        </div>
      </Dialog>
    </div>
  );
}
