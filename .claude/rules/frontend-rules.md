# Frontend/WebUI Rules (PART 16)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO
- Never use JS frameworks (React, Vue, Alpine, jQuery), bundlers (webpack/vite/rollup), transpilers (TypeScript/Babel), or npm/node for the frontend
- Never split JS across multiple files — all JS lives in one `static/js/app.js`
- Never use inline `style=""` or inline `<script>`; never use inline event handlers (`onclick`, `onchange` — CSP blocks them)
- Never use `alert()`, `confirm()`, or `prompt()` — use custom modals/native `<dialog>`
- Never leave a list/table/view blank — always render an empty state with icon, title, message, and action
- Never write desktop-first CSS (base styles = mobile; `@media (min-width)` for larger screens)
- Never let long strings (IPs, tokens, hashes, .onion addresses) overflow — always `word-break`/`overflow-wrap` or horizontal scroll
- Never put the theme class on `<body>` — only on `<html>`; never create layout-scoped/duplicate theme CSS
- Never link to admin/server-administration paths from public nav (no admin web UI exists)
- Never use `!important` in CSS except print styles
- Never show generic unstyled/browser-default error pages — all error pages use the site theme
- Never fix/pin header, nav, or footer to the viewport — everything scrolls with the page
- Never use a toast for something requiring a decision/input; never use a modal for simple non-blocking confirmations; never stack multiple modals
- Never allow trailing slashes as canonical URLs (301 redirect to no-trailing-slash form, except root `/`)
- Never pass user-submitted/untrusted content through `template.HTML` unless sanitized via an approved allow-list sanitizer
- Never render `server.contact.admin.email` publicly on the contact page; never link raw `/.well-known/security.txt` — link to `/server/security`
- Never allow remote SVG logos/images without sanitization/rasterization; never fetch remote branding images over `http://`, from localhost/private/internal hosts, or without size/type/timeout limits
- Never render site-verification meta tags with empty content, invalid format, or characters failing validation
- Never include authenticated/server-management pages or `/api/*` endpoints in `sitemap.xml`
- Never persist user preferences (theme/lang) server-side in a database — read only from cookies per request
- Never store PII in cookies or localStorage
- Never treat `Origin`/`Referer` header presence as a CSRF bypass
- Never use `Access-Control-Allow-Headers: *` with credentials allowed; never combine `Access-Control-Allow-Credentials: true` with `Access-Control-Allow-Origin: *`
- Never cache API responses in the service worker (network-only) except explicit offline-first data
- Never require JavaScript for core functionality — navigation, forms, and CRUD must work without JS

## CRITICAL - ALWAYS DO
- Build a fully functional, professional, mobile-first, WCAG 2.1 AA-accessible, PWA-capable frontend for every project (API is the source of truth)
- Use Go `html/template` (`.tmpl` files) for all HTML, embedded in the binary via `//go:embed`
- Priority order: HTML5 first → CSS second → JavaScript last resort; every JS line must be justified
- Auto-detect client type (HTML/text/JSON) via `Accept` header first, then User-Agent (browsers→HTML, curl/CLI tools→text, empty UA→text, default→HTML)
- Support full CRUD via HTML forms, JSON API, and form-encoded/text for CLI, on the same frontend routes
- Apply `URLNormalizeMiddleware` first in the middleware chain to strip trailing slashes (301 redirect), excluding root and file paths
- All non-HTML responses (JSON, text) end with a single trailing `\n`
- Use the standard `{ok, data}` / `{ok:false, error, message}` unified response envelope for API/AJAX; text responses use `OK: {message}` / `ERROR: {code}: {message}`
- Use semantic HTML5 elements correctly: `<code>`, `<pre><code>`, `<kbd>`, `<samp>`, `<var>`, `<time>`, `<mark>`, `<abbr>`, `<header>`, `<nav>`, `<main>`, `<footer>`, `<article>`, `<section>`, `<aside>`
- Copy buttons must show visible "Copied!" feedback (icon + label swap, `aria-live="polite"`, revert after 2s) for long/complex values (tokens, .onion addresses, git URLs)
- Disable submit buttons immediately on click, show a loading-state label ("Saving...", etc.), re-enable on success or error, preserve button width
- Use native `<dialog>` for modals (focus trap, Esc, backdrop native); close/cancel via `<form method="dialog">` with zero JS
- Toasts: top-right, stack newest-on-top, max 5 visible, auto-dismiss 3s (success/info) / 5s (warning) / never (error), pause on hover, click/X/Escape to dismiss
- Site banner is the first element inside `<body>`, before `<main>`, server-rendered, dismissal works via POST form + cookie with zero JS required
- Theme toggle (dark/light/auto) lives in header, persisted via server-readable `theme` cookie, rendered server-side on `<html>` (no FOUC, no init JS); "auto" is pure CSS via `prefers-color-scheme`
- All forms: HTML5 validation first (`required`, `pattern`, `type=`), validate on blur, inline errors below field with `aria-describedby`/`role=alert`, trim whitespace, reject passwords with leading/trailing whitespace
- Respect `prefers-reduced-motion` — disable animations/transitions when set
- Ship a complete PWA: `/manifest.json`, service worker (install/activate/fetch lifecycle), offline fallback page, all icon sizes incl. maskable, HTTPS required
- Service worker caching: static assets cache-first, HTML pages network-first with cache fallback, API calls network-only (queued via background sync when offline)
- Cache versioning follows semver in cache name (`api-cache-v{major}.{minor}.{patch}`); clean old caches on `activate`
- Show custom install-prompt UI (capture `beforeinstallprompt`), detect standalone/installed mode, provide manual iOS install instructions (no `beforeinstallprompt` on iOS)
- Request geolocation only on explicit user action, with a stated reason, and handle permission denial gracefully
- All CSS variables/themes/palette defined once (`src/common/theme/colors.go`) and shared across Web, Swagger, GraphQL, CLI, TUI, GUI
- Both dark and light themes must meet WCAG AA contrast (4.5:1 minimum text, 3:1 large text) with no information conveyed by color alone
- Every page must include header, nav, and footer partials — no page defines its own
- Content for `/server/about`, `/server/help`, etc. MUST be sourced from IDEA.md — never generic placeholders
- Cookie consent banner always shown (server-rendered, visible with zero JS) until a valid `cookie_consent` cookie exists; granular categories (essential/preferences/analytics)
- CSRF: stateless double-submit cookie pattern (`csrf_token` cookie `SameSite=Strict`, not HttpOnly, matched against header/form field in constant time) for all mutating browser (non-Bearer) requests
- Sanitize any operator-supplied custom HTML (footer branding) with a strict allow-list (bluemonday-style) — strip scripts, event handlers, `style` attr, iframes, forms

## Key Rules Summary
- **Route structure**: `/api/{v}/{resource}` maps to `/{resource}` frontend equivalents; route priority: API > healthz > static > `/server/*` > reserved names > project catch-all/slug routes
- **Reserved names**: block registration of system/common/technical path names (`api`, `server`, `static`, `search`, `help`, `docs`, etc.) from colliding with vanity/slug routes
- **Middleware order**: URLNormalize → RequestID → PathSecurity → SecurityHeaders → Allowlist → Blocklist → RateLimit → GeoIP → Auth → Logging
- **Breakpoints**: base = mobile (<768px); `min-width:768px` tablet; `min-width:1024px` desktop; `min-width:1280px` optional large desktop
- **Container**: 100% width/1rem padding mobile → 90% width, max-width 1400px, centered at 768px+
- **Responsive table pattern**: wrap in `.table-wrapper` with horizontal scroll on mobile, `min-width` forcing scroll until 768px
- **Definition lists** (`<dl>`) stack on mobile, become 2-column (`auto 1fr`) at 768px+
- **Mobile nav**: CSS-only hamburger via hidden checkbox + `:checked ~` selectors, no JS; menu slides in from right; theme toggle always stays in header, never inside the menu; only show hamburger if links overflow
- **No fixed/sticky elements**: header/nav/footer scroll with page; footer always at bottom via flex `min-height:100vh` + `main{flex:1}`
- **Print styles**: hide nav/header/footer/buttons/toasts/modals; reset to white background/black text; show URL after external links; avoid page breaks inside `pre`/tables/images
- **Templates**: layout (`layout/public.tmpl`) → partials (`head`, `header`, `nav`, `footer`, `scripts`) → page content; mandatory partials must exist; app-specific partials only when reused 2+ places
- **Error pages** (400/401/403/404/500/502/503) must use the themed `error.tmpl`, include nav, no stack traces in production
- **CSS organization**: `common.css` (variables/reset) → `components.css` → `public.css`, loaded in that order; BEM-like naming; variables in `:root`
- **JavaScript in app.js only**: clipboard, toasts, modal helpers, complex validation, AJAX, WebSockets, and small data-action bindings (never inline handlers)
- **HTML5/CSS-first substitution table**: `<details>/<summary>` for collapsibles, checkbox hack for menus/toggles, `:focus-within` for dropdowns, `<dialog>` for modals, `<progress>`/`<input type=range/date/color>` instead of custom JS widgets
- **UI element selection**: dropdown for >5 mutually exclusive options, radio for 2-5, checkbox/toggle for boolean, never plain free text for constrained choices
- **Accessibility**: keyboard-operable, visible focus rings, ARIA labels/`aria-describedby`/`role`, 4.5:1 contrast (3:1 large text), alt text, labeled inputs, skip-to-content link, proper heading hierarchy, screen-reader announced errors
- **PWA manifest** required fields: `name`, `short_name`, `start_url`, `display`, `icons` (incl. 192/512 + maskable); `start_url` must be within `scope`
- **iOS quirks**: no `beforeinstallprompt`/background sync/badging/web share target; storage evicted after 7 days idle; needs `apple-mobile-web-app-*` meta tags and `apple-touch-icon`
- **Offline behavior**: static assets cache-first, HTML network-first w/ cache fallback, API network-only + IndexedDB queue with exponential-backoff sync retries; offline indicator toggled on `online`/`offline` events
- **Token/preference storage**: `owner_token` cookie is HttpOnly+Secure+SameSite=Strict (primary, web-only); localStorage is optional convenience copy only, never load-bearing; theme/lang/consent stored in cookies, never server-persisted
- **Content-Type detection**: API routes → JSON; `.txt` suffix (API only) → text; `Accept` header negotiation; CLI auto→text; browser default→HTML
- **CORS**: default `allowed_origins: ["*"]` (no credentials); explicit origin list enables credentials; resolution order: explicit config → DOMAIN env → trusted-proxy X-Forwarded-Host → fallback `*`; never wildcard Allow-Headers with credentials
- **SEO**: dynamic `/sitemap.xml` (homepage priority 1.0/daily, public pages 0.8/weekly, API docs 0.7/weekly; never include auth/admin/API endpoints); site-verification meta tags validated by provider-specific regex/length before render; OpenGraph/Twitter card meta tags generated from branding config
- **Branding**: cosmetic only (title, logo, footer, SEO tags) — never changes system paths/binary/service names; remote image fetch requires HTTPS-only, SSRF-safe (no private/loopback/internal IPs), size/type/timeout limits, redirect re-validation
- **Announcements**: config-driven (`web.announcements`), rendered via Site Banner, multiple stack in config order, per-item `start`/`end` ISO 8601 window, dismissal by cookie keyed on announcement `id`
- **Standard pages required**: `/server/about`, `/server/privacy`, `/server/contact`, `/server/help`, `/server/terms`, `/server/healthz`, plus matching JSON API equivalents under `/api/{v}/server/*`
- **Privacy page**: auto-generated sections from `server.privacy` config (cookies, data collection/usage/security/storage/retention, third parties, rights); CCPA "Do Not Sell" section only rendered when `data.sold=true`
- **Contact page**: submits to `server.contact.general.email` (fallback `admin.email`); must render fixed Security Issues and Abuse Reports sections as specified
- **i18n**: `lang` cookie (BCP 47 tag) drives `<html lang>`/`dir`, falls back to `Accept-Language` header, default `en`

For complete details, see AI.md PART 16
