const TOKEN_KEY = 'taboo.access';
const REFRESH_KEY = 'taboo.refresh';
const EPOCH_KEY = 'taboo.session-event';
let generation = 0;
let sessionChanged = false;
export const invalidateRequests = () => { generation++; };
export function announceSession() { localStorage.setItem(EPOCH_KEY, crypto.randomUUID()); }
function blockChangedSession() {
  generation++; sessionChanged = true; browserInfo = null;
  window.dispatchEvent(new Event('taboo:session-changed'));
}
window.addEventListener('storage', (e) => { if (e.key === EPOCH_KEY) blockChangedSession(); });
export function sessionFingerprint() { return browserSession ? browserCSRF : getToken(); }
export async function checkSessionContinuity() {
  if (sessionChanged) return;
  const before = sessionFingerprint();
  if (browserSession) {
    await initializeBrowserSession();
    if (before !== sessionFingerprint()) blockChangedSession();
  }
}


export const getToken = () => localStorage.getItem(TOKEN_KEY);
export const setTokens = (t: { access: string; refresh: string }, notify = true) => {
  localStorage.setItem(TOKEN_KEY, t.access);
  localStorage.setItem(REFRESH_KEY, t.refresh);
  if (notify) announceSession();
};
export const clearTokens = () => {
  localStorage.removeItem(TOKEN_KEY);
  localStorage.removeItem(REFRESH_KEY);
};

export class ApiError extends Error {
  code: string;
  status: number;
  details?: unknown;
  constructor(status: number, code: string, message: string, details?: unknown) {
    super(message);
    this.status = status;
    this.code = code;
    this.details = details;
  }
}

export interface BrowserSessionInfo {
  revocation_profile: 'BOUNDED' | 'ONLINE_REQUIRED';
  sensitive_operations_allowed: boolean;
  expires_at: number;
  auth_valid_until: number;
  reauthentication_url: string;
  rp_logout: boolean;
}
let browserInfo: BrowserSessionInfo | null = null;
export const getBrowserSessionInfo = () => browserInfo;
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
  browserInfo = browserSession ? data : null;
  if (browserSession || data.authentication === 'oidc') {
    clearTokens();
    localStorage.removeItem('taboo.idp_logout');
  }
  return browserSession;
}

export async function logout(): Promise<void> {
  const result = browserSession ? await request<{logout_url?:string}>('POST', '/api/v1/auth/logout') : undefined;
  browserCSRF = '';
  browserSession = false;
  browserInfo = null;
  clearTokens();
  localStorage.removeItem('taboo.idp_logout');
  generation++; announceSession();
  window.dispatchEvent(new Event('taboo:logout'));
  if (result?.logout_url) window.location.assign(result.logout_url);
}

async function refreshLocal(): Promise<boolean> {
  const started = generation;
  const refresh = localStorage.getItem(REFRESH_KEY);
  if (!refresh) return false;
  const res = await fetch('/api/v1/auth/refresh', {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ refresh }),
  });
  if (!res.ok) return false;
  const data = await res.json();
  if (started !== generation || sessionChanged) return false;
  setTokens(data.tokens, false);
  return true;
}

export type RequestOptions = {idempotencyKey?:string; signal?:AbortSignal; blob?:boolean};
async function request<T>(method: string, path: string, body?: unknown, options:RequestOptions = {}, retried = false): Promise<T> {
  if (sessionChanged) throw new ApiError(409, 'SESSION_CHANGED', '登录身份已在其他页面变化，请重新加载后继续。');
  const started = generation;
  const token = browserSession ? null : getToken();
  const res = await fetch(path, {
    method, credentials: 'same-origin', cache: 'no-store', signal:options.signal,
    headers: {
      ...(options.idempotencyKey ? { 'Idempotency-Key': options.idempotencyKey } : {}),
      ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...(browserSession && method !== 'GET' ? { 'X-CSRF-Token': browserCSRF } : {}),
    },
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  if (started !== generation) throw new ApiError(409, 'CONTEXT_CHANGED', '访问上下文已变化，请刷新后重试。');
  if (res.status === 401 && token && !retried) {
    if (!refreshRequest) refreshRequest = refreshLocal().finally(() => { refreshRequest = null; });
    if (await refreshRequest) return request<T>(method, path, body, options, true);
  }
  if (started !== generation || sessionChanged) throw new ApiError(409, 'CONTEXT_CHANGED', '登录上下文已变化，请重新加载。');
  if (res.status === 401 && (token || browserSession)) {
    browserSession = false;
    browserInfo = null;
    browserCSRF = '';
    clearTokens();
    generation++; announceSession();
    window.dispatchEvent(new Event('taboo:logout'));
  }
  if (res.status === 204) return undefined as T;
  if (res.ok && options.blob) { const value = await res.blob(); if (started !== generation) throw new ApiError(409, 'CONTEXT_CHANGED', '访问上下文已变化'); return value as T; }
  const data = await res.json().catch(() => ({}));
  if (started !== generation && res.ok) throw new ApiError(409, 'CONTEXT_CHANGED', '访问上下文已变化');
  if (!res.ok) throw new ApiError(res.status, data.code ?? 'ERROR', data.message ?? res.statusText, data.details);
  return data as T;
}

export const api = {
  get: <T,>(path: string, options?:RequestOptions) => request<T>('GET', path, undefined, options),
  post: <T,>(path: string, body?: unknown, options?:RequestOptions) => request<T>('POST', path, body, options),
  patch: <T,>(path: string, body?: unknown, options?:RequestOptions) => request<T>('PATCH', path, body, options),
  put: <T,>(path: string, body?: unknown, options?:RequestOptions) => request<T>('PUT', path, body, options),
  del: <T,>(path: string, options?:RequestOptions) => request<T>('DELETE', path, undefined, options),
  download: async (path:string, name:string) => {
    const blob = await request<Blob>('GET', path, undefined, {blob:true});
    const url=URL.createObjectURL(blob), link=document.createElement('a');
    link.href=url; link.download=name; link.click(); setTimeout(()=>URL.revokeObjectURL(url),1000);
  },
};

// 实体类型由 api/openapi.yaml 生成（npm run types），此处仅做别名映射保持导入路径不变
import type { components } from './api-types';

type Schemas = components['schemas'];

export type User = Schemas['User'];
export type Org = Schemas['OrgMembership'];
export type Project = Schemas['Project'];
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
export type ImportResult = Schemas['ImportResult'];
export type ScanReport = Schemas['ScanReport'];
export type SecretVersionValue = Schemas['SecretVersionValue'];
