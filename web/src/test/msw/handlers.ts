import { http, HttpResponse } from "msw";
import { admins as seedAdmins } from "../../lib/mockData";

// Simulates the vane_session cookie server-side: real cookie semantics
// (Set-Cookie/credentials) are covered by the Go integration tests
// (I1-I4); this in-memory flag only lets the auth handlers below model
// "logged in or not" across requests within a test.
let sessionAdminId: string | null = null;

export function resetAuthSession(): void {
  sessionAdminId = null;
}

export const handlers = [
  http.post("/api/auth/login", async ({ request }) => {
    const body = (await request.json()) as { email?: string; password?: string };
    const admin = seedAdmins.find((a) => a.email === body.email && a.password === body.password);
    if (!admin) {
      return HttpResponse.json({ error: "invalid email or password" }, { status: 401 });
    }
    sessionAdminId = admin.id;
    return HttpResponse.json({ token: `msw-token-${admin.id}` });
  }),

  http.get("/api/auth/me", () => {
    const admin = seedAdmins.find((a) => a.id === sessionAdminId);
    if (!admin) {
      return HttpResponse.json({ error: "unauthorized" }, { status: 401 });
    }
    return HttpResponse.json({ id: admin.id, email: admin.email, role: admin.role });
  }),

  http.post("/api/auth/logout", () => {
    sessionAdminId = null;
    return new HttpResponse(null, { status: 200 });
  }),
];
