// Locale-aware timestamp formatting, shared by every feature that used to
// duplicate its own `formatTimestamp(iso)` with the locale hardcoded to
// "pt-BR" - the date never changed format when the admin switched the UI
// language via SettingsPage's selector (useLanguage/i18n.language). Callers
// pass `i18n.language` explicitly (via useTranslation()'s `i18n`) rather
// than this module reading it itself, so it stays a pure function and easy
// to unit test.
export function formatDateTime(iso: string, locale: string): string {
  return new Date(iso).toLocaleString(locale);
}

export function formatDateTimeShort(iso: string, locale: string): string {
  return new Date(iso).toLocaleString(locale, {
    day: "2-digit",
    month: "short",
    hour: "2-digit",
    minute: "2-digit",
  });
}
