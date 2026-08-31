import { useState } from "react";
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

export function PollerStatusPage() {
  const [page, setPage] = useState(1);
  const { data, isLoading } = usePollerStatus(page);
  const totalPages = Math.max(1, Math.ceil((data?.total ?? 0) / (data?.page_size ?? 20)));
  const items = data?.items ?? [];

  return (
    <div className="mx-auto flex w-full max-w-[1280px] flex-col gap-6">
      <div>
        <h2 className="text-text">Status do poller</h2>
        <p className="m-0 text-[13.5px] text-neutral-400">
          Última execução de cada integração conectada — apenas leitura.
        </p>
      </div>
      <div>
        {isLoading ? (
          <p className="text-neutral-400">Carregando…</p>
        ) : (
          <>
            <Card elevation="elev-sm" className="divide-y divide-divider overflow-hidden">
              {items.length === 0 ? (
                <p className="px-4 py-6 text-center text-neutral-400">Nenhuma integração conectada.</p>
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
                      {e.status !== "active" ? (
                        <div className="mt-0.5 text-xs text-neutral-400">{e.last_error}</div>
                      ) : null}
                    </div>
                    <div className="text-right text-xs text-neutral-400">
                      <div>Última execução</div>
                      <div className="mt-0.5 text-[13px] text-text">{formatTimestamp(e.last_checked_at)}</div>
                    </div>
                    {e.status === "active" ? (
                      <Tag variant="success">Sucesso</Tag>
                    ) : (
                      <Tag variant="critical">Falha</Tag>
                    )}
                  </div>
                ))
              )}
            </Card>
            <Pager page={page} totalPages={totalPages} onChange={setPage} />
          </>
        )}
      </div>
    </div>
  );
}
