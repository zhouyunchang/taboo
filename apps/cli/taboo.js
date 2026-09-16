#!/usr/bin/env node
// taboo CLI（MVP）—— 对齐设计文档 §2 CLI 命令子集
//   taboo login <email> <password>          登录，会话存 ~/.taboo/session.json
//   taboo set <KEY> <VALUE> [--env dev]     写入/更新密钥（产生新版本）
//   taboo get <KEY> [--env dev]             读取明文（记审计）
//   taboo list [--env dev]                  列密钥（无值）
//   taboo export [--env dev]                导出 .env 到 stdout
//   taboo run -- <cmd...>                   注入环境变量并执行子进程（设计文档 §7.1 流程）
// 用法：node cli/taboo.js <command> [args]   （后续打包为独立二进制分发）
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawn } from 'node:child_process';

const BASE = process.env.TABOO_SERVER || 'http://localhost:7100';
const SESSION = path.join(os.homedir(), '.taboo', 'session.json');

const [,, cmd, ...args] = process.argv;
const flag = (name, def) => {
  const i = args.indexOf(`--${name}`);
  return i >= 0 ? args[i + 1] : def;
};
const env = flag('env', 'dev');

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
async function call(method, p, body, raw = false) {
  const s = await ensureToken(loadSession());
  const res = await fetch(BASE + p, {
    method,
    headers: {
      ...(body ? { 'Content-Type': 'application/json' } : {}),
      ...(s?.access ? { Authorization: `Bearer ${s.access}` } : {}),
    },
    body: body ? JSON.stringify(body) : undefined,
  });
  if (raw) return res;
  const data = await res.json().catch(() => ({}));
  if (!res.ok) { console.error(`error ${res.status}: ${data.code} ${data.message}`); process.exit(1); }
  return data;
}

async function projectId() {
  const explicit = flag('project', null);
  if (explicit) return explicit;
  const s = loadSession();
  if (s?.kind === 'identity') {
    console.error('机器身份会话请显式指定 --project <project_id>（scope 内项目）');
    process.exit(1);
  }
  const me = await call('GET', '/api/v1/me');
  const slug = me.orgs[0].slug;
  const projects = await call('GET', `/api/v1/orgs/${slug}/projects`);
  const p = projects.projects.find((x) => x.slug === 'default') || projects.projects[0];
  return p.id;
}

switch (cmd) {
  case 'login': {
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
    const [email, password] = args;
    if (!email || !password) { console.error('usage: taboo login <email> <password> | --client-id <id> --client-secret <secret>'); process.exit(1); }
    const d = await call('POST', '/api/v1/auth/login', { email, password });
    saveSession(d.tokens);
    console.log(`logged in as ${d.user.email}`);
    break;
  }
  case 'set': {
    const [key, value] = args.filter((a) => !a.startsWith('--'));
    if (!key || value === undefined) { console.error('usage: taboo set <KEY> <VALUE> [--env dev]'); process.exit(1); }
    const d = await call('POST', `/api/v1/projects/${await projectId()}/secrets?env=${env}`, { key, value });
    console.log(`${key} saved (v${d.version})`);
    break;
  }
  case 'get': {
    const key = args.find((a) => !a.startsWith('--'));
    const d = await call('GET', `/api/v1/projects/${await projectId()}/secrets/${encodeURIComponent(key)}?env=${env}`);
    console.log(d.value);
    break;
  }
  case 'list': {
    const d = await call('GET', `/api/v1/projects/${await projectId()}/secrets?env=${env}`);
    for (const s of d.secrets) console.log(`${s.key}\tv${s.version}\t${s.updated_at}`);
    break;
  }
  case 'export': {
    const res = await call('GET', `/api/v1/projects/${await projectId()}/export?env=${env}`, null, true);
    process.stdout.write(await res.text());
    break;
  }
  case 'run': {
    // 密钥注入子进程；父进程退出前清理（设计文档 §7.1）
    const idx = args.indexOf('--');
    if (idx < 0 || idx === args.length - 1) { console.error('usage: taboo run -- <cmd...>'); process.exit(1); }
    const res = await call('GET', `/api/v1/projects/${await projectId()}/export?env=${env}`, null, true);
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
  default:
    console.log(`taboo CLI (MVP) — server: ${BASE}

  taboo login <email> <password>                用户登录
  taboo login --client-id X --client-secret Y   机器身份登录（token 临期自动重换）
  taboo set <KEY> <VALUE> [--env] [--project]   写入/更新密钥
  taboo get <KEY> [--env] [--project]           读取明文（审计）
  taboo list [--env] [--project]                列出密钥（无值）
  taboo export [--env] [--project]              导出 .env 格式到 stdout
  taboo run -- <cmd...>                         注入环境变量执行命令
`);
}
