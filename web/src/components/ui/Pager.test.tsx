import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Pager } from "./Pager";

describe("Pager", () => {
  it("renderiza 'Página X de Y'", () => {
    render(<Pager page={2} totalPages={5} onChange={vi.fn()} />);
    expect(screen.getByText("Página 2 de 5")).toBeInTheDocument();
  });

  it("desabilita Anterior na página 1", () => {
    render(<Pager page={1} totalPages={5} onChange={vi.fn()} />);
    expect(screen.getByRole("button", { name: "Anterior" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Próximo" })).not.toBeDisabled();
  });

  it("desabilita Próximo na última página", () => {
    render(<Pager page={5} totalPages={5} onChange={vi.fn()} />);
    expect(screen.getByRole("button", { name: "Próximo" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Anterior" })).not.toBeDisabled();
  });

  it("chama onChange com page-1/page+1 ao clicar", async () => {
    const onChange = vi.fn();
    render(<Pager page={2} totalPages={5} onChange={onChange} />);

    await userEvent.click(screen.getByRole("button", { name: "Anterior" }));
    expect(onChange).toHaveBeenCalledWith(1);

    await userEvent.click(screen.getByRole("button", { name: "Próximo" }));
    expect(onChange).toHaveBeenCalledWith(3);
  });

  it("com totalPages=1 (total=0, per caller's max(1,...)) desabilita os dois botões", () => {
    render(<Pager page={1} totalPages={1} onChange={vi.fn()} />);
    expect(screen.getByRole("button", { name: "Anterior" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Próximo" })).toBeDisabled();
    expect(screen.getByText("Página 1 de 1")).toBeInTheDocument();
  });
});
