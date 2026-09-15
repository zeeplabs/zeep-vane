import { useEffect, useState, type FormEvent } from "react";
import { Drawer, drawerFooterPrimaryStyle, drawerFooterSecondaryStyle } from "../../components/ui/Drawer";
import { Button } from "../../components/ui/Button";
import { Field } from "../../components/ui/Field";
import { ApiError } from "../../lib/apiClient";
import { modelAllowlist, type LLMProviderName } from "../../lib/llmProviders";
import { useConnectLLMProvider } from "../settings/hooks";

const PROVIDER_ID: LLMProviderName = "openai";

export interface ConnectLLMProviderDrawerProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

/** Painel "Conectar LLM Provider" - mesmo chrome padrão de Drawer usado em
 * "Anexar domínio"/"Criar status page" (INTG-09). */
export function ConnectLLMProviderDrawer({ open, onOpenChange }: ConnectLLMProviderDrawerProps) {
  const connectMutation = useConnectLLMProvider(PROVIDER_ID);
  const [apiKey, setApiKey] = useState("");
  const [model, setModel] = useState("");
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (open) {
      setApiKey("");
      setModel("");
      setError(null);
    }
  }, [open]);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    try {
      await connectMutation.mutateAsync({ api_key: apiKey, model: model || undefined });
      onOpenChange(false);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Não foi possível conectar ao LLM Provider.");
    }
  }

  return (
    <Drawer
      open={open}
      onOpenChange={onOpenChange}
      title="Conectar LLM Provider"
      description="A chave é validada contra o provedor e criptografada em repouso. Não é reexibida após salvar."
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
            form="connect-llm-form"
            variant="solid"
            style={drawerFooterPrimaryStyle}
            disabled={connectMutation.isPending}
          >
            Salvar
          </Button>
        </>
      }
    >
      <form id="connect-llm-form" onSubmit={handleSubmit} className="flex flex-col gap-3">
        <Field label="API key" type="password" value={apiKey} onChange={(e) => setApiKey(e.target.value)} required />
        <div className="flex flex-col gap-1">
          <label htmlFor="connect-llm-model" className="text-sm font-medium text-text">
            Modelo
          </label>
          <select
            id="connect-llm-model"
            value={model}
            onChange={(e) => setModel(e.target.value)}
            className="min-h-9 rounded-md border border-divider bg-surface px-3 text-sm text-text"
          >
            <option value="">{modelAllowlist[PROVIDER_ID][0]}</option>
            {modelAllowlist[PROVIDER_ID].slice(1).map((m) => (
              <option key={m} value={m}>
                {m}
              </option>
            ))}
          </select>
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
