# taboo frontend behavior contract

## Canonical owners

| Capability | Owner |
| --- | --- |
| Session initialization, same-origin requests, CSRF, bounded refresh retries | `apps/web/src/api.ts` |
| Login/logout/loading/retry and workspace lifetime | `apps/web/src/App.tsx` |
| Identity connection form and confirmation | `apps/web/src/pages/Settings.tsx`, shared `components/Modal.tsx` |
| Native inputs/select/labels/focus/scrollbars | `apps/web/src/styles.css` and native HTML semantics |
| Organization and project access | Server authorization; UI visibility is supplementary |

## Session and sensitive state

External sessions have no Token in localStorage. The BFF session is checked before rendering a workspace. On 401, remove the workspace and any mounted secret values; do not retry external authentication as a local identity. Local user refresh has one shared request and at most one retry. Logout remains pending until server confirmation; failures provide an inline retry opportunity. The local-only Node reference backend may return 404 for session introspection; this compatibility case is not an OIDC fallback.

A browser session is bound to one organization. Mutations carry a session-derived CSRF value and exact Origin. Another tab changing the cookie invalidates an old tab's write proof. Project/environment switches remount secret panels. Sensitive drafts and secrets are never persisted for recovery. Authentication changes broadcast a non-secret epoch; other tabs immediately unmount and require reload. Visible tabs check session continuity and current membership every 30 seconds and on focus. Permission revocation clears inaccessible values/forms on refresh; every server request still authorizes independently. Stale request generations cannot populate a new session or organization.

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

Identity replacement must explain that old sign-in credentials stop working. Disable submission until a registered target and acknowledgement are selected. Clear local password/TOTP fields after either success or failure; never persist them or copy them into navigation parameters. Existing-member data survives only after both identity proofs and the atomic audit commit succeed.
