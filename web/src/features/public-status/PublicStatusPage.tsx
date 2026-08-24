import { useState } from "react";
import { useParams } from "react-router-dom";
import { Card } from "../../components/ui/Card";
import { Tag } from "../../components/ui/Tag";
import type { TagVariant } from "../../components/ui/Tag";
import type { PublicHourlyBucket, PublicHourlyStatus, PublicIncidentEntry, PublicServiceStatus } from "../../lib/publicStatus";
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
function hourlyTooltip(bucket: PublicHourlyBucket): string {
  const start = new Date(bucket.start);
  const parts = HOURLY_TOOLTIP_FORMATTER.formatToParts(start);
  const get = (type: string) => parts.find((p) => p.type === type)?.value ?? "";
  const day = get("day");
  const month = get("month");
  const startHour = Number(get("hour")) % 24;
  const endHour = (startHour + 1) % 24;
  return `${day}/${month}, ${startHour}h–${endHour}h · ${hourlyLabel[bucket.status]}`;
}

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

export function PublicStatusPage() {
  const { id = "" } = useParams();
  const { data, isLoading, isError } = usePublicStatusPage(id);

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
        <h2 className="text-xs uppercase tracking-wide text-neutral-400">Serviços</h2>
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
                <Tag variant={serviceTagVariant[service.status]}>{serviceLabel[service.status]}</Tag>
              </div>
              <div className="flex flex-col gap-0.5">
                <div className="flex gap-px">
                  {service.hourly_history.map((bucket, i) => (
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
                  <span>24h atrás</span>
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
