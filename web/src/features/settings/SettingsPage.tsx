import { useEffect, useRef, useState, type FormEvent } from "react";
import { Card } from "../../components/ui/Card";
import { Field } from "../../components/ui/Field";
import { Button } from "../../components/ui/Button";
import { ApiError, resolveAssetUrl } from "../../lib/apiClient";
import { useCompanySettings, useUpdateCompanySettings, useUploadCompanyLogo } from "./hooks";

function UploadIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M12 15V3M7 8l5-5 5 5M4 17v3a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-3" />
    </svg>
  );
}

function ImagePlaceholderIcon() {
  return (
    <svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <rect x="3" y="4" width="18" height="16" rx="2" />
      <circle cx="9" cy="10" r="1.5" />
      <path d="M21 16l-5-5-4 4-3-3-6 6" />
    </svg>
  );
}

export function SettingsPage() {
  const { data, isLoading } = useCompanySettings();
  const updateSettings = useUpdateCompanySettings();
  const uploadLogo = useUploadCompanyLogo();
  const fileInputRef = useRef<HTMLInputElement>(null);

  const [name, setName] = useState("");
  const [contactEmail, setContactEmail] = useState("");
  const [logoUrl, setLogoUrl] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!data) return;
    setName(data.name);
    setContactEmail(data.contact_email);
    setLogoUrl(data.logo_url);
  }, [data]);

  // Uploads the logo immediately on selection (SET-07), independent of the
  // name/e-mail form's own submit below - the multipart upload endpoint is
  // separate from PATCH /api/company-settings.
  async function handleLogoChange(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    if (!file) return;
    setError(null);
    try {
      const updated = await uploadLogo.mutateAsync(file);
      setLogoUrl(updated.logo_url);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Não foi possível enviar a logo.");
    }
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    try {
      await updateSettings.mutateAsync({ name, contact_email: contactEmail });
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Não foi possível salvar as alterações.");
    }
  }

  if (isLoading) {
    return <p className="text-neutral-400">Carregando…</p>;
  }

  return (
    <form onSubmit={handleSubmit} className="mx-auto flex w-full max-w-[1280px] flex-col gap-4">
      <div>
        <h2 className="text-text">Configurações</h2>
        <p className="m-0 text-[13.5px] text-neutral-400">
          Dados da empresa exibidos no painel e nas status pages públicas. Visível apenas para Owners.
        </p>
      </div>

      <Card elevation="elev-sm" className="grid grid-cols-[280px_1fr] gap-6 p-6">
        <div className="flex flex-col gap-3 border-r border-divider pr-6">
          <span className="text-sm font-medium text-text">Logo da empresa</span>
          <div className="flex h-[260px] w-full items-center justify-center rounded-md border border-divider bg-bg text-neutral-500">
            {logoUrl ? (
              <img src={resolveAssetUrl(logoUrl)!} alt="Logo da empresa" className="max-h-full max-w-full object-contain" />
            ) : (
              <ImagePlaceholderIcon />
            )}
          </div>
          <input
            ref={fileInputRef}
            type="file"
            accept="image/png,image/svg+xml"
            className="hidden"
            onChange={handleLogoChange}
          />
          <Button type="button" variant="secondary" onClick={() => fileInputRef.current?.click()}>
            <UploadIcon />
            Enviar/alterar logo
          </Button>
          <p className="m-0 text-xs text-neutral-400">PNG ou SVG, fundo transparente.</p>
        </div>

        <div className="flex flex-col gap-4">
          <Field
            label="Nome da empresa"
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
          />
          <Field
            label="E-mail de contato"
            type="email"
            value={contactEmail}
            onChange={(e) => setContactEmail(e.target.value)}
            required
          />
        </div>
      </Card>

      {error ? (
        <p role="alert" className="text-xs text-critical">
          {error}
        </p>
      ) : null}

      <Button type="submit" variant="primary" className="w-fit" disabled={updateSettings.isPending}>
        Salvar alterações
      </Button>
    </form>
  );
}
