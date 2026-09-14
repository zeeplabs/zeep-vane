import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Bar, BarChart, ResponsiveContainer } from "recharts";
import { MdOutlineAdd, MdOutlineWeb, MdOutlineGroupAdd, MdOutlinePublic, MdOutlineBolt } from "react-icons/md";
import { Card } from "../../components/ui/Card";
import type { OverviewIncident, OverviewResponse, OverviewUptimeBucket } from "../../types/api";
import { useOverview } from "./hooks";

// bucketColorVar mirrors handoff-new-layout/Visao Geral.dc.html's chartBars
// color rule: v < 98.5 -> warning, v < 99.5 -> accent, else success. null (no
// data) is neutral gray, never a fabricated value (OVW-07).
function bucketColorVar(uptimePercent: number | null): string {
  if (uptimePercent === null) return "--color-neutral-600";
  if (uptimePercent < 98.5) return "--color-warning";
  if (uptimePercent < 99.5) return "--color-accent";
  return "--color-success";
}

// barHeightPercent mirrors the handoff's normalization: a day's bar height
// is scaled across a narrow 96-100% band (not 0-100%) so day-to-day
// variance is visible at the uptimes this product actually sees, floored at
// 8% so a bar is never invisible. null (no data) also floors to 8%.
function barHeightPercent(uptimePercent: number | null): number {
  if (uptimePercent === null) return 8;
  const min = 96;
  const max = 100;
  return Math.max(8, Math.round(((uptimePercent - min) / (max - min)) * 100));
}

// formatDayLabel turns the backend's YYYY-MM-DD local day into DD/MM for the
// per-bar tooltip, without constructing a Date (which would reintroduce a
// timezone shift).
function formatDayLabel(date: string): string {
  const [year, month, day] = date.split("-");
  if (!year || !month || !day) return date;
  return `${day}/${month}`;
}

const incidentDotVar: Record<string, string> = {
  investigating: "--color-critical",
  identified: "--color-warning",
  monitoring: "--color-warning",
  resolved: "--color-success",
};

function formatTimestamp(iso: string, locale: string): string {
  return new Date(iso).toLocaleString(locale, { day: "2-digit", month: "short", hour: "2-digit", minute: "2-digit" });
}

const SHORTCUT_ICONS = {
  addService: MdOutlineAdd,
  createStatusPage: MdOutlineWeb,
  inviteUser: MdOutlineGroupAdd,
  viewDomains: MdOutlinePublic,
} as const;

const SHORTCUTS = [
  { key: "addService", to: "/services" },
  { key: "createStatusPage", to: "/domains" },
  { key: "inviteUser", to: "/admins" },
  { key: "viewDomains", to: "/domains" },
] as const;

// ACTIVITY_FEED is a static placeholder (no ActivityEvent backend model
// exists yet - user decision 2026-09-14: hardcode visually, don't fabricate
// a live feed). Mirrors handoff-new-layout/Visao Geral.dc.html's ACTIVITY
// fixture exactly (4 entries) - example data, not real tenant state.
const ACTIVITY_FEED = [
  { initials: "AS", name: "Ana Silva", action: "overview.activity.resolvedIncident", values: { title: "Timeout no Auth Service" }, when: "overview.activity.days5" },
  { initials: "RN", name: "Rafael Nunes", action: "overview.activity.invitedMember", values: undefined, when: "overview.activity.yesterday" },
  { initials: "DR", name: "Diego Rocha", action: "overview.activity.addedDomain", values: { domain: "painel.acme.health" }, when: "overview.activity.days5" },
  { initials: "AS", name: "Ana Silva", action: "overview.activity.updatedPlan", values: { plan: "Free" }, when: "overview.activity.weekAgo" },
] as const;

function trendLabel(t: (key: string, opts?: Record<string, unknown>) => string, current: number | null, prior: number | null): string | null {
  if (current === null || prior === null) return null;
  const delta = Math.round((current - prior) * 10) / 10;
  if (delta === 0) return null;
  const abs = Math.abs(delta).toFixed(1) + "%";
  return delta > 0 ? t("overview.cards.uptimeTrendUp", { value: abs }) : t("overview.cards.uptimeTrendDown", { value: `-${abs}` });
}

function SummaryCard({
  testId,
  label,
  value,
  valueColorVar,
  subtext,
}: {
  testId: string;
  label: string;
  value: string;
  valueColorVar: string;
  subtext?: string | null;
}) {
  return (
    <Card elevation="none" className="border border-divider flex flex-col gap-1 p-4">
      <span className="text-[11.5px] font-medium uppercase tracking-wide text-text-muted">{label}</span>
      <span data-testid={testId} className="text-2xl font-semibold" style={{ color: `var(${valueColorVar})` }}>
        {value}
      </span>
      {subtext ? <span className="text-xs text-text-muted">{subtext}</span> : null}
    </Card>
  );
}

// ChartDatum is what we hand to recharts: displayHeight is the chart-only
// normalized bar height (barHeightPercent); uptimePercent stays the real
// value (or null) for the label and color.
interface ChartDatum {
  date: string;
  uptimePercent: number | null;
  displayHeight: number;
}

// UptimeBar is recharts' custom Bar `shape` - it receives the bar's
// computed x/y/width/height and the original datum, and is where we attach
// the accessible label/testid the component tests assert on. This is the
// one place a bar's pixel geometry is touched directly; recharts still owns
// scale/layout/axes.
interface UptimeBarProps {
  x?: number;
  y?: number;
  width?: number;
  height?: number;
  index?: number | string;
  payload?: ChartDatum;
}

function UptimeBar(props: UptimeBarProps) {
  const { t } = useTranslation();
  const { x, y, width, height, payload, index } = props;
  if (x === undefined || y === undefined || width === undefined || height === undefined || !payload) return null;

  const value = payload.uptimePercent === null ? t("overview.noData") : `${payload.uptimePercent.toFixed(1)}%`;
  const label = t("overview.chart.barLabel", { date: formatDayLabel(payload.date), value });

  return (
    <rect
      x={x}
      y={y}
      width={width}
      height={height}
      rx={3}
      role="img"
      aria-label={label}
      tabIndex={0}
      data-testid={`overview-bar-${index}`}
      fill={`var(${bucketColorVar(payload.uptimePercent)})`}
    >
      <title>{label}</title>
    </rect>
  );
}

function UptimeChart({ series, average }: { series: OverviewUptimeBucket[]; average: number | null }) {
  const { t } = useTranslation();

  const data: ChartDatum[] = series.map((bucket) => ({
    date: bucket.date,
    uptimePercent: bucket.uptime_percent,
    displayHeight: barHeightPercent(bucket.uptime_percent),
  }));

  return (
    <div className="flex h-full flex-col gap-4">
      <div className="flex items-center justify-between">
        <h3 className="m-0 text-[13.5px] font-bold text-text">{t("overview.chart.title")}</h3>
        {average !== null ? (
          <span className="text-xs text-text-muted">{t("overview.chart.average", { value: `${average.toFixed(2)}%` })}</span>
        ) : null}
      </div>
      <div className="min-h-28 flex-1">
        <ResponsiveContainer width="100%" height="100%">
          <BarChart data={data} margin={{ top: 0, right: 0, bottom: 0, left: 0 }} barCategoryGap="20%">
            <Bar dataKey="displayHeight" isAnimationActive={false} shape={(props: unknown) => <UptimeBar {...(props as UptimeBarProps)} />} />
          </BarChart>
        </ResponsiveContainer>
      </div>
      <div className="flex items-center justify-between text-[10.5px] text-text-muted">
        <span>{t("overview.chart.daysAgo")}</span>
        <span>{t("overview.chart.today")}</span>
      </div>
    </div>
  );
}

function RecentIncidents({ incidents }: { incidents: OverviewIncident[] }) {
  const { t, i18n } = useTranslation();

  return (
    <div className="flex h-full flex-col gap-3.5">
      <div className="flex items-center justify-between">
        <h3 className="m-0 text-[13.5px] font-bold text-text">{t("overview.incidents.title")}</h3>
        <Link to="/incidents" className="text-xs font-medium text-accent hover:underline">
          {t("overview.incidents.viewAll")}
        </Link>
      </div>
      {incidents.length === 0 ? (
        <p className="m-0 text-[13px] text-text-muted">{t("overview.incidents.empty")}</p>
      ) : (
        <div className="flex flex-col gap-3">
          {incidents.map((incident) => (
            <div key={incident.id} className="flex items-start gap-2.5">
              <span
                className="mt-[5px] h-[7px] w-[7px] shrink-0 rounded-full"
                style={{ background: `var(${incidentDotVar[incident.status] ?? "--color-neutral-600"})` }}
              />
              <div className="min-w-0">
                <div className="truncate text-[12.5px] font-semibold text-text">{incident.title}</div>
                <div className="text-[11.5px] text-text-muted">
                  {t(`overview.incidentStatus.${incident.status}`)} · {formatTimestamp(incident.created_at, i18n.language)}
                </div>
              </div>
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
      <h3 className="m-0 text-[13.5px] font-bold text-text">{t("overview.shortcuts.title")}</h3>
      <div className="grid grid-cols-2 gap-3.5 lg:grid-cols-4">
        {SHORTCUTS.map((shortcut) => {
          const Icon = SHORTCUT_ICONS[shortcut.key];
          return (
            <Link
              key={shortcut.key}
              to={shortcut.to}
              className="flex flex-col gap-2.5 rounded-md border border-divider bg-surface p-4 text-[13px] font-semibold text-text transition-colors hover:bg-card-header-bg"
            >
              <span className="text-accent">
                <Icon size={19} aria-hidden="true" />
              </span>
              {t(`overview.shortcuts.${shortcut.key}`)}
            </Link>
          );
        })}
      </div>
    </div>
  );
}

function UpsellBanner() {
  const { t } = useTranslation();
  return (
    <div
      className="flex items-center gap-2.5 rounded-md border px-3.5 py-3"
      style={{ borderColor: "var(--color-warning)", background: "color-mix(in srgb, var(--color-warning) 8%, var(--color-surface))" }}
    >
      <span aria-hidden="true" className="shrink-0" style={{ color: "var(--color-warning)" }}>
        <MdOutlineBolt size={16} />
      </span>
      <div className="flex-1 text-[12.5px] leading-snug text-text">{t("overview.upsell.message")}</div>
      <button
        type="button"
        className="shrink-0 rounded-md bg-accent px-3 py-1.5 text-[12.5px] font-bold text-white hover:bg-accent-hover"
      >
        {t("overview.upsell.cta")}
      </button>
    </div>
  );
}

function RecentActivity() {
  const { t } = useTranslation();
  return (
    <Card elevation="none" className="border border-divider flex flex-col gap-3.5 p-5">
      <h3 className="m-0 text-[13.5px] font-bold text-text">{t("overview.activity.title")}</h3>
      <div className="flex flex-col gap-3.5">
        {ACTIVITY_FEED.map((entry, i) => (
          <div key={i} className="flex items-start gap-3">
            <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md bg-accent-100 text-[11px] font-bold text-accent-700">
              {entry.initials}
            </span>
            <div className="min-w-0">
              <div className="text-[12.5px] text-text">
                <span className="font-bold">{entry.name}</span> {t(entry.action, entry.values)}
              </div>
              <div className="mt-0.5 text-[11.5px] text-text-muted">{t(entry.when)}</div>
            </div>
          </div>
        ))}
      </div>
    </Card>
  );
}

function SummaryGrid({ data, t }: { data: OverviewResponse; t: (key: string, opts?: Record<string, unknown>) => string }) {
  const uptime = data.uptime_avg_30d === null ? t("overview.noData") : `${data.uptime_avg_30d.toFixed(1)}%`;
  const uptimeSubtext = trendLabel(t, data.uptime_avg_30d, data.uptime_avg_30d_prior);

  const incidentsSubtext =
    data.open_incidents_critical > 0 || data.open_incidents_monitoring > 0
      ? t("overview.cards.openIncidentsBreakdown", {
          critical: data.open_incidents_critical,
          monitoring: data.open_incidents_monitoring,
        })
      : null;

  const pendingDomains = data.total_domains - data.verified_domains;

  return (
    <div className="grid grid-cols-2 gap-3.5 lg:grid-cols-4">
      <SummaryCard
        testId="overview-card-uptime"
        label={t("overview.cards.uptime")}
        value={uptime}
        valueColorVar="--color-success"
        subtext={uptimeSubtext}
      />
      <SummaryCard
        testId="overview-card-open-incidents"
        label={t("overview.cards.openIncidents")}
        value={String(data.open_incidents)}
        valueColorVar="--color-critical"
        subtext={incidentsSubtext}
      />
      <SummaryCard
        testId="overview-card-unhealthy"
        label={t("overview.cards.unhealthyServices")}
        value={String(data.unhealthy_services)}
        valueColorVar="--color-warning"
        subtext={t("overview.cards.unhealthyServicesTotal", { total: data.total_services })}
      />
      <SummaryCard
        testId="overview-card-domains"
        label={t("overview.cards.verifiedDomains")}
        value={`${data.verified_domains}/${data.total_domains}`}
        valueColorVar="--color-accent"
        subtext={pendingDomains > 0 ? t("overview.cards.verifiedDomainsPending", { pending: pendingDomains }) : null}
      />
    </div>
  );
}

// OverviewPage is the authenticated landing screen (dashboard-overview-page
// OVW-01/02). Layout matches handoff-new-layout/Visao Geral.dc.html: 4
// summary cards (fixed semantic color per card), a 1.4fr/1fr chart+incidents
// row, a 4-up shortcuts row, and an activity card. The upsell banner and the
// "Atividade recente do time" card are static placeholders (2026-09-14
// decision: no Tenant.Plan/billing model and no ActivityEvent model exist
// yet) - they render fixed example content, not real tenant state.
export function OverviewPage() {
  const { t } = useTranslation();
  const { data, isLoading, isError } = useOverview();

  return (
    <div className="mx-auto flex w-full max-w-[1280px] flex-col gap-6">
      <div>
        <h1 className="text-text">{t("overview.title")}</h1>
        <p className="m-0 text-[13.5px] text-neutral-400">{t("overview.subtitle")}</p>
      </div>

      <UpsellBanner />

      {isLoading ? (
        <p className="text-neutral-400">{t("overview.loading")}</p>
      ) : isError || !data ? (
        <p role="alert" className="text-critical">
          {t("overview.loadError")}
        </p>
      ) : (
        <>
          <SummaryGrid data={data} t={t} />
          <div className="grid gap-4 lg:grid-cols-[1.4fr_1fr]">
            <Card elevation="none" className="border border-divider p-5">
              <UptimeChart series={data.uptime_series} average={average(data.uptime_series)} />
            </Card>
            <Card elevation="none" className="border border-divider p-5">
              <RecentIncidents incidents={data.recent_incidents} />
            </Card>
          </div>
          <Shortcuts />
          <RecentActivity />
        </>
      )}
    </div>
  );
}

function average(series: OverviewUptimeBucket[]): number | null {
  const values = series.map((b) => b.uptime_percent).filter((v): v is number => v !== null);
  if (values.length === 0) return null;
  return values.reduce((sum, v) => sum + v, 0) / values.length;
}
