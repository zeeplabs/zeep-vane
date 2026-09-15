import { useEffect, useState } from "react";
import { Drawer, drawerFooterPrimaryStyle, drawerFooterSecondaryStyle } from "../../components/ui/Drawer";
import { Button } from "../../components/ui/Button";
import { Tag } from "../../components/ui/Tag";
import { ApiError } from "../../lib/apiClient";
import { useDNSTarget } from "../status-pages/hooks";
import type { Domain } from "../../types/api";
import { useDeleteDomain, useRecheckDomain } from "./hooks";
import { domainStatusLabel, domainStatusVariant, sslStatusLabel } from "./domainStatusMeta";

function formatTimestamp(iso: string | null): string {
  return iso ? new Date(iso).toLocaleString("pt-BR") : "—";
}

export interface DomainDetailDrawerProps {
  domain: Domain | null;
  onClose: () => void;
}

/** Drawer de detalhe (somente leitura) da aba Domínios (spec.md DSP-05..08):
 * status/tipo/SSL/verificado, banner de erro quando `last_error` existe,
 * bloco de configuração DNS (CNAME) para domínios custom, e as ações
 * "Verificar novamente"/"Remover domínio". */
export function DomainDetailDrawer({ domain, onClose }: DomainDetailDrawerProps) {
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
      else setRemoveError("Não foi possível remover o domínio.");
    }
  }

  return (
    <Drawer
      open={current !== null}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
      title={current?.hostname ?? ""}
      closeLabel="Fechar"
      footer={
        current ? (
          <>
            <Button
              type="button"
              variant="secondary"
              style={drawerFooterSecondaryStyle}
              onClick={handleRecheck}
              disabled={recheckDomain.isPending}
            >
              Verificar novamente
            </Button>
            <Button
              type="button"
              variant="solid"
              style={drawerFooterPrimaryStyle}
              onClick={handleRemove}
              disabled={deleteDomain.isPending}
            >
              Remover domínio
            </Button>
          </>
        ) : null
      }
    >
      {current ? (
        <div className="flex flex-col gap-4">
          <div className="flex items-center gap-2">
            <Tag variant={domainStatusVariant[current.status]}>{domainStatusLabel[current.status]}</Tag>
            <span className="text-sm text-neutral-400">Domínio próprio · {current.hostname}</span>
          </div>

          {current.last_error ? (
            <p role="alert" className="rounded-md border border-critical/40 bg-critical/10 px-3 py-2 text-xs text-critical">
              {current.last_error}
            </p>
          ) : null}

          <div className="grid grid-cols-2 gap-3 text-sm">
            <div>
              <div className="text-xs text-neutral-400">SSL</div>
              <div className="text-text">{sslStatusLabel[current.ssl_status]}</div>
            </div>
            <div>
              <div className="text-xs text-neutral-400">Verificado</div>
              <div className="text-text">{formatTimestamp(current.verified_at)}</div>
            </div>
          </div>

          {current.domain_type === "custom" ? (
            <div className="flex flex-col gap-1 rounded-md border border-divider p-3">
              <span className="text-sm font-medium text-text">Configuração de DNS</span>
              {dnsTarget ? (
                <p className="text-xs text-neutral-400">
                  Aponte <strong>{current.hostname}</strong> (CNAME) para <strong>{dnsTarget}</strong>.
                </p>
              ) : (
                <p className="text-xs text-neutral-400">
                  O operador ainda não configurou o valor de destino do DNS (PUBLIC_DNS_TARGET).
                </p>
              )}
            </div>
          ) : null}

          {removeError ? (
            <p role="alert" className="text-xs text-critical">
              {removeError}
            </p>
          ) : null}
        </div>
      ) : null}
    </Drawer>
  );
}
