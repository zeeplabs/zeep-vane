import { Card } from "../../components/ui/Card";
import { useAuth } from "../../auth/AuthProvider";

const ROLE_LABEL: Record<string, string> = {
  owner: "Owner",
  operator: "Operator",
  viewer: "Viewer",
};

export function SettingsPage() {
  const { admin } = useAuth();

  return (
    <div className="flex flex-col gap-4">
      <h3 className="text-text">Configurações</h3>
      <Card elevation="elev-sm" className="max-w-md p-5">
        <dl className="flex flex-col gap-3 text-sm">
          <div className="flex justify-between">
            <dt className="text-neutral-400">E-mail</dt>
            <dd className="text-text">{admin?.email}</dd>
          </div>
          <div className="flex justify-between">
            <dt className="text-neutral-400">Papel</dt>
            <dd className="text-text">{admin ? ROLE_LABEL[admin.role] : "-"}</dd>
          </div>
        </dl>
      </Card>
    </div>
  );
}
