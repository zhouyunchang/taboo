# taboo UI design context

Existing-product seed, 2026-09-27. The standard identity federation work preserves the existing Chinese admin UI and dark palette.

## Canonical runtime source

`apps/web/src/styles.css` owns CSS custom properties and shared element styles. This document describes their use; it does not generate a second theme. Background `--bg: #0d1117`, surfaces `--bg2: #161b22`, foreground `--fg: #e6edf3`, muted `--muted: #8b949e`, border `--border: #2d333b`, accent `--accent: #58a6ff`, secondary accent `--accent2: #7c3aed`, danger `--danger: #f85149`. Scrollbar colors alias existing tokens. Typography uses the existing system Chinese font stack and 14px body baseline.

## Intent and components

Dense, readable operational screens with clear organization context and explicit access boundaries. Preserve existing spacing, native form fields and role names. No vendor identity appears as a prerequisite. OIDC connection forms use the shared native-dialog `Modal` with an inert background, Escape, focus containment and restoration. Buttons own actions; native links own navigation. Native select popup behavior is accepted.

## Scope and migration ledger

The federation console covers organization selection, identity replacement, member action grants, login/session policies, OAuth trusts and workload bindings, plus the existing audit/sync/webhook/dynamic panels. All overlay drawers now use native Modal focus containment. Browser-native confirmation prompts remain for small destructive actions; these are keyboard operable. This is a scoped accessibility check, not a claim of full WCAG certification. Browser and release evidence is recorded in docs/design/standard-federation-acceptance.zh-CN.md.

Identity replacement is a separate native dialog available to each member. It names the target connection, requires an explicit acknowledgement, and asks local users for fresh password/TOTP proof. The server keeps IDs and grants but unbinds the previous identity and ends prior sessions. The UI does not imply account merging or automatic inheritance of secret access. Escape and cancel restore focus; pending navigation prevents duplicate submission. Mobile overflow and the actual Keycloak replacement path are covered by the browser suite.

Governance tables scroll within their named region on narrow screens. The header wraps brand and organization onto separate rows to prevent long tenant names colliding with actions. Permission grants enumerate six independent operations; blank workload grants deliberately deny all access. Cross-tab authentication changes unmount the workspace and require explicit reload. Local organization selection remounts panels; external selection starts a fresh organization-bound OIDC flow.
