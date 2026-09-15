import { useState, type FormEvent, type ReactNode } from "react";
import { MdOutlinePublic, MdOutlineLock } from "react-icons/md";
import { Drawer, drawerFooterPrimaryStyle, drawerFooterSecondaryStyle } from "../../components/ui/Drawer";
import { Button } from "../../components/ui/Button";
import { Field } from "../../components/ui/Field";
import { Tag } from "../../components/ui/Tag";
import { ApiError } from "../../lib/apiClient";
import { useServices } from "../services/hooks";
import { useCreateStatusPage } from "./hooks";

export interface AddStatusPageDrawerProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

type VisibilityChoice = "public" | "private";

/** Drawer de criação de status page da aba Status Pages (spec.md DSP-15):
 * nome + escolha de visibilidade ("Público" selecionado por padrão e o
 * único funcional, "Privado" desabilitado/decorativo) + checklist de
 * serviços, enviado via `useCreateStatusPage`. Segue o mesmo padrão de tile
 * desabilitado de `AddDomainDrawer.tsx`'s `ModeCard` local (T8's Reuses). */
export function AddStatusPageDrawer({ open, onOpenChange }: AddStatusPageDrawerProps) {
  const [visibility, setVisibility] = useState<VisibilityChoice>("public");
  const [name, setName] = useState("");
  const [serviceIds, setServiceIds] = useState<string[]>([]);
  const [error, setError] = useState<string | null>(null);

  // SPEC_DEVIATION: fixed page 1 for now - the service checklist source has
  // no Pager UI here, same deviation as StatusPagesSection.tsx.
  const { data: servicesPage } = useServices(1);
  const services = servicesPage?.items;
  const createStatusPage = useCreateStatusPage();

  function toggleService(id: string) {
    setServiceIds((prev) => (prev.includes(id) ? prev.filter((s) => s !== id) : [...prev, id]));
  }

  function resetForm() {
    setVisibility("public");
    setName("");
    setServiceIds([]);
    setError(null);
  }

  function handleOpenChange(next: boolean) {
    if (!next) resetForm();
    onOpenChange(next);
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    try {
      await createStatusPage.mutateAsync({ name, service_ids: serviceIds });
      resetForm();
      onOpenChange(false);
    } catch (err) {
      if (err instanceof ApiError) setError(err.message);
      else setError("Não foi possível criar a status page.");
    }
  }

  return (
    <Drawer
      open={open}
      onOpenChange={handleOpenChange}
      title="Criar status page"
      description="Vincule os serviços que essa status page vai exibir."
      closeLabel="Fechar"
      footer={
        <>
          <Button
            type="button"
            variant="secondary"
            style={drawerFooterSecondaryStyle}
            onClick={() => handleOpenChange(false)}
          >
            Cancelar
          </Button>
          <Button
            type="submit"
            form="add-status-page-form"
            variant="solid"
            style={drawerFooterPrimaryStyle}
            disabled={createStatusPage.isPending || name.trim().length === 0}
          >
            Criar
          </Button>
        </>
      }
    >
      <form id="add-status-page-form" onSubmit={handleSubmit} className="flex flex-col gap-[18px]">
        <Field label="Nome" value={name} onChange={(e) => setName(e.target.value)} required />

        <div className="flex flex-col gap-[6px]">
          <span className="text-sm font-medium text-text">Visibilidade</span>
          <div role="group" aria-label="Visibilidade" className="grid grid-cols-2 gap-[10px]">
            <ModeCard
              active={visibility === "public"}
              title="Público"
              description="Qualquer pessoa com o link pode acessar."
              icon={<MdOutlinePublic size={18} />}
              onClick={() => setVisibility("public")}
            />
            <ModeCard
              active={false}
              disabled
              title="Privado"
              description="Em breve: exige autenticação para acessar."
              icon={<MdOutlineLock size={18} />}
            />
          </div>
        </div>

        <div className="flex flex-col gap-1">
          <span className="text-sm font-medium text-text">Serviços</span>
          <div className="flex flex-wrap gap-2">
            {(services ?? []).map((s) => {
              const active = serviceIds.includes(s.id);
              return (
                <button key={s.id} type="button" onClick={() => toggleService(s.id)} aria-pressed={active} className="cursor-pointer">
                  <Tag variant={active ? "accent" : "accent-outline"}>{s.name}</Tag>
                </button>
              );
            })}
          </div>
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

interface ModeCardProps {
  active: boolean;
  disabled?: boolean;
  icon: ReactNode;
  title: string;
  description: string;
  onClick?: () => void;
}

// ModeCard mirrors AddDomainDrawer.tsx's local ModeCard - a disabled tile has
// no onClick attached at all (not just a disabled prop), so no click
// handler exists for "Privado" to fire (DSP-15, Out of Scope).
function ModeCard({ active, disabled, icon, title, description, onClick }: ModeCardProps) {
  return (
    <button
      type="button"
      aria-pressed={active && !disabled}
      aria-disabled={disabled}
      aria-label={title}
      onClick={disabled ? undefined : onClick}
      className={
        "flex flex-col items-start rounded-[10px] border p-[14px] text-left transition-colors " +
        (disabled
          ? "cursor-not-allowed border-divider bg-surface text-neutral-400 opacity-60"
          : "cursor-pointer " +
            (active
              ? "border-accent bg-accent-100 text-accent"
              : "border-divider bg-surface text-neutral-400 hover:text-text"))
      }
    >
      <span className="mb-[8px]">{icon}</span>
      <span className="mb-0.5 text-[13px] font-bold">{title}</span>
      <span className="text-[11.5px] leading-[1.4] text-neutral-400">{description}</span>
    </button>
  );
}
