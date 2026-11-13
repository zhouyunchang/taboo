#!/usr/bin/env node
// 双主题对比度审计：只测量真实渲染（playwright + getComputedStyle），含 alpha 合成与祖先后台回溯。
// 用法：node scripts/contrast-audit.mjs [--themes=dark,light] [--viewports=desktop,mobile]
// env：CONSOLE_URL / LOGIN_URL / OUT_DIR（默认 .impeccable/review/theme）
// 产物：OUT_DIR/contrast.json（原始数据）、OUT_DIR/contrast.md（失败项 markdown 表）
// 阈值：正文 <4.5 失败；大字（≥24px 或 ≥18.66px 且 bold）与非文本指示 <3 失败。
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
const VP = { desktop: { width: 1440, height: 900 }, mobile: { width: 390, height: 844 } };

const TEXT = (def) => ({ kind: 'text', ...def });
const NON_TEXT = (def) => ({ kind: 'nonText', ...def });
const D = ['desktop'];
const M = ['mobile'];

// 真实页面上的测量点。selector 都来自当前产品源码；找不到时记录 not-found-or-hidden，不伪造数值。
const PROBES = {
  console: [
    TEXT({ selector: 'main .stage p', label: '正文 p（控制台主区）' }),
    TEXT({ selector: '.faint', label: '.faint（11–12px 次要文字）' }),
    TEXT({ selector: '.section-hint', label: '.section-hint' }),
    TEXT({ selector: '.secret-table th', label: '.secret-table th（密钥表头，sticky 背景）' }),
    TEXT({ selector: '.btn-primary', label: '.btn-primary 文本' }),
    TEXT({ selector: '.cipher', label: '.cipher（密文占位）' }),
    TEXT({ selector: '.pill-muted', label: '.pill-muted（标签）' }),
    TEXT({ selector: '.rail-label', label: '.rail-label', viewports: D }),
    TEXT({ selector: '.rail-link.active', label: '.rail-link.active', viewports: D }),
    TEXT({ selector: '.status-chip', label: '.status-chip', viewports: D }),
    TEXT({ selector: '.mobile-nav button.active', label: '.mobile-nav button.active', viewports: M }),
    TEXT({ selector: 'input[aria-label="搜索密钥"]', label: 'input::placeholder（搜索密钥）', pseudo: '::placeholder' }),
  ],
  popover: [
    TEXT({ selector: '.popover .pop-item-sub', label: '.pop-item-sub（账户菜单副标题）' }),
  ],
  flash: [
    TEXT({ selector: '.flash-dialog', label: '.flash-dialog（复制回执）' }),
  ],
  consoleRevealed: [
    TEXT({ selector: '.cipher-value', label: '.cipher-value（显示后的明文）' }),
    TEXT({ selector: '.ref-chip', label: '.ref-chip（引用标记，amber）' }),
  ],
  record: [
    TEXT({ selector: '.version-sub', label: '.version-sub（版本写入者）' }),
    TEXT({ selector: '.chapters-note', label: '.chapters-note（章节注）', viewports: D }),
    TEXT({ selector: '.cipher', label: '.cipher（卷宗封条）' }),
    TEXT({ selector: '.danger-btn', label: '.danger-btn（卷宗删除动作）' }),
  ],
  audit: [
    TEXT({ selector: '.audit-row .ip', label: '.audit-row .ip（审计 IP）', viewports: D }),
    TEXT({ selector: '.audit-row .t', label: '.audit-row .t（审计时间）' }),
    TEXT({ selector: '.audit-row .who', label: '.audit-row .who（审计操作者）' }),
    TEXT({ selector: '.act-glyph', label: '.act-glyph（审计动作）' }),
  ],
  settings: [
    TEXT({ selector: '.table th', label: '.table th（表格 sticky 表头）' }),
    TEXT({ selector: '.pill-ok', label: '.pill-ok' }),
    TEXT({ selector: '.pill-warn', label: '.pill-warn（表格内 pending 状态）' }),
    TEXT({ selector: '.btn-danger', label: '.btn-danger 文本' }),
    TEXT({ selector: '.settings-nav .rail-label', label: '.rail-label（设置分区）', viewports: D }),
    TEXT({ selector: '.settings-nav .rail-link.active', label: '.rail-link.active（设置分区）', viewports: D }),
    TEXT({ selector: '.section-hint', label: '.section-hint（设置）' }),
    TEXT({ selector: '.page-meta', label: '.page-meta（设置屏头）' }),
  ],
  settingsSync: [
    TEXT({ selector: '.tag', label: '.tag（平台标签）' }),
    TEXT({ selector: '.pill-ok', label: '.pill-ok（Secret Sync）' }),
    TEXT({ selector: '.pill-warn', label: '.pill-warn（Secret Sync）' }),
    TEXT({ selector: '.table th', label: '.table th（Secret Sync 表头）' }),
    TEXT({ selector: '.section-hint', label: '.section-hint（Secret Sync）' }),
    TEXT({ selector: '.btn-danger', label: '.btn-danger 文本（Secret Sync）' }),
  ],
  organizations: [
    TEXT({ selector: '.pill-warn', label: '.pill-warn' }),
    TEXT({ selector: '.pill-accent', label: '.pill-accent' }),
    TEXT({ selector: '.section-hint', label: '.section-hint（组织页）' }),
  ],
  cli: [
    TEXT({ selector: '.once-secret p', label: '.once-secret 文本（p）' }),
    TEXT({ selector: '.once-secret h3', label: '.once-secret h3' }),
    TEXT({ selector: '.seal-stamp', label: '.seal-stamp' }),
    TEXT({ selector: '.faint', label: '.faint（CLI 同意页）' }),
    TEXT({ selector: '.notice-warn', label: '.notice-warn（CLI 警告）' }),
  ],
  cliError: [
    TEXT({ selector: '.error', label: '.error' }),
  ],
  login: [
    TEXT({ selector: '.auth-book-body', label: '.auth-book-body 正文', viewports: D }),
    TEXT({ selector: '.auth-sub', label: '.auth-sub' }),
    TEXT({ selector: '.faint', label: '.faint（登录页）' }),
    TEXT({ selector: '.btn-primary', label: '.btn-primary（登录）' }),
    TEXT({ selector: 'input[type="email"]', label: 'input::placeholder（登录邮箱）', pseudo: '::placeholder' }),
  ],
};

// 非文本指示（WCAG 1.4.11，≥3）：边框 / 状态点与相邻背景。
const NON_TEXT_TARGETS = [
  NON_TEXT({ selector: '.pill-ok', label: '.pill-ok 边框', metric: 'border' }),
  NON_TEXT({ selector: '.pill-warn', label: '.pill-warn 边框', metric: 'border' }),
  NON_TEXT({ selector: '.pill-accent', label: '.pill-accent 边框', metric: 'border' }),
  NON_TEXT({ selector: '.tag', label: '.tag 边框', metric: 'border' }),
  NON_TEXT({ selector: '.status-chip', label: '.status-chip 边框', metric: 'border', viewports: D }),
  NON_TEXT({ selector: '.status-chip .dot', label: '.status-chip .dot 指示点', metric: 'fill', viewports: D }),
  NON_TEXT({ selector: '.chapter-dot', label: '.chapter-dot 章节点', metric: 'border' }),
  NON_TEXT({ selector: '.btn-danger', label: '.btn-danger 边框', metric: 'border' }),
];
PROBES.settings.push(...NON_TEXT_TARGETS.filter((t) => t.metric && ['border'].includes(t.metric) && ['.pill-ok', '.tag', '.btn-danger'].includes(t.selector)));
PROBES.console.push(...NON_TEXT_TARGETS.filter((t) => ['.status-chip', '.status-chip .dot'].includes(t.selector)));
PROBES.organizations.push(...NON_TEXT_TARGETS.filter((t) => ['.pill-warn', '.pill-accent'].includes(t.selector)));
PROBES.record.push(...NON_TEXT_TARGETS.filter((t) => t.selector === '.chapter-dot'));

const results = [];
const skipped = [];

function parseColor(str) {
  if (!str) return null;
  const s = String(str).trim();
  if (s === 'transparent') return { r: 0, g: 0, b: 0, a: 0 };
  let m = s.match(/^rgba?\(([^)]+)\)$/i);
  if (m) {
    const parts = m[1].split(/[,\s/]+/).filter(Boolean).map(Number);
    return { r: parts[0] || 0, g: parts[1] || 0, b: parts[2] || 0, a: parts.length > 3 ? parts[3] : 1 };
  }
  m = s.match(/^color\(srgb\s+([\d.]+)\s+([\d.]+)\s+([\d.]+)(?:\s*\/\s*([\d.]+))?\)$/i);
  if (m) return { r: Math.round(Number(m[1]) * 255), g: Math.round(Number(m[2]) * 255), b: Math.round(Number(m[3]) * 255), a: m[4] !== undefined ? Number(m[4]) : 1 };
  return null;
}
function composite(over, under) {
  if (!over) return under;
  if (!under) return { ...over };
  const a = over.a + under.a * (1 - over.a);
  if (a === 0) return { r: 0, g: 0, b: 0, a: 0 };
  return {
    r: Math.round((over.r * over.a + under.r * under.a * (1 - over.a)) / a),
    g: Math.round((over.g * over.a + under.g * under.a * (1 - over.a)) / a),
    b: Math.round((over.b * over.a + under.b * under.a * (1 - over.a)) / a),
    a,
  };
}
function css(c) { return c ? `rgb(${c.r} ${c.g} ${c.b})` : null; }
function luminance({ r, g, b }) {
  const f = (v) => { v /= 255; return v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4; };
  return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b);
}
function contrast(a, b) {
  const la = luminance(a), lb = luminance(b);
  const hi = Math.max(la, lb), lo = Math.min(la, lb);
  return (hi + 0.05) / (lo + 0.05);
}

/** 从 html 往上叠背景：chain[0] 是元素本身，chain[last] 是 html。 */
function backdropFrom(chain) {
  let bg = { r: 255, g: 255, b: 255, a: 1 }; // 画布兜底：所有真实页面 html/body 都有不透明底色，走到这里会记录 fallback
  let fallback = true;
  for (let i = chain.length - 1; i >= 0; i--) {
    const c = parseColor(chain[i].bg);
    if (!c || c.a === 0) continue;
    const alpha = c.a * (Number.isFinite(chain[i].opacity) ? chain[i].opacity : 1);
    bg = composite({ ...c, a: alpha }, bg);
    if (alpha >= 1) fallback = false;
  }
  return { bg, fallback };
}

function thresholdForText(fontSize, fontWeight) {
  const bold = Number(fontWeight) >= 700;
  const large = fontSize >= 24 || (fontSize >= 18.66 && bold);
  return { large, threshold: large ? 3 : 4.5 };
}

function computeRecord(def, raw, ctxMeta, chain) {
  const base = {
    theme: ctxMeta.theme, viewport: ctxMeta.viewport, surface: ctxMeta.surface,
    selector: def.selector, label: def.label, kind: def.kind, metric: def.metric || null,
    pseudo: def.pseudo || null, text: raw.text || '',
  };
  if (!raw.found) {
    return { ...base, status: 'not-found-or-hidden', note: '当前页面/视口找不到可见元素（可能被 media query 隐藏或页面无该状态）', pass: null };
  }
  const opacityChain = chain.reduce((acc, c) => acc * (Number.isFinite(c.opacity) ? c.opacity : 1), 1);

  if (def.kind === 'text') {
    const color = parseColor(raw.color);
    if (!color || opacityChain === 0) {
      return { ...base, status: 'not-measured', detail: `color=${raw.color} opacity=${opacityChain}`, pass: null };
    }
    const { bg, fallback } = backdropFrom(chain);
    const fg = composite({ ...color, a: color.a * opacityChain }, bg);
    const ratio = contrast(fg, bg);
    const { large, threshold } = thresholdForText(raw.fontSize, raw.fontWeight);
    return {
      ...base, status: 'measured',
      fg: css(fg), bg: css(bg), fgSource: raw.color, bgFallback: fallback,
      fontSize: raw.fontSize, fontWeight: raw.fontWeight, large, threshold,
      ratio: Number(ratio.toFixed(2)), pass: ratio >= threshold,
      opacityChain: Number(opacityChain.toFixed(3)),
    };
  }

  // nonText：边框 / 色块指示
  const ownBgRaw = parseColor(raw.backgroundColor);
  const ancestorChain = chain.slice(1);
  const { bg: outside } = backdropFrom(ancestorChain);
  if (def.metric === 'fill') {
    if (!ownBgRaw || ownBgRaw.a === 0) return { ...base, status: 'not-measured', detail: `backgroundColor=${raw.backgroundColor}`, pass: null };
    const fg = composite({ ...ownBgRaw, a: ownBgRaw.a * opacityChain }, outside);
    const ratio = contrast(fg, outside);
    return { ...base, status: 'measured', fg: css(fg), bg: css(outside), threshold: 3, ratio: Number(ratio.toFixed(2)), pass: ratio >= 3 };
  }
  // border：与元素外相邻背景（祖先合成）比较
  const border = parseColor(raw.borderColor);
  if (!border || border.a === 0) return { ...base, status: 'not-measured', detail: `borderColor=${raw.borderColor}`, pass: null };
  const fg = composite({ ...border, a: border.a * opacityChain }, outside);
  const ratio = contrast(fg, outside);
  return { ...base, status: 'measured', fg: css(fg), bg: css(outside), threshold: 3, ratio: Number(ratio.toFixed(2)), pass: ratio >= 3 };
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function gotoConsole(page) {
  await page.goto(`${CONSOLE_URL}/`, { waitUntil: 'domcontentloaded' });
  await page.locator('.appbar').first().waitFor({ state: 'visible', timeout: 15000 });
  await page.locator('button.secret-key-btn').first().waitFor({ state: 'visible', timeout: 15000 }).catch(() => {});
  await page.waitForLoadState('networkidle', { timeout: 2500 }).catch(() => {});
  await page.waitForTimeout(600);
}
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

async function measure(page, meta, defs) {
  const applicable = defs.filter((d) => !d.viewports || d.viewports.includes(meta.viewport));
  const inapplicable = defs.filter((d) => d.viewports && !d.viewports.includes(meta.viewport));
  for (const def of inapplicable) {
    skipped.push({ theme: meta.theme, viewport: meta.viewport, surface: meta.surface, selector: def.selector, label: def.label, reason: `该视口不适用（仅 ${def.viewports.join('/')}）` });
  }
  if (!applicable.length) return;
  let raws = [];
  try {
    raws = await page.evaluate((defs) => {
      const describe = (el, def) => {
        const cs = getComputedStyle(el, def.pseudo || undefined);
        const chain = [];
        let node = el;
        while (node && node.nodeType === 1) {
          const s = getComputedStyle(node);
          chain.push({
            tag: node.tagName.toLowerCase(),
            cls: typeof node.className === 'string' ? node.className.trim().split(/\s+/).slice(0, 3).join('.') : '',
            bg: s.backgroundColor,
            opacity: Number.parseFloat(s.opacity),
          });
          node = node.parentElement;
        }
        const r = el.getBoundingClientRect();
        return {
          found: true,
          text: (el.textContent || '').trim().replace(/\s+/g, ' ').slice(0, 80),
          color: cs.color,
          borderColor: cs.borderTopColor,
          backgroundColor: cs.backgroundColor,
          fontSize: Number.parseFloat(cs.fontSize),
          fontWeight: cs.fontWeight,
          rect: { w: Math.round(r.width), h: Math.round(r.height) },
          chain,
        };
      };
      return defs.map((def) => {
        for (const el of Array.from(document.querySelectorAll(def.selector))) {
          const cs = getComputedStyle(el, def.pseudo || undefined);
          const baseCs = getComputedStyle(el);
          if (baseCs.display === 'none' || baseCs.visibility === 'hidden') continue;
          const r = el.getBoundingClientRect();
          if (r.width < 1 || r.height < 1) continue;
          if (def.kind === 'text' && def.pseudo !== '::placeholder' && !(el.textContent || '').trim()) continue;
          // 只有 opacity 链乘积 > 0 才算真实可见文字
          let op = 1, node = el;
          while (node && node.nodeType === 1) { op *= Number.parseFloat(getComputedStyle(node).opacity); node = node.parentElement; }
          if (!(op > 0.01)) continue;
          return describe(el, def);
        }
        return { found: false };
      });
    }, applicable);
  } catch (e) {
    for (const def of applicable) {
      results.push({ theme: meta.theme, viewport: meta.viewport, surface: meta.surface, selector: def.selector, label: def.label, kind: def.kind, status: 'measure-error', detail: e.message, pass: null });
    }
    return;
  }
  raws.forEach((raw, i) => {
    const def = applicable[i];
    try {
      results.push(computeRecord(def, raw, meta, raw.chain || []));
    } catch (e) {
      results.push({ theme: meta.theme, viewport: meta.viewport, surface: meta.surface, selector: def.selector, label: def.label, kind: def.kind, status: 'compute-error', detail: e.message, pass: null });
    }
  });
}

async function runThemeViewport(browser, theme, vpName) {
  const mobile = vpName === 'mobile';
  const ctx = await browser.newContext({
    viewport: VP[vpName], deviceScaleFactor: 2, locale: 'zh-CN', timezoneId: 'Asia/Shanghai', colorScheme: theme,
  });
  await ctx.addInitScript((t) => { try { window.localStorage.setItem('taboo:theme', t); } catch { /* 隐私模式 */ } }, theme);
  await ctx.grantPermissions(['clipboard-read', 'clipboard-write']).catch(() => {});
  const page = await ctx.newPage();
  const meta = (surface) => ({ theme, viewport: vpName, surface });

  // ---------- 控制台 ----------
  await gotoConsole(page);
  await measure(page, meta('console'), PROBES.console);

  // 账户菜单里的 .pop-item-sub
  const avatar = page.locator('.avatar-btn').first();
  if (await avatar.count()) {
    await avatar.click();
    await page.waitForTimeout(350);
    await measure(page, meta('console-popover'), PROBES.popover);
    await page.keyboard.press('Escape');
    await page.waitForTimeout(250);
  }
  // 复制回执 .flash-dialog
  const copyBtn = page.getByRole('button', { name: '复制', exact: true }).first();
  if (await copyBtn.count()) {
    await copyBtn.click().catch(() => {});
    await page.locator('.flash-dialog').first().waitFor({ state: 'visible', timeout: 4000 }).catch(() => {});
    await page.waitForTimeout(200);
    await measure(page, meta('console-flash'), PROBES.flash);
    await page.waitForTimeout(900);
  }

  // ---------- 显示明文 / 引用 chip（cloud 文件夹 UPSTREAM_API 的值含 ${...}） ----------
  const cloudFolder = page.locator('.tree-label').filter({ hasText: 'cloud' }).first();
  if (await cloudFolder.count()) {
    await cloudFolder.click();
    await page.waitForTimeout(750);
    await page.waitForLoadState('networkidle', { timeout: 2000 }).catch(() => {});
  }
  const revealEye = page.getByRole('button', { name: '显示', exact: true }).first();
  if (await revealEye.count()) {
    await revealEye.click().catch(() => {});
    await page.waitForTimeout(700);
    await measure(page, meta('console-revealed'), PROBES.consoleRevealed);
    await page.getByRole('button', { name: '隐藏', exact: true }).first().click().catch(() => {});
    await page.waitForTimeout(300);
  } else {
    skipped.push({ theme, viewport: vpName, surface: 'console-revealed', reason: '未找到「显示」按钮（cloud 文件夹可能为空）' });
  }
  // 回根目录：卷宗仍取第一行，与历史证据同一条
  const rootFolder = page.locator('.tree-label').filter({ hasText: 'core-api' }).first();
  if (await rootFolder.count()) {
    await rootFolder.click();
    await page.waitForTimeout(600);
    await page.waitForLoadState('networkidle', { timeout: 2000 }).catch(() => {});
  }

  // ---------- 卷宗 ----------
  const keyBtn = page.locator('button.secret-key-btn').first();
  if ((await keyBtn.count()) && (await keyBtn.isVisible().catch(() => false))) {
    await keyBtn.click();
    await page.waitForTimeout(750);
    await page.waitForLoadState('networkidle', { timeout: 2000 }).catch(() => {});
    await measure(page, meta('record'), PROBES.record);
  } else {
    skipped.push({ theme, viewport: vpName, surface: 'record', reason: '未找到 .secret-key-btn' });
  }
  await navClick(page, mobile, ['密钥']).catch(() => {});
  await page.waitForTimeout(350);

  // ---------- 审计 ----------
  if (await navClick(page, mobile, ['审计日志', '审计'])) {
    await page.locator('.audit-row').first().waitFor({ timeout: 10000 }).catch(() => {});
    await page.waitForTimeout(400);
    await measure(page, meta('audit'), PROBES.audit);
  } else {
    skipped.push({ theme, viewport: vpName, surface: 'audit', reason: '无审计入口（角色/视口）' });
  }

  // ---------- 设置（个人空间默认落到审批页；再点 Secret Sync 覆盖 .tag / .pill-ok） ----------
  if (await navClick(page, mobile, ['组织设置', '个人设置', '设置'])) {
    await page.locator('.settings-panel, .settings').first().waitFor({ timeout: 10000 }).catch(() => {});
    await page.waitForTimeout(500);
    await measure(page, meta('settings'), PROBES.settings);
    const syncTab = page.locator('.settings-nav button').filter({ hasText: 'Secret Sync' }).first();
    if (await syncTab.count()) {
      await syncTab.click();
      await page.waitForTimeout(700);
      await page.waitForLoadState('networkidle', { timeout: 2000 }).catch(() => {});
      await measure(page, meta('settings-sync'), PROBES.settingsSync);
    }
  } else {
    skipped.push({ theme, viewport: vpName, surface: 'settings', reason: '无设置入口' });
  }

  // ---------- 组织页 ----------
  await page.goto(`${CONSOLE_URL}/organizations`, { waitUntil: 'domcontentloaded' });
  await page.waitForTimeout(1100);
  await page.waitForLoadState('networkidle', { timeout: 3000 }).catch(() => {});
  await measure(page, meta('organizations'), PROBES.organizations);

  // ---------- CLI 同意页 ----------
  await page.goto(`${CONSOLE_URL}/device`, { waitUntil: 'domcontentloaded' });
  await page.locator('.once-secret').first().waitFor({ timeout: 15000 }).catch(() => {});
  await page.waitForTimeout(600);
  if (await page.locator('.once-secret').first().count()) {
    await measure(page, meta('cli'), PROBES.cli);
    // 触发一次真实失败：夹具没有 /auth/device/decision 端点 → .error 渲染
    const code = page.getByPlaceholder('ABCD-EFGH').first();
    const confirm = page.getByRole('checkbox', { name: '这个代码来自我自己正在使用的终端' }).first();
    if (await code.count()) {
      await code.fill('ABCD-EFGH');
      if (await confirm.count()) await confirm.check().catch(() => {});
      const approve = page.getByRole('button', { name: '批准', exact: true }).first();
      if (await approve.count()) await approve.click().catch(() => {});
      await page.locator('.error').first().waitFor({ timeout: 5000 }).catch(() => {});
      await page.waitForTimeout(300);
      await measure(page, meta('cli-error'), PROBES.cliError);
    }
  } else {
    skipped.push({ theme, viewport: vpName, surface: 'cli', reason: '/device 未渲染 .once-secret（未登录或路由未命中）' });
  }

  // ---------- 登录页 ----------
  await page.goto(`${LOGIN_URL}/`, { waitUntil: 'domcontentloaded' });
  await page.locator('.auth-card, .auth-shell').first().waitFor({ timeout: 15000 }).catch(() => {});
  await page.waitForTimeout(800);
  if (await page.locator('.auth-card').first().count()) {
    await measure(page, meta('login'), PROBES.login);
  } else {
    skipped.push({ theme, viewport: vpName, surface: 'login', reason: `LOGIN_URL(${LOGIN_URL}) 未渲染登录页` });
  }

  await ctx.close();
}

const rel = (file) => path.relative(ROOT, file).split(path.sep).join('/');
fs.mkdirSync(OUT, { recursive: true });
console.log(`[contrast-audit] themes=${THEMES.join(',')} viewports=${VIEWPORT_NAMES.join(',')} console=${CONSOLE_URL} login=${LOGIN_URL}`);

const browser = await chromium.launch();
const started = Date.now();
for (const theme of THEMES) {
  for (const vpName of VIEWPORT_NAMES) {
    console.log(`[contrast-audit] ${theme}/${vpName} …`);
    try {
      await runThemeViewport(browser, theme, vpName);
    } catch (e) {
      skipped.push({ theme, viewport: vpName, surface: '*', reason: `流程异常：${e.message}` });
      console.error(`[contrast-audit] ${theme}/${vpName} 失败：`, e.message);
    }
  }
}
const durationMs = Date.now() - started;
await browser.close();

const measured = results.filter((r) => r.status === 'measured');
const failed = measured.filter((r) => r.pass === false);
const unmeasured = results.filter((r) => r.status !== 'measured');
const summary = {
  durationMs,
  measured: measured.length,
  passed: measured.length - failed.length,
  failed: failed.length,
  skipped: skipped.length,
  unmeasured: unmeasured.length,
  byTheme: Object.fromEntries(THEMES.map((t) => {
    const m = measured.filter((r) => r.theme === t);
    const f = m.filter((r) => r.pass === false);
    return [t, { measured: m.length, failed: f.length, worst: m.length ? Math.min(...m.map((r) => r.ratio)) : null }];
  })),
};
const payload = {
  generatedAt: new Date().toISOString(),
  consoleUrl: CONSOLE_URL,
  loginUrl: LOGIN_URL,
  themes: THEMES,
  viewports: VIEWPORT_NAMES,
  thresholds: { text: 4.5, largeText: 3, nonText: 3, alphaComposite: 'foreground alpha × 祖先 opacity 链；背景从 html 向上合成，遇不透明层停止' },
  summary,
  results: results.slice().sort((a, b) => (a.theme + a.viewport + a.surface + a.selector).localeCompare(b.theme + b.viewport + b.surface + b.selector)),
  skipped,
};
fs.writeFileSync(path.join(OUT, 'contrast.json'), JSON.stringify(payload, null, 2));

// 失败项 markdown 表
const md = [];
md.push('# 对比度审计（WCAG 实测）', '');
md.push(`生成时间：${payload.generatedAt}；浏览器：chromium ${browser.version?.() ?? ''}；命令：\`node scripts/contrast-audit.mjs\``, '');
md.push('阈值：正文 < 4.5 失败；大字（≥24px 或 ≥18.66px 且 bold）与非文本指示 < 3 失败。', '');
md.push(`总量：实测 ${summary.measured}，通过 ${summary.passed}，失败 ${summary.failed}，跳过 ${summary.skipped}。`, '');
md.push('## 失败项', '');
const failRows = failed;
if (!failRows.length) {
  md.push('（无）', '');
} else {
  md.push('| 主题 | 视口 | 页面 | selector | 文字 | 前景 | 背景 | 比值 | 阈值 | 说明 |');
  md.push('| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |');
  for (const r of failRows) {
    const text = (r.text || '').replace(/\|/g, '\\|').slice(0, 18);
    md.push(`| ${r.theme} | ${r.viewport} | ${r.surface} | \`${r.selector}\`${r.pseudo ? r.pseudo : ''} | ${text} | ${r.fg} | ${r.bg} | ${r.ratio}:1 | ${r.threshold}:1 | ${r.large ? '大字' : r.kind === 'nonText' ? '非文本' : '正文'} |`);
  }
  md.push('');
}
md.push('## 按主题', '');
md.push('| 主题 | 实测 | 失败 | 最低比值 |');
md.push('| --- | --- | --- | --- |');
for (const [t, v] of Object.entries(summary.byTheme)) md.push(`| ${t} | ${v.measured} | ${v.failed} | ${v.worst == null ? '—' : v.worst.toFixed(2)}:1 |`);
md.push('');
const near = measured.filter((r) => r.pass && r.ratio < r.threshold * 1.15).sort((a, b) => a.ratio - b.ratio);
if (near.length) {
  md.push('## 接近阈值（通过但 < 阈值×1.15，供复核）', '');
  md.push('| 主题 | 视口 | 页面 | selector | 前景 | 背景 | 比值 | 阈值 |');
  md.push('| --- | --- | --- | --- | --- | --- | --- | --- |');
  for (const r of near.slice(0, 40)) md.push(`| ${r.theme} | ${r.viewport} | ${r.surface} | \`${r.selector}\`${r.pseudo ? r.pseudo : ''} | ${r.fg} | ${r.bg} | ${r.ratio}:1 | ${r.threshold}:1 |`);
  md.push('');
}
if (unmeasured.length || skipped.length) {
  md.push('## 未测到（不伪造数值）', '');
  for (const s of skipped) md.push(`- ${s.theme}/${s.viewport}/${s.surface} \`${s.selector ?? ''}\`：${s.reason}`);
  for (const u of unmeasured) md.push(`- ${u.theme}/${u.viewport}/${u.surface} \`${u.selector}\`（${u.label}）：${u.status}${u.detail ? ' · ' + u.detail : ''}${u.note ? ' · ' + u.note : ''}`);
  md.push('');
}
fs.writeFileSync(path.join(OUT, 'contrast.md'), md.join('\n'));

console.log(`[contrast-audit] 实测 ${summary.measured}，通过 ${summary.passed}，失败 ${summary.failed}，跳过 ${summary.skipped}；用时 ${(durationMs / 1000).toFixed(1)}s`);
for (const r of failed) console.log(`  ✗ ${r.theme}/${r.viewport}/${r.surface} [${r.kind}] ${r.label} ${r.selector}${r.pseudo || ''} ${r.fg} on ${r.bg} = ${r.ratio}:1 (<${r.threshold})`);
console.log(`[contrast-audit] 数据：${rel(path.join(OUT, 'contrast.json'))}；表：${rel(path.join(OUT, 'contrast.md'))}`);
process.exitCode = 0;
