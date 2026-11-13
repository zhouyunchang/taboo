#!/usr/bin/env node
// 双主题视觉证据 + 机械验收（不修改产品代码）。
// 用法：
//   node scripts/capture-theme.mjs [--themes=dark,light] [--viewports=desktop,mobile] [--checks=1|0]
// env：
//   CONSOLE_URL  已登录夹具（默认 http://127.0.0.1:7301，FIXTURE_MODE=new）
//   LOGIN_URL    登录页夹具（默认 http://127.0.0.1:7303，FIXTURE_MODE=login）
//   OUT_DIR      默认 .impeccable/review/theme
// 前置：npm run build -w @taboo/web + 两个 fixture-server 已在跑。
// 产物：OUT_DIR/{theme}/{desktop|mobile}-{surface}.jpg
//       OUT_DIR/capture-manifest.json（截图清单 + pageerror + 5xx）
//       OUT_DIR/theme-checks.json（持久化 / 切换控件 / 无闪 / 390px 溢出 / 页面错误）
import { chromium } from 'playwright';
import fs from 'node:fs';
import path from 'node:path';

const ROOT = process.cwd();
const OUT = path.resolve(process.env.OUT_DIR || '.impeccable/review/theme');
const CONSOLE_URL = (process.env.CONSOLE_URL || 'http://127.0.0.1:7301').replace(/\/$/, '');
const LOGIN_URL = (process.env.LOGIN_URL || 'http://127.0.0.1:7303').replace(/\/$/, '');
const argOf = (name, fallback) => {
  const hit = process.argv.slice(2).find((a) => a.startsWith(`--${name}=`));
  return hit ? hit.slice(name.length + 3) : fallback;
};
const THEMES = argOf('themes', process.env.THEMES || 'dark,light').split(',').map((s) => s.trim()).filter(Boolean);
const VIEWPORT_NAMES = argOf('viewports', process.env.VIEWPORTS || 'desktop,mobile').split(',').map((s) => s.trim()).filter(Boolean);
const RUN_CHECKS = argOf('checks', process.env.CHECKS || '1') !== '0';
const CHECKS_ONLY = argOf('checks-only', process.env.CHECKS_ONLY || '0') === '1';
const VP = { desktop: { width: 1440, height: 900 }, mobile: { width: 390, height: 844 } };
const STORAGE_KEY = 'taboo:theme';

const manifest = {
  generatedAt: new Date().toISOString(),
  consoleUrl: CONSOLE_URL,
  loginUrl: LOGIN_URL,
  themes: THEMES,
  viewports: VIEWPORT_NAMES,
  browser: '',
  shots: [],
  notes: [],
};
const checks = [];
fs.mkdirSync(OUT, { recursive: true });

const rel = (file) => path.relative(ROOT, file).split(path.sep).join('/');

// 主题只能通过 localStorage 注入：addInitScript 在文档脚本之前执行（每个导航都执行一次）。
function injectTheme(theme) {
  try { window.localStorage.setItem('taboo:theme', theme); } catch { /* 隐私模式 */ }
}

async function settle(page, selector, extra = 600) {
  if (selector) {
    await page.locator(selector).first().waitFor({ state: 'visible', timeout: 15000 }).catch(() => {});
  }
  await page.waitForLoadState('networkidle', { timeout: 2500 }).catch(() => {});
  await page.waitForTimeout(extra);
}

/** 用整页截图 + clip 裁区域：元素自身背景可能是半透明，element.screenshot 会丢背景。 */
async function clipShot(page, locator, file) {
  const box = await locator.first().boundingBox();
  if (!box) return false;
  await page.screenshot({
    path: file,
    clip: { x: Math.max(0, Math.floor(box.x)), y: Math.max(0, Math.floor(box.y)), width: Math.ceil(box.width), height: Math.ceil(box.height) },
    animations: 'disabled',
  });
  return true;
}

async function gotoConsole(page) {
  await page.goto(`${CONSOLE_URL}/`, { waitUntil: 'domcontentloaded' });
  await page.locator('.appbar').first().waitFor({ state: 'visible', timeout: 15000 });
  await page.locator('button.secret-key-btn').first().waitFor({ state: 'visible', timeout: 15000 }).catch(() => {});
  await page.waitForLoadState('networkidle', { timeout: 2500 }).catch(() => {});
  await page.waitForTimeout(600);
}

/** 桌面走侧栏 .rail，移动走底栏 .mobile-nav。夹具当前空间是个人空间，设置页可能叫「个人设置」。 */
async function navClick(page, mobile, labels) {
  const scope = mobile ? '.mobile-nav' : '.rail';
  for (const label of labels) {
    const btn = page.locator(`${scope} button`).filter({ hasText: label }).first();
    if (await btn.count()) {
      await btn.click();
      await page.waitForTimeout(600);
      await page.waitForLoadState('networkidle', { timeout: 2000 }).catch(() => {});
      return label;
    }
  }
  return null;
}

async function captureThemeViewport(browser, theme, vpName) {
  const vp = VP[vpName];
  if (!vp) throw new Error(`未知视口 ${vpName}`);
  const mobile = vpName === 'mobile';
  const dir = path.join(OUT, theme);
  fs.mkdirSync(dir, { recursive: true });

  const ctx = await browser.newContext({
    viewport: vp, deviceScaleFactor: 2, locale: 'zh-CN', timezoneId: 'Asia/Shanghai',
    colorScheme: theme, // 显式主题与系统偏好配对，避免默认 system 的 light 干扰注入结果
  });
  await ctx.addInitScript(injectTheme, theme);
  await ctx.grantPermissions(['clipboard-read', 'clipboard-write']).catch(() => {});
  const page = await ctx.newPage();

  let surface = 'init';
  let errs = [];
  let net5xx = [];
  const global = { pageErrors: [], http5xx: [], consoleErrors: [] };

  page.on('pageerror', (e) => {
    const rec = { theme, viewport: vpName, surface, message: e.message };
    errs.push(rec); global.pageErrors.push(rec);
  });
  page.on('response', (r) => {
    if (r.status() >= 500) {
      const rec = { theme, viewport: vpName, surface, status: r.status(), method: r.request().method(), url: r.url() };
      net5xx.push(rec); global.http5xx.push(rec);
    }
  });
  page.on('console', (m) => {
    if (m.type() !== 'error') return;
    const text = m.text();
    // 资源 404 是夹具缺端点（与主题无关），单独标记不当作 JS 错误
    const rec = { theme, viewport: vpName, surface, message: text, resource: /Failed to load resource/i.test(text) };
    global.consoleErrors.push(rec);
  });

  async function shot(name, note = '') {
    surface = name;
    const file = path.join(dir, `${vpName}-${name}.jpg`);
    await page.waitForTimeout(500);
    await page.screenshot({ path: file, animations: 'disabled' });
    manifest.shots.push({
      theme, viewport: vpName, surface: name, file: rel(file), url: page.url(), note,
      pageErrors: errs.slice(), http5xx: net5xx.slice(),
    });
    errs = []; net5xx = [];
    console.log(`  ✓ ${theme}/${vpName}-${name}.jpg`);
  }

  // ---------- 控制台默认面 ----------
  surface = 'console-secrets';
  await gotoConsole(page);
  await shot('console-secrets', '密钥列表（默认页）');

  // 顶栏裁剪：主题开关必须在这里看得清
  surface = 'appbar';
  const appbarFile = path.join(dir, `${vpName}-appbar.jpg`);
  await clipShot(page, page.locator('.appbar'), appbarFile);
  const themeGroup = page.locator('.appbar [role="group"][aria-label="界面主题"]');
  const hasControl = (await themeGroup.count()) > 0;
  manifest.shots.push({
    theme, viewport: vpName, surface: 'appbar', file: rel(appbarFile), url: page.url(),
    note: '顶栏区域裁剪（含主题开关）', hasThemeControl: hasControl, pageErrors: [], http5xx: [],
  });
  console.log(`  ✓ ${theme}/${vpName}-appbar.jpg${hasControl ? '' : '（未见主题开关）'}`);
  if (hasControl) {
    const ctlFile = path.join(dir, `${vpName}-theme-control.jpg`);
    await clipShot(page, themeGroup, ctlFile);
    manifest.shots.push({ theme, viewport: vpName, surface: 'theme-control', file: rel(ctlFile), note: '主题开关裁剪', pageErrors: [], http5xx: [] });
    console.log(`  ✓ ${theme}/${vpName}-theme-control.jpg`);
  } else {
    manifest.notes.push(`${theme}/${vpName}: appbar 内没有 role=group[aria-label=界面主题]`);
  }
  // 顶栏动作区（含主题开关）：桌面与移动各一张，便于核对控件比例
  surface = 'appbar-actions';
  const actions = page.locator('.appbar-actions').first();
  if (await actions.count()) {
    const actFile = path.join(dir, `${vpName}-appbar-actions.jpg`);
    await clipShot(page, actions, actFile);
    manifest.shots.push({ theme, viewport: vpName, surface: 'appbar-actions', file: rel(actFile), note: '.appbar-actions 裁剪（含主题开关）', pageErrors: [], http5xx: [] });
    console.log(`  ✓ ${theme}/${vpName}-appbar-actions.jpg`);
  } else {
    manifest.notes.push(`${theme}/${vpName}: 未找到 .appbar-actions`);
  }

  const currentTheme = await page.evaluate(() => ({
    theme: document.documentElement.dataset.theme,
    mode: document.documentElement.dataset.themeMode,
    stored: (() => { try { return window.localStorage.getItem('taboo:theme'); } catch { return null; } })(),
  }));
  manifest.shots.push({ theme, viewport: vpName, surface: 'theme-state', note: '控制台首屏主题状态', state: currentTheme, pageErrors: [], http5xx: [] });

  // ---------- 显示明文 ----------
  surface = 'console-revealed';
  const eye = page.getByRole('button', { name: '显示', exact: true }).first();
  if (await eye.count()) {
    await eye.click();
    await page.waitForTimeout(650);
    await shot('console-revealed', '点击「显示」后的明文');
    await page.getByRole('button', { name: '隐藏', exact: true }).first().click().catch(() => {});
    await page.waitForTimeout(300);
  } else {
    manifest.notes.push(`${theme}/${vpName}: 未找到「显示」按钮，跳过 console-revealed`);
  }

  // ---------- 引用 chip（--amber-strong）：cloud 文件夹 UPSTREAM_API 的值含 ${...} ----------
  surface = 'console-ref';
  const cloudFolder = page.locator('.tree-label').filter({ hasText: 'cloud' }).first();
  if (await cloudFolder.count()) {
    await cloudFolder.click();
    await page.waitForTimeout(700);
    const refEye = page.getByRole('button', { name: '显示', exact: true }).first();
    if (await refEye.count()) {
      await refEye.click().catch(() => {});
      await page.waitForTimeout(650);
      await page.locator('.ref-chip').first().waitFor({ timeout: 4000 }).catch(() => {});
      if (await page.locator('.ref-chip').first().count()) {
        await shot('console-ref', 'cloud/UPSTREAM_API 显示后的引用 chip（--amber-strong）');
      } else {
        manifest.notes.push(`${theme}/${vpName}: 显示后未出现 .ref-chip，跳过 console-ref`);
      }
    }
    // 回根目录，后续卷宗仍取第一行
    const rootTree = page.locator('.tree-label').filter({ hasText: 'core-api' }).first();
    if (await rootTree.count()) {
      await rootTree.click();
      await page.waitForTimeout(600);
      await page.waitForLoadState('networkidle', { timeout: 2000 }).catch(() => {});
    }
  } else {
    manifest.notes.push(`${theme}/${vpName}: 文件夹树没有 cloud，跳过 console-ref`);
  }

  // ---------- 密钥卷宗 ----------
  surface = 'record';
  const keyBtn = page.locator('button.secret-key-btn').first();
  if ((await keyBtn.count()) && (await keyBtn.isVisible().catch(() => false))) {
    await keyBtn.click();
    await page.waitForTimeout(700);
    await page.waitForLoadState('networkidle', { timeout: 2000 }).catch(() => {});
    await shot('record', '密钥卷宗（点第一个密钥名）');
  } else {
    manifest.notes.push(`${theme}/${vpName}: 未找到 .secret-key-btn，跳过 record`);
  }
  // 回列表
  await navClick(page, mobile, ['密钥']).catch(() => {});
  await page.waitForTimeout(400);

  // ---------- 新建密钥弹窗 ----------
  surface = 'dialog';
  const newBtn = page.locator('button[aria-label="＋ 新建密钥"]').first();
  if ((await newBtn.count()) && (await newBtn.isVisible().catch(() => false))) {
    await newBtn.click();
    await page.locator('dialog.modal[open]').first().waitFor({ timeout: 8000 }).catch(() => {});
    await page.waitForTimeout(500);
    await shot('dialog', '＋ 新建密钥 弹窗');
    await page.keyboard.press('Escape');
    await page.waitForTimeout(400);
  } else {
    manifest.notes.push(`${theme}/${vpName}: 未找到「＋ 新建密钥」，跳过 dialog`);
  }

  // ---------- 审计日志 ----------
  surface = 'audit';
  const auditLabel = await navClick(page, mobile, ['审计日志', '审计']);
  if (auditLabel) {
    await page.locator('.audit-row, .table').first().waitFor({ timeout: 10000 }).catch(() => {});
    await page.waitForTimeout(500);
    await shot('audit', '审计日志');
  } else {
    manifest.notes.push(`${theme}/${vpName}: 侧栏/底栏没有审计入口，跳过 audit（当前角色可能无权限）`);
  }

  // ---------- 设置 ----------
  surface = 'settings';
  const settingsLabel = await navClick(page, mobile, ['组织设置', '个人设置', '设置']);
  if (settingsLabel) {
    await page.locator('.settings-panel, .settings').first().waitFor({ timeout: 10000 }).catch(() => {});
    await page.waitForTimeout(500);
    await shot('settings', `设置（入口「${settingsLabel}」）`);
  } else {
    manifest.notes.push(`${theme}/${vpName}: 没有设置入口，跳过 settings`);
  }

  // ---------- 组织页（SPA 路由 /organizations） ----------
  surface = 'organizations';
  await page.goto(`${CONSOLE_URL}/organizations`, { waitUntil: 'domcontentloaded' });
  await page.waitForTimeout(1000);
  await page.waitForLoadState('networkidle', { timeout: 3000 }).catch(() => {});
  await shot('organizations', '/organizations 组织页');

  // ---------- CLI 同意页（SPA 路由 /device） ----------
  surface = 'cli-consent';
  await page.goto(`${CONSOLE_URL}/device`, { waitUntil: 'domcontentloaded' });
  await page.locator('.once-secret').first().waitFor({ timeout: 15000 }).catch(() => {});
  await page.waitForTimeout(700);
  if (await page.locator('.once-secret').first().count()) {
    await shot('cli-consent', '命令行登录确认 /device');
  } else {
    manifest.notes.push(`${theme}/${vpName}: /device 未渲染 .once-secret，跳过 cli-consent`);
  }

  // ---------- 登录页（独立夹具 7303） ----------
  surface = 'login';
  await page.goto(`${LOGIN_URL}/`, { waitUntil: 'domcontentloaded' });
  await page.locator('.auth-card, .auth-shell').first().waitFor({ timeout: 15000 }).catch(() => {});
  await page.waitForTimeout(800);
  if (await page.locator('.auth-card').first().count()) {
    await shot('login', '登录页（FIXTURE_MODE=login）');
    // 登录页也有主题开关：录一下状态
    const loginState = await page.evaluate(() => ({
      theme: document.documentElement.dataset.theme,
      mode: document.documentElement.dataset.themeMode,
      stored: (() => { try { return window.localStorage.getItem('taboo:theme'); } catch { return null; } })(),
    }));
    manifest.shots.push({ theme, viewport: vpName, surface: 'login-state', note: '登录页主题状态', state: loginState, pageErrors: [], http5xx: [] });
  } else {
    manifest.notes.push(`${theme}/${vpName}: LOGIN_URL(${LOGIN_URL}) 没渲染登录页；登录夹具是否以 FIXTURE_MODE=login 启动？`);
  }

  await ctx.close();
  return global;
}

// ============================================================================
// 机械验收
// ============================================================================
function parseRgb(str) {
  if (!str) return null;
  if (str === 'transparent') return { r: 0, g: 0, b: 0, a: 0 };
  const m = str.match(/rgba?\(([^)]+)\)/);
  if (!m) return null;
  const parts = m[1].split(/[,\s/]+/).filter(Boolean).map(Number);
  return { r: parts[0] || 0, g: parts[1] || 0, b: parts[2] || 0, a: parts.length > 3 ? parts[3] : 1 };
}
function luminance({ r, g, b }) {
  const f = (v) => { v /= 255; return v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4; };
  return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b);
}

function addCheck(c) {
  checks.push({ id: c.id, category: c.category, theme: c.theme ?? null, viewport: c.viewport ?? null, pass: !!c.pass, expected: c.expected, actual: c.actual, detail: c.detail ?? '' });
}

function checkDistIndex() {
  const file = path.join(ROOT, 'apps/web/dist/index.html');
  if (!fs.existsSync(file)) {
    addCheck({ id: 'dist-index-exists', category: 'no-flash', pass: false, expected: 'apps/web/dist/index.html 存在', actual: '缺失' });
    return;
  }
  const html = fs.readFileSync(file, 'utf8');
  const inlineBootstrap = html.search(/<script(?![^>]*\bsrc=)[^>]*>[\s\S]*?data-theme/);
  const moduleScript = html.search(/<script[^>]*type="module"/);
  const stylesheet = html.search(/<link[^>]*rel="stylesheet"/);
  const htmlAttr = /<html[^>]*data-theme="(dark|light)"/.exec(html);
  const orderPass = inlineBootstrap >= 0 && moduleScript > inlineBootstrap && (stylesheet < 0 || stylesheet > inlineBootstrap);
  addCheck({
    id: 'dist-inline-bootstrap-first', category: 'no-flash', pass: orderPass,
    expected: '内联 data-theme bootstrap 早于 <script type="module"> 与样式 <link rel=stylesheet>',
    actual: `inline@${inlineBootstrap}, module@${moduleScript}, stylesheet@${stylesheet}`,
    detail: 'dist/index.html（vite build 产物）静态顺序；首帧前落好 data-theme 才可能无闪。',
  });
  addCheck({
    id: 'dist-html-default-theme', category: 'no-flash', pass: !!htmlAttr,
    expected: '<html> 上有初始 data-theme（无 JS 时也有确定底色）',
    actual: htmlAttr ? `<html data-theme="${htmlAttr[1]}">` : '未找到',
  });
}

async function openWithTheme(browser, { theme, colorScheme }) {
  const ctx = await browser.newContext({
    viewport: { width: 1440, height: 900 }, deviceScaleFactor: 1, locale: 'zh-CN',
    timezoneId: 'Asia/Shanghai', colorScheme,
  });
  if (theme !== undefined) await ctx.addInitScript(injectTheme, theme);
  const page = await ctx.newPage();
  return { ctx, page };
}

async function themeState(page) {
  return page.evaluate(() => {
    const root = document.documentElement;
    const bodyBg = getComputedStyle(document.body).backgroundColor;
    return {
      theme: root.dataset.theme ?? null,
      mode: root.dataset.themeMode ?? null,
      stored: (() => { try { return window.localStorage.getItem('taboo:theme'); } catch { return null; } })(),
      bodyBg,
      htmlBg: getComputedStyle(root).backgroundColor,
    };
  });
}

function bgMatchesTheme(bg, theme) {
  const c = parseRgb(bg);
  if (!c) return { ok: false, why: `无法解析背景色 ${bg}` };
  const L = luminance(c);
  if (theme === 'dark') return { ok: L <= 0.25, why: `相对亮度 ${L.toFixed(3)}（暗色应 ≤0.25）` };
  return { ok: L >= 0.45, why: `相对亮度 ${L.toFixed(3)}（亮色应 ≥0.45）` };
}

async function runChecks(browser) {
  checkDistIndex();

  // ---- 持久化：注入 localStorage 后刷新仍是该主题 ----
  for (const theme of ['dark', 'light']) {
    const { ctx, page } = await openWithTheme(browser, { theme, colorScheme: theme === 'dark' ? 'light' : 'dark' });
    try {
      await gotoConsole(page);
      const before = await themeState(page);
      await page.reload({ waitUntil: 'domcontentloaded' });
      await page.locator('.appbar').first().waitFor({ timeout: 15000 }).catch(() => {});
      await page.waitForTimeout(500);
      const after = await themeState(page);
      const bg = bgMatchesTheme(after.bodyBg, theme);
      addCheck({
        id: `persist-${theme}-reload`, category: 'persistence', theme,
        pass: before.theme === theme && after.theme === theme && after.stored === theme && bg.ok,
        expected: `data-theme=${theme}，localStorage=${theme}，背景属于${theme === 'dark' ? '暗' : '亮'}色`,
        actual: `before=${JSON.stringify(before)} after=${JSON.stringify(after)}；${bg.why}`,
      });
    } catch (e) {
      addCheck({ id: `persist-${theme}-reload`, category: 'persistence', theme, pass: false, expected: '控制台可加载并保持主题', actual: `异常：${e.message}` });
    } finally { await ctx.close(); }
  }

  // ---- system 模式跟随 prefers-color-scheme；空 localStorage 默认 system ----
  for (const colorScheme of ['dark', 'light']) {
    const expected = colorScheme;
    // 显式存 system
    {
      const { ctx, page } = await openWithTheme(browser, { theme: 'system', colorScheme });
      try {
        await gotoConsole(page);
        const s = await themeState(page);
        const bg = bgMatchesTheme(s.bodyBg, expected);
        addCheck({
          id: `system-${colorScheme}-follows`, category: 'system', theme: 'system', viewport: 'desktop',
          pass: s.theme === expected && s.mode === 'system' && s.stored === 'system' && bg.ok,
          expected: `localStorage=system + 系统 ${colorScheme} → data-theme=${expected}、data-theme-mode=system`,
          actual: `${JSON.stringify(s)}；${bg.why}`,
        });
      } catch (e) {
        addCheck({ id: `system-${colorScheme}-follows`, category: 'system', theme: 'system', pass: false, expected: '跟随系统', actual: `异常：${e.message}` });
      } finally { await ctx.close(); }
    }
    // localStorage 为空（默认 system）
    {
      const { ctx, page } = await openWithTheme(browser, { colorScheme });
      try {
        await gotoConsole(page);
        const s = await themeState(page);
        addCheck({
          id: `default-system-${colorScheme}`, category: 'system', theme: null, viewport: 'desktop',
          pass: s.theme === expected && s.stored === null && (s.mode === 'system' || s.mode === null),
          expected: `localStorage 为空 + 系统 ${colorScheme} → 解析为 ${expected}（mode=system）`,
          actual: JSON.stringify(s),
        });
      } catch (e) {
        addCheck({ id: `default-system-${colorScheme}`, category: 'system', pass: false, expected: '默认 system', actual: `异常：${e.message}` });
      } finally { await ctx.close(); }
    }
  }

  // ---- 切换控件：存在、aria-pressed、点击生效、Tab 可达 ----
  for (const vpName of ['desktop', 'mobile']) {
    const { ctx, page } = await openWithTheme(browser, { theme: 'system', colorScheme: 'dark' });
    try {
      await page.setViewportSize(VP[vpName]);
      await gotoConsole(page);
      const group = page.locator('.appbar [role="group"][aria-label="界面主题"]');
      const count = await group.count();
      const visible = count ? await group.first().isVisible() : false;
      const buttons = group.first().locator('button');
      const n = count ? await buttons.count() : 0;
      const labels = [];
      const pressed = [];
      for (let i = 0; i < n; i++) {
        labels.push((await buttons.nth(i).getAttribute('aria-label')) || '');
        pressed.push((await buttons.nth(i).getAttribute('aria-pressed')) === 'true');
      }
      addCheck({
        id: `control-present-${vpName}`, category: 'control', viewport: vpName,
        pass: count === 1 && visible && n === 3 && labels.includes('亮色') && labels.includes('暗色') && labels.includes('跟随系统'),
        expected: 'appbar 内 1 个 role=group[aria-label=界面主题]，含 亮色/暗色/跟随系统 三个按钮',
        actual: `count=${count} visible=${visible} buttons=${n} labels=${JSON.stringify(labels)}`,
      });
      // 主题开关命中区域尺寸（Lead 定稿：desktop 26×24，移动 24×24，高至少 24px）
      if (count) {
        const box = await group.first().locator('button').first().boundingBox().catch(() => null);
        const w = box ? Math.round(box.width) : null;
        const h = box ? Math.round(box.height) : null;
        addCheck({
          id: `theme-opt-size-${vpName}`, category: 'control', viewport: vpName,
          pass: h === 24 && (vpName === 'desktop' ? w === 26 : w === 24),
          expected: vpName === 'desktop' ? '主题按钮 26×24px（高 24px）' : '主题按钮 24×24px',
          actual: box ? `${w}×${h}px` : '未取到 boundingBox',
        });
      }
      // 390px 顶栏项目上下文完整显示（最终修正：≤760px 隐藏 .appbar-spacer 与 .ctx-owner，只见项目名）
      if (vpName === 'mobile') {
        const ctxInfo = await page.evaluate(() => {
          const el = document.querySelector('.appbar .ctx-name');
          if (!el) return null;
          const owner = el.querySelector('.ctx-owner');
          const cs = getComputedStyle(el);
          return {
            projectName: (el.textContent || '').trim(),
            ownerHidden: owner ? getComputedStyle(owner).display === 'none' : null,
            clientWidth: el.clientWidth,
            scrollWidth: el.scrollWidth,
            overflow: cs.textOverflow,
            ellipsisClipped: el.scrollWidth > el.clientWidth + 1,
          };
        });
        addCheck({
          id: 'mobile-appbar-ctx-full', category: 'control', viewport: 'mobile',
          pass: !!ctxInfo && ctxInfo.projectName.includes('core-api') && !ctxInfo.ellipsisClipped && ctxInfo.ownerHidden !== false,
          expected: '390px 顶栏仅显示项目名 core-api（.ctx-owner「个人 /」被 ≤760px 隐藏），无省略裁切',
          actual: JSON.stringify(ctxInfo),
        });
      }
      addCheck({
        id: `control-initial-pressed-${vpName}`, category: 'control', viewport: vpName,
        pass: pressed.length === 3 && pressed[labels.indexOf('跟随系统')] === true,
        expected: 'system 模式下「跟随系统」aria-pressed=true，其余 false',
        actual: JSON.stringify(Object.fromEntries(labels.map((l, i) => [l, pressed[i]]))),
      });
      // 点击三态
      const steps = [
        { label: '亮色', stored: 'light', theme: 'light' },
        { label: '暗色', stored: 'dark', theme: 'dark' },
        { label: '跟随系统', stored: 'system', theme: 'dark' }, // colorScheme: dark
      ];
      for (const step of steps) {
        const btn = page.locator(`.appbar [role="group"][aria-label="界面主题"] button[aria-label="${step.label}"]`).first();
        const okClick = await btn.click({ timeout: 5000 }).then(() => true).catch(() => false);
        await page.waitForTimeout(350);
        const s = await themeState(page);
        const pressedNow = await page.locator(`.appbar [role="group"][aria-label="界面主题"] [aria-label="${step.label}"]`).first().getAttribute('aria-pressed').catch(() => null);
        addCheck({
          id: `control-click-${step.label}-${vpName}`, category: 'control', viewport: vpName,
          pass: okClick && s.theme === step.theme && s.mode === step.stored && s.stored === step.stored && pressedNow === 'true',
          expected: `点击「${step.label}」→ data-theme=${step.theme}、data-theme-mode=${step.stored}、localStorage=${step.stored}、aria-pressed=true`,
          actual: `click=${okClick} ${JSON.stringify(s)} aria-pressed=${pressedNow}`,
        });
      }
      // Tab 可达：从文档开头循环 Tab，找三个 aria-label
      await page.evaluate(() => { document.body.focus(); window.scrollTo(0, 0); });
      const found = new Set();
      for (let i = 0; i < 120 && found.size < 3; i++) {
        await page.keyboard.press('Tab');
        const focused = await page.evaluate(() => {
          const el = document.activeElement;
          if (!el || el === document.body) return null;
          return { label: el.getAttribute('aria-label') || '', text: (el.textContent || '').trim(), tag: el.tagName };
        });
        if (focused && ['亮色', '暗色', '跟随系统'].includes(focused.label)) found.add(focused.label);
        if (focused && found.size === 3) break;
      }
      addCheck({
        id: `control-keyboard-${vpName}`, category: 'control', viewport: vpName,
        pass: found.size === 3,
        expected: '三个主题按钮都能通过 Tab 聚焦（aria-label 可被读屏读出）',
        actual: `找到 ${JSON.stringify([...found])}`,
      });
    } catch (e) {
      addCheck({ id: `control-${vpName}`, category: 'control', viewport: vpName, pass: false, expected: '主题控件可用', actual: `异常：${e.message}` });
    } finally { await ctx.close(); }
  }

  // ---- 无闪：尽早读 data-theme；样式加载后主题保持不变 ----
  for (const theme of ['dark', 'light']) {
    const { ctx, page } = await openWithTheme(browser, { theme, colorScheme: theme === 'dark' ? 'light' : 'dark' });
    try {
      await page.goto(`${CONSOLE_URL}/`, { waitUntil: 'commit' });
      let early = null;
      try {
        await page.waitForFunction(() => !!document.documentElement && document.documentElement.dataset.theme !== undefined, { timeout: 5000 });
        early = await page.evaluate(() => ({
          theme: document.documentElement.dataset.theme,
          mode: document.documentElement.dataset.themeMode,
          bodyBg: document.body ? getComputedStyle(document.body).backgroundColor : null,
          metaThemeColor: document.querySelector('meta[name="theme-color"]')?.getAttribute('content') ?? null,
        }));
      } catch { /* 记录为失败 */ }
      await page.locator('.appbar').first().waitFor({ timeout: 15000 }).catch(() => {});
      await page.waitForLoadState('load', { timeout: 15000 }).catch(() => {});
      await page.waitForTimeout(500);
      const late = await themeState(page);
      const bg = bgMatchesTheme(late.bodyBg, theme);
      const meta = await page.evaluate(() => {
        const el = document.querySelector('meta[name="theme-color"]');
        const themed = getComputedStyle(document.documentElement).getPropertyValue('--meta-theme-color').trim();
        return { content: el?.getAttribute('content') ?? null, themed };
      });
      const expectedMeta = theme === 'dark' ? '#0a0907' : '#f7f4ec';
      addCheck({
        id: `no-flash-${theme}`, category: 'no-flash', theme,
        pass: !!early && early.theme === theme && late.theme === theme && bg.ok,
        expected: `首帧 data-theme=${theme}（不依赖 React 挂载），加载完成后仍为 ${theme} 且背景与主题一致`,
        actual: `early=${JSON.stringify(early)} late=${JSON.stringify(late)}；${bg.why}`,
      });
      // 定稿色板 A「素白宣」：亮色页面底 #f7f4ec、暗色页面底 #0a0907
      const expectedBodyBg = theme === 'dark' ? 'rgb(10, 9, 7)' : 'rgb(247, 244, 236)';
      addCheck({
        id: `body-bg-${theme}`, category: 'palette', theme,
        pass: late.bodyBg === expectedBodyBg,
        expected: `body 背景 ${expectedBodyBg}（#${theme === 'dark' ? '0a0907' : 'f7f4ec'}）`,
        actual: late.bodyBg,
      });
      // 极早期表面（BootSurface 等无开关面）依赖 bootstrap 写的 meta theme-color
      addCheck({
        id: `meta-theme-color-${theme}`, category: 'meta', theme,
        pass: meta.content === expectedMeta && meta.themed === expectedMeta,
        expected: `meta[name=theme-color]=${expectedMeta}（bootstrap/CSS 同步，Early BootSurface 也正确）`,
        actual: `content=${meta.content} --meta-theme-color=${meta.themed}（早期 meta=${early?.metaThemeColor ?? '未读到'}）`,
      });
    } catch (e) {
      addCheck({ id: `no-flash-${theme}`, category: 'no-flash', theme, pass: false, expected: '无主题闪烁', actual: `异常：${e.message}` });
    } finally { await ctx.close(); }
  }

  // ---- 主题开关挂载点：控制台顶栏 / 登录页 auth-foot / 组织页 spaces-topbar-actions / CLI .theme-corner ----
  // （落款页 onboard-head 在夹具里不可达：/me 返回 handle_confirmed=true，记录为未覆盖。）
  for (const theme of ['dark', 'light']) {
    const mounts = [
      { id: 'console-appbar', url: `${CONSOLE_URL}/`, ready: '.appbar', sel: '.appbar [role="group"][aria-label="界面主题"]' },
      { id: 'login-auth-foot', url: `${LOGIN_URL}/`, ready: '.auth-card', sel: '.auth-foot [role="group"][aria-label="界面主题"]' },
      { id: 'organizations-topbar', url: `${CONSOLE_URL}/organizations`, ready: '.spaces-topbar', sel: '.spaces-topbar-actions [role="group"][aria-label="界面主题"]' },
      { id: 'cli-theme-corner', url: `${CONSOLE_URL}/device`, ready: '.once-secret', sel: '.theme-corner[role="group"][aria-label="界面主题"]' },
    ];
    for (const m of mounts) {
      const { ctx, page } = await openWithTheme(browser, { theme, colorScheme: theme });
      try {
        await page.goto(m.url, { waitUntil: 'domcontentloaded' });
        await page.locator(m.ready).first().waitFor({ timeout: 15000 }).catch(() => {});
        await page.waitForTimeout(600);
        const group = page.locator(m.sel);
        const count = await group.count();
        const visible = count ? await group.first().isVisible() : false;
        const n = count ? await group.first().locator('button').count() : 0;
        const state = await themeState(page);
        addCheck({
          id: `mount-${m.id}-${theme}`, category: 'mount', theme, viewport: 'desktop',
          pass: count === 1 && visible && n === 3 && state.theme === theme,
          expected: `${m.id} 有 1 个主题开关（3 个按钮）且页面 data-theme=${theme}`,
          actual: `count=${count} visible=${visible} buttons=${n} data-theme=${state.theme} stored=${state.stored}`,
        });
      } catch (e) {
        addCheck({ id: `mount-${m.id}-${theme}`, category: 'mount', theme, viewport: 'desktop', pass: false, expected: '主题开关挂载', actual: `异常：${e.message}` });
      } finally { await ctx.close(); }
    }
  }

  // ---- 390px 无横向溢出：登录 / 控制台 / 设置 / 组织页 ----
  for (const theme of THEMES) {
    const { ctx, page } = await openWithTheme(browser, { theme, colorScheme: 'dark' });
    try {
      await page.setViewportSize(VP.mobile);
      const overflow = async (label) => {
        const m = await page.evaluate(() => {
          const root = document.documentElement;
          const items = [];
          for (const el of document.querySelectorAll('*')) {
            const r = el.getBoundingClientRect();
            if (r.width > 0 && r.right > window.innerWidth + 1) {
              const cls = typeof el.className === 'string' ? el.className.trim().split(/\s+/).slice(0, 2).join('.') : '';
              items.push(`${el.tagName.toLowerCase()}${cls ? '.' + cls : ''} right=${Math.round(r.right)}`);
              if (items.length >= 8) break;
            }
          }
          return { scrollWidth: root.scrollWidth, bodyScrollWidth: document.body.scrollWidth, innerWidth: window.innerWidth, offenders: items };
        });
        addCheck({
          id: `overflow-${label}-${theme}`, category: 'overflow', theme, viewport: 'mobile',
          pass: m.scrollWidth <= m.innerWidth,
          expected: `documentElement.scrollWidth ≤ innerWidth(${m.innerWidth})`,
          actual: `scrollWidth=${m.scrollWidth} body=${m.bodyScrollWidth} diff=${m.scrollWidth - m.innerWidth}；越界元素=${JSON.stringify(m.offenders)}`,
        });
      };
      // 登录页
      await page.goto(`${LOGIN_URL}/`, { waitUntil: 'domcontentloaded' });
      await page.locator('.auth-card, .auth-shell').first().waitFor({ timeout: 15000 }).catch(() => {});
      await page.waitForTimeout(700);
      await overflow('login');
      // 控制台
      await gotoConsole(page);
      await overflow('console');
      // 设置
      await navClick(page, true, ['组织设置', '个人设置', '设置']);
      await page.locator('.settings-panel, .settings').first().waitFor({ timeout: 10000 }).catch(() => {});
      await page.waitForTimeout(600);
      await overflow('settings');
      // 组织页
      await page.goto(`${CONSOLE_URL}/organizations`, { waitUntil: 'domcontentloaded' });
      await page.waitForTimeout(900);
      await page.waitForLoadState('networkidle', { timeout: 3000 }).catch(() => {});
      await overflow('organizations');
    } catch (e) {
      addCheck({ id: `overflow-${theme}`, category: 'overflow', theme, viewport: 'mobile', pass: false, expected: '逐页检查 390px 溢出', actual: `异常：${e.message}` });
    } finally { await ctx.close(); }
  }
}

// ============================================================================
// main
// ============================================================================
console.log(`[capture-theme] themes=${THEMES.join(',')} viewports=${VIEWPORT_NAMES.join(',')} console=${CONSOLE_URL} login=${LOGIN_URL}`);
console.log(`[capture-theme] out=${OUT}`);
const browser = await chromium.launch();
manifest.browser = `chromium ${browser.version()}`;

const failures = [];
if (!CHECKS_ONLY) {
  for (const theme of THEMES) {
    for (const vpName of VIEWPORT_NAMES) {
      try {
        const global = await captureThemeViewport(browser, theme, vpName);
        if (global.pageErrors.length) failures.push({ kind: 'pageerror', theme, viewport: vpName, items: global.pageErrors });
        if (global.http5xx.length) failures.push({ kind: 'http5xx', theme, viewport: vpName, items: global.http5xx });
      } catch (e) {
        failures.push({ kind: 'capture-failed', theme, viewport: vpName, message: e.message });
        console.error(`[capture-theme] ${theme}/${vpName} 截图失败：`, e.message);
      }
    }
  }
  manifest.notes.push('落款页 UsernameSetup 的 onboard-head 挂载点在夹具中不可达（/me 返回 handle_confirmed=true），未覆盖。');
} else {
  console.log('[capture-theme] checks-only：跳过截图，保留已有 capture-manifest.json');
}

if (RUN_CHECKS) {
  console.log('[capture-theme] 机械验收：持久化 / 切换控件 / 无闪 / 390px 溢出 …');
  try {
    await runChecks(browser);
  } catch (e) {
    checks.push({ id: 'checks-crashed', category: 'harness', pass: false, expected: '机械验收完成', actual: `异常：${e.message}`, detail: e.stack ?? '' });
  }
}
await browser.close();

// 汇总
const failedChecks = checks.filter((c) => !c.pass);
const summary = {
  shots: manifest.shots.filter((s) => s.surface !== 'theme-state' && s.surface !== 'login-state').length,
  checks: checks.length,
  checksPassed: checks.length - failedChecks.length,
  checksFailed: failedChecks.length,
  pageErrorCount: failures.filter((f) => f.kind === 'pageerror').length,
  http5xxCount: failures.filter((f) => f.kind === 'http5xx').length,
  captureFailures: failures.filter((f) => f.kind === 'capture-failed').length,
};
const payload = { summary, manifest, checks, failures };
if (!CHECKS_ONLY) fs.writeFileSync(path.join(OUT, 'capture-manifest.json'), JSON.stringify(payload, null, 2));
// checks-only 时沿用上一次全量运行的截图数，避免把 summary.shots 覆盖成 0
let shotsForSummary = summary.shots;
if (CHECKS_ONLY) {
  try {
    const prev = JSON.parse(fs.readFileSync(path.join(OUT, 'theme-checks.json'), 'utf8'));
    if (typeof prev?.summary?.shots === 'number') shotsForSummary = prev.summary.shots;
  } catch { /* 无历史文件 */ }
}
const checkSummary = { ...summary, shots: shotsForSummary };
fs.writeFileSync(path.join(OUT, 'theme-checks.json'), JSON.stringify({ generatedAt: manifest.generatedAt, themes: THEMES, viewports: VIEWPORT_NAMES, summary: checkSummary, checks }, null, 2));

console.log(`[capture-theme] 截图 ${summary.shots} 张；检查 ${summary.checks} 项（失败 ${summary.checksFailed}）；pageerror ${summary.pageErrorCount} 组；5xx ${summary.http5xxCount} 组；截图失败 ${summary.captureFailures}`);
if (failedChecks.length) {
  console.log('[capture-theme] 失败检查：');
  for (const c of failedChecks) console.log(`  ✗ ${c.id} | expected=${c.expected} | actual=${c.actual}`);
}
if (failures.length) {
  console.log('[capture-theme] 页面/网络错误：');
  for (const f of failures) console.log(`  ✗ ${f.kind} ${f.theme}/${f.viewport}: ${f.message ?? JSON.stringify(f.items?.slice(0, 5))}`);
}
console.log(`[capture-theme] 报告数据：${rel(path.join(OUT, 'capture-manifest.json'))}、${rel(path.join(OUT, 'theme-checks.json'))}`);
process.exitCode = 0;
