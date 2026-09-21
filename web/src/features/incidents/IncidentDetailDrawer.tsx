import { useState, type FormEvent } from "react";
import * as RadixDialog from "@radix-ui/react-dialog";
import { MdClose, MdOutlineAutoAwesome, MdOutlineRefresh } from "react-icons/md";
import { Button } from "../../components/ui/Button";
import { Tag } from "../../components/ui/Tag";
import { Textarea } from "../../components/ui/Textarea";
import { ApiError } from "../../lib/apiClient";
import type { Incident, IncidentStatus } from "../../types/api";
import {
  useAddIncidentUpdate,
  useConfirmCloseIncident,
  useDiscardCloseProposal,
  useIncidentUpdates,
  useTransitionIncident,
} from "./hooks";

import { formatDuration, severityColor, severityLabel } from "./incidentStatusMeta";
import { IncidentStatusTag } from "./IncidentStatusTag";
import { TimelineEntry } from "./TimelineEntry";

const transitionOptions: { value: IncidentStatus; label: string }[] = [
  { value: "identified", label: "Identificado" },
  { value: "monitoring", label: "Monitorando" },
  { value: "resolved", label: "Marcar como resolvido" },
];

function formatTimestamp(iso: string): string {
  return new Date(iso).toLocaleString("pt-BR", { day: "2-digit", month: "short", hour: "2-digit", minute: "2-digit" });
}

export interface IncidentDetailDrawerProps {
  incident: Incident | null;
  canManage: boolean;
  serviceName: (id: string) => string;
  onClose: () => void;
}

/** Drawer de detalhe (handoff-new-layout/Incidentes.dc.html's selected-incident
 * panel, INCPG-*). Não usa o <Drawer> compartilhado - mesmo motivo do
 * DomainDetailDrawer/ServiceDetailDrawer: o mock não tem cabeçalho com
 * borda nem rodapé, é um painel contínuo com badge+X no topo.
 *
 * A caixa "RESUMO GERADO POR IA" fica acima da timeline, igual ao mock -
 * mas o texto vem de dois lugares reais possíveis: `pending_close_comment`
 * (proposta ainda não confirmada) ou, pra incidente já resolvido, a entrada
 * da timeline com `is_ai_summary: true` (é isso que `ConfirmPendingClose`
 * grava - `pending_close_comment` já foi limpo nesse ponto). Qualquer
 * entrada `is_ai_summary` é removida da lista "Linha do tempo" (já
 * representada na caixa, não duplicada).
 *
 * O botão "Resolver com resumo de IA" do mock dispara geração síncrona
 * fictícia; o backend real só propõe o resumo de forma assíncrona (AI-19).
 * Decisão confirmada via AskUserQuestion: manter o texto/visual do mock,
 * mas desabilitado até existir uma proposta pendente de verdade - quando
 * existe, o clique confirma o fechamento real (`confirm-close`). "Descartar
 * proposta" e os botões de transição manual (Identificado/Monitorando/
 * Marcar como resolvido) são capacidade real adicional que o mock não
 * mostra - mantidos como ações secundárias, mesmo raciocínio de "Reabrir
 * incidente" abaixo. */
export function IncidentDetailDrawer({ incident, canManage, serviceName, onClose }: IncidentDetailDrawerProps) {
  const { data: updatesPage } = useIncidentUpdates(incident?.id ?? "", 1);
  const updates = updatesPage?.items ?? [];
  const timelineUpdates = updates.filter((u) => !u.is_ai_summary);
  const aiSummaryText = incident?.pending_close_comment ?? updates.find((u) => u.is_ai_summary)?.body ?? null;
  const addUpdate = useAddIncidentUpdate(incident?.id ?? "");
  const transition = useTransitionIncident(incident?.id ?? "");
  const confirmClose = useConfirmCloseIncident(incident?.id ?? "");
  const discardCloseProposal = useDiscardCloseProposal(incident?.id ?? "");
  const [body, setBody] = useState("");
  const [proposalError, setProposalError] = useState<string | null>(null);

  async function handlePublish(e: FormEvent) {
    e.preventDefault();
    if (!incident || !body.trim()) return;
    await addUpdate.mutateAsync(body);
    setBody("");
  }

  async function handleConfirmClose() {
    if (!incident?.pending_close_comment) return;
    setProposalError(null);
    try {
      await confirmClose.mutateAsync(incident.pending_close_comment);
    } catch (err) {
      setProposalError(err instanceof ApiError ? err.message : "Não foi possível confirmar o encerramento.");
    }
  }

  async function handleDiscardCloseProposal() {
    setProposalError(null);
    try {
      await discardCloseProposal.mutateAsync();
    } catch (err) {
      setProposalError(err instanceof ApiError ? err.message : "Não foi possível descartar a proposta.");
    }
  }

  return (
    <RadixDialog.Root
      open={incident !== null}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <RadixDialog.Portal>
        <RadixDialog.Overlay className="fixed inset-0 z-40 bg-black/60 data-[state=closed]:animate-[drawer-overlay-out_180ms_ease-in] data-[state=open]:animate-[drawer-overlay-in_200ms_ease-out]" />
        <RadixDialog.Content className="fixed right-0 top-0 z-50 flex h-full w-full max-w-md flex-col overflow-y-auto bg-surface p-6 shadow-lg outline-none data-[state=closed]:animate-[drawer-panel-out_220ms_ease-in] data-[state=open]:animate-[drawer-panel-in_260ms_cubic-bezier(0.16,1,0.3,1)]">
          {incident ? (
            <div className="flex flex-col gap-4">
              <div className="flex items-start justify-between">
                <div className="flex items-center gap-2">
                  <IncidentStatusTag status={incident.status} />
                  {incident.auto_created ? <Tag variant="neutral-outline">Automático</Tag> : null}
                </div>
                <RadixDialog.Close asChild>
                  <button type="button" aria-label="Fechar" className="cursor-pointer text-text-muted hover:text-text">
                    <MdClose size={18} aria-hidden="true" />
                  </button>
                </RadixDialog.Close>
              </div>

              <div className="-mt-1">
                <RadixDialog.Title asChild>
                  <h2 className="text-[18px] font-bold leading-tight text-text">{incident.title}</h2>
                </RadixDialog.Title>
                <p data-testid="incident-detail-subtitle" className="mt-1 text-[13px] text-text-muted">
                  {incident.service_ids.length > 0 ? incident.service_ids.map(serviceName).join(", ") : "—"} ·{" "}
                  <span className="font-bold" style={{ color: severityColor(incident.severity) }}>
                    {severityLabel(incident.severity)}
                  </span>
                </p>
              </div>

              <div className="grid grid-cols-2 gap-3">
                <div>
                  <div className="mb-1 text-[10.5px] font-bold uppercase tracking-wide text-text-muted">Aberto em</div>
                  <div className="text-[14.5px] font-bold text-text">{formatTimestamp(incident.created_at)}</div>
                </div>
                <div>
                  <div className="mb-1 text-[10.5px] font-bold uppercase tracking-wide text-text-muted">Duração</div>
                  <div className="text-[14.5px] font-bold text-text">
                    {formatDuration(incident.created_at, incident.resolved_at)}
                  </div>
                </div>
              </div>

              {aiSummaryText ? (
                <div
                  data-testid="ai-summary-box"
                  className="flex flex-col gap-2 rounded-md border px-3.5 py-3"
                  style={{
                    backgroundColor: "color-mix(in oklch, var(--color-accent) 10%, transparent)",
                    borderColor: "color-mix(in oklch, var(--color-accent) 40%, transparent)",
                  }}
                >
                  <div className="flex items-center gap-1.5 text-[11px] font-bold uppercase tracking-wide text-accent">
                    <MdOutlineAutoAwesome size={14} aria-hidden="true" />
                    Resumo gerado por IA
                  </div>
                  <p className="text-[13px] leading-relaxed text-text-muted">{aiSummaryText}</p>
                  {proposalError ? (
                    <p role="alert" className="text-xs text-critical">
                      {proposalError}
                    </p>
                  ) : null}
                  {canManage && incident.pending_close_comment ? (
                    <button
                      type="button"
                      onClick={handleDiscardCloseProposal}
                      disabled={confirmClose.isPending || discardCloseProposal.isPending}
                      className="w-fit cursor-pointer text-xs text-accent hover:underline disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      Descartar proposta
                    </button>
                  ) : null}
                </div>
              ) : null}

              <div>
                <div className="mb-2 text-[10.5px] font-bold uppercase tracking-wide text-text-muted">Linha do tempo</div>
                <div className="ml-1 flex flex-col gap-3 border-l-2 border-divider pl-4">
                  {timelineUpdates.length === 0 ? (
                    <p className="text-xs text-text-muted">Nenhuma atualização ainda.</p>
                  ) : (
                    timelineUpdates.map((update) => <TimelineEntry key={update.id} update={update} />)
                  )}
                </div>
              </div>

              {incident.status !== "resolved" ? (
                <>
                  {canManage ? (
                    <form onSubmit={handlePublish} className="flex flex-col gap-2">
                      <label htmlFor="incident-update-body" className="text-[12.5px] font-semibold text-text-muted">
                        Adicionar atualização
                      </label>
                      <Textarea
                        id="incident-update-body"
                        variant="filled"
                        value={body}
                        onChange={(e) => setBody(e.target.value)}
                        placeholder="Descreva o progresso da investigação..."
                        rows={3}
                      />
                      <Button type="submit" variant="secondary" className="w-full" disabled={addUpdate.isPending}>
                        Publicar atualização
                      </Button>
                    </form>
                  ) : null}

                  {canManage ? (
                    <Button
                      variant="solid"
                      className="w-full"
                      onClick={handleConfirmClose}
                      disabled={!incident.pending_close_comment || confirmClose.isPending || discardCloseProposal.isPending}
                    >
                      Resolver com resumo de IA
                    </Button>
                  ) : null}

                  {canManage ? (
                    <div className="flex flex-wrap gap-2">
                      {transitionOptions.map((opt) => (
                        <Button
                          key={opt.value}
                          variant="secondary"
                          disabled={transition.isPending || incident.status === opt.value}
                          onClick={() => transition.mutate(opt.value)}
                        >
                          {opt.label}
                        </Button>
                      ))}
                    </div>
                  ) : null}
                </>
              ) : (
                <>
                  <div
                    className="rounded-md border px-3.5 py-3 text-[12.5px] leading-relaxed"
                    style={{
                      backgroundColor: "color-mix(in oklch, var(--color-success) 14%, transparent)",
                      borderColor: "color-mix(in oklch, var(--color-success) 55%, transparent)",
                      color: "var(--color-success)",
                    }}
                  >
                    Incidente resolvido em {formatTimestamp(incident.resolved_at ?? incident.created_at)}.
                  </div>
                  {canManage ? (
                    <Button variant="ghost" onClick={() => transition.mutate("investigating")} disabled={transition.isPending}>
                      <MdOutlineRefresh size={14} aria-hidden="true" />
                      Reabrir incidente
                    </Button>
                  ) : null}
                </>
              )}
            </div>
          ) : null}
        </RadixDialog.Content>
      </RadixDialog.Portal>
    </RadixDialog.Root>
  );
}
