import { useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { Card } from "../../components/ui/Card";
import { Field } from "../../components/ui/Field";
import { Button } from "../../components/ui/Button";
import { Pager } from "../../components/ui/Pager";
import { Tag } from "../../components/ui/Tag";
import { useAuth } from "../../auth/AuthProvider";
import { ApiError } from "../../lib/apiClient";
import { modelAllowlist, type LLMProviderName, type LLMProviderStatus } from "../../lib/llmProviders";
import {
  useActivateLLMProvider,
  useConnectLLMProvider,
  useLLMProviders,
  useSetLLMProviderModel,
} from "./hooks";

const PROVIDER_ID: LLMProviderName = "openai";

function formatTimestamp(iso: string | null | undefined): string {
  if (!iso) return "-";
  return new Date(iso).toLocaleString("pt-BR");
}

// AISettings is structurally identical to EmailProvidersPage/ProviderRow
// (T21's direct template) - a connect form (API key + optional model),
// activate button, and connected/active status display, but scoped to the
// single known LLM provider ("openai", AI-01) instead of a fixed list.
export function AISettings() {
  const { t } = useTranslation();
  const { hasRole } = useAuth();
  const canManage = hasRole(["owner", "operator"]);
  const [page, setPage] = useState(1);
  const { data, isLoading } = useLLMProviders(page);
  const connectMutation = useConnectLLMProvider(PROVIDER_ID);
  const setModelMutation = useSetLLMProviderModel(PROVIDER_ID);
  const activateMutation = useActivateLLMProvider();

  const [formOpen, setFormOpen] = useState(false);
  const [apiKey, setApiKey] = useState("");
  const [model, setModel] = useState("");
  const [error, setError] = useState<string | null>(null);

  if (isLoading) {
    return <p className="text-neutral-400">Carregando…</p>;
  }

  const status: LLMProviderStatus | undefined = data?.providers.find((p) => p.provider === PROVIDER_ID);
  const isActive = data?.active_provider === PROVIDER_ID;
  const totalPages = Math.max(1, Math.ceil((data?.total ?? 0) / (data?.page_size ?? 20)));

  async function handleConnectSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    try {
      await connectMutation.mutateAsync({ api_key: apiKey, model: model || undefined });
      setApiKey("");
      setModel("");
      setFormOpen(false);
    } catch (err) {
      if (err instanceof ApiError) setError(err.message);
      else setError(t("aiSettings.genericConnectError"));
    }
  }

  async function handleModelChange(nextModel: string) {
    setError(null);
    try {
      await setModelMutation.mutateAsync(nextModel);
    } catch (err) {
      if (err instanceof ApiError) setError(err.message);
      else setError(t("aiSettings.genericModelError"));
    }
  }

  async function handleActivate() {
    setError(null);
    try {
      await activateMutation.mutateAsync(PROVIDER_ID);
    } catch (err) {
      if (err instanceof ApiError) setError(err.message);
      else setError(t("aiSettings.genericActivateError"));
    }
  }

  return (
    <div className="mx-auto flex w-full max-w-[1280px] flex-col gap-4">
      <div>
        <h2 className="text-text">{t("aiSettings.title")}</h2>
        <p className="m-0 text-[13.5px] text-neutral-400">{t("aiSettings.subtitle")}</p>
      </div>

      <Card elevation="elev-sm" className="flex flex-col gap-3 p-4">
        <div className="flex items-center gap-3">
          <div className="flex-1">
            <div className="text-[15px] font-medium text-text">{t("aiSettings.providerLabel")}</div>
            <div className="text-xs text-neutral-400">
              {status ? (
                <>
                  {status.model}
                  {"  ·  "}
                  {t("aiSettings.lastChecked")}: {formatTimestamp(status.last_checked_at)}
                  {status.status === "invalid" && status.last_error ? `  ·  ${status.last_error}` : ""}
                </>
              ) : (
                t("aiSettings.notConnected")
              )}
            </div>
          </div>

          {status?.status === "invalid" ? (
            <Tag variant="critical">{t("aiSettings.invalid")}</Tag>
          ) : status?.status === "connected" && isActive ? (
            <Tag variant="success">{t("aiSettings.active")}</Tag>
          ) : status?.status === "connected" ? (
            <Tag variant="accent-outline">{t("aiSettings.connected")}</Tag>
          ) : (
            <Tag variant="neutral-outline">{t("aiSettings.notConnected")}</Tag>
          )}

          {status?.status === "connected" ? (
            <select
              aria-label={t("aiSettings.modelLabel")}
              value={status.model}
              disabled={!canManage || setModelMutation.isPending}
              onChange={(e) => handleModelChange(e.target.value)}
              className="h-9 rounded-md border border-divider bg-bg px-2 text-sm text-text"
            >
              {modelAllowlist[PROVIDER_ID].map((m) => (
                <option key={m} value={m}>
                  {m}
                </option>
              ))}
            </select>
          ) : null}

          {canManage && status?.status === "connected" && !isActive ? (
            <Button variant="secondary" onClick={handleActivate} disabled={activateMutation.isPending}>
              {t("aiSettings.activateButton")}
            </Button>
          ) : null}

          {canManage ? (
            <Button variant={status ? "secondary" : "primary"} onClick={() => setFormOpen((open) => !open)}>
              {status ? t("aiSettings.reconnectButton") : t("aiSettings.connectButton")}
            </Button>
          ) : null}
        </div>

        {error ? (
          <p role="alert" className="text-xs text-critical">
            {error}
          </p>
        ) : null}

        {formOpen && canManage ? (
          <>
            <div className="h-px bg-divider" />
            <form onSubmit={handleConnectSubmit} className="flex flex-col gap-3">
              <div className="grid grid-cols-2 gap-3">
                <Field
                  label={t("aiSettings.apiKeyLabel")}
                  type="password"
                  value={apiKey}
                  onChange={(e) => setApiKey(e.target.value)}
                  required
                />
                <div className="flex flex-col gap-1">
                  <label htmlFor="ai-settings-model" className="text-sm font-medium text-text">
                    {t("aiSettings.modelLabel")}
                  </label>
                  <select
                    id="ai-settings-model"
                    value={model}
                    onChange={(e) => setModel(e.target.value)}
                    className="h-9 rounded-md border border-divider bg-bg px-2 text-sm text-text"
                  >
                    <option value="">{modelAllowlist[PROVIDER_ID][0]}</option>
                    {modelAllowlist[PROVIDER_ID].slice(1).map((m) => (
                      <option key={m} value={m}>
                        {m}
                      </option>
                    ))}
                  </select>
                </div>
              </div>
              <p className="m-0 text-[11.5px] text-neutral-400">{t("aiSettings.keyHint")}</p>
              <div className="flex justify-end gap-2">
                <Button type="button" variant="secondary" onClick={() => setFormOpen(false)}>
                  {t("aiSettings.cancelButton")}
                </Button>
                <Button type="submit" variant="primary" disabled={connectMutation.isPending}>
                  {t("aiSettings.saveButton")}
                </Button>
              </div>
            </form>
          </>
        ) : null}
      </Card>

      <Pager page={page} totalPages={totalPages} onChange={setPage} />
    </div>
  );
}
