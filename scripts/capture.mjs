// 截图巡检：npm exec -- playwright 通过 @playwright/test 的 chromium 运行。
// 用法：node scripts/capture.mjs <baseURL> <outDir> [mode]
// mode=app 走已登录控制台；mode=login 走登录页。
import { chromium } from 'playwright';
import fs from 'node:fs';

const base = process.argv[2] || 'http://127.0.0.1:7302';
const out = process.argv[3] || '/tmp/shots/old';
const mode = process.argv[4] || 'app';
fs.mkdirSync(out, { recursive: true });

const browser = await chromium.launch();

async function shot(page, name) {
  await page.waitForTimeout(450);
  await page.screenshot({ path: `${out}/${name}.png`, fullPage: true });
  console.log('✓', name);
}

async function run(viewport, tag) {
  const ctx = await browser.newContext({ viewport, deviceScaleFactor: 2, locale: 'zh-CN', timezoneId: 'Asia/Shanghai' });
  const page = await ctx.newPage();
  const errors = [];
  page.on('pageerror', (e) => errors.push(e.message));
  await page.goto(base + '/', { waitUntil: 'load' });
  await page.waitForTimeout(500);
  if (mode === 'login') {
    await shot(page, `${tag}-login`);
    await ctx.close();
    return errors;
  }
  await shot(page, `${tag}-01-secrets`);
  // 展开一个密钥的值
  const eye = page.getByRole('button', { name: '显示', exact: true }).first();
  if (await eye.count()) { await eye.click(); await shot(page, `${tag}-02-reveal`); }
  // 版本抽屉
  const key = page.locator('button.key-link, td.key button, .secret-key-btn').first();
  if (await key.count()) {
    await key.click(); await page.waitForTimeout(500);
    await shot(page, `${tag}-03-detail`);
    await page.keyboard.press('Escape');
  }
  // 逐个导航
  for (const [nav, name] of [
    ['机器身份', '04-identities'],
    ['动态密钥', '05-dynamic'],
    ['审计日志', '06-audit'],
    ['扫描报告', '07-scans'],
    ['组织设置', '08-settings'],
  ]) {
    const b = page.getByRole('button', { name: nav, exact: true }).first();
    if (!(await b.count())) continue;
    await b.click();
    await page.waitForTimeout(700);
    await shot(page, `${tag}-${name}`);
    // 设置页：逐个二级分区
    if (nav === '组织设置') {
      for (const [sub, sname] of [['成员与角色', '08a-members'], ['用户源 / SSO', '08b-sso'], ['登录策略', '08c-security'], ['OAuth 工作负载', '08d-workloads'], ['审批与访问', '08e-approvals'], ['Secret Sync', '08f-sync'], ['Webhooks', '08g-webhooks']]) {
        const s = page.getByRole('button', { name: sub, exact: true }).first();
        if (!(await s.count())) continue;
        await s.click(); await page.waitForTimeout(600);
        await shot(page, `${tag}-${sname}`);
      }
      // 身份连接对话框
      const sso = page.getByRole('button', { name: '用户源 / SSO', exact: true }).first();
      if (await sso.count()) {
        await sso.click(); await page.waitForTimeout(400);
        const add = page.getByRole('button', { name: '＋ 添加身份连接', exact: true });
        if (await add.count()) {
          await add.click(); await page.waitForTimeout(500);
          await shot(page, `${tag}-08h-dialog`);
          await page.keyboard.press('Escape');
        }
      }
    }
  }
  await ctx.close();
  return errors;
}

const all = [];
all.push(...await run({ width: 1440, height: 900 }, 'desktop'));
all.push(...await run({ width: 390, height: 844 }, 'mobile'));
await browser.close();
if (all.length) { console.log('page errors:', all); process.exitCode = 2; } else console.log('no page errors');
