import type { ReactNode } from "react";
import { Navigate } from "react-router-dom";
import { useAuth } from "../auth/AuthProvider";
import type { Role } from "../types/api";

export function RequireAuth({ children }: { children: ReactNode }) {
  const { status, needsTenantSelection } = useAuth();

  if (status === "loading") return null;
  if (status !== "authenticated") return <Navigate to="/login" replace />;
  // A user with >1 tenant_membership and no active tenant yet must pick
  // one before any tenant-scoped route renders - the backend has no role
  // to enforce (RequireRole below) until a tenant is active (T17,
  // TENANT-19/20/21).
  if (needsTenantSelection) return <Navigate to="/select-tenant" replace />;
  return <>{children}</>;
}

export function RequireRole({ roles, children }: { roles: Role[]; children: ReactNode }) {
  const { hasRole, status } = useAuth();

  if (status === "loading") return null;
  if (!hasRole(roles)) return <Navigate to="/" replace />;
  return <>{children}</>;
}
