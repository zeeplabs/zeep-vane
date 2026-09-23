import { describe, it, expect } from "vitest";
import "../../lib/i18n";
import { render, screen } from "@testing-library/react";
import { IntegrationCard } from "./IntegrationCard";

describe("IntegrationCard", () => {
  it("renders the Conectado badge and meta when status is connected", () => {
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

  it("renders the Não conectado badge when status is not_connected", () => {
    render(
      <IntegrationCard icon={<span>icon</span>} bannerBg="#FAFAFC" status="not_connected" title="New Relic" description="desc" />,
    );
    expect(screen.getByText("Não conectado")).toBeInTheDocument();
  });

  it("renders the Em breve badge for a decorative integration (coming_soon)", () => {
    render(
      <IntegrationCard icon={<span>icon</span>} bannerBg="#FAFAFC" status="coming_soon" title="New Relic" description="desc" />,
    );
    expect(screen.getByText("Em breve")).toBeInTheDocument();
  });

  it("renders no button when action is omitted (viewer/decorative)", () => {
    render(
      <IntegrationCard icon={<span>icon</span>} bannerBg="#FAFAFC" status="not_connected" title="New Relic" description="desc" />,
    );
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
  });

  it("renders the received action (e.g. the Conectar button)", () => {
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
