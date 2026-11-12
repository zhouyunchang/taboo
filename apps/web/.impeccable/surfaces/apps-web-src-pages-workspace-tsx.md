---
version: 1
slug: "apps-web-src-pages-workspace-tsx"
primary_target: "apps/web/src/pages/Workspace.tsx"
related_targets: ["apps/web/src/pages/Login.tsx","apps/web/src/pages/Settings.tsx"]
---

# Surface brief — 控制台 (apps/web)

## Scope and mode
The authenticated console and its front door (login, CLI consent): every screen under `apps/web/src`, Operate mode. Success = the operator finds a secret, reads its state, and acts within seconds, and always knows which 禁制 (organization / project) they are inside and what they are allowed to do.

## Audience, job, action
Self-hosting developers and small-team admins, at a desk mostly, on a phone when something urgent needs reading or revoking. Primary task: locate a secret → reveal or copy (20s clipboard wipe) → leave. Secondary tasks: mint or revoke machine identities, read the audit trail, change policy in settings.

## Chosen direction (seed 9ee163f2)
禁制 / 印鉴: ink ground, one vermilion seal, 1px barrier seams, monospaced data and ciphertext whose length is legible. Signature moment: the sealed ●●● block that carries the value's length before reveal and unseals once. Records (a secret's versions, the audit trail) are timelines rather than dialogs stacked on dialogs.

## Constraints
`UX-CONTRACT.md` owns behaviour: native `<dialog>`, native `<select>`, session and CSRF semantics, server-side authorization as truth, no plaintext persisted, no secrets in URL. Accessible names asserted by `apps/web/tests/*.spec.ts` are frozen. Demo data used for design review is synthetic (scripts/fixture-server.mjs) and never ships.

## Unresolved
Whether the rail bottom block becomes a seam-only row; whether compact tables lower their 390px min-width to 420px.
