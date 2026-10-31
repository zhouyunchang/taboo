// 浏览器回归（1280 / 390）：登录 → 密钥表 → 版本标识 → 本地时区时间 → 无横向溢出。
//
//   echo '{"email":"…","password":"…"}' | TABOO_BASE=https://taboo-dev122.swetcc.com \
//     [TABOO_HOST_MAP='taboo-dev122.swetcc.com 10.0.30.122'] [OUT=/tmp/shots] node scripts/browser-check.mjs
//
// 凭据只从 stdin 读取，不落盘。会以该账号在默认项目 dev 环境创建一个 BROWSER_CHECK_<ts>
// 密钥用于渲染检查，结束时删除。浏览器时区固定为 Asia/Shanghai，用于验证 UTC → 本地换算。
import { chromium } from 'playwright';
import { mkdirSync } from 'node:fs';

const BASE = (process.env.TABOO_BASE || 'http://localhost:7199').replace(/\/$/, '');
const OUT = process.env.OUT || '';
const TZ = process.env.TABOO_TZ || 'Asia/Shanghai';
const creds = JSON.parse(await new Promise((res) => { let d = ''; process.stdin.on('data', (c) => (d += c)); process.stdin.on('end', () => res(d)); }));
let failures = 0;
const check = (name, ok, extra = '') => { if (ok) console.log(`  ✓ ${name}`); else { failures++; console.error(`  ✗ ${name} ${extra}`); } };

const args = process.env.TABOO_HOST_MAP ? [`--host-resolver-rules=MAP ${process.env.TABOO_HOST_MAP}`] : [];
const browser = await chromium.launch({ args });
// API 调用在浏览器页面内 fetch（APIRequestContext 走 Node 解析器，不认 --host-resolver-rules）
const apiCtx = await browser.newContext();
const apiPage = await apiCtx.newPage();
await apiPage.goto(BASE + '/api/v1/healthz');
const call = (method, path, { token, body } = {}) => apiPage.evaluate(async ({ url, method, token, body }) => {
  const r = await fetch(url, { method, headers: { ...(token ? { Authorization: `Bearer ${token}` } : {}), ...(body ? { 'Content-Type': 'application/json' } : {}) }, body: body ? JSON.stringify(body) : undefined });
  return { status: r.status, data: await r.json().catch(() => ({})) };
}, { url: BASE + path, method, token, body });

const health = (await call('GET', '/api/v1/healthz')).data;
check('healthz reports version', !!health.version && !!health.commit, JSON.stringify(health));
const li = await call('POST', '/api/v1/auth/login', { body: creds });
const token = li.data?.tokens?.access;
check('api login', !!token, `status=${li.status}`);
const me = (await call('GET', '/api/v1/me', { token })).data;
const slug = me.orgs?.[0]?.slug;
const projectId = (await call('GET', `/api/v1/orgs/${slug}/projects`, { token })).data?.projects?.[0]?.id;
const KEY = `BROWSER_CHECK_${Date.now()}`;
const created = await call('POST', `/api/v1/projects/${projectId}/secrets`, { token, body: { key: KEY, value: 'browser-check', tags: ['check'] } });
check('seed secret', created.data?.version === 1, JSON.stringify(created.data));
const meta = (await call('GET', `/api/v1/projects/${projectId}/secrets?env=dev`, { token })).data?.secrets?.find((s) => s.key === KEY);
const fmt = (d) => new Intl.DateTimeFormat('sv-SE', { timeZone: TZ, year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }).format(d);
const expectTime = meta ? fmt(new Date(meta.updated_at.replace(' ', 'T') + 'Z')) : '';

try {
  for (const [label, width, height] of [['desktop', 1280, 800], ['narrow', 390, 844]]) {
    console.log(`-- ${label} ${width}px`);
    const ctx = await browser.newContext({ viewport: { width, height }, timezoneId: TZ, locale: 'zh-CN' });
    const page = await ctx.newPage();
    await page.goto(BASE + '/');
    const ver = page.getByTestId('server-version');
    await ver.waitFor({ timeout: 15000 });
    check('login page shows version', (await ver.textContent())?.includes(`v${health.version} · ${health.commit}`), await ver.textContent());
    if (OUT) { mkdirSync(OUT, { recursive: true }); await page.screenshot({ path: `${OUT}/${label}-login.png`, fullPage: true }); }
    await page.locator('input[type=email]').fill(creds.email);
    await page.locator('input[type=password]').fill(creds.password);
    await page.locator('button[type=submit]').click();
    const row = page.locator('.secrets-table tr', { hasText: KEY });
    await row.waitFor({ timeout: 20000 });
    check('workspace shows version', (await page.getByTestId('server-version').textContent())?.includes(health.commit));
    const timeText = (await row.locator('td[data-label="更新时间"]').textContent())?.trim();
    check(`更新时间 in ${TZ} (${expectTime})`, timeText === expectTime, `got ${timeText}`);
    const layout = await page.evaluate(() => {
      const vw = document.documentElement.clientWidth;
      // 按钮须同时落在视口和列表滚动容器的可见区域内（容器 overflow:auto 时按钮可能被裁切而页面不溢出）
      const table = document.querySelector('.secrets-table');
      const box = table.parentElement.getBoundingClientRect();
      const right = Math.min(vw, box.right), left = Math.max(0, box.left);
      const buttons = [...document.querySelectorAll('.secrets-table td.actions button')].map((b) => b.getBoundingClientRect());
      return { vw, sw: document.documentElement.scrollWidth, containerOverflow: table.parentElement.scrollWidth - table.parentElement.clientWidth, right, left, maxRight: Math.max(0, ...buttons.map((r) => r.right)), minLeft: Math.min(vw, ...buttons.map((r) => r.left)), n: buttons.length };
    });
    check('no page-level horizontal overflow', layout.sw <= layout.vw, JSON.stringify(layout));
    check('action buttons fully visible', layout.n > 0 && layout.maxRight <= layout.right + 0.5 && layout.minLeft >= layout.left - 0.5, JSON.stringify(layout));
    check('secrets list has no horizontal scroll', layout.containerOverflow <= 1, JSON.stringify(layout));
    if (OUT) await page.screenshot({ path: `${OUT}/${label}-workspace.png`, fullPage: true });
    await ctx.close();
  }
} finally {
  const del = await call('DELETE', `/api/v1/projects/${projectId}/secrets/${KEY}?env=dev&expectedVersion=1`, { token });
  check('cleanup seeded secret', del.status === 200 || del.status === 204, `status=${del.status}`);
  await browser.close();
}
console.log(failures ? `\nFAILED: ${failures} check(s)` : '\nALL BROWSER CHECKS PASSED');
process.exit(failures ? 1 : 0);
