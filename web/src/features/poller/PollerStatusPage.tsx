import { useState } from "react";
import { useTranslation } from "react-i18next";
import { MdOutlineWarningAmber } from "react-icons/md";
import { Card } from "../../components/ui/Card";
import { Pager } from "../../components/ui/Pager";
import { Tag } from "../../components/ui/Tag";
import { Skeleton } from "../../components/ui/Skeleton";
import { formatDateTime } from "../../lib/formatDate";
import { failureMessage, providerLabel, replicaLabel } from "./format";
import { usePollerStatus } from "./hooks";

function StatCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border border-divider p-4">
      <div className="mb-2 text-[10.5px] font-bold tracking-wide text-text-muted uppercase">{label}</div>
      <div className="text-[22px] font-bold text-text">{value}</div>
    </div>
  );
}

function AlertBanner({ children }: { children: React.ReactNode }) {
  return (
    <div
      className="mb-4 flex items-start gap-2.5 rounded-md border px-3.5 py-3 text-[12.5px] leading-normal text-text-muted"
      style={{
        backgroundColor: "color-mix(in oklch, var(--color-warning) 10%, transparent)",
        borderColor: "color-mix(in oklch, var(--color-warning) 40%, transparent)",
      }}
    >
      <MdOutlineWarningAmber size={16} className="mt-0.5 flex-none text-warning" aria-hidden="true" />
      <span>{children}</span>
    </div>
  );
}

export function PollerStatusPage() {
  const { t, i18n } = useTranslation();
  const [page, setPage] = useState(1);
  const { data, isLoading, isError } = usePollerStatus(page);
  const totalPages = Math.max(1, Math.ceil((data?.total ?? 0) / (data?.page_size ?? 20)));
  const items = data?.items ?? [];
  const failing = items.filter((e) => e.status !== "active");

  const pollerStatusLabel = !data
    ? t("poller.statusLabel.unknown")
    : !data.leader_elected
      ? t("poller.statusLabel.noLeader")
      : data.poller_running
        ? t("poller.statusLabel.active")
        : t("poller.statusLabel.waitingIntegration");
  const replicaName =
    data?.leader_elected && data.replica ? replicaLabel(t, data.replica.application_name) : null;

  return (
    <div className="mx-auto flex w-full max-w-[1280px] flex-col gap-6">
      <div>
        <h2 className="text-text">{t("poller.title")}</h2>
        <p className="m-0 text-[13.5px] text-text-muted">{t("poller.subtitle")}</p>
      </div>
      {isLoading ? (
        <div aria-busy="true" className="flex flex-col gap-3.5">
          <span className="sr-only">{t("poller.loading")}</span>
          <div className="grid grid-cols-1 gap-3.5 sm:grid-cols-3">
            {Array.from({ length: 3 }).map((_, i) => (
              <div key={i} className="rounded-md border border-divider p-4">
                <Skeleton width={80} height={10} className="mb-2" />
                <Skeleton width={100} height={22} />
              </div>
            ))}
          </div>
          <Card elevation="none" className="divide-y divide-divider overflow-hidden border border-divider">
            {Array.from({ length: 5 }).map((_, i) => (
              <div key={i} className="flex items-center gap-3 px-4 py-3.5">
                <Skeleton width={8} height={8} radius={999} />
                <div className="flex-1">
                  <Skeleton width={140} height={14} />
                </div>
                <Skeleton width={70} height={20} radius={999} />
              </div>
            ))}
          </Card>
        </div>
      ) : isError ? (
        <p className="text-text-muted">{t("poller.loadError")}</p>
      ) : (
        <>
          <div className="grid grid-cols-1 gap-3.5 sm:grid-cols-3">
            <StatCard label={t("poller.stat.poller")} value={pollerStatusLabel + (replicaName ? ` · ${replicaName}` : "")} />
            <StatCard label={t("poller.stat.checksPerMinute")} value={String(data?.checks_last_minute ?? 0)} />
            <StatCard label={t("poller.stat.connectedIntegrations")} value={String(data?.total ?? 0)} />
          </div>

          {data && data.leader_elected && !data.poller_running ? (
            <AlertBanner>{t("poller.leaderNoIntegration")}</AlertBanner>
          ) : null}
          {failing.length > 0 ? (
            <AlertBanner>{failureMessage(t, failing.map((e) => e.provider))}</AlertBanner>
          ) : null}

          <Card elevation="none" className="divide-y divide-divider overflow-hidden border border-divider">
            {items.length === 0 ? (
              <p className="px-4 py-6 text-center text-text-muted">{t("poller.empty")}</p>
            ) : (
              items.map((e) => (
                <div key={e.provider} data-testid="poller-row" className="flex items-center gap-3 px-4 py-3.5">
                  <span
                    className="h-2 w-2 flex-none rounded-full"
                    style={{
                      backgroundColor: e.status === "active" ? "var(--color-success)" : "var(--color-critical)",
                    }}
                    aria-hidden="true"
                  />
                  <div className="flex-1">
                    <div className="text-[15px] font-medium text-text">{providerLabel(e.provider)}</div>
                    {e.status !== "active" ? <div className="mt-0.5 text-xs text-text-muted">{e.last_error}</div> : null}
                  </div>
                  <div className="text-right text-xs text-text-muted">
                    <div>{t("poller.lastRun")}</div>
                    <div className="mt-0.5 text-[13px] text-text">
                      {e.last_checked_at ? formatDateTime(e.last_checked_at, i18n.language) : "-"}
                    </div>
                  </div>
                  {e.status === "active" ? (
                    <Tag variant="success">{t("poller.result.success")}</Tag>
                  ) : (
                    <Tag variant="critical">{t("poller.result.failure")}</Tag>
                  )}
                </div>
              ))
            )}
          </Card>
          <Pager page={page} totalPages={totalPages} onChange={setPage} />
        </>
      )}
    </div>
  );
}
