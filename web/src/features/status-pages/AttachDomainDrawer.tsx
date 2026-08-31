import { useEffect, useState, type FormEvent } from "react";
import { Drawer } from "../../components/ui/Drawer";
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

/** Painel "Anexar domínio" (SPD-06 through SPD-10) - abre a partir de
 * `StatusPageDetail` para uma status page sem domínio, escolhe um
 * `Domain` existente + subdomínio e mostra o registro DNS que o
 * operador precisa configurar. Mesmo padrão de `Drawer` já usado em
 * "Criar status page"/"Criar incidente". */
export function AttachDomainDrawer({ statusPageId, open, onOpenChange }: AttachDomainDrawerProps) {
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

  // Reseta o formulário toda vez que o painel abre - evita reaproveitar
  // estado (domínio/subdomínio/erro) de uma abertura anterior.
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
      else setError("Não foi possível anexar o domínio.");
    }
  }

  const selectedDomain = domains?.find((d) => d.id === domainId);

  return (
    <Drawer
      open={open}
      onOpenChange={onOpenChange}
      title="Anexar domínio"
      description="Escolha o domínio e o subdomínio que essa status page vai usar. O certificado é emitido automaticamente depois que o DNS propagar."
      footer={
        <>
          <Button type="button" variant="secondary" onClick={() => onOpenChange(false)}>
            Cancelar
          </Button>
          <Button type="submit" form="attach-domain-form" variant="primary" disabled={attachDomain.isPending}>
            Anexar
          </Button>
        </>
      }
    >
      <form id="attach-domain-form" onSubmit={handleSubmit} className="flex flex-col gap-3">
        <div className="flex flex-col gap-1">
          <label htmlFor="attach-domain-picker" className="text-sm font-medium text-text">
            Domínio
          </label>
          <select
            id="attach-domain-picker"
            value={domainId}
            onChange={(e) => setDomainId(e.target.value)}
            className="min-h-9 rounded-md border border-divider bg-surface px-3 text-sm text-text"
            required
          >
            <option value="" disabled>
              Selecione um domínio
            </option>
            {(domains ?? []).map((d) => (
              <option key={d.id} value={d.id}>
                {d.hostname}
              </option>
            ))}
          </select>
        </div>

        <Field
          label="Subdomínio"
          value={subdomain}
          onChange={(e) => setSubdomain(e.target.value)}
          required
        />

        <div className="flex flex-col gap-1 rounded-md border border-divider p-3">
          <span className="text-sm font-medium text-text">Registro DNS</span>
          {dnsTargetLoading ? (
            <p className="text-xs text-neutral-400">Carregando…</p>
          ) : dnsTarget ? (
            <p className="text-xs text-neutral-400">
              Aponte {subdomain || "<subdomínio>"}.{selectedDomain?.hostname ?? "<domínio>"} (CNAME) para{" "}
              <strong>{dnsTarget}</strong>.
            </p>
          ) : (
            <p className="text-xs text-neutral-400">
              O operador ainda não configurou o valor de destino do DNS (PUBLIC_DNS_TARGET). Você ainda pode
              anexar o domínio agora.
            </p>
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
