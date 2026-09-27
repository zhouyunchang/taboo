# taboo UI design context

Existing-product seed, 2026-09-27. The standard identity federation work preserves the existing Chinese admin UI and dark palette.

## Canonical runtime source

`apps/web/src/styles.css` owns CSS custom properties and shared element styles. This document describes their use; it does not generate a second theme. Background `--bg: #0d1117`, surfaces `--bg2: #161b22`, foreground `--fg: #e6edf3`, muted `--muted: #8b949e`, border `--border: #2d333b`, accent `--accent: #58a6ff`, secondary accent `--accent2: #7c3aed`, danger `--danger: #f85149`. Scrollbar colors alias existing tokens. Typography uses the existing system Chinese font stack and 14px body baseline.

## Intent and components

Dense, readable operational screens with clear organization context and explicit access boundaries. Preserve existing spacing, native form fields and role names. No vendor identity appears as a prerequisite. OIDC connection forms use the shared native-dialog `Modal` with an inert background, Escape, focus containment and restoration. Buttons own actions; native links own navigation. Native select popup behavior is accepted.

## Scope and migration ledger

This change covers login/session recovery, external-session workspace chrome and the identity connection form. Other legacy secret/editor/member workflows still contain custom drawers and browser confirmations; their full migration remains part of KTI-07. No claim is made that this whole application has completed an accessibility audit. Browser checks and static audit evidence are recorded in the federation implementation document.
