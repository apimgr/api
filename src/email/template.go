package email

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	emailtemplates "github.com/apimgr/api/src/server/template/email"
)

// Template represents a parsed email notification template (AI.md PART 17
// "Template Format"): a subject line followed by a "---" separator and a
// plain-text body, both using "{variable}" substitution syntax.
type Template struct {
	Name    string
	Subject string
	Body    string
}

// TemplateNames lists the 10 built-in notification templates (AI.md
// PART 17 "Default Templates"), in the order they appear there.
var TemplateNames = []string{
	"security_alert",
	"backup_complete",
	"backup_failed",
	"ssl_expiring",
	"ssl_renewed",
	"ssl_renewal_failed",
	"scheduler_error",
	"update_available",
	"update_installed",
	"test",
}

// GlobalVariables lists the variables available in every template (AI.md
// PART 17 "Global Variables").
var GlobalVariables = []string{
	"app_name",
	"app_url",
	"fqdn",
	"onion_url",
	"onion_address",
	"i2p_url",
	"i2p_address",
	"notification_reply_to",
	"timestamp",
	"year",
}

// templateVariables lists the extra, template-specific variables allowed
// on top of GlobalVariables (AI.md PART 17 "Template-Specific Variables").
var templateVariables = map[string][]string{
	"security_alert":     {"event", "ip", "details"},
	"backup_complete":    {"filename", "size"},
	"backup_failed":      {"filename", "size", "error"},
	"ssl_expiring":       {"expires_in", "expiry_date"},
	"ssl_renewed":        {"expires_in", "expiry_date", "valid_until"},
	"ssl_renewal_failed": {"error", "expires_in", "expiry_date", "next_retry"},
	"scheduler_error":    {"task_name", "error", "next_run"},
	"update_available":   {"current_version", "new_version", "channel"},
	"update_installed":   {"previous_version", "new_version"},
	"test":               {},
}

// varPattern matches "{variable_name}" tokens (AI.md PART 17 "Template
// Format" - "Variables: {variable_name} syntax").
var varPattern = regexp.MustCompile(`\{([a-zA-Z_][a-zA-Z0-9_]*)\}`)

// allowedVariableSet returns the set of variable names permitted in the
// named template: GlobalVariables plus that template's own extra
// variables (AI.md PART 17 "Global Variables" + "Template-Specific
// Variables").
func allowedVariableSet(name string) map[string]bool {
	allowed := make(map[string]bool, len(GlobalVariables)+len(templateVariables[name]))
	for _, v := range GlobalVariables {
		allowed[v] = true
	}
	for _, v := range templateVariables[name] {
		allowed[v] = true
	}
	return allowed
}

// ParseTemplate parses the raw "{project_name} email test"-format content
// of one template file (AI.md PART 17 "Template Format": first line
// "Subject: ...", then a "---" separator line, then the plain-text body).
func ParseTemplate(name string, raw []byte) (*Template, error) {
	lines := strings.Split(string(raw), "\n")
	if len(lines) == 0 || !strings.HasPrefix(strings.TrimRight(lines[0], "\r"), "Subject:") {
		return nil, fmt.Errorf("invalid template syntax at line 1")
	}
	subject := strings.TrimSpace(strings.TrimPrefix(strings.TrimRight(lines[0], "\r"), "Subject:"))

	sepIdx := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			sepIdx = i
			break
		}
	}
	if sepIdx == -1 {
		return nil, fmt.Errorf("invalid template syntax at line 2")
	}

	body := strings.Join(lines[sepIdx+1:], "\n")
	body = strings.TrimRight(body, "\n") + "\n"

	return &Template{Name: name, Subject: subject, Body: body}, nil
}

// checkBraceBalance rejects a template where some line has mismatched
// "{"/"}" counts, reported as AI.md PART 17's "Invalid template syntax at
// line N" validation error.
func checkBraceBalance(raw []byte) error {
	lines := strings.Split(string(raw), "\n")
	for i, line := range lines {
		if strings.Count(line, "{") != strings.Count(line, "}") {
			return fmt.Errorf("invalid template syntax at line %d", i+1)
		}
	}
	return nil
}

// extractVariables returns the sorted, de-duplicated set of "{name}"
// variable tokens referenced in s.
func extractVariables(s string) []string {
	matches := varPattern.FindAllStringSubmatch(s, -1)
	seen := make(map[string]bool, len(matches))
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		if !seen[m[1]] {
			seen[m[1]] = true
			names = append(names, m[1])
		}
	}
	sort.Strings(names)
	return names
}

// levenshtein computes the edit distance between a and b, used to power
// the "Did you mean {x}?" validation hint.
func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			del := prev[j] + 1
			ins := curr[j-1] + 1
			sub := prev[j-1] + cost
			min := del
			if ins < min {
				min = ins
			}
			if sub < min {
				min = sub
			}
			curr[j] = min
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

// suggestVariable returns the closest name in allowed to want (edit
// distance <= 2), or "" if nothing is close enough to suggest.
func suggestVariable(want string, allowed map[string]bool) string {
	best := ""
	bestDist := 3
	names := make([]string, 0, len(allowed))
	for name := range allowed {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if d := levenshtein(want, name); d < bestDist {
			bestDist = d
			best = name
		}
	}
	return best
}

// ValidateTemplate checks raw template content before it is saved, per
// AI.md PART 17 "Template Validation". It returns non-blocking warnings
// plus a blocking error for the first violation found (unknown variable,
// empty subject/body, or invalid syntax).
func ValidateTemplate(name string, raw []byte) (warnings []string, err error) {
	if syntaxErr := checkBraceBalance(raw); syntaxErr != nil {
		return nil, syntaxErr
	}

	tmpl, parseErr := ParseTemplate(name, raw)
	if parseErr != nil {
		return nil, parseErr
	}

	if strings.TrimSpace(tmpl.Subject) == "" {
		return nil, fmt.Errorf("subject cannot be empty")
	}
	if strings.TrimSpace(tmpl.Body) == "" {
		return nil, fmt.Errorf("body cannot be empty")
	}

	allowed := allowedVariableSet(name)
	for _, v := range extractVariables(tmpl.Subject + "\n" + tmpl.Body) {
		if allowed[v] {
			continue
		}
		if suggestion := suggestVariable(v, allowed); suggestion != "" {
			return warnings, fmt.Errorf("unknown variable: {%s}. Did you mean {%s}?", v, suggestion)
		}
		return warnings, fmt.Errorf("unknown variable: {%s}", v)
	}

	for _, v := range extractVariables(tmpl.Subject + "\n" + tmpl.Body) {
		if replacement, ok := deprecatedVariables[v]; ok {
			warnings = append(warnings, fmt.Sprintf("Deprecated variable {%s}, use {%s} instead", v, replacement))
		}
	}

	if len(tmpl.Subject) > 78 {
		warnings = append(warnings, fmt.Sprintf("Subject line is very long (%d characters, recommended max 78)", len(tmpl.Subject)))
	}

	warnings = append(warnings, recommendedSectionWarnings(name, tmpl)...)

	return warnings, nil
}

// deprecatedVariables maps a retired variable name to its replacement.
// A template still referencing one of these validates successfully but
// produces a non-blocking warning (AI.md PART 17 "Template Validation" -
// "Warnings (non-blocking): Using deprecated variables"). The map is
// empty while every documented variable is still current; retiring a
// variable is a matter of adding its entry here.
var deprecatedVariables = map[string]string{}

// recommendedSections lists the variables a template of each kind is
// expected to reference, backing AI.md PART 17's non-blocking "Missing
// recommended sections (e.g., contact info in security alerts)" warning.
var recommendedSections = map[string][]string{
	"security_alert":     {"app_url", "fqdn", "notification_reply_to"},
	"backup_failed":      {"app_url", "fqdn"},
	"ssl_renewal_failed": {"app_url", "fqdn"},
	"scheduler_error":    {"app_url", "fqdn"},
}

// recommendedSectionWarnings reports which recommended variables the
// template omits, so an operator override cannot silently drop the app
// identity or contact link every notification email must carry.
func recommendedSectionWarnings(name string, tmpl *Template) []string {
	wanted := recommendedSections[name]
	if len(wanted) == 0 {
		return nil
	}
	present := make(map[string]bool)
	for _, v := range extractVariables(tmpl.Subject + "\n" + tmpl.Body) {
		present[v] = true
	}
	var warnings []string
	for _, v := range wanted {
		if !present[v] {
			warnings = append(warnings, fmt.Sprintf("Missing recommended section: template does not reference {%s}", v))
		}
	}
	return warnings
}

// SampleVars returns the preview substitution set from AI.md PART 17
// "Sample Data for Preview". Live values (app name, URL, FQDN, reply-to,
// timestamp) come from the notifier's global variables; the remaining
// template-specific variables get representative sample values so an
// operator sees a fully rendered message before saving or sending a test.
func SampleVars(name string, vars GlobalVars) map[string]string {
	out := vars.toMap()
	sample := map[string]string{
		"event":            "Repeated failed authentication attempts",
		"ip":               "192.168.1.100",
		"details":          "5 failed attempts in 60 seconds",
		"filename":         "api_backup_2025-01-15_030000.tar.gz",
		"size":             "24.7 MB",
		"error":            "connection refused",
		"expires_in":       "14 days",
		"expiry_date":      "2025-02-01 00:00:00 UTC",
		"valid_until":      "2025-04-15 00:00:00 UTC",
		"next_retry":       "2025-01-16 03:00:00 UTC",
		"task_name":        "backup_daily",
		"next_run":         "2025-01-16 02:00:00 UTC",
		"current_version":  "1.0.0",
		"new_version":      "1.1.0",
		"previous_version": "1.0.0",
		"channel":          "stable",
	}
	for _, v := range templateVariables[name] {
		if val, ok := sample[v]; ok {
			out[v] = val
		}
	}
	return out
}

// SaveCustomTemplate validates raw and, when it passes, writes it as the
// operator override for the named template under
// {config_dir}/template/email/ (AI.md PART 17 "Template Storage" +
// "Template Validation": templates are validated before saving). The
// returned warnings are non-blocking; a non-nil error means nothing was
// written.
func SaveCustomTemplate(name, configDir string, raw []byte) (warnings []string, err error) {
	if !isBuiltinTemplate(name) {
		return nil, fmt.Errorf("unknown template %q", name)
	}
	warnings, err = ValidateTemplate(name, raw)
	if err != nil {
		return warnings, err
	}
	path := customTemplatePath(configDir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return warnings, fmt.Errorf("failed to create template directory: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return warnings, fmt.Errorf("failed to write custom template %s: %w", path, err)
	}
	return warnings, nil
}

// ResetTemplate removes the operator override for the named template so
// the embedded default takes effect again on the next send (AI.md PART 17
// "Template Storage": "Delete custom file to reset to default"). Removing
// an override that does not exist is not an error.
func ResetTemplate(name, configDir string) error {
	if !isBuiltinTemplate(name) {
		return fmt.Errorf("unknown template %q", name)
	}
	path := customTemplatePath(configDir, name)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove custom template %s: %w", path, err)
	}
	return nil
}

// HasCustomTemplate reports whether an operator override exists for the
// named template, letting callers show which templates are customised.
func HasCustomTemplate(name, configDir string) bool {
	_, err := os.Stat(customTemplatePath(configDir, name))
	return err == nil
}

// isBuiltinTemplate reports whether name is one of the 10 built-in
// notification templates.
func isBuiltinTemplate(name string) bool {
	for _, n := range TemplateNames {
		if n == name {
			return true
		}
	}
	return false
}

// RenderTemplate substitutes "{variable}" tokens in tmpl using vars. A
// token with no matching entry in vars is left as literal text, per
// AI.md PART 17's plain-substitution-only template engine (no functions,
// no i18n calls - callers must translate values before calling this).
func RenderTemplate(tmpl *Template, vars map[string]string) (subject, body string) {
	replace := func(s string) string {
		return varPattern.ReplaceAllStringFunc(s, func(m string) string {
			name := m[1 : len(m)-1]
			if v, ok := vars[name]; ok {
				return v
			}
			return m
		})
	}
	return replace(tmpl.Subject), replace(tmpl.Body)
}

// customTemplatePath returns where an operator override for the named
// template would live (AI.md PART 17 "Template Storage").
func customTemplatePath(configDir, name string) string {
	return filepath.Join(configDir, "template", "email", name+".txt")
}

// LoadTemplate resolves and parses the named template using the two-tier
// lookup from AI.md PART 17 "Template Storage": a custom file under
// {config_dir}/template/email/ wins when present, otherwise the embedded
// binary default is used. Deliberately uncached so edits/deletes of the
// custom file take effect on the very next send with no restart.
func LoadTemplate(name, configDir string) (*Template, error) {
	custom := customTemplatePath(configDir, name)
	if raw, err := os.ReadFile(custom); err == nil {
		return ParseTemplate(name, raw)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to read custom template %s: %w", custom, err)
	}

	raw, err := emailtemplates.Defaults.ReadFile(name + ".txt")
	if err != nil {
		return nil, fmt.Errorf("no embedded default template for %q: %w", name, err)
	}
	return ParseTemplate(name, raw)
}

// nowFunc is overridable in tests so GlobalVars.toMap produces a
// deterministic {timestamp}/{year}.
var nowFunc = time.Now
