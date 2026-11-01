import test from 'node:test';
import assert from 'node:assert/strict';
import crypto from 'node:crypto';
import { pkce, listenLoopback } from './browser-login.js';

test('pkce challenge is S256 of the verifier', () => {
  const { verifier, challenge } = pkce();
  assert.match(verifier, /^[A-Za-z0-9_-]{43,}$/);
  const want = crypto.createHash('sha256').update(verifier).digest('base64url');
  assert.equal(challenge, want);
});

test('loopback listener is 127.0.0.1 /callback', async () => {
  const { server, redirect } = await listenLoopback();
  try {
    const url = new URL(redirect);
    assert.equal(url.protocol, 'http:');
    assert.equal(url.hostname, '127.0.0.1');
    assert.equal(url.pathname, '/callback');
    assert.notEqual(url.port, '');
  } finally {
    await new Promise((resolve) => server.close(resolve));
  }
});
