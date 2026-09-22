import i18next from "i18next";
import ptBR from "../../locales/pt-BR.json";
import en from "../../locales/en.json";

// Own i18next instance, deliberately isolated from the admin SPA's
// (../../lib/i18n.ts, "vane:language") - same isolation principle
// usePublicStatusTheme already applies to the page's theme, independent
// of the admin's own vane:theme. An anonymous visitor to a public status
// page has no admin session and no language selector; this instance's
// language comes from the visitor's own browser (navigator.language),
// never from whatever an admin happens to have chosen for themselves last
// time they were logged in on this same device (e.g. previewing
// /status/:id). Reuses the same two locale JSON files as the admin
// instance - single source of truth for translated copy, no duplicated
// strings.
const publicStatusI18n = i18next.createInstance();

function detectLanguage(): "pt-BR" | "en" {
  const nav = typeof navigator !== "undefined" ? navigator.language : "";
  return nav.toLowerCase().startsWith("en") ? "en" : "pt-BR";
}

if (!publicStatusI18n.isInitialized) {
  publicStatusI18n.init({
    resources: {
      "pt-BR": { translation: ptBR },
      en: { translation: en },
    },
    lng: detectLanguage(),
    fallbackLng: "pt-BR",
    interpolation: { escapeValue: false },
  });
}

export default publicStatusI18n;
