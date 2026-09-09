import { useState, type FormEvent } from "react";
import { useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Card } from "../../components/ui/Card";
import { Tag } from "../../components/ui/Tag";
import { Button } from "../../components/ui/Button";
import { Field } from "../../components/ui/Field";
import { useAuth } from "../../auth/AuthProvider";
import type { IncidentStatus } from "../../types/api";
import { ApiError } from "../../lib/apiClient";
import {
  useAddIncidentUpdate,
  useConfirmCloseIncident,
  useDiscardCloseProposal,
  useIncidents,
  useIncidentUpdates,
  useTransitionIncident,
} from "./hooks";

const transitionOptions: { value: IncidentStatus; label: string }[] = [
  { value: "identified", label: "Identificado" },
  { value: "monitoring", label: "Monitorando" },
  { value: "resolved", label: "Marcar como resolvido" },
];

export function IncidentDetail() {
  const { id = "" } = useParams();
  const { t } = useTranslation();
  const { hasRole } = useAuth();
  const canManage = hasRole(["owner", "operator"]);
  // SPEC_DEVIATION: design.md/tasks.md didn't account for IncidentDetail.tsx
  // as a caller of these hooks (only IncidentsPage.tsx is named in T5). Both
  // hooks now require a page argument and return a Page<T> envelope, so this
  // fixes compilation with the minimum change: fixed page 1, read .items.
  // spec.md's Out of Scope table already defers any dedicated Pager UI for
  // this timeline view; this file gets no Pager, matching that decision.
  // A real gap remains: an incident whose id isn't on page 1 of /api/incidents
  // (more than 25 incidents exist) won't be found here, since there is no
  // GET /api/incidents/{id} endpoint - this pre-dates pagination (the page
  // was already fetching "all" incidents to find one by id) but pagination
  // makes the ceiling explicit. Out of scope for this feature; flagged for
  // a future task.
  const { data: incidentsPage } = useIncidents(1);
  const { data: updatesPage } = useIncidentUpdates(id, 1);
  const incidents = incidentsPage?.items;
  const updates = updatesPage?.items;
  const addUpdate = useAddIncidentUpdate(id);
  const transition = useTransitionIncident(id);
  const confirmClose = useConfirmCloseIncident(id);
  const discardCloseProposal = useDiscardCloseProposal(id);

  const [body, setBody] = useState("");
  const [proposalError, setProposalError] = useState<string | null>(null);

  const incident = incidents?.find((i) => i.id === id);

  async function handlePublish(e: FormEvent) {
    e.preventDefault();
    if (!body.trim()) return;
    await addUpdate.mutateAsync(body);
    setBody("");
  }

  async function handleConfirmClose() {
    if (!incident?.pending_close_comment) return;
    setProposalError(null);
    try {
      await confirmClose.mutateAsync(incident.pending_close_comment);
    } catch (err) {
      setProposalError(err instanceof ApiError ? err.message : t("incidentDetail.genericConfirmCloseError"));
    }
  }

  async function handleDiscardCloseProposal() {
    setProposalError(null);
    try {
      await discardCloseProposal.mutateAsync();
    } catch (err) {
      setProposalError(err instanceof ApiError ? err.message : t("incidentDetail.genericDiscardProposalError"));
    }
  }

  if (!incident) return <p className="text-neutral-400">Incidente não encontrado.</p>;

  return (
    <div className="mx-auto flex w-full max-w-[1280px] flex-col gap-6">
      <div className="flex items-center gap-2">
        <Tag variant={incident.status === "resolved" ? "neutral" : "accent"}>
          {incident.status === "resolved" ? "Resolvido" : incident.status}
        </Tag>
        {incident.auto_created ? <Tag variant="neutral-outline">Automático</Tag> : null}
        <h3 className="text-text">{incident.title}</h3>
      </div>

      {/* Pending-close-comment banner (AI-19..AI-22): shown whenever the LLM
          has proposed a closing comment awaiting confirmation, regardless
          of role - only the confirm/discard buttons are gated on canManage
          (server still enforces 403 on either action regardless, T17). */}
      {incident.pending_close_comment ? (
        <Card
          elevation="elev-sm"
          className="flex flex-col gap-2 p-4"
          style={{ border: "1px solid color-mix(in oklch, var(--color-accent) 30%, var(--color-divider))" }}
        >
          <p className="m-0 text-xs uppercase tracking-wide text-neutral-400">
            {t("incidentDetail.pendingCloseBannerLabel")}
          </p>
          <p className="m-0 text-sm text-text">{incident.pending_close_comment}</p>
          {proposalError ? (
            <p role="alert" className="text-xs text-critical">
              {proposalError}
            </p>
          ) : null}
          {canManage ? (
            <div className="flex gap-2">
              <Button
                variant="primary"
                onClick={handleConfirmClose}
                disabled={confirmClose.isPending || discardCloseProposal.isPending}
              >
                {t("incidentDetail.confirmCloseButton")}
              </Button>
              <Button
                variant="secondary"
                onClick={handleDiscardCloseProposal}
                disabled={confirmClose.isPending || discardCloseProposal.isPending}
              >
                {t("incidentDetail.discardProposalButton")}
              </Button>
            </div>
          ) : null}
        </Card>
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

      {canManage ? (
        <form onSubmit={handlePublish} className="flex flex-col gap-2">
          <Field
            label="Novo update"
            value={body}
            onChange={(e) => setBody(e.target.value)}
            placeholder="Descreva o progresso da investigação"
          />
          <Button type="submit" variant="primary" className="w-fit" disabled={addUpdate.isPending}>
            Publicar
          </Button>
        </form>
      ) : null}

      <div className="flex flex-col gap-3">
        {(updates ?? []).map((update) => (
          <Card key={update.id} elevation="elev-sm" className="flex gap-3 p-4">
            <div className="mt-1 h-2 w-2 shrink-0 rounded-full bg-accent" />
            <div className="flex flex-col gap-1">
              <p className="text-sm text-text">{update.body}</p>
              <p className="text-xs text-neutral-400">
                {new Date(update.created_at).toLocaleString("pt-BR")}
              </p>
            </div>
          </Card>
        ))}
      </div>
    </div>
  );
}
