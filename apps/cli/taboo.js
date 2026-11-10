#!/usr/bin/env node
// taboo CLI（MVP）—— 对齐设计文档 §2 CLI 命令子集
//   taboo login <email> <password>          登录，会话存 ~/.taboo/session.json
//   taboo set <locator> <KEY> <VALUE>       写入/更新密钥（产生新版本）
//   taboo get <locator> <KEY>               读取明文（记审计）
//   taboo list <locator>                    列密钥（无值）
//   taboo export <locator>                  导出 .env 到 stdout
//   taboo run <locator> -- <cmd...>         注入环境变量并执行子进程（设计文档 §7.1 流程）
//   taboo scan [opts]                       密钥泄漏扫描（M3 #6，设计文档 §2）
// 用法：node cli/taboo.js <command> [args]   （后续打包为独立二进制分发）
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawn } from 'node:child_process';
import { collectScan, printScan, runScan, uploadFindings } from './scan.js';
import { randomUUID } from 'node:crypto';
import { externalOAuth } from './oauth.js';
import { listenLoopback, openURL, pkce, waitForCode } from './browser-login.js';

const BASE = process.env.TABOO_SERVER || 'http://localhost:7100';
const SESSION = process.env.TABOO_SESSION_FILE || path.join(os.homedir(), '.taboo', 'session.json');

const [,, cmd, ...args] = process.argv;
const flag = (name, def) => {
  const i = args.indexOf(`--${name}`);
  return i >= 0 ? args[i + 1] : def;
};
const resolveQuery = args.includes('--raw') ? '' : '&resolve=true';
function isLocator(value) {
  return typeof value === 'string' && /^[a-z0-9][a-z0-9-]*(?:\/[a-z0-9][a-z0-9-]*)+$/.test(value);
}
function positional(valueFlags = ['env', 'project', 'org', 'strategy', 'path', 'idempotency-key', 'expected-version', 'client-id', 'client-secret']) {
  const out = [];
  for (let i = 0; i < args.length; i++) {
    if (args[i] === '--') break;
    if (args[i].startsWith('--')) {
      if (valueFlags.includes(args[i].slice(2))) i++;
      continue;
    }
    out.push(args[i]);
  }
  return out;
}
let oauth;
try { oauth = externalOAuth(process.env, cmd === 'login' && args.includes('--oauth')); } catch (error) { console.error(error.message); process.exit(2); }
process.on('unhandledRejection', (error) => { console.error(error?.message || 'request failed'); process.exit(1); });

function saveSession(d) {
  fs.mkdirSync(path.dirname(SESSION), { recursive: true, mode: 0o700 });
  fs.writeFileSync(SESSION, JSON.stringify(d, null, 2), { mode: 0o600 });
}
function loadSession() {
  try { return JSON.parse(fs.readFileSync(SESSION, 'utf8')); } catch { return null; }
}
async function identityToken(clientId, clientSecret) {
  const res = await fetch(BASE + '/api/v1/identities/token', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ client_id: clientId, client_secret: clientSecret }),
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) { console.error(`error ${res.status}: ${data.code} ${data.message}`); process.exit(1); }
  return {
    kind: 'identity',
    client_id: clientId,
    client_secret: clientSecret,
    access: data.access_token,
    expires_at: Date.now() + data.expires_in * 1000,
  };
}
async function ensureToken(s) {
  // 机器身份 token 短 TTL：临期自动用 client_credentials 重换（设计文档 §7.1 流程）
  if (s?.kind === 'identity' && s.expires_at < Date.now() + 30_000) {
    const next = await identityToken(s.client_id, s.client_secret);
    saveSession(next);
    return next;
  }
  return s;
}
async function publicJSON(p, body) {
  const res = await fetch(BASE + p, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    const reason = data.error || [data.code, data.message].filter(Boolean).join(' ');
    throw new Error(reason || `HTTP ${res.status}`);
  }
  return data;
}
async function deviceLogin() {
  const start = await publicJSON('/api/v1/auth/device', { client_id: 'taboo-cli' });
  const link = start.verification_uri;
  console.error(`打开 ${link}`);
  console.error(`代码 ${start.user_code}`);
  openURL(link);
  let wait = Number(start.interval) || 5;
  for (;;) {
    const res = await fetch(BASE + '/api/v1/auth/device/token', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ client_id: 'taboo-cli', device_code: start.device_code, grant_type: 'urn:ietf:params:oauth:grant-type:device_code' }) });
    const data = await res.json().catch(() => ({}));
    if (res.ok) return data;
    if (data.error === 'slow_down' || res.status === 429) wait += 5;
    else if (data.error !== 'authorization_pending') throw new Error(data.error || 'device login failed');
    await new Promise((resolve) => setTimeout(resolve, wait * 1000));
  }
}
async function browserLogin() {
  const { verifier, challenge } = pkce();
  const state = randomUUID();
  const { server, redirect } = await listenLoopback();
  const link = `${BASE}/cli/login?${new URLSearchParams({ code_challenge: challenge, redirect_uri: redirect, state })}`;
  console.error(`打开 ${link}`);
  openURL(link);
  const code = await waitForCode(server, state);
  return publicJSON('/api/v1/auth/cli/token', { client_id: 'taboo-cli', code, code_verifier: verifier, redirect_uri: redirect });
}
async function call(method, p, body, raw = false, idempotencyKey) {
  const s = oauth ? await oauth.session() : await ensureToken(loadSession());
  const res = await fetch(BASE + p, {
    method,
    headers: {
      ...(idempotencyKey ? { 'Idempotency-Key': idempotencyKey } : {}),
      ...(body ? { 'Content-Type': 'application/json' } : {}),
      ...(s?.access ? { Authorization: `Bearer ${s.access}` } : {}),
      ...(s?.kind === 'oauth' ? { 'X-Taboo-Trust': s.trust, 'X-Taboo-Organization': s.organization } : {}),
    },
    body: body ? JSON.stringify(body) : undefined,
    redirect: 'manual', signal: AbortSignal.timeout(30000),
  });
  if (raw && res.ok) return res;
  const data = await res.json().catch(() => ({}));
  if (!res.ok) { console.error(`error ${res.status}: ${data.code || 'HTTP_ERROR'} ${data.message || 'request rejected'}`); process.exit(1); }
  return data;
}

async function orgSlug() {
  const explicit = flag('org', process.env.TABOO_ORG || null);
  if (explicit) return explicit;
  const me = await call('GET', '/api/v1/me');
  if (!me.orgs?.[0]?.slug) { console.error('无法确定组织，请传入 --org <slug>'); process.exit(1); }
  return me.orgs[0].slug;
}

switch (cmd) {
  case 'login': {
    if (oauth) { await oauth.session(); console.log('External OAuth credentials verified; no token or credential saved to disk.'); break; }
    const clientId = flag('client-id', null);
    const clientSecret = flag('client-secret', null);
    if (clientId || clientSecret) {
      // 机器身份登录（M2 #3）
      if (!clientId || !clientSecret) { console.error('usage: taboo login --client-id <id> --client-secret <secret>'); process.exit(1); }
      const d = await identityToken(clientId, clientSecret);
      saveSession(d);
      console.log(`logged in as machine identity (token ttl ${Math.round((d.expires_at - Date.now()) / 1000)}s, auto-refresh)`);
      break;
    }
    if (args.includes('--browser') || args.includes('--device')) {
      const d = args.includes('--device') ? await deviceLogin() : await browserLogin();
      saveSession(d.tokens);
      console.log('logged in');
      break;
    }
    const [email, password] = positional();
    if (!email || !password) { console.error('usage: taboo login <email> <password> | --browser | --device | --client-id <id> --client-secret <secret>'); process.exit(1); }
    const d = await call('POST', '/api/v1/auth/login', { email, password });
    saveSession(d.tokens);
    console.log(`logged in as ${d.user.email}`);
    break;
  }
  case 'set': {
    const pos = positional();
    const idempotencyKey=flag('idempotency-key',randomUUID());
    const expected=flag('expected-version',undefined);
    if(expected!==undefined && (!/^\d+$/.test(expected)||!Number.isSafeInteger(Number(expected)))){console.error('--expected-version must be a non-negative integer');process.exit(2);}
    const extra = expected!==undefined?{expectedVersion:Number(expected)}:{};
    console.error(`mutation receipt: ${idempotencyKey}; if the outcome is unknown, repeat the identical command with --idempotency-key ${idempotencyKey}`);
    const [locator, key, value] = pos;
    if (!isLocator(locator) || !key || value === undefined) { console.error('usage: taboo set <locator> <KEY> <VALUE>'); process.exit(1); }
    const d = await call('PUT', `/api/v1/access?locator=${encodeURIComponent(locator)}`, { key, value, ...extra }, false, idempotencyKey);
    console.log(`${key} saved (v${d.version})`);
    break;
  }
  case 'get': {
    const pos = positional();
    const [locator, key] = pos;
    if (!isLocator(locator) || !key) { console.error('usage: taboo get <locator> <KEY>'); process.exit(1); }
    const d = await call('GET', `/api/v1/access?locator=${encodeURIComponent(locator)}&key=${encodeURIComponent(key)}${resolveQuery}`);
    console.log(d.value);
    break;
  }
  case 'list': {
    const locator = positional()[0];
    if (!isLocator(locator)) { console.error('usage: taboo list <locator>'); process.exit(1); }
    const d = await call('GET', `/api/v1/access?locator=${encodeURIComponent(locator)}`);
    for (const s of d.secrets) console.log(`${s.key}\tv${s.version}\t${s.updated_at}`);
    break;
  }
  case 'export': {
    const locator = positional()[0];
    if (!isLocator(locator)) { console.error('usage: taboo export <locator> [--raw]'); process.exit(1); }
    const path = `/api/v1/access?locator=${encodeURIComponent(locator)}&export=1${resolveQuery}`;
    const res = await call('GET', path, null, true);
    process.stdout.write(await res.text());
    break;
  }
  case 'import': {
    const pos = positional();
    const file = pos[1];
    if (!isLocator(pos[0]) || !file) { console.error('usage: taboo import <locator> <file> [--format dotenv|doppler|1password] [--preview] [--strategy skip|overwrite]'); process.exit(1); }
    const content = fs.readFileSync(file, 'utf8');
    const strategy = flag('strategy', '');
    const format = flag('format', 'dotenv');
    const preview = args.includes('--preview');
    if (strategy && strategy !== 'skip' && strategy !== 'overwrite') { console.error('--strategy must be skip or overwrite'); process.exit(2); }
    if (!['dotenv', 'doppler', '1password'].includes(format)) { console.error('--format must be dotenv, doppler, or 1password'); process.exit(2); }
    const idempotencyKey = preview ? undefined : flag('idempotency-key', randomUUID());
    if (!preview) console.error(`mutation receipt: ${idempotencyKey}; if the outcome is unknown, repeat the identical command with --idempotency-key ${idempotencyKey}`);
    const importURL = `/api/v1/access?locator=${encodeURIComponent(pos[0])}`;
    const d = await call('POST', importURL,
      { content, format, preview, ...(strategy ? { strategy } : {}) }, false, idempotencyKey);
    const conflicts = d.conflicts?.length ? ` conflicts=${d.conflicts.join(',')}` : '';
    console.log(`${preview ? 'preview' : 'imported'}: created=${d.created} updated=${d.updated} skipped=${d.skipped}${conflicts}`);
    for (const entry of d.entries ?? []) console.log(`${entry.action}\t${entry.key}\tv${entry.version}`);
    if ((d.conflicts?.length ?? 0) > 0 && !strategy) process.exit(1);
    break;
  }
  case 'run': {    // 密钥注入子进程；默认展开引用
    const idx = args.indexOf('--');
    const locator = positional()[0];
    if (idx < 0 || idx === args.length - 1 || !isLocator(locator)) { console.error('usage: taboo run <locator> [--raw] -- <cmd...>'); process.exit(1); }
    const exportURL = `/api/v1/access?locator=${encodeURIComponent(locator)}&export=1${resolveQuery}`;
    const res = await call('GET', exportURL, null, true);
    const text = await res.text();
    const childEnv = { ...process.env };
    for (const line of text.split('\n')) {
      const i = line.indexOf('=');
      if (i > 0) childEnv[line.slice(0, i)] = line.slice(i + 1);
    }
    const child = spawn(args[idx + 1], args.slice(idx + 2), { stdio: 'inherit', env: childEnv, shell: process.platform === 'win32' });
    child.on('exit', (code) => process.exit(code ?? 0));
    break;
  }
  case 'scan': {
    if (args.includes('--upload')) {
      const result = collectScan(args.filter((a) => a !== '--upload'));
      printScan(result);
      if (!result.special) {
        const slug = await orgSlug();
        const findings = uploadFindings(result.findings);
        const report = await call('POST', `/api/v1/orgs/${encodeURIComponent(slug)}/scan-reports`, { source: 'cli', findings });
        console.error(`scan report ${report.id}: ${report.finding_count} finding(s), no matched text uploaded`);
      }
      process.exit(result.code);
    }
    process.exit(runScan(args));
  }
  default:
    console.log(`taboo CLI (MVP) — server: ${BASE}

  taboo login <email> <password>                用户登录
  taboo login --browser                         浏览器 PKCE 登录（回环）
  taboo login --device                          设备码登录
  taboo login --client-id X --client-secret Y   机器身份登录（token 临期自动重换）
  taboo set <locator> <KEY> <VALUE>            按 归属/项目/目录 写入密钥
  taboo get <locator> <KEY> [--raw]            按定位符读取明文
  taboo list <locator>                         列出该目录的密钥
  taboo export <locator> [--raw]               按定位符导出 .env
  taboo import <locator> <file> [--format dotenv|doppler|1password] [--preview] [--strategy skip|overwrite]
  taboo run <locator> [--raw] -- <cmd...>       注入该目录的密钥后执行命令
  taboo scan [--staged|--json|--list-rules]     密钥泄漏扫描（150+ 规则，命中即非零退出）
  taboo scan --upload [--org slug]              上传报告（服务端 HMAC，原文不落库）
  taboo scan --update-baseline                  将现存命中写入 baseline 抑制
  taboo scan --install-hook                     安装 git pre-commit 钩子
`);
}
