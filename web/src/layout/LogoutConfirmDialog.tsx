import { useTranslation } from "react-i18next";
import { Dialog } from "../components/ui/Dialog";
import { Button } from "../components/ui/Button";

export interface LogoutConfirmDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: () => void;
}

// Extracted from Sidebar.tsx's inline modal (new-layout-migration, T8) so
// AvatarMenu (T10) can reuse the exact same copy/behavior instead of
// duplicating the JSX (design.md's Risks & Concerns).
export function LogoutConfirmDialog({ open, onOpenChange, onConfirm }: LogoutConfirmDialogProps) {
  const { t } = useTranslation();

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={t("logoutDialog.title")}
      description={t("logoutDialog.body")}
      footer={
        <>
          <Button variant="secondary" onClick={() => onOpenChange(false)}>
            {t("logoutDialog.cancel")}
          </Button>
          <Button variant="primary" onClick={onConfirm}>
            {t("logoutDialog.confirm")}
          </Button>
        </>
      }
    />
  );
}
