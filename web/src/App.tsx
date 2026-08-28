import type { ReactNode } from "react";
import { Routes, Route, Outlet, Navigate } from "react-router-dom";
import { Toaster } from "sonner";
import { AuthProvider, useAuth } from "./auth/AuthProvider";
import { SessionExpiredModal } from "./auth/SessionExpiredModal";
import { RequireAuth, RequireRole } from "./routes/RequireRole";
import { Sidebar } from "./layout/Sidebar";
import { LoginPage } from "./features/auth/LoginPage";
import { BootstrapPage } from "./features/auth/BootstrapPage";
import { PasswordResetRequestPage } from "./features/auth/PasswordResetRequestPage";
import { AcceptInvitePage } from "./features/auth/AcceptInvitePage";
import { IntegrationsPage } from "./features/integrations/IntegrationsPage";
import { ServicesPage } from "./features/services/ServicesPage";
import { DomainsStatusPagesPage } from "./features/domains/DomainsStatusPagesPage";
import { StatusPagesPage } from "./features/status-pages/StatusPagesPage";
import { StatusPageDetail } from "./features/status-pages/StatusPageDetail";
import { IncidentsPage } from "./features/incidents/IncidentsPage";
import { IncidentDetail } from "./features/incidents/IncidentDetail";
import { AdminsPage } from "./features/admins/AdminsPage";
import { PollerBanner } from "./features/poller/PollerBanner";
import { PollerStatusPage } from "./features/poller/PollerStatusPage";
import { SettingsPage } from "./features/settings/SettingsPage";
import { PublicStatusPage } from "./features/public-status/PublicStatusPage";
import "./lib/i18n";

// RedirectToBootstrapIfNeeded gates the anonymous-facing admin routes
// (login, reset-password, the authenticated area) on whether the instance
// still needs its first admin (SHD-19, SHD-21). Deliberately never wraps
// /status/:id - that route serves public status-page visitors, a wholly
// separate audience the admin instance's bootstrap state has no bearing
// on. Follows RequireAuth's own "status === loading → render nothing yet"
// convention so it never redirects on a guess before the boot check
// resolves.
function RedirectToBootstrapIfNeeded({ children }: { children: ReactNode }) {
  const { needsBootstrap, status } = useAuth();

  if (status === "loading") return null;
  if (needsBootstrap) return <Navigate to="/bootstrap" replace />;
  return <>{children}</>;
}

// BootstrapRoute is the mirror-image guard for /bootstrap itself: once an
// admin already exists, the bootstrap form is a dead end, so a direct
// visit redirects to /login instead of showing the form (SHD-21, spec.md
// AC7 under "First-run bootstrap").
function BootstrapRoute() {
  const { needsBootstrap, status } = useAuth();

  if (status === "loading") return null;
  if (!needsBootstrap) return <Navigate to="/login" replace />;
  return <BootstrapPage />;
}

function AuthenticatedLayout() {
  return (
    <div className="flex h-screen w-full bg-bg">
      <Sidebar />
      <div className="flex min-w-0 flex-1 flex-col">
        {/* Slot fixo acima do conteúdo, visível em qualquer rota autenticada (T34). */}
        <div data-testid="global-banner-slot">
          <PollerBanner />
        </div>
        <main className="flex-1 overflow-auto p-6">
          <Outlet />
        </main>
      </div>
    </div>
  );
}

export default function App() {
  return (
    <AuthProvider>
      <SessionExpiredModal />
      <Toaster richColors position="top-right" />
      <Routes>
        <Route
          path="/login"
          element={
            <RedirectToBootstrapIfNeeded>
              <LoginPage />
            </RedirectToBootstrapIfNeeded>
          }
        />
        <Route path="/bootstrap" element={<BootstrapRoute />} />
        <Route
          path="/reset-password"
          element={
            <RedirectToBootstrapIfNeeded>
              <PasswordResetRequestPage />
            </RedirectToBootstrapIfNeeded>
          }
        />
        <Route path="/status/:id" element={<PublicStatusPage />} />
        {/* No RedirectToBootstrapIfNeeded/auth guard - matches /status/:id's
            precedent (spec.md accept-invite-page: an already-authenticated
            admin opening this link renders normally, no redirect). */}
        <Route path="/accept-invite/:token" element={<AcceptInvitePage />} />
        <Route
          element={
            <RedirectToBootstrapIfNeeded>
              <RequireAuth>
                <AuthenticatedLayout />
              </RequireAuth>
            </RedirectToBootstrapIfNeeded>
          }
        >
          <Route path="/" element={<Navigate to="/domains" replace />} />
          <Route path="/domains" element={<DomainsStatusPagesPage />} />
          <Route path="/status-pages" element={<StatusPagesPage />} />
          <Route path="/status-pages/:id" element={<StatusPageDetail />} />
          <Route path="/incidents" element={<IncidentsPage />} />
          <Route path="/incidents/:id" element={<IncidentDetail />} />
          <Route path="/integrations" element={<IntegrationsPage />} />
          <Route path="/services" element={<ServicesPage />} />
          <Route
            path="/admins"
            element={
              <RequireRole roles={["owner"]}>
                <AdminsPage />
              </RequireRole>
            }
          />
          <Route path="/poller-status" element={<PollerStatusPage />} />
          <Route
            path="/settings"
            element={
              <RequireRole roles={["owner"]}>
                <SettingsPage />
              </RequireRole>
            }
          />
        </Route>
      </Routes>
    </AuthProvider>
  );
}
