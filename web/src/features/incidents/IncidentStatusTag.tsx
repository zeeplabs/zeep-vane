import { Tag } from "../../components/ui/Tag";
import type { IncidentStatus } from "../../types/api";
import { incidentStatusDotColor, incidentStatusLabel, incidentStatusVariant } from "./incidentStatusMeta";

export interface IncidentStatusTagProps {
  status: IncidentStatus;
}

/** Same dot+pill composition as services/StatusTag.tsx and
 * domains/DomainStatusTag.tsx - every status badge in the app follows this
 * one shape, not a plain square Tag. */
export function IncidentStatusTag({ status }: IncidentStatusTagProps) {
  return (
    <Tag variant={incidentStatusVariant[status]} className="gap-1.5" style={{ borderRadius: "999px" }}>
      <span
        className="h-1.5 w-1.5 flex-none rounded-full"
        style={{ backgroundColor: incidentStatusDotColor[status] }}
        aria-hidden="true"
      />
      {incidentStatusLabel[status]}
    </Tag>
  );
}
