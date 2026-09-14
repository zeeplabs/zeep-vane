import { useState } from "react";
import { useAuth } from "../../auth/AuthProvider";
import { ServiceListPage } from "./ServiceListPage";
import { ServiceDetailDrawer } from "./ServiceDetailDrawer";
import { AddServiceDrawer } from "./AddServiceDrawer";

export function ServicesPage() {
  const { hasRole } = useAuth();
  const canManage = hasRole(["owner", "operator"]);
  const [selectedServiceId, setSelectedServiceId] = useState<string | null>(null);
  const [addDrawerOpen, setAddDrawerOpen] = useState(false);

  return (
    <div className="mx-auto flex w-full max-w-[1280px] flex-col gap-4">
      <ServiceListPage
        onSelectService={setSelectedServiceId}
        onAddService={() => setAddDrawerOpen(true)}
        canManage={canManage}
      />
      {selectedServiceId ? (
        <ServiceDetailDrawer serviceId={selectedServiceId} onClose={() => setSelectedServiceId(null)} />
      ) : null}
      <AddServiceDrawer open={addDrawerOpen} onOpenChange={setAddDrawerOpen} />
    </div>
  );
}
