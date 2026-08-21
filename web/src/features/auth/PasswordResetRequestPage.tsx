import { useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { Card } from "../../components/ui/Card";
import { Field } from "../../components/ui/Field";
import { Button } from "../../components/ui/Button";

export function PasswordResetRequestPage() {
  const [email, setEmail] = useState("");
  const [submitted, setSubmitted] = useState(false);

  function handleSubmit(e: FormEvent) {
    e.preventDefault();
    // Confirmação sempre genérica, mesmo que o e-mail não exista — assim
    // evitamos vazar quais e-mails têm conta cadastrada.
    setSubmitted(true);
  }

  return (
    <div className="flex min-h-screen w-full items-center justify-center bg-bg px-4">
      <Card elevation="elev-md" className="w-full max-w-[380px] p-7">
        <h3 className="mb-2 text-text">Recuperar senha</h3>
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
      </Card>
    </div>
  );
}
