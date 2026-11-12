# 密钥记录页 · 实现截图

2026-11-13 落地后的运行截图，连接本地 Postgres 与真实 API。演示数据来自测试账号 `record-fixture`，其中的密钥值均为合成值。

- `record.png` — 桌面，8 个版本的记录页（封印 / 档案 / 版本时间线；右侧章节一次只读一章）。
- `compare.png` — 版本对比：显式显示明文后的整值差异；默认状态下两块都是 12 点掩码。
- `rotation.png` — 轮换章节：策略摘要、动作、最近轮次。
- `mobile-record.png` — 390px 手机宽度的记录页；底部「密钥」是长页面的出口。

页面实现：`apps/web/src/pages/SecretRecord.tsx`；章节组件在 `apps/web/src/components/SecretRotation.tsx`、`SecretCompare.tsx`、`SecretTraces.tsx`。
