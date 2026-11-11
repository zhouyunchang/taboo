// 本地账号回归（不需要 Keycloak）。覆盖 federation.spec.ts 中除 OIDC 之外的全部界面交互，
// 并补上本轮重构新增的界面：详情抽屉、密钥面、治理面板、成员操作授权、工作负载。
// 断言的可访问名与旧版一致——它们是产品与浏览器之间的契约，不随视觉重构变化。
import { expect, test } from '@playwright/test';

const password = 'fixture-password';

async function account(request: import('@playwright/test').APIRequestContext, email: string, name: string) {
  const body = { email, password, name };
  const register = await request.post('/api/v1/auth/register', { data: body });
  const res = register.status() === 409 ? await request.post('/api/v1/auth/login', { data: body }) : register;
  expect(res.ok()).toBe(true);
  return res.json() as Promise<{ user: { id: string; email: string }; tokens: { access: string; refresh: string }; orgs?: unknown }>;
}

async function signIn(page: import('@playwright/test').Page, tokens: { access: string; refresh: string }) {
  await page.addInitScript((t) => {
    localStorage.setItem('taboo.access', t.access);
    localStorage.setItem('taboo.refresh', t.refresh);
  }, tokens);
  await page.goto('/');
  await expect(page.getByRole('button', { name: '账户菜单' })).toBeVisible();
}

/** 通过账户菜单的「当前组织」选择器切换组织，并等到工作区按新组织重挂载。 */
async function selectOrg(page: import('@playwright/test').Page, name: string) {
  await page.getByRole('button', { name: '账户菜单' }).click();
  const picker = page.getByRole('combobox', { name: '当前组织', exact: true });
  const option = picker.locator('option', { hasText: name });
  await expect(option).toHaveCount(1);
  const value = await option.getAttribute('value');
  await picker.selectOption(value ?? '');
  await expect(page.locator('.rail-owner-name')).toHaveText(name);
}

test('本地账号：登录页 → 注册即进入控制台，退出回到登录页', async ({ page, request }) => {
  const email = `local-${Date.now()}@example.test`;
  await request.post('/api/v1/auth/register', { data: { email, password, name: 'Local operator' } });
  const errors: string[] = [];
  page.on('pageerror', (e) => errors.push(e.message));

  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'taboo · 禁制' })).toBeVisible();
  await page.getByLabel('邮箱').fill(email);
  await page.getByLabel('密码').fill(password);
  await page.getByRole('button', { name: '登录', exact: true }).click();

  await expect(page.getByRole('button', { name: '账户菜单' })).toBeVisible();
  await expect(page.getByText('本地会话')).toBeVisible();
  expect(await page.evaluate(() => localStorage.getItem('taboo.access'))).toBeTruthy();
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.screenshot({ path: '/tmp/taboo-local-mobile.png' });

  await page.setViewportSize({ width: 1280, height: 800 });
  await page.getByRole('button', { name: '账户菜单' }).click();
  await page.getByRole('button', { name: '退出', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'taboo · 禁制' })).toBeVisible();
  await expect(page.getByRole('button', { name: '账户菜单' })).toHaveCount(0);
  expect(errors).toEqual([]);
});

test('本地账号：新建/显示/隐藏/编辑/删除密钥，版本抽屉，窄屏不溢出', async ({ page, request }) => {
  const owner = await account(request, 'local-owner@example.test', '本地 owner');
  const headers = { Authorization: `Bearer ${owner.tokens.access}` };
  const projects = await (await request.get('/api/v1/orgs/local-owner/projects', { headers })).json();
  const projectId = projects.projects[0].id;
  const key = `LOCAL_${Date.now()}`;
  const secret = 'local-plain-value';
  const next = 'local-plain-second';
  const errors: string[] = [];
  page.on('pageerror', (e) => errors.push(e.message));

  await signIn(page, owner.tokens);
  await page.getByRole('button', { name: '＋ 新建密钥', exact: true }).click();
  await page.getByLabel('Key', { exact: true }).fill(key);
  await page.getByLabel('值', { exact: true }).fill(secret);
  await page.getByRole('button', { name: '保存（产生新版本）', exact: true }).click();

  const row = page.getByRole('row').filter({ hasText: key });
  await expect(row).toContainText('v1');
  await expect(row).not.toContainText(secret);
  await row.getByRole('button', { name: '显示', exact: true }).click();
  await expect(row).toContainText(secret);
  await row.getByRole('button', { name: '隐藏', exact: true }).click();
  await expect(row).not.toContainText(secret);

  await row.getByRole('button', { name: '编辑', exact: true }).click();
  await page.getByLabel('值', { exact: true }).fill(next);
  await page.getByRole('button', { name: '保存（产生新版本）', exact: true }).click();
  await expect(row).toContainText('v2');
  await expect(row).not.toContainText(next);

  // 详情抽屉：版本时间线 + 掩码对比 + Escape 归还焦点
  const opener = page.getByRole('button', { name: key, exact: true });
  await opener.click();
  const drawer = page.getByRole('dialog', { name: '版本历史' });
  await expect(drawer).toBeVisible();
  await expect(drawer.getByText('v2')).toBeVisible();
  await drawer.getByRole('button', { name: '对比', exact: true }).click();
  await expect(drawer.getByText('值已掩码')).toBeVisible();
  await expect(drawer).not.toContainText(secret);
  await expect(drawer).not.toContainText(next);
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await drawer.evaluate((el) => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.keyboard.press('Escape');
  await expect(drawer).toBeHidden();
  await expect(opener).toBeFocused();

  // 删除（浏览器原生确认）
  await page.setViewportSize({ width: 1280, height: 800 });
  page.once('dialog', (d) => void d.accept());
  await row.getByRole('button', { name: '删除', exact: true }).click();
  await expect(page.getByRole('row').filter({ hasText: key })).toHaveCount(0);
  expect(errors).toEqual([]);
});

test('本地账号：新建文件夹（成功/取消/非法名/断网）都有明确反馈', async ({ page, request }) => {
  const owner = await account(request, 'local-folder@example.test', '文件夹 owner');
  const errors: string[] = [];
  page.on('pageerror', (e) => errors.push(e.message));
  await signIn(page, owner.tokens);

  // 成功：出现在树里、自动进入、并给出回执
  page.once('dialog', (d) => void d.accept('db'));
  await page.getByRole('button', { name: '＋ 新建文件夹', exact: true }).click();
  await expect(page.locator('.folder-tree')).toContainText('db');
  await expect(page.locator('.path-bar')).toContainText('db');
  await expect(page.getByText('已新建文件夹 /db/', { exact: false })).toBeVisible();

  // 再建一层子目录
  page.once('dialog', (d) => void d.accept('mysql'));
  await page.getByRole('button', { name: '＋ 新建文件夹', exact: true }).click();
  // 面包屑按层渲染：根目录 / db / mysql 三个 crumb
  await expect(page.locator('.path-bar .crumb')).toHaveCount(3);
  await expect(page.locator('.path-bar .crumb').nth(2)).toHaveText('mysql');
  await expect(page.getByText('已新建文件夹 /db/mysql/', { exact: false })).toBeVisible();

  // 取消：什么都不发生，也不报错
  page.once('dialog', (d) => void d.dismiss());
  await page.getByRole('button', { name: '＋ 新建文件夹', exact: true }).click();
  await expect(page.getByRole('alert')).toHaveCount(0);

  // 非法名：本地直接拒绝，不发请求
  let posted = 0;
  page.on('request', (r) => { if (r.method() === 'POST' && r.url().includes('/folders')) posted += 1; });
  page.once('dialog', (d) => void d.accept('a/b'));
  await page.getByRole('button', { name: '＋ 新建文件夹', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('不能包含 /');
  expect(posted).toBe(0);

  // 空格：名称是定位符的一段，含空白会让不同客户端解析到不同目录
  page.once('dialog', (d) => void d.accept('tes t'));
  await page.getByRole('button', { name: '＋ 新建文件夹', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('不能包含空格');
  expect(posted).toBe(0);
  await expect(page.locator('.folder-tree')).not.toContainText('tes t');

  // 断网：错误要说清楚“连不上服务器”，而不是 TypeError: Failed to fetch
  page.once('dialog', (d) => void d.accept('offline'));
  await page.route('**/api/v1/projects/*/folders', (r) => (r.request().method() === 'POST' ? r.abort('connectionrefused') : r.continue()));
  await page.getByRole('button', { name: '＋ 新建文件夹', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('连不上服务器');
  await expect(page.getByRole('alert')).not.toContainText('Failed to fetch');
  await page.unroute('**/api/v1/projects/*/folders');
  expect(errors).toEqual([]);
});

test('本地账号：组织设置、身份连接表单、成员操作授权、工作负载与登录策略', async ({ page, request }) => {
  const owner = await account(request, 'local-settings@example.test', '设置 owner');
  const headers = { Authorization: `Bearer ${owner.tokens.access}` };
  // 个人空间不可共享：成员与治理都放在一个共享组织里。
  const org = await request.post('/api/v1/orgs', { headers, data: { name: '本地治理', code: 'local-gov' } });
  expect(org.status(), await org.text()).toBe(201);
  const emails = ['local-dev@example.test', 'local-viewer@example.test'];
  for (const email of emails) await account(request, email, email.split('@')[0]);
  for (const [email, role] of [[emails[0], 'developer'], [emails[1], 'viewer']] as const) {
    const added = await request.post('/api/v1/orgs/local-gov/members', { headers, data: { email, role } });
    expect(added.status(), `add ${email}: ${await added.text()}`).toBe(201);
  }
  const errors: string[] = [];
  page.on('pageerror', (e) => errors.push(e.message));

  await signIn(page, owner.tokens);
  await selectOrg(page, '本地治理');
  await page.getByRole('button', { name: '组织设置', exact: true }).click();
  await expect(page.getByRole('button', { name: '成员与角色', exact: true })).toBeVisible();

  // 成员 → 操作授权
  await page.getByRole('button', { name: '操作授权', exact: true }).first().click();
  const grants = page.getByRole('dialog', { name: '明确授权的操作' });
  await expect(grants).toBeVisible();
  await expect(grants.getByLabel('项目 / 环境')).toBeVisible();
  const reveal = grants.getByLabel('查看明文', { exact: true });
  await expect(reveal).toHaveCount(1);
  await reveal.check();
  await grants.getByRole('button', { name: '保存授权', exact: true }).click();
  await expect(grants).toBeHidden();

  // 身份连接表单：原生校验 + Escape 归还焦点
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
  await page.screenshot({ path: '/tmp/taboo-local-identity-mobile.png' });
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
  await expect(trigger).toBeFocused();
  await page.setViewportSize({ width: 1280, height: 800 });

  // OAuth 工作负载：登记信任 → 绑定 → 授权。
  // 信任登记要求 issuer 在部署允许列表内、且提供方发现文档可用，本地实例没有这些条件，
  // 所以用 page.route 让这条写请求得到 201——界面剩下的流程（错误→重试→绑定）才是被测对象。
  await page.getByRole('button', { name: 'OAuth 工作负载', exact: true }).click();
  await page.getByRole('button', { name: '添加资源信任', exact: true }).click();
  const trust = page.getByRole('dialog', { name: '添加资源信任', exact: true });
  await trust.getByLabel('名称', { exact: true }).fill('Local trust');
  await trust.getByLabel('Issuer', { exact: true }).fill('https://idp.local.test');
  await trust.getByLabel('资源 audience').fill('taboo-local');
  // 真实服务器的拒绝必须留在表单里（不关窗、不丢输入）
  await trust.getByRole('button', { name: '保存信任', exact: true }).click();
  await expect(trust.getByRole('alert')).toBeVisible();
  await expect(trust.getByLabel('Issuer', { exact: true })).toHaveValue('https://idp.local.test');
  await page.route('**/oauth-trusts', (route) =>
    route.request().method() === 'POST' ? route.fulfill({ status: 201, json: { id: 'trust-fixture' } }) : route.continue());
  await trust.getByRole('button', { name: '保存信任', exact: true }).click();
  await expect(trust).toBeHidden();
  await page.unroute('**/oauth-trusts');
  // 绑定：列表里还没有信任时按钮禁用，并说明原因
  await expect(page.getByRole('button', { name: '绑定工作负载', exact: true })).toBeDisabled();

  // 登录策略 + 切换组织对话框
  await page.getByRole('button', { name: '登录策略', exact: true }).click();
  await expect(page.getByLabel('允许的登录方式')).toHaveValue('LOCAL_OR_EXTERNAL');
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  expect(errors).toEqual([]);
});

test('本地账号：机器身份与动态密钥面板渲染真实数据', async ({ page, request }) => {
  const owner = await account(request, 'local-identity@example.test', '机器身份 owner');
  const headers = { Authorization: `Bearer ${owner.tokens.access}` };
  const projects = await (await request.get('/api/v1/orgs/local-identity/projects', { headers })).json();
  const projectId = projects.projects[0].id;
  const created = await request.post('/api/v1/orgs/local-identity/identities', {
    headers, data: { name: 'local-ci', token_ttl: 900, scopes: [{ project_id: projectId, permission: 'read' }] },
  });
  expect(created.status()).toBe(201);
  const errors: string[] = [];
  page.on('pageerror', (e) => errors.push(e.message));

  await signIn(page, owner.tokens);
  await page.getByRole('button', { name: '机器身份', exact: true }).first().click();
  const identityRow = page.getByRole('row').filter({ hasText: 'local-ci' });
  await expect(identityRow).toBeVisible();

  await page.getByRole('button', { name: '动态密钥', exact: true }).first().click();
  await expect(page.getByRole('heading', { name: /动态密钥/ })).toBeVisible();
  await expect(page.getByText('尚未接入任何数据库引擎')).toBeVisible();
  expect(errors).toEqual([]);
});
