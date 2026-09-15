import { Tag } from "../../components/ui/Tag";
import { adminStatusDotColor, adminStatusLabel, adminStatusVariant, type AdminStatus } from "./adminMeta";

export interface AdminStatusTagProps {
  status: AdminStatus;
}

// Dot+pill status badge - same pattern as IncidentStatusTag/StatusTag/
// DomainStatusTag elsewhere in new-layout-migration.
export function AdminStatusTag({ status }: AdminStatusTagProps) {
  return (
    <Tag variant={adminStatusVariant[status]} className="gap-1.5 rounded-full">
      <span
        className="h-1.5 w-1.5 flex-none rounded-full"
        style={{ backgroundColor: adminStatusDotColor[status] }}
        aria-hidden="true"
      />
      {adminStatusLabel[status]}
    </Tag>
  );
}
