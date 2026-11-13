# 亮色主题配色候选（2026-11-13）

用户反馈「宣纸 · 暖米纸色颜色最好再淡些」。以下四套是同一套令牌、只换纸阶与派生 alpha 的候选，全部为真实页面渲染（Playwright 在已登录夹具上覆盖 `html[data-theme='light']` 令牌后截图，不是色卡）。

现状基线：页面 `#f0e9d8` · 铬/凹面 `#e7ddc8` · 纸卡 `#fdfbf3` · 缝 `#d2c5ac`。

| 候选 | 页面 | 铬/凹面 | 纸卡 | 缝 | 说明 |
| --- | --- | --- | --- | --- | --- |
| **A 素白宣** | `#f7f4ec` | `#f1ece2` | `#fffdfa` | `#ded4c4` | 明显更淡、仍留一点暖纸；**用户已选，已落实现** |
| B 米白 | `#f4f0e5` | `#eee9dd` | `#fffdf7` | `#d9cfbc` | 只淡一档，米纸感最足 |
| C 冷瓷白 | `#f6f5f1` | `#efeee9` | `#fffefb` | `#dcd8cd` | 去暖调，最中性 |
| D 极淡暖白 | `#f9f7f1` | `#f3f0e8` | `#ffffff` | `#e2dbcb` | 几乎纯白 |

- 四套的亮色文本对比度均通过（最低：A 5.17 / B 4.88 / C 4.91 / D 5.37，按实际令牌与叠层背景计算）。
- 对比板：`palettes/palettes-console.jpg`、`palettes/palettes-login.jpg`。
- 单方案截图：`option-{A,B,C,D}-{desktop-console,desktop-login,mobile-console}.jpg`。
- 最终实现采用 A 的完整令牌块（含 appbar/backdrop/焦点环/阴影/水洗的纸面重算），见 `apps/web/src/styles.css` 的 `html[data-theme='light']`；DESIGN.md 的 Day 调色板已同步。
