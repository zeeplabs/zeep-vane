import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { IntegrationCard } from "./IntegrationCard";

describe("IntegrationCard", () => {
  it("renderiza badge Conectado e meta quando status é connected", () => {
    render(
      <IntegrationCard
        icon={<span>icon</span>}
        bannerBg="#F3F1FB"
        status="connected"
        title="Datadog"
        description="desc"
        meta="Sincronizado há 12 min"
      />,
    );
    expect(screen.getByText("Conectado")).toBeInTheDocument();
    expect(screen.getByText("Sincronizado há 12 min")).toBeInTheDocument();
  });

  it("renderiza badge Não conectado quando status é not_connected", () => {
    render(
      <IntegrationCard icon={<span>icon</span>} bannerBg="#FAFAFC" status="not_connected" title="New Relic" description="desc" />,
    );
    expect(screen.getByText("Não conectado")).toBeInTheDocument();
  });

  it("renderiza badge Em breve para integração decorativa (coming_soon)", () => {
    render(
      <IntegrationCard icon={<span>icon</span>} bannerBg="#FAFAFC" status="coming_soon" title="New Relic" description="desc" />,
    );
    expect(screen.getByText("Em breve")).toBeInTheDocument();
  });

  it("não renderiza nenhum botão quando action é omitido (viewer/decorativo)", () => {
    render(
      <IntegrationCard icon={<span>icon</span>} bannerBg="#FAFAFC" status="not_connected" title="New Relic" description="desc" />,
    );
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
  });

  it("renderiza a action recebida (ex.: botão Conectar)", () => {
    render(
      <IntegrationCard
        icon={<span>icon</span>}
        bannerBg="#FAFAFC"
        status="not_connected"
        title="New Relic"
        description="desc"
        action={<button type="button">Conectar</button>}
      />,
    );
    expect(screen.getByRole("button", { name: "Conectar" })).toBeInTheDocument();
  });
});
