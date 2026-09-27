import { test, expect } from '@playwright/test';

const issuer = process.env.TABOO_TEST_KEYCLOAK_ISSUER ?? 'http://127.0.0.1:18081/realms/taboo-test';
let localTokens: { access: string; refresh: string };
let slug: string;
let providerID: string;

test.beforeAll(async ({ request }) => {
  // Safe to repeat after a failed worker; this is a disposable local database.
  async function account(email: string) {
    const data = { email, password: 'fixture-password', name: 'Fixture operator' };
    const register = await request.post('/api/v1/auth/register', { data });
    const res = register.status() === 409 ? await request.post('/api/v1/auth/login', { data }) : register;
    expect(res.ok()).toBe(true); return res.json();
  }
  await account('browser-fixture@example.test');
  const owner = await account('a@example.test'); localTokens = owner.tokens; slug = 'a';
  const headers = { Authorization: `Bearer ${owner.tokens.access}` };
  const list = await (await request.get('/api/v1/orgs/a/oidc', { headers })).json();
  for (const provider of list.providers) await request.delete(`/api/v1/orgs/a/oidc/${provider.id}`, { headers });
  const create = await request.post('/api/v1/orgs/a/oidc', { headers, data: {
    name: 'Standard OIDC', issuer, client_id: 'taboo-test', client_secret: 'fixture-secret', scopes: 'openid', autocreate: true, default_role: 'viewer', role_map: {},
  }});
  expect(create.status()).toBe(201); providerID = (await create.json()).id;
});

test('real Keycloak login, cookie session, isolation, CSRF, logout', async ({ page, context }) => {
  const errors: string[] = []; page.on('pageerror', (e) => errors.push(e.message));
  await page.goto('/');
  await page.getByRole('button', { name: '使用 Standard OIDC 登录', exact: true }).click();
  await page.getByLabel('Username').fill('fixture-user');
  await page.getByLabel('Password', { exact: true }).fill('fixture-password');
  await page.getByRole('button', { name: 'Sign In', exact: true }).click();
  await expect(page.getByRole('button', { name: '退出', exact: true })).toBeVisible();
  await expect(page.getByText('外部登录 · 当前组织：', { exact: false })).toBeVisible();
  expect(await page.evaluate(() => localStorage.getItem('taboo.access'))).toBeNull();
  expect(await page.evaluate(() => localStorage.getItem('taboo.refresh'))).toBeNull();
  expect((await context.cookies()).find((c) => c.name === 'taboo.session')?.httpOnly).toBe(true);
  expect(await page.evaluate(() => document.cookie.includes('taboo.session'))).toBe(false);
  expect(await page.evaluate(async () => (await fetch('/api/v1/auth/logout', { method: 'POST' })).status)).toBe(403);
  expect(await page.evaluate(async () => (await fetch('/api/v1/orgs/browser-fixture/projects')).status)).toBe(403);
  const oldCSRF = await page.evaluate(async () => (await (await fetch('/api/v1/auth/session')).json()).csrf_token);
  const secondTab = await context.newPage();
  await secondTab.goto('/api/v1/auth/oidc/a/login');
  await expect(secondTab.getByRole('button', { name: '退出', exact: true })).toBeVisible();
  expect(await page.evaluate(async (csrf) => (await fetch('/api/v1/auth/logout', { method: 'POST', headers: { 'X-CSRF-Token': csrf } })).status, oldCSRF)).toBe(403);
  await secondTab.close();
  await page.reload(); await expect(page.getByRole('button', { name: '退出', exact: true })).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.screenshot({ path: '/tmp/taboo-federation-mobile.png' });
  await page.getByRole('button', { name: '退出', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'taboo · 禁制' })).toBeVisible();
  expect((await context.cookies()).find((c) => c.name === 'taboo.session')).toBeUndefined();
  expect(errors).toEqual([]);
});

test('connection form uses generic fields, explicit admission, accessible modal', async ({ page }) => {
  await page.addInitScript((tokens) => { localStorage.setItem('taboo.access', tokens.access); localStorage.setItem('taboo.refresh', tokens.refresh); }, localTokens);
  await page.goto('/');
  await page.getByRole('button', { name: '组织设置', exact: true }).click();
  await page.getByRole('button', { name: '用户源 / SSO', exact: true }).click();
  const trigger = page.getByRole('button', { name: '＋ 添加身份连接', exact: true });
  await trigger.click();
  const dialog = page.getByRole('dialog', { name: '添加身份连接', exact: true });
  await expect(dialog).toBeVisible();
  await dialog.getByRole('button', { name: '接入', exact: true }).click();
  await expect(dialog.getByRole('alert')).toBeVisible();
  await expect(dialog.getByLabel('Issuer URL')).toBeFocused();
  await expect(dialog.getByLabel('允许此提供方的任意已认证用户首次登录时加入本组织')).not.toBeChecked();
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(dialog).toBeVisible();
  expect(await dialog.evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(true);
  await page.screenshot({ path: '/tmp/taboo-identity-connection-mobile.png' });
  await page.keyboard.press('Escape');
  await expect(dialog).not.toBeVisible(); await expect(trigger).toBeFocused();
});

test('provider disable invalidates an existing browser session', async ({ page, request }) => {
  await page.goto('/'); await page.getByRole('button', { name: '使用 Standard OIDC 登录', exact: true }).click();
  await page.getByLabel('Username').fill('fixture-user'); await page.getByLabel('Password', { exact: true }).fill('fixture-password');
  await page.getByRole('button', { name: 'Sign In', exact: true }).click();
  await expect(page.getByRole('button', { name: '退出', exact: true })).toBeVisible();
  expect((await request.patch(`/api/v1/orgs/${slug}/oidc/${providerID}`, { headers: { Authorization: `Bearer ${localTokens.access}` }, data: { enabled: false } })).status()).toBe(200);
  await page.reload(); await expect(page.getByRole('heading', { name: 'taboo · 禁制' })).toBeVisible();
  expect(await page.evaluate(() => localStorage.getItem('taboo.access'))).toBeNull();
});

test('session transport failure has an explicit retry path', async ({ page }) => {
  let failed = false;
  await page.route('**/api/v1/auth/session', (route) => {
    if (!failed) { failed = true; return route.fulfill({ status: 503, body: '{}' }); }
    return route.continue();
  });
  await page.goto('/');
  await expect(page.getByRole('alert')).toContainText('暂时无法检查登录状态');
  await page.getByRole('button', { name: '重试', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'taboo · 禁制' })).toBeVisible();
});

test('explicitly granted developer can write and reveal dev secrets with BFF CSRF', async ({ page, request }) => {
  const update = await request.patch(`/api/v1/orgs/${slug}/oidc/${providerID}`, {
    headers: { Authorization: `Bearer ${localTokens.access}` }, data: { enabled: true, default_role: 'developer' },
  });
  expect(update.status()).toBe(200);
  await page.goto('/api/v1/auth/oidc/a/login');
  await page.getByLabel('Username').fill('fixture-user');
  await page.getByLabel('Password', { exact: true }).fill('fixture-password');
  await page.getByRole('button', { name: 'Sign In', exact: true }).click();
  await expect(page.getByRole('button', { name: '退出', exact: true })).toBeVisible();
  const uid = await page.evaluate(async () => (await (await fetch('/api/v1/me')).json()).user.id);
  const headers = { Authorization: `Bearer ${localTokens.access}` };
  const projects = await (await request.get('/api/v1/orgs/a/projects', { headers })).json();
  const pid = projects.projects[0].id;
  const environments = await (await request.get(`/api/v1/projects/${pid}/environments`, { headers })).json();
  const environment = environments.environments.find((x: { slug: string }) => x.slug === 'dev');
  const granted = await request.put(`/api/v1/orgs/a/members/${uid}/actions`, { headers, data: {
    grants: ['read', 'write', 'reveal', 'rollback'].map((action) => ({ project_id: pid, environment_id: environment.id, action })),
  }});
  expect(granted.status()).toBe(200);
  await page.reload();
  await page.getByRole('button', { name: '＋ 新建密钥', exact: true }).click();
  await page.getByLabel('Key', { exact: true }).fill('PUBLIC_FIXTURE');
  await page.getByLabel('值', { exact: true }).fill('not-a-real-secret');
  await page.getByRole('button', { name: '保存（产生新版本）', exact: true }).click();
  const row = page.getByRole('row').filter({ hasText: 'PUBLIC_FIXTURE' });
  await expect(row).toContainText('v1');
  await expect(row).not.toContainText('not-a-real-secret');
  await row.getByRole('button', { name: '显示', exact: true }).click();
  await expect(row).toContainText('not-a-real-secret');
  await row.getByRole('button', { name: '编辑', exact: true }).click();
  await page.getByLabel('值', { exact: true }).fill('second-fixture-version');
  await page.getByRole('button', { name: '保存（产生新版本）', exact: true }).click();
  await expect(row).toContainText('v2');
  await expect(row).not.toContainText('not-a-real-secret');
  const rolledBack = await page.evaluate(async () => {
    const session = await (await fetch('/api/v1/auth/session')).json();
    const projects = await (await fetch('/api/v1/orgs/a/projects')).json();
    const res = await fetch(`/api/v1/projects/${projects.projects[0].id}/secrets/PUBLIC_FIXTURE/rollback?env=dev`, {
      method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': session.csrf_token }, body: JSON.stringify({ version: 1 }),
    });
    return { status: res.status, data: await res.json() };
  });
  expect(rolledBack.status).toBe(200); expect(rolledBack.data.version).toBe(3);
  await page.reload();
  const restoredRow = page.getByRole('row').filter({ hasText: 'PUBLIC_FIXTURE' });
  await restoredRow.getByRole('button', { name: '显示', exact: true }).click();
  await expect(restoredRow).toContainText('not-a-real-secret');

  await page.getByRole('button', { name: 'prod', exact: true }).click();
  await expect(page.getByText('not-a-real-secret', { exact: true })).not.toBeVisible();
  await expect(page.getByRole('button', { name: '＋ 新建密钥', exact: true })).not.toBeVisible();
  await page.getByRole('button', { name: '退出', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'taboo · 禁制' })).toBeVisible();
});

test('identity replacement proves both sign-ins and preserves membership and secret versions', async ({ page, request }) => {
  const headers = { Authorization: `Bearer ${localTokens.access}` };
  const response = await request.post('/api/v1/orgs/a/oidc', { headers, data: {
    name: 'Replacement connection', issuer, client_id: 'taboo-test', client_secret: 'fixture-secret', scopes: 'openid', autocreate: false, public_login: false, default_role: 'admin', role_map: {},
  }});
  expect(response.status()).toBe(201); const replacement = (await response.json()).id;
  await page.goto(`/api/v1/auth/oidc/a/login?pid=${providerID}`);
  await page.getByLabel('Username').fill('fixture-user');
  await page.getByLabel('Password', { exact: true }).fill('fixture-password');
  await page.getByRole('button', { name: 'Sign In', exact: true }).click();
  await expect(page.getByRole('button', { name: '登录身份', exact: true })).toBeVisible();
  const original = await page.evaluate(async () => (await (await fetch('/api/v1/me')).json()));
  await page.getByRole('button', { name: '登录身份', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: '更换登录身份' });
  await dialog.getByLabel('新的身份连接').selectOption(replacement);
  await expect(dialog.getByRole('button', { name: '验证新身份并更换', exact: true })).toBeDisabled();
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await dialog.evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(true);
  await dialog.getByLabel('我确认解除旧身份关联，并验证新身份').check();
  await dialog.getByRole('button', { name: '验证新身份并更换', exact: true }).click();
  await page.getByLabel('Password', { exact: true }).fill('fixture-password');
  await page.getByRole('button', { name: 'Sign In', exact: true }).click();
  await expect(page.getByRole('button', { name: '退出', exact: true })).toBeVisible();
  const current = await page.evaluate(async () => (await (await fetch('/api/v1/me')).json()));
  expect(current.user.id).toBe(original.user.id); expect(current.orgs).toEqual(original.orgs);
  const row = page.getByRole('row').filter({ hasText: 'PUBLIC_FIXTURE' }); await expect(row).toContainText('v3');
  expect(await page.evaluate(() => localStorage.getItem('taboo.access'))).toBeNull();
  expect((await request.patch(`/api/v1/orgs/a/oidc/${providerID}`, { headers, data: { enabled: false } })).status()).toBe(200);
  await page.reload(); await expect(page.getByRole('button', { name: '退出', exact: true })).toBeVisible();
});
