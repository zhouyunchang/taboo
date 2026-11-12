import test from 'node:test';
import assert from 'node:assert/strict';
import crypto from 'node:crypto';
import { pkce, listenLoopback, waitForCode } from './browser-login.js';

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

test('stray loopback requests do not end the login; state is checked', async () => {
  const { server, redirect } = await listenLoopback();
  const got = waitForCode(server, 'st-1', 5000);
  const base = new URL(redirect);
  const stray = await fetch(new URL('/favicon.ico', base));
  assert.equal(stray.status, 404);
  const ok = await fetch(`${redirect}?code=abc&state=st-1`);
  assert.equal(ok.status, 200);
  assert.equal(await got, 'abc');
});

test('wrong state is rejected', async () => {
  const { server, redirect } = await listenLoopback();
  const got = waitForCode(server, 'st-2', 5000).then(() => null, (e) => e);
  await fetch(`${redirect}?code=abc&state=other`);
  assert.match(String(await got), /state/);
});
