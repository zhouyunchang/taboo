#!/usr/bin/env node
// 全栈开发模式：同时启动 Go server（apps/server-go，go run）和 @taboo/web（Vite）。
// 任一进程退出或收到 Ctrl-C 时，两个子进程都会被终止。
import { spawn } from 'node:child_process';

const procs = [
  ['server', 'npm', ['run', 'dev:server']],
  ['web', 'npm', ['run', 'dev:web']],
];

const children = [];

function shutdown(code = 0) {
  for (const child of children) {
    if (!child.killed) child.kill('SIGTERM');
  }
  process.exit(code);
}

for (const [name, cmd, args] of procs) {
  const child = spawn(cmd, args, { stdio: 'inherit', shell: process.platform === 'win32' });
  child.on('exit', (code, signal) => {
    if (signal) return; // 被 shutdown 主动终止
    console.error(`\n[dev] ${name} exited (code ${code}), shutting down...`);
    shutdown(code ?? 1);
  });
  children.push(child);
}

process.on('SIGINT', () => shutdown(0));
process.on('SIGTERM', () => shutdown(0));
