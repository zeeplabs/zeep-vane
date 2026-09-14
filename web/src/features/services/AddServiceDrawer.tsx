import { useState, type FormEvent } from "react";
import { Drawer } from "../../components/ui/Drawer";
import { Button } from "../../components/ui/Button";
import { Field } from "../../components/ui/Field";
import { ApiError } from "../../lib/apiClient";
import { useSLOSearch } from "../integrations/hooks";
import { useCreateService } from "./hooks";

export interface AddServiceDrawerProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

// SPEC_DEVIATION: copy (title/labels/button text) is kept identical to
// ServicesSection's original inline dialog rather than the mock's
// "Adicionar serviço" wording - extracted verbatim so ServicesSection's
// existing tests keep passing unmodified (T8's own constraint) and design.md
// documents ServicesSection as untouched by this feature. SVC-20's "Adicionar
// serviço" trigger label lives on ServiceListPage's own button, which opens
// this same drawer.
/** Vínculo de um novo serviço a um SLO do Datadog (SVC-20..25) - extraído de
 * ServicesSection.tsx para ser reutilizado também por ServiceListPage. */
export function AddServiceDrawer({ open, onOpenChange }: AddServiceDrawerProps) {
  const [name, setName] = useState("");
  const [query, setQuery] = useState("");
  const [selectedSlo, setSelectedSlo] = useState<{ id: string; name: string } | null>(null);
  const [error, setError] = useState<string | null>(null);

  const createService = useCreateService();
  const sloSearch = useSLOSearch(query);

  function resetForm() {
    setName("");
    setQuery("");
    setSelectedSlo(null);
    setError(null);
  }

  function handleOpenChange(next: boolean) {
    if (!next) resetForm();
    onOpenChange(next);
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    if (!selectedSlo) {
      // The real backend requires slo_id on creation (SPEC_DEVIATION, I15:
      // the earlier mock allowed a service with no SLO at all) - validated
      // client-side so the admin gets an immediate, specific message
      // instead of a generic 422 from the API.
      setError("Selecione um SLO da lista antes de salvar.");
      return;
    }
    try {
      await createService.mutateAsync({ name, slo_id: selectedSlo.id, slo_name: selectedSlo.name });
      resetForm();
      onOpenChange(false);
    } catch (err) {
      if (err instanceof ApiError) setError(err.message);
      else setError("Não foi possível vincular o serviço.");
    }
  }

  const canSubmit = name.trim().length > 0 && selectedSlo !== null;

  return (
    <Drawer
      open={open}
      onOpenChange={handleOpenChange}
      title="Vincular serviço"
      description="Associe um serviço a um SLO existente no Datadog."
      footer={
        <>
          <Button type="button" variant="secondary" onClick={() => handleOpenChange(false)}>
            Cancelar
          </Button>
          <Button
            type="submit"
            form="add-service-form"
            variant="primary"
            disabled={createService.isPending || !canSubmit}
          >
            Salvar
          </Button>
        </>
      }
    >
      <form id="add-service-form" onSubmit={handleSubmit} className="flex flex-col gap-3">
        <Field label="Nome do serviço" value={name} onChange={(e) => setName(e.target.value)} required />
        <Field
          label="Buscar SLO"
          value={query}
          onChange={(e) => {
            setQuery(e.target.value);
            setSelectedSlo(null);
          }}
          placeholder="Digite o nome do SLO"
        />
        {query.trim() && sloSearch.data ? (
          <ul className="flex flex-col gap-1 rounded-md border border-divider bg-bg p-1">
            {sloSearch.data.length === 0 ? (
              <li className="px-2 py-1.5 text-xs text-neutral-400">Nenhum SLO encontrado.</li>
            ) : (
              sloSearch.data.map((slo) => (
                <li key={slo.id}>
                  <button
                    type="button"
                    onClick={() => {
                      setSelectedSlo(slo);
                      setQuery(slo.name);
                    }}
                    className={
                      "w-full cursor-pointer rounded-sm px-2 py-1.5 text-left text-sm hover:bg-neutral-800 " +
                      (selectedSlo?.id === slo.id ? "text-accent" : "text-text")
                    }
                  >
                    {slo.name}
                  </button>
                </li>
              ))
            )}
          </ul>
        ) : null}
        {error ? (
          <p role="alert" className="text-xs text-critical">
            {error}
          </p>
        ) : null}
      </form>
    </Drawer>
  );
}
