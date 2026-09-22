import { formatDateTimeShort } from "../../lib/formatDate";

type Translator = (key: string, opts?: Record<string, unknown>) => string;

// formatRelativeTime is the one function here that can't just take a
// `locale` string - "5 minutes ago" (en) vs "há 5 minutos" (pt) put the
// count in a different position relative to the surrounding words, so the
// whole phrase has to come from a translated, pluralized string
// (relativeTime.minute_one/_other etc. in the locale JSON) rather than
// word-order-agnostic concatenation. `t` is injected instead of importing
// the public-status i18n instance directly, so this stays a pure,
// easily-unit-testable function.
export function formatRelativeTime(iso: string, t: Translator, now: number = Date.now()): string {
  const diffMs = now - new Date(iso).getTime();
  const minutes = Math.max(0, Math.round(diffMs / 60_000));
  if (minutes < 1) return t("publicStatus.now");
  if (minutes < 60) return t("publicStatus.relativeTime.minute", { count: minutes });
  const hours = Math.round(minutes / 60);
  if (hours < 24) return t("publicStatus.relativeTime.hour", { count: hours });
  const days = Math.round(hours / 24);
  return t("publicStatus.relativeTime.day", { count: days });
}

export function formatDateTime(iso: string, locale: string): string {
  return formatDateTimeShort(iso, locale);
}

// formatDuration's output ("5min"/"2h"/"2h5min") is a bare, locale-free
// unit abbreviation - the surrounding word ("duração"/"duration") lives in
// the composed publicStatus.resolvedAt translation key at the call site,
// since minute/hour abbreviations don't need translating themselves.
export function formatDuration(startIso: string, endIso: string): string {
  const ms = Math.max(0, new Date(endIso).getTime() - new Date(startIso).getTime());
  const totalMinutes = Math.round(ms / 60_000);
  const hours = Math.floor(totalMinutes / 60);
  const minutes = totalMinutes % 60;
  if (hours === 0) return `${minutes}min`;
  return minutes === 0 ? `${hours}h` : `${hours}h${minutes}min`;
}
