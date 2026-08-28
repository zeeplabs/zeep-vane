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

// resolveAssetUrl prefixa uma URL relativa vinda do backend (ex.:
// logo_url = "/uploads/logo") com o mesmo baseUrl usado por apiFetch -
// necessário para <img src> etc., que o browser resolve contra a própria
// origem da página, não contra o backend. Em produção baseUrl é vazio (SPA
// e API na mesma origem) então isto é um no-op; em dev (front em :5173,
// back em :8080) sem isto a imagem 404 silenciosamente. URLs absolutas
// (http(s)://...) passam intactas.
export function resolveAssetUrl(url: string | null): string | null {
  if (!url || /^https?:\/\//.test(url)) return url;
  return `${baseUrl}${url}`;
}

async function parseErrorMessage(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as { error?: string };
    return body.error ?? res.statusText;
  } catch {
    return res.statusText;
  }
}

// skipUnauthorizedHandler evita o modal global de "sessão expirada" em
// chamadas cujo 401 é um resultado ESPERADO, não sinal de sessão que
// morreu no meio do uso: o probe de boot em /api/auth/me (visitante
// anônimo, inclusive na própria tela de login) e a tentativa de login em
// si (credencial errada). Sem isto, abrir /login sem sessão alguma já
// dispara o modal de sessão expirada, o que é sempre falso - nunca houve
// sessão para expirar.
interface ApiFetchInit extends RequestInit {
  skipUnauthorizedHandler?: boolean;
}

export async function apiFetch<T>(path: string, init?: ApiFetchInit): Promise<T> {
  const { skipUnauthorizedHandler, ...fetchInit } = init ?? {};
  // A FormData body (multipart upload, e.g. the company logo) must never
  // get an explicit Content-Type here - the browser sets its own
  // multipart/form-data header with the correct boundary. Forcing
  // application/json on it would break server-side multipart parsing.
  const isFormData = typeof FormData !== "undefined" && fetchInit.body instanceof FormData;
  const res = await fetch(`${baseUrl}${path}`, {
    ...fetchInit,
    credentials: "include",
    headers: {
      ...(fetchInit.body && !isFormData ? { "Content-Type": "application/json" } : {}),
      ...fetchInit.headers,
    },
  });

  if (res.status === 401 && !skipUnauthorizedHandler) {
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
