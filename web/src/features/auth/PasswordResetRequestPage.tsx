import { useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { Field } from "../../components/ui/Field";
import { Button } from "../../components/ui/Button";
import { useBrandLogoUrl } from "../../lib/branding";

export function PasswordResetRequestPage() {
  const logoUrl = useBrandLogoUrl();

  const [email, setEmail] = useState("");
  const [submitted, setSubmitted] = useState(false);

  function handleSubmit(e: FormEvent) {
    e.preventDefault();
    // Confirmação sempre genérica, mesmo que o e-mail não exista — assim
    // evitamos vazar quais e-mails têm conta cadastrada.
    setSubmitted(true);
  }

  return (
    <div className="grid min-h-screen w-full grid-cols-1 bg-bg lg:grid-cols-[minmax(0,1.1fr)_minmax(0,1fr)]">
      <div className="relative hidden overflow-hidden border-r border-divider lg:flex lg:flex-col lg:justify-between lg:p-12">
        <div
          aria-hidden="true"
          className="pointer-events-none absolute -left-40 -top-40 h-[520px] w-[520px] rounded-full opacity-40 blur-3xl"
          style={{
            background:
              "radial-gradient(circle, var(--color-accent) 0%, var(--color-accent-2) 45%, transparent 70%)",
          }}
        />
        <div
          aria-hidden="true"
          className="pointer-events-none absolute -bottom-56 -right-24 h-[420px] w-[420px] rounded-full opacity-25 blur-3xl"
          style={{ background: "radial-gradient(circle, var(--color-accent-2) 0%, transparent 70%)" }}
        />

        <div className="relative flex items-center gap-2">
          {logoUrl ? (
            <img src={logoUrl} alt="Company logo" className="w-[180px] object-contain" />
          ) : (
            <svg width="22" height="22" viewBox="0 0 24 24" fill="none" aria-hidden="true">
              <path
                d="M12 2l2.4 7.2L22 12l-7.6 2.8L12 22l-2.4-7.2L2 12l7.6-2.8L12 2z"
                fill="var(--color-accent)"
              />
            </svg>
          )}
        </div>

        <div className="relative flex flex-col gap-4">
          <h1 className="max-w-md text-[32px] font-medium leading-tight text-text">
            Status e incidentes, sob controle.
          </h1>
          <p className="max-w-sm text-[14.5px] leading-relaxed text-neutral-300">
            Monitore integrações, comunique incidentes e mantenha suas status pages sempre
            atualizadas — tudo em um painel só.
          </p>
        </div>

        <p className="relative text-xs text-neutral-500">© {new Date().getFullYear()} Vane. Todos os direitos reservados.</p>
      </div>

      <div
        className="flex w-full items-center justify-center px-4 py-12"
        style={{ background: "color-mix(in srgb, var(--color-bg) 80%, black)" }}
      >
        <div className="w-full max-w-[380px]">
          <div className="mb-8 flex flex-col gap-1 lg:hidden">
            <div className="flex items-center gap-2">
              <svg width="20" height="20" viewBox="0 0 24 24" fill="none" aria-hidden="true">
                <path
                  d="M12 2l2.4 7.2L22 12l-7.6 2.8L12 22l-2.4-7.2L2 12l7.6-2.8L12 2z"
                  fill="var(--color-accent)"
                />
              </svg>
              <span className="text-[15px] font-medium tracking-tight text-text">Vane</span>
            </div>
          </div>

          <div className="mb-7">
            <h3 className="text-text">Recuperar senha</h3>
            {!submitted ? (
              <p className="mt-1 text-[13.5px] text-neutral-400">
                Informe seu e-mail e enviaremos instruções para redefinir sua senha.
              </p>
            ) : null}
          </div>

          {submitted ? (
            <p className="text-sm text-neutral-300">
              Se este e-mail estiver cadastrado, você receberá instruções para redefinir sua senha
              em instantes.
            </p>
          ) : (
            <form onSubmit={handleSubmit} className="flex flex-col gap-4">
              <Field
                label="E-mail"
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                required
              />
              <Button type="submit" variant="primary" className="w-full">
                Enviar instruções
              </Button>
            </form>
          )}
          <Link to="/login" className="mt-4 inline-block text-[12.5px] text-accent hover:underline">
            Voltar para o login
          </Link>
        </div>
      </div>
    </div>
  );
}
