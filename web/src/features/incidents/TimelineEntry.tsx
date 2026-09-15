import type { IncidentUpdate } from "../../types/api";

// INCPG-08/09/10/11: AI-generated closing summaries render distinct from
// human/system updates; human updates get a generic "Equipe" label since no
// admin-name-by-ID lookup is wired into this endpoint (spec.md Assumptions,
// confirmed via AskUserQuestion).
function authorLabel(update: IncidentUpdate): string {
  if (update.is_ai_summary) return "Resumo gerado por IA";
  if (update.author_id) return "Equipe";
  return "Sistema";
}

export function TimelineEntry({ update }: { update: IncidentUpdate }) {
  if (update.is_ai_summary) {
    return (
      <div
        className="flex flex-col gap-0.5 rounded-md border px-3 py-2.5"
        style={{
          backgroundColor: "color-mix(in oklch, var(--color-accent) 10%, transparent)",
          borderColor: "color-mix(in oklch, var(--color-accent) 40%, transparent)",
        }}
      >
        <p className="text-[11px] font-bold tracking-wide text-accent uppercase">{authorLabel(update)}</p>
        <p className="text-sm text-text">{update.body}</p>
        <p className="text-xs text-neutral-400">{new Date(update.created_at).toLocaleString("pt-BR")}</p>
      </div>
    );
  }
  return (
    <div className="flex gap-3">
      <div className="mt-1.5 h-1.5 w-1.5 shrink-0 rounded-full bg-accent" />
      <div className="flex flex-col gap-0.5">
        <p className="text-xs font-semibold text-text-muted">{authorLabel(update)}</p>
        <p className="text-sm text-text">{update.body}</p>
        <p className="text-xs text-neutral-400">{new Date(update.created_at).toLocaleString("pt-BR")}</p>
      </div>
    </div>
  );
}
