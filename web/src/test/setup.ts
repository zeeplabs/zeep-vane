import "@testing-library/jest-dom/vitest";
import { beforeAll, afterEach, afterAll } from "vitest";
import { server } from "./msw/server";
import { resetAuthSession, resetDomainsAndStatusPages } from "./msw/handlers";

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => {
  server.resetHandlers();
  resetAuthSession();
  resetDomainsAndStatusPages();
});
afterAll(() => server.close());
