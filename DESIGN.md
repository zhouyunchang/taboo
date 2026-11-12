# taboo UI design context

<!-- impeccable:design-schema 1 -->

Built with the 禁制 / 印鉴 redesign (2026-11-11, direction seed `9ee163f2`). This document describes the world as built; it does not generate a second theme. `apps/web/src/styles.css` remains the only runtime source of tokens and shared element styles, and `src/design/Icon.tsx` + `src/design/primitives.tsx` are the only shared component vocabulary.

## The world

密钥是一枚被封印的东西，界面是那道禁制本身。墨底如夜，朱砂为印；结界划界，密文等宽。

Three images carry every screen: **墨** (ink: the ground and its layers), **朱砂** (vermilion: the seal, the danger, the current thing), **结界** (the barrier: 1px seams that divide regions instead of shadows and rounded card stacks). Anything that does not serve one of those three is not in the world.

## Palette

Defined in `:root` of `styles.css`; never re-declare a hex outside that block.

| Role | Token | Value | Used for |
| --- | --- | --- | --- |
| Ink ground | `--ink-950` | `#0a0907` | page field |
| Rail / chrome | `--ink-925` `--ink-900` | `#100e0b` `#14120e` | rail, appbar wash, surfaces, tables |
| Raised field | `--ink-850` `--ink-800` | `#1c1813` `#231e17` | controls, hover, avatars |
| Seam | `--border` `--border-strong` | `#241f18` `#383124` | all dividers and control borders |
| Foreground | `--porcelain` / `--porcelain-dim` / `--porcelain-faint` | `#f2ead9` `#c9bea9` `#9c9283` | body / secondary / tertiary |
| Seal | `--vermilion` `--vermilion-hi` `--vermilion-lo` | `#d4432e` `#e8573f` `#a8301f` | primary action, current item, danger, focus ring, seals |
| Seal tint | `--seal-tint` `--seal-line` | `rgb(212 67 46 / 10%)` / `34%` | revealed value chip, `--danger` buttons, once-secret |
| Jade (ok) | `--jade` `--jade-tint` | `#6db98a` | success, active identity, diff additions |
| Amber (warn) | `--amber` `--amber-tint` | `#d59a3a` | pending approvals, references, degraded session |

**One saturated colour carries authority.** Vermilion may appear only for: the primary action on a screen, the current/active item, danger or revocation, the revealed value, and the seal mark itself. Everything else is ink, porcelain or a status hue. Status hues colour state and never decoration. Text on vermilion uses `--on-seal` (`#fff6ea`), never a grey.

Contrast floor: body and placeholder ≥ 4.5:1, large text ≥ 3:1. Secondary text on a tinted surface is tinted from that hue, never greyed.

## Type

| Step | Size | Use |
| --- | --- | --- |
| 11 | `--fs-11` | labels, uppercase column heads, badges, timestamps, hints |
| 12 | `--fs-12` | secondary copy, field labels, table cells in compact tables |
| 13 | `--fs-13` | body, controls, key names |
| 14 | `--fs-14` | base, dialog headings, form values |
| 15–17 | `--fs-15` `--fs-17` | section and card headings |
| 20 | `--fs-20` | page title (`h1`/`h2` in `.page-head`) |
| 26–34 | `--fs-26` `--fs-34` | the login book page's 题签 and its single principle line |

- `--font-ui`: Inter when installed, then the system UI stack, then PingFang / Source Han / YaHei for Chinese. `font-synthesis: none` — no faux bold or oblique across the CJK fallback.
- `--font-mono`: system monospace. Used for every identifier, key name, project slug, issuer, timestamp, and all ciphertext. It is data, not costume.
- `--font-cjk-display`: Songti stack, system CJK serif first (`Songti SC` / `STSong` / `SimSun`, then Noto / Source Han). Reserved for the 「禁」 seal glyph, the wordmark's Chinese half, and the login book page's vertical text.
- The seal glyph is set by hand: `.seal-mark` is a vermilion square with a 1px inner rule and a 600-weight 「禁」. It is the only brand mark.
- Tracking stops at `-0.02em` on headings; `font-variant-numeric: tabular-nums` is global so timestamps and versions align.

## Space, shape, depth

- Scale 2 / 4 / 8 / 12 / 16 / 20 / 24 / 32 / 48. `.main` is 24px × `--pad`, stage gap 20px.
- Radii: `--r-sm` 3px (chips, keycaps), `--r-md` 5px (controls), `--r-lg` 8px (panels, dialogs' inner blocks), `--r-xl` 14px (dialogs, cards). Pills are round only for avatars and dots.
- **Depth is a seam, not a shadow.** 1px borders divide regions. Shadows exist only where something is lifted out of the page: `.popover`, `.modal`, `.flash-dialog` (`--shadow-pop`, `--shadow-dialog`). A bordered element with a wide soft shadow is wrong in this world.
- Every browser surface is themed: selection is vermilion on `--on-seal`, scrollbars are `--scrollbar-thumb` on the page field, focus is `2px --vermilion-hi` with 2px offset, `caret-color` is vermilion.

## Components (the vocabulary)

Primitives live in `src/design/primitives.tsx`; styles are their matching rules in `styles.css`. Pages must not invent a parallel name.

- **Button** — `.btn` with variants `-primary` / `-solid` / `-default` / `-quiet` / `-danger` / `-stamp`, sizes `.btn-sm` / `.btn-lg` / `.btn-block`. `-stamp` is reserved for a confirm that leaves a record (保存轮换、我已保存). Icon-only actions are `.icon-btn`; destructive row actions are `.row-act` after an `.actions-divider` seam.
- **Field / Input / Select / Textarea / Check** — native controls, styled; `.field` owns label + control + `.field-hint`. Forms stack in `.form-stack`, pair in `.form-row`, close in `.form-actions`.
- **Pill** — `.pill` with `ok` / `bad` / `warn` / `muted` / `accent`; state only.
- **PageHead / Section / EmptyState / Notice / Skeleton / MetaList / TableWrap** — the page skeleton. `.empty` teaches; loading uses `.skeleton` lines, never a centred spinner.
- **Modal** — native `<dialog class="modal">` (title bar with close, sticky head, inert background, Escape, focus restore). **Record page** — the secret's 卷宗, in the stage rather than a dialog: `.record` → `.record-sheet`, left `.record-left` (seal + archive), right `.chapters` / `.chapter` (版本 / 轮换 / 痕迹). It replaces the old `.detail-dialog` drawer.
- **App shell** — `.appbar` (seal, wordmark, `.appbar-ctx` context switcher + `.popover`, session `.status-chip`, `.avatar-btn`), `.rail` (grouped `.rail-group` with `.rail-link`, `.rail-foot` with the current project and version), `.mobile-nav` bottom bar ≤760px, `.stage` for content.

## The secret record (卷宗)

The secret detail is not a drawer. It is a page-sized 卷宗 in the stage: a left leaf that fixes **what the secret is** and a right leaf that holds **what happened to it**, separated by one seam. The list unmounts while it is open; 返回密钥, the rail's 密钥 item and the mobile 密钥 tab return to it.

- **Left leaf — 封印与档案.** The seal chamber shows `.cipher` at mid density until an explicit 显示明文; after reveal/hide the mask carries the shown length. The archive is three or four `.ledger-row`s (位置 / 标签 / 备注 / 轮换). Rotation is a one-line status + next link, never the policy form. 删除密钥 sits at the leaf foot behind a seam.
- **Right leaf — 版本 / 轮换 / 痕迹.** `.chapters` is the index; exactly one `.chapter` is visible and the index sticks below the appbar. 版本 is the timeline plus 最近读取; 轮换 owns the policy summary, runs, form and fixed state; 痕迹 is the per-secret audit, day-banded, filterable and load-more.
- **Compare is a subview of 版本.** `.compare-bar` selects two versions; the default is masked with fixed 12-dot plates, never a fabricated length. 显示明文 fetches both historical values, audits the reads, and renders a character diff (single line) or a numbered line diff (multiline). 失焦 clears it.
- **Truth over the comp.** The real API does not return value lengths or version lengths until a value is read. The live page therefore omits the per-version character counts and masked counts that the static comp explored; the mask is a nominal shape, and all length/diff numbers appear only after an explicit reveal.
- **Focus.** Opening focuses the record `h1`; closing restores the row key button (or search). Escape closes the record only when no native dialog is open and focus is not in a text field; native modals keep their own Escape/focus behaviour.
- **Narrow.** ≤1040px the two leaves become 封印 → chapters → 档案 in one column; `.chapters` stays sticky. ≤900px the header becomes 返回 / 标题＋编辑图标 / 元信息. The bottom 密钥 tab is the long-page exit.

## The login leaf (门面)

The login screen is a split spread: the left half is the gate (the form, `.auth-aside`), the right half is the taboo itself — one page of a 古籍 that fills its whole half (`.auth-lead` → `.auth-book`, 100% × 100vh).

- **版框** — the page edge is the 版框: a 1px border on the panel edges plus a 1px inner rule at a 4px inset, `--ink-900` ground, no radius, no shadow. The `.auth-book-spine` seam divides 题签 from 正文.
- **题签** — a 184px title slip (148/128/112px in the compact steps): vertical 「禁制」 (`--fs-34`, Songti), the rotated `TABOO` imprint, 「洞府有禁」, and the deployment line (`密钥管理 · 自部署 · 单二进制`) pushed to the foot of the spine.
- **界行与竖排** — `.auth-book-body` is one `writing-mode: vertical-lr` flow filling the rest of the page. Its `line-height` is the 界行 pitch (50px; 40/32/28px in the compact steps) and the same value drives the `repeating-linear-gradient` that prints the 1px rules across the **full page width**. The column runs **left→right** and characters **top→bottom** — the order this surface is asked to read in, the reverse of a historical 古籍.
- **卷末留白** — the text ends after the `MIT · 无 open-core 付费墙` colophon column; the remaining columns stay empty 界行 (the ruled page is the right half's content). A flat 60px `.seal-mark` sits at the page's bottom right (48/38/34px in the compact steps; the lift glow is dropped — it is ink on paper, not a button) and never touches the text.
- The form is untouched vocabulary: same `.auth-card`, a 60×60 brand 印 beside the wordmark, same `h2` `taboo · 禁制` (now `--fs-26` with its Chinese half in Songti, so the left half has a title at the same scale as the book; `--fs-20` ≤1024px), same controls and accessible names.

## The first-run leaf (落款)

A confirmed username is a sealed act, so it is not a notice bar inside the console: `.onboard-shell` is a full-page stop before the workspace, one `.onboard-card` on the ink ground (seam border, no shadow, 440px wide, stacking to full width ≤640px). The check runs on **every entry** — after a password/OIDC login, after the boot `/me`, after a 30s refresh — whenever the server says `handle_confirmed === false`; confirming once is what retires the screen.

- **Head** — the 48px 印, `taboo · 禁制`, and the kicker `入府 · 落款`; then `定下你的用户名` at `--fs-26` and one paragraph saying what the name is for: the 第一段 of the locator and what the audit records.
- **Field** — a `.handle-input` seam with a mono `/` prefix; the input is mono and 44px tall. `.onboard-preview` prints the locator the name produces (`用户名/default/目录 / 密钥`) in mono inside a quiet seam block, so editing the name visibly edits the address.
- **Actions** — one vermilion primary `确认用户名，进入控制台`, one quiet `退出登录`. Confirming is the only path in; the screen has no skip.
- **Rules** — format and reserved-word errors are stated inline in Chinese before submit; the server remains the only judge of uniqueness, and `CODE_TAKEN` is rewritten as `「name」已被占用，换一个再试。`. Confirmed names lock server-side (`HANDLE_LOCKED`); the screen does not restate the lock.
- **No footer** — the card ends after 退出登录; the build stamp stays on the login leaf and in the rail, not on this one-off sheet.

## The space registry (组织管理)

`/organizations` is a full-screen register, not a popover. It keeps the console's world: a sticky topbar with the 印, `taboo 禁制 / 组织`, the current operator and `返回控制台`; below it a 264px left index and the opened folio.

- **Index** — `个人与组织` head, then seam rows: a 32px mark (circle = 个人空间, square = 组织), name, `code · role`, and one vermilion `当前` pill on the active context. Selection is `--ink-850` with no second accent. `新建组织` sits at the index foot as a default-weight button, not a second primary.
- **Folio** — the selected space's management sheet: 56px mark, name at `--fs-26`, `code · 角色`, one vermilion `进入控制台`, a `MetaList` (类型 / 成员 / 项目), project seam rows, member seam rows for owners (non-owners see a teaching notice), and a footnote pointing at 组织设置 inside the console. `新建组织` swaps the sheet for an in-page form — 显示名称, 组织 code with the locator prefix (`code/default/…`) previewed, no modal.
- **External sessions** — a federated session is bound to one organization, so the sheet keeps `进入控制台` enabled only for the current space and states why; creation is disabled with the same reason.
- **Narrow** — ≤900px the register becomes two views: index first, then the selected sheet with a `空间列表` back button; ≤640px the wordmark and crumb collapse to the seal and `组织`.

## Signature behaviours

- **Ciphertext carries length.** `.cipher` renders ● segments sized to the value's length (`cipher-xs|sm|mid|md|lg|xl`); unseen values render at mid density, never a fabricated length. A revealed value is `.cipher-value` — a vermilion-tinted chip that enters with one `unseal` clip+blur ease. Re-sealing is instant.
- **Row actions are quiet until needed.** `.action-fade` sits at 0.5 opacity and rises on hover, on `focus-within`, and is always fully visible on touch layouts.
- **Records are timelines.** Version history is `.version-list` with a current node in vermilion; the per-secret audit is `.trace-*` with day bands and action glyphs coloured by kind (reveal amber, write jade, delete/rollback vermilion) and mono resources; the organisation-wide `.audit-list` remains the second timeline.
- **Once-only secrets** are `.once-secret` — a sealed credential block that must be dismissed by the operator, never a toast.
- **Motion** is state change only, 140–260ms, exponential ease-out: `modal-in`, `record-in`, `pop-in`, `unseal`, `flash-in`, `skeleton`. One authored moment per transition, never an entrance choreography. `prefers-reduced-motion` collapses all of it to 1ms.

## Responsive rules

Structural, not fluid type. ≤1100px the rail narrows and the audit column stacks; the login book page steps down at ≤1200px width (or ≤735px height): 40px 界行, 148px 题签, `--fs-26` opening line; at ≤680px height it takes 32px 界行 / 128px 题签, and at ≤560px height 28px 界行 / 112px 题签 with `--fs-13` notes; ≤900px the login loses its lead column; ≤1040px the secret record stacks its two leaves and pins `.chapters`; ≤900px the record header becomes a 返回 / title＋编辑 / meta grid; ≤760px the rail becomes `.mobile-nav`, the appbar hides the wordmark and the session chip, settings nav becomes a scroll row, tables scroll inside `TableWrap`; ≤640px the secrets table becomes `data-mobile="cards"` rows (key + version / value + tags / date + actions) and card tables drop to a single column. `document.documentElement.scrollWidth ≤ innerWidth` holds at every tab from 390px up.

## Accessibility commitments

Keyboard-operable everywhere; native `<dialog>` focus containment and Escape; `role="alert"` / `role="status"` for errors and state; visible focus ring on every control; form validation names the first invalid field and focuses it; icon-only buttons carry an `aria-label` equal to the action word (显示 / 隐藏 / 复制 / 编辑 / 删除); native `<select>` popups are accepted; `.sr-only` for table action headers. This is a scoped check, not a WCAG certification claim.

## Scope and migration ledger

Replaced: the GitHub-dark palette (`#0d1117` / `#58a6ff` / `#7c3aed`), the topbar + flat sidebar shell, `.card`-per-panel composition, emoji and text-glyph icons, native `window.alert`-styled confirmations, and the Modal-based version history (now the secret record page). The narrow drawer that squeezed value, versions, diff and rotation into one column was replaced by the 卷宗 record page on 2026-11-13. The login's stacked lead list (brand block + three-icon covenant bullets + MIT footer) was replaced by the single 古籍 leaf above.

Preserved because the product depends on it: session/CSRF/BFF behaviour in `api.ts`, organization-bound workspace lifetimes, `taboo:permissions-refresh` and `taboo:verification-unavailable` listeners, blur-clears-revealed-value and the 20-second clipboard wipe, server-side authorization as the only truth, and every accessible name the browser suite asserts (`显示`, `隐藏`, `复制`, `编辑`, `删除`, `＋ 新建密钥`, `保存（产生新版本）`, `回滚到此`, `对比`, `版本历史`, `添加身份连接`, `当前组织`, `退出`, heading `taboo · 禁制`).

Still native by contract (`UX-CONTRACT.md` owns the behaviour): `<select>` lists, the six-form grant matrix, OIDC connection form and identity replacement, cross-tab session invalidation.

Open items: none outstanding from the finish review. Two recorded decisions replaced the earlier gaps: the rail's current-project block is a seam-only row (`.rail-project`, no box) in the same rhythm as the nav; compact tables at 390px scroll inside their named region at a 420px minimum (not 560px) because hiding columns or clipping identifiers would lose data.

