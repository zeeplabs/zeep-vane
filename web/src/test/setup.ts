import "@testing-library/jest-dom/vitest";
import { beforeAll, afterEach, afterAll } from "vitest";
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
  resetEmailProviders,
  resetLLMProviders,
  resetPasswordResetTokens,
  resetSignupState,
} from "./msw/handlers";

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
  resetEmailProviders();
  resetLLMProviders();
  resetPasswordResetTokens();
  resetSignupState();
});
afterAll(() => server.close());
