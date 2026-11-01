import assert from 'node:assert/strict';
import test from 'node:test';
import { ApiError, Client } from './index.js';

test('node sdk matches the Go consumer contract', async (t) => {
  if (process.env.TABOO_TEST_CONSUMER !== '1') { t.skip('run scripts/test-consumers.sh'); return; }
  const client = new Client(process.env.TABOO_SERVER);
  const scopes = ['read', 'write', 'reveal', 'export'];
  if (process.env.TABOO_AUTH_MODE === 'oauth') {
    await client.withOAuthClientCredentials({
      issuer: process.env.TABOO_OAUTH_ISSUER, metadataMode: 'oauth', clientID: process.env.TABOO_CLIENT_ID,
      clientSecret: process.env.TABOO_CLIENT_SECRET, scopes, resource: 'https://taboo.example/api', organization: 'a', trustID: process.env.TABOO_OAUTH_TRUST_ID,
    });
  } else client.withIdentity(process.env.TABOO_CLIENT_ID, process.env.TABOO_CLIENT_SECRET);
  const created = await client.upsertSecret('project-a', 'dev', '/', 'NODE_SECRET', 'node-fixture-value', '', null, { idempotencyKey: 'node-consumer-create', expectedVersion: 0 });
  assert.ok(created.version >= 1);
  const replay = await client.upsertSecret('project-a', 'dev', '/', 'NODE_SECRET', 'node-fixture-value', '', null, { idempotencyKey: 'node-consumer-create', expectedVersion: 0 });
  assert.equal(replay.version, created.version);
  const listed = await client.listSecrets('project-a', 'dev', '/');
  assert.ok(listed.secrets.some((item) => item.key === 'NODE_SECRET'));
  const value = await client.getSecret('project-a', 'dev', '/', 'NODE_SECRET');
  assert.equal(value.value, 'node-fixture-value');
  const exported = await client.exportSecrets('project-a', 'dev');
  assert.match(exported, /NODE_SECRET=node-fixture-value/);
  await assert.rejects(() => client.listSecrets('project-b', 'dev', '/'), (err) => err instanceof ApiError && err.status === 403);
  await client.upsertSecret('project-a', 'dev', '/', 'NODE_REF_TARGET', 'node-resolved', '', null, { idempotencyKey: 'node-ref-target', expectedVersion: 0 });
  await client.upsertSecret('project-a', 'dev', '/', 'NODE_REF_SRC', '${dev:NODE_REF_TARGET}', '', null, { idempotencyKey: 'node-ref-src', expectedVersion: 0 });
  assert.equal((await client.getSecret('project-a', 'dev', '/', 'NODE_REF_SRC')).value, '${dev:NODE_REF_TARGET}');
  assert.equal((await client.getSecret('project-a', 'dev', '/', 'NODE_REF_SRC', { resolve: true })).value, 'node-resolved');
  const preview = await client.importEnv('project-a', 'dev', '/', 'NODE_FROM_ENV=imported\nNODE_REF_TARGET=clash\n', '', true);
  assert.equal(preview.created, 1);
  assert.equal(preview.conflicts.length, 1);
  const imported = await client.importEnv('project-a', 'dev', '/', 'NODE_FROM_ENV=imported\n', 'skip', false, { idempotencyKey: 'node-import' });
  assert.equal(imported.created, 1);
  const report = await client.createScanReport('a', 'sdk-node', [{ rule_id: 'generic-api-key', level: 'warning', file: 'src/app.env', line: 2, fingerprint: 'ab'.repeat(32) }]);
  assert.equal(report.finding_count, 1);
  assert.equal(JSON.stringify(report).includes('plaintext-secret'), false);
  const reports = await client.listScanReports('a');
  assert.ok(reports.reports.length >= 1);
  if (process.env.TABOO_AUTH_MODE === 'oauth') {
    const limited = new Client(client.baseURL);
    await limited.withOAuthClientCredentials({
      issuer: process.env.TABOO_OAUTH_ISSUER, metadataMode: 'oauth', clientID: process.env.TABOO_CLIENT_ID,
      clientSecret: process.env.TABOO_CLIENT_SECRET, scopes: ['write'], resource: 'https://taboo.example/api', organization: 'a', trustID: process.env.TABOO_OAUTH_TRUST_ID,
    });
    await assert.rejects(() => limited.getSecret('project-a', 'dev', '/', 'NODE_SECRET'), (err) => err instanceof ApiError && err.status === 403);
    const wrong = new Client(client.baseURL);
    await wrong.withOAuthClientCredentials({
      issuer: process.env.TABOO_OAUTH_ISSUER, metadataMode: 'oauth', clientID: process.env.TABOO_CLIENT_ID,
      clientSecret: process.env.TABOO_CLIENT_SECRET, scopes: ['write'], resource: 'https://taboo.example/api', organization: 'b', trustID: process.env.TABOO_OAUTH_TRUST_ID,
    });
    await assert.rejects(() => wrong.listSecrets('project-a', 'dev', '/'), (err) => err instanceof ApiError && err.status === 401);
  }
});
