package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// AutoTightenChange records one field the IDEA.md → Header Tightening
// Auto-Map actually changed away from its default, for the setup audit log
// (AI.md "the spec never tightens silently — every auto-tighten is logged
// to the setup audit so operators know what changed and why").
type AutoTightenChange struct {
	Field    string
	OldValue string
	NewValue string
	Trigger  string
}

var (
	lastAutoTightenMu      sync.Mutex
	lastAutoTightenChanges []AutoTightenChange
)

// LastAutoTightenChanges returns the changes applied by the most recent
// first-run IDEA.md → Header Tightening Auto-Map pass (empty if Load() has
// not run yet, if server.yml already existed, or if no IDEA.md declarations
// triggered a tightening). Callers (main.go, once the logger is
// initialized) use this to write the setup-audit log entries — Load() runs
// before server.InitLogger, so the audit write can't happen inline.
func LastAutoTightenChanges() []AutoTightenChange {
	lastAutoTightenMu.Lock()
	defer lastAutoTightenMu.Unlock()
	out := make([]AutoTightenChange, len(lastAutoTightenChanges))
	copy(out, lastAutoTightenChanges)
	return out
}

// applyIdeaHeaderAutoMap implements AI.md "IDEA.md → Header Tightening
// Auto-Map": at first-run setup (server.yml does not exist yet) the binary
// reads IDEA.md's "## Compliance declarations" section and pre-fills
// web.headers.* per the spec's trigger table. It is first-run-only by
// construction — the only caller is Load()'s "config file does not exist"
// branch, so it never re-tightens a server.yml an operator has already
// edited or reverted. No IDEA.md, or an IDEA.md with no "## Compliance
// declarations" section, or one that declares nothing: cfg is returned
// unmodified ("everyone" defaults stay loose, per spec).
func applyIdeaHeaderAutoMap(cfg *Config) {
	declared := loadComplianceDeclarations()

	changes := buildHeaderAutoMap(cfg, declared)

	lastAutoTightenMu.Lock()
	lastAutoTightenChanges = changes
	lastAutoTightenMu.Unlock()
}

// loadComplianceDeclarations locates and parses IDEA.md's "## Compliance
// declarations" section. Returns an empty (non-nil) map if no IDEA.md is
// found or the section is absent/empty — a genuinely deployed binary
// (server.yml's directory, not a source checkout) commonly has no IDEA.md
// alongside it at all, which is the expected "no declarations" case.
func loadComplianceDeclarations() map[string][]string {
	path := findIdeaMD()
	if path == "" {
		return map[string][]string{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string][]string{}
	}
	return parseComplianceDeclarations(data)
}

// findIdeaMD looks for IDEA.md next to the current working directory and
// next to the running binary — the two places a self-contained binary might
// reasonably find a project's own IDEA.md at first-run setup time. Returns
// "" if neither exists.
func findIdeaMD() string {
	candidates := []string{}

	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, "IDEA.md"))
	}
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "IDEA.md"))
	}

	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && !info.IsDir() {
			return c
		}
	}
	return ""
}

// complianceSectionHeading is the exact "## Compliance declarations"
// heading this project's IDEA.md uses for the auto-map's declared
// audience/compliance/data-class/capability flags.
const complianceSectionHeading = "## Compliance declarations"

// declarationLineRE-equivalent parsing (no regex import needed): a
// declaration line is "key: value[, value2, ...]" — anything else inside
// the section (prose, blank lines, comments) is ignored, so a project that
// documents the empty case in prose (as this project's own IDEA.md does)
// parses to an empty map rather than an error.
func parseComplianceDeclarations(data []byte) map[string][]string {
	out := map[string][]string{}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	inSection := false
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "## ") {
			inSection = trimmed == complianceSectionHeading
			continue
		}
		if !inSection || trimmed == "" {
			continue
		}

		idx := strings.Index(trimmed, ":")
		if idx <= 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(trimmed[:idx]))
		if !isDeclarationKey(key) {
			continue
		}
		valuePart := strings.TrimSpace(trimmed[idx+1:])
		if valuePart == "" {
			continue
		}
		for _, v := range strings.Split(valuePart, ",") {
			v = strings.ToLower(strings.TrimSpace(v))
			if v != "" {
				out[key] = append(out[key], v)
			}
		}
	}

	return out
}

// isDeclarationKey restricts parsing to the fixed set of keys the auto-map
// table understands, so unrelated "## Compliance declarations" prose that
// happens to contain a colon (e.g. a sentence with "note:") is never
// mistaken for a declaration.
func isDeclarationKey(key string) bool {
	switch key {
	case "audience", "compliance", "data_class", "uses_sharedarraybuffer", "uses_wasm_threads", "embeds_third_party":
		return true
	default:
		return false
	}
}

func declHas(declared map[string][]string, key, value string) bool {
	for _, v := range declared[key] {
		if v == value {
			return true
		}
	}
	return false
}

func declTrue(declared map[string][]string, key string) bool {
	for _, v := range declared[key] {
		switch v {
		case "true", "yes", "on", "1", "enable", "enabled":
			return true
		}
	}
	return false
}

// coopRank/coepRank/corpRank/referrerRank define "strictest wins" ordering
// for AI.md's multi-compliance merge rule: "when multiple compliances
// apply ... the per-header strictest value wins, mirroring PART 11 →
// Compliance Requirements Matrix". Higher rank = stricter; a header is only
// ever tightened (moved to a higher rank than its current default), never
// loosened, by the auto-map.
var coopRank = map[string]int{
	"unsafe-none":              0,
	"same-origin-allow-popups": 1,
	"same-origin":              2,
}

var coepRank = map[string]int{
	"unsafe-none":  0,
	"require-corp": 1,
}

var corpRank = map[string]int{
	"cross-origin": 0,
	"same-site":    1,
	"same-origin":  2,
}

var referrerRank = map[string]int{
	"strict-origin-when-cross-origin": 0,
	"no-referrer":                     1,
}

// buildHeaderAutoMap applies AI.md's IDEA.md → Header Tightening Auto-Map
// trigger table to cfg in place and returns the list of fields it actually
// changed (for the setup audit log). Rows that target config surface this
// project genuinely has no implementation path for (payment-page
// frame-ancestors/X-Frame-Options overrides, PHI-endpoint Cache-Control)
// are intentionally not applied — see the doc comments on each skip below.
func buildHeaderAutoMap(cfg *Config, declared map[string][]string) []AutoTightenChange {
	var changes []AutoTightenChange

	coppa := declHas(declared, "audience", "children") || declHas(declared, "compliance", "coppa")
	hipaa := declHas(declared, "compliance", "hipaa") || declHas(declared, "data_class", "phi")
	pciDSS := declHas(declared, "compliance", "pci-dss") || declHas(declared, "data_class", "cardholder")
	privacy := declHas(declared, "compliance", "gdpr") || declHas(declared, "compliance", "ccpa") ||
		declHas(declared, "compliance", "ukgdpr") || declHas(declared, "compliance", "lgpd")
	glba := declHas(declared, "compliance", "glba") || declHas(declared, "audience", "financial")
	ferpa := declHas(declared, "compliance", "ferpa") || declHas(declared, "data_class", "education_records")
	sabOrWasmThreads := declTrue(declared, "uses_sharedarraybuffer") || declTrue(declared, "uses_wasm_threads")

	h := &cfg.Web.Headers

	// COOP: strictest wins across coppa/pci-dss/glba/sab-wasm-threads.
	coopTarget, coopTrigger := "", ""
	if coppa && coopRank["same-origin"] > coopRank[coopTarget] {
		coopTarget, coopTrigger = "same-origin", "audience:children / compliance:coppa"
	}
	if pciDSS && coopRank["same-origin"] > coopRank[coopTarget] {
		coopTarget, coopTrigger = "same-origin", "compliance:pci-dss / data_class:cardholder"
	}
	if glba && coopRank["same-origin-allow-popups"] > coopRank[coopTarget] {
		coopTarget, coopTrigger = "same-origin-allow-popups", "compliance:glba / audience:financial"
	}
	if sabOrWasmThreads && coopRank["same-origin"] > coopRank[coopTarget] {
		coopTarget, coopTrigger = "same-origin", "uses_sharedarraybuffer / uses_wasm_threads"
	}
	if coopTarget != "" && coopRank[coopTarget] > coopRank[h.COOP] {
		changes = append(changes, AutoTightenChange{"web.headers.coop", h.COOP, coopTarget, coopTrigger})
		h.COOP = coopTarget
	}

	// COEP: strictest wins across pci-dss/glba (glba inherits pci-dss's
	// COEP per AI.md's "as PCI-DSS" prefix on the glba row)/sab-wasm-threads.
	coepTarget, coepTrigger := "", ""
	if pciDSS && coepRank["require-corp"] > coepRank[coepTarget] {
		coepTarget, coepTrigger = "require-corp", "compliance:pci-dss / data_class:cardholder"
	}
	if glba && coepRank["require-corp"] > coepRank[coepTarget] {
		coepTarget, coepTrigger = "require-corp", "compliance:glba / audience:financial (as pci-dss)"
	}
	if sabOrWasmThreads && coepRank["require-corp"] > coepRank[coepTarget] {
		coepTarget, coepTrigger = "require-corp", "uses_sharedarraybuffer / uses_wasm_threads"
	}
	if coepTarget != "" && coepRank[coepTarget] > coepRank[h.COEP] {
		changes = append(changes, AutoTightenChange{"web.headers.coep", h.COEP, coepTarget, coepTrigger})
		h.COEP = coepTarget
	}

	// CORP: strictest wins across coppa(same-site)/hipaa/ferpa(same-origin).
	corpTarget, corpTrigger := "", ""
	if coppa && corpRank["same-site"] > corpRank[corpTarget] {
		corpTarget, corpTrigger = "same-site", "audience:children / compliance:coppa"
	}
	if hipaa && corpRank["same-origin"] > corpRank[corpTarget] {
		corpTarget, corpTrigger = "same-origin", "compliance:hipaa / data_class:phi"
	}
	if ferpa && corpRank["same-origin"] > corpRank[corpTarget] {
		corpTarget, corpTrigger = "same-origin", "compliance:ferpa / data_class:education_records"
	}
	if corpTarget != "" && corpRank[corpTarget] > corpRank[h.CORP] {
		changes = append(changes, AutoTightenChange{"web.headers.corp", h.CORP, corpTarget, corpTrigger})
		h.CORP = corpTarget
	}

	// Referrer-Policy: strictest wins; privacy (gdpr/ccpa/ukgdpr/lgpd) only
	// ever targets the already-default value so it never produces a
	// logged change on its own.
	referrerTarget, referrerTrigger := "", ""
	if coppa && referrerRank["no-referrer"] > referrerRank[referrerTarget] {
		referrerTarget, referrerTrigger = "no-referrer", "audience:children / compliance:coppa"
	}
	if pciDSS && referrerRank["no-referrer"] > referrerRank[referrerTarget] {
		referrerTarget, referrerTrigger = "no-referrer", "compliance:pci-dss / data_class:cardholder"
	}
	if glba && referrerRank["no-referrer"] > referrerRank[referrerTarget] {
		referrerTarget, referrerTrigger = "no-referrer", "compliance:glba / audience:financial"
	}
	current := h.ReferrerPolicy
	if current == "" {
		current = "strict-origin-when-cross-origin"
	}
	if referrerTarget != "" && referrerRank[referrerTarget] > referrerRank[current] {
		changes = append(changes, AutoTightenChange{"web.headers.referrer_policy", current, referrerTarget, referrerTrigger})
		h.ReferrerPolicy = referrerTarget
	}

	// honor_sec_gpc: bool OR across coppa/hipaa/privacy/ferpa. Already
	// true by default in this project, so this is a no-op unless an
	// operator's own base default ever changes.
	if (coppa || hipaa || privacy || ferpa) && !h.HonorSecGPC {
		var trigger string
		switch {
		case coppa:
			trigger = "audience:children / compliance:coppa"
		case hipaa:
			trigger = "compliance:hipaa / data_class:phi"
		case privacy:
			trigger = "compliance:gdpr|ccpa|ukgdpr|lgpd"
		case ferpa:
			trigger = "compliance:ferpa / data_class:education_records"
		}
		changes = append(changes, AutoTightenChange{"web.headers.honor_sec_gpc", "false", "true", trigger})
		h.HonorSecGPC = true
	}

	// clear_site_data.execution_contexts: coppa only.
	if coppa && !h.ClearSiteData.ExecutionContexts {
		changes = append(changes, AutoTightenChange{
			"web.headers.clear_site_data.execution_contexts", "false", "true",
			"audience:children / compliance:coppa",
		})
		h.ClearSiteData.ExecutionContexts = true
	}

	// Permissions-Policy sensors/camera/mic/geo lock (coppa row) and the
	// FERPA tracking-proposals lock are both already "()" in
	// defaultConfig() (see PermissionsPolicyConfig in config.go), so they
	// never produce a logged change — the row's requirement is satisfied
	// by this project's existing default, not by this auto-map.

	// server_timing_in_debug_only (hipaa row) is already true in
	// defaultConfig() ("already default" per the AI.md table itself) — no
	// action needed.

	// HIPAA "Cache-Control: no-store, no-cache on PHI endpoints" and
	// PCI-DSS/GLBA "frame-ancestors='none'/X-Frame-Options=DENY on payment
	// pages": this IDEA.md-scoped project has no PHI-classified endpoints
	// and no payment pages (no accounts, no checkout flow — IDEA.md
	// non-goals), so there is no per-endpoint/per-page config surface to
	// target. Intentionally not implemented; see
	// src/server/middleware.go's X-Frame-Options comment.

	return changes
}
