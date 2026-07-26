package generate

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Config renders a configuration-file scaffold in the requested format
// (yaml, json, env, toml) from a set of key=value pairs.
func (s *Service) Config(format string, values map[string]string) (string, error) {
	if len(values) == 0 {
		return "", fmt.Errorf("at least one key=value pair is required")
	}

	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	switch strings.ToLower(strings.TrimSpace(format)) {
	case "yaml", "":
		var b strings.Builder
		for _, k := range keys {
			b.WriteString(fmt.Sprintf("%s: %s\n", k, configScalar(values[k])))
		}
		return b.String(), nil
	case "json":
		var b strings.Builder
		b.WriteString("{\n")
		for i, k := range keys {
			comma := ","
			if i == len(keys)-1 {
				comma = ""
			}
			b.WriteString(fmt.Sprintf("  %q: %s%s\n", k, configJSONScalar(values[k]), comma))
		}
		b.WriteString("}\n")
		return b.String(), nil
	case "env":
		var b strings.Builder
		for _, k := range keys {
			envKey := strings.ToUpper(strings.ReplaceAll(k, "-", "_"))
			b.WriteString(fmt.Sprintf("%s=%s\n", envKey, values[k]))
		}
		return b.String(), nil
	case "toml":
		var b strings.Builder
		for _, k := range keys {
			b.WriteString(fmt.Sprintf("%s = %s\n", k, configJSONScalar(values[k])))
		}
		return b.String(), nil
	default:
		return "", fmt.Errorf("unsupported config format %q: supported formats are yaml, json, env, toml", format)
	}
}

// configScalar renders a value for YAML, quoting it only when necessary.
func configScalar(v string) string {
	if v == "" {
		return `""`
	}
	if _, err := strconv.ParseFloat(v, 64); err == nil {
		return v
	}
	if v == "true" || v == "false" {
		return v
	}
	return v
}

// configJSONScalar renders a value as a JSON/TOML scalar: numbers and
// booleans unquoted, everything else as a quoted string.
func configJSONScalar(v string) string {
	if v == "" {
		return `""`
	}
	if _, err := strconv.ParseFloat(v, 64); err == nil {
		return v
	}
	if v == "true" || v == "false" {
		return v
	}
	return strconv.Quote(v)
}
