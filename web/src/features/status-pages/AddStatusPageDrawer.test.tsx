import { describe, it, expect, afterEach } from "vitest";
import "../../lib/i18n";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
import type { Page, Service } from "../../types/api";
import { AddStatusPageDrawer } from "./AddStatusPageDrawer";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

afterEach(async () => {
  await apiFetch("/api/auth/logout", { method: "POST" });
});

function renderDrawer(onOpenChange: (open: boolean) => void = () => {}) {
  return render(
    <TestQueryProvider>
      <AddStatusPageDrawer open onOpenChange={onOpenChange} />
    </TestQueryProvider>
  );
}

describe("AddStatusPageDrawer", () => {
  it("mostra 'Público' selecionado por padrão e 'Privado' desabilitado, sem onClick, sem mudar seleção ao clicar (DSP-15)", async () => {
    await loginAsOwner();
    renderDrawer();

    const publicTile = screen.getByRole("button", { name: "Público" });
    const privateTile = screen.getByRole("button", { name: "Privado" });
    expect(publicTile).toHaveAttribute("aria-pressed", "true");
    expect(privateTile).toHaveAttribute("aria-pressed", "false");
    expect(privateTile).toHaveAttribute("aria-disabled", "true");

    await userEvent.click(privateTile);

    expect(screen.getByRole("button", { name: "Público" })).toHaveAttribute("aria-pressed", "true");
  });

  it("envia nome e serviços marcados via POST /api/status-pages ao submeter", async () => {
    await loginAsOwner();
    const servicesPage = await apiFetch<Page<Service>>("/api/services?page=1");
    const service = servicesPage.items[0];
    expect(service).toBeDefined();

    renderDrawer();

    await userEvent.type(screen.getByLabelText("Nome da página"), "Status Público Novo");
    await userEvent.click(screen.getByRole("button", { name: service.name }));
    await userEvent.click(screen.getByRole("button", { name: "Criar status page" }));

    await waitFor(async () => {
      const list = await apiFetch<Page<{ name: string; service_ids: string[] }>>("/api/status-pages?page=1");
      const created = list.items.find((p) => p.name === "Status Público Novo");
      expect(created).toBeDefined();
      expect(created?.service_ids).toContain(service.id);
    });
  });
});
