# 设计评审 · 宣纸亮色主题与三态主题开关

- 日期：2026-11-13
- 范围：`apps/web` 控制台的亮色主题（宣纸）与用户可控切换；暗色主题保持不变。
- 配色迭代：首版为偏深暖米纸（页面 `#f0e9d8`）；用户反馈要更淡后，出 A/B/C/D 四套候选并渲染对比板（见 `proposals/`），用户选定 **A 素白宣（页面 `#f7f4ec`）**，最终实现与证据均基于 A。
- 证据：`.impeccable/review/theme/{dark,light}/*.jpg`（素白宣定稿后重拍）、`report.md`、`contrast.json`、`contrast.md`、`theme-checks.json`、`capture-manifest.json`；候选对比在 `proposals/`。
- 方法：独立批评者（read-only）只依据真实渲染截图 + 世界文档 + 源码判定，不参与构建；Lead 落实 material fixes 后由验证工程师复测。

## 结果

- 亮色文本对比度：**0 失败**（88 条文本实测全部 ≥ 4.5，最低 5.17：`.audit-row .ip`）。
- 机械验收：**45/45 PASS**；pageerror / API 5xx = 0；light `bodyBg rgb(247,244,236)`、meta `#f7f4ec`（early+late）。
- 暗色：25 个可对比测量点与改动前逐位一致，无回归。
- 仍失败仅两类且不在亮色范围：暗色既有低对比 11 条（pre-existing）+ 20 条已接受的 1px 接缝非文本边框。

## disposition

`fix-then-ship` → material fix 已修复并复测，最终判定 **ship**。

## 批评结论（theme-critic）

- **同一世界**：亮色的 page `#f0e9d8` / chrome `#e7ddc8` / raised `#fdfbf3` 是三层可分辨的纸，表头、档案左页、封印匣、dialog 字段都靠 1px 缝而不是阴影；不是通用浅色后台。
- **印的层次**：印面渐变与内框、封印匣（亮纸卡 + 封印/已封印/水印/密文）、`.cipher`（亮色 5.61–6.34:1）、版本时间线当前节点都成立；朱砂仍是唯一饱和色，只给主操作/当前/危险/印/revealed/focus，没有第二个饱和色抢权。
- **主题开关**：appbar 首位、登录 footer、落款 head、组织 topbar、CLI 同意页角落五处挂载自然；当前态（seal-tint 底 + 朱砂下划 + 朱砂图标）一眼可读，不与主操作或会话 chip 竞争；桌面 26×24、移动 24×24，`role=group` + `aria-pressed` + 键盘可达，45/45 机械检查通过。
- **Operate 扫读**：亮色没有变「软」——危险、当前、待批仍是第一批红/琥珀；审计 ip、表头、rail 标签都在阈值之上。
- **未处理表面**：dialog backdrop（38% 暖灰 + blur）、滚动条、焦点环、禁用态、空态、native select 弹层均按令牌成立或与暗色同水平。

## material fix（已修复）

| # | 现象 | 证据 | 修复 |
| --- | --- | --- | --- |
| 1 | 亮色 `.pill-warn`（pending，11px）在 `.table-scroll`（`--ink-925` 底）上 4.14:1 < 4.5；同令牌 `.ref-chip` 也偏低 | `theme/light/desktop-settings.jpg`「待处理的密钥变更」状态列 | `html[data-theme='light']` 的 `--amber` `#81540e → #744c0d`，tint/line/ring 同步到 `rgb(116 76 13 / …)`；A 案复测 `.pill-warn` **5.52 / 5.18:1** |
| 2 | 亮色 `.ref-chip`（`${env:KEY}`，11px）落在 `.cipher-value` 的朱砂 tint 之上，三层叠加后 4.00:1 < 4.5 | `theme/light/desktop-console-ref.jpg` 显示值里的引用 chip | 新增令牌 `--amber-strong`（night 仍为 `#d59a3a`，day `#60400a`），`.ref-chip` 只改前景；背景/边框不动；A 案复测 **5.68:1**（dark 5.77 不变） |

违反的约束：DESIGN「Contrast floor: body and placeholder ≥ 4.5:1」；待审批与引用解析都是 Operate 必须先扫到的边界（PRODUCT 原则 1）。暗色同组件 6.7:1 / 5.77:1，因此这是亮色缺陷，不是世界取舍。

## keep（不要动）

1. 纸的三层 + 1px 缝线系统：亮色靠纸的厚度分层，没有用阴影造卡。
2. 朱砂稀缺性：唯一饱和色仍是朱砂；状态只用 jade/amber。
3. 印的层次：封印匣、密文点、时间线节点、状态印戳都保留。
4. 三态开关的五个挂载点与当前态表达。
5. 暗色令牌零改动：25 个可对比测量点逐位一致。

## 已知取舍 / 非阻塞

- 1px 结界边框的非文本对比 <3 属接缝系统接受项（文字全部达标）。
- 暗色既有低对比文本（`.btn-primary` 4.23、`.rail-label`、`.audit-row .ip`、`.cipher` 4.0）为 pre-existing，本轮不改暗色令牌。
- 落款页（`handle_confirmed=false`）夹具不可达，未截图；主题开关与其它表面同一组件。
- 下一轮可做：CLI 同意页的 `.theme-corner` 可像登录页一样收进卡片页脚；若再改亮色令牌，优先复跑 `.pill-warn`/`.ref-chip`/`.pill-ok` 三个刚过 4.5 的 selector。
