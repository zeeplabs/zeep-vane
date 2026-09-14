import { Tag } from "../../components/ui/Tag";
import type { ServiceStatus } from "../../types/api";
import { statusDotColor, statusLabel, statusVariant } from "./statusMeta";

export interface StatusTagProps {
  status: ServiceStatus;
}

/** Status badge with the small colored dot the mock renders inside the
 * pill (`handoff-new-layout/Servicos Monitorados.dc.html`'s
 * `svc.statusDot` span) - shared by ServiceListPage's table rows and
 * ServiceDetailDrawer's header so both match the mock identically. */
export function StatusTag({ status }: StatusTagProps) {
  return (
    <Tag variant={statusVariant[status]} className="gap-1.5">
      <span
        className="h-1.5 w-1.5 flex-none rounded-full"
        style={{ backgroundColor: statusDotColor[status] }}
        aria-hidden="true"
      />
      {statusLabel[status]}
    </Tag>
  );
}
