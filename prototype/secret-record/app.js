// taboo · 密钥记录 — 高保真交互原型
// 这是一个独立页面：数据全部为合成演示数据，不连接真实 API。
// 结构：左「封印 + 档案 + 轮换」，右「版本 / 轮换 / 痕迹」三章，一次读一章。

/* ───────────────────────── 图标：与 apps/web/src/design/Icon.tsx 同源 ───────────────────────── */
const ICONS = {
  key: 'M6.5 2.5a4 4 0 1 0 2.9 6.7l1.1 1.1v1.9h1.9v1.9h1.9v-1.9l-3.4-3.4A4 4 0 0 0 6.5 2.5Z M6.5 5.2h.01',
  folder: 'M2 4.5h4.2l1.3 1.5H14v6.5a1 1 0 0 1-1 1H3a1 1 0 0 1-1-1v-8Z',
  'folder-open': 'M2 13V4.5h4.2l1.3 1.5H13v1.5 M2 13l2-5.5h11.5L13.4 13H2Z',
  search: 'M7 2.5a4.5 4.5 0 1 0 0 9 4.5 4.5 0 0 0 0-9Z M10.4 10.4 14 14',
  plus: 'M8 3v10 M3 8h10',
  'chevron-right': 'M6 3.5 10.5 8 6 12.5',
  'chevron-down': 'M3.5 6 8 10.5 12.5 6',
  'chevron-left': 'M10 3.5 5.5 8 10 12.5',
  eye: 'M1.5 8S4 3.5 8 3.5 14.5 8 14.5 8 12 12.5 8 12.5 1.5 8 1.5 8Z M8 6a2 2 0 1 0 0 4 2 2 0 0 0 0-4Z',
  'eye-off': 'M2 2l12 12 M6.2 6.4A2 2 0 0 0 8 10a2 2 0 0 0 1.6-.8 M4.2 4.6C2.7 5.7 1.5 8 1.5 8s2.5 4.5 6.5 4.5c1 0 1.9-.3 2.7-.7 M12.6 10.3c1.2-1 1.9-2.3 1.9-2.3S12 3.5 8 3.5c-.5 0-1 .1-1.4.2',
  copy: 'M5.5 5.5h7v8h-7v-8Z M3.5 10.5h-1v-8h7v1',
  check: 'M3 8.5 6.4 12 13 4',
  pencil: 'M3 13l.6-3.1 7.6-7.6 2.5 2.5-7.6 7.6L3 13Z M9.6 4.4l2.5 2.5',
  trash: 'M2.5 4.5h11 M6 4.5V3h4v1.5 M4 4.5l.7 9h6.6l.7-9 M6.6 7v4 M9.4 7v4',
  clock: 'M8 1.8a6.2 6.2 0 1 0 0 12.4A6.2 6.2 0 0 0 8 1.8Z M8 4.6V8l2.4 1.6',
  history: 'M2.2 8a5.8 5.8 0 1 0 1.8-4.2 M2 2.4v3.2h3.2 M8 5v3.2l2.2 1.4',
  shield: 'M8 1.8 3 3.6v4.2c0 3 2.1 5.3 5 6.4 2.9-1.1 5-3.4 5-6.4V3.6L8 1.8Z M6 7.8l1.6 1.6 2.6-2.8',
  seal: 'M8 1.6 9.6 4l2.8.3.5 2.8L14.4 8l-1.5 2 .1 2.8-2.8.5L8 14.4l-2.2-.9-2.8-.5.1-2.8L1.6 8l1.5-2 .5-2.8L6.4 4 8 1.6Z M6.2 8l1.3 1.4 2.6-2.8',
  audit: 'M3 2.2h7l3 3v8.6H3V2.2Z M10 2.2v3h3 M5.5 8.4h5 M5.5 11h3.2',
  robot: 'M3.2 5.6h9.6v7.2H3.2V5.6Z M8 2.4v3.2 M2 9.2H3.2 M12.8 9.2H14 M6.2 8.8v1.4 M9.8 8.8v1.4',
  db: 'M8 1.8c3 0 5.2.9 5.2 1.9S11 5.6 8 5.6 2.8 4.7 2.8 3.7 5 1.8 8 1.8Z M2.8 3.7v8.6c0 1 2.2 1.9 5.2 1.9s5.2-.9 5.2-1.9V3.7 M2.8 8c0 1 2.2 1.9 5.2 1.9s5.2-.9 5.2-1.9',
  settings: 'M8 6a2 2 0 1 0 0 4 2 2 0 0 0 0-4Z M8 1.6l.7 1.7 1.8-.3 1 1.5 1.7.8-.3 1.8 1 1.5-1 1.5.3 1.8-1.7.8-1 1.5-1.8-.3L8 14.4l1.5-1.6-.3-1.8 1-1.5-1-1.5.3-1.8-1.5-1.6Z',
  users: 'M6 7.4a2.6 2.6 0 1 0 0-5.2 2.6 2.6 0 0 0 0 5.2Z M1.6 14c.4-2.6 2.2-3.9 4.4-3.9S10 11.4 10.4 14 M10.8 2.6a2.4 2.4 0 0 1 0 4.6 M11.6 10.4c1.4.4 2.4 1.4 2.8 3.6',
  logout: 'M6.6 2.2H3v11.6h3.6 M10.4 5.2 13.2 8l-2.8 2.8 M13.2 8H6.4',
  alert: 'M8 2 15 14H1L8 2Z M8 6.4v3.4 M8 11.6h.01',
  lock: 'M3.6 7.2h8.8v6.4H3.6V7.2Z M5.4 6.2V4.8a2.6 2.6 0 0 1 5.2 0v1.4',
  unlock: 'M3.6 7.2h8.8v6.4H3.6V7.2Z M5.4 5V4.8a2.6 2.6 0 0 1 5-.9',
  'arrow-right': 'M2.6 8h10.8 M9.2 3.8 13.4 8l-4.2 4.2',
  'arrow-left': 'M13.4 8H2.6 M6.8 3.8 2.6 8l4.2 4.2',
  x: 'M3.6 3.6l8.8 8.8 M12.4 3.6l-8.8 8.8',
  menu: 'M2 4.5h12 M2 8h12 M2 11.5h12',
  more: 'M3.6 8h.01 M8 8h.01 M12.4 8h.01',
  upload: 'M8 11V2.4 M4.4 6 8 2.4 11.6 6 M2.4 13.6h11.2',
  download: 'M8 2.4V11 M4.4 7.6 8 11.2l3.6-3.6 M2.4 14h11.2',
  refresh: 'M13.2 8a5.2 5.2 0 1 1-1.6-3.8 M13.4 2.2v3.2h-3.2',
  scan: 'M2 6V2.6h3.4 M10.6 2.6H14V6 M14 10v3.4h-3.4 M5.4 13.4H2V10 M4.4 8h7.2',
  link: 'M6.4 9.6a2.8 2.8 0 0 0 4 0l2-2a2.8 2.8 0 0 0-4-4l-.8.8 M9.6 6.4a2.8 2.8 0 0 0-4 0l-2 2a2.8 2.8 0 0 0 4 4l.8-.8',
  terminal: 'M2.2 3h11.6v10H2.2V3Z M4.8 6.6 6.8 8.5l-2 1.9 M8.2 10.6h3',
  globe: 'M8 1.8a6.2 6.2 0 1 0 0 12.4A6.2 6.2 0 0 0 8 1.8Z M1.8 8h12.4 M8 1.8c1.8 1.8 2.6 3.9 2.6 6.2S9.8 12.4 8 14.2C6.2 12.4 5.4 10.3 5.4 8s.8-4.4 2.6-6.2Z',
  bell: 'M4 6.6a4 4 0 0 1 8 0c0 3.4 1.4 4.6 1.4 4.6H2.6S4 10 4 6.6Z M6.6 13.4a1.6 1.6 0 0 0 2.8 0',
  sliders: 'M2 4.6h3 M9 4.6h5 M2 11.4h5 M11 11.4h3 M5 2.6v4 M11 9.4v4',
  file: 'M3.6 2h5.2l3.6 3.6V14H3.6V2Z M8.6 2v3.8h3.8',
  play: 'M4.6 2.6 13 8l-8.4 5.4V2.6Z',
  swap: 'M2.5 5.5h9 M9 3l2.5 2.5L9 8 M13.5 10.5h-9 M7 8l-2.5 2.5L7 13',
};

function icon(name, cls = '') {
  return `<svg viewBox="0 0 16 16" width="16" height="16" class="icon ${cls}" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false"><path d="${ICONS[name] || ''}"/></svg>`;
}

function hydrateIcons(root = document) {
  root.querySelectorAll('[data-icon]').forEach((el) => {
    el.innerHTML = icon(el.getAttribute('data-icon'));
    el.removeAttribute('data-icon');
  });
}

/* ───────────────────────── 工具 ───────────────────────── */
const TZ = 'Asia/Shanghai';
const fmtDate = new Intl.DateTimeFormat('sv-SE', {
  timeZone: TZ, year: 'numeric', month: '2-digit', day: '2-digit',
  hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false,
});
const fmt = (iso) => (iso ? fmtDate.format(new Date(iso)) : '—');
const day = (iso) => fmt(iso).slice(0, 10);
const md = (iso) => fmt(iso).slice(5, 10);
const nowIso = () => new Date().toISOString();

function rel(iso) {
  const diff = new Date(iso).getTime() - Date.now();
  const abs = Math.abs(diff) / 60000;
  if (abs < 1) return '刚刚';
  if (diff > 0) {
    if (abs < 60) return `${Math.round(abs)} 分钟后`;
    if (abs < 1440) return `${Math.round(abs / 60)} 小时后`;
    return `${Math.round(abs / 1440)} 天后`;
  }
  if (abs < 60) return `${Math.round(abs)} 分钟前`;
  if (abs < 1440) return `${Math.round(abs / 60)} 小时前`;
  return `${Math.round(abs / 1440)} 天前`;
}

function esc(value) {
  return String(value ?? '').replace(/[&<>"']/g, (ch) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[ch]));
}

function addHours(hours) {
  return new Date(Date.now() + hours * 3600_000).toISOString();
}

function wait(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function dotsFor(len) {
  const count = Math.max(3, Math.min(18, len));
  return '•'.repeat(count);
}

function maskFor(len) {
  if (len < 8) return ['•••', 'cipher-xs'];
  if (len < 24) return ['••••••••', 'cipher-sm'];
  if (len < 96) return ['••••••••••••', 'cipher-md'];
  return ['••••••••••••••••••', 'cipher-lg'];
}

function keyGlyph(key) {
  if (/KEY|SECRET|TOKEN|PASSWORD|PASS$|_PWD$/i.test(key)) return 'lock';
  if (/URL|DSN|URI|HOST|ENDPOINT/i.test(key)) return 'link';
  if (/CERT|PEM|PRIVATE/i.test(key)) return 'file';
  return 'key';
}

function pill(tone, text) {
  return `<span class="pill pill-${tone}">${esc(text)}</span>`;
}

function statusPill(status) {
  const map = {
    active: ['ok', '运行中'], paused: ['muted', '已暂停'], draft: ['warn', '草稿'], fixed: ['muted', '固定'],
    succeeded: ['ok', '已完成'], failed: ['bad', '失败'], skipped: ['muted', '已跳过'],
    awaiting_generation: ['warn', '待生成'], awaiting_distribution: ['warn', '待分发'],
    awaiting_manual: ['warn', '待人工'], awaiting_external: ['warn', '待回写'], overdue: ['bad', '已逾期'],
  };
  const [tone, label] = map[status] || ['muted', status];
  return pill(tone, label);
}

const MODE_LABEL = { generate: '生成并写入', manual: '人工轮换', webhook: '外部 Webhook' };
const TRIGGER_LABEL = { schedule: '定时', manual: '手动', retry: '重试' };
const OPEN_RUN = new Set(['awaiting_generation', 'awaiting_distribution', 'awaiting_manual', 'awaiting_external', 'overdue']);

function periodLabel(policy) {
  if (!policy) return '';
  if (policy.cron) return `cron ${policy.cron}（UTC）`;
  const h = policy.intervalHours || 0;
  if (h >= 24 && h % 24 === 0) return `每 ${h / 24} 天`;
  return `每 ${h} 小时`;
}

/* ───────────────────────── 合成数据 ───────────────────────── */
function write(version, value, actor, at) {
  return { version, value, actor, at };
}

const SEED = [
  {
    key: 'STRIPE_SECRET_KEY',
    folder: 'prod',
    comment: 'Stripe 生产环境服务端密钥，仅 API 服务器读取。',
    tags: ['payments', 'prod'],
    versions: [
      write(1, 'sk_live_demo_4Hx2mQ9pL7tR1wZ5', '0d94e2d3', '2026-08-15T03:00:41+08:00'),
      write(2, 'sk_live_demo_4Hx2mQ9pL7tR1wZ5bN3', 'a17c55e0', '2026-09-14T03:00:07+08:00'),
      write(3, 'sk_live_demo_4Hx2mQ9pL7tR1wZ5bN3cV8', '0d94e2d3', '2026-10-14T03:00:12+08:00'),
      write(4, 'sk_live_demo_4Hx2mQ9pL7tR1wZ5bN3cV8dK6', '0d94e2d3', '2026-11-13T02:00:54+08:00'),
    ],
    policy: {
      status: 'active', mode: 'webhook', intervalHours: 720, cron: '',
      nextAt: '2026-12-13T03:00:00+08:00', lastAt: '2026-11-13T03:00:00+08:00',
      graceDays: 7, owner: '林岚', profile: '', autoGenerate: false,
    },
    runs: [
      { id: 'run_9f2c4a', dueAt: '2026-11-13T03:00:00+08:00', status: 'succeeded', oldVersion: 3, newVersion: 4, trigger: 'schedule', owner: '林岚', attempts: 1, note: '' },
      { id: 'run_7b1d3e', dueAt: '2026-10-14T03:00:00+08:00', status: 'succeeded', oldVersion: 2, newVersion: 3, trigger: 'schedule', owner: '林岚', attempts: 1, note: '' },
      { id: 'run_6a0c2d', dueAt: '2026-09-14T03:00:00+08:00', status: 'skipped', oldVersion: 2, newVersion: null, trigger: 'schedule', owner: '林岚', attempts: 0, note: '上游维护窗口，下一轮补上。' },
      { id: 'run_5e9b1c', dueAt: '2026-08-15T03:00:00+08:00', status: 'failed', oldVersion: 1, newVersion: null, trigger: 'schedule', owner: '林岚', attempts: 3, error: 'Webhook 返回 503：上游暂时不可用。' },
    ],
  },
  {
    key: 'OPENAI_API_KEY',
    folder: 'prod',
    comment: '内容审核异步任务使用；仅 worker 读取。',
    tags: ['ai', 'prod'],
    versions: [
      write(1, 'sk-proj-demo-A1bQ2xLm8Vc4Rw7Yp1Kd6Hn3Js5Fg0Az', 'a17c55e0', '2026-09-18T09:31:10+08:00'),
      write(2, 'sk-proj-demo-T9bQ2xLm8Vc4Rw7Yp1Kd6Hn3Js5Fg0Az', 'a17c55e0', '2026-11-02T10:12:41+08:00'),
    ],
    policy: {
      status: 'paused', mode: 'manual', intervalHours: 2160, cron: '',
      nextAt: null, lastAt: '2026-10-18T03:00:00+08:00',
      graceDays: 14, owner: '林岚', profile: '', autoGenerate: false,
    },
    runs: [
      { id: 'run_4d8a1b', dueAt: '2026-10-18T03:00:00+08:00', status: 'succeeded', oldVersion: 1, newVersion: 2, trigger: 'schedule', owner: '林岚', attempts: 1, note: '' },
      { id: 'run_3c7f9a', dueAt: '2026-09-18T03:00:00+08:00', status: 'skipped', oldVersion: 1, newVersion: null, trigger: 'schedule', owner: '林岚', attempts: 0, note: '等待应用侧升级 SDK。' },
    ],
  },
  {
    key: 'DATABASE_URL',
    folder: 'prod',
    comment: 'payments 读写账号；迁移使用独立账号，不在此处。',
    tags: ['db', 'payments'],
    versions: [
      write(1, 'postgres://taboo_demo:e3b1c7a2@db.internal:5432/payments', '0d94e2d3', '2026-08-28T04:00:18+08:00'),
      write(2, 'postgres://taboo_demo:1a9e7c42@db.internal:5432/payments', '0d94e2d3', '2026-09-28T04:00:09+08:00'),
      write(3, 'postgres://taboo_demo:5f2c8a91@db.internal:5432/payments', '0d94e2d3', '2026-10-28T04:00:22+08:00'),
    ],
    policy: {
      status: 'active', mode: 'generate', intervalHours: 720, cron: '',
      nextAt: '2026-11-27T04:00:00+08:00', lastAt: '2026-10-28T04:00:00+08:00',
      graceDays: 7, owner: '林岚', profile: 'alnum32', autoGenerate: false,
    },
    runs: [
      { id: 'run_2b6e8d', dueAt: '2026-10-28T04:00:00+08:00', status: 'succeeded', oldVersion: 2, newVersion: 3, trigger: 'schedule', owner: '林岚', attempts: 1, note: '' },
      { id: 'run_1a5d7c', dueAt: '2026-09-28T04:00:00+08:00', status: 'succeeded', oldVersion: 1, newVersion: 2, trigger: 'schedule', owner: '林岚', attempts: 1, note: '' },
    ],
  },
  {
    key: 'REDIS_URL',
    folder: 'prod',
    comment: '会话缓存；仅 API 服务器读取。',
    tags: ['cache', 'prod'],
    versions: [
      write(1, 'rediss://default:8a1c@cache.internal:6379/0', '0d94e2d3', '2026-09-02T11:24:36+08:00'),
      write(2, 'rediss://default:b7d3@cache.internal:6379/0', '0d94e2d3', '2026-11-05T15:07:02+08:00'),
    ],
    policy: null,
    runs: [],
  },
  {
    key: 'SMTP_PASSWORD',
    folder: 'staging',
    comment: '预发环境邮件出口；与生产凭据隔离。',
    tags: ['mail', 'staging'],
    versions: [
      write(1, 'demo-smtp-pass-42', '0d94e2d3', '2026-11-10T17:40:26+08:00'),
    ],
    policy: null,
    runs: [],
  },
];

/* ───────────────────────── 状态 ───────────────────────── */
const state = {
  secrets: [],
  currentKey: null,
  chapter: 'versions',
  revealed: false,
  compare: null,
  policyEditing: false,
  traceFilter: 'all',
  traceLimit: 8,
  openRuns: new Set(),
  folder: '/prod/',
  search: '',
  listScroll: 0,
};

const $ = (sel, root = document) => root.querySelector(sel);
const $$ = (sel, root = document) => Array.from(root.querySelectorAll(sel));
const current = () => state.secrets.find((s) => s.key === state.currentKey) || null;

/* ───────────────────────── 数据整理 ───────────────────────── */
function ipFor(actor) {
  if (actor === '0d94e2d3') return '10.0.0.12';
  if (actor === 'a17c55e0') return '10.0.0.31';
  if (actor === 'deploy-bot') return '10.0.0.44';
  if (actor === '林岚') return '203.0.113.24';
  if (actor === '陈一') return '203.0.113.51';
  return '127.0.0.1';
}

function seedTraces(secret) {
  const out = [];
  const add = (at, actor, kind, label, desc, action, ip) => {
    out.push({ at, actor, kind, label, desc, action, ip: ip || ipFor(actor) });
  };

  for (const v of secret.versions) {
    add(v.at, v.actor, 'write', '写入', `写入 v${v.version} · ${v.value.length} 字符`, 'secrets.write');
  }

  const latest = secret.versions[0].version;
  if (secret.key === 'STRIPE_SECRET_KEY') {
    add('2026-11-13T02:02:04+08:00', '林岚', 'rotation', '轮换', '确认分发 · v4 已生效', 'rotation.run.distribute');
    add('2026-11-12T18:22:10+08:00', '林岚', 'reveal', '显示', `读取 v${latest} 明文`, 'secrets.reveal');
    add('2026-11-12T09:05:44+08:00', 'deploy-bot', 'reveal', '显示', '部署时读取 v4 明文', 'secrets.reveal');
    add('2026-09-13T16:40:12+08:00', '林岚', 'policy', '改策略', '轮换策略 → 每 30 天 · Webhook', 'rotation.policy.update');
  } else if (secret.key === 'OPENAI_API_KEY') {
    add('2026-11-11T21:04:38+08:00', '陈一', 'deny', '拒绝', 'viewer 尝试显示明文', 'authz.denied', '203.0.113.51');
    add('2026-11-11T09:18:02+08:00', '林岚', 'reveal', '显示', `读取 v${latest} 明文`, 'secrets.reveal');
    add('2026-10-18T10:02:15+08:00', '林岚', 'policy', '改策略', '暂停轮换：等待 SDK 升级', 'rotation.policy.update');
  } else if (secret.key === 'DATABASE_URL') {
    add('2026-11-12T22:31:09+08:00', 'deploy-bot', 'reveal', '显示', '迁移任务读取 v3 明文', 'secrets.reveal');
    add('2026-10-28T04:00:22+08:00', '林岚', 'rotation', '轮换', '生成并写入 v3', 'rotation.run.generate');
  } else if (secret.key === 'REDIS_URL') {
    add('2026-11-12T08:14:51+08:00', 'deploy-bot', 'reveal', '显示', `读取 v${latest} 明文`, 'secrets.reveal');
  } else if (secret.key === 'SMTP_PASSWORD') {
    add('2026-11-11T14:27:03+08:00', '林岚', 'reveal', '显示', '排查预发邮件失败', 'secrets.reveal');
  }

  for (const run of secret.runs) {
    if (run.status === 'succeeded') {
      add(run.dueAt, '林岚', 'rotation', '轮换', `轮次完成 · v${run.oldVersion} → v${run.newVersion}`, 'rotation.run.complete');
    } else if (run.status === 'failed') {
      add(run.dueAt, '林岚', 'rotation', '轮换', `轮次失败 · ${run.error}`, 'rotation.run.failed');
    } else if (run.status === 'skipped') {
      add(run.dueAt, '林岚', 'rotation', '轮换', `跳过轮次 · ${run.note}`, 'rotation.run.skip');
    }
  }

  return out.sort((a, b) => new Date(b.at) - new Date(a.at));
}

function normalizeSecret(seed) {
  const versions = seed.versions.slice().sort((a, b) => b.version - a.version);
  const versionValues = {};
  for (const v of versions) versionValues[v.version] = v.value;
  return {
    ...seed,
    versions,
    versionValues,
    value: versions[0].value,
    updated: versions[0].at,
    traces: seedTraces({ ...seed, versions }),
  };
}

/* ───────────────────────── 列表 ───────────────────────── */
function renderPath() {
  const parts = state.folder === '/' ? [] : state.folder.split('/').filter(Boolean);
  let html = `<button type="button" class="crumb ${parts.length ? '' : 'active'}" ${parts.length ? '' : 'aria-current="page"'} data-folder="/">根目录</button>`;
  parts.forEach((part, index) => {
    const path = '/' + parts.slice(0, index + 1).join('/') + '/';
    const last = index === parts.length - 1;
    html += `<span style="display:inline-flex;align-items:center;gap:4px"><span data-icon="chevron-right"></span><button type="button" class="crumb ${last ? 'active' : ''}" ${last ? 'aria-current="page"' : ''} data-folder="${esc(path)}">${esc(part)}</button></span>`;
  });
  $('#path-crumbs').innerHTML = html;
  hydrateIcons($('#path-crumbs'));
  $$('.tree-item').forEach((item) => {
    item.classList.toggle('active', item.dataset.treeFolder === state.folder);
    const iconName = item.dataset.treeFolder === state.folder ? 'folder-open' : 'folder';
    const slot = $('.tree-label [data-icon], .tree-label .icon', item);
    if (slot) slot.outerHTML = icon(iconName);
  });
}

function renderList() {
  const q = state.search.trim().toLowerCase();
  let items = state.secrets.filter((s) => state.folder === '/' || '/' + s.folder + '/' === state.folder);
  if (q) {
    items = items.filter((s) =>
      s.key.toLowerCase().includes(q) ||
      s.comment.toLowerCase().includes(q) ||
      s.tags.some((t) => t.toLowerCase().includes(q)));
  }
  items.sort((a, b) => new Date(b.updated) - new Date(a.updated));
  $('#list-count').textContent = q ? `${items.length} / ${state.secrets.filter((s) => state.folder === '/' || '/' + s.folder + '/' === state.folder).length} 把` : `${items.length} 把`;
  renderPath();

  const region = $('#secret-list-region');
  if (items.length === 0) {
    const searching = Boolean(q);
    region.innerHTML = `<div class="empty">
      <span class="empty-mark">${icon(searching ? 'search' : 'key')}</span>
      <p class="empty-title">${searching ? '没有匹配的密钥' : '此文件夹还没有密钥'}</p>
      <p class="empty-body">${searching ? `没有 Key、备注或标签包含「${esc(state.search.trim())}」。` : 'key 归属于一个文件夹；同一 key 在不同目录互不冲突。'}</p>
      ${searching ? `<div class="empty-action"><button type="button" class="btn btn-quiet" data-clear-search>清除搜索</button></div>` : ''}
    </div>`;
    return;
  }

  region.innerHTML = `<div class="table-scroll" role="region" aria-label="密钥列表" tabindex="0">
    <table class="secret-table" data-mobile="cards">
      <thead><tr><th>Key</th><th>值</th><th>版本</th><th>标签</th><th>更新时间</th><th class="col-actions"><span class="sr-only">动作</span></th></tr></thead>
      <tbody>
        ${items.map((s) => {
          const [dots, cls] = maskFor(s.value.length);
          return `<tr data-row="${esc(s.key)}">
            <td class="col-key">
              <button type="button" class="secret-key-btn" data-open="${esc(s.key)}" title="打开密钥记录">${icon(keyGlyph(s.key))}<span>${esc(s.key)}</span></button>
            </td>
            <td class="col-value"><span class="cipher-cell"><span class="${cls}" title="密文 · 点击显示">${dots}</span></span></td>
            <td class="col-version"><span class="mono">v${s.versions[0].version}</span></td>
            <td class="col-tags">${s.tags.length ? `<span class="tag-row">${s.tags.map((t) => pill('muted', t)).join('')}</span>` : '<span class="faint">—</span>'}</td>
            <td class="col-updated" title="${fmt(s.updated)}"><span class="nowrap">${fmt(s.updated)}</span></td>
            <td class="col-actions"><span class="secret-actions">
              <button type="button" class="row-act action-fade" data-act="reveal" data-key="${esc(s.key)}" aria-label="显示" title="显示明文并打开记录">${icon('eye')}</button>
              <button type="button" class="row-act action-fade" data-act="copy" data-key="${esc(s.key)}" aria-label="复制" title="复制">${icon('copy')}</button>
              <button type="button" class="row-act action-fade" data-act="edit" data-key="${esc(s.key)}" aria-label="编辑" title="编辑">${icon('pencil')}</button>
              <span class="actions-divider" aria-hidden="true"></span>
              <button type="button" class="row-act danger action-fade" data-act="delete" data-key="${esc(s.key)}" aria-label="删除" title="删除">${icon('trash')}</button>
            </span></td>
          </tr>`;
        }).join('')}
      </tbody>
    </table>
  </div>`;
}

function setFolder(path) {
  state.folder = path;
  renderList();
}

function findRowButton(key) {
  return $$('#secret-list-region [data-open]').find((el) => el.dataset.open === key) || null;
}

/* ───────────────────────── 记录页 ───────────────────────── */
function openRecord(key, { reveal = false, push = true, initial = false } = {}) {
  const secret = state.secrets.find((s) => s.key === key);
  if (!secret) return;
  if (!initial) state.listScroll = window.scrollY;
  state.currentKey = key;
  state.chapter = 'versions';
  state.revealed = false;
  state.compare = null;
  state.policyEditing = false;
  state.traceFilter = 'all';
  state.traceLimit = 8;
  state.openRuns = new Set();
  clearNotices();
  $('#list-view').hidden = true;
  $('#record-view').hidden = false;
  renderRecord();
  switchChapter('versions');
  document.title = `${key} · taboo`;
  window.scrollTo(0, 0);
  if (push) {
    try { history.pushState({ record: key }, '', '#/secret/' + encodeURIComponent(key)); } catch { /* file:// 原型也允许 */ }
  }
  requestAnimationFrame(() => $('#record-title').focus({ preventScroll: true }));
  if (reveal) setTimeout(() => void toggleReveal(), 180);
}

function hideRecord({ focus = true } = {}) {
  if ($('#record-view').hidden) return;
  const key = state.currentKey;
  state.currentKey = null;
  state.revealed = false;
  state.compare = null;
  state.policyEditing = false;
  $('#record-view').hidden = true;
  $('#list-view').hidden = false;
  document.title = 'taboo · 密钥';
  renderList();
  requestAnimationFrame(() => {
    window.scrollTo(0, state.listScroll || 0);
    if (focus) {
      const row = key && findRowButton(key);
      if (row) row.focus({ preventScroll: true });
      else $('#list-title')?.focus?.();
    }
  });
}

function closeRecord() {
  const hadState = Boolean(history.state && history.state.record);
  hideRecord({ focus: true });
  try {
    if (hadState) history.back();
    else history.replaceState(null, '', location.pathname + location.search);
  } catch { /* ignore */ }
}

function renderRecord() {
  const s = current();
  if (!s) return;
  const latest = s.versions[0];
  $('#record-title').textContent = s.key;
  $('#record-current-pill').textContent = `v${latest.version} 当前`;
  $('#record-path').textContent = `payments / ${s.folder}`;
  $('#record-updated').textContent = `最后写入 ${fmt(latest.at)}`;
  $('#record-version-count').textContent = `${s.versions.length} 个版本`;
  $('#record-location').textContent = `payments / ${s.folder}`;
  $('#record-tags').innerHTML = s.tags.length ? s.tags.map((t) => `<span class="tag">${esc(t)}</span>`).join('') : '<span class="faint">—</span>';
  $('#record-comment').textContent = s.comment || '—';
  const dangerNote = $('#delete-secret-note');
  if (dangerNote) dangerNote.textContent = s.policy ? '版本历史与轮换记录会一并删除，不可恢复。' : '版本历史会一并删除，不可恢复。';
  $('#count-versions').textContent = s.versions.length;
  $('#count-traces').textContent = s.traces.length;

  renderSeal();
  renderRotationMeta();
  renderVersions();
  renderRotation();
  renderTraces();
  if (state.compare) {
    $('#versions-view').hidden = true;
    $('#compare-view').hidden = false;
    renderCompare();
  } else {
    $('#versions-view').hidden = false;
    $('#compare-view').hidden = true;
  }
}

function clearNotices() {
  ['versions', 'rotation', 'traces'].forEach((which) => {
    const el = document.getElementById(which + '-notice');
    if (el) el.innerHTML = '';
  });
  Object.keys(noticeTimers).forEach((key) => clearTimeout(noticeTimers[key]));
}

/* ───────────────────────── 封印 ───────────────────────── */
function renderSeal() {
  const s = current();
  if (!s) return;
  const len = s.value.length;
  $('#seal-chamber').dataset.revealed = String(state.revealed);
  $('#seal-state').textContent = state.revealed ? '明文中' : '已封印';
  $('#seal-value').innerHTML = state.revealed
    ? `<code class="seal-plain" id="seal-plain">${esc(s.value)}</code>`
    : `<span class="seal-cipher" aria-label="密文，长度 ${len} 字符">${dotsFor(len)}</span>`;
  $('#seal-sub').innerHTML = `<span>${len} 字符</span><span class="sep" aria-hidden="true">·</span><span>AES-256-GCM</span>`;
  $('#seal-reveal').innerHTML = `${icon(state.revealed ? 'eye-off' : 'eye')}<span class="btn-label">${state.revealed ? '隐藏' : '显示明文'}</span>`;
  $('#seal-reveal').setAttribute('aria-pressed', String(state.revealed));
  $('#seal-hint').textContent = state.revealed
    ? '失焦即收回；明文不会写入剪贴板，除非你主动复制。'
    : '明文只在此处停留，失焦即收回。复制后 20 秒清空剪贴板。';
}

async function toggleReveal() {
  const s = current();
  if (!s) return;
  if (state.revealed) {
    state.revealed = false;
    renderSeal();
    $('#seal-reveal').focus();
    return;
  }
  const button = $('#seal-reveal');
  button.disabled = true;
  button.innerHTML = `${icon('eye')}<span class="btn-label">读取中…</span>`;
  await wait(180);
  state.revealed = true;
  renderSeal();
  addTrace('reveal', '显示', `读取 v${s.versions[0].version} 明文`, 'secrets.reveal');
  $('#seal-reveal').disabled = false;
  $('#seal-reveal').focus();
}

async function copyText(text) {
  if (navigator.clipboard && navigator.clipboard.writeText) {
    try { await navigator.clipboard.writeText(text); return true; } catch { /* fallback */ }
  }
  try {
    const area = document.createElement('textarea');
    area.value = text;
    area.setAttribute('readonly', '');
    area.style.position = 'fixed';
    area.style.opacity = '0';
    document.body.appendChild(area);
    area.select();
    const ok = document.execCommand('copy');
    document.body.removeChild(area);
    return ok;
  } catch {
    return false;
  }
}

async function copyValue(value, label = '已复制') {
  const ok = await copyText(value);
  showFlash(ok ? `${label} · 20 秒后清空剪贴板` : '复制失败：浏览器不允许写入剪贴板');
  return ok;
}

async function copyCurrent() {
  const s = current();
  if (!s) return;
  const wasRevealed = state.revealed;
  const button = $('#seal-copy');
  button.disabled = true;
  const ok = await copyValue(s.value, '已复制');
  button.disabled = false;
  if (ok && !wasRevealed) addTrace('reveal', '显示', `复制 v${s.versions[0].version} 明文`, 'secrets.reveal');
  if (ok) {
    button.innerHTML = `${icon('check')}<span class="btn-label">已复制</span>`;
    setTimeout(() => {
      if (state.currentKey && $('#seal-copy')) {
        $('#seal-copy').innerHTML = `${icon('copy')}<span class="btn-label">复制</span>`;
      }
    }, 1600);
  }
  $('#seal-copy').focus();
}

/* ───────────────────────── 档案里的轮换状态 ───────────────────────── */
function renderRotationMeta() {
  const s = current();
  if (!s) return;
  const p = s.policy;
  const slot = $('#record-rotation');
  const dot = $('#rotation-dot');
  const dotText = $('#rotation-dot-text');
  if (!slot || !dot) return;
  dot.className = 'chapter-dot';
  if (!p) {
    slot.innerHTML = `${pill('muted', '未设置')} <button type="button" class="link-inline" data-go-rotation>设置</button>`;
    dot.classList.add('none');
    dot.title = '未设置轮换';
    if (dotText) dotText.textContent = '未设置';
    return;
  }
  const openRun = s.runs.find((run) => OPEN_RUN.has(run.status));
  const label = { active: '运行中', paused: '已暂停', draft: '草稿', fixed: '固定' }[p.status] || p.status;
  const tone = p.status === 'active' ? 'ok' : p.status === 'draft' ? 'warn' : 'muted';
  const next = p.status === 'active'
    ? (p.cron ? '按 cron' : p.nextAt ? `下次 ${fmt(p.nextAt).slice(5, 16)}` : '')
    : p.status === 'fixed' ? '没有下次' : '';
  slot.innerHTML = `${pill(tone, label)}${openRun ? ' ' + statusPill(openRun.status) : next ? ` <span class="mono">${next}</span>` : ''} <button type="button" class="link-inline" data-go-rotation>管理</button>`;
  dot.classList.add(openRun ? 'warn' : tone === 'ok' ? 'ok' : tone === 'warn' ? 'warn' : 'muted');
  dot.title = openRun ? `${label} · ${statusLabel(openRun.status)}` : `${label}${next ? ' · ' + next : ''}`;
  if (dotText) dotText.textContent = openRun ? statusLabel(openRun.status) : label;
}

function statusLabel(status) {
  return {
    succeeded: '已完成', failed: '失败', skipped: '已跳过', awaiting_generation: '待生成',
    awaiting_distribution: '待分发', awaiting_manual: '待人工', awaiting_external: '待回写', overdue: '已逾期',
  }[status] || status;
}

/* ───────────────────────── 版本 ───────────────────────── */
function renderVersions() {
  const s = current();
  if (!s) return;
  $('#count-versions').textContent = s.versions.length;
  const latest = s.versions[0].version;
  const rows = s.versions.map((v) => {
    const isCurrent = v.version === latest;
    return `<li class="version-row ${isCurrent ? 'current' : ''}" data-version="${v.version}">
      <div class="version-main">
        <div class="version-top">
          <span class="version-num">v${v.version}</span>
          ${isCurrent ? pill('accent', '当前') : ''}
          <span class="version-time">${fmt(v.at)}</span>
        </div>
        <div class="version-sub">写入者 <span class="mono">${esc(v.actor)}</span> · ${(s.versionValues[v.version] || '').length} 字符</div>
      </div>
      <div class="version-actions">
        ${isCurrent ? '' : `<button type="button" class="btn btn-quiet" data-compare-from="${v.version}" aria-label="对比 v${v.version} 与当前版本">对比</button>
        <button type="button" class="btn btn-quiet" data-rollback="${v.version}" aria-label="回滚到 v${v.version}">回滚到此</button>`}
      </div>
    </li>`;
  }).join('');
  $('#version-list').innerHTML = rows;
  $('#compare-open').disabled = s.versions.length < 2;
  $('#versions-foot').textContent = s.versions.length < 2
    ? '这是第一版；下一次写入后，旧值会保留在这里。'
    : `共 ${s.versions.length} 个版本；旧版本不会因为回滚或轮换被删除。`;
  $('#versions-range').textContent = s.versions.length > 1
    ? `v${s.versions[s.versions.length - 1].version} — v${latest}`
    : `v${latest}`;

  const teach = $('#version-teach');
  if (s.versions.length < 2) {
    teach.hidden = false;
    teach.innerHTML = `<div class="version-teach">
      ${icon('history')}
      <div class="version-teach-body">
        <p class="version-teach-title">至少需要两个版本才能对比</p>
        <p class="version-teach-text">下一次写入会产生 v${latest + 1}，这一版会作为 v${latest} 保留。对比时默认掩码，只有主动显示才读明文。</p>
      </div>
    </div>`;
  } else {
    teach.hidden = true;
    teach.innerHTML = '';
  }
  renderRecentReads();
}

function renderRecentReads() {
  const s = current();
  const el = $('#recent-reads');
  if (!s || !el) return;
  const reads = s.traces.filter((trace) => trace.kind === 'reveal').slice(0, 2);
  const body = reads.length
    ? `<div class="recent-list">${reads.map((trace) => `<div class="recent-row">
        <span class="recent-who" title="${esc(trace.actor)}">${esc(trace.actor)}</span>
        <span class="recent-desc">${esc(trace.desc)}</span>
        <span class="recent-when" title="${esc(fmt(trace.at))}">${esc(fmt(trace.at).slice(5, 16))}</span>
      </div>`).join('')}</div>`
    : `<p class="recent-empty">还没有人显示过这把密钥。第一次显示会出现在这里，并进入审计。</p>`;
  el.innerHTML = `<div class="block-head"><h2 id="recent-reads-label">最近读取</h2><button type="button" class="link-inline" data-go-traces>全部痕迹 ${icon('arrow-right')}</button></div>${body}`;
}

/* ───────────────────────── 对比 ───────────────────────── */
function openCompare(leftVersion) {
  const s = current();
  if (!s || s.versions.length < 2) return;
  const latest = s.versions[0].version;
  const left = Number(leftVersion) || s.versions[1].version;
  state.compare = { left: left === latest ? s.versions[1].version : left, right: latest, revealed: false };
  $('#versions-view').hidden = true;
  $('#compare-view').hidden = false;
  renderCompare();
  requestAnimationFrame(() => $('#compare-back').focus({ preventScroll: true }));
}

function closeCompare() {
  state.compare = null;
  $('#compare-view').hidden = true;
  $('#versions-view').hidden = false;
  renderVersions();
  requestAnimationFrame(() => $('#compare-open').focus({ preventScroll: true }));
}

function compareValues() {
  const s = current();
  if (!s || !state.compare) return null;
  return {
    left: state.compare.left,
    right: state.compare.right,
    leftValue: s.versionValues[state.compare.left] ?? '',
    rightValue: s.versionValues[state.compare.right] ?? '',
    leftMeta: s.versions.find((v) => v.version === state.compare.left),
    rightMeta: s.versions.find((v) => v.version === state.compare.right),
  };
}

function commonPrefix(a, b) {
  const max = Math.min(a.length, b.length);
  let i = 0;
  while (i < max && a[i] === b[i]) i += 1;
  return i;
}

function commonSuffix(a, b, prefix) {
  const max = Math.min(a.length, b.length) - prefix;
  let i = 0;
  while (i < max && a[a.length - 1 - i] === b[b.length - 1 - i]) i += 1;
  return i;
}

function charDelta(a, b) {
  const prefix = commonPrefix(a, b);
  const suffix = commonSuffix(a, b, prefix);
  return {
    prefix: a.slice(0, prefix),
    suffix: suffix ? a.slice(a.length - suffix) : '',
    oldMid: a.slice(prefix, a.length - suffix),
    newMid: b.slice(prefix, b.length - suffix),
  };
}

function renderChangedValue(value, delta, side) {
  if (!value) return '<span class="faint">（空值）</span>';
  if (!delta.oldMid && !delta.newMid) return `<span class="common">${esc(value)}</span>`;
  const prefix = esc(delta.prefix);
  const suffix = esc(delta.suffix);
  if (side === 'old') {
    return `<span class="common">${prefix}</span>${delta.oldMid ? `<span class="changed-del">${esc(delta.oldMid)}</span>` : ''}<span class="common">${suffix}</span>`;
  }
  return `<span class="common">${prefix}</span>${delta.newMid ? `<span class="changed-add">${esc(delta.newMid)}</span>` : ''}<span class="common">${suffix}</span>`;
}

function renderCompare() {
  const s = current();
  const values = compareValues();
  if (!s || !values) return;
  const { left, right, leftValue, rightValue, leftMeta, rightMeta } = values;
  const revealed = Boolean(state.compare.revealed);

  $('#compare-range').textContent = `v${left} ⇄ v${right}`;
  const options = s.versions.slice().sort((a, b) => a.version - b.version);
  const optionHtml = options.map((v) => `<option value="${v.version}">v${v.version} · ${fmt(v.at).slice(0, 10)}</option>`).join('');
  const leftSelect = $('#compare-left');
  const rightSelect = $('#compare-right');
  leftSelect.innerHTML = optionHtml;
  rightSelect.innerHTML = optionHtml;
  leftSelect.value = String(left);
  rightSelect.value = String(right);
  $('#compare-reveal').innerHTML = `${icon(revealed ? 'eye-off' : 'eye')}<span class="btn-label">${revealed ? '隐藏明文' : '显示明文'}</span>`;
  $('#compare-reveal').setAttribute('aria-pressed', String(revealed));
  $('#compare-note').textContent = revealed
    ? '正在显示明文；失焦即收回。对比不会写入新版本。'
    : '值默认掩码。显示明文只作用于本屏，失焦即收回。';

  const delta = charDelta(leftValue, rightValue);
  const same = leftValue === rightValue;
  const multiline = leftValue.includes('\n') || rightValue.includes('\n');
  const maxLen = Math.max(leftValue.length, rightValue.length);
  const summary = $('#compare-summary');

  if (multiline) {
    const rows = diffLines(leftValue, rightValue);
    const adds = rows.filter((r) => r.kind === 'add').length;
    const dels = rows.filter((r) => r.kind === 'del').length;
    const countBits = [
      adds ? `<span class="diff-add-count">+${adds} 行</span>` : '',
      dels ? `<span class="diff-del-count">−${dels} 行</span>` : '',
    ].join('');
    summary.innerHTML = `<span>${same ? '两个版本的值相同' : '逐行对照'}</span>${countBits}
      <span class="mono">${fmt(leftMeta?.at)} → ${fmt(rightMeta?.at)}</span>`;
    $('#compare-body').innerHTML = `<div class="diff-box">
      <div class="diff-box-head"><span>行级对照</span><span>${revealed ? '明文' : '掩码'}</span></div>
      <div class="diff-lines">${rows.map((row) => {
        const sign = row.kind === 'add' ? '+' : row.kind === 'del' ? '−' : '';
        const text = revealed ? (row.text || '（空行）') : `第 ${row.no} 行 · ${(row.text || '').length} 字符`;
        return `<div class="diff-line ${row.kind}"><span class="sign">${sign}</span><span class="ln">${row.no}</span><span class="text">${esc(text)}</span></div>`;
      }).join('')}</div>
    </div>`;
  } else {
    const addChars = delta.newMid.length;
    const delChars = delta.oldMid.length;
    const countBits = [
      addChars ? `<span class="diff-add-count">+${addChars} 字符</span>` : '',
      delChars ? `<span class="diff-del-count">−${delChars} 字符</span>` : '',
    ].join('');
    summary.innerHTML = `<span>${same ? '两个版本的值相同' : '整值对照'}</span>${countBits}
      <span class="mono">${fmt(leftMeta?.at)} → ${fmt(rightMeta?.at)}</span>`;
    const leftBody = revealed
      ? renderChangedValue(leftValue, delta, 'old')
      : `<span class="seal-cipher">${dotsFor(leftValue.length)}</span>`;
    const rightBody = revealed
      ? renderChangedValue(rightValue, delta, 'new')
      : `<span class="seal-cipher">${dotsFor(rightValue.length)}</span>`;
    $('#compare-body').innerHTML = `<div class="value-compare">
      <section class="value-side" aria-label="基准版本 v${left}">
        <div class="value-side-head"><span class="mono">v${left}</span><span>${esc(leftMeta?.actor || '')}</span></div>
        <div class="value-side-body">${leftBody}</div>
        <div class="value-side-foot">${leftValue.length} 字符 · ${esc(fmt(leftMeta?.at).slice(11))}</div>
      </section>
      <div class="value-arrow" aria-hidden="true">${icon('arrow-right')}</div>
      <section class="value-side new" aria-label="对比版本 v${right}">
        <div class="value-side-head"><span class="mono">v${right}</span><span>${esc(rightMeta?.actor || '')}</span></div>
        <div class="value-side-body">${rightBody}</div>
        <div class="value-side-foot">${rightValue.length} 字符 · ${esc(fmt(rightMeta?.at).slice(11))}</div>
      </section>
    </div>`;
  }
}

function diffLines(a, b) {
  const A = a.split('\n');
  const B = b.split('\n');
  const dp = Array.from({ length: A.length + 1 }, () => new Array(B.length + 1).fill(0));
  for (let i = A.length - 1; i >= 0; i -= 1) {
    for (let j = B.length - 1; j >= 0; j -= 1) {
      dp[i][j] = A[i] === B[j] ? dp[i + 1][j + 1] + 1 : Math.max(dp[i + 1][j], dp[i][j + 1]);
    }
  }
  const out = [];
  let i = 0;
  let j = 0;
  let oldNo = 1;
  let newNo = 1;
  while (i < A.length && j < B.length) {
    if (A[i] === B[j]) {
      out.push({ kind: 'same', text: A[i], no: oldNo });
      i += 1; j += 1; oldNo += 1; newNo += 1;
    } else if (dp[i + 1][j] >= dp[i][j + 1]) {
      out.push({ kind: 'del', text: A[i], no: oldNo });
      i += 1; oldNo += 1;
    } else {
      out.push({ kind: 'add', text: B[j], no: newNo });
      j += 1; newNo += 1;
    }
  }
  while (i < A.length) { out.push({ kind: 'del', text: A[i], no: oldNo }); i += 1; oldNo += 1; }
  while (j < B.length) { out.push({ kind: 'add', text: B[j], no: newNo }); j += 1; newNo += 1; }
  return out;
}

function swapCompare() {
  if (!state.compare) return;
  const { left, right } = state.compare;
  state.compare.left = right;
  state.compare.right = left;
  renderCompare();
}

function changeCompareSide(side, value) {
  if (!state.compare) return;
  const target = Number(value);
  const otherKey = side === 'left' ? 'right' : 'left';
  if (target === state.compare[otherKey]) {
    const list = current().versions.slice().sort((a, b) => a.version - b.version).map((v) => v.version);
    const idx = list.indexOf(target);
    const alt = list[idx + 1] ?? list[idx - 1];
    if (alt) {
      state.compare[otherKey] = alt;
      showFlash('基准与对比不能是同一版本，已调整另一侧');
    }
  }
  state.compare[side] = target;
  renderCompare();
}

/* ───────────────────────── 轮换章节 ───────────────────────── */
function renderRotation() {
  const s = current();
  if (!s) return;
  const actions = $('#rotation-actions');
  const region = $('#policy-region');
  const p = s.policy;

  if (state.policyEditing) {
    actions.innerHTML = '';
    region.innerHTML = policyFormHTML(s);
    bindPolicyForm();
    return;
  }

  if (!p) {
    actions.innerHTML = '';
    region.innerHTML = `<div class="chapter-empty">
      <span class="chapter-empty-mark">${icon('clock')}</span>
      <div class="chapter-empty-body">
        <p class="chapter-empty-title">这把密钥还没有轮换计划</p>
        <p class="chapter-empty-text">到期时 taboo 只创建待办与通知，不会擅自替换外部真源。设置周期、负责人和回写方式后，轮次会记录在这里。</p>
        <div class="lifecycle" aria-label="轮换生命周期">
          <span class="step"><b>到期</b> 创建轮次</span><span class="arrow">→</span>
          <span class="step"><b>生成 / 回写</b> 产生新版本</span><span class="arrow">→</span>
          <span class="step"><b>确认分发</b> 本轮完成</span>
        </div>
        <div class="btn-row"><button type="button" class="btn btn-default" data-policy-edit>${icon('clock')}<span class="btn-label">设置轮换策略</span></button></div>
      </div>
    </div>`;
    return;
  }

  const canPause = p.status === 'active';
  const canResume = p.status === 'paused';
  const fixed = p.status === 'fixed';
  const openRun = s.runs.find((run) => OPEN_RUN.has(run.status));
  actions.innerHTML = `
    ${statusPill(p.status)}
    ${openRun ? statusPill(openRun.status) : ''}
    ${!fixed && p.status !== 'draft' ? `<button type="button" class="btn btn-default btn-sm" data-run-now>${icon('play')}<span class="btn-label">立即运行</span></button>` : ''}
    <button type="button" class="btn btn-default btn-sm" data-policy-edit>${icon('pencil')}<span class="btn-label">${fixed ? '改为轮换' : '编辑策略'}</span></button>
    ${canPause ? `<button type="button" class="btn btn-quiet btn-sm" data-policy-pause>${icon('clock')}<span class="btn-label">暂停</span></button>` : ''}
    ${canResume ? `<button type="button" class="btn btn-quiet btn-sm" data-policy-resume>${icon('play')}<span class="btn-label">恢复</span></button>` : ''}
    ${!fixed ? `<button type="button" class="btn btn-quiet btn-sm" data-policy-fix>${icon('lock')}<span class="btn-label">标记固定</span></button>` : ''}
  `;

  const nextText = fixed ? '没有下次' : p.status === 'paused' ? '已暂停' : p.status === 'draft' ? '未启用' : p.cron ? '按 cron' : p.nextAt ? fmt(p.nextAt).slice(0, 19) : '—';
  const metaBits = [];
  if (fixed) {
    if (p.fixedReason) metaBits.push(`原因 ${esc(p.fixedReason)}`);
    if (p.fixedBy) metaBits.push(`由 <span class="mono">${esc(p.fixedBy)}</span> 标记`);
  } else {
    if (p.owner) metaBits.push(`负责人 <span class="mono">${esc(p.owner)}</span>`);
    metaBits.push(`宽限 ${p.graceDays} 天`);
    if (p.lastAt) metaBits.push(`上次 <span class="mono">${fmt(p.lastAt).slice(0, 10)}</span>`);
  }
  if (openRun) metaBits.push(`本轮 ${statusPill(openRun.status)}`);

  const summary = `<div class="policy-summary">
    <div class="policy-cell"><span class="k">方式</span><span class="v">${MODE_LABEL[p.mode]}</span></div>
    <div class="policy-cell"><span class="k">周期</span><span class="v">${periodLabel(p)}</span></div>
    <div class="policy-cell"><span class="k">下次</span><span class="v mono">${nextText}</span></div>
  </div>
  <div class="policy-meta">${metaBits.join('<span aria-hidden="true">·</span>')}</div>`;

  const runs = s.runs.length
    ? `<div class="policy-section">
        <div class="policy-section-head"><h3>最近轮次</h3><p>到期、生成、分发、完成都留痕；旧版本不自动删除。</p></div>
        <div class="run-list">
          <div class="run-head"><span>到期</span><span>状态</span><span>版本</span><span>触发</span><span style="text-align:right">操作</span></div>
          ${s.runs.map((run) => runRowHTML(run)).join('')}
        </div>
      </div>`
    : `<div class="policy-section"><div class="policy-section-head"><h3>最近轮次</h3><p>还没有轮次；到期后会在这里出现。</p></div></div>`;

  const note = fixed
    ? '固定后不会创建新轮次，也不会提醒；历史轮次保留。改为轮换后会重新计算下次时间。'
    : '轮换只负责创建待办、生成或提醒；外部真源仍要在上游更新。确认分发后本轮才算完成。';
  region.innerHTML = summary + runs + `<p class="policy-note">${note}</p>`;
}

function runRowHTML(run) {
  const open = state.openRuns.has(run.id);
  const canRetry = run.status === 'failed' || run.status === 'overdue';
  const canComplete = ['awaiting_generation', 'awaiting_distribution', 'awaiting_manual', 'awaiting_external'].includes(run.status);
  const version = `v${run.oldVersion} → ${run.newVersion ? 'v' + run.newVersion : '—'}`;
  return `<div class="run-row">
      <span class="run-due" title="${esc(fmt(run.dueAt))}">${esc(fmt(run.dueAt))}</span>
      <span class="run-status">${statusPill(run.status)}</span>
      <span class="run-ver">${version}</span>
      <span class="run-trigger">${TRIGGER_LABEL[run.trigger] || run.trigger}</span>
      <span class="run-act"><button type="button" class="btn btn-quiet btn-sm" data-run-toggle="${run.id}" aria-expanded="${open}" aria-controls="run-detail-${run.id}" aria-label="${open ? '收起' : '查看'}轮次 ${esc(run.id)} 详情">${open ? '收起' : '详情'}</button></span>
    </div>
    ${open ? `<div class="run-expand" id="run-detail-${run.id}" role="region" aria-label="轮次 ${esc(run.id)} 详情">
      <div><span class="k">到期</span><span class="v mono">${esc(fmt(run.dueAt))}</span></div>
      <div><span class="k">负责人</span><span class="v">${esc(run.owner || '未指派')}</span></div>
      <div><span class="k">尝试</span><span class="v mono">${run.attempts}</span></div>
      <div><span class="k">轮次</span><span class="v mono">${esc(run.id)}</span></div>
      <div><span class="k">旧版本</span><span class="v mono">v${run.oldVersion}${run.newVersion ? ' → v' + run.newVersion : ''}</span></div>
      <div><span class="k">备注</span><span class="v ${run.error ? 'run-error' : ''}">${esc(run.error || run.note || '—')}</span></div>
      ${(canRetry || canComplete) ? `<div class="run-expand-actions">
        ${canRetry ? `<button type="button" class="btn btn-default btn-sm" data-run-retry="${run.id}" aria-label="重试轮次 ${esc(run.id)}">${icon('refresh')}<span class="btn-label">重试</span></button>` : ''}
        ${canComplete ? `<button type="button" class="btn btn-default btn-sm" data-run-complete="${run.id}" aria-label="完成轮次 ${esc(run.id)}">${icon('check')}<span class="btn-label">完成本轮</span></button>` : ''}
      </div>` : ''}
    </div>` : ''}`;
}

function policyFormHTML(s) {
  const p = s.policy || {
    mode: 'webhook', intervalHours: 720, cron: '', graceDays: 7, owner: '林岚',
    profile: 'alnum32', autoGenerate: false,
  };
  const kind = p.cron ? 'cron' : 'interval';
  return `<form class="form-stack policy-form" id="policy-form" novalidate>
    <div class="form-row">
      <div class="field">
        <label class="field-label" for="policy-mode">方式</label>
        <select id="policy-mode">
          <option value="generate" ${p.mode === 'generate' ? 'selected' : ''}>生成并写入（taboo 作为真源）</option>
          <option value="manual" ${p.mode === 'manual' ? 'selected' : ''}>人工轮换</option>
          <option value="webhook" ${p.mode === 'webhook' ? 'selected' : ''}>外部 Webhook</option>
        </select>
      </div>
      <div class="field">
        <label class="field-label" for="policy-kind">周期</label>
        <select id="policy-kind">
          <option value="interval" ${kind === 'interval' ? 'selected' : ''}>固定间隔（按计划节拍）</option>
          <option value="cron" ${kind === 'cron' ? 'selected' : ''}>cron（UTC）</option>
        </select>
      </div>
    </div>
    <div class="form-row">
      <div class="field" id="interval-field" ${kind === 'interval' ? '' : 'hidden'}>
        <label class="field-label" for="policy-hours">间隔（小时，至少 1）</label>
        <input id="policy-hours" type="number" min="1" step="1" value="${p.intervalHours || 720}">
      </div>
      <div class="field" id="cron-field" ${kind === 'cron' ? '' : 'hidden'}>
        <label class="field-label" for="policy-cron">cron（五段 UTC）</label>
        <input id="policy-cron" class="mono" value="${esc(p.cron || '0 3 * * *')}" placeholder="0 3 * * *">
      </div>
      <div class="field">
        <label class="field-label" for="policy-grace">宽限期（天）</label>
        <input id="policy-grace" type="number" min="0" max="30" value="${p.graceDays ?? 7}">
      </div>
      <div class="field">
        <label class="field-label" for="policy-owner">负责人</label>
        <select id="policy-owner">
          ${['林岚', '陈一', '未指派'].map((name) => `<option ${p.owner === name || (!p.owner && name === '未指派') ? 'selected' : ''}>${name}</option>`).join('')}
        </select>
      </div>
      <div class="policy-preview" id="policy-preview" aria-live="polite">
        <span class="k">保存并启用后</span>
        <p class="v" id="policy-preview-next">下次 —</p>
        <p class="hint" id="policy-preview-mode">到期只创建轮次与通知，不会自动替换上游。</p>
      </div>
    </div>
    <div id="gen-fields" ${p.mode === 'generate' ? '' : 'hidden'}>
      <div class="form-row">
        <div class="field">
          <label class="field-label" for="policy-profile">生成档位</label>
          <select id="policy-profile">
            <option value="alnum32" ${p.profile === 'alnum32' || !p.profile ? 'selected' : ''}>字母数字 32 位（默认）</option>
            <option value="alnum64" ${p.profile === 'alnum64' ? 'selected' : ''}>字母数字 64 位</option>
            <option value="base64url32" ${p.profile === 'base64url32' ? 'selected' : ''}>base64url 32 字节</option>
            <option value="hex32" ${p.profile === 'hex32' ? 'selected' : ''}>hex 32 字节</option>
            <option value="password24" ${p.profile === 'password24' ? 'selected' : ''}>密码 24–32 位</option>
          </select>
        </div>
        <div class="field">
          <span class="field-label">自动生成</span>
          <label class="check" style="margin-top:5px">
            <input type="checkbox" id="policy-auto" ${p.autoGenerate ? 'checked' : ''}>
            <span class="check-text">到期自动生成<span class="field-hint">默认关闭；消费方必须支持热重载。</span></span>
          </label>
        </div>
      </div>
    </div>
    <p class="field-hint">保存草稿不会启用；保存并启用才计算下次时间。到期只创建轮次和通知。</p>
    <p class="form-error" id="policy-error" role="alert" hidden></p>
    <div class="form-actions split">
      <div class="btn-row">
        ${s.policy ? `<button type="button" class="btn btn-quiet" data-policy-delete>${icon('trash')}<span class="btn-label">删除策略</span></button>` : ''}
      </div>
      <div class="btn-row">
        <button type="button" class="btn btn-quiet" data-policy-cancel>取消</button>
        <button type="button" class="btn btn-default" data-policy-save="draft">保存草稿</button>
        <button type="button" class="btn btn-primary" data-policy-save="active">${icon('seal')}<span class="btn-label">保存并启用</span></button>
      </div>
    </div>
  </form>`;
}

function bindPolicyForm() {
  const form = $('#policy-form');
  if (!form) return;
  $('#policy-mode').addEventListener('change', (e) => {
    $('#gen-fields').hidden = e.target.value !== 'generate';
    updatePolicyPreview();
  });
  $('#policy-kind').addEventListener('change', (e) => {
    const cron = e.target.value === 'cron';
    $('#interval-field').hidden = cron;
    $('#cron-field').hidden = !cron;
    updatePolicyPreview();
  });
  $('#policy-hours').addEventListener('input', updatePolicyPreview);
  form.addEventListener('submit', (event) => {
    event.preventDefault();
    savePolicy('active');
  });
  $$('[data-policy-cancel]', form).forEach((b) => b.addEventListener('click', () => {
    state.policyEditing = false;
    renderRotation();
    $('#tab-rotation')?.focus();
  }));
  $$('[data-policy-save]', form).forEach((b) => b.addEventListener('click', () => savePolicy(b.dataset.policySave)));
  const del = $('[data-policy-delete]', form);
  if (del) del.addEventListener('click', openDeletePolicyDialog);
  updatePolicyPreview();
  requestAnimationFrame(() => $('#policy-mode')?.focus());
}

function updatePolicyPreview() {
  const kind = $('#policy-kind')?.value;
  const mode = $('#policy-mode')?.value;
  const hours = Number($('#policy-hours')?.value || 0);
  const next = $('#policy-preview-next');
  const hint = $('#policy-preview-mode');
  if (!next) return;
  if (kind === 'cron') {
    next.innerHTML = '下次 <span class="mono">由 cron 计算</span>';
  } else if (!Number.isFinite(hours) || hours < 1) {
    next.innerHTML = '<span class="faint">间隔至少 1 小时</span>';
  } else {
    next.innerHTML = `下次 <span class="mono">${fmt(addHours(hours)).slice(0, 16)}</span>`;
  }
  if (hint) {
    hint.textContent = mode === 'webhook'
      ? '到期只创建轮次与通知，等待外部回写。'
      : mode === 'manual'
        ? '到期只创建待办，由负责人人工完成。'
        : '到期生成新值并写入新版本；仍需确认分发。';
  }
}

function showPolicyError(text) {
  const el = $('#policy-error');
  if (!el) return;
  el.textContent = text;
  el.hidden = !text;
}

function savePolicy(status) {
  const s = current();
  if (!s) return;
  const mode = $('#policy-mode').value;
  const kind = $('#policy-kind').value;
  const hours = Number($('#policy-hours').value || 0);
  const cron = $('#policy-cron').value.trim();
  const grace = Number($('#policy-grace').value || 0);
  const owner = $('#policy-owner').value === '未指派' ? '' : $('#policy-owner').value;
  if (kind === 'interval' && (!Number.isFinite(hours) || hours < 1)) {
    showPolicyError('间隔至少 1 小时。轮换靠节拍，不接受 0。');
    $('#policy-hours').focus();
    return;
  }
  if (kind === 'cron' && cron.split(/\s+/).filter(Boolean).length !== 5) {
    showPolicyError('cron 必须恰好五段，例如 0 3 * * *。');
    $('#policy-cron').focus();
    return;
  }
  showPolicyError('');
  const previous = s.policy;
  const next = {
    status,
    mode,
    intervalHours: kind === 'interval' ? hours : 0,
    cron: kind === 'cron' ? cron : '',
    graceDays: Number.isFinite(grace) ? grace : 0,
    owner,
    profile: mode === 'generate' ? $('#policy-profile').value : '',
    autoGenerate: mode === 'generate' ? $('#policy-auto').checked : false,
    lastAt: previous?.lastAt || null,
    nextAt: status === 'active' && kind === 'interval' ? addHours(hours) : null,
  };
  s.policy = next;
  state.policyEditing = false;
  addTrace('policy', '改策略', `轮换策略 → ${status === 'active' ? '启用 · ' + (next.cron ? 'cron ' + next.cron : periodLabel(next)) : '草稿'}`, 'rotation.policy.update');
  renderRotationMeta();
  renderRotation();
  renderRecord();
  showNotice('rotation', status === 'active' ? `已保存并启用。${next.cron ? '下次由 cron 计算。' : '下次 ' + fmt(next.nextAt).slice(0, 16) + '。'}` : '已保存草稿，不会提醒或生成。', 'ok');
  $('#tab-rotation')?.focus();
}

function setPolicyStatus(status) {
  const s = current();
  if (!s || !s.policy) return;
  s.policy.status = status;
  if (status === 'active') {
    s.policy.nextAt = s.policy.cron ? null : addHours(s.policy.intervalHours || 720);
    s.policy.lastAt = s.policy.lastAt || nowIso();
  } else if (status === 'paused') {
    s.policy.nextAt = null;
  }
  addTrace('policy', '改策略', status === 'active' ? '恢复轮换' : '暂停轮换', 'rotation.policy.update');
  renderRotationMeta();
  renderRotation();
  renderRecord();
  showNotice('rotation', status === 'active' ? '已恢复轮换。' : '已暂停：不会提醒或生成。', 'ok');
  $('#tab-rotation')?.focus();
}

function runNow() {
  const s = current();
  if (!s || !s.policy) return;
  const status = s.policy.mode === 'generate' ? 'awaiting_generation' : s.policy.mode === 'webhook' ? 'awaiting_external' : 'awaiting_manual';
  const run = {
    id: 'run_' + Date.now().toString(36).slice(-6),
    dueAt: nowIso(),
    status,
    oldVersion: s.versions[0].version,
    newVersion: null,
    trigger: 'manual',
    owner: s.policy.owner,
    attempts: 0,
    note: '手动创建的轮次；上报或回写后完成分发。',
  };
  s.runs.unshift(run);
  state.openRuns.add(run.id);
  addTrace('rotation', '轮换', `手动创建轮次 · ${statusLabel(status)}`, 'rotation.run.create');
  renderRotationMeta();
  renderRotation();
  showNotice('rotation', '已创建轮次；本轮不会自动替换密钥，生成或回写后再确认分发。', 'ok');
  requestAnimationFrame(() => {
    const toggle = $(`[data-run-toggle="${run.id}"]`);
    toggle?.focus({ preventScroll: false });
  });
}

function runRetry(id) {
  const s = current();
  if (!s) return;
  const run = s.runs.find((r) => r.id === id);
  if (!run) return;
  run.status = s.policy?.mode === 'webhook' ? 'awaiting_external' : 'awaiting_manual';
  run.attempts += 1;
  run.error = '';
  addTrace('rotation', '轮换', `重试轮次 ${run.id}`, 'rotation.run.retry');
  renderRotationMeta();
  renderRotation();
  showNotice('rotation', '已重试：轮次回到待回写。', 'ok');
  requestAnimationFrame(() => $(`[data-run-toggle="${id}"]`)?.focus());
}

function runComplete(id) {
  const s = current();
  if (!s) return;
  const run = s.runs.find((r) => r.id === id);
  if (!run) return;
  run.status = 'succeeded';
  run.newVersion = s.versions[0].version;
  if (s.policy) {
    s.policy.lastAt = nowIso();
    if (s.policy.status === 'active' && !s.policy.cron) s.policy.nextAt = addHours(s.policy.intervalHours || 720);
  }
  addTrace('rotation', '轮换', `完成轮次 ${run.id} · v${run.newVersion}`, 'rotation.run.complete');
  renderRotationMeta();
  renderRotation();
  showNotice('rotation', '本轮已完成；下一轮按策略重新计时。', 'ok');
  requestAnimationFrame(() => $(`[data-run-toggle="${id}"]`)?.focus());
}

function openMarkFixedDialog() {
  const s = current();
  if (!s || !s.policy) return;
  openDialog({
    title: '标记为固定？',
    body: `<p class="dialog-lede">标记后没有下次轮换，也不会提醒。</p>
      <p>密钥本身、版本历史和已有轮次都不变；只是不再把它当作需要轮换的密钥。之后可以「改为轮换」。</p>
      <form class="form-stack" id="fixed-form" novalidate>
        <div class="field">
          <label class="field-label" for="fixed-reason">固定原因（必填）</label>
          <input id="fixed-reason" data-autofocus placeholder="例如：厂商托管，无法轮换" maxlength="120">
        </div>
        <p class="form-error" id="fixed-error" role="alert" hidden></p>
        <div class="form-actions">
          <button type="button" class="btn btn-quiet" data-dialog-cancel>取消</button>
          <button type="submit" class="btn btn-stamp">${icon('seal')}<span class="btn-label">标记固定</span></button>
        </div>
      </form>`,
    onMount: (body) => {
      $('#fixed-form', body).addEventListener('submit', (event) => {
        event.preventDefault();
        const reason = $('#fixed-reason', body).value.trim();
        const error = $('#fixed-error', body);
        if (!reason) {
          error.textContent = '请填写固定原因；它会让后来的人知道为什么不再轮换。';
          error.hidden = false;
          $('#fixed-reason', body).focus();
          return;
        }
        s.policy.status = 'fixed';
        s.policy.fixedReason = reason;
        s.policy.fixedBy = '云中鹤';
        s.policy.nextAt = null;
        addTrace('policy', '改策略', `标记固定 · ${reason}`, 'rotation.policy.fixed');
        closeDialog();
        renderRotationMeta();
        renderRotation();
        renderRecord();
        showNotice('rotation', '已标记固定：没有下次轮换，也不会提醒。', 'ok');
        $('#tab-rotation')?.focus();
      });
    },
  });
}

function openDeletePolicyDialog() {
  openDialog({
    title: '删除轮换策略？',
    body: `<p class="dialog-lede">删除后不会再计算下次时间，也不会提醒。</p>
      <p>历史轮次保留；密钥本身和版本不受影响。之后可以重新设置。</p>
      <div class="form-actions">
        <button type="button" class="btn btn-quiet" data-dialog-cancel>取消</button>
        <button type="button" class="btn btn-danger" id="confirm-delete-policy">删除策略</button>
      </div>`,
    onMount: (body) => {
      $('#confirm-delete-policy', body).addEventListener('click', () => {
        const s = current();
        if (!s) return;
        s.policy = null;
        state.policyEditing = false;
        addTrace('policy', '改策略', '删除轮换策略', 'rotation.policy.delete');
        closeDialog();
        renderRotationMeta();
        renderRotation();
        renderRecord();
        showNotice('rotation', '轮换策略已删除；版本没有被改动。', 'ok');
        $('#tab-rotation')?.focus();
      });
    },
  });
}

/* ───────────────────────── 痕迹 ───────────────────────── */
function traceKindLabel(kind) {
  return { write: '写入', reveal: '显示', rollback: '回滚', rotation: '轮换', policy: '改策略', deny: '拒绝' }[kind] || kind;
}

function traceCategory(kind) {
  if (kind === 'reveal') return 'reveal';
  if (kind === 'write' || kind === 'rollback') return 'write';
  if (kind === 'rotation' || kind === 'policy') return 'rotation';
  return 'other';
}

function renderTraces() {
  const s = current();
  if (!s) return;
  $('#count-traces').textContent = s.traces.length;
  const counts = {
    all: s.traces.length,
    reveal: s.traces.filter((t) => traceCategory(t.kind) === 'reveal').length,
    write: s.traces.filter((t) => traceCategory(t.kind) === 'write').length,
    rotation: s.traces.filter((t) => traceCategory(t.kind) === 'rotation').length,
  };
  const filters = [
    ['all', '全部'], ['reveal', '显示'], ['write', '写入'], ['rotation', '轮换'],
  ];
  $('#trace-filters').innerHTML = filters.map(([key, label]) =>
    `<button type="button" class="chip" data-filter="${key}" aria-pressed="${state.traceFilter === key}">${label} <span class="n">${counts[key]}</span></button>`).join('');

  const filtered = state.traceFilter === 'all' ? s.traces : s.traces.filter((t) => traceCategory(t.kind) === state.traceFilter);
  const visible = filtered.slice(0, state.traceLimit);
  const remaining = filtered.length - visible.length;
  if (!visible.length) {
    $('#trace-list').innerHTML = `<p class="trace-empty">这个筛选下还没有记录。显示明文或写入一次后，痕迹会立即出现在这里。</p>`;
  } else {
    const groups = [];
    for (const trace of visible) {
      const key = day(trace.at);
      const last = groups[groups.length - 1];
      if (last && last.key === key) last.rows.push(trace);
      else groups.push({ key, rows: [trace] });
    }
    const groupsHtml = groups.map((group) => `<section class="trace-day" aria-label="${esc(group.key)}">
      <div class="trace-day-head"><span>${esc(group.key)}</span><span>${group.rows.length} 条</span></div>
      ${group.rows.map((trace) => `<div class="trace-row" title="${esc(trace.action || '')}">
        <span class="trace-time">${esc(fmt(trace.at).slice(11))}</span>
        <span class="trace-act ${esc(trace.kind)}">${traceKindLabel(trace.kind)}</span>
        <span class="trace-actor" title="${esc(trace.actor)}">${esc(trace.actor)}</span>
        <span class="trace-desc">${esc(trace.desc)}</span>
        <span class="trace-ip">${esc(trace.ip || '')}</span>
      </div>`).join('')}
    </section>`).join('');
    const more = remaining
      ? `<div class="trace-more"><button type="button" class="btn btn-quiet btn-sm" data-trace-more>显示更早的 ${remaining} 条</button></div>`
      : '';
    $('#trace-list').innerHTML = groupsHtml + more;
  }

  $('#traces-foot').innerHTML = `只显示 <span class="mono" style="color:var(--porcelain-dim)">project/payments/secret/${esc(s.key)}</span> 的 ${s.traces.length} 条记录；组织全量审计在「审计日志」。`;
}

function addTrace(kind, label, desc, action) {
  const s = current();
  if (!s) return;
  s.traces.unshift({ at: nowIso(), actor: '云中鹤', kind, label, desc, action: action || '', ip: '127.0.0.1' });
  $('#count-traces').textContent = s.traces.length;
  if (state.chapter === 'traces') renderTraces();
  if (state.chapter === 'versions') renderRecentReads();
}

/* ───────────────────────── 章节切换 ───────────────────────── */
function switchChapter(chapter, { focus = false } = {}) {
  if (!state.currentKey) return;
  if (chapter !== 'versions' && state.compare) state.compare = null;
  state.chapter = chapter;
  $$('.chapter-tab').forEach((tab) => {
    const on = tab.dataset.chapter === chapter;
    tab.setAttribute('aria-selected', String(on));
    tab.tabIndex = on ? 0 : -1;
  });
  $$('.chapter').forEach((panel) => { panel.hidden = panel.id !== 'chapter-' + chapter; });
  if (chapter === 'versions') {
    $('#versions-view').hidden = Boolean(state.compare);
    $('#compare-view').hidden = !state.compare;
    if (state.compare) renderCompare();
    else renderVersions();
  }
  if (chapter === 'rotation') renderRotation();
  if (chapter === 'traces') renderTraces();
  if (focus) $(`.chapter-tab[data-chapter="${chapter}"]`)?.focus();
}

/* ───────────────────────── 回执 / 通知 ───────────────────────── */
const noticeTimers = {};
function showNotice(which, text, tone = 'ok') {
  const el = document.getElementById(which + '-notice');
  if (!el) return;
  el.setAttribute('role', tone === 'danger' ? 'alert' : 'status');
  el.innerHTML = `<p class="notice notice-${tone}">${esc(text)}</p>`;
  clearTimeout(noticeTimers[which]);
  noticeTimers[which] = setTimeout(() => { el.innerHTML = ''; }, 7000);
}

let flashTimer = null;
function showFlash(text) {
  const dialog = $('#flash-dialog');
  const el = $('#flash-text');
  if (!dialog || !el) return;
  if ($('#app-dialog').open) return;
  el.textContent = text;
  if (!dialog.open) {
    try { dialog.show(); } catch { return; }
  }
  clearTimeout(flashTimer);
  flashTimer = setTimeout(() => { if (dialog.open) dialog.close(); }, 1900);
}

/* ───────────────────────── 弹窗 ───────────────────────── */
function openDialog({ title, body, onMount }) {
  const dialog = $('#app-dialog');
  $('#dialog-title').textContent = title;
  const bodyEl = $('#dialog-body');
  bodyEl.innerHTML = body;
  hydrateIcons(bodyEl);
  if (!dialog.open) dialog.showModal();
  dialog.scrollTop = 0;
  onMount?.(bodyEl, dialog);
  requestAnimationFrame(() => {
    const first = bodyEl.querySelector('[data-autofocus]') || bodyEl.querySelector('input:not([disabled]):not([readonly]), textarea, select, button:not([disabled])');
    first?.focus({ preventScroll: true });
  });
}

function closeDialog() {
  const dialog = $('#app-dialog');
  if (dialog.open) dialog.close();
}

function openEditDialog() {
  const s = current();
  if (!s) return;
  const nextVersion = s.versions[0].version + 1;
  openDialog({
    title: '编辑密钥',
    body: `<form class="form-stack" id="edit-form" novalidate>
      <div class="field">
        <label class="field-label" for="edit-key">Key</label>
        <input id="edit-key" class="mono" value="${esc(s.key)}" readonly>
        <span class="field-hint">Key 即定位符的一段，创建后不可改名 · 位置 payments/${esc(s.folder)}</span>
      </div>
      <div class="field">
        <label class="field-label" for="edit-value">新的值</label>
        <textarea id="edit-value" class="mono resize-none" rows="4" spellcheck="false" required data-autofocus placeholder="输入新的值；当前值不会预填到这里"></textarea>
        <span class="field-hint">保存会产生 v${nextVersion}；当前明文不进入这个表单，旧版本保留。</span>
      </div>
      <div class="field">
        <label class="field-label" for="edit-comment">备注</label>
        <input id="edit-comment" value="${esc(s.comment)}">
      </div>
      <div class="field">
        <label class="field-label" for="edit-tags">标签（逗号分隔）</label>
        <input id="edit-tags" value="${esc(s.tags.join(', '))}">
      </div>
      <p class="form-error" id="edit-error" role="alert" hidden></p>
      <div class="form-actions">
        <button type="button" class="btn btn-quiet" data-dialog-cancel>取消</button>
        <button type="submit" class="btn btn-primary">${icon('seal')}<span class="btn-label">保存（产生新版本）</span></button>
      </div>
    </form>`,
    onMount: (body) => {
      $('#edit-form', body).addEventListener('submit', (event) => {
        event.preventDefault();
        const value = $('#edit-value').value;
        const error = $('#edit-error');
        if (!value.trim()) {
          error.textContent = '值不能为空。空值不是一次有效写入。';
          error.hidden = false;
          $('#edit-value').focus();
          return;
        }
        error.hidden = true;
        const version = s.versions[0].version + 1;
        s.versions.unshift({ version, value, actor: '云中鹤', at: nowIso() });
        s.versionValues[version] = value;
        s.value = value;
        s.updated = s.versions[0].at;
        s.comment = $('#edit-comment').value.trim();
        s.tags = $('#edit-tags').value.split(',').map((t) => t.trim()).filter(Boolean);
        addTrace('write', '写入', `编辑并写入 v${version}`, 'secrets.write');
        closeDialog();
        state.revealed = false;
        state.compare = null;
        renderRecord();
        renderList();
        showNotice('versions', `已保存为 v${version}；旧版本保留。`, 'ok');
        $('#tab-versions')?.focus();
      });
    },
  });
}

function openRollbackDialog(version) {
  const s = current();
  if (!s) return;
  const next = s.versions[0].version + 1;
  openDialog({
    title: `回滚到 v${version}？`,
    body: `<p class="dialog-lede">将 v${version} 的值写成 v${next}，旧版本全部保留。</p>
      <p>回滚不是删除，也不会让旧值失效；消费方需要重新读取。这次动作会进入审计链。</p>
      <div class="form-actions">
        <button type="button" class="btn btn-quiet" data-dialog-cancel>取消</button>
        <button type="button" class="btn btn-stamp" id="confirm-rollback">${icon('seal')}<span class="btn-label">回滚到此</span></button>
      </div>`,
    onMount: (body) => {
      $('#confirm-rollback', body).addEventListener('click', () => {
        const value = s.versionValues[version];
        const versionNumber = s.versions[0].version + 1;
        s.versions.unshift({ version: versionNumber, value, actor: '云中鹤', at: nowIso() });
        s.versionValues[versionNumber] = value;
        s.value = value;
        s.updated = s.versions[0].at;
        addTrace('rollback', '回滚', `回滚到 v${version} → v${versionNumber}`, 'secrets.rollback');
        closeDialog();
        state.revealed = false;
        state.compare = null;
        renderRecord();
        renderList();
        showNotice('versions', `已回滚到 v${version}，产生 v${versionNumber}。`, 'ok');
        $('#tab-versions')?.focus();
      });
    },
  });
}

function openDeleteSecretDialog(key) {
  const s = state.secrets.find((item) => item.key === key);
  if (!s) return;
  openDialog({
    title: `删除 ${s.key}？`,
    body: `<form class="form-stack" id="delete-form" novalidate>
      <p class="dialog-lede">这会移除 ${s.policy ? `${s.versions.length} 个版本与轮换记录` : `${s.versions.length} 个版本`}。审计痕迹保留，但密钥本身不可恢复。</p>
      <div class="dialog-confirm">
        <label class="field-label" for="delete-confirm">输入 <span class="mono">${esc(s.key)}</span> 以确认</label>
        <input id="delete-confirm" autocomplete="off" spellcheck="false" data-autofocus placeholder="${esc(s.key)}">
      </div>
      <div class="form-actions">
        <button type="button" class="btn btn-quiet" data-dialog-cancel>取消</button>
        <button type="submit" class="btn btn-danger" id="confirm-delete" disabled>删除密钥</button>
      </div>
    </form>`,
    onMount: (body) => {
      const input = $('#delete-confirm', body);
      const confirm = $('#confirm-delete', body);
      input.addEventListener('input', () => { confirm.disabled = input.value !== s.key; });
      $('#delete-form', body).addEventListener('submit', (event) => {
        event.preventDefault();
        if (input.value !== s.key) return;
        const wasOpen = state.currentKey === s.key;
        state.secrets = state.secrets.filter((item) => item.key !== s.key);
        closeDialog();
        if (wasOpen) {
          const hadState = Boolean(history.state && history.state.record);
          hideRecord({ focus: false });
          try {
            if (hadState) history.back();
            else history.replaceState(null, '', location.pathname + location.search);
          } catch { /* ignore */ }
          $('#list-title')?.focus?.();
        }
        renderList();
        showFlash(`已删除 ${s.key} · 审计痕迹仍保留`);
        if (!wasOpen) requestAnimationFrame(() => $('#list-title')?.focus({ preventScroll: true }));
      });
    },
  });
}

/* ───────────────────────── 事件绑定 ───────────────────────── */
function bindEvents() {
  const goSecrets = () => {
    if (state.currentKey) closeRecord();
    else window.scrollTo({ top: 0, behavior: 'smooth' });
  };
  $('#rail-secrets').addEventListener('click', goSecrets);
  $('#mobile-secrets').addEventListener('click', goSecrets);

  $('#record-back').addEventListener('click', closeRecord);
  $('#record-edit').addEventListener('click', openEditDialog);
  $('#seal-reveal').addEventListener('click', () => void toggleReveal());
  $('#seal-copy').addEventListener('click', () => void copyCurrent());
  $('#delete-secret').addEventListener('click', () => {
    if (state.currentKey) openDeleteSecretDialog(state.currentKey);
  });

  $$('.chapter-tab').forEach((tab) => {
    tab.addEventListener('click', () => switchChapter(tab.dataset.chapter));
  });
  $('.chapters').addEventListener('keydown', (event) => {
    const keys = ['ArrowLeft', 'ArrowRight', 'Home', 'End'];
    if (!keys.includes(event.key)) return;
    event.preventDefault();
    const tabs = $$('.chapter-tab');
    const index = tabs.indexOf(document.activeElement);
    let next = index;
    if (event.key === 'ArrowLeft') next = (index - 1 + tabs.length) % tabs.length;
    if (event.key === 'ArrowRight') next = (index + 1) % tabs.length;
    if (event.key === 'Home') next = 0;
    if (event.key === 'End') next = tabs.length - 1;
    switchChapter(tabs[next].dataset.chapter, { focus: true });
  });

  $('#compare-open').addEventListener('click', () => openCompare());
  $('#compare-back').addEventListener('click', closeCompare);
  $('#compare-back-2').addEventListener('click', closeCompare);
  $('#compare-swap').addEventListener('click', swapCompare);
  $('#compare-left').addEventListener('change', (e) => changeCompareSide('left', e.target.value));
  $('#compare-right').addEventListener('change', (e) => changeCompareSide('right', e.target.value));
  $('#compare-reveal').addEventListener('click', () => {
    if (!state.compare) return;
    state.compare.revealed = !state.compare.revealed;
    renderCompare();
    $('#compare-reveal').focus();
  });

  $('#record-view').addEventListener('click', (event) => {
    if (event.target.closest('[data-go-rotation]')) {
      switchChapter('rotation', { focus: true });
      $('#chapter-rotation').scrollIntoView({ block: 'start', behavior: 'smooth' });
      return;
    }
    if (event.target.closest('[data-go-traces]')) {
      switchChapter('traces', { focus: true });
      $('#chapter-traces').scrollIntoView({ block: 'start', behavior: 'smooth' });
    }
  });

  $('#version-list').addEventListener('click', (event) => {
    const compare = event.target.closest('[data-compare-from]');
    if (compare) { openCompare(Number(compare.dataset.compareFrom)); return; }
    const rollback = event.target.closest('[data-rollback]');
    if (rollback) openRollbackDialog(Number(rollback.dataset.rollback));
  });

  $('#rotation-actions').addEventListener('click', (event) => {
    if (event.target.closest('[data-run-now]')) { runNow(); return; }
    if (event.target.closest('[data-policy-edit]')) {
      state.policyEditing = true;
      renderRotation();
      const first = $('#policy-mode');
      first?.focus();
      return;
    }
    if (event.target.closest('[data-policy-pause]')) { setPolicyStatus('paused'); return; }
    if (event.target.closest('[data-policy-resume]')) { setPolicyStatus('active'); return; }
    if (event.target.closest('[data-policy-fix]')) openMarkFixedDialog();
  });

  $('#policy-region').addEventListener('click', (event) => {
    if (event.target.closest('[data-policy-edit]')) {
      state.policyEditing = true;
      renderRotation();
      $('#policy-mode')?.focus();
      return;
    }
    const toggle = event.target.closest('[data-run-toggle]');
    if (toggle) {
      const id = toggle.dataset.runToggle;
      if (state.openRuns.has(id)) state.openRuns.delete(id); else state.openRuns.add(id);
      renderRotation();
      requestAnimationFrame(() => $(`[data-run-toggle="${id}"]`)?.focus());
      return;
    }
    const retry = event.target.closest('[data-run-retry]');
    if (retry) { runRetry(retry.dataset.runRetry); return; }
    const complete = event.target.closest('[data-run-complete]');
    if (complete) runComplete(complete.dataset.runComplete);
  });

  $('#trace-filters').addEventListener('click', (event) => {
    const chip = event.target.closest('[data-filter]');
    if (!chip) return;
    state.traceFilter = chip.dataset.filter;
    state.traceLimit = 8;
    renderTraces();
    requestAnimationFrame(() => $(`[data-filter="${state.traceFilter}"]`)?.focus());
  });

  $('#trace-list').addEventListener('click', (event) => {
    if (!event.target.closest('[data-trace-more]')) return;
    state.traceLimit += 20;
    renderTraces();
    requestAnimationFrame(() => {
      const more = $('[data-trace-more]');
      if (more) more.focus();
      else $('#tab-traces')?.focus();
    });
  });

  $('#secret-search').addEventListener('input', (event) => {
    state.search = event.target.value;
    renderList();
  });

  $('#list-view').addEventListener('click', (event) => {
    const folder = event.target.closest('[data-folder]');
    if (folder) { setFolder(folder.dataset.folder); return; }
    const tree = event.target.closest('[data-tree-folder]');
    if (tree) { setFolder(tree.dataset.treeFolder); return; }
    const clear = event.target.closest('[data-clear-search]');
    if (clear) {
      state.search = '';
      $('#secret-search').value = '';
      renderList();
      $('#secret-search').focus();
    }
  });

  $('#secret-list-region').addEventListener('click', (event) => {
    const open = event.target.closest('[data-open]');
    if (open) { openRecord(open.dataset.open); return; }
    const action = event.target.closest('[data-act]');
    if (!action) return;
    const key = action.dataset.key;
    const secret = state.secrets.find((item) => item.key === key);
    if (!secret) return;
    if (action.dataset.act === 'reveal') openRecord(key, { reveal: true });
    if (action.dataset.act === 'copy') void copyValue(secret.value, '已复制');
    if (action.dataset.act === 'edit') { openRecord(key); setTimeout(openEditDialog, 0); }
    if (action.dataset.act === 'delete') openDeleteSecretDialog(key);
  });

  $('#ctx-button').addEventListener('click', () => {
    const menu = $('#ctx-menu');
    const open = menu.hidden;
    menu.hidden = !open;
    $('#ctx-button').setAttribute('aria-expanded', String(open));
  });
  document.addEventListener('pointerdown', (event) => {
    const menu = $('#ctx-menu');
    if (!menu || menu.hidden) return;
    if (!event.target.closest('.appbar-ctx')) {
      menu.hidden = true;
      $('#ctx-button').setAttribute('aria-expanded', 'false');
    }
  });
  document.addEventListener('click', (event) => {
    const proto = event.target.closest('[data-proto]');
    if (proto) {
      event.preventDefault();
      if (proto.closest('#ctx-menu')) {
        $('#ctx-menu').hidden = true;
        $('#ctx-button').setAttribute('aria-expanded', 'false');
      }
      showFlash(proto.dataset.proto);
    }
  });

  $('#dialog-close').addEventListener('click', closeDialog);
  $('#app-dialog').addEventListener('click', (event) => {
    if (event.target.closest('[data-dialog-cancel]')) closeDialog();
  });

  window.addEventListener('blur', () => {
    if (!state.currentKey) return;
    if (state.revealed) {
      state.revealed = false;
      renderSeal();
    }
    if (state.compare && state.compare.revealed) {
      state.compare.revealed = false;
      renderCompare();
    }
  });

  window.addEventListener('popstate', () => {
    const key = keyFromHash();
    if (key) {
      if (state.currentKey !== key) openRecord(key, { push: false });
    } else {
      hideRecord({ focus: true });
    }
  });

  document.addEventListener('keydown', (event) => {
    if (event.key !== 'Escape') return;
    if ($('#app-dialog').open) return;
    const menu = $('#ctx-menu');
    if (menu && !menu.hidden) {
      menu.hidden = true;
      $('#ctx-button').setAttribute('aria-expanded', 'false');
      return;
    }
    if (state.policyEditing) {
      event.preventDefault();
      state.policyEditing = false;
      renderRotation();
      $('#tab-rotation')?.focus();
      return;
    }
    if (state.compare) {
      event.preventDefault();
      closeCompare();
      return;
    }
    if (!$('#record-view').hidden) {
      event.preventDefault();
      closeRecord();
    }
  });
}

function keyFromHash() {
  const match = /^#\/secret\/(.+)$/.exec(location.hash);
  return match ? decodeURIComponent(match[1]) : null;
}

/* ───────────────────────── 启动 ───────────────────────── */
function boot() {
  state.secrets = SEED.map(normalizeSecret);
  hydrateIcons(document);
  bindEvents();
  renderList();

  const initialKey = keyFromHash();
  if (initialKey && state.secrets.some((s) => s.key === initialKey)) {
    openRecord(initialKey, { push: false, initial: true });
    try { history.replaceState(null, '', '#/secret/' + encodeURIComponent(initialKey)); } catch { /* ignore */ }
  } else {
    openRecord('STRIPE_SECRET_KEY', { push: false, initial: true });
    try { history.replaceState(null, '', '#/secret/STRIPE_SECRET_KEY'); } catch { /* ignore */ }
  }
}

boot();
