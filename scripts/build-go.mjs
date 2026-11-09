#!/usr/bin/env node
// 跨平台编译 apps/server-go（避免 just backtick 在 Windows PowerShell 下非 UTF-8）。
import { execFileSync, execSync } from 'node:child_process';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const serverDir = join(root, 'apps', 'server-go');
const exe = process.platform === 'win32' ? '.exe' : '';
const out = join('bin', `taboo-server${exe}`);

const ldflags = execFileSync(process.execPath, [join(root, 'scripts', 'ldflags.mjs')], {
  cwd: root,
  encoding: 'utf8',
}).trim();

execFileSync('go', ['build', `-ldflags=${ldflags}`, '-o', out, './cmd/server'], {
  cwd: serverDir,
  stdio: 'inherit',
  env: process.env,
});

console.log(`build-go: ${join(serverDir, out)}`);
