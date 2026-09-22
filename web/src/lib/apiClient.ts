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

/** Fires the registered 401 handler. Exposed for tests/manual simulation
 * of session expiry — never called automatically on a timeout. */
export function triggerUnauthorized(): void {
  unauthorizedHandler?.();
}

// An empty baseUrl resolves against the page's own origin (embedded in the
// same binary in production, via internal/webui). In dev, VITE_API_BASE_URL
// points at the separately-running Go backend (see web/.env.development).
const baseUrl = import.meta.env.VITE_API_BASE_URL ?? "";

// resolveAssetUrl prefixes a relative URL coming from the backend (e.g.
// logo_url = "/uploads/logo") with the same baseUrl used by apiFetch -
// needed for <img src> etc., which the browser resolves against the page's
// own origin, not the backend. In production baseUrl is empty (SPA and
// API share an origin) so this is a no-op; in dev (frontend on :5173,
// backend on :8080) without this the image silently 404s. Absolute URLs
// (http(s)://...) pass through untouched.
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

// skipUnauthorizedHandler avoids the global "session expired" modal for
// calls whose 401 is an EXPECTED outcome, not a sign that a session died
// mid-use: the boot probe on /api/auth/me (anonymous visitor, including on
// the login screen itself) and the login attempt itself (wrong
// credentials). Without this, opening /login with no session at all
// already triggers the session-expired modal, which is always false -
// there was never a session to expire.
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
