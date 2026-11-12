import { expect, test } from '@playwright/test';

test('fixed rotation is visible and opens the rotation drawer', async ({ page, request }) => {
  const email = `rotation-ui-${Date.now()}@example.test`;
  const body = { email, password: 'fixture-password', name: 'Rotation UI' };
  const res = await request.post('/api/v1/auth/register', { data: body });
  expect(res.ok(), await res.text()).toBeTruthy();
  const account = await res.json();
  const headers = { Authorization: `Bearer ${account.tokens.access}` };
  expect((await request.post('/api/v1/me/handle/confirm', { headers })).ok()).toBeTruthy();

  const me = await (await request.get('/api/v1/me', { headers })).json();
  const org = me.orgs[0];
  const projects = await (await request.get(`/api/v1/orgs/${org.slug}/projects`, { headers })).json();
  const projectId = projects.projects[0].id;
  const key = `ROT_UI_${Date.now()}`;
  expect((await request.post(`/api/v1/projects/${projectId}/secrets`, { headers, data: { key, value: 'ui-old' } })).ok()).toBeTruthy();
  const fixed = await request.put(`/api/v1/projects/${projectId}/secrets/${key}/rotation?path=%2F`, {
    headers, data: { status: 'fixed', fixed_reason: 'vendor managed' },
  });
  expect(fixed.ok(), await fixed.text()).toBeTruthy();

  await page.addInitScript((tokens) => {
    localStorage.setItem('taboo.access', tokens.access);
    localStorage.setItem('taboo.refresh', tokens.refresh);
  }, account.tokens);
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.goto('/');
  await page.getByRole('button', { name: key, exact: true }).waitFor();
  await page.getByRole('button', { name: '轮换', exact: true }).first().click();
  await page.getByRole('heading', { name: '轮换', exact: true }).waitFor();
  const row = page.locator('tr', { hasText: key });
  await expect(row).toContainText('固定');
  await row.getByRole('button', { name: '查看', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog.getByRole('heading', { name: '轮换策略', exact: true })).toBeVisible();
  await expect(dialog.getByText(/固定原因/)).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
});

test('generate run through the console creates and distributes a new value', async ({ page, request }) => {
  const email = `rotation-gen-${Date.now()}@example.test`;
  const body = { email, password: 'fixture-password', name: 'Rotation Generate' };
  const res = await request.post('/api/v1/auth/register', { data: body });
  expect(res.ok(), await res.text()).toBeTruthy();
  const account = await res.json();
  const headers = { Authorization: `Bearer ${account.tokens.access}` };
  expect((await request.post('/api/v1/me/handle/confirm', { headers })).ok()).toBeTruthy();
  const me = await (await request.get('/api/v1/me', { headers })).json();
  const org = me.orgs[0];
  const projects = await (await request.get(`/api/v1/orgs/${org.slug}/projects`, { headers })).json();
  const projectId = projects.projects[0].id;
  const key = `GEN_UI_${Date.now()}`;
  expect((await request.post(`/api/v1/projects/${projectId}/secrets`, { headers, data: { key, value: 'before' } })).ok()).toBeTruthy();

  const errors: string[] = [];
  page.on('pageerror', (e) => errors.push(e.message));
  page.on('dialog', (d) => d.accept());
  await page.addInitScript((tokens) => {
    localStorage.setItem('taboo.access', tokens.access);
    localStorage.setItem('taboo.refresh', tokens.refresh);
  }, account.tokens);
  await page.goto('/');
  await page.getByRole('button', { name: key, exact: true }).click();
  const drawer = page.getByRole('dialog', { name: '密钥详情' });
  await drawer.getByRole('tab', { name: '轮换', exact: true }).click();
  await drawer.getByLabel('方式').selectOption('generate');
  await drawer.getByRole('button', { name: '保存并启用', exact: true }).click();
  await drawer.getByText('运行中', { exact: true }).first().waitFor();

  await drawer.getByRole('button', { name: '立即运行', exact: true }).click();
  await drawer.getByText('已创建轮换轮次。').waitFor();
  await drawer.getByRole('button', { name: '查看', exact: true }).first().click();
  await drawer.getByRole('button', { name: '生成新值', exact: true }).click();
  await drawer.getByText('新值（仅此屏显示一次）').waitFor();
  const value = await drawer.locator('.once-secret .mono').innerText();
  expect(value.length).toBe(32);
  await drawer.getByRole('button', { name: '完成分发', exact: true }).click();
  await drawer.getByText('已完成', { exact: true }).first().waitFor();
  expect(errors).toEqual([]);
});
