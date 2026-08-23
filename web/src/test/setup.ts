import "@testing-library/jest-dom/vitest";
import { beforeAll, afterEach, afterAll } from "vitest";
import { server } from "./msw/server";
import {
  resetAuthSession,
  resetDomainsAndStatusPages,
  resetServicesAndIntegration,
  resetIncidents,
  resetAdmins,
  resetCompanySettings,
} from "./msw/handlers";

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => {
  server.resetHandlers();
  resetAuthSession();
  resetDomainsAndStatusPages();
  resetServicesAndIntegration();
  resetIncidents();
  resetAdmins();
  resetCompanySettings();
});
afterAll(() => server.close());
