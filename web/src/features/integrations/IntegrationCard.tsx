import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Tag, type TagVariant } from "../../components/ui/Tag";

export type IntegrationStatusKind = "connected" | "not_connected" | "coming_soon";

const statusVariant: Record<IntegrationStatusKind, TagVariant> = {
  connected: "success",
  not_connected: "neutral-outline",
  coming_soon: "neutral-outline",
};

const statusDotColor: Record<IntegrationStatusKind, string> = {
  connected: "var(--color-success)",
  not_connected: "var(--color-text-muted)",
  coming_soon: "var(--color-text-muted)",
};

export interface IntegrationCardProps {
  icon: ReactNode;
  bannerBg: string;
  status: IntegrationStatusKind;
  title: string;
  description: string;
  meta?: string;
  action?: ReactNode;
}

/** Category-grid card (INTG-01) matching handoff-new-layout/Dashboard
 * Integracoes.dc.html: colored banner with centered icon + status
 * dot+pill (same composition as DomainStatusTag), body with title,
 * description, optional meta line and a single footer action. No
 * elevation - the card is bordered, not shadowed (post shadow-removal
 * convention). */
export function IntegrationCard({ icon, bannerBg, status, title, description, meta, action }: IntegrationCardProps) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col overflow-hidden rounded-2xl border border-divider bg-surface">
      <div className="relative flex h-[112px] flex-none items-center justify-center" style={{ background: bannerBg }}>
        <div className="grid h-[52px] w-[52px] place-items-center rounded-[14px] bg-surface">{icon}</div>
        <Tag
          variant={statusVariant[status]}
          className="absolute top-3 right-3 gap-1.5"
          style={{ borderRadius: "999px" }}
        >
          <span
            className="h-1.5 w-1.5 flex-none rounded-full"
            style={{ backgroundColor: statusDotColor[status] }}
            aria-hidden="true"
          />
          {t(`integrations.statusLabel.${status}`)}
        </Tag>
      </div>
      <div className="flex flex-1 flex-col p-5">
        <div className="mb-1.5 text-[15px] font-semibold text-text">{title}</div>
        <p className="m-0 mb-3.5 flex-1 text-[13px] leading-[1.55] text-text-muted">{description}</p>
        {meta ? <div className="mb-3.5 text-xs text-text-muted">{meta}</div> : null}
        {action}
      </div>
    </div>
  );
}
