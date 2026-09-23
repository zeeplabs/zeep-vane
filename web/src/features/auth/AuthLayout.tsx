import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { useBrandLogoUrl } from "../../lib/branding";
import vaneLogo from "../../assets/vane-logo.webp";

interface AuthLayoutProps {
  children: ReactNode;
}

// Shared brand panel for every public auth screen (login, bootstrap, signup,
// verify-email, password reset) - handoff-new-layout/Login Bootstrap.dc.html.
// Extracted once here instead of duplicated per page (AUTHPG-01/02/03): the
// network illustration, headline and uptime card never vary by page, only
// the form content (children) does.
export function AuthLayout({ children }: AuthLayoutProps) {
  const { t } = useTranslation();
  const logoUrl = useBrandLogoUrl();

  return (
    <div className="grid min-h-screen w-full grid-cols-1 bg-bg lg:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)]">
      <div
        className="relative hidden overflow-hidden lg:flex lg:flex-col"
        style={{
          background:
            "radial-gradient(circle at 18% 16%, rgba(122,92,230,0.38), transparent 55%), radial-gradient(circle at 84% 80%, rgba(214,57,145,0.14), transparent 50%), #241A38",
        }}
      >
        <div
          aria-hidden="true"
          className="absolute inset-0 opacity-60"
          style={{
            backgroundImage: "radial-gradient(rgba(255,255,255,0.05) 1px, transparent 1px)",
            backgroundSize: "26px 26px",
          }}
        />

        <svg
          aria-hidden="true"
          viewBox="0 0 600 900"
          preserveAspectRatio="xMidYMid slice"
          className="absolute inset-0 h-full w-full"
        >
          <defs>
            <radialGradient id="authNodeGlow" cx="50%" cy="50%" r="50%">
              <stop offset="0%" stopColor="#C7B8FF" stopOpacity="0.9" />
              <stop offset="100%" stopColor="#C7B8FF" stopOpacity="0" />
            </radialGradient>
            <linearGradient id="authLineGrad" x1="0" y1="0" x2="1" y2="1">
              <stop offset="0%" stopColor="#A78BFA" stopOpacity="0.5" />
              <stop offset="100%" stopColor="#A78BFA" stopOpacity="0.05" />
            </linearGradient>
          </defs>

          <g stroke="url(#authLineGrad)" strokeWidth="1.4" fill="none">
            <path d="M150,180 Q230,240 300,320" />
            <path d="M420,140 Q365,225 300,320" />
            <path d="M300,320 Q400,360 480,420" />
            <path d="M300,320 Q235,395 180,480" />
            <path d="M480,420 Q420,510 350,600" />
            <path d="M180,480 Q145,565 120,650" />
            <path d="M350,600 Q410,650 460,700" />
            <path d="M350,600 Q310,690 280,780" />
            <path d="M120,650 Q195,715 280,780" />
          </g>

          <circle cx="300" cy="320" r="10" fill="none" stroke="#C7B8FF" strokeWidth="1.5" opacity="0.6">
            <animate attributeName="r" values="10;34;10" dur="3.4s" repeatCount="indefinite" />
            <animate attributeName="opacity" values="0.6;0;0.6" dur="3.4s" repeatCount="indefinite" />
          </circle>
          <circle cx="350" cy="600" r="10" fill="none" stroke="#C7B8FF" strokeWidth="1.5" opacity="0.6">
            <animate attributeName="r" values="10;30;10" dur="3.4s" begin="1.2s" repeatCount="indefinite" />
            <animate attributeName="opacity" values="0.6;0;0.6" dur="3.4s" begin="1.2s" repeatCount="indefinite" />
          </circle>

          <g>
            <circle cx="150" cy="180" r="18" fill="url(#authNodeGlow)" />
            <circle cx="150" cy="180" r="4.5" fill="#E9E4FF" />
            <circle cx="420" cy="140" r="16" fill="url(#authNodeGlow)" />
            <circle cx="420" cy="140" r="4" fill="#E9E4FF" />
            <circle cx="300" cy="320" r="24" fill="url(#authNodeGlow)" />
            <circle cx="300" cy="320" r="6" fill="#FFFFFF" />
            <circle cx="480" cy="420" r="15" fill="url(#authNodeGlow)" />
            <circle cx="480" cy="420" r="4" fill="#E9E4FF" />
            <circle cx="180" cy="480" r="15" fill="url(#authNodeGlow)" />
            <circle cx="180" cy="480" r="4" fill="#E9E4FF" />
            <circle cx="350" cy="600" r="22" fill="url(#authNodeGlow)" />
            <circle cx="350" cy="600" r="5.5" fill="#FFFFFF" />
            <circle cx="120" cy="650" r="14" fill="url(#authNodeGlow)" />
            <circle cx="120" cy="650" r="4" fill="#E9E4FF" />
            <circle cx="460" cy="700" r="14" fill="url(#authNodeGlow)" />
            <circle cx="460" cy="700" r="4" fill="#E9E4FF" />
            <circle cx="280" cy="780" r="15" fill="url(#authNodeGlow)" />
            <circle cx="280" cy="780" r="4" fill="#E9E4FF" />
          </g>

          <circle cx="309" cy="311" r="4" fill="#3DD68C" stroke="#241A38" strokeWidth="2" />
          <circle cx="359" cy="591" r="4" fill="#3DD68C" stroke="#241A38" strokeWidth="2" />
          <circle cx="429" cy="131" r="4" fill="#F5A623" stroke="#241A38" strokeWidth="2" />
        </svg>

        <div className="absolute inset-x-11 top-16 z-10">
          <div className="mb-3.5 text-[11px] font-bold uppercase tracking-[0.14em] text-[#B7A8F0]">
            {t("authLayout.eyebrow")}
          </div>
          <h2 className="max-w-[360px] font-heading text-[28px] font-semibold leading-[1.35] tracking-[-0.01em] text-white">
            {t("authLayout.headline")}
          </h2>
        </div>

        <div className="absolute bottom-[104px] left-11 z-10 min-w-[220px] rounded-2xl border border-white/[0.14] bg-white/[0.07] px-[18px] py-3.5 shadow-[0_12px_30px_rgba(0,0,0,0.25)] backdrop-blur-xl">
          <div className="mb-1.5 flex items-center gap-2">
            <span className="h-2 w-2 rounded-full bg-[#3DD68C] shadow-[0_0_0_3px_rgba(61,214,140,0.25)]" />
            <span className="text-[13px] font-semibold text-white">{t("authLayout.statusTitle")}</span>
          </div>
          <div className="text-xs text-[#B7AEC9]">{t("authLayout.uptime")}</div>
        </div>

        <div className="absolute bottom-9 left-11 z-10 pointer-events-none">
          <img src={logoUrl ?? vaneLogo} alt={t("authLayout.companyLogoAlt")} className="h-9 w-auto object-contain" />
        </div>
      </div>

      <div className="flex w-full items-center justify-center bg-bg px-4 py-12">
        <div className="w-full max-w-[380px]">
          <div className="mb-8 flex flex-col gap-1 lg:hidden">
            <div className="flex items-center gap-2">
              {logoUrl ? (
                <>
                  <img src={logoUrl} alt="" className="h-5 w-5 object-contain" />
                  <span className="text-[15px] font-medium tracking-tight text-text">Vane</span>
                </>
              ) : (
                <img src={vaneLogo} alt="Vane" className="h-6 object-contain" />
              )}
            </div>
          </div>

          {children}
        </div>
      </div>
    </div>
  );
}
