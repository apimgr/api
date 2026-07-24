# Web Frontend Rules (PART 16)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

**IDEA.md override:** IDEA.md non-goals declare no user accounts, no admin web
panel (`server.yml`-only configuration), no auth/sessions. PART 16 base spec
already only defines a single public layout with no admin UI, so no override
of PART 16 layout rules is needed here — but any base-spec text elsewhere
implying an admin/session-aware frontend does NOT apply to this project.
`src/admin/`, `src/session/`, and related templates/config were removed
accordingly; do not reintroduce them from generic PART 16 guidance.

## CRITICAL - NEVER DO
- Never use a client-side JS framework (React/Vue/Alpine/jQuery) — Go
  `html/template` (`.tmpl`) renders all HTML server-side
- Never require JavaScript for a core feature — JS enhances, it does not enable
- Never ship a feature that is browser-only; every user-facing API feature
  must also be reachable as a frontend page (forms, not just JSON)
- Never put inline `<style>`/`style="..."` or inline `<script>`/`onclick="..."`
  in templates — CSS lives in `static/css/*.css`, JS in `static/js/app.js` only
  (CSP blocks inline handlers)
- Never hardcode colors — theme via CSS custom properties (`--color-*`) only,
  overridden per theme class on `<html>` (never on `<body>`)
- Never use desktop-first CSS (`max-width` media queries) — mobile-first only
  (base = mobile, `@media (min-width: …)` layers up)
- Never let a long unbreakable string (IPv6, .onion, token, hash, UUID)
  overflow its container on mobile — apply `word-break`/`overflow-wrap` or a
  scrollable `.code-block`
- Never create a page-specific partial (used once = not a partial) or a
  `template/components/` directory — reusable component partials live under
  `partial/` (e.g. `partial/toast.tmpl`, `partial/modal.tmpl`)
- Never add admin-panel or session/login UI, links, or hints to any public
  page or nav — IDEA.md non-goals forbid it (no accounts, no admin web UI)
- Never use `alert()`/`confirm()` — use the toast/modal/native `<dialog>` patterns
- Never render an unstyled/generic error page — 400/401/403/404/500/502/503
  all use `error.tmpl` inside the themed `public.tmpl` layout

## CRITICAL - ALWAYS DO
- Render all HTML through Go's `html/template`; mandatory layout is
  `layout/public.tmpl` (there is only ever a public layout — no admin layout)
- Include the mandatory partial set: `partial/head.tmpl`, `partial/scripts.tmpl`,
  `partial/public/header.tmpl`, `partial/public/nav.tmpl`, `partial/public/footer.tmpl`
- Support dark/light/auto theme via a `theme` cookie read server-side into
  `class="theme-{{.Theme}}"` on `<html>`; `auto` is pure CSS
  (`prefers-color-scheme`) — no `matchMedia` init JS, no FOUC
- Keep frontend routes working with zero JavaScript: links navigate, forms
  submit and validate server-side, content is visible without JS
- Auto-detect client type (browser vs curl/CLI vs `Accept` header) and
  respond HTML / text / JSON accordingly on every frontend route
- Apply mobile-first responsive CSS with the standard breakpoints
  (base <768px, `min-width:768px` tablet, `min-width:1024px` desktop)
- Apply `word-break: break-all; overflow-wrap: break-word;` (or a scrollable
  `.code-block`) to any element that may hold IPs, tokens, hashes, UUIDs, or
  .onion addresses
- Meet WCAG 2.1 AA: keyboard operability, visible focus rings, ARIA labels,
  4.5:1 contrast, alt text, `<label>`s, `aria-live` errors, skip link,
  correct heading hierarchy, `prefers-reduced-motion` support (see PART 30
  for full i18n/a11y detail — PART 16 states the baseline, does not duplicate it)
- Keep all JavaScript in a single `static/js/app.js`, no bundlers/transpilers/
  npm — plain browser-native JS only
- Meet PWA requirements where implemented: installable manifest, service
  worker, offline behavior, maskable icons

## KEY DECISIONS (pre-answered)
| Question | Answer | Spec Reference |
|----------|--------|----------------|
| Which layouts exist? | Only `layout/public.tmpl` — no admin layout | PART 16, Layout Separation |
| Admin/session UI in the frontend? | Never — IDEA.md non-goals override; config is `server.yml`/CLI only | IDEA.md non-goals; PART 16 Public Nav Rules |
| CSS approach | Mobile-first, CSS custom properties for theme, no hardcoded colors | PART 16, Mobile-First Responsive Design / Themes |
| JS framework allowed? | No — vanilla JS only, one file (`static/js/app.js`) | PART 16, Technology Stack / JavaScript Rules |
| Must features work without JS? | Yes — JS enhances, never enables | PART 16, No JavaScript-Disabled Broken State |
| Where do optional component partials live? | `partial/` (e.g. `partial/modal.tmpl`) — never `template/components/` | PART 16, Partials Rules |
| Theme class placement | `class="theme-{{.Theme}}"` on `<html>`, never `<body>` | PART 16, Shared Theme Classes |
| Long strings on mobile | `word-break`/`overflow-wrap` or `.code-block` scroll — never allowed to overflow | PART 16, Long Strings |

## TERMINOLOGY
| Term | Meaning |
|------|---------|
| Public layout | `layout/public.tmpl` — the only layout; no admin layout exists |
| Mandatory partials | `partial/head.tmpl`, `partial/scripts.tmpl`, `partial/public/{header,nav,footer}.tmpl` |
| Component partials | Optional reusable UI fragments under `partial/` (toast, modal, pagination, …) |
| Progressive enhancement | Core features work with no JS; JS only improves the experience |
| Smart content detection | Frontend route auto-responds HTML/text/JSON based on `Accept`/User-Agent |

## QUICK REFERENCE
- Templates: `src/server/template/layout/public.tmpl`,
  `src/server/template/partial/{head,scripts}.tmpl`,
  `src/server/template/partial/public/{header,nav,footer}.tmpl`,
  `src/server/template/page/*.tmpl` — matches `initTemplates()` in
  `src/server/server.go`
- Static assets: `static/css/{common,public,components}.css`,
  `static/js/app.js` (one file), `static/images/`, `static/fonts/`
- No admin/session frontend work — IDEA.md forbids it; do not add nav links,
  routes, or templates implying a web login/dashboard
- Mobile-first CSS, CSS custom properties for theme, word-break on long strings
- Every frontend route: works with no JS, degrades to text for CLI/curl,
  returns JSON when `Accept: application/json`

---
For complete details, see AI.md PART 16
