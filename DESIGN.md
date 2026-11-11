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
| 26–34 | `--fs-26` `--fs-34` | login wordmark and the single principle line |

- `--font-ui`: Inter when installed, then the system UI stack, then PingFang / Source Han / YaHei for Chinese. `font-synthesis: none` — no faux bold or oblique across the CJK fallback.
- `--font-mono`: system monospace. Used for every identifier, key name, project slug, issuer, timestamp, and all ciphertext. It is data, not costume.
- `--font-cjk-display`: Songti stack, reserved for the 「禁」 seal glyph and the wordmark's Chinese half.
- The seal glyph is set by hand: `.seal-mark` is a vermilion square with a 1px inner rule and a 600-weight 「禁」. It is the only brand mark.
- Tracking stops at `-0.02em` on headings; `font-variant-numeric: tabular-nums` is global so timestamps and versions align.

## Space, shape, depth

- Scale 2 / 4 / 8 / 12 / 16 / 20 / 24 / 32 / 48. `.main` is 24px × `--pad`, stage gap 20px.
- Radii: `--r-sm` 3px (chips, keycaps), `--r-md` 5px (controls), `--r-lg` 8px (panels, dialogs' inner blocks), `--r-xl` 14px (dialogs, cards). Pills are round only for avatars and dots.
- **Depth is a seam, not a shadow.** 1px borders divide regions. Shadows exist only where something is lifted out of the page: `.popover`, `.modal`, `.detail-dialog`, `.flash-dialog` (`--shadow-pop`, `--shadow-dialog`). A bordered element with a wide soft shadow is wrong in this world.
- Every browser surface is themed: selection is vermilion on `--on-seal`, scrollbars are `--scrollbar-thumb` on the page field, focus is `2px --vermilion-hi` with 2px offset, `caret-color` is vermilion.

## Components (the vocabulary)

Primitives live in `src/design/primitives.tsx`; styles are their matching rules in `styles.css`. Pages must not invent a parallel name.

- **Button** — `.btn` with variants `-primary` / `-solid` / `-default` / `-quiet` / `-danger` / `-stamp`, sizes `.btn-sm` / `.btn-lg` / `.btn-block`. `-stamp` is reserved for a confirm that leaves a record (保存轮换、我已保存). Icon-only actions are `.icon-btn`; destructive row actions are `.row-act` after an `.actions-divider` seam.
- **Field / Input / Select / Textarea / Check** — native controls, styled; `.field` owns label + control + `.field-hint`. Forms stack in `.form-stack`, pair in `.form-row`, close in `.form-actions`.
- **Pill** — `.pill` with `ok` / `bad` / `warn` / `muted` / `accent`; state only.
- **PageHead / Section / EmptyState / Notice / Skeleton / MetaList / TableWrap** — the page skeleton. `.empty` teaches; loading uses `.skeleton` lines, never a centred spinner.
- **Modal** — native `<dialog class="modal">` (title bar with close, sticky head, inert background, Escape, focus restore). **Drawer** — native `<dialog class="detail-dialog">` for a record that stays open beside the list (a secret's versions, diff, rotation).
- **App shell** — `.appbar` (seal, wordmark, `.appbar-ctx` context switcher + `.popover`, session `.status-chip`, `.avatar-btn`), `.rail` (grouped `.rail-group` with `.rail-link`, `.rail-foot` with the current project and version), `.mobile-nav` bottom bar ≤760px, `.stage` for content.

## Signature behaviours

- **Ciphertext carries length.** `.cipher` renders ● segments sized to the value's length (`cipher-xs|sm|mid|md|lg|xl`); unseen values render at mid density, never a fabricated length. A revealed value is `.cipher-value` — a vermilion-tinted chip that enters with one `unseal` clip+blur ease. Re-sealing is instant.
- **Row actions are quiet until needed.** `.action-fade` sits at 0.5 opacity and rises on hover, on `focus-within`, and is always fully visible on touch layouts.
- **Records are timelines.** Version history is `.timeline` with a current node in vermilion; audit is `.audit-list` with day bands, action glyphs coloured by kind (reveal/export amber, write jade, delete/revoke vermilion) and mono resources.
- **Once-only secrets** are `.once-secret` — a sealed credential block that must be dismissed by the operator, never a toast.
- **Motion** is state change only, 140–260ms, exponential ease-out: `modal-in`, `drawer-in`, `pop-in`, `unseal`, `flash-in`, `skeleton`. One authored moment per transition, never an entrance choreography. `prefers-reduced-motion` collapses all of it to 1ms.

## Responsive rules

Structural, not fluid type. ≤1100px the rail narrows and the audit column stacks; ≤900px the login loses its lead column; ≤760px the rail becomes `.mobile-nav`, the appbar hides the wordmark, settings nav becomes a scroll row, tables scroll inside `TableWrap`; ≤640px the secrets table becomes `data-mobile="cards"` rows (key + version / value + tags / date + actions) and card tables drop to a single column. `document.documentElement.scrollWidth ≤ innerWidth` holds at every tab from 390px up.

## Accessibility commitments

Keyboard-operable everywhere; native `<dialog>` focus containment and Escape; `role="alert"` / `role="status"` for errors and state; visible focus ring on every control; form validation names the first invalid field and focuses it; icon-only buttons carry an `aria-label` equal to the action word (显示 / 隐藏 / 复制 / 编辑 / 删除); native `<select>` popups are accepted; `.sr-only` for table action headers. This is a scoped check, not a WCAG certification claim.

## Scope and migration ledger

Replaced: the GitHub-dark palette (`#0d1117` / `#58a6ff` / `#7c3aed`), the topbar + flat sidebar shell, `.card`-per-panel composition, emoji and text-glyph icons, native `window.alert`-styled confirmations, and the Modal-based version history (now the drawer).

Preserved because the product depends on it: session/CSRF/BFF behaviour in `api.ts`, organization-bound workspace lifetimes, `taboo:permissions-refresh` and `taboo:verification-unavailable` listeners, blur-clears-revealed-value and the 20-second clipboard wipe, server-side authorization as the only truth, and every accessible name the browser suite asserts (`显示`, `隐藏`, `复制`, `编辑`, `删除`, `＋ 新建密钥`, `保存（产生新版本）`, `回滚到此`, `对比`, `版本历史`, `添加身份连接`, `当前组织`, `退出`, heading `taboo · 禁制`).

Still native by contract (`UX-CONTRACT.md` owns the behaviour): `<select>` lists, the six-form grant matrix, OIDC connection form and identity replacement, cross-tab session invalidation.

Open items: none outstanding from the finish review. Two recorded decisions replaced the earlier gaps: the rail's current-project block is a seam-only row (`.rail-project`, no box) in the same rhythm as the nav; compact tables at 390px scroll inside their named region at a 420px minimum (not 560px) because hiding columns or clipping identifiers would lose data.
