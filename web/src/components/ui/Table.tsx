import type { ReactNode } from "react";

export interface TableColumn<T> {
  key: string;
  header: string;
  render: (row: T) => ReactNode;
}

export interface TableProps<T> {
  columns: TableColumn<T>[];
  rows: T[];
  rowKey: (row: T) => string;
  emptyMessage?: string;
}

// Nocturne's visual signature: a hairline between rows that fades out in the
// final 48px of each edge (never a solid full-width border).
const fadingHairline: React.CSSProperties = {
  backgroundImage:
    "linear-gradient(to right, transparent 0, var(--color-divider) 48px, var(--color-divider) calc(100% - 48px), transparent 100%)",
  backgroundRepeat: "no-repeat",
  backgroundPosition: "bottom",
  backgroundSize: "100% 1px",
};

export function Table<T>({ columns, rows, rowKey, emptyMessage }: TableProps<T>) {
  return (
    <table className="w-full border-collapse text-sm">
      <thead>
        <tr>
          {columns.map((col) => (
            <th
              key={col.key}
              className="text-left text-[11px] uppercase tracking-wider text-neutral-400 font-medium pb-2 px-3"
            >
              {col.header}
            </th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows.length === 0 ? (
          <tr>
            <td colSpan={columns.length} className="px-3 py-6 text-center text-neutral-400">
              {emptyMessage ?? "Nenhum registro encontrado."}
            </td>
          </tr>
        ) : (
          rows.map((row) => (
            <tr key={rowKey(row)} data-testid="table-row" style={fadingHairline}>
              {columns.map((col) => (
                <td key={col.key} className="px-3 py-2.5 text-text">
                  {col.render(row)}
                </td>
              ))}
            </tr>
          ))
        )}
      </tbody>
    </table>
  );
}
