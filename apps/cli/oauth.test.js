import test from 'node:test';
import assert from 'node:assert/strict';
import http from 'node:http';
import { externalOAuth } from './oauth.js';

test('explicit external OAuth, concurrent cache, exact resource and no fallback', async (t) => {
  let issuer, exchanges = 0;
  const server = http.createServer(async (req, res) => {
    res.setHeader('Content-Type', 'application/json');
    if (req.url === '/.well-known/oauth-authorization-server') return res.end(JSON.stringify({ issuer, token_endpoint: issuer + '/token', token_endpoint_auth_methods_supported: ['client_secret_basic'] }));
    assert.equal(req.url, '/token'); exchanges++;
    const chunks = []; for await (const chunk of req) chunks.push(chunk);
    const form = new URLSearchParams(Buffer.concat(chunks).toString());
    assert.equal(form.get('grant_type'), 'client_credentials'); assert.equal(form.get('resource'), 'https://kms.example/api'); assert.equal(form.get('scope'), 'read reveal');
    assert.equal(Buffer.from(req.headers.authorization.slice(6), 'base64').toString(), 'service%3Aclient:secret%2B+value');
    res.end(JSON.stringify({ access_token: 'opaque-fixture', token_type: 'Bearer', expires_in: 300, scope: 'read reveal' }));
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve)); t.after(() => server.close()); issuer = `http://127.0.0.1:${server.address().port}`;
  const env = { TABOO_AUTH_MODE: 'oauth', TABOO_OAUTH_ISSUER: issuer, TABOO_CLIENT_ID: 'service:client', TABOO_CLIENT_SECRET: 'secret+ value', TABOO_ORG: 'a', TABOO_OAUTH_TRUST_ID: 't', TABOO_OAUTH_SCOPES: 'read reveal', TABOO_OAUTH_RESOURCE: 'https://kms.example/api' };
  const client = externalOAuth(env); const sessions = await Promise.all(Array.from({ length: 16 }, () => client.session()));
  assert.equal(exchanges, 1); assert.ok(sessions.every((s) => s.access === 'opaque-fixture' && s.organization === 'a' && s.trust === 't'));
  assert.throws(() => externalOAuth({ ...env, TABOO_AUTH_MODE: 'local' }), /TABOO_AUTH_MODE/);
  assert.throws(() => externalOAuth({ ...env, TABOO_CLIENT_SECRET: '' }), /requires/);
});

test('rejects foreign token endpoint without sending a credential', async (t) => {
  let issuer;
  const server = http.createServer((req, res) => res.end(JSON.stringify({ issuer, token_endpoint: 'https://untrusted.invalid/token' })));
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve)); t.after(() => server.close()); issuer = `http://127.0.0.1:${server.address().port}`;
  const c = externalOAuth({ TABOO_AUTH_MODE: 'oauth', TABOO_OAUTH_ISSUER: issuer, TABOO_CLIENT_ID: 'fixture', TABOO_CLIENT_SECRET: 'fixture', TABOO_ORG: 'a', TABOO_OAUTH_TRUST_ID: 't', TABOO_OAUTH_SCOPES: 'read' });
  await assert.rejects(c.session(), /not trusted/);
});
