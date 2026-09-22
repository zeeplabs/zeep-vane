import { useEffect, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { Drawer, drawerFooterPrimaryStyle, drawerFooterSecondaryStyle } from "../../components/ui/Drawer";
import { Button } from "../../components/ui/Button";
import { Field } from "../../components/ui/Field";
import { ApiError } from "../../lib/apiClient";
import { useDomains } from "../domains/hooks";
import { useAttachDomain, useDNSTarget } from "./hooks";

export interface AttachDomainDrawerProps {
  statusPageId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

/** "Attach domain" panel (SPD-06 through SPD-10) - opened from
 * `StatusPageDetail` for a status page with no domain attached, picks an
 * existing `Domain` + subdomain and shows the DNS record the operator
 * needs to configure. Same `Drawer` pattern already used by
 * "Create status page"/"Create incident". */
export function AttachDomainDrawer({ statusPageId, open, onOpenChange }: AttachDomainDrawerProps) {
  const { t } = useTranslation();
  // SPEC_DEVIATION: fixed page 1 for now - Pager UI for the domains
  // dropdown is out of scope here (this reads domains only to resolve a
  // hostname/build a select list); T14/T16 (Pager) is a later phase not
  // yet built. Mirrors the same deviation in DomainsSection.tsx.
  const { data: domainsPage } = useDomains(1);
  const domains = domainsPage?.items;
  const { data: dnsTarget, isLoading: dnsTargetLoading } = useDNSTarget();
  const attachDomain = useAttachDomain();

  const [domainId, setDomainId] = useState("");
  const [subdomain, setSubdomain] = useState("");
  const [error, setError] = useState<string | null>(null);

  // Resets the form every time the panel opens - avoids reusing
  // state (domain/subdomain/error) from a previous opening.
  useEffect(() => {
    if (open) {
      setDomainId(domains?.[0]?.id ?? "");
      setSubdomain("");
      setError(null);
    }
  }, [open, domains]);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    try {
      await attachDomain.mutateAsync({ id: statusPageId, domain_id: domainId, subdomain });
      onOpenChange(false);
    } catch (err) {
      // Erros do servidor (404/409/422 - SPD-07, SPD-08, SPD-09) ficam
      // inline; o painel permanece aberto pro admin corrigir e tentar
      // de novo, em vez de fechar e perder o que foi digitado.
      if (err instanceof ApiError) setError(err.message);
      else setError(t("statusPages.attach.error"));
    }
  }

  const selectedDomain = domains?.find((d) => d.id === domainId);

  return (
    <Drawer
      open={open}
      onOpenChange={onOpenChange}
      title={t("statusPages.attach.title")}
      description={t("statusPages.attach.description")}
      closeLabel={t("common.close")}
      footer={
        <>
          <Button
            type="button"
            variant="secondary"
            style={drawerFooterSecondaryStyle}
            onClick={() => onOpenChange(false)}
          >
            {t("common.cancel")}
          </Button>
          <Button
            type="submit"
            form="attach-domain-form"
            variant="solid"
            style={drawerFooterPrimaryStyle}
            disabled={attachDomain.isPending}
          >
            {t("statusPages.attach.submitButton")}
          </Button>
        </>
      }
    >
      <form id="attach-domain-form" onSubmit={handleSubmit} className="flex flex-col gap-3">
        <div className="flex flex-col gap-1">
          <label htmlFor="attach-domain-picker" className="text-sm font-medium text-text">
            {t("statusPages.attach.domainLabel")}
          </label>
          <select
            id="attach-domain-picker"
            value={domainId}
            onChange={(e) => setDomainId(e.target.value)}
            className="min-h-9 rounded-md border border-divider bg-surface px-3 text-sm text-text"
            required
          >
            <option value="" disabled>
              {t("statusPages.attach.domainPlaceholder")}
            </option>
            {(domains ?? []).map((d) => (
              <option key={d.id} value={d.id}>
                {d.hostname}
              </option>
            ))}
          </select>
        </div>

        <Field
          label={t("statusPages.attach.subdomainLabel")}
          value={subdomain}
          onChange={(e) => setSubdomain(e.target.value)}
          required
        />

        <div className="flex flex-col gap-1 rounded-md border border-divider p-3">
          <span className="text-sm font-medium text-text">{t("statusPages.attach.dnsRecordLabel")}</span>
          {dnsTargetLoading ? (
            <p className="text-xs text-neutral-400">{t("statusPages.loading")}</p>
          ) : dnsTarget ? (
            <p className="text-xs text-neutral-400">
              {t("statusPages.attach.dnsInstruction", {
                sub: subdomain || t("statusPages.attach.subdomainPlaceholder"),
                domain: selectedDomain?.hostname ?? t("statusPages.attach.domainPlaceholderShort"),
              })}{" "}
              <strong>{dnsTarget}</strong>.
            </p>
          ) : (
            <p className="text-xs text-neutral-400">{t("statusPages.attach.dnsNotConfigured")}</p>
          )}
        </div>

        {error ? (
          <p role="alert" className="text-xs text-critical">
            {error}
          </p>
        ) : null}
      </form>
    </Drawer>
  );
}
