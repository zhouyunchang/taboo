// taboo API Server 入口 —— 单体优先：API + 前端静态文件同端口
import http from 'node:http';
import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import { fileURLToPath } from 'node:url';
import { openDatabase } from './db.js';
import { loadMasterKey } from './crypto.js';
import { makeAuth } from './auth.js';
import { makeApi } from './api.js';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const PORT = Number(process.env.TABOO_PORT || process.env.PORT || 7100);
const DATA_DIR = process.env.TABOO_DATA_DIR || path.join(__dirname, '..', 'data');
const WEB_DIST = process.env.TABOO_WEB_DIST || path.resolve(__dirname, '../../../apps/web/dist');

const db = openDatabase(DATA_DIR);
const masterKey = loadMasterKey(DATA_DIR);
const jwtSecret = process.env.TABOO_JWT_SECRET || crypto.randomBytes(32).toString('hex');
if (!process.env.TABOO_JWT_SECRET) {
  console.log('[taboo] TABOO_JWT_SECRET not set, using ephemeral secret (sessions reset on restart). Set it for production.');
}

const requireAuth = makeAuth(db, jwtSecret);
const api = makeApi({ db, masterKey, jwtSecret, requireAuth });

const MIME = {
  '.html': 'text/html; charset=utf-8', '.js': 'text/javascript', '.css': 'text/css',
  '.json': 'application/json', '.svg': 'image/svg+xml', '.png': 'image/png',
  '.ico': 'image/x-icon', '.woff2': 'font/woff2', '.map': 'application/json',
};

const server = http.createServer(async (req, res) => {
  const u = new URL(req.url, `http://${req.headers.host || 'localhost'}`);
  const pathname = u.pathname;

  // CORS（开发模式：Vite dev server 跨域调用）
  res.setHeader('Access-Control-Allow-Origin', process.env.TABOO_CORS_ORIGIN || '*');
  res.setHeader('Access-Control-Allow-Methods', 'GET,POST,PUT,DELETE,OPTIONS');
  res.setHeader('Access-Control-Allow-Headers', 'Content-Type,Authorization');
  if (req.method === 'OPTIONS') { res.writeHead(204); return res.end(); }

  // 安全响应头
  res.setHeader('X-Content-Type-Options', 'nosniff');
  res.setHeader('X-Frame-Options', 'DENY');
  res.setHeader('Referrer-Policy', 'no-referrer');

  if (pathname.startsWith('/api/')) {
    try {
      await api(req, res, pathname, u.searchParams);
    } catch (err) {
      console.error('[taboo] api error:', err);
      if (!res.headersSent) jsonErr(res, 500, 'INTERNAL', 'internal error');
    }
    return;
  }

  // 静态前端（web/dist 存在时）；SPA fallback 到 index.html
  if (req.method === 'GET' && fs.existsSync(WEB_DIST)) {
    const safe = path.normalize(pathname).replace(/^([/\\])+/, '');
    let file = path.join(WEB_DIST, safe);
    if (!file.startsWith(WEB_DIST) || (!fs.existsSync(file) || fs.statSync(file).isDirectory())) {
      file = path.join(WEB_DIST, 'index.html');
    }
    const ext = path.extname(file).toLowerCase();
    res.writeHead(200, { 'Content-Type': MIME[ext] || 'application/octet-stream', 'Cache-Control': 'no-store' });
    return fs.createReadStream(file).pipe(res);
  }

  res.writeHead(200, { 'Content-Type': 'text/plain; charset=utf-8' });
  res.end('taboo (禁制) API is running. Web UI not built yet — see web/README.md.\n');
});

function jsonErr(res, code, c, m) {
  res.writeHead(code, { 'Content-Type': 'application/json' });
  res.end(JSON.stringify({ code: c, message: m }));
}

server.listen(PORT, () => {
  console.log(`[taboo] server listening on http://localhost:${PORT}`);
  console.log(`[taboo] data dir: ${DATA_DIR}`);
});
