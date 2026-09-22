import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import { readStoredLanguage, toI18nLng } from "./language";
import ptBR from "../locales/pt-BR.json";
import en from "../locales/en.json";

// Locale files are keyed by full BCP-47 tag (not the bare "pt"/"en"
// language subtag) so a future regional variant - pt-PT most likely
// (language.ts's own header comment) - is an additive new resource
// bundle/file instead of a rename of this one.
const resources = {
  "pt-BR": { translation: ptBR },
  en: { translation: en },
};

if (!i18n.isInitialized) {
  i18n.use(initReactI18next).init({
    resources,
    lng: toI18nLng(readStoredLanguage()),
    fallbackLng: "pt-BR",
    interpolation: { escapeValue: false },
  });
}

export default i18n;
