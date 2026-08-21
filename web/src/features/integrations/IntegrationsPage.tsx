import { useState, type FormEvent } from "react";
import { Card } from "../../components/ui/Card";
import { Field } from "../../components/ui/Field";
import { Button } from "../../components/ui/Button";
import { Tag } from "../../components/ui/Tag";
import { useAuth } from "../../auth/AuthProvider";
import { ApiError } from "../../lib/apiClient";
import { ServicesSection } from "../services/ServicesSection";
import { useConnectDatadog, useIntegrationStatus } from "./hooks";

function DatadogIcon() {
  return (
    <svg width="19" height="19" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M9 2v4M15 2v4M7 8h2v4a3 3 0 0 0 3 3 3 3 0 0 0 3-3V8h2M12 15v4M9 22h6" />
    </svg>
  );
}

function formatTimestamp(iso: string | null | undefined): string {
  if (!iso) return "-";
  return new Date(iso).toLocaleString("pt-BR");
}

export function IntegrationsPage() {
  const { hasRole } = useAuth();
  const canManage = hasRole(["owner", "operator"]);
  const { data, isLoading } = useIntegrationStatus();
  const connectMutation = useConnectDatadog();

  const [formOpen, setFormOpen] = useState(false);
  const [apiKey, setApiKey] = useState("");
  const [appKey, setAppKey] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [lastMaskedKey, setLastMaskedKey] = useState<string | null>(null);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    try {
      const res = await connectMutation.mutateAsync({ api_key: apiKey, app_key: appKey });
      setLastMaskedKey(res.masked_key ?? null);
      setApiKey("");
      setAppKey("");
      setFormOpen(false);
    } catch (err) {
      if (err instanceof ApiError) setError(err.message);
      else setError("Não foi possível conectar ao Datadog.");
    }
  }

  if (isLoading) {
    return <p className="text-neutral-400">Carregando…</p>;
  }

  const connected = data?.connected ?? false;

  return (
    <div className="mx-auto flex w-full max-w-[1280px] flex-col gap-6">
      <div>
        <h2 className="text-text">Integrações</h2>
        <p className="m-0 text-[13.5px] text-neutral-400">
          Conecte o Datadog e vincule SLOs aos serviços monitorados.
        </p>
      </div>

      {!connected && !formOpen ? (
        <Card elevation="elev-sm" className="flex flex-col gap-3 p-4">
          <div className="flex items-center gap-3">
            <div className="grid h-[38px] w-[38px] flex-none place-items-center rounded-[10px] bg-accent-800 text-accent-200">
              <DatadogIcon />
            </div>
            <div className="flex-1">
              <div className="text-[15px] font-medium text-text">Datadog</div>
              <div className="text-xs text-neutral-400">Nenhuma integração conectada</div>
            </div>
            {canManage ? (
              <Button variant="primary" onClick={() => setFormOpen(true)}>
                Conectar Datadog
              </Button>
            ) : null}
          </div>
        </Card>
      ) : null}

      {connected && !formOpen ? (
        <Card elevation="elev-sm" className="flex flex-col gap-3 p-4">
          <div className="flex items-center gap-3">
            <div className="grid h-[38px] w-[38px] flex-none place-items-center rounded-[10px] bg-accent-800 text-accent-200">
              <DatadogIcon />
            </div>
            <div className="flex-1">
              <div className="text-[15px] font-medium text-text">Datadog</div>
              <div className="text-xs text-neutral-400">
                Chave: {lastMaskedKey ?? data?.masked_key ?? "•••• •••• •••• ????"}
                {"  ·  "}
                Última verificação: {formatTimestamp(data?.last_checked_at)}
              </div>
            </div>
            <Tag variant="success">Conectado</Tag>
            {canManage ? (
              <Button variant="secondary" onClick={() => setFormOpen(true)}>
                Rotacionar chave
              </Button>
            ) : null}
          </div>
        </Card>
      ) : null}

      {formOpen && canManage ? (
        <Card elevation="elev-sm" className="flex flex-col gap-3 p-4">
          <div className="flex items-center gap-3">
            <div className="grid h-[38px] w-[38px] flex-none place-items-center rounded-[10px] bg-accent-800 text-accent-200">
              <DatadogIcon />
            </div>
            <div className="flex-1 text-[15px] font-medium text-text">
              {connected ? "Rotacionar chave" : "Conectar Datadog"}
            </div>
          </div>
          <div className="h-px bg-divider" />
          <form onSubmit={handleSubmit} className="flex flex-col gap-3">
            <div className="grid grid-cols-2 gap-3">
              <Field
                label="Nova API key"
                type="password"
                value={apiKey}
                onChange={(e) => setApiKey(e.target.value)}
                placeholder="Chave da API"
                required
              />
              <Field
                label="Nova App key"
                type="password"
                value={appKey}
                onChange={(e) => setAppKey(e.target.value)}
                placeholder="Chave da aplicação"
                required
              />
            </div>
            <p className="m-0 text-[11.5px] text-neutral-400">
              As chaves são validadas contra o Datadog e criptografadas em repouso. Não são reexibidas após salvar.
            </p>
            {error ? (
              <p role="alert" className="text-xs text-critical">
                {error}
              </p>
            ) : null}
            <div className="flex justify-end gap-2">
              <Button type="button" variant="secondary" onClick={() => setFormOpen(false)}>
                Cancelar
              </Button>
              <Button type="submit" variant="primary" disabled={connectMutation.isPending}>
                Salvar chave
              </Button>
            </div>
          </form>
        </Card>
      ) : null}

      <ServicesSection />
    </div>
  );
}
