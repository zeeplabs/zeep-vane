import "@testing-library/jest-dom/vitest";
import { configure } from "@testing-library/react";
import { beforeAll, afterEach, afterAll } from "vitest";

// testing-library's default asyncUtilTimeout (waitFor/findBy*) is 1000ms -
// fine on local hardware, too tight on CircleCI's shared containers where
// a single async tick (React Query settling, MSW round-trip) can take
// longer under load. A tight default made CI flaky (a different random
// subset of tests timing out each run) even after the resource class and
// vitest thread pool were both tuned - this is the actual bottleneck,
// not CPU/thread provisioning.
configure({ asyncUtilTimeout: 5000 });
import { server } from "./msw/server";
import {
  resetAuthSession,
  resetSessions,
  resetDomainsAndStatusPages,
  resetServicesAndIntegration,
  resetIncidents,
  resetAdmins,
  resetCompanySettings,
  resetBootstrapState,
  resetDeploymentMode,
  resetEmailProviders,
  resetLLMProviders,
  resetPasswordResetTokens,
  resetSignupState,
  resetAuditLog,
} from "./msw/handlers";

// jsdom has no ResizeObserver and never lays out real pixel sizes, so
// recharts' ResponsiveContainer (used by OverviewPage's uptime chart) would
// otherwise measure a 0x0 container and render nothing. Polyfill the
// observer and give elements a fixed non-zero box so the chart can size
// itself in tests.
class MockResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}
// eslint-disable-next-line @typescript-eslint/no-explicit-any
if (typeof HTMLElement !== "undefined") {
  (globalThis as any).ResizeObserver ??= MockResizeObserver;
  Object.defineProperty(HTMLElement.prototype, "offsetWidth", { configurable: true, value: 600 });
  Object.defineProperty(HTMLElement.prototype, "offsetHeight", { configurable: true, value: 200 });
  HTMLElement.prototype.getBoundingClientRect = () =>
    ({
      width: 600,
      height: 200,
      top: 0,
      left: 0,
      bottom: 200,
      right: 600,
      x: 0,
      y: 0,
      toJSON() {},
    }) as DOMRect;
}

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => {
  server.resetHandlers();
  resetAuthSession();
  resetSessions();
  resetDomainsAndStatusPages();
  resetServicesAndIntegration();
  resetIncidents();
  resetAdmins();
  resetCompanySettings();
  resetBootstrapState();
  resetDeploymentMode();
  resetEmailProviders();
  resetLLMProviders();
  resetPasswordResetTokens();
  resetSignupState();
  resetAuditLog();
});
afterAll(() => server.close());
