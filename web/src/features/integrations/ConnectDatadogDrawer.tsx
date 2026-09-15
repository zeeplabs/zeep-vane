import { useEffect, useState, type FormEvent } from "react";
import { Drawer, drawerFooterPrimaryStyle, drawerFooterSecondaryStyle } from "../../components/ui/Drawer";
import { Button } from "../../components/ui/Button";
import { Field } from "../../components/ui/Field";
import { ApiError } from "../../lib/apiClient";
import { useConnectDatadog } from "./hooks";

export interface ConnectDatadogDrawerProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

/** Painel "Conectar Datadog" - mesmo chrome padrão de Drawer usado em
 * "Anexar domínio"/"Criar status page" (INTG-08). */
export function ConnectDatadogDrawer({ open, onOpenChange }: ConnectDatadogDrawerProps) {
  const connectMutation = useConnectDatadog();
  const [apiKey, setApiKey] = useState("");
  const [appKey, setAppKey] = useState("");
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (open) {
      setApiKey("");
      setAppKey("");
      setError(null);
    }
  }, [open]);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    try {
      await connectMutation.mutateAsync({ api_key: apiKey, app_key: appKey });
      onOpenChange(false);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Não foi possível conectar ao Datadog.");
    }
  }

  return (
    <Drawer
      open={open}
      onOpenChange={onOpenChange}
      title="Conectar Datadog"
      description="As chaves são validadas contra o Datadog e criptografadas em repouso. Não são reexibidas após salvar."
      closeLabel="Fechar"
      footer={
        <>
          <Button
            type="button"
            variant="secondary"
            style={drawerFooterSecondaryStyle}
            onClick={() => onOpenChange(false)}
          >
            Cancelar
          </Button>
          <Button
            type="submit"
            form="connect-datadog-form"
            variant="solid"
            style={drawerFooterPrimaryStyle}
            disabled={connectMutation.isPending}
          >
            Salvar
          </Button>
        </>
      }
    >
      <form id="connect-datadog-form" onSubmit={handleSubmit} className="flex flex-col gap-3">
        <Field label="API key" type="password" value={apiKey} onChange={(e) => setApiKey(e.target.value)} required />
        <Field label="App key" type="password" value={appKey} onChange={(e) => setAppKey(e.target.value)} required />
        {error ? (
          <p role="alert" className="text-xs text-critical">
            {error}
          </p>
        ) : null}
      </form>
    </Drawer>
  );
}
