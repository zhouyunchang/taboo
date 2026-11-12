#!/usr/bin/env node
// 输出 go build -ldflags 字符串；可用 TABOO_VERSION / TABOO_COMMIT / BUILD_TIME 覆盖。
import { readFileSync } from 'node:fs';
import { execSync } from 'node:child_process';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const bi = 'github.com/zhouyunchang/taboo/apps/server-go/internal/buildinfo';

const version =
  process.env.TABOO_VERSION ||
  JSON.parse(readFileSync(join(root, 'package.json'), 'utf8')).version ||
  'dev';

let commit = process.env.TABOO_COMMIT || '';
if (!commit) {
  try {
    commit = execSync('git rev-parse --short=7 HEAD', { cwd: root, encoding: 'utf8' }).trim();
    try {
      execSync('git diff --quiet HEAD --', { cwd: root, stdio: 'ignore' });
    } catch {
      commit += '-dirty';
    }
  } catch {
    commit = 'unknown';
  }
}

const buildTime =
  process.env.BUILD_TIME || new Date().toISOString().replace(/\.\d{3}Z$/, 'Z');

if (process.argv.includes('--meta')) {
  process.stdout.write(`${version} ${commit}\n`);
} else {
  process.stdout.write(
    `-X ${bi}.version=${version} -X ${bi}.commit=${commit} -X ${bi}.buildTime=${buildTime}`,
  );
}
