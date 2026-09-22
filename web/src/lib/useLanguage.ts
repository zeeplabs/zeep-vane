import { useCallback, useState } from "react";
import i18n from "./i18n";
import { LANGUAGE_STORAGE_KEY, readStoredLanguage, toI18nLng, type LanguageOption } from "./language";

/**
 * Current UI language + setter, synchronized with i18next and
 * `localStorage["vane:language"]` - mirrors useThemeToggle's shape/pattern.
 */
export function useLanguage(): { language: LanguageOption; setLanguage: (next: LanguageOption) => void } {
  const [language, setLanguageState] = useState<LanguageOption>(() => readStoredLanguage());

  const setLanguage = useCallback((next: LanguageOption) => {
    setLanguageState(next);
    void i18n.changeLanguage(toI18nLng(next));
    try {
      window.localStorage.setItem(LANGUAGE_STORAGE_KEY, next);
    } catch {
      // localStorage unavailable - language still updates in memory/UI.
    }
  }, []);

  return { language, setLanguage };
}
