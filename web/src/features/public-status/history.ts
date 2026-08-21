// Barra de "uptime" por serviço: decoração adicional do handoff (SP público),
// sem endpoint real de histórico diário ainda — dados seedados de forma
// determinística por nome de serviço, nunca aleatórios (status-page-handoff/README.md).
import type { PublicServiceStatus } from "../../lib/publicStatus";

const HISTORY_DAYS = 45;

const HISTORY_SEED: Record<string, [number, PublicServiceStatus][]> = {
  Checkout: [[10, "outage"], [11, "degraded"]],
  "API pública": [[36, "degraded"]],
};

export interface HistoryDay {
  daysAgo: number;
  status: PublicServiceStatus;
}

export function buildServiceHistory(name: string, todayStatus: PublicServiceStatus): HistoryDay[] {
  const overrides = new Map(HISTORY_SEED[name] ?? []);
  const days: HistoryDay[] = [];
  for (let i = 0; i < HISTORY_DAYS; i += 1) {
    const daysAgo = HISTORY_DAYS - 1 - i;
    const status = daysAgo === 0 ? todayStatus : overrides.get(daysAgo) ?? "operational";
    days.push({ daysAgo, status });
  }
  return days;
}
