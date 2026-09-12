// ProfilePage composes the Meu Perfil screen: Informações pessoais, Segurança
// (password + 2FA), Sessões ativas and Notificações (profile-page PROFPAGE-01;
// the Notificações section was added by notification-preferences). It owns no
// data: each card reads the authenticated identity from AuthProvider or its own
// hooks, so a failure in one card does not affect the others.

import { useTranslation } from "react-i18next";
import { PersonalInfoCard } from "./PersonalInfoCard";
import { SecurityCard } from "./SecurityCard";
import { SessionsSection } from "../sessions/SessionsSection";
import { NotificationsSection } from "../notifications/NotificationsSection";

export function ProfilePage() {
  const { t } = useTranslation();

  return (
    <div className="flex flex-col gap-6">
      <h1 className="text-text">{t("profile.title")}</h1>
      <PersonalInfoCard />
      <SecurityCard />
      <SessionsSection />
      <NotificationsSection />
    </div>
  );
}
