import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useReducer,
  type ReactNode,
} from "react";
import { apiFetch, ApiError, setUnauthorizedHandler } from "../lib/apiClient";
import { admins, type Admin, type Role } from "../lib/mockData";

type Status = "loading" | "authenticated" | "anonymous";

interface State {
  admin: Admin | null;
  status: Status;
}

type Action =
  | { type: "BOOT_START" }
  | { type: "AUTHENTICATED"; admin: Admin }
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
  admin: Admin | null;
  status: Status;
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

  useEffect(() => {
    let cancelled = false;
    (async () => {
      dispatch({ type: "BOOT_START" });
      try {
        const res = await apiFetch<{ admin: Admin }>("/api/auth/me");
        if (!cancelled) dispatch({ type: "AUTHENTICATED", admin: res.admin });
      } catch {
        if (!cancelled) dispatch({ type: "ANONYMOUS" });
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
    // A resposta mock pode incluir um "token" — descartado deliberadamente:
    // o estado de autenticação nunca guarda token, só {id,email,role}.
    const res = await apiFetch<{ admin: Admin; token?: string }>("/api/auth/login", {
      method: "POST",
      body: JSON.stringify({ email, password }),
    });
    dispatch({ type: "AUTHENTICATED", admin: res.admin });
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

  const setDevRole = useCallback((role: Role) => {
    const seed = admins.find((a) => a.role === role);
    if (!seed) return;
    dispatch({
      type: "AUTHENTICATED",
      admin: { id: seed.id, email: seed.email, role: seed.role, status: seed.status },
    });
  }, []);

  const simulateSessionExpired = useCallback(() => setSessionExpired(true), []);

  return (
    <AuthContext.Provider
      value={{
        admin: state.admin,
        status: state.status,
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
