// External OAuth is opt-in and process-local. Credentials/tokens never enter the
// legacy on-disk user session. Only public discovery and client_credentials are used.
function protocolURL(value) {
  const u = new URL(value);
  if (u.username || u.password || u.search || u.hash || (u.protocol !== 'https:' && !(u.protocol === 'http:' && ['localhost', '127.0.0.1', '[::1]'].includes(u.hostname)))) throw new Error('OAuth requires HTTPS or loopback HTTP URLs');
  return u;
}
async function jsonResponse(response) {
  const reader = response.body?.getReader();
  if (!reader) throw new Error('OAuth response unavailable');
  const chunks = []; let size = 0;
  try {
    for (;;) { const { done, value } = await reader.read(); if (done) break; size += value.length; if (size > 1024 * 1024) throw new Error('OAuth response too large'); chunks.push(value); }
    return JSON.parse(Buffer.concat(chunks).toString('utf8'));
  } finally { await reader.cancel(); }
}
const formEscape = (value) => new URLSearchParams({ x: value }).toString().slice(2);
export function externalOAuth(env = process.env, explicit = false) {
  const mode = explicit ? 'oauth' : (env.TABOO_AUTH_MODE || 'local');
  if (!['local', 'oauth'].includes(mode)) throw new Error('TABOO_AUTH_MODE must be local or oauth');
  if (mode === 'local') { if (env.TABOO_OAUTH_ISSUER) throw new Error('Set TABOO_AUTH_MODE=oauth for the configured external issuer'); return null; }
  const issuer = env.TABOO_OAUTH_ISSUER;
  const issuerURL = protocolURL(issuer);
  protocolURL(env.TABOO_SERVER || 'http://localhost:7100');
  const metadataMode = env.TABOO_OAUTH_METADATA_MODE || 'oauth';
  if (!['oauth', 'oidc'].includes(metadataMode)) throw new Error('OAuth metadata mode must be oauth or oidc');
  const clientID = env.TABOO_CLIENT_ID, clientSecret = env.TABOO_CLIENT_SECRET;
  const organization = env.TABOO_ORG, trust = env.TABOO_OAUTH_TRUST_ID;
  const scopes = (env.TABOO_OAUTH_SCOPES || '').split(' ').filter(Boolean);
  if (!clientID || !clientSecret || !organization || !trust || !scopes.length) throw new Error('External OAuth requires client credentials, TABOO_ORG, TABOO_OAUTH_TRUST_ID and TABOO_OAUTH_SCOPES');
  const resource = env.TABOO_OAUTH_RESOURCE;
  if (resource) { const u = new URL(resource); if (u.hash) throw new Error('OAuth resource must not contain a fragment'); }
  let endpoint, metadataExpires = 0, token, expires = 0, pending;
  const fetchProtocol = (url, options = {}) => fetch(url, { ...options, redirect: 'manual', signal: AbortSignal.timeout(10000) });
  async function exchange() {
    if (!endpoint || Date.now() >= metadataExpires) {
      const location = metadataMode === 'oauth' ? `${issuerURL.origin}/.well-known/oauth-authorization-server${issuerURL.pathname.replace(/\/$/, '')}` : `${issuer.replace(/\/$/, '')}/.well-known/openid-configuration`;
      const response = await fetchProtocol(location); if (!response.ok) throw new Error('OAuth metadata unavailable');
      const metadata = await jsonResponse(response);
      if (metadata.issuer !== issuer || protocolURL(metadata.token_endpoint).origin !== issuerURL.origin) throw new Error('OAuth metadata or token endpoint is not trusted');
      if (metadata.token_endpoint_auth_methods_supported?.length && !metadata.token_endpoint_auth_methods_supported.includes('client_secret_basic')) throw new Error('OAuth client_secret_basic is not supported');
      endpoint = metadata.token_endpoint; metadataExpires = Date.now() + 300000;
    }
    const body = new URLSearchParams({ grant_type: 'client_credentials', scope: scopes.join(' ') });
    if (resource) body.set('resource', resource);
    const response = await fetchProtocol(endpoint, { method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded', Authorization: `Basic ${Buffer.from(`${formEscape(clientID)}:${formEscape(clientSecret)}`).toString('base64')}` }, body });
    if (!response.ok) throw new Error(`OAuth token exchange rejected (${response.status})`);
    const result = await jsonResponse(response);
    if (typeof result.access_token !== 'string' || !result.access_token || result.access_token.length > 16384 || result.token_type?.toLowerCase() !== 'bearer' || !Number.isInteger(result.expires_in) || result.expires_in <= 0 || result.expires_in > 86400) throw new Error('Invalid OAuth token response');
    if (result.scope !== undefined && (typeof result.scope !== 'string' || scopes.some((scope) => !result.scope.split(' ').includes(scope)))) throw new Error('OAuth server did not grant all requested scopes');
    token = result.access_token; expires = Date.now() + result.expires_in * 1000;
    return { access: token, kind: 'oauth', organization, trust };
  }
  return {
    async session() {
      if (token && expires > Date.now() + 30000) return { access: token, kind: 'oauth', organization, trust };
      if (!pending) pending = exchange().finally(() => { pending = undefined; });
      return pending;
    },
  };
}
