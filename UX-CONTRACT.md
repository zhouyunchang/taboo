# taboo frontend behavior contract

## Canonical owners

| Capability | Owner |
| --- | --- |
| Session initialization, same-origin requests, CSRF, bounded refresh retries | `apps/web/src/api.ts` |
| Login/logout/loading/retry and workspace lifetime | `apps/web/src/App.tsx` |
| Identity connection form and confirmation | `apps/web/src/pages/Settings.tsx`, shared `components/Modal.tsx` |
| Native inputs/select/labels/focus/scrollbars | `apps/web/src/styles.css` and native HTML semantics |
| Organization and project access | Server authorization; UI visibility is supplementary |
| Secret record, compare, rotation chapter, per-secret audit | `apps/web/src/pages/SecretRecord.tsx`, `components/SecretCompare.tsx`, `components/SecretRotation.tsx`, `components/SecretTraces.tsx` |

## Session and sensitive state

External sessions have no Token in localStorage. The BFF session is checked before rendering a workspace. On 401, remove the workspace and any mounted secret values; do not retry external authentication as a local identity. Local user refresh has one shared request and at most one retry. Logout remains pending until server confirmation; failures provide an inline retry opportunity. The local-only Node reference backend may return 404 for session introspection; this compatibility case is not an OIDC fallback.

A browser session is bound to one organization. Mutations carry a session-derived CSRF value and exact Origin. Another tab changing the cookie invalidates an old tab's write proof. Organization switching lives on the full-screen `/organizations` register; entering a space re-scopes the workspace and remounts its panels, and external sessions must re-authenticate instead of switching in place. Project switches remount secret panels. Sensitive drafts and secrets are never persisted for recovery. Authentication changes broadcast a non-secret epoch; other tabs immediately unmount and require reload. Visible tabs check session continuity and current membership every 30 seconds and on focus. Permission revocation clears inaccessible values/forms on refresh; every server request still authorizes independently. Stale request generations cannot populate a new session or organization.

## Secret record

The secret record replaces the old right-hand drawer with an in-stage view. The list stays unmounted while a record is open; the record is not a URL state, and the explicit exits are 返回密钥, the rail's 密钥 item, the mobile 密钥 tab, and Escape when no dialog is open and focus is not in a text field. Opening focuses the record heading; closing restores the triggering list row (or the search input if the row is gone). Native `<dialog>` modals keep their own Escape, focus containment and focus restoration.

Sensitive state: opening a record never fetches a plaintext. 显示明文 and 复制 are the only reads; they fetch the value, audit the read server-side, and the value is removed on window blur, on `taboo:verification-unavailable`, and when `canReveal` is withdrawn by a permissions refresh. Copy performs the 20-second clipboard wipe. The masked seal is a nominal 12-dot shape; the length is shown only after a value has been seen, and no per-version value length is fabricated from metadata.

Editing is a write of a new version: comment and tags are prefilled from the current metadata, but the value field stays empty; the current plaintext is never put into the form. Deletion uses a native Modal with a typed Key confirmation; rollback uses a native Modal and creates a new version rather than deleting history. Approval-shaped responses are surfaced as pending receipts, never as success.

Rotation is a chapter, not a drawer section. The policy form, runs, fixed reason, pause/resume/run-now and generated-once value live there. A fixed policy has no mode/schedule in the API; 改为轮换 starts from an editable default (webhook, 30 days) and must be saved again. Generated values are cleared on blur and after 30 seconds. Rotation actions that change the secret report back to the record so the versions and audit refetch; the server remains the only source for the next run and for run completion.

The per-secret traces request the existing audit endpoint with `resource=project/<slug>/secret/<key>` and are read-only. Only owner/admin (audit read) can see them; a 403 renders the permission teaching state, not an empty timeline. Filters are client-side over the latest 200 records; the disclosure count and load-more reflect that cap. The foot links the reader to 审计日志 conceptually, without copying the org-wide timeline into this page.

Compare masks both plates until 显示明文; that action fetches the two historical values (each audited) and computes a character diff for single-line values and a numbered line diff for multiline values. Selecting the same version on both sides is prevented; the selectors and swap are disabled while plaintext is visible, so 隐藏明文 is the explicit way back to masked. Compare never writes a version.

## Connection editing

Chinese copy names standard OpenID Connect, issuer and client settings. Role/group mapping is optional and exact. Automatic admission defaults off and describes its scope. The local role remains authoritative after explicit administrator changes. Display the bounded upstream revocation window without suggesting MFA or immediate IdP revocation.

Connection creation/deletion is pessimistic: prevent duplicate submission, preserve errors in the form, close only on success, and restore focus. Native dialog owns background inertness and Escape; while a mutation is pending dismissal is blocked. No secrets in URL state or diagnostic screenshots. Other configuration and secret dialogs use the same native Modal.

## Layout and validation

Identity connection forms fit a 390px viewport without horizontal content loss. Tables use a named scroll container. Login toggles and project navigation are keyboard buttons. Status/errors have native live semantics and focus remains visible. Validate the actual Keycloak/browser path, expiry/disable behavior, network failure, narrow layout and dialog Escape/focus restoration. Static audit is supplementary and remaining baseline findings must be disclosed.

## Canonical UI Map

| Capability | Canonical owner | Source of truth | Allowed variants | Verification |
| --- | --- | --- | --- | --- |
| Table Selection | Native tables in workspace and settings | Existing response permissions | No bulk selection in identity flow | Browser role and overflow checks |
| Select/Listbox | Native HTML select | Browser/OS keyboard and popup behavior | Existing role selectors | Explicit native ownership; keyboard check |
| Date | No identity-flow date input | Server session expiry | Display only | Session expiry tests |
| Form | ValidatedForm, native constraints and inline errors with first-error focus | components/ValidatedForm.tsx, OidcCreator and Login | Shared validation; native configuration and secret dialogs | Browser submission and inline failure checks; strict static audit plus runtime checks |
| Scrollbar | Global styles.css | Runtime CSS tokens | Stable dialog gutter, scrollable table | Narrow viewport tests |
| Toast | Inline role=alert and role=status | App and identity form | No toast provider introduced | Browser recovery tests |
| CRUD | Identity form and Modal | Server-confirmed create/delete | Native configuration and secret dialogs | Escape/focus and mutation checks |
| Secret record | `pages/SecretRecord.tsx`, `components/SecretCompare.tsx`, `SecretRotation.tsx`, `SecretTraces.tsx` | Server permissions + `api.ts`; audit resource filter | No drawer fallback; no fabricated value lengths | Reveal/copy/blur, compare reveal, rollback/delete/approval, fixed policy, 403 audit, 390px overflow, Escape/focus |

Identity replacement must explain that old sign-in credentials stop working. Disable submission until a registered target and acknowledgement are selected. Clear local password/TOTP fields after either success or failure; never persist them or copy them into navigation parameters. Existing-member data survives only after both identity proofs and the atomic audit commit succeed.
