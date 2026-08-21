import { Routes, Route, Outlet, Navigate } from "react-router-dom";
import { Toaster } from "sonner";
import { AuthProvider } from "./auth/AuthProvider";
import { SessionExpiredModal } from "./auth/SessionExpiredModal";
import { RequireAuth, RequireRole } from "./routes/RequireRole";
import { Sidebar } from "./layout/Sidebar";
import { LoginPage } from "./features/auth/LoginPage";
import { PasswordResetRequestPage } from "./features/auth/PasswordResetRequestPage";
import { IntegrationsPage } from "./features/integrations/IntegrationsPage";
import { ServicesPage } from "./features/services/ServicesPage";
import { DomainsPage } from "./features/domains/DomainsPage";
import { StatusPagesPage } from "./features/status-pages/StatusPagesPage";
import { StatusPageDetail } from "./features/status-pages/StatusPageDetail";
import { IncidentsPage } from "./features/incidents/IncidentsPage";
import { IncidentDetail } from "./features/incidents/IncidentDetail";
import { AdminsPage } from "./features/admins/AdminsPage";
import { PollerBanner } from "./features/poller/PollerBanner";
import { PollerStatusPage } from "./features/poller/PollerStatusPage";
import { SettingsPage } from "./features/settings/SettingsPage";
import "./lib/i18n";

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
        <Route path="/login" element={<LoginPage />} />
        <Route path="/reset-password" element={<PasswordResetRequestPage />} />
        <Route
          element={
            <RequireAuth>
              <AuthenticatedLayout />
            </RequireAuth>
          }
        >
          <Route path="/" element={<Navigate to="/domains" replace />} />
          <Route path="/domains" element={<DomainsPage />} />
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
          <Route path="/settings" element={<SettingsPage />} />
        </Route>
      </Routes>
    </AuthProvider>
  );
}
