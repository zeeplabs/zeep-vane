import { useState, type FormEvent } from "react";
import { Table, type TableColumn } from "../../components/ui/Table";
import { Dialog } from "../../components/ui/Dialog";
import { Button } from "../../components/ui/Button";
import { Field } from "../../components/ui/Field";
import { Tag } from "../../components/ui/Tag";
import { IconRoleSelector } from "../../components/ui/IconRoleSelector";
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

  const [cancelTarget, setCancelTarget] = useState<AdminRow | null>(null);

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
      await deleteAdmin.mutateAsync(removeTarget.id);
      setRemoveTarget(null);
    } catch (err) {
      setRemoveError(err instanceof ApiError ? err.message : "Não foi possível remover o admin.");
    }
  }

  async function confirmCancelInvite() {
    if (!cancelTarget) return;
    await cancelInvite.mutateAsync(cancelTarget.id);
    setCancelTarget(null);
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
        <Button variant="ghost" onClick={() => setRemoveTarget(a)}>
          Remover
        </Button>
      ),
    },
  ];

  const pendingColumns: TableColumn<AdminRow>[] = [
    { key: "email", header: "E-mail", render: (a) => a.email },
    { key: "role", header: "Papel", render: (a) => a.role },
    { key: "status", header: "", render: () => <Tag variant="accent-outline">Pendente</Tag> },
    {
      key: "actions",
      header: "",
      render: (a) => (
        <div className="flex gap-2">
          <Button variant="ghost" onClick={() => resendInvite.mutate(a.id)}>
            Reenviar
          </Button>
          <Button variant="ghost" onClick={() => setCancelTarget(a)}>
            Cancelar
          </Button>
        </div>
      ),
    },
  ];

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <h3 className="text-text">Admins</h3>
        <Button variant="primary" onClick={() => setInviteOpen(true)}>
          Convidar admin
        </Button>
      </div>

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

      {pending.length > 0 ? (
        <div className="flex flex-col gap-2">
          <h4 className="text-text">Convites pendentes</h4>
          <Table columns={pendingColumns} rows={pending} rowKey={(a) => a.id} />
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

      <Dialog
        open={cancelTarget !== null}
        onOpenChange={(open) => {
          if (!open) setCancelTarget(null);
        }}
        title="Cancelar convite"
        description={
          cancelTarget
            ? `Cancelar o convite de ${cancelTarget.email}? Esta ação não pode ser desfeita.`
            : undefined
        }
      >
        <div className="flex justify-end gap-2">
          <Button variant="secondary" onClick={() => setCancelTarget(null)}>
            Voltar
          </Button>
          <Button variant="primary" onClick={confirmCancelInvite}>
            Cancelar convite
          </Button>
        </div>
      </Dialog>
    </div>
  );
}
