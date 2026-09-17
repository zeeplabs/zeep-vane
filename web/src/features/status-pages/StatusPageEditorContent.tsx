import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { MdOutlineOpenInNew, MdCheck, MdContentCopy } from "react-icons/md";
import { Tag, type TagVariant } from "../../components/ui/Tag";
import { Button, buttonBaseClasses, buttonVariantClasses } from "../../components/ui/Button";
import { Field } from "../../components/ui/Field";
import { useAuth } from "../../auth/AuthProvider";
import { ApiError } from "../../lib/apiClient";
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

const statePillLabel: Record<StatusPageState, string> = {
  draft: "Sem domínio configurado",
  pending_tls: "Aguardando validação de DNS/certificado",
  published: "Publicada",
  tls_failed: "Falha",
};

interface StatusPagePillProps {
  page: StatusPage;
}

// Same dot+pill composition DomainStatusTag/services' StatusTag already
// established for the new layout - a status page's badge shouldn't look
// like a different, older component just because it has its own state
// machine (SPD-12/13).
function StatusPagePill({ page }: StatusPagePillProps) {
  // SPD-12: sem domínio nenhum anexado ainda - distinto de "draft" com
  // domínio (que não deveria mais ocorrer, ver AD-017), mas o texto e a
  // variante servem pros dois. "published"/"tls_failed" sempre vencem,
  // mesmo no formato defendido/impossível de published sem domain_id
  // (nunca produzido pelo fluxo real - MarkPublished exige domain_id via
  // JOIN por hostname - mas publicUrl() e este pill continuam defendendo
  // contra ele, mesmo raciocínio de StatusPagesSection.test.tsx).
  const state: StatusPageState =
    page.domain_id === null && page.state !== "published" && page.state !== "tls_failed" ? "draft" : page.state;
  return (
    <Tag variant={statePillVariant[state]} className="gap-1.5" style={{ borderRadius: "999px" }}>
      {state === "pending_tls" ? <span className="h-1.5 w-1.5 flex-none animate-pulse rounded-full bg-current" aria-hidden="true" /> : null}
      {statePillLabel[state]}
    </Tag>
  );
}

export interface StatusPageEditorContentProps {
  page: StatusPage;
  /** Controla o link "Pré-visualizar página pública". Default `true` -
   * necessário pra tela legada `/status-pages/{id}` (`StatusPageDetail.tsx`,
   * sem drawer de detalhe separado, esse é seu único jeito de pré-visualizar).
   * `EditStatusPageDrawer` do novo layout passa `false`: o link mudou para o
   * `StatusPageDetailDrawer` ("visualizar detalhes"), a pedido do Julio -
   * antes só existia no drawer de edição, o que era o lugar errado. */
  showPreviewLink?: boolean;
}

/** Corpo real de edição de uma status page (status, anexar domínio,
 * verificação de DNS/certificado, serviços vinculados) - extraído de
 * `StatusPageDetail.tsx` pra ser reusado tanto pela tela legada
 * `/status-pages/{id}` (ainda usada pelo fluxo pré-redesign) quanto pelo
 * `EditStatusPageDrawer` do novo layout, que substitui a navegação pra
 * tela separada por um drawer (mesmo modelo do de criação), por pedido
 * explícito do Julio: "em tela separada nao ficou legal". Layout alinhado
 * com os componentes já migrados (`DomainDetailDrawer`,
 * `AddStatusPageDrawer`): sem `Card`, seções separadas por `border-t`,
 * rótulos em uppercase 10.5px, checklist de serviço com checkbox quadrado
 * em vez do checkbox nativo redondo. */
export function StatusPageEditorContent({ page, showPreviewLink = true }: StatusPageEditorContentProps) {
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
      else setError("Não foi possível salvar os serviços vinculados.");
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-2">
        <StatusPagePill page={page} />
      </div>

      {page.domain_id === null ? (
        <div className="flex items-center justify-between gap-3">
          <p className="text-[13px] text-text-muted">Nenhum domínio anexado ainda.</p>
          <Button type="button" variant="secondary" className="w-fit" onClick={() => setAttachOpen(true)}>
            Anexar domínio
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
          Pré-visualizar página pública
        </a>
      ) : null}

      {/* Painel fixo de configuração de DNS/certificado (mirrors o fluxo de
          domínio customizado de plataformas como Vercel/Render) - permanece
          visível enquanto a página tem domínio anexado e ainda não está
          publicada, mesmo que o polling automático (useStatusPage) já
          esteja tentando detectar a transição sozinho a cada 10s. Restrito
          a canManage: o endpoint que ele lê (GET /api/instance/dns-target)
          e o que ele aciona (POST .../verify-domain) são ambos
          write-role-gated no backend - um viewer só veria um 403
          confuso/enganoso ("DNS não configurado" quando na verdade só não
          teve permissão de ler) em vez de nada. */}
      {page.domain_id !== null && page.subdomain !== null && page.state !== "published" && canManage ? (
        <div className="flex flex-col gap-3 border-t border-divider pt-4">
          <DomainVerificationPanel statusPageId={page.id} fullHostname={`${page.subdomain}.${hostname ?? "?"}`} />
        </div>
      ) : null}

      <div className="flex flex-col gap-3 border-t border-divider pt-4">
        <div className="flex items-center justify-between">
          <span className="text-[10.5px] font-bold uppercase tracking-wide text-text-muted">
            Serviços vinculados ({selectedServiceIds.length}/{allServices.length})
          </span>
        </div>

        {allServices.length === 0 ? (
          <p className="text-[13px] text-text-muted">Nenhum serviço cadastrado.</p>
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
              <Field
                type="text"
                variant="filled"
                label="Disponíveis"
                placeholder="Buscar serviço…"
                value={serviceQuery}
                onChange={(e) => setServiceQuery(e.target.value)}
              />
              <ServiceGroup
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
          <div className="flex items-center gap-3">
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
  const { t } = useTranslation();
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
        <span className="text-[10.5px] font-bold uppercase tracking-wide text-text-muted">Configuração DNS</span>
        <Button
          type="button"
          variant="secondary"
          disabled={verifyDomain.isPending}
          onClick={() => verifyDomain.mutate(statusPageId)}
        >
          {verifyDomain.isPending ? "Verificando…" : "Verificar DNS/certificado"}
        </Button>
      </div>

      <div className="overflow-hidden rounded-md border border-divider">
        <div className="grid grid-cols-[70px_1fr] gap-2 border-b border-divider bg-card-header-bg px-3.5 py-2.5">
          <span className="text-[11px] font-bold text-text-muted">TIPO</span>
          <span className="text-[11px] font-bold text-text-muted">VALOR</span>
        </div>
        <div className="grid grid-cols-[70px_1fr_auto] items-center gap-2 px-3.5 py-3">
          <span className="font-mono text-[12.5px] font-bold text-text">CNAME</span>
          {dnsTargetLoading ? (
            <span className="font-mono text-[12.5px] text-text-muted">Carregando…</span>
          ) : (
            <span className="min-w-0 truncate font-mono text-[12.5px] text-text-muted">
              {dnsTarget ?? "não configurado"}
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
        Aponte <strong className="text-text">{fullHostname}</strong> para o valor acima. O certificado é emitido
        automaticamente assim que o DNS propagar e alguém acessar a página (ou ao clicar em "Verificar" acima).
      </p>

      {verifyDomain.isError ? (
        <p role="alert" className="text-xs text-critical">
          {verifyDomain.error instanceof ApiError
            ? verifyDomain.error.message
            : "Não foi possível verificar o domínio agora."}
        </p>
      ) : null}

      {result ? (
        <div className="flex flex-col gap-1.5 border-t border-divider pt-3 text-xs">
          <VerificationRow
            ok={result.dns_resolved && result.dns_matches_target !== false}
            label={
              !result.dns_resolved
                ? "DNS ainda não resolve para nenhum destino"
                : result.dns_matches_target === false
                  ? `DNS resolve para ${result.resolved_ips.join(", ")}, diferente do destino esperado`
                  : result.dns_matches_target === true
                    ? `DNS resolve corretamente (${result.resolved_ips.join(", ")})`
                    : `DNS resolve (${result.resolved_ips.join(", ")})`
            }
          />
          <VerificationRow
            ok={result.tls_cert_valid}
            label={
              result.tls_cert_valid
                ? "Certificado TLS emitido e válido"
                : result.tls_reachable
                  ? `Conexão HTTPS respondeu, mas o certificado não é válido para ${fullHostname}${
                      result.tls_error ? `: ${result.tls_error}` : ""
                    }`
                  : `Conexão HTTPS ainda falha${result.tls_error ? `: ${result.tls_error}` : ""}`
            }
          />
          <p className="text-text-muted">Última verificação: {new Date(result.checked_at).toLocaleString()}</p>
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
// ("Vinculados" / "Disponíveis") - same checkbox-row composition
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
