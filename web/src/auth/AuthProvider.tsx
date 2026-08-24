import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useReducer,
  useState,
  type ReactNode,
} from "react";
import { apiFetch, ApiError, setUnauthorizedHandler } from "../lib/apiClient";
import type { Role } from "../types/api";

type Status = "loading" | "authenticated" | "anonymous";

// Shape of GET /api/auth/me's real response body (AF-34) - flat, no
// wrapper. Deliberately narrower than mockData's full Admin (no `status`):
// the session only needs the caller's own identity, not the admin-list
// row shape. Full type migration out of mockData.ts happens in I9.
export interface AuthenticatedAdmin {
  id: string;
  email: string;
  role: Role;
}

interface State {
  admin: AuthenticatedAdmin | null;
  status: Status;
}

type Action =
  | { type: "BOOT_START" }
  | { type: "AUTHENTICATED"; admin: AuthenticatedAdmin }
  | { type: "ANONYMOUS" };

function reducer(state: State, action: Action): State {
  switch (action.type) {
    case "BOOT_START":
      return { ...state, status: "loading" };
    case "AUTHENTICATED":
      return { admin: action.admin, status: "authenticated" };
    case "ANONYMOUS":
      return { admin: null, status: "anonymous" };
    default:
      return state;
  }
}

export interface AuthContextValue {
  admin: AuthenticatedAdmin | null;
  status: Status;
  /** true once the boot-time check confirms no admin exists yet (SHD-19).
   * Stays false while that check is still in flight or failed - never
   * assume bootstrap is needed without a confirmed "false" from the
   * server. */
  needsBootstrap: boolean;
  login: (email: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  hasRole: (roles: Role[]) => boolean;
  sessionExpired: boolean;
  dismissSessionExpired: () => void;
  /** Dev-only: troca o papel visualizado sem novo login. Usado pelo seletor "Visualizando como" da sidebar. */
  setDevRole: (role: Role) => void;
  /** Dev-only: dispara o modal de sessão expirada manualmente, sem esperar um 401 real. */
  simulateSessionExpired: () => void;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(reducer, { admin: null, status: "loading" });
  const [sessionExpired, setSessionExpired] = useReducer(
    (_prev: boolean, next: boolean) => next,
    false
  );
  const [needsBootstrap, setNeedsBootstrap] = useState(false);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      dispatch({ type: "BOOT_START" });
      try {
        // skipUnauthorizedHandler: a 401 here just means "no session yet"
        // (anonymous visitor, including on /login itself) - never a
        // session that expired mid-use.
        const admin = await apiFetch<AuthenticatedAdmin>("/api/auth/me", { skipUnauthorizedHandler: true });
        if (!cancelled) dispatch({ type: "AUTHENTICATED", admin });
      } catch {
        if (!cancelled) dispatch({ type: "ANONYMOUS" });
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  // Runs in parallel with the /api/auth/me boot fetch above (SHD-19), not
  // chained after it: the bootstrap-status check is independent of whether
  // this visitor happens to have a session.
  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        // Public, unauthenticated endpoint (never 401s) - skipUnauthorizedHandler
        // guards it the same way the /api/auth/me boot probe is guarded.
        const { bootstrapped } = await apiFetch<{ bootstrapped: boolean }>("/api/bootstrap/status", {
          skipUnauthorizedHandler: true,
        });
        if (!cancelled) setNeedsBootstrap(!bootstrapped);
      } catch {
        // Fails closed: an unreachable/erroring check never traps a real
        // install behind a bootstrap redirect it can't get past.
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    setUnauthorizedHandler(() => {
      setSessionExpired(true);
    });
    return () => setUnauthorizedHandler(null);
  }, []);

  const login = useCallback(async (email: string, password: string) => {
    // O corpo de /api/auth/login traz só {token} - descartado
    // deliberadamente, nunca guardado em estado. A sessão real vem do
    // cookie httpOnly que o login também seta (AD-004); a identidade é
    // hidratada por /api/auth/me logo em seguida, mesmo caminho do boot.
    // skipUnauthorizedHandler: a wrong-credentials 401 here is a normal
    // login failure (LoginPage shows its own inline error), never a
    // session that expired.
    await apiFetch<{ token: string }>("/api/auth/login", {
      method: "POST",
      body: JSON.stringify({ email, password }),
      skipUnauthorizedHandler: true,
    });
    const admin = await apiFetch<AuthenticatedAdmin>("/api/auth/me");
    dispatch({ type: "AUTHENTICATED", admin });
  }, []);

  const logout = useCallback(async () => {
    try {
      await apiFetch("/api/auth/logout", { method: "POST" });
    } finally {
      dispatch({ type: "ANONYMOUS" });
    }
  }, []);

  const hasRole = useCallback(
    (roles: Role[]) => state.admin !== null && roles.includes(state.admin.role),
    [state.admin]
  );

  const dismissSessionExpired = useCallback(() => setSessionExpired(false), []);

  // Dev-only, dynamically imported: mockData must never end up in the
  // production bundle now that the app talks to a real backend (I6).
  const setDevRole = useCallback((role: Role) => {
    if (!import.meta.env.DEV) return;
    void import("../lib/mockData").then(({ admins }) => {
      const seed = admins.find((a) => a.role === role);
      if (!seed) return;
      dispatch({
        type: "AUTHENTICATED",
        admin: { id: seed.id, email: seed.email, role: seed.role },
      });
    });
  }, []);

  const simulateSessionExpired = useCallback(() => setSessionExpired(true), []);

  return (
    <AuthContext.Provider
      value={{
        admin: state.admin,
        status: state.status,
        needsBootstrap,
        login,
        logout,
        hasRole,
        sessionExpired,
        dismissSessionExpired,
        setDevRole,
        simulateSessionExpired,
      }}
    >
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth deve ser usado dentro de <AuthProvider>");
  return ctx;
}

export { ApiError };
