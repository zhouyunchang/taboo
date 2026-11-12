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
  const data = await res.json() as { user: { id: string; email: string }; tokens: { access: string; refresh: string }; orgs?: unknown };
  // 控制台现在有「先落款」这道门：夹具账号先确认用户名，再进工作区。
  const confirmed = await request.post('/api/v1/me/handle/confirm', {
    headers: { Authorization: `Bearer ${data.tokens.access}` },
  });
  expect(confirmed.ok()).toBe(true);
  return data;
}

async function signIn(page: import('@playwright/test').Page, tokens: { access: string; refresh: string }) {
  await page.addInitScript((t) => {
    localStorage.setItem('taboo.access', t.access);
    localStorage.setItem('taboo.refresh', t.refresh);
  }, tokens);
  await page.goto('/');
  await expect(page.getByRole('button', { name: '账户菜单' })).toBeVisible();
}

/** 通过「账户菜单 → 管理组织」进入管理页，选中空间并进入控制台。 */
async function selectOrg(page: import('@playwright/test').Page, name: string) {
  await page.getByRole('button', { name: '账户菜单' }).click();
  await page.getByRole('button', { name: '管理组织' }).click();
  await expect(page.locator('.spaces-shell')).toBeVisible();
  await page.locator('.space-row').filter({ hasText: name }).click();
  await page.getByRole('button', { name: '进入控制台' }).click();
  await expect(page.locator('.ctx-owner')).toContainText(name);
}

test('本地账号：登录页 → 注册即进入控制台，退出回到登录页', async ({ page, request }) => {
  const email = `local-${Date.now()}@example.test`;
  await account(request, email, 'Local operator');
  const errors: string[] = [];
  page.on('pageerror', (e) => errors.push(e.message));

  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'taboo · 禁制' })).toBeVisible();
  // 门面：左边是开门的表单，右边是仿古籍竖排书页；列从左向右，字从上到下。
  const gate = await page.locator('.auth-aside').boundingBox();
  const leaf = await page.locator('.auth-lead').boundingBox();
  expect(gate && leaf && gate.x + gate.width <= leaf.x + 1).toBe(true);
  await expect(page.locator('.auth-book-body')).toHaveCSS('writing-mode', 'vertical-lr');
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

test('初次注册：先落款确认用户名，占用名被拒绝，确认后才进控制台', async ({ page, request }) => {
  const stamp = Date.now();
  const email = `onboard-${stamp}@example.test`;
  // 另一个账号注册时会自动占住一个用户名，用来验证唯一性报错。
  const takenEmail = `blocked-${stamp}@example.test`;
  const blocked = await request.post('/api/v1/auth/register', { data: { email: takenEmail, password, name: 'Blocked' } });
  expect(blocked.ok()).toBe(true);
  const takenHandle = `blocked-${stamp}`;

  const errors: string[] = [];
  page.on('pageerror', (e) => errors.push(e.message));

  await page.goto('/');
  await page.getByRole('button', { name: '没有账号？注册' }).click();
  await page.getByLabel('邮箱').fill(email);
  await page.getByLabel('昵称').fill('新来的人');
  await page.getByLabel('密码').fill(password);
  await page.getByRole('button', { name: '注册', exact: true }).click();

  // 注册后停在落款页：控制台与旧的首页横条都不出现
  await expect(page.getByRole('heading', { name: '定下你的用户名' })).toBeVisible();
  await expect(page.getByRole('button', { name: '账户菜单' })).toHaveCount(0);
  await expect(page.getByText('确认后不能再改，它会成为定位符的第一段。')).toHaveCount(0);
  await page.screenshot({ path: '/tmp/taboo-username-setup.png' });
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.screenshot({ path: '/tmp/taboo-username-setup-mobile.png' });
  await page.setViewportSize({ width: 1280, height: 800 });

  const handle = page.getByLabel('用户名', { exact: true });
  // 保留字在提交前就被拦下
  await handle.fill('login');
  await handle.press('Tab');
  await expect(page.getByText('「login」是保留字，换一个。')).toBeVisible();
  await expect(page.getByRole('button', { name: '确认用户名，进入控制台' })).toBeDisabled();

  // 已被占用的用户名：服务端拒绝，仍停在落款页
  await handle.fill(takenHandle);
  await handle.press('Tab');
  await page.getByRole('button', { name: '确认用户名，进入控制台' }).click();
  await expect(page.getByRole('alert')).toContainText('已被占用');
  await expect(page.getByRole('heading', { name: '定下你的用户名' })).toBeVisible();

  // 换成唯一的名字：确认后进入控制台，首页不再有确认用户名的横条
  await handle.fill(`operator-${stamp}`);
  await page.getByRole('button', { name: '确认用户名，进入控制台' }).click();
  await expect(page.getByRole('button', { name: '账户菜单' })).toBeVisible();
  await expect(page.getByRole('heading', { name: '定下你的用户名' })).toHaveCount(0);
  await expect(page.getByText('确认用户名', { exact: true })).toHaveCount(0);
  expect(errors).toEqual([]);
});

test('每次登录都检查用户名：未确认先落款，确认后再次进入直接进控制台', async ({ page, request }) => {
  const stamp = Date.now();
  const email = `returning-${stamp}@example.test`;
  // 直接注册、不确认用户名，模拟「已有账号但从未确认」的再次登录。
  const reg = await request.post('/api/v1/auth/register', { data: { email, password, name: 'Returning' } });
  expect(reg.ok()).toBe(true);
  const fixture = await reg.json();
  await page.addInitScript((t) => {
    localStorage.setItem('taboo.access', t.access);
    localStorage.setItem('taboo.refresh', t.refresh);
  }, fixture.tokens);

  // 登录后任何入口都先检查：命令行授权页也不能绕过落款
  await page.goto('/device');
  await expect(page.getByRole('heading', { name: '定下你的用户名' })).toBeVisible();
  await expect(page.getByRole('heading', { name: '命令行登录' })).toHaveCount(0);
  await page.getByRole('button', { name: '确认用户名，进入控制台' }).click();
  await expect(page.getByRole('heading', { name: '命令行登录' })).toBeVisible();

  // 确认后重新进入和刷新都不再拦
  await page.goto('/');
  await expect(page.getByRole('button', { name: '账户菜单' })).toBeVisible();
  await page.reload();
  await expect(page.getByRole('button', { name: '账户菜单' })).toBeVisible();
  await expect(page.getByRole('heading', { name: '定下你的用户名' })).toHaveCount(0);
});

test('组织管理页：空间列表、创建组织、进入并切回个人空间', async ({ page, request }) => {
  const owner = await account(request, `orgadmin-${Date.now()}@example.test`, '组织 owner');
  const errors: string[] = [];
  page.on('pageerror', (e) => errors.push(e.message));
  await signIn(page, owner.tokens);

  // 账户菜单现在是账户动作 + 一个「管理组织」入口
  await page.getByRole('button', { name: '账户菜单' }).click();
  await expect(page.getByRole('button', { name: '管理组织' })).toBeVisible();
  await page.waitForTimeout(260);
  await page.screenshot({ path: '/tmp/taboo-org-menu.png' });
  await page.getByRole('button', { name: '管理组织' }).click();
  await expect(page.locator('.spaces-shell')).toBeVisible();
  await expect(page.locator('.space-row').filter({ hasText: '个人空间' })).toHaveCount(1);
  expect(await page.evaluate(() => location.pathname)).toBe('/organizations');
  // 直接刷新管理页 URL 也能落到同一面（不需要先回工作台）
  await page.reload();
  await expect(page.locator('.spaces-shell')).toBeVisible();
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.screenshot({ path: '/tmp/taboo-org-admin.png' });

  // 新建组织：页内独立表单，code 有定位符前缀预览
  await page.getByRole('button', { name: '新建组织' }).click();
  await expect(page.getByRole('heading', { name: '新建组织' })).toBeVisible();
  await page.getByLabel('显示名称', { exact: true }).fill('设计系统');
  const code = `design-${Date.now()}`;
  await page.getByLabel('组织 code', { exact: true }).fill(code);
  await expect(page.locator('.space-code-preview code')).toContainText(code);
  await page.screenshot({ path: '/tmp/taboo-org-create.png' });
  await page.getByRole('button', { name: '创建组织' }).click();
  await expect(page.getByRole('heading', { name: '设计系统' })).toBeVisible();
  await expect(page.getByText('已创建组织')).toBeVisible();
  await expect(page.locator('.space-row').filter({ hasText: '设计系统' })).toHaveCount(1);
  await page.screenshot({ path: '/tmp/taboo-org-detail.png' });

  // 进入组织，工作台上下文切换
  await page.getByRole('button', { name: '进入控制台' }).click();
  await expect(page.locator('.ctx-owner')).toContainText('设计系统');
  expect(await page.evaluate(() => location.pathname)).toBe('/');

  // 从管理页切回个人空间
  await page.getByRole('button', { name: '账户菜单' }).click();
  await page.getByRole('button', { name: '管理组织' }).click();
  await page.locator('.space-row').filter({ hasText: '个人空间' }).click();
  await page.getByRole('button', { name: '进入控制台' }).click();
  await expect(page.locator('.ctx-owner')).toContainText('个人');

  // 窄屏：先看列表，选中后看详情，可返回列表
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole('button', { name: '账户菜单' }).click();
  await page.getByRole('button', { name: '管理组织' }).click();
  await expect(page.locator('.spaces-shell')).toHaveAttribute('data-mobile-view', 'list');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.screenshot({ path: '/tmp/taboo-org-admin-mobile.png' });
  await page.locator('.space-row').filter({ hasText: '个人空间' }).click();
  await expect(page.getByRole('button', { name: '空间列表' })).toBeVisible();
  await page.screenshot({ path: '/tmp/taboo-org-detail-mobile.png' });
  await page.getByRole('button', { name: '空间列表' }).click();
  await expect(page.locator('.space-row').first()).toBeVisible();
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
