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

let browserCSRF = '';
let browserSession = false;
let refreshRequest: Promise<boolean> | null = null;
export const hasBrowserSession = () => browserSession;

export async function initializeBrowserSession(): Promise<boolean> {
  const res = await fetch('/api/v1/auth/session', { credentials: 'same-origin', cache: 'no-store' });
  if (res.status === 404) { browserSession = false; browserCSRF = ''; return false; } // Legacy Node local-account UI.
  if (!res.ok) throw new ApiError(res.status, 'SESSION_UNAVAILABLE', '暂时无法检查登录状态，请重试。');
  const data = await res.json();
  browserSession = data.authenticated === true;
  browserCSRF = browserSession ? data.csrf_token : '';
  if (browserSession || data.authentication === 'oidc') {
    clearTokens();
    localStorage.removeItem('taboo.idp_logout');
  }
  return browserSession;
}

export async function logout(): Promise<void> {
  if (browserSession) await request<void>('POST', '/api/v1/auth/logout');
  browserCSRF = '';
  browserSession = false;
  clearTokens();
  localStorage.removeItem('taboo.idp_logout');
  window.dispatchEvent(new Event('taboo:logout'));
}

async function refreshLocal(): Promise<boolean> {
  const refresh = localStorage.getItem(REFRESH_KEY);
  if (!refresh) return false;
  const res = await fetch('/api/v1/auth/refresh', {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ refresh }),
  });
  if (!res.ok) return false;
  const data = await res.json();
  setTokens(data.tokens);
  return true;
}

async function request<T>(method: string, path: string, body?: unknown, retried = false): Promise<T> {
  const token = browserSession ? null : getToken();
  const res = await fetch(path, {
    method, credentials: 'same-origin', cache: 'no-store',
    headers: {
      ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...(browserSession && method !== 'GET' ? { 'X-CSRF-Token': browserCSRF } : {}),
    },
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  if (res.status === 401 && token && !retried) {
    if (!refreshRequest) refreshRequest = refreshLocal().finally(() => { refreshRequest = null; });
    if (await refreshRequest) return request<T>(method, path, body, true);
  }
  if (res.status === 401 && (token || browserSession)) {
    browserSession = false;
    browserCSRF = '';
    clearTokens();
    window.dispatchEvent(new Event('taboo:logout'));
  }
  if (res.status === 204) return undefined as T;
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new ApiError(res.status, data.code ?? 'ERROR', data.message ?? res.statusText);
  return data as T;
}

export const api = {
  get: <T,>(path: string) => request<T>('GET', path),
  post: <T,>(path: string, body?: unknown) => request<T>('POST', path, body),
  patch: <T,>(path: string, body?: unknown) => request<T>('PATCH', path, body),
  put: <T,>(path: string, body?: unknown) => request<T>('PUT', path, body),
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
