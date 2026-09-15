import { Tag } from "../../components/ui/Tag";
import type { DomainStatus } from "../../types/api";
import { domainStatusDotColor, domainStatusLabel, domainStatusVariant } from "./domainStatusMeta";

export interface DomainStatusTagProps {
  status: DomainStatus;
}

/** Same dot+pill composition as services/StatusTag.tsx, applied to
 * DomainStatus instead of ServiceStatus - the user asked for domain rows
 * to match the already-established status badge from the Serviços
 * Monitorados screen exactly, not a plain square Tag. */
export function DomainStatusTag({ status }: DomainStatusTagProps) {
  return (
    <Tag variant={domainStatusVariant[status]} className="gap-1.5" style={{ borderRadius: "999px" }}>
      <span
        className="h-1.5 w-1.5 flex-none rounded-full"
        style={{ backgroundColor: domainStatusDotColor[status] }}
        aria-hidden="true"
      />
      {domainStatusLabel[status]}
    </Tag>
  );
}
