// taboo Node SDK. Method names follow packages/sdk-go; values stay out of logs.
export class ApiError extends Error {
  constructor(status, code, message, details) {
    super(`taboo ${status} ${code}: ${message}`);
    this.status = status;
    this.code = code;
    this.messageText = message;
    this.details = details;
  }
}

function query(pairs) {
  const params = new URLSearchParams();
  for (let i = 0; i + 1 < pairs.length; i += 2) if (pairs[i + 1] !== '' && pairs[i + 1] != null) params.set(pairs[i], String(pairs[i + 1]));
  const text = params.toString();
  return text ? `?${text}` : '';
}

function loopback(url) {
  const parsed = new URL(url);
  const host = parsed.hostname;
  if (parsed.username || parsed.search || parsed.hash) throw new Error('HTTPS or loopback HTTP URL required');
  if (parsed.protocol === 'https:') return parsed;
  if (parsed.protocol === 'http:' && (host === '127.0.0.1' || host === 'localhost' || host === '[::1]' || host === '::1')) return parsed;
  throw new Error('HTTPS or loopback HTTP URL required');
}

export class Client {
  constructor(baseURL) {
    this.baseURL = String(baseURL).replace(/\/$/, '');
    this.token = '';
    this.refreshToken = '';
    this.identity = null;
    this.oauth = null;
    this.onRefresh = null;
  }

  withToken(token) {
    this.token = token;
    this.refreshToken = '';
    this.identity = null;
    this.oauth = null;
    return this;
  }

  withUserTokens(access, refresh, onRefresh) {
    this.token = access;
    this.refreshToken = refresh;
    this.identity = null;
    this.oauth = null;
    this.onRefresh = onRefresh ?? null;
    return this;
  }

  withIdentity(clientID, clientSecret) {
    this.identity = { clientID, clientSecret, expiresAt: 0 };
    this.oauth = null;
    this.token = '';
    this.refreshToken = '';
    return this;
  }

  async withOAuthClientCredentials(config) {
    loopback(config.issuer);
    loopback(this.baseURL);
    const mode = config.metadataMode || 'oauth';
    if (mode !== 'oauth' && mode !== 'oidc') throw new Error('metadata mode must be oauth or oidc');
    if (!config.clientID || !config.clientSecret || !config.organization || !config.trustID || !config.scopes?.length) {
      throw new Error('OAuth client credential, scopes, organization and trust ID are required');
    }
    for (const scope of config.scopes) if (!scope || /[\s]/.test(scope)) throw new Error('each OAuth scope must be one exact value');
    if (config.resource) {
      const resource = new URL(config.resource);
      if (!resource.protocol || resource.hash) throw new Error('resource must be an absolute URI without a fragment');
    }
    this.oauth = { ...config, metadataMode: mode, scopes: [...config.scopes], endpoint: '', metadataExpires: 0, expires: 0 };
    this.identity = null;
    this.token = '';
    this.refreshToken = '';
    this.onRefresh = null;
    return this;
  }

  async ensureToken() {
    if (this.oauth) return this.#oauthToken();
    if (this.identity && this.identity.expiresAt - Date.now() < 30_000) {
      const res = await fetch(`${this.baseURL}/api/v1/identities/token`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, redirect: 'manual', signal: AbortSignal.timeout(30_000),
        body: JSON.stringify({ client_id: this.identity.clientID, client_secret: this.identity.clientSecret }),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok || !data.access_token) throw new Error(`identity token exchange failed: ${res.status}`);
      this.token = data.access_token;
      this.identity.expiresAt = Date.now() + data.expires_in * 1000;
    }
    return this.token;
  }

  async #oauthToken() {
    const cfg = this.oauth;
    if (this.token && cfg.expires - Date.now() > 30_000) return this.token;
    const issuer = loopback(cfg.issuer);
    if (!cfg.endpoint || Date.now() > cfg.metadataExpires) {
      const metadata = cfg.metadataMode === 'oauth'
        ? `${issuer.protocol}//${issuer.host}/.well-known/oauth-authorization-server${issuer.pathname.replace(/\/$/, '')}`
        : `${cfg.issuer.replace(/\/$/, '')}/.well-known/openid-configuration`;
      const res = await fetch(metadata, { redirect: 'manual', signal: AbortSignal.timeout(30_000) });
      const data = await readLimited(res);
      if (!res.ok || data.issuer !== cfg.issuer) throw new Error('OAuth metadata rejected');
      const endpoint = loopback(data.token_endpoint);
      if (endpoint.protocol !== issuer.protocol || endpoint.host !== issuer.host) throw new Error('untrusted OAuth token endpoint');
      const methods = data.token_endpoint_auth_methods_supported ?? [];
      if (methods.length && !methods.includes('client_secret_basic')) throw new Error('OAuth client_secret_basic is not supported');
      cfg.endpoint = data.token_endpoint;
      cfg.metadataExpires = Date.now() + 5 * 60_000;
    }
    const form = new URLSearchParams({ grant_type: 'client_credentials', scope: cfg.scopes.join(' ') });
    if (cfg.resource) form.set('resource', cfg.resource);
    const basic = Buffer.from(`${encodeURIComponent(cfg.clientID)}:${encodeURIComponent(cfg.clientSecret)}`).toString('base64');
    const res = await fetch(cfg.endpoint, {
      method: 'POST', redirect: 'manual', signal: AbortSignal.timeout(30_000), body: form,
      headers: { 'Content-Type': 'application/x-www-form-urlencoded', Authorization: `Basic ${basic}` },
    });
    const token = await readLimited(res);
    if (!res.ok || !token.access_token || token.access_token.length > 16384 || String(token.token_type).toLowerCase() !== 'bearer' || token.expires_in <= 0 || token.expires_in > 86400) {
      throw new Error('OAuth token exchange rejected');
    }
    if (token.scope != null) {
      const granted = String(token.scope).split(/\s+/);
      for (const scope of cfg.scopes) if (!granted.includes(scope)) throw new Error('OAuth server did not grant every requested scope');
    }
    this.token = token.access_token;
    cfg.expires = Date.now() + token.expires_in * 1000;
    return this.token;
  }

  async request(method, path, { body, raw = false, idempotencyKey, expectText = false } = {}) {
    const token = await this.ensureToken();
    const res = await fetch(this.baseURL + path, {
      method, redirect: 'manual', signal: AbortSignal.timeout(30_000),
      headers: {
        ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
        ...(idempotencyKey ? { 'Idempotency-Key': idempotencyKey } : {}),
        ...(token ? { Authorization: `Bearer ${token}` } : {}),
        ...(this.oauth ? { 'X-Taboo-Trust': this.oauth.trustID, 'X-Taboo-Organization': this.oauth.organization } : {}),
      },
      body: body !== undefined ? JSON.stringify(body) : undefined,
    });
    if (res.status >= 400) {
      const data = await res.json().catch(() => ({}));
      throw new ApiError(res.status, data.code || `HTTP_${res.status}`, data.message || 'request rejected', data.details);
    }
    if (raw) return res;
    if (expectText) return res.text();
    if (res.status === 204) return null;
    return res.json();
  }

  login(email, password) { return this.request('POST', '/api/v1/auth/login', { body: { email, password } }); }
  totpLogin(challenge, code) { return this.request('POST', '/api/v1/auth/totp/login', { body: { challenge, code } }); }
  register(email, password, name) { return this.request('POST', '/api/v1/auth/register', { body: { email, password, name } }); }
  async refresh(refresh) {
    const out = await this.request('POST', '/api/v1/auth/refresh', { body: { refresh } });
    this.token = out.access;
    this.refreshToken = out.refresh;
    this.onRefresh?.(out.access, out.refresh);
    return out;
  }
  me() { return this.request('GET', '/api/v1/me'); }
  projects(orgSlug) { return this.request('GET', `/api/v1/orgs/${encodeURIComponent(orgSlug)}/projects`); }
  listFolders(projectID, env) { return this.request('GET', `/api/v1/projects/${encodeURIComponent(projectID)}/folders${query(['env', env])}`); }
  createFolder(projectID, env, path) { return this.request('POST', `/api/v1/projects/${encodeURIComponent(projectID)}/folders${query(['env', env])}`, { body: { path } }); }
  listSecrets(projectID, env, folderPath = '') { return this.request('GET', `/api/v1/projects/${encodeURIComponent(projectID)}/secrets${query(['env', env, 'path', folderPath])}`); }
  upsertSecret(projectID, env, folderPath, key, value, comment = '', tags = null, options = {}) {
    const body = { key, value, comment, tags: tags ?? [], ...(options.expectedVersion != null ? { expectedVersion: options.expectedVersion } : {}) };
    return this.request('POST', `/api/v1/projects/${encodeURIComponent(projectID)}/secrets${query(['env', env, 'path', folderPath])}`, { body, idempotencyKey: options.idempotencyKey });
  }
  getByLocator(locator, key) {
    return this.request('GET', `/api/v1/access?${new URLSearchParams({ locator, key })}`);
  }
  putByLocator(locator, key, value, options = {}) {
    const body = { key, value, ...(options.expectedVersion != null ? { expectedVersion: options.expectedVersion } : {}) };
    return this.request('PUT', `/api/v1/access?${new URLSearchParams({ locator })}`, { body, idempotencyKey: options.idempotencyKey });
  }
  listByLocator(locator) {
    return this.request('GET', `/api/v1/access?${new URLSearchParams({ locator })}`);
  }
  getSecret(projectID, env, folderPath, key, options = {}) {
    return this.request('GET', `/api/v1/projects/${encodeURIComponent(projectID)}/secrets/${encodeURIComponent(key)}${query(['env', env, 'path', folderPath, 'resolve', options.resolve ? 'true' : ''])}`);
  }
  revealSecretVersion(projectID, env, folderPath, key, version, options = {}) {
    return this.request('GET', `/api/v1/projects/${encodeURIComponent(projectID)}/secrets/${encodeURIComponent(key)}/versions/${version}${query(['env', env, 'path', folderPath, 'resolve', options.resolve ? 'true' : ''])}`);
  }
  secretVersions(projectID, env, folderPath, key) {
    return this.request('GET', `/api/v1/projects/${encodeURIComponent(projectID)}/secrets/${encodeURIComponent(key)}/versions${query(['env', env, 'path', folderPath])}`);
  }
  rollbackSecret(projectID, env, folderPath, key, version, options = {}) {
    const body = { version, ...(options.expectedVersion != null ? { expectedVersion: options.expectedVersion } : {}) };
    return this.request('POST', `/api/v1/projects/${encodeURIComponent(projectID)}/secrets/${encodeURIComponent(key)}/rollback${query(['env', env, 'path', folderPath])}`, { body, idempotencyKey: options.idempotencyKey });
  }
  exportSecrets(projectID, env, options = {}) {
    return this.request('GET', `/api/v1/projects/${encodeURIComponent(projectID)}/export${query(['env', env, 'resolve', options.resolve ? 'true' : ''])}`, { expectText: true });
  }
  importEnv(projectID, env, folderPath, content, strategy = '', preview = false, options = {}) {
    return this.request('POST', `/api/v1/projects/${encodeURIComponent(projectID)}/import${query(['env', env, 'path', folderPath])}`, {
      body: { content, preview, ...(strategy ? { strategy } : {}) }, idempotencyKey: options.idempotencyKey,
    });
  }
  deleteSecret(projectID, env, folderPath, key, options = {}) {
    const pairs = ['env', env, 'path', folderPath];
    if (options.expectedVersion != null) pairs.push('expectedVersion', String(options.expectedVersion));
    return this.request('DELETE', `/api/v1/projects/${encodeURIComponent(projectID)}/secrets/${encodeURIComponent(key)}${query(pairs)}`, { idempotencyKey: options.idempotencyKey });
  }
  async defaultProject(orgSlug) {
    const data = await this.projects(orgSlug);
    const found = data.projects?.find((item) => item.slug === 'default') ?? data.projects?.[0];
    if (!found) throw new Error(`no project in org ${orgSlug}`);
    return found;
  }
  createScanReport(orgSlug, source, findings) {
    return this.request('POST', `/api/v1/orgs/${encodeURIComponent(orgSlug)}/scan-reports`, { body: { source, findings: findings ?? [] } });
  }
  listScanReports(orgSlug) { return this.request('GET', `/api/v1/orgs/${encodeURIComponent(orgSlug)}/scan-reports`); }
  getScanReport(orgSlug, id) { return this.request('GET', `/api/v1/orgs/${encodeURIComponent(orgSlug)}/scan-reports/${encodeURIComponent(id)}`); }
}

async function readLimited(res) {
  const buf = await res.arrayBuffer();
  if (buf.byteLength > 1 << 20) throw new Error('OAuth response unavailable');
  try { return JSON.parse(new TextDecoder().decode(buf)); } catch { throw new Error('invalid OAuth response'); }
}
