import i18n from "i18next";
import { initReactI18next } from "react-i18next";

const resources = {
  pt: {
    translation: {
      login: {
        title: "Entrar",
        email: "E-mail",
        password: "Senha",
        submit: "Entrar",
        forgotPassword: "Esqueci minha senha",
        invalidCredentials: "E-mail ou senha inválidos.",
      },
      sidebar: {
        brand: "Vane",
        domainsStatusPages: "Domínios & Status Pages",
        incidents: "Incidentes",
        integrations: "Integrações",
        services: "Serviços",
        admins: "Equipe",
        pollerStatus: "Status do poller",
        settings: "Configurações",
        viewingAs: "Visualizando como",
        logout: "Sair",
      },
      logoutDialog: {
        title: "Sair do painel",
        body: "Tem certeza que deseja encerrar sua sessão?",
        cancel: "Cancelar",
        confirm: "Sair",
      },
      sessionExpired: {
        title: "Sessão expirada",
        body: "Sua sessão expirou. Faça login novamente para continuar.",
        cta: "Ir para o login",
      },
      bootstrap: {
        title: "Crie a conta do primeiro administrador",
        subtitle: "Esta instância ainda não tem nenhum administrador. Crie a conta owner para começar.",
        email: "E-mail",
        password: "Senha",
        confirmPassword: "Confirmar senha",
        submit: "Criar administrador",
        passwordMismatch: "As senhas não coincidem.",
        alreadyBootstrapped: "Esta instância já tem um administrador.",
        goToLogin: "Ir para o login",
        genericError: "Não foi possível criar o administrador. Tente novamente.",
      },
      acceptInvite: {
        title: "Defina sua senha",
        subtitle: "Você foi convidado para o painel administrativo. Defina uma senha para ativar sua conta.",
        password: "Senha",
        confirmPassword: "Confirmar senha",
        submit: "Ativar conta",
        passwordMismatch: "As senhas não coincidem.",
        invalidOrExpired: "Este link de convite é inválido ou expirou. Peça ao seu administrador para enviar um novo.",
        genericError: "Não foi possível ativar a conta. Tente novamente.",
      },
    },
  },
  en: {
    translation: {
      login: {
        title: "Sign in",
        email: "Email",
        password: "Password",
        submit: "Sign in",
        forgotPassword: "Forgot my password",
        invalidCredentials: "Invalid email or password.",
      },
      sidebar: {
        brand: "Vane",
        domainsStatusPages: "Domains & Status Pages",
        incidents: "Incidents",
        integrations: "Integrations",
        services: "Services",
        admins: "Admins",
        pollerStatus: "Poller status",
        settings: "Settings",
        viewingAs: "Viewing as",
        logout: "Sign out",
      },
      logoutDialog: {
        title: "Sign out",
        body: "Are you sure you want to end your session?",
        cancel: "Cancel",
        confirm: "Sign out",
      },
      sessionExpired: {
        title: "Session expired",
        body: "Your session has expired. Please sign in again to continue.",
        cta: "Go to login",
      },
      bootstrap: {
        title: "Create the first administrator account",
        subtitle: "This instance has no administrator yet. Create the owner account to get started.",
        email: "Email",
        password: "Password",
        confirmPassword: "Confirm password",
        submit: "Create administrator",
        passwordMismatch: "Passwords do not match.",
        alreadyBootstrapped: "This instance already has an administrator.",
        goToLogin: "Go to login",
        genericError: "Could not create the administrator. Please try again.",
      },
      acceptInvite: {
        title: "Set your password",
        subtitle: "You've been invited to the admin dashboard. Set a password to activate your account.",
        password: "Password",
        confirmPassword: "Confirm password",
        submit: "Activate account",
        passwordMismatch: "Passwords do not match.",
        invalidOrExpired: "This invite link is invalid or has expired. Ask your admin to send a new one.",
        genericError: "Could not activate the account. Please try again.",
      },
    },
  },
};

if (!i18n.isInitialized) {
  i18n.use(initReactI18next).init({
    resources,
    lng: "pt",
    fallbackLng: "pt",
    interpolation: { escapeValue: false },
  });
}

export default i18n;
