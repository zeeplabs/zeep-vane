import { useState, type FormEvent } from "react";
import { Card } from "../../components/ui/Card";
import { Field } from "../../components/ui/Field";
import { Button } from "../../components/ui/Button";
import { Pager } from "../../components/ui/Pager";
import { Tag } from "../../components/ui/Tag";
import { useAuth } from "../../auth/AuthProvider";
import { ApiError } from "../../lib/apiClient";
import {
  useActivateEmailProvider,
  useConnectEmailProvider,
  useEmailProviders,
  type EmailProviderName,
  type EmailProviderStatus,
} from "./hooks";

const PROVIDERS: { id: EmailProviderName; label: string }[] = [
  { id: "sendgrid", label: "SendGrid" },
  { id: "resend", label: "Resend" },
];

function formatTimestamp(iso: string | null | undefined): string {
  if (!iso) return "-";
  return new Date(iso).toLocaleString("pt-BR");
}

interface ProviderRowProps {
  id: EmailProviderName;
  label: string;
  status?: EmailProviderStatus;
  isActive: boolean;
  canManage: boolean;
}

function ProviderRow({ id, label, status, isActive, canManage }: ProviderRowProps) {
  const connectMutation = useConnectEmailProvider(id);
  const activateMutation = useActivateEmailProvider();

  const [formOpen, setFormOpen] = useState(false);
  const [apiKey, setApiKey] = useState("");
  const [fromEmail, setFromEmail] = useState("");
  const [fromName, setFromName] = useState("");
  const [error, setError] = useState<string | null>(null);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    try {
      await connectMutation.mutateAsync({ api_key: apiKey, from_email: fromEmail, from_name: fromName });
      setApiKey("");
      setFromEmail("");
      setFromName("");
      setFormOpen(false);
    } catch (err) {
      if (err instanceof ApiError) setError(err.message);
      else setError(`Não foi possível conectar ao ${label}.`);
    }
  }

  async function handleActivate() {
    setError(null);
    try {
      await activateMutation.mutateAsync(id);
    } catch (err) {
      if (err instanceof ApiError) setError(err.message);
      else setError(`Não foi possível ativar o ${label}.`);
    }
  }

  return (
    <Card elevation="elev-sm" className="flex flex-col gap-3 p-4">
      <div className="flex items-center gap-3">
        <div className="flex-1">
          <div className="text-[15px] font-medium text-text">{label}</div>
          <div className="text-xs text-neutral-400">
            {status ? (
              <>
                {status.from_name} &lt;{status.from_email}&gt;
                {"  ·  "}
                Última verificação: {formatTimestamp(status.last_checked_at)}
                {status.status === "invalid" && status.last_error ? `  ·  ${status.last_error}` : ""}
              </>
            ) : (
              "Nenhuma integração conectada"
            )}
          </div>
        </div>

        {status?.status === "invalid" ? (
          <Tag variant="critical">Inválido</Tag>
        ) : status?.status === "connected" && isActive ? (
          <Tag variant="success">Ativo</Tag>
        ) : status?.status === "connected" ? (
          <Tag variant="accent-outline">Conectado</Tag>
        ) : (
          <Tag variant="neutral-outline">Não conectado</Tag>
        )}

        {canManage && status?.status === "connected" && !isActive ? (
          <Button variant="secondary" onClick={handleActivate} disabled={activateMutation.isPending}>
            Ativar
          </Button>
        ) : null}

        {canManage ? (
          <Button variant={status ? "secondary" : "primary"} onClick={() => setFormOpen((open) => !open)}>
            {status ? "Reconectar" : "Conectar"}
          </Button>
        ) : null}
      </div>

      {error ? (
        <p role="alert" className="text-xs text-critical">
          {error}
        </p>
      ) : null}

      {formOpen && canManage ? (
        <>
          <div className="h-px bg-divider" />
          <form onSubmit={handleSubmit} className="flex flex-col gap-3">
            <div className="grid grid-cols-3 gap-3">
              <Field
                label={`${label} API key`}
                type="password"
                value={apiKey}
                onChange={(e) => setApiKey(e.target.value)}
                placeholder="Chave da API"
                required
              />
              <Field
                label="E-mail do remetente"
                type="email"
                value={fromEmail}
                onChange={(e) => setFromEmail(e.target.value)}
                placeholder="remetente@dominio.com"
                required
              />
              <Field
                label="Nome do remetente"
                value={fromName}
                onChange={(e) => setFromName(e.target.value)}
                placeholder="Nome exibido no e-mail"
                required
              />
            </div>
            <p className="m-0 text-[11.5px] text-neutral-400">
              A chave é validada contra o provedor e criptografada em repouso. Não é reexibida após salvar.
            </p>
            <div className="flex justify-end gap-2">
              <Button type="button" variant="secondary" onClick={() => setFormOpen(false)}>
                Cancelar
              </Button>
              <Button type="submit" variant="primary" disabled={connectMutation.isPending}>
                Salvar
              </Button>
            </div>
          </form>
        </>
      ) : null}
    </Card>
  );
}

export function EmailProvidersPage() {
  const { hasRole } = useAuth();
  const canManage = hasRole(["owner", "operator"]);
  const [page, setPage] = useState(1);
  const { data, isLoading } = useEmailProviders(page);

  if (isLoading) {
    return <p className="text-neutral-400">Carregando…</p>;
  }

  const byProvider = new Map(data?.providers.map((p) => [p.provider, p]));
  const totalPages = Math.max(1, Math.ceil((data?.total ?? 0) / (data?.page_size ?? 20)));

  return (
    <div className="mx-auto flex w-full max-w-[1280px] flex-col gap-6">
      <div>
        <h2 className="text-text">Provedores de e-mail</h2>
        <p className="m-0 text-[13.5px] text-neutral-400">
          Conecte SendGrid e/ou Resend para enviar e-mails transacionais (ex.: convites de administrador).
        </p>
      </div>

      {PROVIDERS.map(({ id, label }) => (
        <ProviderRow
          key={id}
          id={id}
          label={label}
          status={byProvider.get(id)}
          isActive={data?.active_provider === id}
          canManage={canManage}
        />
      ))}

      <Pager page={page} totalPages={totalPages} onChange={setPage} />
    </div>
  );
}
