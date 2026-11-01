import { expect, test } from '@playwright/test';

const secret = 'phase4-plain-left';
const revised = 'phase4-plain-right';

test('version diff masks values, escape restores focus, and a viewer cannot read them', async ({ page, request }) => {
  const ownerBody = { email: 'phase4-owner@example.test', password: 'fixture-password', name: 'Phase4 owner' };
  const ownerRes = await request.post('/api/v1/auth/register', { data: ownerBody });
  const owner = ownerRes.status() === 409 ? await (await request.post('/api/v1/auth/login', { data: ownerBody })).json() : await ownerRes.json();
  const headers = { Authorization: `Bearer ${owner.tokens.access}` };
  const viewerBody = { email: 'phase4-viewer@example.test', password: 'fixture-password', name: 'Phase4 viewer' };
  const viewerRes = await request.post('/api/v1/auth/register', { data: viewerBody });
  const viewer = viewerRes.status() === 409 ? await (await request.post('/api/v1/auth/login', { data: viewerBody })).json() : await viewerRes.json();
  expect((await request.post('/api/v1/orgs/phase4-owner/members', { headers, data: { email: viewerBody.email, role: 'viewer' } })).ok()).toBeTruthy();
  const projects = await (await request.get('/api/v1/orgs/phase4-owner/projects', { headers })).json();
  const projectId = projects.projects[0].id;
  const key = `PHASE4_${Date.now()}`;
  expect((await request.post(`/api/v1/projects/${projectId}/secrets?env=dev`, { headers, data: { key, value: secret } })).ok()).toBeTruthy();
  expect((await request.post(`/api/v1/projects/${projectId}/secrets?env=dev`, { headers, data: { key, value: revised, expectedVersion: 1 } })).ok()).toBeTruthy();

  await page.addInitScript((tokens) => {
    const chosen = sessionStorage.getItem('phase4-tokens');
    const use = chosen ? JSON.parse(chosen) : tokens;
    localStorage.setItem('taboo.access', use.access);
    localStorage.setItem('taboo.refresh', use.refresh);
  }, owner.tokens);
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.goto('/');
  const opener = page.getByRole('button', { name: key, exact: true });
  await opener.click();
  const dialog = page.getByRole('dialog', { name: '版本历史' });
  await dialog.getByRole('button', { name: '对比', exact: true }).click();
  await expect(dialog.getByText('值已掩码')).toBeVisible();
  await expect(dialog).not.toContainText(secret);
  await expect(dialog).not.toContainText(revised);
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await dialog.evaluate((el) => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
  await expect(opener).toBeFocused();

  await page.evaluate((tokens) => sessionStorage.setItem('phase4-tokens', JSON.stringify(tokens)), viewer.tokens);
  await page.goto('/');
  const orgSelect = page.getByLabel('当前组织');
  const viewerOrg = orgSelect.locator('option', { hasText: '(viewer)' });
  await expect(viewerOrg).toHaveCount(1);
  await orgSelect.selectOption(await viewerOrg.getAttribute('value') ?? '');
  await page.getByRole('button', { name: key, exact: true }).click();
  const viewerDialog = page.getByRole('dialog', { name: '版本历史' });
  await expect(viewerDialog.getByText('需要 reveal 权限才能对比版本内容。')).toBeVisible();
  await expect(viewerDialog.getByRole('button', { name: '对比', exact: true })).toHaveCount(0);
  await expect(viewerDialog).not.toContainText(secret);
  await expect(viewerDialog).not.toContainText(revised);
});
