import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { Table } from "./Table";

interface Row {
  id: string;
  name: string;
}

const columns = [
  { key: "name", header: "Nome", render: (r: Row) => r.name },
];

describe("Table", () => {
  it("renderiza header uppercase e linhas com hairline que desvanece (48px)", () => {
    const rows: Row[] = [{ id: "1", name: "Domínio A" }, { id: "2", name: "Domínio B" }];
    render(<Table columns={columns} rows={rows} rowKey={(r) => r.id} />);

    expect(screen.getByText("Nome")).toBeInTheDocument();
    const trs = screen.getAllByTestId("table-row");
    expect(trs).toHaveLength(2);
    trs.forEach((tr) => {
      expect(tr.style.backgroundImage).toContain("linear-gradient");
      expect(tr.style.backgroundImage).toContain("48px");
    });
  });

  it("renderiza mensagem vazia quando não há linhas", () => {
    render(<Table columns={columns} rows={[]} rowKey={(r) => r.id} emptyMessage="Vazio" />);
    expect(screen.getByText("Vazio")).toBeInTheDocument();
  });
});
