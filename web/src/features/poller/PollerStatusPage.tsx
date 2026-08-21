import { Table, type TableColumn } from "../../components/ui/Table";
import { Tag } from "../../components/ui/Tag";
import type { PollerStatusEntry } from "../../types/api";
import { usePollerStatus } from "./hooks";

function formatTimestamp(iso: string | null): string {
  if (!iso) return "-";
  return new Date(iso).toLocaleString("pt-BR");
}

export function PollerStatusPage() {
  const { data, isLoading } = usePollerStatus();

  const columns: TableColumn<PollerStatusEntry>[] = [
    { key: "provider", header: "Integração", render: (e) => e.provider },
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
    <div className="flex flex-col gap-4">
      <h3 className="text-text">Status do Poller</h3>
      {isLoading ? (
        <p className="text-neutral-400">Carregando…</p>
      ) : (
        <Table columns={columns} rows={data ?? []} rowKey={(e) => e.provider} />
      )}
    </div>
  );
}
