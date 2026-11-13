# 对比度审计（WCAG 实测）

生成时间：2026-11-13T04:03:09.204Z；浏览器：chromium 153.0.8010.12；命令：`node scripts/contrast-audit.mjs`

阈值：正文 < 4.5 失败；大字（≥24px 或 ≥18.66px 且 bold）与非文本指示 < 3 失败。

总量：实测 192，通过 161，失败 31，跳过 22。

## 失败项

| 主题 | 视口 | 页面 | selector | 文字 | 前景 | 背景 | 比值 | 阈值 | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| dark | desktop | console | `.btn-primary` | 新建密钥 | rgb(255 246 234) | rgb(212 67 46) | 4.23:1 | 4.5:1 | 正文 |
| dark | desktop | console | `.cipher` | •••••••• | rgb(121 113 101) | rgb(16 14 11) | 4:1 | 4.5:1 | 正文 |
| dark | desktop | console | `.rail-label` | 密钥面 | rgb(74 66 53) | rgb(10 9 7) | 2.01:1 | 4.5:1 | 正文 |
| dark | desktop | console | `.status-chip` | 外部登录· owner | rgb(36 31 24) | rgb(10 9 7) | 1.22:1 | 3:1 | 非文本 |
| dark | desktop | record | `.cipher` | •••••••••••• | rgb(120 112 100) | rgb(10 9 7) | 4.08:1 | 4.5:1 | 正文 |
| dark | desktop | record | `.chapter-dot` |  | rgb(56 49 36) | rgb(20 18 14) | 1.45:1 | 3:1 | 非文本 |
| dark | desktop | audit | `.audit-row .ip` | 203.0.113.42 | rgb(74 66 53) | rgb(16 14 11) | 1.95:1 | 4.5:1 | 正文 |
| dark | desktop | settings | `.settings-nav .rail-label` | 治理 | rgb(74 66 53) | rgb(10 9 7) | 2.01:1 | 4.5:1 | 正文 |
| dark | desktop | settings | `.pill-ok` | approved | rgb(44 65 49) | rgb(16 14 11) | 1.75:1 | 3:1 | 非文本 |
| dark | desktop | settings | `.btn-danger` | 拒绝 | rgb(83 32 23) | rgb(16 14 11) | 1.46:1 | 3:1 | 非文本 |
| dark | desktop | organizations | `.pill-warn` | 变更审批 | rgb(71 52 22) | rgb(10 9 7) | 1.68:1 | 3:1 | 非文本 |
| dark | desktop | organizations | `.pill-accent` | 当前 | rgb(91 39 28) | rgb(28 24 19) | 1.47:1 | 3:1 | 非文本 |
| dark | desktop | login | `.btn-primary` | 登录 | rgb(255 246 234) | rgb(212 67 46) | 4.23:1 | 4.5:1 | 正文 |
| dark | mobile | console | `.btn-primary` | 新建密钥 | rgb(255 246 234) | rgb(212 67 46) | 4.23:1 | 4.5:1 | 正文 |
| dark | mobile | console | `.cipher` | •••••••• | rgb(121 113 101) | rgb(16 14 11) | 4:1 | 4.5:1 | 正文 |
| dark | mobile | record | `.cipher` | •••••••••••• | rgb(120 112 100) | rgb(10 9 7) | 4.08:1 | 4.5:1 | 正文 |
| dark | mobile | record | `.chapter-dot` |  | rgb(56 49 36) | rgb(10 9 7) | 1.55:1 | 3:1 | 非文本 |
| dark | mobile | settings | `.pill-ok` | approved | rgb(44 65 49) | rgb(16 14 11) | 1.75:1 | 3:1 | 非文本 |
| dark | mobile | settings | `.btn-danger` | 拒绝 | rgb(83 32 23) | rgb(16 14 11) | 1.46:1 | 3:1 | 非文本 |
| dark | mobile | organizations | `.pill-accent` | 当前 | rgb(91 39 28) | rgb(28 24 19) | 1.47:1 | 3:1 | 非文本 |
| dark | mobile | login | `.btn-primary` | 登录 | rgb(255 246 234) | rgb(212 67 46) | 4.23:1 | 4.5:1 | 正文 |
| light | desktop | console | `.status-chip` | 外部登录· owner | rgb(222 212 196) | rgb(252 249 243) | 1.4:1 | 3:1 | 非文本 |
| light | desktop | record | `.chapter-dot` |  | rgb(206 194 171) | rgb(255 253 250) | 1.73:1 | 3:1 | 非文本 |
| light | desktop | settings | `.pill-ok` | approved | rgb(176 192 174) | rgb(241 236 226) | 1.62:1 | 3:1 | 非文本 |
| light | desktop | settings | `.btn-danger` | 拒绝 | rgb(218 167 152) | rgb(241 236 226) | 1.79:1 | 3:1 | 非文本 |
| light | desktop | organizations | `.pill-warn` | 变更审批 | rgb(202 188 161) | rgb(247 244 236) | 1.7:1 | 3:1 | 非文本 |
| light | desktop | organizations | `.pill-accent` | 当前 | rgb(224 173 159) | rgb(250 246 238) | 1.83:1 | 3:1 | 非文本 |
| light | mobile | record | `.chapter-dot` |  | rgb(206 194 171) | rgb(247 244 236) | 1.6:1 | 3:1 | 非文本 |
| light | mobile | settings | `.pill-ok` | approved | rgb(176 192 174) | rgb(241 236 226) | 1.62:1 | 3:1 | 非文本 |
| light | mobile | settings | `.btn-danger` | 拒绝 | rgb(218 167 152) | rgb(241 236 226) | 1.79:1 | 3:1 | 非文本 |
| light | mobile | organizations | `.pill-accent` | 当前 | rgb(224 173 159) | rgb(250 246 238) | 1.83:1 | 3:1 | 非文本 |

## 按主题

| 主题 | 实测 | 失败 | 最低比值 |
| --- | --- | --- | --- |
| dark | 96 | 21 | 1.22:1 |
| light | 96 | 10 | 1.40:1 |

## 接近阈值（通过但 < 阈值×1.15，供复核）

| 主题 | 视口 | 页面 | selector | 前景 | 背景 | 比值 | 阈值 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| dark | desktop | organizations | `.pill-accent` | rgb(232 87 63) | rgb(46 28 22) | 4.53:1 | 4.5:1 |
| dark | mobile | organizations | `.pill-accent` | rgb(232 87 63) | rgb(46 28 22) | 4.53:1 | 4.5:1 |
| dark | desktop | console-flash | `.flash-dialog` | rgb(232 87 63) | rgb(23 20 15) | 5.13:1 | 4.5:1 |
| dark | mobile | console | `.mobile-nav button.active` | rgb(232 87 63) | rgb(23 20 15) | 5.13:1 | 4.5:1 |
| dark | mobile | console-flash | `.flash-dialog` | rgb(232 87 63) | rgb(23 20 15) | 5.13:1 | 4.5:1 |
| light | desktop | audit | `.audit-row .ip` | rgb(107 97 79) | rgb(241 236 226) | 5.17:1 | 4.5:1 |

## 未测到（不伪造数值）

- dark/desktop/console `.mobile-nav button.active`：该视口不适用（仅 mobile）
- dark/mobile/console `.rail-label`：该视口不适用（仅 desktop）
- dark/mobile/console `.rail-link.active`：该视口不适用（仅 desktop）
- dark/mobile/console `.status-chip`：该视口不适用（仅 desktop）
- dark/mobile/console `.status-chip`：该视口不适用（仅 desktop）
- dark/mobile/console `.status-chip .dot`：该视口不适用（仅 desktop）
- dark/mobile/record `.chapters-note`：该视口不适用（仅 desktop）
- dark/mobile/audit `.audit-row .ip`：该视口不适用（仅 desktop）
- dark/mobile/settings `.settings-nav .rail-label`：该视口不适用（仅 desktop）
- dark/mobile/settings `.settings-nav .rail-link.active`：该视口不适用（仅 desktop）
- dark/mobile/login `.auth-book-body`：该视口不适用（仅 desktop）
- light/desktop/console `.mobile-nav button.active`：该视口不适用（仅 mobile）
- light/mobile/console `.rail-label`：该视口不适用（仅 desktop）
- light/mobile/console `.rail-link.active`：该视口不适用（仅 desktop）
- light/mobile/console `.status-chip`：该视口不适用（仅 desktop）
- light/mobile/console `.status-chip`：该视口不适用（仅 desktop）
- light/mobile/console `.status-chip .dot`：该视口不适用（仅 desktop）
- light/mobile/record `.chapters-note`：该视口不适用（仅 desktop）
- light/mobile/audit `.audit-row .ip`：该视口不适用（仅 desktop）
- light/mobile/settings `.settings-nav .rail-label`：该视口不适用（仅 desktop）
- light/mobile/settings `.settings-nav .rail-link.active`：该视口不适用（仅 desktop）
- light/mobile/login `.auth-book-body`：该视口不适用（仅 desktop）
- dark/desktop/console `.faint`（.faint（11–12px 次要文字））：not-found-or-hidden · 当前页面/视口找不到可见元素（可能被 media query 隐藏或页面无该状态）
- dark/desktop/console `.section-hint`（.section-hint）：not-found-or-hidden · 当前页面/视口找不到可见元素（可能被 media query 隐藏或页面无该状态）
- dark/desktop/record `.chapters-note`（.chapters-note（章节注））：not-found-or-hidden · 当前页面/视口找不到可见元素（可能被 media query 隐藏或页面无该状态）
- dark/desktop/settings `.tag`（.tag 边框）：not-found-or-hidden · 当前页面/视口找不到可见元素（可能被 media query 隐藏或页面无该状态）
- dark/desktop/login `.faint`（.faint（登录页））：not-found-or-hidden · 当前页面/视口找不到可见元素（可能被 media query 隐藏或页面无该状态）
- dark/mobile/console `.faint`（.faint（11–12px 次要文字））：not-found-or-hidden · 当前页面/视口找不到可见元素（可能被 media query 隐藏或页面无该状态）
- dark/mobile/console `.section-hint`（.section-hint）：not-found-or-hidden · 当前页面/视口找不到可见元素（可能被 media query 隐藏或页面无该状态）
- dark/mobile/console `.secret-table th`（.secret-table th（密钥表头，sticky 背景））：not-found-or-hidden · 当前页面/视口找不到可见元素（可能被 media query 隐藏或页面无该状态）
- dark/mobile/settings `.tag`（.tag 边框）：not-found-or-hidden · 当前页面/视口找不到可见元素（可能被 media query 隐藏或页面无该状态）
- dark/mobile/organizations `.pill-warn`（.pill-warn）：not-found-or-hidden · 当前页面/视口找不到可见元素（可能被 media query 隐藏或页面无该状态）
- dark/mobile/organizations `.section-hint`（.section-hint（组织页））：not-found-or-hidden · 当前页面/视口找不到可见元素（可能被 media query 隐藏或页面无该状态）
- dark/mobile/organizations `.pill-warn`（.pill-warn 边框）：not-found-or-hidden · 当前页面/视口找不到可见元素（可能被 media query 隐藏或页面无该状态）
- dark/mobile/login `.faint`（.faint（登录页））：not-found-or-hidden · 当前页面/视口找不到可见元素（可能被 media query 隐藏或页面无该状态）
- light/desktop/console `.faint`（.faint（11–12px 次要文字））：not-found-or-hidden · 当前页面/视口找不到可见元素（可能被 media query 隐藏或页面无该状态）
- light/desktop/console `.section-hint`（.section-hint）：not-found-or-hidden · 当前页面/视口找不到可见元素（可能被 media query 隐藏或页面无该状态）
- light/desktop/record `.chapters-note`（.chapters-note（章节注））：not-found-or-hidden · 当前页面/视口找不到可见元素（可能被 media query 隐藏或页面无该状态）
- light/desktop/settings `.tag`（.tag 边框）：not-found-or-hidden · 当前页面/视口找不到可见元素（可能被 media query 隐藏或页面无该状态）
- light/desktop/login `.faint`（.faint（登录页））：not-found-or-hidden · 当前页面/视口找不到可见元素（可能被 media query 隐藏或页面无该状态）
- light/mobile/console `.faint`（.faint（11–12px 次要文字））：not-found-or-hidden · 当前页面/视口找不到可见元素（可能被 media query 隐藏或页面无该状态）
- light/mobile/console `.section-hint`（.section-hint）：not-found-or-hidden · 当前页面/视口找不到可见元素（可能被 media query 隐藏或页面无该状态）
- light/mobile/console `.secret-table th`（.secret-table th（密钥表头，sticky 背景））：not-found-or-hidden · 当前页面/视口找不到可见元素（可能被 media query 隐藏或页面无该状态）
- light/mobile/settings `.tag`（.tag 边框）：not-found-or-hidden · 当前页面/视口找不到可见元素（可能被 media query 隐藏或页面无该状态）
- light/mobile/organizations `.pill-warn`（.pill-warn）：not-found-or-hidden · 当前页面/视口找不到可见元素（可能被 media query 隐藏或页面无该状态）
- light/mobile/organizations `.section-hint`（.section-hint（组织页））：not-found-or-hidden · 当前页面/视口找不到可见元素（可能被 media query 隐藏或页面无该状态）
- light/mobile/organizations `.pill-warn`（.pill-warn 边框）：not-found-or-hidden · 当前页面/视口找不到可见元素（可能被 media query 隐藏或页面无该状态）
- light/mobile/login `.faint`（.faint（登录页））：not-found-or-hidden · 当前页面/视口找不到可见元素（可能被 media query 隐藏或页面无该状态）
