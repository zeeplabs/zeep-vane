import { useState } from "react";
import { MdOutlineWarningAmber } from "react-icons/md";
import { Card } from "../../components/ui/Card";
import { Pager } from "../../components/ui/Pager";
import { Tag } from "../../components/ui/Tag";
import { usePollerStatus } from "./hooks";

function formatTimestamp(iso: string | null): string {
  if (!iso) return "-";
  return new Date(iso).toLocaleString("pt-BR");
}

function providerLabel(provider: string): string {
  return provider.charAt(0).toUpperCase() + provider.slice(1);
}

function failureMessage(providers: string[]): string {
  const labels = providers.map(providerLabel);
  if (labels.length === 1) {
    return `Falha ao verificar a integração ${labels[0]} — última tentativa não teve sucesso.`;
  }
  return `Falha ao verificar as integrações ${labels.join(" e ")} — última tentativa não teve sucesso.`;
}

function StatCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border border-divider p-4">
      <div className="mb-2 text-[10.5px] font-bold tracking-wide text-text-muted uppercase">{label}</div>
      <div className="text-[22px] font-bold text-text">{value}</div>
    </div>
  );
}

function AlertBanner({ children }: { children: React.ReactNode }) {
  return (
    <div
      className="mb-4 flex items-start gap-2.5 rounded-md border px-3.5 py-3 text-[12.5px] leading-normal text-text-muted"
      style={{
        backgroundColor: "color-mix(in oklch, var(--color-warning) 10%, transparent)",
        borderColor: "color-mix(in oklch, var(--color-warning) 40%, transparent)",
      }}
    >
      <MdOutlineWarningAmber size={16} className="mt-0.5 flex-none text-warning" aria-hidden="true" />
      <span>{children}</span>
    </div>
  );
}

export function PollerStatusPage() {
  const [page, setPage] = useState(1);
  const { data, isLoading, isError } = usePollerStatus(page);
  const totalPages = Math.max(1, Math.ceil((data?.total ?? 0) / (data?.page_size ?? 20)));
  const items = data?.items ?? [];
  const failing = items.filter((e) => e.status !== "active");

  const pollerStatusLabel = !data
    ? "-"
    : !data.leader_elected
      ? "Sem líder no momento"
      : data.poller_running
        ? "Ativo"
        : "Aguardando integração";
  const replicaName = data?.leader_elected ? (data.replica?.application_name ?? null) : null;

  return (
    <div className="mx-auto flex w-full max-w-[1280px] flex-col gap-6">
      <div>
        <h2 className="text-text">Status do poller</h2>
        <p className="m-0 text-[13.5px] text-text-muted">Estado real do poller ativo e das integrações conectadas.</p>
      </div>
      {isLoading ? (
        <p className="text-text-muted">Carregando…</p>
      ) : isError ? (
        <p className="text-text-muted">Não foi possível carregar o status do poller.</p>
      ) : (
        <>
          <div className="grid grid-cols-1 gap-3.5 sm:grid-cols-3">
            <StatCard label="Poller" value={pollerStatusLabel + (replicaName ? ` · ${replicaName}` : "")} />
            <StatCard label="Verificações/min" value={String(data?.checks_last_minute ?? 0)} />
            <StatCard label="Integrações conectadas" value={String(data?.total ?? 0)} />
          </div>

          {data && data.leader_elected && !data.poller_running ? (
            <AlertBanner>Réplica líder ativa, mas nenhuma integração Datadog conectada.</AlertBanner>
          ) : null}
          {failing.length > 0 ? (
            <AlertBanner>{failureMessage(failing.map((e) => e.provider))}</AlertBanner>
          ) : null}

          <Card elevation="none" className="divide-y divide-divider overflow-hidden border border-divider">
            {items.length === 0 ? (
              <p className="px-4 py-6 text-center text-text-muted">Nenhuma integração conectada.</p>
            ) : (
              items.map((e) => (
                <div key={e.provider} data-testid="poller-row" className="flex items-center gap-3 px-4 py-3.5">
                  <span
                    className="h-2 w-2 flex-none rounded-full"
                    style={{
                      backgroundColor: e.status === "active" ? "var(--color-success)" : "var(--color-critical)",
                    }}
                    aria-hidden="true"
                  />
                  <div className="flex-1">
                    <div className="text-[15px] font-medium text-text">{providerLabel(e.provider)}</div>
                    {e.status !== "active" ? <div className="mt-0.5 text-xs text-text-muted">{e.last_error}</div> : null}
                  </div>
                  <div className="text-right text-xs text-text-muted">
                    <div>Última execução</div>
                    <div className="mt-0.5 text-[13px] text-text">{formatTimestamp(e.last_checked_at)}</div>
                  </div>
                  {e.status === "active" ? <Tag variant="success">Sucesso</Tag> : <Tag variant="critical">Falha</Tag>}
                </div>
              ))
            )}
          </Card>
          <Pager page={page} totalPages={totalPages} onChange={setPage} />
        </>
      )}
    </div>
  );
}
