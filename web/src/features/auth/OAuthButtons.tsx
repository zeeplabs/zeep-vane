import { toast } from "sonner";
import { useTranslation } from "react-i18next";

// Decorative-only (AUTHPG-04/05, spec.md Out of Scope): the mock shows
// Google/Microsoft social login, but no OAuth client/handler exists in the
// backend. Clicking shows a toast instead of silently doing nothing or
// wiring a login attempt against an endpoint that doesn't exist.
export function OAuthButtons() {
  const { t } = useTranslation();

  function handleClick() {
    toast.info(t("auth.oauth.comingSoon"));
  }

  return (
    <div className="mb-5 flex gap-3">
      <button
        type="button"
        onClick={handleClick}
        className="flex flex-1 items-center justify-between gap-2 rounded-lg bg-card-header-bg px-3.5 py-2.5 text-[13.5px] font-semibold text-text transition-colors hover:brightness-95"
      >
        {t("auth.oauth.google")}
        <svg width="16" height="16" viewBox="0 0 24 24" aria-hidden="true">
          <path
            fill="#4285F4"
            d="M23.52 12.27c0-.85-.08-1.67-.22-2.45H12v4.64h6.47a5.54 5.54 0 0 1-2.4 3.63v3h3.88c2.27-2.09 3.57-5.17 3.57-8.82z"
          />
          <path
            fill="#34A853"
            d="M12 24c3.24 0 5.96-1.07 7.95-2.91l-3.88-3c-1.08.72-2.45 1.15-4.07 1.15-3.13 0-5.78-2.11-6.73-4.96H1.27v3.12A11.99 11.99 0 0 0 12 24z"
          />
          <path
            fill="#FBBC05"
            d="M5.27 14.28A7.2 7.2 0 0 1 4.89 12c0-.79.14-1.56.38-2.28V6.6H1.27A11.99 11.99 0 0 0 0 12c0 1.94.47 3.77 1.27 5.4z"
          />
          <path
            fill="#EA4335"
            d="M12 4.75c1.76 0 3.34.6 4.58 1.79l3.44-3.44C17.95 1.19 15.24 0 12 0 7.31 0 3.26 2.69 1.27 6.6l4 3.12C6.22 6.86 8.87 4.75 12 4.75z"
          />
        </svg>
      </button>
      <button
        type="button"
        onClick={handleClick}
        className="flex flex-1 items-center justify-between gap-2 rounded-lg bg-card-header-bg px-3.5 py-2.5 text-[13.5px] font-semibold text-text transition-colors hover:brightness-95"
      >
        {t("auth.oauth.microsoft")}
        <svg width="15" height="15" viewBox="0 0 24 24" aria-hidden="true">
          <rect x="1" y="1" width="10" height="10" fill="#F25022" />
          <rect x="13" y="1" width="10" height="10" fill="#7FBA00" />
          <rect x="1" y="13" width="10" height="10" fill="#00A4EF" />
          <rect x="13" y="13" width="10" height="10" fill="#FFB900" />
        </svg>
      </button>
    </div>
  );
}
