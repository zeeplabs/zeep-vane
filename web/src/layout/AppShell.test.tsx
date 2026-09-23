import { describe, it, expect, afterEach } from "vitest";
import { render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import "../lib/i18n";
import { AuthProvider } from "../auth/AuthProvider";
import { AppShell } from "./AppShell";
import { apiFetch } from "../lib/apiClient";
import { TestQueryProvider } from "../test/queryClient";

async function loginAs(email: string) {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email, password: "demo1234" }),
  });
}

afterEach(async () => {
  try {
    await apiFetch("/api/auth/logout", { method: "POST" });
  } catch {
    /* ignore */
  }
});

function renderShellAt(pathname: string) {
  return render(
    <TestQueryProvider>
      <MemoryRouter initialEntries={[pathname]}>
        <AuthProvider>
          <AppShell>
            <div data-testid="routed-content">conteúdo da rota</div>
          </AppShell>
        </AuthProvider>
      </MemoryRouter>
    </TestQueryProvider>
  );
}

describe("AppShell", () => {
  // Topbar's title is a plain styled <p>, not an <h1> - matching the
  // handoff (the topbar title there is a plain div; only the routed page's
  // own content has a real h1). A second <h1> with the same text was also
  // an a11y bug (two h1s per page). So these assert the header's text
  // directly instead of a heading role.
  it("derives the Topbar title from the current route (SHELL-08)", async () => {
    await loginAs("owner@vane.app");
    const { container } = renderShellAt("/incidents");
    const header = container.querySelector("header")!;
    expect(await within(header).findByText("Incidentes")).toBeInTheDocument();
  });

  it("derives a different title for another route", async () => {
    await loginAs("owner@vane.app");
    const { container } = renderShellAt("/settings");
    const header = container.querySelector("header")!;
    expect(await within(header).findByText("Configurações")).toBeInTheDocument();
  });

  it("shows 'Visão geral' (not the app name) as the title on /overview", async () => {
    await loginAs("owner@vane.app");
    const { container } = renderShellAt("/overview");
    const header = container.querySelector("header")!;
    expect(await within(header).findByText("Visão geral")).toBeInTheDocument();
  });

  // PROFRD-01: /profile had no entry in routeTitleKeys, falling back to
  // "Vane" - same bug class already fixed for /overview.
  it("shows 'Meu Perfil' (not the app name) as the title on /profile", async () => {
    await loginAs("owner@vane.app");
    const { container } = renderShellAt("/profile");
    const header = container.querySelector("header")!;
    expect(await within(header).findByText("Meu Perfil")).toBeInTheDocument();
  });

  // BILLPG-01: /billing had no entry in routeTitleKeys until this feature.
  it("shows 'Planos & Faturamento' (not the app name) as the title on /billing", async () => {
    await loginAs("owner@vane.app");
    const { container } = renderShellAt("/billing");
    const header = container.querySelector("header")!;
    expect(await within(header).findByText("Planos & Faturamento")).toBeInTheDocument();
  });

  it("PollerBanner keeps the same position relative to the routed content (before main)", async () => {
    await loginAs("owner@vane.app");
    const { container } = renderShellAt("/services");
    await screen.findByTestId("routed-content");

    const bannerSlot = container.querySelector('[data-testid="global-banner-slot"]');
    const routedContent = screen.getByTestId("routed-content");
    expect(bannerSlot).not.toBeNull();
    expect(bannerSlot!.compareDocumentPosition(routedContent) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  // SHELL-19: the spec pins exact layout literals (own scroll, `32px 40px`
  // padding, `1200px` max-width centered). jsdom doesn't compute layout
  // pixels, so this asserts the literal Tailwind arbitrary-value classes in
  // the DOM instead of the resolved geometry (lessons L-038).
  it("content area has its own scroll and the handoff's layout literals (SHELL-19)", async () => {
    await loginAs("owner@vane.app");
    const { container } = renderShellAt("/services");
    await screen.findByTestId("routed-content");

    const main = container.querySelector("main");
    expect(main).not.toBeNull();
    expect(main!.className).toContain("overflow-auto");

    const wrapper = main!.querySelector("div");
    expect(wrapper).not.toBeNull();
    expect(wrapper!.className).toContain("max-w-[1200px]");
    expect(wrapper!.className).toContain("px-[40px]");
    expect(wrapper!.className).toContain("py-[32px]");
    expect(wrapper!.className).toContain("mx-auto");
  });
});
