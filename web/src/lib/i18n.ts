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
        admins: "Admins",
        pollerStatus: "Status do poller",
        settings: "Configurações",
        viewingAs: "Visualizando como",
        simulateSessionExpired: "Simular sessão expirada",
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
        simulateSessionExpired: "Simulate expired session",
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
