import { useEffect, useState } from "react";
import { useParams } from "react-router-dom";
import { Tag } from "../../components/ui/Tag";
import { Button, buttonBaseClasses, buttonVariantClasses } from "../../components/ui/Button";
import { Card } from "../../components/ui/Card";
import { Input } from "../../components/ui/Input";
import { useAuth } from "../../auth/AuthProvider";
import { ApiError } from "../../lib/apiClient";
import { useDomains } from "../domains/hooks";
import { useServices } from "../services/hooks";
import type { Service } from "../../types/api";
import { useSetStatusPageServices, useStatusPage } from "./hooks";
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

export function StatusPageDetail() {
  const { id = "" } = useParams();
  const { data: page, isLoading } = useStatusPage(id);
  const { data: domains } = useDomains();
  const { data: services } = useServices();
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
    if (page) setSelectedServiceIds(page.service_ids);
  }, [page]);

  if (isLoading) return <p className="text-neutral-400">Carregando…</p>;
  if (!page) return <p className="text-neutral-400">Status page não encontrada.</p>;

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
      else setError("Não foi possível salvar os serviços vinculados.");
    }
  }

  return (
    <div className="mx-auto flex w-full max-w-[1280px] flex-col gap-4">
      <Card elevation="elev-sm" className="flex flex-col gap-3 p-4">
        <div className="flex items-center justify-between gap-3">
          <h3 className="text-text">{page.name}</h3>

          {/* SPD-12: sem domínio nenhum anexado ainda. */}
          {page.domain_id === null ? (
            <Tag variant="accent-outline" className="w-fit">
              Sem domínio configurado
            </Tag>
          ) : null}

          {/* SPD-13: domínio anexado, mas o certificado ainda não foi emitido -
              substitui o antigo texto ambíguo "Emitindo certificado", que não
              distinguia esse caso do de "sem domínio" acima. */}
          {page.domain_id !== null && page.state === "draft" ? (
            <Tag variant="accent" className="w-fit animate-pulse">
              Aguardando validação de DNS/certificado
            </Tag>
          ) : null}

          {page.state === "published" ? (
            <Tag variant="success" className="w-fit">
              Publicada
            </Tag>
          ) : null}

          {page.state === "tls_failed" ? (
            <Tag variant="critical" className="w-fit">
              Falha
            </Tag>
          ) : null}
        </div>

        {page.domain_id === null ? (
          <div className="flex items-center justify-between gap-3 border-t border-divider pt-3">
            <p className="text-sm text-neutral-400">Nenhum domínio anexado ainda.</p>
            <Button variant="secondary" className="w-fit" onClick={() => setAttachOpen(true)}>
              Anexar domínio
            </Button>
          </div>
        ) : null}

        {page.state === "published" && url ? (
          <div className="flex items-center gap-2 border-t border-divider pt-3">
            <a href={url} target="_blank" rel="noreferrer" className="text-sm text-accent hover:underline">
              {url}
            </a>
          </div>
        ) : null}

        {page.state === "tls_failed" ? (
          <p className="border-t border-divider pt-3 text-sm text-neutral-400">{page.tls_last_error}</p>
        ) : null}

        <div className="border-t border-divider pt-3">
          {/* SPD-01/SPD-14: sempre visível, independente de state/domain - o
              preview (`public-preview`) já compõe pra qualquer estado (AD-008),
              então não há motivo pra escondê-lo aqui. */}
          <a
            href={`/status/${page.id}`}
            target="_blank"
            rel="noreferrer"
            className={`${buttonBaseClasses} ${buttonVariantClasses.secondary} w-fit`}
          >
            <ExternalLinkIcon />
            Pré-visualizar página pública
          </a>
        </div>
      </Card>

      <Card elevation="elev-sm" className="flex flex-col gap-3 p-4">
        <div className="flex items-center justify-between">
          <span className="text-sm font-medium text-text">Serviços vinculados</span>
          <span className="text-xs text-neutral-400">
            {selectedServiceIds.length} de {allServices.length} selecionados
          </span>
        </div>

        {allServices.length === 0 ? (
          <p className="text-sm text-neutral-400">Nenhum serviço cadastrado.</p>
        ) : (
          <>
            {linkedServices.length > 0 ? (
              <ServiceGroup
                label={`Vinculados (${linkedServices.length})`}
                services={linkedServices}
                selectedServiceIds={selectedServiceIds}
                canManage={canManage}
                onToggle={toggleService}
              />
            ) : null}

            <div className="flex flex-col gap-2">
              <Input
                type="text"
                placeholder="Buscar serviço…"
                value={serviceQuery}
                onChange={(e) => setServiceQuery(e.target.value)}
                aria-label="Buscar serviço"
              />
              <ServiceGroup
                label="Disponíveis"
                services={availableServices}
                selectedServiceIds={selectedServiceIds}
                canManage={canManage}
                onToggle={toggleService}
                emptyLabel="Nenhum serviço encontrado."
                scrollable
              />
            </div>
          </>
        )}

        {canManage ? (
          <div className="flex items-center gap-2 border-t border-divider pt-3">
            <Button
              type="button"
              variant="secondary"
              disabled={!isDirty || setServices.isPending}
              onClick={handleSaveServices}
            >
              Salvar serviços
            </Button>
            {error ? (
              <p role="alert" className="text-xs text-critical">
                {error}
              </p>
            ) : null}
          </div>
        ) : null}
      </Card>

      <AttachDomainDrawer statusPageId={page.id} open={attachOpen} onOpenChange={setAttachOpen} />
    </div>
  );
}

function ExternalLinkIcon() {
  return (
    <svg
      width="14"
      height="14"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6" />
      <path d="M15 3h6v6" />
      <path d="M10 14 21 3" />
    </svg>
  );
}

interface ServiceGroupProps {
  label: string;
  services: Service[];
  selectedServiceIds: string[];
  canManage: boolean;
  onToggle: (serviceId: string) => void;
  emptyLabel?: string;
  scrollable?: boolean;
}

// ServiceGroup renders one labeled block of ServiceRows ("Vinculados" /
// "Disponíveis") - split out so the checked/unchecked row markup is
// defined once and each group only differs by which services it lists.
function ServiceGroup({
  label,
  services,
  selectedServiceIds,
  canManage,
  onToggle,
  emptyLabel,
  scrollable,
}: ServiceGroupProps) {
  return (
    <div className="flex flex-col gap-1">
      <span className="text-xs font-medium uppercase tracking-wide text-neutral-400">{label}</span>
      <div
        className={`flex flex-col divide-y divide-divider rounded-md border border-divider ${
          scrollable ? "max-h-64 overflow-y-auto" : ""
        }`}
      >
        {services.length === 0 ? (
          <p className="px-3 py-2 text-sm text-neutral-400">{emptyLabel}</p>
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
    <label
      className={`flex items-center gap-3 px-3 py-2 text-sm transition-colors ${
        disabled ? "cursor-not-allowed text-neutral-400" : "cursor-pointer hover:bg-neutral-800/40"
      }`}
    >
      <input
        type="checkbox"
        checked={checked}
        disabled={disabled}
        onChange={onToggle}
        className="h-4 w-4 rounded-sm border-divider bg-surface accent-accent"
      />
      <span className={checked ? "text-text" : "text-neutral-300"}>{name}</span>
    </label>
  );
}
