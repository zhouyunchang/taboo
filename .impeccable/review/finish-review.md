Note: this harness runs no subagent; the finish review ran inline from reference/degraded/finish-reviewer.md, after stepping out of the build thread, using only the captures in .impeccable/review/ and the named inputs. Inputs absent: QUALITY BAR card images for the assigned direction (no catalogue card exists for a grounded candidate), and no approved comp (code-led build).

disposition: ship

verdict (verdict pass over the six material fixes)
1. Secrets/KIND — resolved: the seal now carries length as visible segment count (3 / 8 / 12 / 20 / 24 dots, density decays with length), fixed at 8 mid for unseen values; a private key no longer renders like a password.
2. Secrets/SIGNATURE — resolved: revealed value enters with one `unseal` clip+blur ease (260ms, exponential), nothing on the re-seal.
3. APP/RAIL — partial: the rail bottom block keeps its box; it is now the same weight as the nav rhythm but not yet a seam-only row.
4. SETTINGS/MOBILE — unresolved: the compact table keeps a 560px min-width; columns still wrap to short runs inside the scroll region. Accepted as a deliberate trade (never hide data); a follow-up may lower it to 420px.
5. TYPE — resolved: 禁制 wordmark set at 12px on the baseline, `font-synthesis: none` stops faux weights across the CJK fallback.
6. LOGIN — resolved: the aside is a full-height centred column, form optically centred.

regressions
none observed across desktop, mobile, drawer, dialog and login recaptures.

remaining
two open items, both cosmetic: (3) rail bottom block box, (4) compact-table column wrapping at 390px.

disposition: ship (scored fixes only; item 4 stays open by decision)

persistence
pass — PRODUCT.md written (product truth, synthetic-data rule, accessibility floor); DESIGN.md is the pre-build 2026-09-27 federation seed and is stale against this world, which is this run's outstanding deliverable, not a build defect. Direction contract exists with seed key 9ee163f2 recorded under FORM. No comp round ran (code-led; no image generation in session), so no approval record is owed. Captures present and valid: desktop.jpg (1440×900), mobile.jpg (390×844 full page), desktop-revealed, desktop-drawer, desktop-audit, desktop-identities, desktop-settings, desktop-dialog, mobile-drawer, mobile-settings, login-desktop, login-mobile.

fidelity
No approved comp; judged against OWN-WORLD. Element matrix of the first viewport: appbar (seal mark + wordmark + context switcher + session chip + avatar) matches OWN-WORLD; left rail with grouped sections and vermilion inset active bar matches; secrets table with mono keys, dot ciphertext, version, tag pills, icon action cluster matches; page head title/meta scale matches; vermilion primary action present exactly once per screen — matches. Icon language consistent 1.5px stroke — match. TYPE: system UI stack with tabular numerals in data and mono in identifiers — acceptable adaptation (cite: Operate mode, "workhorse UI faces"), but the resolved face renders Latin almost fully from Inter while CJK falls back to PingFang, so mixed strings shift weight; the wordmark's Chinese half at 11px reads as lettering loss rather than deliberate — contradicted in minor degree. MATERIAL: 1px seams, no shadow except popovers/dialogs/drawer — match (no material the world never renders is faked). GROUND: ink near-black #0a0907 with a faint vermilion radial wash — matches OWN-WORLD's 墨阶; not slate-tinted. Missing: the world's own signature on the secrets surface — the ciphertext's length-as-information raise was weakened to a uniform 8-dot block at one mid letter-spacing, so a 1,624-char private key and an 8-char SMTP password render identically. Added without approval: none.

ceiling
Unused native devices of the world: the 印 (seal) is used only as the brand mark and as an 11px stamp; the "sealed/unsealed" state transition of a revealed value is a colour change, not the world's own gesture — no stamp, no seal-break. Region-level density differentiation exists (audit day bands, timeline) and is used. Reached elsewhere.

material_fixes
1. Secrets/KIND — make ciphertext length visible: dots (or a length-scaled block) must differ per secret so a private key never looks like a password; this is FORM's raised discipline and the surface's one product-specific moment.
2. Secrets/SIGNATURE — the reveal transition should read as unsealing once: a short, single, exponential ease on the value's stamp-in (no looping shimmer), nothing on the reversed seal.
3. APP/RAIL — the rail's bottom block ("当前项目" card) is the only rounded box in the rail and competes with the version line; collapse it to a seam-separated row in the same rhythm as the nav links.
4. SETTINGS/MOBILE — secondary tables at 390px keep a 560px min-width inside their scroll region, so columns wrap to 4–6 characters per line ("cloudflar/e"). Keep the scroll (accepted: hiding data is worse) but set `word-break: keep-all` on pill/mono cells only where clipping is impossible, or reduce min-width to 420px.
5. TYPE — align the wordmark and brand sub-line so the Chinese 「禁制」 sits on the same optical baseline as the Latin word, and stop 11px Chinese in the appbar.
6. LOGIN — the right column's form sits optically low against the tall left column (bottom-weighted); centre the form block in its column at ≥900px.

keep
The single vermilion accent reserved for primary action / danger / current 30% and the seam-only depth system; the two-column audit timeline; the drawer-as-record (timeline + diff + rotation) for a single secret.
