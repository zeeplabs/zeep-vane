import { useEffect, useRef, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { Card } from "../../components/ui/Card";
import { Field } from "../../components/ui/Field";
import { Seg } from "../../components/ui/Seg";
import { Button } from "../../components/ui/Button";
import { ApiError, resolveAssetUrl } from "../../lib/apiClient";
import { useCompanySettings, useUpdateCompanySettings, useUploadCompanyLogo } from "./hooks";
import type { TaxIDType } from "../../types/api";

// taxIDTypeOptions' first entry ("") means "unset" - tax_id_type is
// optional (TENANT-22); the empty option lets an owner clear a
// previously-set value back to none.
const taxIDTypeValues: Array<TaxIDType | ""> = ["", "cpf", "cnpj"];

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
  const { t } = useTranslation();
  const { data, isLoading } = useCompanySettings();
  const updateSettings = useUpdateCompanySettings();
  const uploadLogo = useUploadCompanyLogo();
  const fileInputRef = useRef<HTMLInputElement>(null);

  const [name, setName] = useState("");
  const [contactEmail, setContactEmail] = useState("");
  const [logoUrl, setLogoUrl] = useState<string | null>(null);
  const [legalName, setLegalName] = useState("");
  const [taxID, setTaxID] = useState("");
  const [taxIDType, setTaxIDType] = useState<TaxIDType | "">("");
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!data) return;
    setName(data.name);
    setContactEmail(data.contact_email);
    setLogoUrl(data.logo_url);
    setLegalName(data.legal_name ?? "");
    setTaxID(data.tax_id ?? "");
    setTaxIDType(data.tax_id_type ?? "");
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
      await updateSettings.mutateAsync({
        name,
        contact_email: contactEmail,
        // Fiscal fields are sent only when non-empty - an empty field means
        // "don't touch this on the backend" (db.TenantUpdate's own nil =
        // unchanged semantics), never "clear it to an empty string", which
        // would risk pairing a blank tax_id against a previously-saved
        // tax_id_type and tripping the length-mismatch rejection on an
        // unrelated name/e-mail save.
        ...(legalName ? { legal_name: legalName } : {}),
        ...(taxID ? { tax_id: taxID } : {}),
        ...(taxIDType ? { tax_id_type: taxIDType } : {}),
      });
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

      <Card elevation="elev-sm" className="flex flex-col gap-4 p-6">
        <div>
          <span className="text-sm font-medium text-text">{t("companyFiscal.title")}</span>
          <p className="m-0 text-xs text-neutral-400">{t("companyFiscal.subtitle")}</p>
        </div>

        <Field
          label={t("companyFiscal.legalName")}
          value={legalName}
          onChange={(e) => setLegalName(e.target.value)}
        />

        <div className="flex flex-col gap-1">
          <span className="text-sm font-medium text-text">{t("companyFiscal.taxIDType")}</span>
          <Seg
            aria-label={t("companyFiscal.taxIDType")}
            value={taxIDType}
            onChange={(value) => setTaxIDType(value as TaxIDType | "")}
            options={taxIDTypeValues.map((value) => ({
              value,
              label:
                value === ""
                  ? t("companyFiscal.taxIDTypeNone")
                  : value === "cpf"
                    ? t("companyFiscal.taxIDTypeCPF")
                    : t("companyFiscal.taxIDTypeCNPJ"),
            }))}
          />
        </div>

        <Field
          label={t("companyFiscal.taxID")}
          value={taxID}
          onChange={(e) => setTaxID(e.target.value)}
          disabled={taxIDType === ""}
          hint={t("companyFiscal.taxIDHint")}
        />
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
