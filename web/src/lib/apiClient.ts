export class ApiError extends Error {
  status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

type UnauthorizedHandler = () => void;

let unauthorizedHandler: UnauthorizedHandler | null = null;

export function setUnauthorizedHandler(fn: UnauthorizedHandler | null): void {
  unauthorizedHandler = fn;
}

/** Dispara o handler de 401 registrado. Exposto para testes/simulação manual
 * de expiração de sessão — não é chamado automaticamente por timeout. */
export function triggerUnauthorized(): void {
  unauthorizedHandler?.();
}

// baseUrl vazio resolve contra a própria origem (embutido no mesmo binário
// em produção, via internal/webui). Em dev, VITE_API_BASE_URL aponta pro
// backend Go rodando à parte (ver web/.env.development).
const baseUrl = import.meta.env.VITE_API_BASE_URL ?? "";

async function parseErrorMessage(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as { error?: string };
    return body.error ?? res.statusText;
  } catch {
    return res.statusText;
  }
}

export async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${baseUrl}${path}`, {
    ...init,
    credentials: "include",
    headers: {
      ...(init?.body ? { "Content-Type": "application/json" } : {}),
      ...init?.headers,
    },
  });

  if (res.status === 401) {
    triggerUnauthorized();
  }

  if (!res.ok) {
    throw new ApiError(res.status, await parseErrorMessage(res));
  }

  // Several endpoints (e.g. logout) respond 200/204 with no body at all.
  // res.text() never throws on an empty stream; only parse it as JSON
  // when there's actually something to parse.
  const text = await res.text();
  return (text ? JSON.parse(text) : undefined) as T;
}
