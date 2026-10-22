import { useId, useState } from "react";
import { useTranslation } from "react-i18next";
import { Textarea } from "../../components/ui/Textarea";

// CLOSING_NOTE_MAX_LENGTH mirrors the backend's cap on PATCH
// /api/incidents/{id} `note` (internal/api's maxClosingNoteRunes, 422
// above it). The textarea's maxLength counts UTF-16 code units, which are
// never fewer than runes, so a note the browser accepts is always within
// the backend's limit.
export const CLOSING_NOTE_MAX_LENGTH = 2000;

export interface ClosingNoteFieldProps {
  value: string;
  onChange: (value: string) => void;
}

/** Optional closing note sent with a manual "Mark as resolved" transition
 * (PATCH /api/incidents/{id} `note`). Collapsed behind a link by default so
 * resolving without a note stays a one-click action, exactly as before; the
 * note only takes effect when the operator then clicks the resolve button.
 *
 * The expanded/collapsed state is local, so callers render it with
 * `key={incidentId}`: a different incident must start collapsed, never
 * inherit the previous incident's open field. */
export function ClosingNoteField({ value, onChange }: ClosingNoteFieldProps) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const id = useId();
  const counterId = `${id}-counter`;

  if (!open) {
    return (
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="w-fit cursor-pointer text-xs text-accent hover:underline"
      >
        {t("incidentDetail.closingNoteToggle")}
      </button>
    );
  }

  return (
    <div className="flex flex-col gap-1.5">
      <label htmlFor={id} className="text-[12.5px] font-semibold text-text-muted">
        {t("incidentDetail.closingNoteLabel")}
      </label>
      <Textarea
        id={id}
        variant="filled"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={t("incidentDetail.closingNotePlaceholder")}
        maxLength={CLOSING_NOTE_MAX_LENGTH}
        aria-describedby={counterId}
        rows={2}
      />
      <p id={counterId} data-testid="closing-note-counter" className="self-end text-[11px] text-text-muted">
        {t("incidentDetail.closingNoteCounter", { length: value.length, max: CLOSING_NOTE_MAX_LENGTH })}
      </p>
    </div>
  );
}
