import { Table, type TableColumn } from "../../components/ui/Table";
import { Tag } from "../../components/ui/Tag";
import type { PollerStatusEntry } from "../../types/api";
import { usePollerStatus } from "./hooks";

function formatTimestamp(iso: string | null): string {
  if (!iso) return "-";
  return new Date(iso).toLocaleString("pt-BR");
}

function providerLabel(provider: string): string {
  return provider.charAt(0).toUpperCase() + provider.slice(1);
}

export function PollerStatusPage() {
  const { data, isLoading } = usePollerStatus();

  const columns: TableColumn<PollerStatusEntry>[] = [
    {
      key: "provider",
      header: "Integração",
      render: (e) => <span className="font-medium text-text">{providerLabel(e.provider)}</span>,
    },
    {
      key: "last_checked_at",
      header: "Última execução",
      render: (e) => formatTimestamp(e.last_checked_at),
    },
    {
      key: "result",
      header: "Resultado",
      render: (e) =>
        e.status === "active" ? (
          <Tag variant="success">Sucesso</Tag>
        ) : (
          <Tag variant="critical">Falha</Tag>
        ),
    },
    {
      key: "error",
      header: "Mensagem de erro",
      render: (e) => (e.status !== "active" ? e.last_error : ""),
    },
  ];

  return (
    <div className="mx-auto flex w-full max-w-[1280px] flex-col gap-4">
      <div>
        <h2 className="text-text">Status do poller</h2>
        <p className="m-0 text-[13.5px] text-neutral-400">
          Última execução de cada integração conectada — apenas leitura.
        </p>
      </div>
      <div className="mt-2">
        {isLoading ? (
          <p className="text-neutral-400">Carregando…</p>
        ) : (
          <Table columns={columns} rows={data ?? []} rowKey={(e) => e.provider} />
        )}
      </div>
    </div>
  );
}
