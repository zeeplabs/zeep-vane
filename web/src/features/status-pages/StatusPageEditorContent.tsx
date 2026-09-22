import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { MdOutlineOpenInNew, MdCheck, MdContentCopy } from "react-icons/md";
import { Tag, type TagVariant } from "../../components/ui/Tag";
import { Button, buttonBaseClasses, buttonVariantClasses } from "../../components/ui/Button";
import { Field } from "../../components/ui/Field";
import { useAuth } from "../../auth/AuthProvider";
import { ApiError } from "../../lib/apiClient";
import { formatDateTime } from "../../lib/formatDate";
import { useDomains } from "../domains/hooks";
import { useServices } from "../services/hooks";
import type { Service, StatusPage, StatusPageState } from "../../types/api";
import { useDNSTarget, useSetStatusPageServices, useVerifyDomain, type VerifyDomainResult } from "./hooks";
import { AttachDomainDrawer } from "./AttachDomainDrawer";

// publicUrl only composes a URL once both domain_id/subdomain are set
// (SPD-01) - null-safe, same guard as StatusPagesSection.tsx's publicUrl.
function publicUrl(domainId: string | null, subdomain: string | null, hostname: string | undefined): string | null {
  if (!domainId || !subdomain) return null;
  return `https://${subdomain}.${hostname ?? "?"}`;
}

// sameServiceSet compares two service-id lists regardless of order - the
// checkbox UI's local selection order has no relation to the server's,
// so a naive index-by-index compare would report a false dirty state.
function sameServiceSet(a: string[], b: string[]): boolean {
  if (a.length !== b.length) return false;
  const sorted = [...b].sort();
  return [...a].sort().every((id, i) => id === sorted[i]);
}

const statePillVariant: Record<StatusPageState, TagVariant> = {
  draft: "accent-outline",
  pending_tls: "accent",
  published: "success",
  tls_failed: "critical",
};

const statePillLabelKey: Record<StatusPageState, string> = {
  draft: "statusPages.section.noDomainTag",
  pending_tls: "statusPages.section.pendingTag",
  published: "statusPages.section.publishedTag",
  tls_failed: "statusPages.section.failedTag",
};

interface StatusPagePillProps {
  page: StatusPage;
}

// Same dot+pill composition DomainStatusTag/services' StatusTag already
// established for the new layout - a status page's badge shouldn't look
// like a different, older component just because it has its own state
// machine (SPD-12/13).
function StatusPagePill({ page }: StatusPagePillProps) {
  const { t } = useTranslation();
  // SPD-12: no domain attached yet - distinct from "draft" with a
  // domain (which shouldn't happen anymore, see AD-017), but the text and
  // variant serve both cases. "published"/"tls_failed" always win, even
  // in the defensive/impossible shape of "published" with no domain_id
  // (never produced by the real flow - MarkPublished requires domain_id
  // via a hostname JOIN - but publicUrl() and this pill still guard
  // against it, same reasoning as StatusPagesSection.test.tsx).
  const state: StatusPageState =
    page.domain_id === null && page.state !== "published" && page.state !== "tls_failed" ? "draft" : page.state;
  return (
    <Tag variant={statePillVariant[state]} className="gap-1.5" style={{ borderRadius: "999px" }}>
      {state === "pending_tls" ? <span className="h-1.5 w-1.5 flex-none animate-pulse rounded-full bg-current" aria-hidden="true" /> : null}
      {t(statePillLabelKey[state])}
    </Tag>
  );
}

export interface StatusPageEditorContentProps {
  page: StatusPage;
  /** Controls the "Preview public page" link. Defaults to `true` -
   * needed by the legacy `/status-pages/{id}` screen (`StatusPageDetail.tsx`,
   * with no separate detail drawer, this is its only way to preview).
   * The new layout's `EditStatusPageDrawer` passes `false`: the link moved
   * to `StatusPageDetailDrawer` ("view details"), at Julio's request -
   * it previously only existed in the edit drawer, which was the wrong
   * place for it. */
  showPreviewLink?: boolean;
}

/** Actual editing body for a status page (status, attach domain,
 * DNS/certificate verification, linked services) - extracted from
 * `StatusPageDetail.tsx` to be reused both by the legacy
 * `/status-pages/{id}` screen (still used by the pre-redesign flow) and by
 * the new layout's `EditStatusPageDrawer`, which replaces navigation to a
 * separate screen with a drawer (same pattern as creation), at Julio's
 * explicit request: "didn't look good in a separate screen". Layout
 * aligned with already-migrated components (`DomainDetailDrawer`,
 * `AddStatusPageDrawer`): no `Card`, sections separated by `border-t`,
 * 10.5px uppercase labels, service checklist with a square checkbox
 * instead of the native round one. */
export function StatusPageEditorContent({ page, showPreviewLink = true }: StatusPageEditorContentProps) {
  const { t } = useTranslation();
  // SPEC_DEVIATION: fixed page 1 for now - Pager UI for the domains
  // dropdown is out of scope here (this reads domains only to resolve a
  // hostname/build a select list); T14/T16 (Pager) is a later phase not
  // yet built. Mirrors the same deviation in DomainsSection.tsx.
  const { data: domainsPage } = useDomains(1);
  const domains = domainsPage?.items;
  // SPEC_DEVIATION: fixed page 1 for now - Pager UI for the services
  // dropdown/lookup is out of scope here; T14/T16 (Pager) is a later
  // phase not yet built. Mirrors the same deviation in ServicesSection.tsx.
  const { data: servicesPage } = useServices(1);
  const services = servicesPage?.items;
  const { hasRole } = useAuth();
  const canManage = hasRole(["owner", "operator"]);
  const setServices = useSetStatusPageServices();
  const [attachOpen, setAttachOpen] = useState(false);
  const [selectedServiceIds, setSelectedServiceIds] = useState<string[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [serviceQuery, setServiceQuery] = useState("");

  // Resyncs local selection whenever the server's linked set changes -
  // on first load and after a save (useSetStatusPageServices invalidates
  // the query, which lands here as a new page.service_ids reference).
  useEffect(() => {
    setSelectedServiceIds(page.service_ids);
  }, [page]);

  const hostname = domains?.find((d) => d.id === page.domain_id)?.hostname;
  const url = publicUrl(page.domain_id, page.subdomain, hostname);

  function toggleService(serviceId: string) {
    setSelectedServiceIds((prev) =>
      prev.includes(serviceId) ? prev.filter((s) => s !== serviceId) : [...prev, serviceId]
    );
  }

  const isDirty = !sameServiceSet(selectedServiceIds, page.service_ids);
  const pageId = page.id;

  const allServices = services ?? [];
  const selectedIdSet = new Set(selectedServiceIds);
  const linkedServices = allServices.filter((s) => selectedIdSet.has(s.id));
  const normalizedQuery = serviceQuery.trim().toLowerCase();
  const availableServices = allServices
    .filter((s) => !selectedIdSet.has(s.id))
    .filter((s) => normalizedQuery === "" || s.name.toLowerCase().includes(normalizedQuery));

  async function handleSaveServices() {
    setError(null);
    try {
      await setServices.mutateAsync({ id: pageId, service_ids: selectedServiceIds });
    } catch (err) {
      if (err instanceof ApiError) setError(err.message);
      else setError(t("statusPages.editor.saveServicesError"));
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-2">
        <StatusPagePill page={page} />
      </div>

      {page.domain_id === null ? (
        <div className="flex items-center justify-between gap-3">
          <p className="text-[13px] text-text-muted">{t("statusPages.editor.noDomainAttached")}</p>
          <Button type="button" variant="secondary" className="w-fit" onClick={() => setAttachOpen(true)}>
            {t("statusPages.section.attachDomainLink")}
          </Button>
        </div>
      ) : null}

      {page.state === "published" && url ? (
        <a href={url} target="_blank" rel="noreferrer" className="font-mono text-[13px] font-semibold text-accent hover:underline">
          {url}
        </a>
      ) : null}

      {page.state === "tls_failed" ? <p className="text-[13px] text-text-muted">{page.tls_last_error}</p> : null}

      {showPreviewLink ? (
        <a
          href={`/status/${page.id}`}
          target="_blank"
          rel="noreferrer"
          className={`${buttonBaseClasses} ${buttonVariantClasses.secondary} w-fit`}
        >
          <MdOutlineOpenInNew size={14} aria-hidden="true" />
          {t("statusPages.detail.previewButton")}
        </a>
      ) : null}

      {/* Fixed DNS/certificate configuration panel (mirrors the custom
          domain flow of platforms like Vercel/Render) - stays visible
          while the page has a domain attached and isn't published yet,
          even though the automatic polling (useStatusPage) is already
          trying to detect the transition on its own every 10s. Restricted
          to canManage: both the endpoint it reads (GET
          /api/instance/dns-target) and the one it triggers (POST
          .../verify-domain) are write-role-gated on the backend - a
          viewer would just see a confusing/misleading 403 ("DNS not
          configured" when really they just lacked read permission)
          instead of nothing. */}
      {page.domain_id !== null && page.subdomain !== null && page.state !== "published" && canManage ? (
        <div className="flex flex-col gap-3 border-t border-divider pt-4">
          <DomainVerificationPanel statusPageId={page.id} fullHostname={`${page.subdomain}.${hostname ?? "?"}`} />
        </div>
      ) : null}

      <div className="flex flex-col gap-3 border-t border-divider pt-4">
        <div className="flex items-center justify-between">
          <span className="text-[10.5px] font-bold uppercase tracking-wide text-text-muted">
            {t("statusPages.editor.linkedServicesLabel", { selected: selectedServiceIds.length, total: allServices.length })}
          </span>
        </div>

        {allServices.length === 0 ? (
          <p className="text-[13px] text-text-muted">{t("statusPages.editor.noServices")}</p>
        ) : (
          <>
            {linkedServices.length > 0 ? (
              <ServiceGroup
                label={t("statusPages.editor.linkedGroupLabel", { count: linkedServices.length })}
                services={linkedServices}
                selectedServiceIds={selectedServiceIds}
                canManage={canManage}
                onToggle={toggleService}
              />
            ) : null}

            <div className="flex flex-col gap-2">
              <Field
                type="text"
                variant="filled"
                label={t("statusPages.editor.availableLabel")}
                placeholder={t("statusPages.editor.searchPlaceholder")}
                value={serviceQuery}
                onChange={(e) => setServiceQuery(e.target.value)}
              />
              <ServiceGroup
                services={availableServices}
                selectedServiceIds={selectedServiceIds}
                canManage={canManage}
                onToggle={toggleService}
                emptyLabel={t("statusPages.editor.noServicesFound")}
                scrollable
              />
            </div>
          </>
        )}

        {canManage ? (
          <div className="flex items-center gap-3">
            <Button
              type="button"
              variant="solid"
              disabled={!isDirty || setServices.isPending}
              onClick={handleSaveServices}
            >
              {t("statusPages.editor.saveServicesButton")}
            </Button>
            {error ? (
              <p role="alert" className="text-xs text-critical">
                {error}
              </p>
            ) : null}
          </div>
        ) : null}
      </div>

      <AttachDomainDrawer statusPageId={page.id} open={attachOpen} onOpenChange={setAttachOpen} />
    </div>
  );
}

interface DomainVerificationPanelProps {
  statusPageId: string;
  fullHostname: string;
}

// DomainVerificationPanel is the persistent "o que configurar no DNS" +
// "verificar novamente" panel shown while a status page has a domain
// attached but isn't published yet - it stays visible the whole time (not
// just right after attaching, unlike AttachDomainDrawer's one-time
// instructions), since DNS propagation can take anywhere from seconds to
// hours and the admin needs somewhere to come back to. "Verificar"
// performs a real DNS lookup + TLS handshake server-side (POST
// .../verify-domain) rather than only waiting for the existing 10s
// background poll, mirroring the "recheck" action platforms like
// Vercel/Render offer for custom domains. Table styling mirrors
// DomainDetailDrawer's TIPO/VALOR DNS block.
function DomainVerificationPanel({ statusPageId, fullHostname }: DomainVerificationPanelProps) {
  const { t, i18n } = useTranslation();
  const { data: dnsTarget, isLoading: dnsTargetLoading } = useDNSTarget();
  const verifyDomain = useVerifyDomain();
  const result: VerifyDomainResult | undefined = verifyDomain.data;

  async function handleCopyCname() {
    if (!dnsTarget) return;
    await navigator.clipboard.writeText(dnsTarget);
    toast.success(t("statusPages.dns.copied"));
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between gap-3">
        <span className="text-[10.5px] font-bold uppercase tracking-wide text-text-muted">{t("domains.detail.dnsConfigLabel")}</span>
        <Button
          type="button"
          variant="secondary"
          disabled={verifyDomain.isPending}
          onClick={() => verifyDomain.mutate(statusPageId)}
        >
          {verifyDomain.isPending ? t("statusPages.editor.verifyingButton") : t("statusPages.editor.verifyButton")}
        </Button>
      </div>

      <div className="overflow-hidden rounded-md border border-divider">
        <div className="grid grid-cols-[70px_1fr] gap-2 border-b border-divider bg-card-header-bg px-3.5 py-2.5">
          <span className="text-[11px] font-bold text-text-muted">{t("domains.detail.dnsType")}</span>
          <span className="text-[11px] font-bold text-text-muted">{t("domains.detail.dnsValue")}</span>
        </div>
        <div className="grid grid-cols-[70px_1fr_auto] items-center gap-2 px-3.5 py-3">
          <span className="font-mono text-[12.5px] font-bold text-text">CNAME</span>
          {dnsTargetLoading ? (
            <span className="font-mono text-[12.5px] text-text-muted">{t("statusPages.loading")}</span>
          ) : (
            <span className="min-w-0 truncate font-mono text-[12.5px] text-text-muted">
              {dnsTarget ?? t("domains.detail.notConfigured")}
            </span>
          )}
          {dnsTarget ? (
            <button
              type="button"
              onClick={handleCopyCname}
              aria-label={t("statusPages.dns.copyCname")}
              title={t("statusPages.dns.copyCname")}
              className="flex-none cursor-pointer text-text-muted hover:text-text"
            >
              <MdContentCopy size={15} aria-hidden="true" />
            </button>
          ) : null}
        </div>
      </div>
      <p className="text-xs text-text-muted">
        {t("statusPages.editor.pointHostnameInstruction", { hostname: fullHostname })}
      </p>

      {verifyDomain.isError ? (
        <p role="alert" className="text-xs text-critical">
          {verifyDomain.error instanceof ApiError
            ? verifyDomain.error.message
            : t("statusPages.editor.verifyGenericError")}
        </p>
      ) : null}

      {result ? (
        <div className="flex flex-col gap-1.5 border-t border-divider pt-3 text-xs">
          <VerificationRow
            ok={result.dns_resolved && result.dns_matches_target !== false}
            label={
              !result.dns_resolved
                ? t("statusPages.editor.dnsNotResolved")
                : result.dns_matches_target === false
                  ? t("statusPages.editor.dnsMismatch", { ips: result.resolved_ips.join(", ") })
                  : result.dns_matches_target === true
                    ? t("statusPages.editor.dnsMatch", { ips: result.resolved_ips.join(", ") })
                    : t("statusPages.editor.dnsResolves", { ips: result.resolved_ips.join(", ") })
            }
          />
          <VerificationRow
            ok={result.tls_cert_valid}
            label={
              result.tls_cert_valid
                ? t("statusPages.editor.tlsValid")
                : result.tls_reachable
                  ? t("statusPages.editor.tlsRespondedInvalid", {
                      hostname: fullHostname,
                      errorSuffix: result.tls_error ? `: ${result.tls_error}` : "",
                    })
                  : t("statusPages.editor.tlsUnreachable", {
                      errorSuffix: result.tls_error ? `: ${result.tls_error}` : "",
                    })
            }
          />
          <p className="text-text-muted">
            {t("statusPages.editor.lastCheckedLabel", { date: formatDateTime(result.checked_at, i18n.language) })}
          </p>
        </div>
      ) : null}
    </div>
  );
}

interface VerificationRowProps {
  ok: boolean;
  label: string;
}

function VerificationRow({ ok, label }: VerificationRowProps) {
  return (
    <div className="flex items-center gap-2">
      <span className={ok ? "text-success" : "text-critical"} aria-hidden="true">
        {ok ? "✓" : "✗"}
      </span>
      <span className="text-text-muted">{label}</span>
    </div>
  );
}

interface ServiceGroupProps {
  label?: string;
  services: Service[];
  selectedServiceIds: string[];
  canManage: boolean;
  onToggle: (serviceId: string) => void;
  emptyLabel?: string;
  scrollable?: boolean;
}

// ServiceGroup renders one labeled block of service checklist rows
// ("Linked" / "Available") - same checkbox-row composition
// AddStatusPageDrawer's service checklist already established (16px
// rounded-square checkbox + MdCheck), not the old native round
// <input type="checkbox">.
function ServiceGroup({ label, services, selectedServiceIds, canManage, onToggle, emptyLabel, scrollable }: ServiceGroupProps) {
  return (
    <div className="flex flex-col gap-1">
      {label ? <span className="text-[10.5px] font-bold uppercase tracking-wide text-text-muted">{label}</span> : null}
      <div className={`flex flex-col rounded-md border border-divider ${scrollable ? "max-h-64 overflow-y-auto" : ""}`}>
        {services.length === 0 ? (
          <p className="px-3 py-2 text-[13px] text-text-muted">{emptyLabel}</p>
        ) : (
          services.map((s) => (
            <ServiceRow
              key={s.id}
              name={s.name}
              checked={selectedServiceIds.includes(s.id)}
              disabled={!canManage}
              onToggle={() => onToggle(s.id)}
            />
          ))
        )}
      </div>
    </div>
  );
}

interface ServiceRowProps {
  name: string;
  checked: boolean;
  disabled: boolean;
  onToggle: () => void;
}

function ServiceRow({ name, checked, disabled, onToggle }: ServiceRowProps) {
  return (
    <button
      type="button"
      onClick={disabled ? undefined : onToggle}
      aria-pressed={checked}
      disabled={disabled}
      className="flex cursor-pointer items-center gap-2.5 rounded-md px-2.5 py-[9px] text-left hover:bg-card-header-bg disabled:cursor-not-allowed disabled:hover:bg-transparent"
    >
      <span
        className={
          "flex h-[16px] w-[16px] flex-none items-center justify-center rounded-[5px] border-[1.5px] " +
          (checked ? "border-accent bg-accent" : "border-divider bg-surface")
        }
      >
        {checked ? <MdCheck size={11} className="text-white" aria-hidden="true" /> : null}
      </span>
      <span className={`text-[13px] font-semibold ${disabled ? "text-text-muted" : "text-text"}`}>{name}</span>
    </button>
  );
}
