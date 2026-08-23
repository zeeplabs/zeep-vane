import { describe, it, expect, afterEach, vi } from "vitest";
import { http, HttpResponse } from "msw";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { server } from "../../test/msw/server";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
import type { StatusPage } from "../../types/api";
import { AttachDomainDrawer } from "./AttachDomainDrawer";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

async function createDomainlessPage(name: string): Promise<StatusPage> {
  return apiFetch<StatusPage>("/api/status-pages", {
    method: "POST",
    body: JSON.stringify({ name, service_ids: [] }),
  });
}

afterEach(async () => {
  await apiFetch("/api/auth/logout", { method: "POST" });
});

function renderDrawer(statusPageId: string, onOpenChange: (open: boolean) => void) {
  return render(
    <TestQueryProvider>
      <AttachDomainDrawer statusPageId={statusPageId} open onOpenChange={onOpenChange} />
    </TestQueryProvider>
  );
}

describe("AttachDomainDrawer", () => {
  it("renderiza o seletor de domínio, o campo de subdomínio e o valor do DNS target configurado", async () => {
    await loginAsOwner();
    const page = await createDomainlessPage("Drawer Render Test");
    renderDrawer(page.id, vi.fn());

    expect(await screen.findByLabelText("Domínio")).toBeInTheDocument();
    expect(screen.getByLabelText("Subdomínio")).toBeInTheDocument();
    expect(await screen.findByText(/203\.0\.113\.10/)).toBeInTheDocument();
  });

  it("mostra aviso quando o operador não configurou PUBLIC_DNS_TARGET, sem bloquear o formulário", async () => {
    server.use(http.get("/api/instance/dns-target", () => HttpResponse.json({ target: null })));
    await loginAsOwner();
    const page = await createDomainlessPage("Drawer No DNS Target Test");
    renderDrawer(page.id, vi.fn());

    expect(
      await screen.findByText(/operador ainda não configurou o valor de destino do DNS/)
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Anexar" })).toBeEnabled();
  });

  it("submit com sucesso fecha o painel e a página passa a refletir o domínio anexado", async () => {
    await loginAsOwner();
    const page = await createDomainlessPage("Drawer Success Test");
    const onOpenChange = vi.fn();
    renderDrawer(page.id, onOpenChange);

    await screen.findByRole("option", { name: "status.acme.com" });
    await userEvent.selectOptions(screen.getByLabelText("Domínio"), "status.acme.com");
    await userEvent.type(screen.getByLabelText("Subdomínio"), "novo-anexado");
    await userEvent.click(screen.getByRole("button", { name: "Anexar" }));

    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));

    const updated = await apiFetch<StatusPage[]>("/api/status-pages");
    const attached = updated.find((p) => p.id === page.id);
    expect(attached?.domain_id).toBe("dom-1");
    expect(attached?.subdomain).toBe("novo-anexado");
  });

  it("404 (página não encontrada) mostra erro inline e mantém o painel aberto", async () => {
    await loginAsOwner();
    const onOpenChange = vi.fn();
    renderDrawer("sp-nao-existe", onOpenChange);

    await screen.findByRole("option", { name: "status.acme.com" });
    await userEvent.selectOptions(screen.getByLabelText("Domínio"), "status.acme.com");
    await userEvent.type(screen.getByLabelText("Subdomínio"), "qualquer");
    await userEvent.click(screen.getByRole("button", { name: "Anexar" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("status page not found");
    expect(onOpenChange).not.toHaveBeenCalled();
  });

  it("409 (página já com domínio) mostra erro inline e mantém o painel aberto", async () => {
    await loginAsOwner();
    const onOpenChange = vi.fn();
    // sp-1 já tem domain_id (fixture) - simula o admin reabrindo a tela
    // pra uma página que outra requisição concorrente já anexou.
    renderDrawer("sp-1", onOpenChange);

    await screen.findByRole("option", { name: "status.acme.com" });
    await userEvent.selectOptions(screen.getByLabelText("Domínio"), "status.acme.com");
    await userEvent.type(screen.getByLabelText("Subdomínio"), "qualquer");
    await userEvent.click(screen.getByRole("button", { name: "Anexar" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "this status page already has a domain attached"
    );
    expect(onOpenChange).not.toHaveBeenCalled();
  });

  it("422 (domain_id inválido) mostra erro inline e mantém o painel aberto", async () => {
    server.use(
      http.get("/api/domains", () =>
        HttpResponse.json([{ id: "dom-stale", hostname: "stale.example.com", created_at: new Date().toISOString() }])
      ),
    );
    await loginAsOwner();
    const page = await createDomainlessPage("Drawer 422 Test");
    const onOpenChange = vi.fn();
    renderDrawer(page.id, onOpenChange);

    await screen.findByRole("option", { name: "stale.example.com" });
    await userEvent.selectOptions(screen.getByLabelText("Domínio"), "stale.example.com");
    await userEvent.type(screen.getByLabelText("Subdomínio"), "qualquer");
    await userEvent.click(screen.getByRole("button", { name: "Anexar" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "domain_id does not reference an existing domain"
    );
    expect(onOpenChange).not.toHaveBeenCalled();
  });
});
