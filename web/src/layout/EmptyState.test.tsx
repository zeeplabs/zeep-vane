import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { EmptyState } from "./EmptyState";

describe("EmptyState", () => {
  it("renderiza título e CTA customizados", () => {
    render(
      <EmptyState
        title="Nenhum domínio cadastrado"
        description="Adicione o primeiro domínio para começar."
        action={<button>Adicionar domínio</button>}
      />
    );
    expect(screen.getByText("Nenhum domínio cadastrado")).toBeInTheDocument();
    expect(screen.getByText("Adicione o primeiro domínio para começar.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Adicionar domínio" })).toBeInTheDocument();
  });
});
