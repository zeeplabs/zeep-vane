import { useEffect, useRef, useState } from "react";
import { useParams } from "react-router-dom";
import { Card } from "../../components/ui/Card";
import { Button } from "../../components/ui/Button";
import { Tag } from "../../components/ui/Tag";
import type { TagVariant } from "../../components/ui/Tag";
import { Seg } from "../../components/ui/Seg";
import type { SegOption } from "../../components/ui/Seg";
import type {
  PublicHistoryBucket,
  PublicHourlyStatus,
  PublicIncidentEntry,
  PublicServiceStatus,
  RangeKey,
} from "../../lib/publicStatus";
import { resolveAssetUrl } from "../../lib/apiClient";
import { usePublicStatusPage } from "./hooks";
import { formatRelativeTime, formatDateTime, formatDuration } from "./format";

const overallCopy: Record<PublicServiceStatus, { label: string; colorVar: string }> = {
  operational: { label: "Todos os sistemas operacionais", colorVar: "--color-success" },
  degraded: { label: "Interrupção parcial em andamento", colorVar: "--color-warning" },
  outage: { label: "Interrupção em andamento", colorVar: "--color-critical" },
};

const serviceTagVariant: Record<PublicServiceStatus, TagVariant> = {
  operational: "success",
  degraded: "warning",
  outage: "critical",
};

const serviceLabel: Record<PublicServiceStatus, string> = {
  operational: "Operacional",
  degraded: "Degradado",
  outage: "Interrupção",
};

// hourlyColorVar covers every PublicHourlyStatus, including "no_data"
// (light gray, UPT-02) - overallCopy/serviceTagVariant above only cover
// PublicServiceStatus, which has no no_data case.
const hourlyColorVar: Record<PublicHourlyStatus, string> = {
  operational: "--color-success",
  degraded: "--color-warning",
  outage: "--color-critical",
  no_data: "--color-neutral-600",
};

const hourlyLabel: Record<PublicHourlyStatus, string> = {
  operational: "Operacional",
  degraded: "Degradado",
  outage: "Interrupção",
  no_data: "Sem dados",
};

const HOURLY_TOOLTIP_FORMATTER = new Intl.DateTimeFormat("pt-BR", {
  day: "2-digit",
  month: "2-digit",
  hour: "2-digit",
  hour12: false,
  timeZone: "America/Sao_Paulo",
});

// hourlyTooltip formats a bar's local date, hour range, and PT-BR status
// label (UPT-05), always in America/Sao_Paulo regardless of the visitor's
// own browser/OS timezone - the offset is computed client-side, but the
// timezone itself is fixed, not detected.
function hourlyTooltip(bucket: PublicHistoryBucket): string {
  const start = new Date(bucket.start);
  const parts = HOURLY_TOOLTIP_FORMATTER.formatToParts(start);
  const get = (type: string) => parts.find((p) => p.type === type)?.value ?? "";
  const day = get("day");
  const month = get("month");
  const startHour = Number(get("hour")) % 24;
  const endHour = (startHour + 1) % 24;
  return `${day}/${month}, ${startHour}h–${endHour}h · ${hourlyLabel[bucket.status]}`;
}

// RANGE_OPTIONS feeds the Seg range selector (public-status-time-range-
// selector T8) - values match RangeKey/rangeSpecs exactly (24h/7d/30d/90d).
const RANGE_OPTIONS: SegOption[] = [
  { value: "24h", label: "24h" },
  { value: "7d", label: "7d" },
  { value: "30d", label: "30d" },
  { value: "90d", label: "90d" },
];

// rangeAgoLabel is the leftmost label under each service's history chart,
// keyed by the selected range - follows this file's existing hardcoded
// PT-BR Record<K,string> pattern (see hourlyLabel/incidentLabel above; this
// feature doesn't route strings through react-i18next anywhere else in
// this file, so a new i18n key here would be an inconsistent one-off).
const rangeAgoLabel: Record<RangeKey, string> = {
  "24h": "24h atrás",
  "7d": "7 dias atrás",
  "30d": "30 dias atrás",
  "90d": "90 dias atrás",
};

const incidentTagVariant: Record<PublicIncidentEntry["status"], TagVariant> = {
  investigating: "critical",
  identified: "warning",
  monitoring: "warning",
  resolved: "neutral",
};

const incidentLabel: Record<PublicIncidentEntry["status"], string> = {
  investigating: "Investigando",
  identified: "Identificado",
  monitoring: "Monitorando",
  resolved: "Resolvido",
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
  return (
    <svg
      width="13"
      height="13"
      viewBox="0 0 24 24"
      fill="none"
      stroke="var(--color-warning)"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <circle cx="12" cy="12" r="9" />
      <path d="M12 7v5l3 3" />
    </svg>
  );
}

function IncidentCard({ incident, tone }: { incident: PublicIncidentEntry; tone: "active" | "resolved" }) {
  const [expanded, setExpanded] = useState(false);

  return (
    <Card
      elevation="elev-sm"
      className="flex flex-col gap-2 p-4"
      style={tone === "active" ? { border: "1px solid color-mix(in oklch, var(--color-critical) 30%, var(--color-divider))" } : undefined}
    >
      <div
        className="flex cursor-pointer items-start justify-between gap-3"
        onClick={() => setExpanded((v) => !v)}
      >
        <div className="flex flex-col gap-1.5">
          <p className="text-[15px] font-medium text-text">{incident.title}</p>
          {incident.description ? (
            <p className="text-[13px] text-neutral-300">{incident.description}</p>
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
              Resolvido {formatDateTime(incident.resolved_at!)} · {formatDuration(incident.created_at, incident.resolved_at!)}
            </p>
          )}
        </div>
        <Tag variant={tone === "active" ? incidentTagVariant[incident.status] : "neutral"}>
          {tone === "active" ? incidentLabel[incident.status] : "Resolvido"}
        </Tag>
      </div>

      {expanded ? (
        <div className="flex flex-col gap-2 border-t border-divider pt-2">
          {incident.updates.map((u, i) => (
            <div key={i} className="flex gap-2">
              <span className="min-w-[96px] whitespace-nowrap text-[11.5px] text-neutral-400">
                {formatDateTime(u.created_at)}
              </span>
              <p className="text-[13px] text-neutral-200">{u.body}</p>
            </div>
          ))}
        </div>
      ) : null}

      <button
        type="button"
        className="cursor-pointer self-start text-xs text-accent hover:underline"
        onClick={() => setExpanded((v) => !v)}
      >
        {expanded ? "Ocultar linha do tempo" : "Ver linha do tempo"}
      </button>
    </Card>
  );
}

function LoadingSkeleton() {
  return (
    <div className="mx-auto flex w-full max-w-[720px] flex-col gap-4 px-4 py-14">
      <div className="h-6 w-56 animate-pulse rounded-md bg-neutral-800" />
      <div className="h-16 w-full animate-pulse rounded-md bg-neutral-800" />
      <div className="h-12 w-full animate-pulse rounded-md bg-neutral-800" />
      <div className="h-12 w-full animate-pulse rounded-md bg-neutral-800" />
    </div>
  );
}

// PublicStatusPage renders both the admin-authenticated dev/preview route
// (/status/:id) and the real production root (AD-018, no id - resolved by
// the request's own hostname on the Go side). useParams() naturally
// returns undefined for :id when this is mounted outside that route.
export function PublicStatusPage() {
  const { id } = useParams();
  const [range, setRange] = useState<RangeKey>("24h");
  const { data, isLoading, isError, hasMoreResolved, loadMoreResolvedIncidents } = usePublicStatusPage(id, range);
  const [loadingMore, setLoadingMore] = useState(false);

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
      `${data.company_name} — ${overallCopy[worstServiceStatus(data.services.map((s) => s.status))].label}.`,
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

  if (isLoading) return <LoadingSkeleton />;

  if (isError || !data) {
    return (
      <div className="mx-auto flex w-full max-w-[720px] flex-col items-center gap-2 px-4 py-24 text-center">
        <p className="text-text">Página não encontrada.</p>
        <p className="text-sm text-neutral-400">Verifique o endereço e tente novamente.</p>
      </div>
    );
  }

  const overall = worstServiceStatus(data.services.map((s) => s.status));
  const overallInfo = overallCopy[overall];

  return (
    <div className="mx-auto flex w-full max-w-[720px] flex-col gap-7 px-4 py-11">
      <header className="flex flex-wrap items-baseline justify-between gap-3">
        {data.logo_url ? (
          <img src={resolveAssetUrl(data.logo_url)!} alt={data.company_name} className="h-11 w-auto" />
        ) : (
          <span className="text-[15px] font-medium tracking-tight text-text">{data.company_name}</span>
        )}
        <div className="flex items-center gap-1.5 text-xs text-neutral-400">
          {data.stale ? <ClockIcon /> : null}
          <span>Atualizado {formatRelativeTime(data.updated_at)}</span>
        </div>
      </header>

      <div
        className="flex items-center gap-3 rounded-md p-4"
        style={{
          background: `color-mix(in oklch, var(${overallInfo.colorVar}) 10%, var(--color-neutral-900))`,
          border: `1px solid color-mix(in oklch, var(${overallInfo.colorVar}) 25%, var(--color-divider))`,
        }}
      >
        <div
          className="h-[11px] w-[11px] flex-none rounded-full"
          style={{
            background: `var(${overallInfo.colorVar})`,
            boxShadow: `0 0 12px color-mix(in oklch, var(${overallInfo.colorVar}) 60%, transparent)`,
          }}
        />
        <div className="flex flex-col gap-0.5">
          <p className="font-medium text-text">{overallInfo.label}</p>
          {data.stale ? (
            <p className="text-xs text-neutral-400">
              Mostrando o último dado disponível — atualização em andamento.
            </p>
          ) : null}
        </div>
      </div>

      {data.incidents.active.length > 0 ? (
        <section className="flex flex-col gap-3">
          <h2 className="text-xs uppercase tracking-wide text-neutral-400">Incidente em andamento</h2>
          {data.incidents.active.map((incident) => (
            <IncidentCard key={incident.id} incident={incident} tone="active" />
          ))}
        </section>
      ) : null}

      <section className="flex flex-col gap-3">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h2 className="text-xs uppercase tracking-wide text-neutral-400">Serviços</h2>
          <Seg
            options={RANGE_OPTIONS}
            value={range}
            onChange={(value) => setRange(value as RangeKey)}
            aria-label="Selecionar período"
          />
        </div>
        <Card elevation="elev-sm" className="overflow-hidden p-0">
          {data.services.map((service, index) => (
            <div
              key={service.name}
              className={`flex flex-col gap-2 px-4 py-3 ${index < data.services.length - 1 ? "border-b border-divider" : ""}`}
            >
              <div className="flex items-center justify-between gap-3">
                <div className="flex items-center gap-2">
                  <span
                    className="h-2 w-2 flex-none rounded-full"
                    style={{ background: `var(${overallCopy[service.status].colorVar})` }}
                  />
                  <span className="text-sm text-text">{service.name}</span>
                  {!service.last_updated_at ? (
                    <span className="text-xs text-neutral-500">(sem dados)</span>
                  ) : null}
                </div>
                <div className="flex items-center gap-2">
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
                    {...(service.status === "degraded" && service.status_analysis
                      ? { title: service.status_analysis, tabIndex: 0 }
                      : {})}
                  >
                    {serviceLabel[service.status]}
                  </Tag>
                </div>
              </div>
              <div className="flex flex-col gap-0.5">
                <div className="flex gap-px">
                  {service.history.map((bucket, i) => (
                    <div
                      key={i}
                      title={hourlyTooltip(bucket)}
                      tabIndex={0}
                      className="h-[22px] flex-1 rounded-[1.5px]"
                      style={{ background: `var(${hourlyColorVar[bucket.status]})` }}
                      data-testid={`hourly-bar-${service.name}-${i}`}
                    />
                  ))}
                </div>
                <div className="flex justify-between text-[10px] text-neutral-500">
                  <span>{rangeAgoLabel[range]}</span>
                  <span>agora</span>
                </div>
              </div>
            </div>
          ))}
        </Card>
      </section>

      <section className="flex flex-col gap-3">
        <h2 className="text-xs uppercase tracking-wide text-neutral-400">Histórico (últimos 90 dias)</h2>
        {data.incidents.resolved.length > 0 ? (
          <div className="flex flex-col gap-2">
            {data.incidents.resolved.map((incident) => (
              <IncidentCard key={incident.id} incident={incident} tone="resolved" />
            ))}
            {hasMoreResolved ? (
              <Button
                variant="secondary"
                className="self-center"
                onClick={handleLoadMore}
                disabled={loadingMore}
              >
                Carregar mais
              </Button>
            ) : null}
          </div>
        ) : (
          <p className="text-sm text-neutral-500">Nenhum incidente nos últimos 90 dias.</p>
        )}
      </section>

      <footer className="mt-3 text-center text-[11.5px] text-neutral-500">
        Atualiza automaticamente a cada 2 minutos.
      </footer>
    </div>
  );
}
