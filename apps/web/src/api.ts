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
  del: <T,>(path: string) => request<T>('DELETE', path),
};

// 实体类型由 api/openapi.yaml 生成（npm run types），此处仅做别名映射保持导入路径不变
import type { components } from './api-types';

type Schemas = components['schemas'];

export type User = Schemas['User'];
export type Org = Schemas['OrgMembership'];
export type Project = Schemas['Project'];
export type Env = Schemas['Environment'];
export type Folder = Schemas['Folder'];
export type SecretMeta = Schemas['SecretMeta'];
export type SecretValue = Schemas['SecretValue'];
export type Version = Schemas['SecretVersion'];
export type AuditLog = Schemas['AuditLog'];
export type IdentityScope = Schemas['IdentityScope'];
export type Identity = Schemas['Identity'];
export type CreatedIdentity = Schemas['IdentityWithSecret'];
export type DynamicEngine = Schemas['DynamicEngine'];
export type DynamicLease = Schemas['DynamicLease'];
export type LeaseCredentials = Schemas['LeaseCredentials'];
export type SyncTarget = Schemas['SyncTarget'];
export type SyncTargetInput = Schemas['SyncTargetInput'];
export type SyncRun = Schemas['SyncRun'];
export type Webhook = Schemas['Webhook'];
export type OIDCProvider = Schemas['OIDCProvider'];
export type OIDCProviderInput = Schemas['OIDCProviderInput'];
export type AuditExport = Schemas['AuditExport'];
