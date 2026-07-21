package main

import (
	"fmt"
	"os"
	"strings"
)

// colorEnabled holds the resolved --color/NO_COLOR state for this process.
// Per AI.md PART 8 "NO_COLOR Support", disabling color also disables emojis
// in terminal output, so cprintf/cprintln (below) strip emoji when false.
var colorEnabled = true

// isEmojiRune reports whether r falls in a Unicode block commonly used for
// emoji in this codebase's CLI output (pictographs, symbols, dingbats,
// transport symbols, and variation selectors/ZWJ used to compose them).
func isEmojiRune(r rune) bool {
	switch {
	case r >= 0x1F300 && r <= 0x1FAFF: // misc symbols/pictographs, supplemental symbols
		return true
	case r >= 0x2600 && r <= 0x27BF: // misc symbols, dingbats
		return true
	case r >= 0x2190 && r <= 0x21FF: // arrows (used by some status glyphs)
		return true
	case r == 0xFE0F || r == 0x200D: // variation selector-16, zero-width joiner
		return true
	default:
		return false
	}
}

// stripEmoji removes emoji runes from s and collapses the resulting extra
// whitespace, used when color/emoji output is disabled.
func stripEmoji(s string) string {
	var b strings.Builder
	for _, r := range s {
		if isEmojiRune(r) {
			continue
		}
		b.WriteRune(r)
	}
	fields := strings.Fields(b.String())
	return strings.Join(fields, " ")
}

// cprintf is fmt.Printf gated by colorEnabled: emoji are stripped from the
// formatted output when color/emoji output is disabled (NO_COLOR, --color=no,
// or non-TTY auto-detection), per AI.md PART 8.
func cprintf(format string, args ...interface{}) {
	out := fmt.Sprintf(format, args...)
	if !colorEnabled {
		out = stripEmoji(out)
	}
	fmt.Print(out)
}

// cprintln is fmt.Println gated by colorEnabled, mirroring cprintf.
func cprintln(args ...interface{}) {
	out := fmt.Sprintln(args...)
	if !colorEnabled {
		trimmed := stripEmoji(strings.TrimRight(out, "\n"))
		out = trimmed + "\n"
	}
	fmt.Print(out)
}

// envOrFlag resolves a CLI flag value against its environment variable
// fallback, per AI.md PART 8 "Environment Variable Fallbacks": an
// explicitly set CLI flag always wins, otherwise the env var is used, and
// otherwise the empty string is returned so the caller can apply its own
// default.
func envOrFlag(flagValue, envKey string) string {
	if flagValue != "" {
		return flagValue
	}
	return os.Getenv(envKey)
}

// applyColorMode resolves --color against NO_COLOR and TTY/TERM
// auto-detection, per AI.md PART 8 "NO_COLOR Support" priority order:
// CLI flag > NO_COLOR env var > auto-detect. Config-file overrides do not
// apply here (server has no `output.color` config key).
func applyColorMode(colorFlag string) {
	switch strings.ToLower(strings.TrimSpace(colorFlag)) {
	case "yes", "true", "on":
		colorEnabled = true
		return
	case "no", "false", "off":
		colorEnabled = false
		return
	}

	if os.Getenv("NO_COLOR") != "" {
		colorEnabled = false
		return
	}
	if os.Getenv("TERM") == "dumb" {
		colorEnabled = false
		return
	}
	colorEnabled = true
}
