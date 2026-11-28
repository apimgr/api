package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/mail"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
	"github.com/apimgr/api/src/securitydata"
)

func blocklistMiddleware(cfg *config.ServerConfig) func(http.Handler) http.Handler {
	securitydata.DefaultBlocklists.Reload(cfg, time.Now().UTC())
	securitydata.RegisterHealthChecks(cfg)
	var configMu sync.Mutex
	loadedConfig := cfg
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			current := config.Get()
			if current == nil {
				current = cfg
			}
			if current == nil || !current.Server.Security.Blocklists.Enabled {
				next.ServeHTTP(w, r)
				return
			}
			configMu.Lock()
			if current != loadedConfig {
				securitydata.DefaultBlocklists.Reload(current, time.Now().UTC())
				loadedConfig = current
			}
			configMu.Unlock()
			if !isAllowlistedRequest(r) {
				clientIP := getClientIP(r)
				if addr, err := netip.ParseAddr(clientIP); err == nil {
					if source, blocked := securitydata.DefaultBlocklists.MatchesIP(addr); blocked {
						writeBlocklistForbidden(w, r, source, "ip")
						return
					}
				}
			}
			for _, host := range blocklistDomainTargets(r) {
				if source, blocked := securitydata.DefaultBlocklists.MatchesDomain(host); blocked {
					writeBlocklistForbidden(w, r, source, "domain")
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func blocklistDomainTargets(r *http.Request) []string {
	targets := make([]string, 0)
	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = r.Header.Get("Referer")
	}
	if origin != "" {
		addURLHost(&targets, origin)
	}
	for key, values := range r.URL.Query() {
		for _, value := range values {
			addSubmittedTarget(&targets, key, value)
		}
	}
	var formErr error
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType == "multipart/form-data" {
		formErr = r.ParseMultipartForm(1 << 20)
	} else {
		formErr = r.ParseForm()
	}
	if formErr == nil {
		for key, values := range r.PostForm {
			for _, value := range values {
				addSubmittedTarget(&targets, key, value)
			}
		}
	}
	mediaType = strings.ToLower(mediaType)
	if strings.Contains(mediaType, "json") && r.Body != nil {
		body, err := io.ReadAll(r.Body)
		if err == nil {
			r.Body = io.NopCloser(bytes.NewReader(body))
			var value any
			if json.Unmarshal(body, &value) == nil {
				walkSubmittedJSON(&targets, "", value)
			}
		}
	}
	return targets
}

func walkSubmittedJSON(targets *[]string, key string, value any) {
	switch typed := value.(type) {
	case map[string]any:
		for childKey, child := range typed {
			walkSubmittedJSON(targets, childKey, child)
		}
	case []any:
		for _, child := range typed {
			walkSubmittedJSON(targets, key, child)
		}
	case string:
		addSubmittedTarget(targets, key, typed)
	}
}

func addSubmittedTarget(targets *[]string, key, value string) {
	key = strings.ToLower(key)
	if key == "email" || strings.HasSuffix(key, "_email") || strings.Contains(value, "@") {
		if address, err := mail.ParseAddress(value); err == nil {
			if at := strings.LastIndex(address.Address, "@"); at >= 0 && at < len(address.Address)-1 {
				*targets = append(*targets, address.Address[at+1:])
			}
		}
	}
	if key == "url" || strings.HasSuffix(key, "_url") || strings.Contains(key, "webhook") || strings.Contains(key, "destination") || strings.Contains(key, "target") || strings.Contains(value, "://") {
		addURLHost(targets, value)
	}
}

func addURLHost(targets *[]string, value string) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Hostname() == "" {
		return
	}
	host := strings.Trim(parsed.Hostname(), "[]")
	if net.ParseIP(host) != nil {
		return
	}
	*targets = append(*targets, host)
}

func writeBlocklistForbidden(w http.ResponseWriter, r *http.Request, id, kind string) {
	if err := database.WriteAuditEvent(r.Context(), "security.blocklist_rejected", "", getClientIP(r), map[string]any{"source": id, "type": kind}); err != nil {
		log.Printf("could not record blocklist rejection (source=%s): %v", id, err)
	}
	writeLocalizedAPIError(w, r, http.StatusForbidden, "FORBIDDEN")
}
