import assert from 'node:assert/strict';
import test from 'node:test';
import { uploadFindings } from './scan.js';

test('upload payload keeps the fingerprint and drops the matched text', () => {
  const secret = 'wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY';
  const body = { source: 'cli', findings: uploadFindings([{ rule: 'aws-secret-access-key', level: 'critical', file: 'src/app.env', line: 4, match: secret }]) };
  const encoded = JSON.stringify(body);
  assert.equal(encoded.includes(secret), false);
  assert.equal(encoded.includes('"match"'), false);
  assert.equal(body.findings[0].fingerprint.length, 64);
  assert.match(body.findings[0].fingerprint, /^[0-9a-f]{64}$/);
  assert.equal(body.findings[0].file, 'src/app.env');
});
