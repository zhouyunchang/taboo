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
    expect(res.ok()).toBe(true);
    const fixture = await res.json();
    // 控制台入口多了一道「先落款」：夹具账号先确认用户名，避免落到落款页。
    expect((await request.post('/api/v1/me/handle/confirm', {
      headers: { Authorization: `Bearer ${fixture.tokens.access}` },
    })).ok()).toBe(true);
    return fixture;
  }
  await account('browser-fixture@example.test');
  const owner = await account('a@example.test'); localTokens = owner.tokens;
  const headers = { Authorization: `Bearer ${owner.tokens.access}` };
  // Personal spaces cannot be shared or federated; create a normal org for OIDC.
  const orgCreate = await request.post('/api/v1/orgs', { headers, data: { name: 'Federation shared', code: 'federation-shared' } });
  expect([201, 409]).toContain(orgCreate.status());
  const me = await (await request.get('/api/v1/me', { headers })).json();
  const shared = me.orgs.find((item: { slug: string }) => item.slug === 'federation-shared');
  expect(shared).toBeTruthy();
  slug = shared.slug;
  const list = await (await request.get(`/api/v1/orgs/${slug}/oidc`, { headers })).json();
  for (const provider of list.providers) await request.delete(`/api/v1/orgs/${slug}/oidc/${provider.id}`, { headers });
  const provider = {
    name: 'Standard OIDC', issuer, client_id: 'taboo-test', client_secret: 'fixture-secret', scopes: 'openid', autocreate: true, default_role: 'viewer', role_map: {},
  };
  const create = await request.post(`/api/v1/orgs/${slug}/oidc`, { headers, data: provider });
  if (create.status() === 409) {
    // A previous failed worker can leave a provider with the same issuer behind;
    // reuse and normalise it instead of failing the whole file in beforeAll.
    const existing = (await (await request.get(`/api/v1/orgs/${slug}/oidc`, { headers })).json()).providers
      .find((item: { issuer: string }) => item.issuer === issuer);
    expect(existing, await create.text()).toBeTruthy();
    providerID = existing.id;
    expect((await request.patch(`/api/v1/orgs/${slug}/oidc/${providerID}`, { headers, data: { ...provider, enabled: true } })).ok()).toBe(true);
  } else {
    expect(create.status(), await create.text()).toBe(201);
    providerID = (await create.json()).id;
  }
});

/** 首次外部登录会先落到「落款」页；已确认用户名的会话直接进控制台。 */
async function enterConsole(page: import('@playwright/test').Page) {
  await page.waitForSelector('.onboard-card, .appbar');
  if (await page.locator('.onboard-card').count()) {
    await page.getByLabel('用户名', { exact: true }).fill('fixture-user');
    await page.getByRole('button', { name: '确认用户名，进入控制台' }).click();
    await expect(page.getByRole('heading', { name: '定下你的用户名' })).toHaveCount(0);
  }
}

/** 外部会话也在账户菜单里退出；控制台本身不再有裸「退出」按钮。 */
async function expectSignedIn(page: import('@playwright/test').Page) {
  await expect(page.getByRole('button', { name: '账户菜单' })).toBeVisible();
}

async function logout(page: import('@playwright/test').Page) {
  await page.getByRole('button', { name: '账户菜单' }).click();
  await page.getByRole('button', { name: '退出', exact: true }).click();
}

test('real Keycloak login, cookie session, isolation, CSRF, logout', async ({ page, context }) => {
  const errors: string[] = []; page.on('pageerror', (e) => errors.push(e.message));
  await page.goto('/');
  await page.getByRole('button', { name: '使用 Standard OIDC 登录', exact: true }).click();
  await page.getByLabel('Username').fill('fixture-user');
  await page.getByLabel('Password', { exact: true }).fill('fixture-password');
  await page.getByRole('button', { name: 'Sign In', exact: true }).click();
  await enterConsole(page);
  await expectSignedIn(page);
  await expect(page.getByText('外部登录 · 当前组织：', { exact: false })).toBeVisible();
  expect(await page.evaluate(() => localStorage.getItem('taboo.access'))).toBeNull();
  expect(await page.evaluate(() => localStorage.getItem('taboo.refresh'))).toBeNull();
  expect((await context.cookies()).find((c) => c.name === 'taboo.session')?.httpOnly).toBe(true);
  expect(await page.evaluate(() => document.cookie.includes('taboo.session'))).toBe(false);
  expect(await page.evaluate(async () => (await fetch('/api/v1/auth/logout', { method: 'POST' })).status)).toBe(403);
  expect(await page.evaluate(async () => (await fetch('/api/v1/orgs/browser-fixture/projects')).status)).toBe(403);
  const oldCSRF = await page.evaluate(async () => (await (await fetch('/api/v1/auth/session')).json()).csrf_token);
  const secondTab = await context.newPage();
  await secondTab.goto(`/api/v1/auth/oidc/${slug}/login`);
  await expectSignedIn(secondTab);
  expect(await page.evaluate(async (csrf) => (await fetch('/api/v1/auth/logout', { method: 'POST', headers: { 'X-CSRF-Token': csrf } })).status, oldCSRF)).toBe(403);
  await expect(page.getByRole('heading', {name:'登录身份已变化'})).toBeVisible();
  await secondTab.close();
  await page.reload(); await expectSignedIn(page);
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.screenshot({ path: '/tmp/taboo-federation-mobile.png' });
  await logout(page);
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
  await enterConsole(page);
  await expectSignedIn(page);
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
  await page.goto(`/api/v1/auth/oidc/${slug}/login`);
  await page.getByLabel('Username').fill('fixture-user');
  await page.getByLabel('Password', { exact: true }).fill('fixture-password');
  await page.getByRole('button', { name: 'Sign In', exact: true }).click();
  await enterConsole(page);
  await expectSignedIn(page);
  const uid = await page.evaluate(async () => (await (await fetch('/api/v1/me')).json()).user.id);
  const headers = { Authorization: `Bearer ${localTokens.access}` };
  const projects = await (await request.get(`/api/v1/orgs/${slug}/projects`, { headers })).json();
  const pid = projects.projects[0].id;
  const granted = await request.put(`/api/v1/orgs/${slug}/members/${uid}/actions`, { headers, data: {
    grants: ['read', 'write', 'reveal', 'rollback'].map((action) => ({ project_id: pid, action })),
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
  const rolledBack = await page.evaluate(async (org) => {
    const session = await (await fetch('/api/v1/auth/session')).json();
    const projects = await (await fetch(`/api/v1/orgs/${org}/projects`)).json();
    const res = await fetch(`/api/v1/projects/${projects.projects[0].id}/secrets/PUBLIC_FIXTURE/rollback`, {
      method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': session.csrf_token }, body: JSON.stringify({ version: 1 }),
    });
    return { status: res.status, data: await res.json() };
  }, slug);
  expect(rolledBack.status).toBe(200); expect(rolledBack.data.version).toBe(3);
  await page.reload();
  const restoredRow = page.getByRole('row').filter({ hasText: 'PUBLIC_FIXTURE' });
  await restoredRow.getByRole('button', { name: '显示', exact: true }).click();
  await expect(restoredRow).toContainText('not-a-real-secret');

  await logout(page);
  await expect(page.getByRole('heading', { name: 'taboo · 禁制' })).toBeVisible();
});

test('identity replacement proves both sign-ins and preserves membership and secret versions', async ({ page, request }) => {
  const headers = { Authorization: `Bearer ${localTokens.access}` };
  const response = await request.post(`/api/v1/orgs/${slug}/oidc`, { headers, data: {
    name: 'Replacement connection', issuer, client_id: 'taboo-test', client_secret: 'fixture-secret', scopes: 'openid', autocreate: false, public_login: false, default_role: 'admin', role_map: {},
  }});
  expect(response.status()).toBe(201); const replacement = (await response.json()).id;
  await page.goto(`/api/v1/auth/oidc/${slug}/login?pid=${providerID}`);
  await page.getByLabel('Username').fill('fixture-user');
  await page.getByLabel('Password', { exact: true }).fill('fixture-password');
  await page.getByRole('button', { name: 'Sign In', exact: true }).click();
  await enterConsole(page);
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
  await enterConsole(page);
  await expectSignedIn(page);
  const current = await page.evaluate(async () => (await (await fetch('/api/v1/me')).json()));
  expect(current.user.id).toBe(original.user.id); expect(current.orgs).toEqual(original.orgs);
  const row = page.getByRole('row').filter({ hasText: 'PUBLIC_FIXTURE' }); await expect(row).toContainText('v3');
  expect(await page.evaluate(() => localStorage.getItem('taboo.access'))).toBeNull();
  expect((await request.patch(`/api/v1/orgs/${slug}/oidc/${providerID}`, { headers, data: { enabled: false } })).status()).toBe(200);
  await page.reload(); await expectSignedIn(page);
});

test('external owner manages standard governance and safely retries a lost write response', async ({ page, request }) => {
  const headers = { Authorization: `Bearer ${localTokens.access}` };
  expect((await request.patch(`/api/v1/orgs/${slug}/oidc/${providerID}`,{headers,data:{enabled:true}})).ok()).toBe(true);
  await page.goto(`/api/v1/auth/oidc/${slug}/login?pid=${providerID}`);
  await page.getByLabel('Username').fill('fixture-user');await page.getByLabel('Password',{exact:true}).fill('fixture-password');await page.getByRole('button',{name:'Sign In',exact:true}).click();
  await enterConsole(page);
  await expectSignedIn(page);
  const uid=await page.evaluate(async()=>(await(await fetch('/api/v1/me')).json()).user.id);
  expect((await request.patch(`/api/v1/orgs/${slug}/members/${uid}`,{headers,data:{role:'owner'}})).ok()).toBe(true);
  await page.reload();
  await page.getByRole('button',{name:'组织设置',exact:true}).click();
  await page.getByRole('button',{name:'成员与角色',exact:true}).click();
  // Exact external subject is represented by the provider's display name.
  const grantButtons=page.getByRole('button',{name:'操作授权',exact:true});
  await expect(grantButtons.first()).toBeVisible();await grantButtons.first().click();
  const grants=page.getByRole('dialog',{name:'明确授权的操作'});
  await expect(grants.getByLabel('项目 / 环境')).toBeVisible();
  await expect(grants.getByLabel('查看明文',{exact:true})).toBeVisible();
  await page.keyboard.press('Escape');await expect(grants).not.toBeVisible();
  await page.getByRole('button',{name:'OAuth 工作负载',exact:true}).click();
  await page.getByRole('button',{name:'添加资源信任',exact:true}).click();
  const trust=page.getByRole('dialog',{name:'添加资源信任',exact:true});
  await trust.getByLabel('名称',{exact:true}).fill('Public resource fixture');
  await trust.getByLabel('Issuer',{exact:true}).fill(issuer);
  await trust.getByLabel('资源 audience').fill('taboo-test');
  await trust.getByLabel('发现协议').selectOption('oidc');
  await trust.getByLabel('令牌验证').selectOption('INTROSPECTION_RFC7662');
  await trust.getByLabel('资源服务器 client_id',{exact:true}).fill('taboo-test');
  await trust.getByLabel('资源服务器 client_secret',{exact:true}).fill('fixture-secret');
  await trust.getByRole('button',{name:'保存信任',exact:true}).click();await expect(trust).not.toBeVisible();
  await page.getByRole('button',{name:'绑定工作负载',exact:true}).click();
  const binding=page.getByRole('dialog',{name:'绑定外部工作负载'});
  await binding.getByLabel('名称',{exact:true}).fill('Fixture workload');await binding.getByLabel('Subject',{exact:true}).fill('fixture-service-subject');await binding.getByLabel('Client ID',{exact:true}).fill('fixture-service');await binding.getByRole('button',{name:'创建绑定',exact:true}).click();await expect(binding).not.toBeVisible();
  await page.getByRole('button',{name:'操作授权',exact:true}).click();
  await expect(grants.getByLabel('查看目录',{exact:true})).toBeVisible();await grants.getByLabel('查看目录',{exact:true}).check();await grants.getByRole('button',{name:'保存授权',exact:true}).click();await expect(grants).not.toBeVisible();
  await expect(page.getByText('1 项 · 启用',{exact:true})).toBeVisible();
  await page.setViewportSize({width:390,height:844});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
  await page.screenshot({path:'/tmp/taboo-workload-governance-mobile.png'});
  await page.getByRole('button',{name:'用户源 / SSO',exact:true}).click();await page.getByRole('button',{name:'会话策略',exact:true}).first().click();
  await expect(page.getByRole('dialog',{name:'登录会话安全策略'}).getByLabel('会话有效期（秒）')).toBeVisible();await page.keyboard.press('Escape');
  await page.getByRole('button',{name:'登录策略',exact:true}).click();await expect(page.getByLabel('允许的登录方式')).toHaveValue('LOCAL_OR_EXTERNAL');
  await page.getByRole('button',{name:'切换组织',exact:true}).click();await expect(page.getByRole('dialog',{name:'切换组织',exact:true})).toBeVisible();await page.keyboard.press('Escape');
  await page.getByRole('button',{name:'dev',exact:true}).click();
  await page.getByRole('button',{name:'＋ 新建密钥',exact:true}).click();await page.getByLabel('Key',{exact:true}).fill('LOST_RESPONSE');await page.getByLabel('值',{exact:true}).fill('public-retry-fixture');
  const receipts:string[]=[];
  await page.route('**/secrets?**',async route=>{
    if(route.request().method()!=='POST')return route.continue();
    receipts.push(route.request().headers()['idempotency-key']);
    if(receipts.length===1){await route.fetch();return route.abort('failed');}
    return route.continue();
  });
  await page.getByRole('button',{name:'保存（产生新版本）',exact:true}).click();await expect(page.getByRole('dialog')).toBeVisible();
  await expect(page.getByRole('dialog').getByText('Failed to fetch',{exact:false})).toBeVisible();
  await page.getByRole('button',{name:'保存（产生新版本）',exact:true}).click();
  const row=page.getByRole('row').filter({hasText:'LOST_RESPONSE'});await expect(row).toContainText('v1');expect(receipts.length).toBe(2);expect(receipts[0]).toBeTruthy();expect(receipts[1]).toBe(receipts[0]);
  const prior=await(await request.get(`/api/v1/orgs/${slug}/members/${uid}/actions`,{headers})).json();
  expect((await request.put(`/api/v1/orgs/${slug}/members/${uid}/actions`,{headers,data:{grants:[]}})).ok()).toBe(true);
  await page.evaluate(()=>window.dispatchEvent(new Event('focus')));
  await expect(row).not.toBeVisible();await expect(page.getByRole('button',{name:'＋ 新建密钥',exact:true})).not.toBeVisible();
  expect((await request.put(`/api/v1/orgs/${slug}/members/${uid}/actions`,{headers,data:prior})).ok()).toBe(true);

});
