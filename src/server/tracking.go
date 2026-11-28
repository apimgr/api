package server

import (
	"encoding/json"
	"html"
	"net/http"
	"net/url"
	"strings"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/i2p"
	"github.com/apimgr/api/src/tor"
)

type trackingOutput struct {
	Tags        string
	Noscript    string
	Body        string
	ScriptHost  string
	ConnectHost string
	ImageHost   string
}

func buildTrackingOutput(cfg config.TrackingConfig) trackingOutput {
	if !cfg.Enabled() {
		return trackingOutput{}
	}
	id := html.EscapeString(cfg.ID)
	base := strings.TrimSuffix(strings.TrimSpace(cfg.URL), "/")
	parsed, _ := url.Parse(base)
	host := ""
	if parsed != nil && parsed.Host != "" {
		host = parsed.Scheme + "://" + parsed.Host
	}
	quotedID, _ := json.Marshal(cfg.ID)
	quotedBase, _ := json.Marshal(base)
	switch cfg.Normalized() {
	case "google":
		if strings.HasPrefix(cfg.ID, "G-") {
			return trackingOutput{
				Tags: `<script async src="https://www.googletagmanager.com/gtag/js?id=` + url.QueryEscape(cfg.ID) + `"></script>`,
				Body: `window.dataLayer=window.dataLayer||[];function gtag(){dataLayer.push(arguments);}` +
					`gtag('js',new Date());gtag('config',` + string(quotedID) + `);`,
				ScriptHost: "https://www.googletagmanager.com", ConnectHost: "https://www.google-analytics.com",
			}
		}
		return trackingOutput{Tags: `<script async src="https://www.google-analytics.com/analytics.js"></script>`,
			Body: `window.ga=window.ga||function(){(ga.q=ga.q||[]).push(arguments)};ga.l=+new Date;` +
				`ga('create',` + string(quotedID) + `,'auto');ga('send','pageview');`, ScriptHost: "https://www.google-analytics.com"}
	case "matomo", "piwik":
		return trackingOutput{Body: `var _paq=window._paq=window._paq||[];_paq.push(['trackPageView']);` +
			`_paq.push(['enableLinkTracking']);_paq.push(['setTrackerUrl',` + string(quotedBase) + `+'/matomo.php']);` +
			`_paq.push(['setSiteId',` + string(quotedID) + `]);var g=document.createElement('script');` +
			`g.async=true;g.src=` + string(quotedBase) + `+'/matomo.js';document.head.appendChild(g);`,
			ScriptHost: host, ConnectHost: host}
	case "owa":
		return trackingOutput{Body: `var owa_baseUrl=` + string(quotedBase) + `+'/';var owa_cmds=window.owa_cmds=window.owa_cmds||[];` +
			`owa_cmds.push(['setSiteId',` + string(quotedID) + `]);owa_cmds.push(['trackPageView']);owa_cmds.push(['trackClicks']);` +
			`var o=document.createElement('script');o.async=true;o.src=owa_baseUrl+'modules/base/js/owa.tracker-combined-min.js';document.head.appendChild(o);`,
			ScriptHost: host, ConnectHost: host}
	case "fathom":
		src := "https://cdn.usefathom.com/script.js"
		if base != "" {
			src = base + "/tracker.js"
		}
		providerHost := hostOr("https://cdn.usefathom.com", host)
		return trackingOutput{Tags: `<script defer src="` + html.EscapeString(src) + `" data-site="` + id + `"></script>`, ScriptHost: providerHost, ConnectHost: providerHost}
	case "plausible":
		src := "https://plausible.io/js/script.js"
		if base != "" {
			src = base + "/js/script.js"
		}
		providerHost := hostOr("https://plausible.io", host)
		return trackingOutput{Tags: `<script defer data-domain="` + id + `" src="` + html.EscapeString(src) + `"></script>`, ScriptHost: providerHost, ConnectHost: providerHost}
	case "umami":
		return trackingOutput{Tags: `<script defer src="` + html.EscapeString(base) + `/script.js" data-website-id="` + id + `"></script>`, ScriptHost: host, ConnectHost: host}
	case "simple":
		return trackingOutput{Tags: `<script async defer src="https://scripts.simpleanalyticscdn.com/latest.js"></script>`,
			Noscript:   `<noscript><img src="https://queue.simpleanalyticscdn.com/noscript.gif" alt="" referrerpolicy="no-referrer-when-downgrade" /></noscript>`,
			ScriptHost: "https://scripts.simpleanalyticscdn.com", ImageHost: "https://queue.simpleanalyticscdn.com"}
	case "cloudflare":
		return trackingOutput{Tags: `<script defer src="https://static.cloudflareinsights.com/beacon.min.js" data-cf-beacon='{"token":"` + id + `"}'></script>`,
			ScriptHost: "https://static.cloudflareinsights.com", ConnectHost: "https://cloudflareinsights.com"}
	default:
		return trackingOutput{}
	}
}

func hostOr(fallback, configured string) string {
	if configured != "" {
		return configured
	}
	return fallback
}

func trackingAllowed(cfg *config.ServerConfig, r *http.Request) bool {
	if cfg == nil || r == nil || !cfg.Server.Privacy.Cookies.Analytics.Enabled || !cfg.Server.Tracking.Enabled() {
		return false
	}
	if IsTorRequest(r) || isTorHostRequest(r) || isI2PBackendRequest(r) || isI2PHostRequest(r) {
		return false
	}
	return readCookieConsent(r).Analytics
}

func isTorHostRequest(r *http.Request) bool {
	manager := tor.Get()
	return manager != nil && manager.OnionAddress() != "" && strings.EqualFold(strings.TrimSpace(r.Host), manager.OnionAddress())
}

func isI2PHostRequest(r *http.Request) bool {
	if strings.HasSuffix(strings.ToLower(strings.TrimSpace(r.Host)), ".b32.i2p") {
		return true
	}
	manager := i2p.Get()
	return manager != nil && manager.Snapshot().Address != "" && strings.EqualFold(strings.TrimSpace(r.Host), manager.Snapshot().Address)
}

func trackingScriptHandler(cfg *config.ServerConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tracking := buildTrackingOutput(cfg.Server.Tracking)
		if !trackingAllowed(cfg, r) || tracking.Body == "" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write([]byte(tracking.Body))
	}
}
