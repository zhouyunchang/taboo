#!/usr/bin/env node
// 将前端构建产物与 OpenAPI 契约拷入 Go embed 目录（跨平台，替代 Makefile web）。
import { cpSync, mkdirSync, rmSync, existsSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const dist = join(root, 'apps', 'web', 'dist');
const embed = join(root, 'apps', 'server-go', 'cmd', 'server', 'web');
const openapi = join(root, 'api', 'openapi.yaml');

if (!existsSync(join(dist, 'index.html'))) {
  console.error('embed-web: apps/web/dist missing — run `npm run build -w @taboo/web` first');
  process.exit(1);
}

rmSync(join(embed, 'assets'), { recursive: true, force: true });
mkdirSync(embed, { recursive: true });
cpSync(dist, embed, { recursive: true });
cpSync(openapi, join(embed, 'openapi.yaml'));
console.log('embed-web: synced apps/web/dist + api/openapi.yaml → apps/server-go/cmd/server/web');
