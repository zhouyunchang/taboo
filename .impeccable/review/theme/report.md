# 双主题视觉与机械验收报告（最终批 · A「素白宣」）

- 生成时间：2026-11-13 12:05（Asia/Shanghai）
- 输入：最终配色 **A「素白宣」**（styles.css `html[data-theme='light']` 整块换成 A；theme.ts `FALLBACK_META` light、index.html 内联 bootstrap 的 meta 均改为 `#f7f4ec`；DESIGN.md day palette 已同步）。旧版 light 值（如 bodyBg `rgb(240,233,216)`）全部作废。
- 方法：**全部来自真实渲染**——chromium 153.0.8010.12（playwright），夹具 `scripts/fixture-server.mjs`（控制台 7301 / 登录 7303），构建 `apps/web/dist`（`index-Cki2Hm6Q.js` / `index-DJoKn1cr.css`）。
- 产物：**52** 张截图（48 固定矩阵 + 4 张 `.ref-chip` 证据）、**192** 条对比度实测、45 项机械检查。**未修改任何产品源码**。

## 结论

| 维度 | 判定 | 数字 |
| --- | --- | --- |
| 主题运行时 / 持久化 / 跟随系统 / 无闪 / 控件 / 挂载点 / 390px 溢出 | **PASS** | 检查 **45/45**；pageerror 0；API 5xx 0；截图失败 0 |
| 亮色主题视觉落地（A 素白宣；9 surface + 3 crop × 2 视口） | **PASS** | 52 张截图（含 4 张 `.ref-chip` 证据），5 个挂载点全部就位 |
| **亮色文本对比度** | **PASS（0 失败）** | 88 条亮色文本实测全部 ≥ 阈值；最低 **5.17**（`.audit-row .ip`） |
| 亮色非文本边框 <3 | 接受（已知取舍 a） | 10 条，装饰性接缝 |
| 暗色文本对比度 | FAIL（**pre-existing，非回归**） | 11 条，令牌本轮未改 |
| 暗色非文本边框 <3 | 接受（已知取舍 a） | 10 条，装饰性接缝 |

**总判定：A「素白宣」+ 三态切换可验收通过。** 关键校验全部命中：light bodyBg **`rgb(247, 244, 236)`**，meta theme-color **`#f7f4ec`**（early + late），亮色文本 **0 失败**，暗色无任何变化。

## 1. 亮色文本对比度（A 实测，全部通过）

| surface | selector | 历史问题值 | A 之前（上一版纸色） | **A 最终实测** | 判定 | 实测前景 / 背景 |
| --- | --- | --- | --- | --- | --- | --- |
| console-secrets（desktop+mobile） | `.cipher` | 3.04:1 | 5.61:1 | **6.21:1** | PASS | `rgb(95 85 70)` / `rgb(241 236 226)`（表格面板） |
| record（desktop+mobile） | `.cipher` | 3.18:1 | 6.34:1 | **6.08:1** | PASS | `rgb(106 96 80)` / `rgb(255 253 250)` |
| settings / settings-sync（desktop+mobile） | `.pill-ok` | 4.33:1 | 4.69:1 | **5.25:1** | PASS | `rgb(39 99 64)` / `rgb(221 222 210)` |
| settings（`.table-scroll` 内 pending，11px） | `.pill-warn` | **4.14:1**（critic） | 5.24:1 | **5.52:1** | PASS | `rgb(116 80 15)` / `rgb(231 224 209)` |
| settings-sync | `.pill-warn` | — | 4.76:1 | **5.18:1** | PASS | `rgb(116 80 15)` / `rgb(226 217 201)` |
| console-revealed（cloud/UPSTREAM_API 显示后，desktop+mobile） | `.ref-chip` | 4.00:1 | 5.18:1 | **5.68:1** | PASS | `rgb(96 64 10)`（`--amber-strong #60400a`）/ `rgb(217 198 180)` |

其他亮色重点项（A 实测）：`.btn-primary` 5.67、`.rail-label` 5.54、`.table th` 5.24、`.section-hint` 5.62、`.tag` 9.33、`.pop-item-sub` 6.08、`.audit-row .ip` **5.17（亮色最低值）**——与 Lead 预期“A 的最低文本对比约 5.17”一致。

暗色同选择器：`.ref-chip` **5.77**、`.pill-warn` 7.06 / 6.73、`.cipher` 4.00 / 4.08（pre-existing）、`.pill-ok` 7.02，均与上一批逐位一致。

## 2. 仍失败项（暗色，pre-existing，非本次回归）

| 主题 | 视口 | surface | selector | 实测前景 / 背景 | 比值 | 期望 |
| --- | --- | --- | --- | --- | --- | --- |
| dark | desktop+mobile | console-secrets、login | `.btn-primary` | `rgb(255 246 234)` / `rgb(212 67 46)` | **4.23:1** | ≥4.5 |
| dark | desktop+mobile | console-secrets、record | `.cipher` | `rgb(121 113 101)` / `rgb(16 14 11)`；`rgb(120 112 100)` / `rgb(10 9 7)` | **4.00 / 4.08:1** | ≥4.5 |
| dark | desktop | console-secrets、settings | `.rail-label` | `rgb(74 66 53)` / `rgb(10 9 7)` | **2.01:1** | ≥4.5 |
| dark | desktop | audit | `.audit-row .ip` | `rgb(74 66 53)` / `rgb(16 14 11)` | **1.95:1** | ≥4.5 |

共 11 条记录（`.btn-primary`×4、`.cipher`×4、`.rail-label`×2、`.audit-row .ip`×1），数值与上一批完全一致。

## 3. 非文本指示 <3（已知取舍 a：1px 接缝系统，接受项，不计失败）

| selector | dark | light（A） | 说明 |
| --- | --- | --- | --- |
| `.status-chip` 边框 | 1.22 | 1.40 | 装饰线；文字对比 9.99/8.23 |
| `.pill-ok` 边框 | 1.75 | 1.62 | 装饰线；文字对比 7.02/5.25 |
| `.pill-warn` 边框 | 1.68 | 1.70 | 装饰线；文字对比 7.06/**5.52** |
| `.pill-accent` 边框 | 1.47 | 1.83 | 装饰线；文字对比 4.53/5.68 |
| `.btn-danger` 边框 | 1.46 | 1.79 | 装饰线；文字对比 5.38/5.93 |
| `.chapter-dot`（none 变体） | 1.45 | 1.73 | 5px 状态点；轮换状态另以文案/章节呈现 |

## 4. 机械验收明细（45/45 PASS）

数据：`theme-checks.json`（每项含 expected/actual）。

| 类别 | 项数 | A 版关键实测 |
| --- | --- | --- |
| no-flash | 4 | dist 内联 bootstrap@412 `<script type=module`@1423 `<link rel=stylesheet`@1503；commit 阶段 `data-theme=dark/light` 正确；**light 首帧 bodyBg 已是 `rgb(247, 244, 236)`、meta 已 `#f7f4ec`**；load 后不翻转 |
| palette | 2 | light bodyBg **`rgb(247, 244, 236)`**（#f7f4ec）；dark bodyBg `rgb(10, 9, 7)` |
| persistence | 2 | 刷新前后 `data-theme` / `data-theme-mode` / localStorage 均为 light/dark |
| system | 4 | system+OS dark→dark；system+OS light→light；空存储+OS light→light；空存储+OS dark→dark |
| control | 15 | 桌面/移动：group×1、3 按钮；初始 aria-pressed 正确；点击三态 → `data-theme`/`data-theme-mode`/localStorage 同步；Tab 可达三按钮；主题按钮 desktop 26×24px、mobile 24×24px；390px 顶栏视觉只显示项目名 `core-api`（`.ctx-owner` 被 ≤760px 隐藏，无省略裁切） |
| mount | 8 | 控制台 `.appbar`、登录 `.auth-foot`、组织页 `.spaces-topbar-actions`、CLI `.theme-corner` × 两主题：控件在（3 按钮）且页面 `data-theme` 正确 |
| meta | 2 | `meta[name=theme-color]` early+late = **`#f7f4ec`**（light）/ `#0a0907`（dark），与 `--meta-theme-color` 一致 |
| overflow | 8 | 390px：登录/控制台/设置/组织页 × 两主题 `documentElement.scrollWidth=390=innerWidth`，diff=0；设置页 `.table-scroll` 内部 435px 表属容器内滚动 |
| 运行期错误 | — | pageerror **0 组**；API 5xx **0 组**；截图失败 **0**（`capture-manifest.json` 逐张记录） |

## 5. 回归结论（暗色主题）

- **计算样式回归：无。** 25 个可对比暗色测量点与上一批逐位一致（`.status-chip` 9.99、`.rail-link.active` 14.75、`.table th` 6.29、`.auth-book-body` 10.17、`.pop-item-sub` 6.10、`.flash-dialog` 5.13；失败点 4.23/4.00/4.08/2.01/1.95 相同）；`.ref-chip` 暗色 **5.77** 不变。
- **行为回归：无。** 持久化、system 跟随、三态点击、键盘可达、无闪、meta theme-color、390px 溢出暗色断言全 PASS；pageerror / 5xx 均 0。
- A 的改动只落在 `html[data-theme='light']` 与 light 的 meta 常量（theme.ts fallback / index.html bootstrap），暗色令牌零变化；`meta-theme-color-dark` 仍为 `#0a0907` 已机械验证。
- 暗色截图内容时间戳随夹具重启变化，不做逐像素对比。

## 6. 已知取舍（Lead 决定，不计失败）

- (a) 1px 结界边框属世界的接缝系统：`.status-chip`/`.pill-*` 边框、`.chapter-dot.none`、`.btn-danger` 边框低于 3:1 非文本阈值属**接受项**；文字对比全部达标。
- (b) 暗色是既有线上主题：`.btn-primary` 4.23、`--ink-600` 文本（`.rail-label` / `.audit-row .ip`）、`.cipher` 4.00/4.08 为既有问题，本轮不修改暗色令牌；标注 pre-existing、非回归。
- (c) 落款页（`handle_confirmed=false`）夹具不可达，未截图；其主题开关与登录/CLI 同一 `ThemeSwitch` 组件（5 个可达挂载点已验证）。
- (d) `.chapters-note` 仅存在于 CSS（当前 TSX 无使用），无真实渲染可测；`.faint`/`.section-hint` 在控制台/登录页无实例，实测取自 CLI 同意页 / 设置页 / 组织页，均 PASS。
- (e) 设置表面为「个人设置」：夹具 `FIXTURE_MODE=new` 的会话为外部登录，组织页「进入控制台」被禁用，无法进入「潮汐工作室」的组织设置；组织页本身已截图。
- (f) `.ref-chip` 只在「cloud 文件夹 → UPSTREAM_API → 显示」的引用值上渲染；本轮通过该路径实测并附专项截图。

## 7. 视觉证据

目录 `.impeccable/review/theme/{dark,light}/`，`{desktop|mobile}-{surface}.jpg`；每主题每视口 13 张，共 **52 张**：
`console-secrets`、`console-revealed`、`console-ref`（cloud/UPSTREAM_API 显示后的 `.ref-chip`）、`record`、`dialog`、`audit`、`settings`、`organizations`、`cli-consent`、`login` + `appbar`、`appbar-actions`、`theme-control` 裁剪。逐张清单（含 pageerror/5xx）在 `capture-manifest.json`。

最值得先看的三张：
1. `.impeccable/review/theme/light/desktop-console-secrets.jpg` — A「素白宣」控制台（#f7f4ec 底 + 墨字 + 朱砂）
2. `.impeccable/review/theme/light/desktop-appbar-actions.jpg` — 主题开关定稿（26×24，太阳选中）
3. `.impeccable/review/theme/light/mobile-console-secrets.jpg` — 390px 完整 `core-api` + 24×24 开关

另附 `.ref-chip` 专项证据：`.impeccable/review/theme/light/desktop-console-ref.jpg`（暗色同路径）。

## 8. 对比度方法与数据

- `contrast.json`：**192** 条实测（theme/viewport/surface/selector/fg/bg/ratio/fontSize/fontWeight/large/threshold/opacityChain/bgFallback）。
- `contrast.md`：**31** 条失败 + 接近阈值复核表 + 未测到清单。
- 分支：实测 192 / 通过 161 / 失败 31 / 跳过 22；dark 96 实测 21 失败；light 96 实测 **10 失败（全部为接受的非文本边框，文本 0 失败，最低 5.17）**。
- 算法：`getComputedStyle` 取真实渲染色；前景 alpha × 祖先 `opacity` 链；背景从 `html` 向下合成（半透明层按 alpha×opacity，遇不透明层止，`bgFallback=false`）；阈值 4.5（正文）/ 3（大字 ≥24px 或 ≥18.66px 且 bold；非文本）。

## 9. 复现命令

```powershell
# 1) 构建
npm run build -w @taboo/web

# 2) 两个夹具服务（DIST_DIR 必须是绝对路径）
$env:DIST_DIR='C:\Users\cloud\src\taboo\apps\web\dist'; $env:PORT='7301'; $env:FIXTURE_MODE='new'; node scripts/fixture-server.mjs
$env:DIST_DIR='C:\Users\cloud\src\taboo\apps\web\dist'; $env:PORT='7303'; $env:FIXTURE_MODE='login'; node scripts/fixture-server.mjs

# 3) 截图 + 45 项机械验收
node scripts/capture-theme.mjs --themes=dark,light --viewports=desktop,mobile
# 只复跑检查（不重拍截图）：
node scripts/capture-theme.mjs --checks-only=1

# 4) 对比度审计（含 .pill-warn / .ref-chip 覆盖）
node scripts/contrast-audit.mjs --themes=dark,light --viewports=desktop,mobile
```

产物：`capture-manifest.json`、`theme-checks.json`、`contrast.json`、`contrast.md`、`capture.log`、`contrast.log`、`{dark,light}/*.jpg`、本报告。
