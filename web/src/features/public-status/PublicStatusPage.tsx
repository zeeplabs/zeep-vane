import { useEffect, useRef, useState } from "react";
import { useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { MdOutlineSchedule, MdOutlineWbSunny, MdOutlineNightlight } from "react-icons/md";
import { Card } from "../../components/ui/Card";
import { Button } from "../../components/ui/Button";
import { Skeleton } from "../../components/ui/Skeleton";
import { Tag } from "../../components/ui/Tag";
import type { TagVariant } from "../../components/ui/Tag";
import { Seg } from "../../components/ui/Seg";
import type { SegOption } from "../../components/ui/Seg";
import { Popover } from "../../components/ui/Popover";
import type {
  PublicDegradedEpisode,
  PublicHistoryBucket,
  PublicHourlyStatus,
  PublicIncidentEntry,
  PublicServiceStatus,
  RangeKey,
} from "../../lib/publicStatus";
import { resolveAssetUrl } from "../../lib/apiClient";
import { usePublicStatusPage } from "./hooks";
import { usePublicStatusTheme } from "./usePublicStatusTheme";
import publicStatusI18n from "./i18n";
import { formatRelativeTime, formatDateTime, formatDuration } from "./format";

type Translator = (key: string, opts?: Record<string, unknown>) => string;

const overallColorVar: Record<PublicServiceStatus, string> = {
  operational: "--color-success",
  degraded: "--color-warning",
  outage: "--color-critical",
};

const serviceTagVariant: Record<PublicServiceStatus, TagVariant> = {
  operational: "success",
  degraded: "warning",
  outage: "critical",
};

// hourlyColorVar covers every PublicHourlyStatus, including "no_data"
// (light gray, UPT-02) - overallColorVar/serviceTagVariant above only
// cover PublicServiceStatus, which has no no_data case.
const hourlyColorVar: Record<PublicHourlyStatus, string> = {
  operational: "--color-success",
  degraded: "--color-warning",
  outage: "--color-critical",
  no_data: "--color-neutral-600",
};

function hourlyTooltipFormatter(locale: string): Intl.DateTimeFormat {
  return new Intl.DateTimeFormat(locale, {
    day: "2-digit",
    month: "2-digit",
    hour: "2-digit",
    hour12: false,
    // Always America/Sao_Paulo regardless of the visitor's own browser/OS
    // timezone - the offset is computed client-side, but the timezone
    // itself is fixed, not detected (unlike `locale`, which now is).
    timeZone: "America/Sao_Paulo",
  });
}

// hourlyTooltip formats a bar's local date, hour range, and status label
// (UPT-05), always in America/Sao_Paulo (see hourlyTooltipFormatter).
function hourlyTooltip(bucket: PublicHistoryBucket, t: Translator, locale: string): string {
  const start = new Date(bucket.start);
  const parts = hourlyTooltipFormatter(locale).formatToParts(start);
  const get = (type: string) => parts.find((p) => p.type === type)?.value ?? "";
  const day = get("day");
  const month = get("month");
  const startHour = Number(get("hour")) % 24;
  const endHour = (startHour + 1) % 24;
  return `${day}/${month}, ${startHour}h–${endHour}h · ${t(`publicStatus.hourlyStatus.${bucket.status}`)}`;
}

// episodeTimeFormatter formats a degraded episode's start/end with minute
// precision (unlike hourlyTooltipFormatter above, which only needs hour
// precision for a whole bucket) - degraded-interval-analysis episodes can
// start/end at any minute within a bucket. Same fixed-timezone convention.
function episodeTimeFormatter(locale: string): Intl.DateTimeFormat {
  return new Intl.DateTimeFormat(locale, {
    day: "2-digit",
    month: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
    timeZone: "America/Sao_Paulo",
  });
}

// episodeTimeRange formats one degraded episode's start-end range
// (degraded-interval-analysis DEGINT-13). A null ends_at (still open as of
// the response) renders as "ongoing"/"em andamento" instead of a
// fabricated end time.
function episodeTimeRange(episode: PublicDegradedEpisode, t: Translator, locale: string): string {
  const formatter = episodeTimeFormatter(locale);
  const starts = formatter.format(new Date(episode.starts_at));
  if (!episode.ends_at) {
    return `${starts} – ${t("publicStatus.ongoing")}`;
  }
  const ends = formatter.format(new Date(episode.ends_at));
  return `${starts} – ${ends}`;
}

// RANGE_OPTIONS feeds the Seg range selector (public-status-time-range-
// selector T8) - values match RangeKey/rangeSpecs exactly (24h/7d/30d/90d).
const RANGE_OPTIONS: SegOption[] = [
  { value: "24h", label: "24h" },
  { value: "7d", label: "7d" },
  { value: "30d", label: "30d" },
  { value: "90d", label: "90d" },
];

const incidentTagVariant: Record<PublicIncidentEntry["status"], TagVariant> = {
  investigating: "critical",
  identified: "warning",
  monitoring: "warning",
  resolved: "neutral",
};

// formatUptimePercent renders the backend's nullable uptime_percent
// (TRS-04: recomputed for whichever range is currently selected, never
// pinned to 24h) - "—" when the service has zero recorded intervals within
// the selected window, matching the backend's own "render a dash" contract
// (internal/api/public_status_handler.go's publicServiceResponse doc).
function formatUptimePercent(uptimePercent: number | null): string {
  if (uptimePercent === null) return "—";
  return `${uptimePercent.toFixed(2)}% uptime`;
}

function worstServiceStatus(statuses: PublicServiceStatus[]): PublicServiceStatus {
  if (statuses.includes("outage")) return "outage";
  if (statuses.includes("degraded")) return "degraded";
  return "operational";
}

function ClockIcon() {
  return <MdOutlineSchedule size={13} style={{ color: "var(--color-warning)" }} aria-hidden="true" />;
}

function SunIcon() {
  return <MdOutlineWbSunny size={16} aria-hidden="true" data-testid="theme-icon-sun" />;
}

function MoonIcon() {
  return <MdOutlineNightlight size={16} aria-hidden="true" data-testid="theme-icon-moon" />;
}

function IncidentCard({
  incident,
  tone,
  t,
  locale,
}: {
  incident: PublicIncidentEntry;
  tone: "active" | "resolved";
  t: Translator;
  locale: string;
}) {
  const [expanded, setExpanded] = useState(false);

  return (
    <Card
      className="flex flex-col gap-2.5 rounded-md border border-divider p-[18px_20px]"
      style={
        tone === "active"
          ? {
              border: "1px solid color-mix(in oklch, var(--color-critical) 30%, var(--color-divider))",
              background: "color-mix(in oklch, var(--color-critical) 8%, var(--color-surface))",
            }
          : undefined
      }
    >
      <div
        className="flex cursor-pointer items-start justify-between gap-3"
        onClick={() => setExpanded((v) => !v)}
      >
        <div className="flex flex-col gap-1.5">
          <p className="text-[15px] font-bold text-text">{incident.title}</p>
          {incident.description ? (
            <p className="text-[13px] leading-relaxed text-neutral-300">{incident.description}</p>
          ) : null}
          {tone === "active" ? (
            <div className="flex flex-wrap gap-1.5">
              {incident.service_names.map((name) => (
                <Tag key={name} variant="neutral">
                  {name}
                </Tag>
              ))}
            </div>
          ) : (
            <p className="text-xs text-neutral-400">
              {t("publicStatus.resolvedAt", {
                time: formatDateTime(incident.resolved_at!, locale),
                duration: formatDuration(incident.created_at, incident.resolved_at!),
              })}
            </p>
          )}
        </div>
        <Tag
          variant={tone === "active" ? incidentTagVariant[incident.status] : "neutral"}
          style={{ fontSize: "11.5px", fontWeight: 700, padding: "4px 10px" }}
        >
          {t(`publicStatus.incidentStatus.${tone === "active" ? incident.status : "resolved"}`)}
        </Tag>
      </div>

      {expanded ? (
        <div className="ml-1 flex flex-col gap-3.5 border-l-2 border-divider pl-4">
          {incident.updates.map((u, i) => (
            <div key={i} className="flex flex-col gap-0.5">
              <span className="text-[11px] font-semibold text-neutral-400">
                {formatDateTime(u.created_at, locale)}
              </span>
              <p className="text-[12.5px] leading-relaxed text-neutral-200">{u.body}</p>
            </div>
          ))}
        </div>
      ) : null}

      <button
        type="button"
        className="cursor-pointer self-start text-[12.5px] font-semibold text-accent"
        onClick={() => setExpanded((v) => !v)}
      >
        {expanded ? t("publicStatus.hideTimeline") : t("publicStatus.showTimeline")}
      </button>
    </Card>
  );
}

function LoadingSkeleton({ loadingLabel }: { loadingLabel: string }) {
  return (
    <div aria-busy="true" className="mx-auto flex w-full max-w-[720px] flex-col gap-4 px-4 py-14">
      <span className="sr-only">{loadingLabel}</span>
      <Skeleton width={224} height={24} />
      <Skeleton width="100%" height={64} />
      <Skeleton width="100%" height={48} />
      <Skeleton width="100%" height={48} />
      <Skeleton width="100%" height={48} />
    </div>
  );
}

// PublicStatusPage renders both the admin-authenticated dev/preview route
// (/status/:id) and the real production root (AD-018, no id - resolved by
// the request's own hostname on the Go side). useParams() naturally
// returns undefined for :id when this is mounted outside that route.
export function PublicStatusPage() {
  const { t, i18n } = useTranslation("translation", { i18n: publicStatusI18n });
  const locale = i18n.language;
  const { id } = useParams();
  const [range, setRange] = useState<RangeKey>("24h");
  const { data, isLoading, isError, hasMoreResolved, loadMoreResolvedIncidents } = usePublicStatusPage(id, range);
  const [loadingMore, setLoadingMore] = useState(false);
  // Theme scoped to this page only (PUBSTATUS-07..10) - default light,
  // independent of the app's own `vane:theme`. Applied via `data-theme` on
  // this component's own root wrapper below, never on
  // document.documentElement.
  const { theme, toggleTheme } = usePublicStatusTheme();

  // This is the one page in the SPA the public internet actually lands on
  // (search results, shared links, embedded status badges) - index.html's
  // static <title>/description describe the admin product, not this
  // specific company's status, so they're overridden here per visit and
  // restored on unmount (defaultTitle/defaultDescription captured once, on
  // mount, so navigating between two different status pages client-side
  // doesn't leak the wrong title if this component ever gets reused).
  const defaultTitleRef = useRef<string>(document.title);
  const defaultDescriptionRef = useRef<string | null>(
    document.querySelector('meta[name="description"]')?.getAttribute("content") ?? null,
  );

  useEffect(() => {
    if (!data) return;

    document.title = `${data.company_name} Status`;

    const descriptionTag = document.querySelector('meta[name="description"]');
    descriptionTag?.setAttribute(
      "content",
      `${data.company_name} — ${t(`publicStatus.overallStatus.${worstServiceStatus(data.services.map((s) => s.status))}`)}.`,
    );

    return () => {
      document.title = defaultTitleRef.current;
      if (defaultDescriptionRef.current !== null) {
        descriptionTag?.setAttribute("content", defaultDescriptionRef.current);
      }
    };
  }, [data]);

  async function handleLoadMore() {
    setLoadingMore(true);
    try {
      await loadMoreResolvedIncidents();
    } finally {
      setLoadingMore(false);
    }
  }

  // data-theme applied here (not document.documentElement) so tokens.css's
  // existing [data-theme="dark"] rule cascades only within this subtree -
  // isolates the toggle to this one page (PUBSTATUS-08).
  const themeAttr = theme === "dark" ? "dark" : undefined;

  if (isLoading) {
    return (
      <div data-theme={themeAttr} data-testid="public-status-theme-root" className="min-h-screen bg-bg">
        <LoadingSkeleton loadingLabel={t("publicStatus.loading")} />
      </div>
    );
  }

  if (isError || !data) {
    return (
      <div data-theme={themeAttr} data-testid="public-status-theme-root" className="min-h-screen bg-bg">
        <div className="mx-auto flex w-full max-w-[720px] flex-col items-center gap-2 px-4 py-24 text-center">
          <p className="text-text">{t("publicStatus.notFoundTitle")}</p>
          <p className="text-sm text-neutral-400">{t("publicStatus.notFoundSubtitle")}</p>
        </div>
      </div>
    );
  }

  const overall = worstServiceStatus(data.services.map((s) => s.status));
  const overallColor = overallColorVar[overall];

  return (
    <div data-theme={themeAttr} data-testid="public-status-theme-root" className="min-h-screen bg-bg">
    <div className="mx-auto flex w-full max-w-[720px] flex-col gap-7 px-[24px] pt-[56px] pb-[80px]">
      <header className="flex flex-wrap items-center justify-between gap-3">
        {data.logo_url ? (
          <img src={resolveAssetUrl(data.logo_url)!} alt={data.company_name} className="h-11 w-auto" />
        ) : (
          <span className="text-[17px] font-semibold tracking-tight text-text">{data.company_name}</span>
        )}
        <div className="flex items-center gap-3">
          <button
            type="button"
            onClick={toggleTheme}
            aria-label={t("topbar.toggleTheme")}
            className="flex cursor-pointer items-center text-neutral-400 hover:text-text"
          >
            {theme === "dark" ? <SunIcon /> : <MoonIcon />}
          </button>
          <div className="flex items-center gap-1.5 text-xs text-neutral-400">
            {data.stale ? <ClockIcon /> : null}
            <span>{t("publicStatus.updatedAt", { time: formatRelativeTime(data.updated_at, t) })}</span>
          </div>
        </div>
      </header>

      <div
        className="flex items-center gap-3 rounded-md p-[18px_22px]"
        style={{
          background: `color-mix(in oklch, var(${overallColor}) 14%, var(--color-surface))`,
        }}
      >
        <div
          className="h-[10px] w-[10px] flex-none rounded-full"
          style={{ background: `var(${overallColor})` }}
        />
        <div className="flex flex-col gap-0.5">
          <p className="text-base font-bold text-text">{t(`publicStatus.overallStatus.${overall}`)}</p>
          {data.stale ? (
            <p className="text-xs text-neutral-400">{t("publicStatus.staleNotice")}</p>
          ) : null}
        </div>
      </div>

      {data.incidents.active.length > 0 ? (
        <section className="flex flex-col gap-3">
          <h2 className="text-xs uppercase tracking-wide text-neutral-400">{t("publicStatus.activeIncidentHeading")}</h2>
          {data.incidents.active.map((incident) => (
            <IncidentCard key={incident.id} incident={incident} tone="active" t={t} locale={locale} />
          ))}
        </section>
      ) : null}

      <section className="flex flex-col gap-3">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h2 className="text-xs uppercase tracking-wide text-neutral-400">{t("publicStatus.servicesHeading")}</h2>
          <Seg
            options={RANGE_OPTIONS}
            value={range}
            onChange={(value) => setRange(value as RangeKey)}
            aria-label={t("publicStatus.selectRangeLabel")}
          />
        </div>
        <div className="flex flex-col gap-2.5">
          {data.services.map((service) => (
            <Card
              key={service.name}
              className="flex flex-col gap-2.5 rounded-md border border-divider p-[16px_20px]"
            >
              <div className="flex items-center justify-between gap-3">
                <div className="flex min-w-0 items-center gap-2.5">
                  <span
                    className="h-[7px] w-[7px] flex-none rounded-full"
                    style={{ background: `var(${overallColorVar[service.status]})` }}
                  />
                  <span className="truncate text-sm font-bold text-text">{service.name}</span>
                  {!service.last_updated_at ? (
                    <span className="text-xs text-neutral-500">{t("publicStatus.noDataLabel")}</span>
                  ) : null}
                </div>
                <div className="flex flex-shrink-0 items-center gap-2.5">
                  <span className="text-xs text-neutral-400" data-testid={`uptime-${service.name}`}>
                    {formatUptimePercent(service.uptime_percent)}
                  </span>
                  {/* status_analysis (AI-16) renders as a native tooltip on
                      the badge only for a degraded service that already has
                      a finished analysis - same title/tabIndex accessibility
                      pattern as hourlyTooltip above, never a fabricated
                      tooltip while the analysis is still pending or for any
                      non-degraded status. */}
                  <Tag
                    variant={serviceTagVariant[service.status]}
                    style={{ fontSize: "11.5px", fontWeight: 700, padding: "3px 10px" }}
                    {...(service.status === "degraded" && service.status_analysis
                      ? { title: service.status_analysis, tabIndex: 0 }
                      : {})}
                  >
                    {t(`publicStatus.serviceStatus.${service.status}`)}
                  </Tag>
                </div>
              </div>
              <div className="flex flex-col gap-1.5">
                <div className="flex gap-[2px]">
                  {service.history.map((bucket, i) => {
                    const barStyle = {
                      background:
                        bucket.status === "no_data"
                          ? `color-mix(in oklch, var(${hourlyColorVar.no_data}) 40%, var(--color-surface))`
                          : `var(${hourlyColorVar[bucket.status]})`,
                    };
                    // Degraded-interval-analysis (DEGINT-13/14/15/16): only
                    // a bucket carrying at least one episode becomes a real
                    // click target (Popover); every other bucket keeps its
                    // existing hover-title-only behavior unchanged.
                    if (bucket.episodes && bucket.episodes.length > 0) {
                      const episodes = bucket.episodes;
                      return (
                        <Popover
                          key={i}
                          trigger={
                            <button
                              type="button"
                              title={hourlyTooltip(bucket, t, locale)}
                              className="h-[24px] flex-1 rounded-[2px] cursor-pointer border-0 p-0"
                              style={barStyle}
                              data-testid={`hourly-bar-${service.name}-${i}`}
                            />
                          }
                        >
                          <p className="m-0 mb-2 text-xs font-bold text-text">
                            {t("publicStatus.episodePopover.heading")}
                          </p>
                          <div className="flex flex-col gap-2">
                            {episodes.map((episode, j) => (
                              <div key={j}>
                                <p className="m-0 text-[11px] text-neutral-400">
                                  {episodeTimeRange(episode, t, locale)}
                                </p>
                                <p className="m-0 text-xs text-text">
                                  {episode.analysis ?? t("publicStatus.episodePopover.noReasonRecorded")}
                                </p>
                              </div>
                            ))}
                          </div>
                        </Popover>
                      );
                    }
                    return (
                      <div
                        key={i}
                        title={hourlyTooltip(bucket, t, locale)}
                        tabIndex={0}
                        className="h-[24px] flex-1 rounded-[2px]"
                        style={barStyle}
                        data-testid={`hourly-bar-${service.name}-${i}`}
                      />
                    );
                  })}
                </div>
                <div className="flex justify-between text-[10.5px] text-neutral-500">
                  <span>{t(`publicStatus.rangeAgo.${range}`)}</span>
                  <span>{t("publicStatus.now")}</span>
                </div>
              </div>
            </Card>
          ))}
        </div>
      </section>

      <section className="flex flex-col gap-3">
        <h2 className="text-xs uppercase tracking-wide text-neutral-400">{t("publicStatus.historyHeading")}</h2>
        {data.incidents.resolved.length > 0 ? (
          <div className="flex flex-col gap-2">
            {data.incidents.resolved.map((incident) => (
              <IncidentCard key={incident.id} incident={incident} tone="resolved" t={t} locale={locale} />
            ))}
            {hasMoreResolved ? (
              <Button
                variant="secondary"
                className="self-center"
                onClick={handleLoadMore}
                disabled={loadingMore}
              >
                {t("publicStatus.loadMore")}
              </Button>
            ) : null}
          </div>
        ) : (
          <p className="text-sm text-neutral-500">{t("publicStatus.noHistoryIncidents")}</p>
        )}
      </section>

      <footer className="mt-[48px] text-center text-xs text-neutral-500">
        {t("publicStatus.poweredByPrefix")} <span className="font-semibold text-accent">Vane</span>
        <span className="mx-1.5">·</span>
        {t("publicStatus.autoRefreshNotice")}
      </footer>
    </div>
    </div>
  );
}
