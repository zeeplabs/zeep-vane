import { useEffect, useState } from "react";
import * as RadixDialog from "@radix-ui/react-dialog";
import { useTranslation } from "react-i18next";
import { MdClose } from "react-icons/md";
import { Button } from "../../components/ui/Button";
import { ApiError } from "../../lib/apiClient";
import { formatDateTime } from "../../lib/formatDate";
import { useDNSTarget } from "../status-pages/hooks";
import type { Domain } from "../../types/api";
import { useDeleteDomain, useRecheckDomain } from "./hooks";
import { attachedPageColumn, domainTypeLabel, sslStatusColor, sslStatusLabel } from "./domainStatusMeta";
import { DomainStatusTag } from "./DomainStatusTag";

export interface DomainDetailDrawerProps {
  domain: Domain | null;
  onClose: () => void;
}

/** Drawer de detalhe (somente leitura) da aba Domínios (spec.md DSP-05..08).
 *
 * Não usa o <Drawer> compartilhado: o mock
 * (`handoff-new-layout/Dominios e Status Pages.dc.html`'s domain-detail
 * panel) não tem título/borda de cabeçalho nem rodapé com borda - é um
 * painel contínuo com badge+X no topo e o hostname como h2 visível logo
 * abaixo, mesma estrutura que `ServiceDetailDrawer` já usa pelo mesmo
 * motivo. RadixDialog usado diretamente pra reproduzir isso; `Title` fica
 * sr-only porque o hostname já é o h2 visível. */
export function DomainDetailDrawer({ domain, onClose }: DomainDetailDrawerProps) {
  const { t, i18n } = useTranslation();
  const [current, setCurrent] = useState<Domain | null>(domain);
  const [removeError, setRemoveError] = useState<string | null>(null);
  const { data: dnsTarget } = useDNSTarget();
  const recheckDomain = useRecheckDomain();
  const deleteDomain = useDeleteDomain();

  useEffect(() => {
    setCurrent(domain);
    setRemoveError(null);
  }, [domain]);

  async function handleRecheck() {
    if (!current) return;
    const updated = await recheckDomain.mutateAsync(current.id);
    setCurrent(updated);
  }

  async function handleRemove() {
    if (!current) return;
    setRemoveError(null);
    try {
      await deleteDomain.mutateAsync(current.id);
      onClose();
    } catch (err) {
      if (err instanceof ApiError) setRemoveError(err.message);
      else setRemoveError(t("domains.detail.removeError"));
    }
  }

  return (
    <RadixDialog.Root
      open={current !== null}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <RadixDialog.Portal>
        <RadixDialog.Overlay className="fixed inset-0 z-40 bg-black/60 data-[state=closed]:animate-[drawer-overlay-out_180ms_ease-in] data-[state=open]:animate-[drawer-overlay-in_200ms_ease-out]" />
        <RadixDialog.Content className="fixed right-0 top-0 z-50 flex h-full w-full max-w-md flex-col overflow-y-auto bg-surface p-6 shadow-lg outline-none data-[state=closed]:animate-[drawer-panel-out_220ms_ease-in] data-[state=open]:animate-[drawer-panel-in_260ms_cubic-bezier(0.16,1,0.3,1)]">
          {current ? (
            <div className="flex flex-col gap-4">
              <div className="flex items-start justify-between">
                <DomainStatusTag status={current.status} />
                <RadixDialog.Close asChild>
                  <button
                    type="button"
                    aria-label={t("common.close")}
                    className="cursor-pointer text-text-muted hover:text-text"
                  >
                    <MdClose size={18} aria-hidden="true" />
                  </button>
                </RadixDialog.Close>
              </div>

              <div className="-mt-1">
                <RadixDialog.Title asChild>
                  <h2 className="font-mono text-[19px] font-bold text-text">{current.hostname}</h2>
                </RadixDialog.Title>
                <p className="mt-0.5 text-[13px] text-text-muted">
                  {domainTypeLabel(t, current.domain_type)} · {t("domains.detail.pointsToConnector")}{" "}
                  {attachedPageColumn(current)}
                </p>
              </div>

              {current.last_error ? (
                <p role="alert" className="rounded-md border border-critical/40 bg-critical/10 px-3 py-2 text-xs text-critical">
                  {current.last_error}
                </p>
              ) : null}

              <div className="grid grid-cols-2 gap-3">
                <div>
                  <div className="mb-1 text-[10.5px] font-bold uppercase tracking-wide text-text-muted">{t("domains.detail.sslLabel")}</div>
                  <div className="text-[15px] font-bold" style={{ color: sslStatusColor[current.ssl_status] }}>
                    {sslStatusLabel(t, current.ssl_status)}
                  </div>
                </div>
                <div>
                  <div className="mb-1 text-[10.5px] font-bold uppercase tracking-wide text-text-muted">{t("domains.detail.verifiedAtLabel")}</div>
                  <div className="text-[15px] font-bold text-text">
                    {current.verified_at ? formatDateTime(current.verified_at, i18n.language) : "—"}
                  </div>
                </div>
              </div>

              {current.domain_type === "custom" ? (
                <div>
                  <div className="mb-1 text-[10.5px] font-bold uppercase tracking-wide text-text-muted">
                    {t("domains.detail.dnsConfigLabel")}
                  </div>
                  <div className="overflow-hidden rounded-md border border-divider">
                    <div className="grid grid-cols-[70px_1fr] gap-2 border-b border-divider bg-card-header-bg px-3.5 py-2.5">
                      <span className="text-[11px] font-bold text-text-muted">{t("domains.detail.dnsType")}</span>
                      <span className="text-[11px] font-bold text-text-muted">{t("domains.detail.dnsValue")}</span>
                    </div>
                    <div className="grid grid-cols-[70px_1fr] items-center gap-2 px-3.5 py-3">
                      <span className="font-mono text-[12.5px] font-bold text-text">CNAME</span>
                      <span className="min-w-0 truncate font-mono text-[12.5px] text-text-muted">
                        {dnsTarget ?? t("domains.detail.notConfigured")}
                      </span>
                    </div>
                  </div>
                </div>
              ) : null}

              {removeError ? (
                <p role="alert" className="text-xs text-critical">
                  {removeError}
                </p>
              ) : null}

              <div className="flex gap-2.5">
                <Button
                  type="button"
                  variant="secondary"
                  className="flex-1"
                  onClick={handleRecheck}
                  disabled={recheckDomain.isPending}
                >
                  {t("domains.detail.recheckButton")}
                </Button>
                <Button
                  type="button"
                  variant="secondary"
                  className="flex-1"
                  style={{ borderColor: "var(--color-critical)", color: "var(--color-critical)" }}
                  onClick={handleRemove}
                  disabled={deleteDomain.isPending}
                >
                  {t("domains.detail.removeButton")}
                </Button>
              </div>
            </div>
          ) : null}
        </RadixDialog.Content>
      </RadixDialog.Portal>
    </RadixDialog.Root>
  );
}
