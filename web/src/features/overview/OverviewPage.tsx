import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Card } from "../../components/ui/Card";
import { Tag, type TagVariant } from "../../components/ui/Tag";
import type { OverviewIncident, OverviewResponse, OverviewUptimeBucket } from "../../types/api";
import { useOverview } from "./hooks";

// bucketColorVar maps a day's uptime to a status color, reusing the same
// tokens the public status page's bars use. null (no data) is neutral gray,
// never a fabricated value (OVW-07).
function bucketColorVar(uptimePercent: number | null): string {
  if (uptimePercent === null) return "--color-neutral-600";
  if (uptimePercent >= 99.5) return "--color-success";
  if (uptimePercent >= 95) return "--color-warning";
  return "--color-critical";
}

const incidentTagVariant: Record<string, TagVariant> = {
  investigating: "critical",
  identified: "warning",
  monitoring: "warning",
  resolved: "neutral",
};

// formatDayLabel turns the backend's YYYY-MM-DD local day into DD/MM without
// constructing a Date (which would reintroduce a timezone shift).
function formatDayLabel(date: string): string {
  const [year, month, day] = date.split("-");
  if (!year || !month || !day) return date;
  return `${day}/${month}`;
}

function formatTimestamp(iso: string, locale: string): string {
  return new Date(iso).toLocaleString(locale, { day: "2-digit", month: "short", hour: "2-digit", minute: "2-digit" });
}

function ShortcutIcon({ path }: { path: string }) {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d={path} />
    </svg>
  );
}

const SHORTCUT_ICON_PATHS = {
  addService: "M12 5v14M5 12h14",
  createStatusPage: "M3 12h18M12 3c2.5 2.7 4 6.1 4 9s-1.5 6.3-4 9c-2.5-2.7-4-6.1-4-9s1.5-6.3 4-9ZM3 12h18",
  inviteUser: "M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2M9 7a4 4 0 1 0 0 8 4 4 0 0 0 0-8ZM19 8v6M22 11h-6",
  viewDomains: "M3 12h18M12 3c2.5 2.7 4 6.1 4 9s-1.5 6.3-4 9c-2.5-2.7-4-6.1-4-9s1.5-6.3 4-9ZM12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18Z",
} as const;

const SHORTCUTS = [
  { key: "addService", to: "/services" },
  { key: "createStatusPage", to: "/domains" },
  { key: "inviteUser", to: "/admins" },
  { key: "viewDomains", to: "/domains" },
] as const;

function SummaryCard({ testId, label, value }: { testId: string; label: string; value: string }) {
  return (
    <Card elevation="elev-sm" className="flex flex-col gap-1 p-4">
      <span className="text-[12.5px] text-text-muted">{label}</span>
      <span data-testid={testId} className="text-2xl font-semibold text-text">
        {value}
      </span>
    </Card>
  );
}

function UptimeChart({ series }: { series: OverviewUptimeBucket[] }) {
  const { t } = useTranslation();

  function barLabel(bucket: OverviewUptimeBucket): string {
    const value = bucket.uptime_percent === null ? t("overview.noData") : `${bucket.uptime_percent.toFixed(1)}%`;
    return t("overview.chart.barLabel", { date: formatDayLabel(bucket.date), value });
  }

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-baseline justify-between">
        <h3 className="m-0 text-sm font-medium text-text">{t("overview.chart.title")}</h3>
        <span className="text-xs text-text-muted">{t("overview.chart.subtitle")}</span>
      </div>
      <div className="flex h-24 items-end gap-1">
        {series.map((bucket, i) => (
          <div key={bucket.date} className="flex h-full flex-1 items-end">
            <div
              role="img"
              aria-label={barLabel(bucket)}
              title={barLabel(bucket)}
              tabIndex={0}
              data-testid={`overview-bar-${i}`}
              className="w-full rounded-sm"
              style={{
                height: bucket.uptime_percent === null ? "3px" : `${Math.max(bucket.uptime_percent, 2)}%`,
                background: `var(${bucketColorVar(bucket.uptime_percent)})`,
              }}
            />
          </div>
        ))}
      </div>
    </div>
  );
}

function RecentIncidents({ incidents }: { incidents: OverviewIncident[] }) {
  const { t, i18n } = useTranslation();

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <h3 className="m-0 text-sm font-medium text-text">{t("overview.incidents.title")}</h3>
        <Link to="/incidents" className="text-xs text-accent hover:underline">
          {t("overview.incidents.viewAll")}
        </Link>
      </div>
      {incidents.length === 0 ? (
        <p className="m-0 text-[13px] text-text-muted">{t("overview.incidents.empty")}</p>
      ) : (
        <div className="flex flex-col gap-2">
          {incidents.map((incident) => (
            <div key={incident.id} className="flex items-center justify-between gap-2">
              <div className="flex min-w-0 items-center gap-2">
                <Tag variant={incidentTagVariant[incident.status] ?? "neutral"}>
                  {t(`overview.incidentStatus.${incident.status}`)}
                </Tag>
                <span className="truncate text-[14px] text-text">{incident.title}</span>
              </div>
              <span className="shrink-0 text-xs text-text-muted">
                {formatTimestamp(incident.created_at, i18n.language)}
              </span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function Shortcuts() {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col gap-3">
      <h3 className="m-0 text-sm font-medium text-text">{t("overview.shortcuts.title")}</h3>
      <div className="grid grid-cols-2 gap-3">
        {SHORTCUTS.map((shortcut) => (
          <Link
            key={shortcut.key}
            to={shortcut.to}
            className="flex items-center gap-2 rounded-md border border-divider px-3 py-2.5 text-[13.5px] text-text transition-colors hover:bg-sidebar-hover-bg"
          >
            <span className="text-accent">
              <ShortcutIcon path={SHORTCUT_ICON_PATHS[shortcut.key]} />
            </span>
            {t(`overview.shortcuts.${shortcut.key}`)}
          </Link>
        ))}
      </div>
    </div>
  );
}

function SummaryGrid({ data }: { data: OverviewResponse }) {
  const { t } = useTranslation();
  const uptime = data.uptime_avg_30d === null ? t("overview.noData") : `${data.uptime_avg_30d.toFixed(1)}%`;
  return (
    <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
      <SummaryCard testId="overview-card-uptime" label={t("overview.cards.uptime")} value={uptime} />
      <SummaryCard testId="overview-card-open-incidents" label={t("overview.cards.openIncidents")} value={String(data.open_incidents)} />
      <SummaryCard testId="overview-card-unhealthy" label={t("overview.cards.unhealthyServices")} value={String(data.unhealthy_services)} />
      <SummaryCard testId="overview-card-domains" label={t("overview.cards.verifiedDomains")} value={String(data.verified_domains)} />
    </div>
  );
}

// OverviewPage is the authenticated landing screen (dashboard-overview-page
// OVW-01/02). It renders one aggregation endpoint's response across the 4
// summary cards, the 14-day uptime chart, the recent-incidents list, and the
// shortcuts grid. No upsell banner and no activity-feed card (spec Out of
// Scope).
export function OverviewPage() {
  const { t } = useTranslation();
  const { data, isLoading, isError } = useOverview();

  return (
    <div className="mx-auto flex w-full max-w-[1280px] flex-col gap-6">
      <div>
        <h1 className="text-text">{t("overview.title")}</h1>
        <p className="m-0 text-[13.5px] text-neutral-400">{t("overview.subtitle")}</p>
      </div>

      {isLoading ? (
        <p className="text-neutral-400">{t("overview.loading")}</p>
      ) : isError || !data ? (
        <p role="alert" className="text-critical">
          {t("overview.loadError")}
        </p>
      ) : (
        <>
          <SummaryGrid data={data} />
          <Card elevation="elev-sm" className="p-4">
            <UptimeChart series={data.uptime_series} />
          </Card>
          <div className="grid gap-4 lg:grid-cols-2">
            <Card elevation="elev-sm" className="p-4">
              <RecentIncidents incidents={data.recent_incidents} />
            </Card>
            <Card elevation="elev-sm" className="p-4">
              <Shortcuts />
            </Card>
          </div>
        </>
      )}
    </div>
  );
}
