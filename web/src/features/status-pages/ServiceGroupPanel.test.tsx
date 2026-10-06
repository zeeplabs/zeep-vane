import { describe, it, expect, vi, afterEach } from "vitest";
import { http, HttpResponse } from "msw";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "../../lib/i18n";
import { server } from "../../test/msw/server";
import { TestQueryProvider } from "../../test/queryClient";
import * as apiClient from "../../lib/apiClient";
import type { Service } from "../../types/api";
import {
  ServiceGroupPanel,
  buildInitialPlacements,
  toServicePlacements,
  moveNode,
  type PlacementNode,
} from "./ServiceGroupPanel";
import type { ServicePlacement } from "./hooks";

async function loginAsOwner() {
  await apiClient.apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

afterEach(async () => {
  await apiClient.apiFetch("/api/auth/logout", { method: "POST" });
});

const svc1: Service = {
  id: "svc-1",
  name: "API pública",
  slo_id: null,
  slo_name: null,
  monitor_mode: "slo",
  poll_type: null,
  poll_target: null,
  poll_interval_seconds: null,
  current_status: "operational",
  last_status_change_at: new Date().toISOString(),
  uptime_30d: null,
  last_seen_at: null,
};

const svc2: Service = { ...svc1, id: "svc-2", name: "Checkout" };

function renderPanel(services: Service[], onToggle = vi.fn()) {
  return {
    onToggle,
    ...render(
      <TestQueryProvider>
        <ServiceGroupPanel statusPageId="sp-1" services={services} canManage onToggle={onToggle} />
      </TestQueryProvider>
    ),
  };
}

describe("ServiceGroupPanel", () => {
  it("renders every linked service, checked, inside the Ungrouped section by default", async () => {
    await loginAsOwner();
    renderPanel([svc1, svc2]);

    expect(await screen.findByText("Sem grupo")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "API pública" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: "Checkout" })).toHaveAttribute("aria-pressed", "true");
  });

  it("clicking a linked service's row still calls onToggle (unlink), same contract as the old ServiceRow", async () => {
    await loginAsOwner();
    const { onToggle } = renderPanel([svc1, svc2]);

    await userEvent.click(await screen.findByRole("button", { name: "API pública" }));
    expect(onToggle).toHaveBeenCalledWith("svc-1");
  });

  it("wires a keyboard-operable drag handle per service (tabIndex 0, role button) - not pointer-only", async () => {
    await loginAsOwner();
    renderPanel([svc1]);

    const handle = await screen.findByRole("button", { name: "Reordenar API pública" });
    expect(handle).toHaveAttribute("tabindex", "0");
  });

  it("creates a group inline via useCreateGroup and renders a new section for it", async () => {
    await loginAsOwner();
    renderPanel([svc1]);

    await userEvent.type(screen.getByLabelText("Novo grupo"), "Pagamentos");
    await userEvent.click(screen.getByRole("button", { name: "Criar grupo" }));

    expect(await screen.findByText("Pagamentos")).toBeInTheDocument();
  });

  it("Save placement persists the working tree via PATCH .../services (SetServices)", async () => {
    await loginAsOwner();
    renderPanel([svc1, svc2]);

    await userEvent.click(await screen.findByRole("button", { name: "Salvar posicionamento" }));

    await vi.waitFor(async () => {
      const placements = await apiClient.apiFetch<ServicePlacement[]>("/api/status-pages/sp-1/services");
      expect(placements.map((p) => p.service_id)).toEqual(["svc-1", "svc-2"]);
      expect(placements.every((p) => p.group_id === undefined && p.parent_service_id === undefined)).toBe(true);
    });
  });

  it("surfaces a 422 placement-rule violation inline instead of failing silently", async () => {
    await loginAsOwner();
    server.use(
      http.patch("/api/status-pages/:id/services", () =>
        HttpResponse.json({ error: "a service cannot be its own parent" }, { status: 422 })
      )
    );
    renderPanel([svc1, svc2]);

    await userEvent.click(await screen.findByRole("button", { name: "Salvar posicionamento" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("a service cannot be its own parent");
  });

  it("renders nothing when there are no linked services", async () => {
    await loginAsOwner();
    const { container } = renderPanel([]);
    expect(container.firstChild).toBeNull();
  });
});

describe("buildInitialPlacements", () => {
  it("falls back to a flat, ungrouped/unparented tree when the server has no placement detail yet", () => {
    expect(buildInitialPlacements(["svc-1", "svc-2"], [])).toEqual([
      { serviceId: "svc-1", groupId: null, parentServiceId: null },
      { serviceId: "svc-2", groupId: null, parentServiceId: null },
    ] satisfies PlacementNode[]);
  });

  it("rebuilds the tree from server placement detail, position-ordered", () => {
    const placements: ServicePlacement[] = [
      { service_id: "svc-2", position: 1, group_id: "sgroup-1" },
      { service_id: "svc-1", position: 0 },
    ];
    expect(buildInitialPlacements(["svc-1", "svc-2"], placements)).toEqual([
      { serviceId: "svc-1", groupId: null, parentServiceId: null },
      { serviceId: "svc-2", groupId: "sgroup-1", parentServiceId: null },
    ] satisfies PlacementNode[]);
  });
});

describe("toServicePlacements", () => {
  it("flattens top-level nodes followed immediately by their own children, positions as array index", () => {
    const nodes: PlacementNode[] = [
      { serviceId: "svc-1", groupId: "sgroup-1", parentServiceId: null },
      { serviceId: "svc-3", groupId: "sgroup-1", parentServiceId: "svc-1" },
      { serviceId: "svc-2", groupId: null, parentServiceId: null },
    ];
    expect(toServicePlacements(nodes)).toEqual([
      { service_id: "svc-1", group_id: "sgroup-1", position: 0 },
      { service_id: "svc-3", group_id: "sgroup-1", parent_service_id: "svc-1", position: 1 },
      { service_id: "svc-2", group_id: undefined, position: 2 },
    ]);
  });
});

describe("moveNode", () => {
  const base: PlacementNode[] = [
    { serviceId: "svc-1", groupId: null, parentServiceId: null },
    { serviceId: "svc-2", groupId: null, parentServiceId: null },
  ];

  it("drag onto a group's container id sets group_id and clears any parent", () => {
    const result = moveNode(base, "svc-1", "sgroup-1", ["sgroup-1"]);
    expect(result.find((n) => n.serviceId === "svc-1")).toEqual({
      serviceId: "svc-1",
      groupId: "sgroup-1",
      parentServiceId: null,
    });
  });

  it("drag onto another service's nest zone sets parent_service_id and inherits its group", () => {
    const grouped: PlacementNode[] = [
      { serviceId: "svc-1", groupId: "sgroup-1", parentServiceId: null },
      { serviceId: "svc-2", groupId: null, parentServiceId: null },
    ];
    const result = moveNode(grouped, "svc-2", "nest:svc-1", ["sgroup-1"]);
    expect(result.find((n) => n.serviceId === "svc-2")).toEqual({
      serviceId: "svc-2",
      groupId: "sgroup-1",
      parentServiceId: "svc-1",
    });
  });

  it("refuses to nest under a service that already has a parent (depth cap, SGD-12)", () => {
    const nested: PlacementNode[] = [
      { serviceId: "svc-1", groupId: null, parentServiceId: null },
      { serviceId: "svc-2", groupId: null, parentServiceId: "svc-1" },
      { serviceId: "svc-3", groupId: null, parentServiceId: null },
    ];
    const result = moveNode(nested, "svc-3", "nest:svc-2", []);
    expect(result).toEqual(nested);
  });

  it("reorders within the same container when dropped onto another top-level service", () => {
    const result = moveNode(base, "svc-2", "svc-1", []);
    expect(result.map((n) => n.serviceId)).toEqual(["svc-2", "svc-1"]);
  });

  it("is a no-op when dropped onto itself", () => {
    expect(moveNode(base, "svc-1", "svc-1", [])).toBe(base);
  });
});
