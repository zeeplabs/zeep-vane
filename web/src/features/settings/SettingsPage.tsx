import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { MdCheck, MdOutlineFileUpload, MdOutlineImage } from "react-icons/md";
import { Card } from "../../components/ui/Card";
import { Field } from "../../components/ui/Field";
import { Button } from "../../components/ui/Button";
import { Dialog } from "../../components/ui/Dialog";
import { Skeleton } from "../../components/ui/Skeleton";
import { ApiError, resolveAssetUrl } from "../../lib/apiClient";
import { useAuth } from "../../auth/AuthProvider";
import { useCompanySettings, useDeleteTenant, useUpdateCompanySettings, useUploadCompanyLogo } from "./hooks";
import { useLanguage } from "../../lib/useLanguage";
import type { LanguageOption } from "../../lib/language";
import type { TaxIDType, TenantBillingAddress } from "../../types/api";

type PersonType = "pj" | "pf";

const timezoneOptions = ["America/Sao_Paulo (GMT-3)", "America/New_York (GMT-5)", "UTC (GMT+0)"];

const emptyAddress: TenantBillingAddress = {
  zip: "",
  street: "",
  number: "",
  complement: "",
  state: "",
  city: "",
  country: "",
};

function UploadIcon() {
  return <MdOutlineFileUpload size={14} aria-hidden="true" />;
}

function ImagePlaceholderIcon() {
  return <MdOutlineImage size={32} aria-hidden="true" />;
}

export function SettingsPage() {
  const { t } = useTranslation();
  const { data, isLoading } = useCompanySettings();
  const updateSettings = useUpdateCompanySettings();
  const uploadLogo = useUploadCompanyLogo();
  const deleteTenant = useDeleteTenant();
  const { logout, deploymentMode } = useAuth();
  const fileInputRef = useRef<HTMLInputElement>(null);

  const [name, setName] = useState("");
  const [website, setWebsite] = useState("");
  const [timezone, setTimezone] = useState(timezoneOptions[0]);
  const { language, setLanguage } = useLanguage();
  const [logoUrl, setLogoUrl] = useState<string | null>(null);

  const [personType, setPersonType] = useState<PersonType>("pj");
  const [legalName, setLegalName] = useState("");
  const [taxId, setTaxId] = useState("");
  const [stateRegistration, setStateRegistration] = useState("");
  const [address, setAddress] = useState<TenantBillingAddress>(emptyAddress);

  const [showSavedToast, setShowSavedToast] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  function applyPersisted() {
    if (!data) return;
    setName(data.name);
    setWebsite(data.website ?? "");
    setTimezone(data.timezone ?? timezoneOptions[0]);
    setLogoUrl(data.logo_url);
    setLegalName(data.legal_name ?? "");
    setTaxId(data.tax_id ?? "");
    setPersonType(data.tax_id_type === "cpf" ? "pf" : "pj");
    setStateRegistration("");
    setAddress(data.billing_address ?? emptyAddress);
  }

  useEffect(applyPersisted, [data]);

  async function handleLogoChange(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    if (!file) return;
    setError(null);
    try {
      const updated = await uploadLogo.mutateAsync(file);
      setLogoUrl(updated.logo_url);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : t("settingsPage.errors.save"));
    }
  }

  async function handleSave() {
    setError(null);
    const taxIdType: TaxIDType = personType === "pj" ? "cnpj" : "cpf";
    try {
      await updateSettings.mutateAsync({
        name,
        contact_email: data?.contact_email ?? "",
        website,
        timezone,
        legal_name: legalName || undefined,
        tax_id: taxId || undefined,
        tax_id_type: taxId ? taxIdType : undefined,
        billing_address: address,
      });
      setShowSavedToast(true);
      setTimeout(() => setShowSavedToast(false), 2200);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : t("settingsPage.errors.save"));
    }
  }

  function handleDiscard() {
    applyPersisted();
    setError(null);
  }

  async function handleConfirmDelete() {
    setDeleteError(null);
    try {
      await deleteTenant.mutateAsync();
      setDeleteDialogOpen(false);
      // The active tenant no longer resolves a role for this session
      // (settings-page CFGPG-09) - logging out sends the user back through
      // the normal login flow, same as any other session-ending action.
      await logout();
    } catch (err) {
      setDeleteError(err instanceof ApiError ? err.message : t("settingsPage.errors.delete"));
    }
  }

  function updateAddress<K extends keyof TenantBillingAddress>(key: K, value: string) {
    setAddress((prev) => ({ ...prev, [key]: value }));
  }

  if (isLoading) {
    return (
      <div aria-busy="true" className="mx-auto flex w-full max-w-[1200px] flex-col gap-4">
        <span className="sr-only">{t("settingsPage.loading")}</span>
        <Skeleton width={220} height={22} />
        <Card elevation="none" className="border border-divider p-6">
          <div className="mb-5 flex items-center gap-4">
            <Skeleton width={64} height={64} radius={999} />
            <Skeleton width={160} height={14} />
          </div>
          <div className="mb-4 grid grid-cols-2 gap-4">
            <Skeleton width="100%" height={36} />
            <Skeleton width="100%" height={36} />
          </div>
          <div className="grid grid-cols-2 gap-4">
            <Skeleton width="100%" height={36} />
            <Skeleton width="100%" height={36} />
          </div>
        </Card>
        <Card elevation="none" className="border border-divider p-6">
          <div className="mb-4 grid grid-cols-2 gap-4">
            <Skeleton width="100%" height={36} />
            <Skeleton width="100%" height={36} />
          </div>
          <div className="grid grid-cols-2 gap-4">
            <Skeleton width="100%" height={36} />
            <Skeleton width="100%" height={36} />
          </div>
        </Card>
      </div>
    );
  }

  return (
    <div className="mx-auto flex w-full max-w-[1200px] flex-col gap-4">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="m-0 mb-1.5 text-xl font-bold tracking-tight text-text">{t("settingsPage.title")}</h1>
          <p className="m-0 max-w-[560px] text-[13.5px] leading-normal text-neutral-400">{t("settingsPage.subtitle")}</p>
        </div>
        {showSavedToast ? (
          <span className="inline-flex items-center gap-1.5 rounded-full bg-[color-mix(in_oklch,var(--color-success)_16%,transparent)] px-3.5 py-1.5 text-[12.5px] font-bold text-success">
            <MdCheck size={14} aria-hidden="true" />
            {t("settingsPage.saved")}
          </span>
        ) : null}
      </div>

      <Card elevation="none" className="border border-divider p-6">
        <div className="mb-1 text-sm font-bold text-text">{t("settingsPage.companyProfile.title")}</div>
        <p className="m-0 mb-5 text-xs text-neutral-400">{t("settingsPage.companyProfile.subtitle")}</p>

        <div className="mb-5 flex items-center gap-4">
          <div className="flex h-16 w-16 flex-shrink-0 items-center justify-center overflow-hidden rounded-full border border-divider bg-bg text-neutral-500">
            {logoUrl ? (
              <img src={resolveAssetUrl(logoUrl)!} alt="Logo da empresa" className="h-full w-full object-contain" />
            ) : (
              <ImagePlaceholderIcon />
            )}
          </div>
          <div className="flex flex-col gap-2">
            <input
              ref={fileInputRef}
              type="file"
              accept="image/png,image/svg+xml"
              className="hidden"
              onChange={handleLogoChange}
            />
            <Button type="button" variant="secondary" className="w-fit" onClick={() => fileInputRef.current?.click()}>
              <UploadIcon />
              {t("settingsPage.companyProfile.uploadLogo")}
            </Button>
            <p className="m-0 text-xs text-neutral-400">{t("settingsPage.companyProfile.logoHint")}</p>
          </div>
        </div>

        <div className="mb-4 grid grid-cols-2 gap-4">
          <Field
            variant="filled"
            label={t("settingsPage.companyProfile.name")}
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
          />
          <Field
            variant="filled"
            label={t("settingsPage.companyProfile.website")}
            value={website}
            onChange={(e) => setWebsite(e.target.value)}
            placeholder="https://acme.health"
          />
        </div>

        <div className="grid grid-cols-2 gap-4">
          <div className="flex flex-col gap-1">
            <label htmlFor="settings-timezone" className="text-sm font-medium text-text">
              {t("settingsPage.companyProfile.timezone")}
            </label>
            <select
              id="settings-timezone"
              value={timezone}
              onChange={(e) => setTimezone(e.target.value)}
              className="min-h-9 rounded-md border border-transparent bg-card-header-bg px-3 text-sm text-text outline-none transition-colors focus:border-accent focus:bg-surface"
            >
              {timezoneOptions.map((opt) => (
                <option key={opt} value={opt}>
                  {opt}
                </option>
              ))}
            </select>
          </div>
          <div className="flex flex-col gap-1">
            <label htmlFor="settings-language" className="text-sm font-medium text-text">
              {t("settingsPage.companyProfile.language")}
            </label>
            <select
              id="settings-language"
              value={language}
              onChange={(e) => setLanguage(e.target.value as LanguageOption)}
              className="min-h-9 rounded-md border border-transparent bg-card-header-bg px-3 text-sm text-text outline-none transition-colors focus:border-accent focus:bg-surface"
            >
              <option value="pt-BR">Português (Brasil)</option>
              <option value="en-US">English (US)</option>
            </select>
          </div>
        </div>
      </Card>

      <Card elevation="none" className="border border-divider p-6">
        <div className="mb-1 text-sm font-bold text-text">{t("settingsPage.fiscal.title")}</div>
        <p className="m-0 mb-5 text-xs text-neutral-400">{t("settingsPage.fiscal.subtitle")}</p>

        <div className="mb-1 text-xs font-medium text-neutral-400">{t("settingsPage.fiscal.personType")}</div>
        <div role="radiogroup" aria-label={t("settingsPage.fiscal.personType")} className="mb-4 grid max-w-[340px] grid-cols-2 gap-2">
          {(["pj", "pf"] as PersonType[]).map((value) => {
            const active = personType === value;
            return (
              <button
                key={value}
                type="button"
                role="radio"
                aria-checked={active}
                onClick={() => setPersonType(value)}
                className="cursor-pointer rounded-md border-[1.5px] px-3 py-2.5 text-center text-[13px] font-semibold transition-colors"
                style={
                  active
                    ? {
                        borderColor: "var(--color-accent)",
                        backgroundColor: "color-mix(in oklch, var(--color-accent) 6%, transparent)",
                        color: "var(--color-text)",
                      }
                    : {
                        borderColor: "var(--color-divider)",
                        backgroundColor: "var(--color-surface)",
                        color: "var(--color-text-muted)",
                      }
                }
              >
                {value === "pj" ? t("settingsPage.fiscal.personTypePJ") : t("settingsPage.fiscal.personTypePF")}
              </button>
            );
          })}
        </div>

        <div className="mb-4 grid grid-cols-2 gap-4">
          <Field
            variant="filled"
            label={personType === "pj" ? t("settingsPage.fiscal.legalNamePJ") : t("settingsPage.fiscal.legalNamePF")}
            value={legalName}
            onChange={(e) => setLegalName(e.target.value)}
          />
          <Field
            variant="filled"
            label={personType === "pj" ? t("settingsPage.fiscal.taxIdPJ") : t("settingsPage.fiscal.taxIdPF")}
            value={taxId}
            onChange={(e) => setTaxId(e.target.value)}
          />
        </div>

        {personType === "pj" ? (
          <div className="mb-4 max-w-[340px]">
            <Field
              variant="filled"
              label={t("settingsPage.fiscal.stateRegistration")}
              value={stateRegistration}
              onChange={(e) => setStateRegistration(e.target.value)}
              placeholder={t("settingsPage.fiscal.stateRegistrationPlaceholder")}
            />
          </div>
        ) : null}

        <div className="my-4 h-px bg-divider" />

        <div className="mb-4 grid grid-cols-[160px_1fr] gap-4">
          <Field
            variant="filled"
            label={t("settingsPage.fiscal.zip")}
            value={address.zip}
            onChange={(e) => updateAddress("zip", e.target.value)}
          />
          <Field
            variant="filled"
            label={t("settingsPage.fiscal.street")}
            value={address.street}
            onChange={(e) => updateAddress("street", e.target.value)}
          />
        </div>

        <div className="mb-4 grid grid-cols-[160px_1fr_160px] gap-4">
          <Field
            variant="filled"
            label={t("settingsPage.fiscal.number")}
            value={address.number}
            onChange={(e) => updateAddress("number", e.target.value)}
          />
          <Field
            variant="filled"
            label={t("settingsPage.fiscal.complement")}
            value={address.complement}
            onChange={(e) => updateAddress("complement", e.target.value)}
            placeholder={t("settingsPage.fiscal.complementPlaceholder")}
          />
          <Field
            variant="filled"
            label={t("settingsPage.fiscal.state")}
            value={address.state}
            onChange={(e) => updateAddress("state", e.target.value)}
            placeholder="UF"
          />
        </div>

        <div className="grid grid-cols-2 gap-4">
          <Field
            variant="filled"
            label={t("settingsPage.fiscal.city")}
            value={address.city}
            onChange={(e) => updateAddress("city", e.target.value)}
          />
          <Field
            variant="filled"
            label={t("settingsPage.fiscal.country")}
            value={address.country}
            onChange={(e) => updateAddress("country", e.target.value)}
          />
        </div>
      </Card>

      {error ? (
        <p role="alert" className="text-xs text-critical">
          {error}
        </p>
      ) : null}

      <div className="mb-2 flex justify-end gap-2.5">
        <Button type="button" variant="secondary" onClick={handleDiscard}>
          {t("settingsPage.discard")}
        </Button>
        <Button type="button" variant="solid" onClick={handleSave} disabled={updateSettings.isPending}>
          {t("settingsPage.save")}
        </Button>
      </div>

      {deploymentMode === "saas" ? (
        <>
          <div
            className="flex items-center justify-between gap-4 rounded-md border p-5"
            style={{ borderColor: "color-mix(in oklch, var(--color-critical) 35%, transparent)" }}
          >
            <div>
              <div className="mb-1 text-[13.5px] font-bold text-critical">{t("settingsPage.dangerZone.title")}</div>
              <p className="m-0 max-w-[480px] text-xs leading-normal text-neutral-400">
                {t("settingsPage.dangerZone.description")}
              </p>
            </div>
            <button
              type="button"
              onClick={() => setDeleteDialogOpen(true)}
              className="flex-shrink-0 cursor-pointer rounded-md border-[1.5px] bg-surface px-4 py-2.5 text-[13px] font-bold text-critical"
              style={{ borderColor: "color-mix(in oklch, var(--color-critical) 45%, transparent)" }}
            >
              {t("settingsPage.dangerZone.button")}
            </button>
          </div>

          <Dialog
            open={deleteDialogOpen}
            onOpenChange={(open) => {
              setDeleteDialogOpen(open);
              if (!open) setDeleteError(null);
            }}
            title={t("settingsPage.deleteDialog.title")}
            description={t("settingsPage.deleteDialog.body")}
            footer={
              <>
                <Button type="button" variant="secondary" onClick={() => setDeleteDialogOpen(false)}>
                  {t("settingsPage.deleteDialog.cancel")}
                </Button>
                <Button
                  type="button"
                  variant="solid"
                  className="!border-critical !bg-critical hover:!bg-critical"
                  onClick={handleConfirmDelete}
                  disabled={deleteTenant.isPending}
                >
                  {t("settingsPage.deleteDialog.confirm")}
                </Button>
              </>
            }
          >
            {deleteError ? (
              <p role="alert" className="text-xs text-critical">
                {deleteError}
              </p>
            ) : null}
          </Dialog>
        </>
      ) : null}
    </div>
  );
}
