import { useState, type FormEvent, type ReactNode } from "react";
import { MdOutlinePublic, MdOutlineLink } from "react-icons/md";
import { Drawer, drawerFooterPrimaryStyle, drawerFooterSecondaryStyle } from "../../components/ui/Drawer";
import { Button } from "../../components/ui/Button";
import { Field } from "../../components/ui/Field";
import { ApiError } from "../../lib/apiClient";
import { useCreateDomain } from "./hooks";

export interface AddDomainDrawerProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

type DomainTypeChoice = "vane-subdomain" | "custom";

/** Drawer de cadastro de domínio da aba Domínios (spec.md DSP-09..12):
 * escolha de tipo ("Subdomínio Vane" desabilitado/decorativo, "Domínio
 * próprio" selecionado por padrão e o único funcional) + hostname, enviado
 * via `useCreateDomain`. Segue o mesmo padrão de tile desabilitado do
 * `ModeCard` local de `AddServiceDrawer.tsx` (T8's Reuses). */
export function AddDomainDrawer({ open, onOpenChange }: AddDomainDrawerProps) {
  const [domainType, setDomainType] = useState<DomainTypeChoice>("custom");
  const [hostname, setHostname] = useState("");
  const [error, setError] = useState<string | null>(null);

  const createDomain = useCreateDomain();

  function resetForm() {
    setDomainType("custom");
    setHostname("");
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
      await createDomain.mutateAsync({ hostname });
      resetForm();
      onOpenChange(false);
    } catch (err) {
      if (err instanceof ApiError) setError(err.message);
      else setError("Não foi possível cadastrar o domínio.");
    }
  }

  return (
    <Drawer
      open={open}
      onOpenChange={handleOpenChange}
      title="Adicionar domínio"
      description="Cadastre um domínio raiz para publicar uma status page em um subdomínio dele."
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
            form="add-domain-form"
            variant="solid"
            style={drawerFooterPrimaryStyle}
            disabled={createDomain.isPending || hostname.trim().length === 0}
          >
            Adicionar domínio
          </Button>
        </>
      }
    >
      <form id="add-domain-form" onSubmit={handleSubmit} className="flex flex-col gap-[18px]">
        <div className="flex flex-col gap-[6px]">
          <span className="text-sm font-medium text-text">Tipo de domínio</span>
          <div role="group" aria-label="Tipo de domínio" className="grid grid-cols-2 gap-[10px]">
            <ModeCard
              active={false}
              disabled
              title="Subdomínio Vane"
              description="Em breve: publique sem configurar DNS."
              icon={<MdOutlinePublic size={18} />}
            />
            <ModeCard
              active={domainType === "custom"}
              title="Domínio próprio"
              description="Use um domínio que você já possui."
              icon={<MdOutlineLink size={18} />}
              onClick={() => setDomainType("custom")}
            />
          </div>
        </div>

        {domainType === "custom" ? (
          <Field
            variant="filled"
            label="Hostname"
            value={hostname}
            onChange={(e) => setHostname(e.target.value)}
            placeholder="status.suaempresa.com"
            required
          />
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

interface ModeCardProps {
  active: boolean;
  disabled?: boolean;
  icon: ReactNode;
  title: string;
  description: string;
  onClick?: () => void;
}

// ModeCard mirrors AddServiceDrawer.tsx's local ModeCard - a disabled tile
// has no onClick attached at all (not just a disabled prop), so no click
// handler exists for "Subdomínio Vane" to fire (DSP-10).
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
