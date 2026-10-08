import { useId, useState } from "react";
import { useTranslation } from "react-i18next";
import { Textarea } from "../../components/ui/Textarea";

export interface ClosingNoteFieldProps {
  value: string;
  onChange: (value: string) => void;
}

/** Optional closing note sent with a manual "Mark as resolved" transition
 * (PATCH /api/incidents/{id} `note`). Collapsed behind a link by default so
 * resolving without a note stays a one-click action, exactly as before; the
 * note only takes effect when the operator then clicks the resolve button. */
export function ClosingNoteField({ value, onChange }: ClosingNoteFieldProps) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const id = useId();

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
        rows={2}
      />
    </div>
  );
}
