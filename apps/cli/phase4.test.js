import assert from 'node:assert/strict';
import test from 'node:test';
import { uploadFindings } from './scan.js';

test('upload payload sends the match for a server-side HMAC and does not prehash it', () => {
  const secret = 'wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY';
  const body = { source: 'cli', findings: uploadFindings([{ rule: 'aws-secret-access-key', level: 'critical', file: 'src/app.env', line: 4, match: secret }]) };
  const encoded = JSON.stringify(body);
  assert.equal(encoded.includes(secret), true);
  assert.equal(body.findings[0].match, secret);
  assert.equal(body.findings[0].fingerprint, undefined);
  assert.equal(body.findings[0].file, 'src/app.env');
});
