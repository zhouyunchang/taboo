// Browser login for the taboo CLI. Device codes and PKCE verifiers stay in
// this process. The only disk write is the resulting user session, done by taboo.js.
import http from 'node:http';
import crypto from 'node:crypto';
import { spawn } from 'node:child_process';

const b64 = (buf) => buf.toString('base64url');

export function pkce() {
  const verifier = b64(crypto.randomBytes(32));
  const challenge = b64(crypto.createHash('sha256').update(verifier).digest());
  return { verifier, challenge };
}

export function openURL(url) {
  const command = process.platform === 'darwin' ? 'open' : process.platform === 'win32' ? 'cmd' : 'xdg-open';
  const args = process.platform === 'win32' ? ['/c', 'start', '', url] : [url];
  try {
    spawn(command, args, { stdio: 'ignore', detached: true }).unref();
  } catch {
    // The URL is always printed, so a missing opener is not fatal.
  }
}

export function listenLoopback() {
  return new Promise((resolve, reject) => {
    const server = http.createServer();
    server.once('error', reject);
    server.listen(0, '127.0.0.1', () => {
      const address = server.address();
      if (!address || typeof address === 'string') {
        reject(new Error('loopback listener failed'));
        return;
      }
      resolve({ server, redirect: `http://127.0.0.1:${address.port}/callback` });
    });
  });
}

export function waitForCode(server, state, timeoutMs = 5 * 60 * 1000) {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      server.close();
      reject(new Error('login timed out'));
    }, timeoutMs);
    server.on('request', (req, res) => {
      const here = new URL(req.url || '/', 'http://127.0.0.1');
      res.writeHead(200, { 'content-type': 'text/html; charset=utf-8', 'cache-control': 'no-store' });
      res.end('<!doctype html><meta charset="utf-8"><title>taboo</title><p>可以关闭这个页面。</p>');
      clearTimeout(timer);
      server.close();
      if (here.searchParams.get('state') !== state) {
        reject(new Error('login state did not match'));
        return;
      }
      const code = here.searchParams.get('code');
      if (!code) reject(new Error(here.searchParams.get('error') || 'login was not approved'));
      else resolve(code);
    });
  });
}
