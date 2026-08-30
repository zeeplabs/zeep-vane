import { Button } from "./Button";

export interface PagerProps {
  page: number;
  totalPages: number;
  onChange: (page: number) => void;
}

// Reusable Anterior/Próximo + "Página X de Y" control for every paginated
// list screen (PAG-07). totalPages is computed by the caller as
// max(1, ceil(total/page_size)) per spec Edge Cases - Pager itself never
// special-cases total===0.
export function Pager({ page, totalPages, onChange }: PagerProps) {
  return (
    <div className="flex items-center justify-end gap-3">
      <span className="text-xs text-neutral-400">
        Página {page} de {totalPages}
      </span>
      <Button variant="ghost" disabled={page <= 1} onClick={() => onChange(page - 1)}>
        Anterior
      </Button>
      <Button variant="ghost" disabled={page >= totalPages} onClick={() => onChange(page + 1)}>
        Próximo
      </Button>
    </div>
  );
}
