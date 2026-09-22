// vane:language - persisted UI language preference, per-browser (sibling
// of useThemeToggle's localStorage pattern, see useSidebarPin.ts). Kept
// dependency-free (no i18next/React import) so both i18n.ts (initial `lng`
// at init time) and useLanguage.ts (runtime changeLanguage) can read it
// without a circular import.
//
// Deliberately independent of the tenant's own `locale` column
// (internal/api/company_settings_handler.go, GET /api/company-settings) -
// that field has no write path today and is company-wide, not a per-user
// display preference; this selector drives only the admin's own browser.
export const LANGUAGE_STORAGE_KEY = "vane:language";

export type LanguageOption = "pt-BR" | "en-US";

const LNG_BY_OPTION: Record<LanguageOption, string> = {
  "pt-BR": "pt",
  "en-US": "en",
};

export function readStoredLanguage(): LanguageOption {
  try {
    return window.localStorage.getItem(LANGUAGE_STORAGE_KEY) === "en-US" ? "en-US" : "pt-BR";
  } catch {
    // localStorage unavailable (private mode, quota) - degrade to the
    // pt-BR default without throwing.
    return "pt-BR";
  }
}

export function toI18nLng(option: LanguageOption): string {
  return LNG_BY_OPTION[option];
}
