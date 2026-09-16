const TOKEN_KEY = 'taboo.access';
const REFRESH_KEY = 'taboo.refresh';

export const getToken = () => localStorage.getItem(TOKEN_KEY);
export const setTokens = (t: { access: string; refresh: string }) => {
  localStorage.setItem(TOKEN_KEY, t.access);
  localStorage.setItem(REFRESH_KEY, t.refresh);
};
export const clearTokens = () => {
  localStorage.removeItem(TOKEN_KEY);
  localStorage.removeItem(REFRESH_KEY);
};

export class ApiError extends Error {
  code: string;
  status: number;
  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: {
      ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
      ...(getToken() ? { Authorization: `Bearer ${getToken()}` } : {}),
    },
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  if (res.status === 401 && getToken()) {
    // 尝试 refresh 一次
    const refresh = localStorage.getItem(REFRESH_KEY);
    if (refresh) {
      const r = await fetch('/api/v1/auth/refresh', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ refresh }),
      });
      if (r.ok) {
        const data = await r.json();
        setTokens(data.tokens);
        return request<T>(method, path, body);
      }
    }
    clearTokens();
    window.dispatchEvent(new Event('taboo:logout'));
  }
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new ApiError(res.status, data.code ?? 'ERROR', data.message ?? res.statusText);
  return data as T;
}

export const api = {
  get: <T,>(path: string) => request<T>('GET', path),
  post: <T,>(path: string, body?: unknown) => request<T>('POST', path, body),
};

export interface User { id: string; email: string; name: string }
export interface Org { id: string; name: string; slug: string; role: string }
export interface Project { id: string; name: string; slug: string; created_at: string }
export interface Env { id: string; name: string; slug: string; sort_order: number }
export interface SecretMeta {
  id: string; folder: string; key: string; comment: string;
  tags: string[]; version: number; updated_at: string; canReveal: boolean;
}
export interface SecretValue extends SecretMeta { value: string }
export interface Version { version: number; created_by: string; created_at: string }
export interface AuditLog {
  id: string; actor_name: string; action: string; resource: string;
  metadata: string; ip: string; created_at: string;
}
export interface IdentityScope {
  project_id: string; project_slug: string; env: string; permission: string;
}
export interface Identity {
  id: string; name: string; client_id: string; status: string;
  token_ttl: number; created_at: string; scopes: IdentityScope[];
}
export interface CreatedIdentity extends Identity { client_secret: string }
