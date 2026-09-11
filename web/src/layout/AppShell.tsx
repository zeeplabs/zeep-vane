import type { ReactNode } from "react";
import { useLocation } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Sidebar } from "./Sidebar";
import { Topbar } from "./Topbar";
import { PollerBanner } from "../features/poller/PollerBanner";

// routeTitleKeys maps a route prefix to the i18n key Topbar's title uses
// (design.md's Risks & Concerns: "a simple pathname->label lookup", same
// idea Sidebar.tsx's own domainsActive prefix-match already applies).
// Longest/most-specific prefixes are irrelevant here since every entry is
// mutually exclusive by leading path segment.
const routeTitleKeys: Array<[prefix: string, i18nKey: string]> = [
  ["/domains", "sidebar.domainsStatusPages"],
  ["/status-pages", "sidebar.domainsStatusPages"],
  ["/incidents", "sidebar.incidents"],
  ["/integrations", "sidebar.integrations"],
  ["/services", "sidebar.services"],
  ["/admins", "sidebar.admins"],
  ["/poller-status", "sidebar.pollerStatus"],
  ["/settings", "sidebar.settings"],
];

function titleKeyFor(pathname: string): string {
  const match = routeTitleKeys.find(([prefix]) => pathname.startsWith(prefix));
  return match ? match[1] : "sidebar.brand";
}

export interface AppShellProps {
  children: ReactNode;
}

// AppShell: Sidebar + Topbar + scrollable content area (new-layout-
// migration, SHELL-01/SHELL-08/SHELL-12) - replaces AuthenticatedLayout's
// old <Sidebar/> + <main> markup in App.tsx. PollerBanner keeps its exact
// prior position (full-width slot above the content area, not inside its
// padded/max-width wrapper).
export function AppShell({ children }: AppShellProps) {
  const location = useLocation();
  const { t } = useTranslation();
  const title = t(titleKeyFor(location.pathname));

  return (
    <div className="flex h-screen w-full bg-bg">
      <Sidebar />
      <div className="flex min-w-0 flex-1 flex-col">
        <Topbar title={title} />
        <div data-testid="global-banner-slot">
          <PollerBanner />
        </div>
        <main className="flex-1 overflow-auto">
          <div className="mx-auto max-w-[1200px] px-[40px] py-[32px]">{children}</div>
        </main>
      </div>
    </div>
  );
}
