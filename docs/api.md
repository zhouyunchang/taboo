# API 参考

契约本体为 [api/openapi.yaml](../api/openapi.yaml)（OpenAPI 3.1，前后端唯一事实源），
运行时文档由 Scalar 渲染：`GET /api/docs`。

## 约定

- 错误统一为 `{code, message, details}`
- Bearer 鉴权：`Authorization: Bearer <access>`（15min）+ refresh token 旋转（30 天）
- 登录类接口限流 5 次/分钟/IP（`TABOO_LOGIN_RATE_LIMIT` 可调）

## 核心资源

| 端点 | 说明 |
|---|---|
| `POST /api/v1/auth/register` `…/login` `…/refresh` | 注册 / 登录 / 刷新 |
| `POST /api/v1/auth/totp/*` | TOTP 2FA（开启/验证/关闭/恢复码登录） |
| `GET /api/v1/auth/oidc/{slug}/login` `…/callback` | OIDC SSO（M5 #13） |
| `GET /api/v1/me` | 当前用户 + 组织成员关系 |
| `GET/POST /api/v1/orgs/{slug}/projects` | 项目；`GET/POST …/projects/{pid}/environments` 环境 |
| `GET/POST /api/v1/projects/{pid}/secrets` | 密钥列表 / upsert（产生新版本） |
| `GET/DELETE /api/v1/projects/{pid}/secrets/{key}` | reveal（审计）/ 删除 |
| `GET …/secrets/{key}/versions` `POST …/rollback` | 版本历史 / 回滚 |
| `GET /api/v1/projects/{pid}/export?env=dev` | 导出 .env（审计） |
| `GET/POST/DELETE /api/v1/orgs/{slug}/identities*` | 机器身份 + client_credentials 换 token |
| `GET/POST /api/v1/projects/{pid}/dynamic-engines*` | 动态密钥引擎（PostgreSQL 短期账号 lease） |
| `GET/POST/DELETE /api/v1/orgs/{slug}/sync/targets*` | Secret Sync 目标（M5 #11） |
| `GET /api/v1/orgs/{slug}/sync/runs` | 同步记录（指纹审计） |
| `GET/POST/DELETE /api/v1/orgs/{slug}/webhooks*` | Webhook 订阅 + 投递日志（M5 #12） |
| `GET/POST/DELETE /api/v1/orgs/{slug}/oidc*` | OIDC IdP 配置（M5 #13） |
| `GET /api/v1/orgs/{slug}/audit` | 审计查询（actor/action/resource/from/to 筛选） |
| `GET /api/v1/orgs/{slug}/audit/export` | 审计导出 CSV/JSONL（M5 #14） |

## SDK

- **Go**：`packages/sdk-go`（原生手写薄封装）
- **MCP Server**：`apps/mcp` — AI Agent 经 Model Context Protocol 只读访问（scope 受限 + 全审计）
- **CLI**：`apps/cli` — `login / run / get / set / export / scan`

类型生成：`npm run types -w @taboo/web`（openapi-typescript）。
