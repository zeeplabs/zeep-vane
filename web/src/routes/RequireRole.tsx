import type { ReactNode } from "react";
import { Navigate } from "react-router-dom";
import { useAuth } from "../auth/AuthProvider";
import type { Role } from "../types/api";

export function RequireAuth({ children }: { children: ReactNode }) {
  const { status } = useAuth();

  if (status === "loading") return null;
  if (status !== "authenticated") return <Navigate to="/login" replace />;
  return <>{children}</>;
}

export function RequireRole({ roles, children }: { roles: Role[]; children: ReactNode }) {
  const { hasRole, status } = useAuth();

  if (status === "loading") return null;
  if (!hasRole(roles)) return <Navigate to="/" replace />;
  return <>{children}</>;
}
