package main

import (
	"fmt"
	"os"
	"strings"
)

// colorEnabled holds the resolved --color/config/NO_COLOR state for this
// process, per AI.md PART 8 "NO_COLOR Support".
var colorEnabled = true

// emojiEnabled holds the resolved emoji-output state for this process.
// It starts equal to colorEnabled (disabling color also disables emojis by
// default), but applyEmojiOverride can force it back on independently via
// the `output.emoji: true` config override, per AI.md PART 8. cprintf/
// cprintln strip emoji when this is false.
var emojiEnabled = true

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

// cprintf is fmt.Printf gated by emojiEnabled: emoji are stripped from the
// formatted output when emoji output is disabled (NO_COLOR, --color=no,
// config, or non-TTY auto-detection), per AI.md PART 8.
func cprintf(format string, args ...interface{}) {
	out := fmt.Sprintf(format, args...)
	if !emojiEnabled {
		out = stripEmoji(out)
	}
	fmt.Print(out)
}

// cprintln is fmt.Println gated by emojiEnabled, mirroring cprintf.
func cprintln(args ...interface{}) {
	out := fmt.Sprintln(args...)
	if !emojiEnabled {
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

// applyColorMode resolves --color against a config-file override, NO_COLOR,
// and TTY/TERM auto-detection, per AI.md PART 8 "NO_COLOR Support" priority
// order: CLI flag > config file > NO_COLOR env var > auto-detect. configColor
// is nil when no config has been loaded yet (e.g. the pre-config.Load() call
// in main.go, or CLI-only commands that never load config) — in that case
// the config-file tier is simply skipped, falling through to NO_COLOR/auto-
// detect, and callers re-invoke this once config.Load() succeeds to apply
// the config-file tier. Setting colorEnabled also resets emojiEnabled to
// match, since disabling color disables emojis by default; call
// applyEmojiOverride afterward to apply the `output.emoji: true` override.
func applyColorMode(colorFlag string, configColor *bool) {
	switch strings.ToLower(strings.TrimSpace(colorFlag)) {
	case "yes", "true", "on":
		colorEnabled = true
		emojiEnabled = true
		return
	case "no", "false", "off":
		colorEnabled = false
		emojiEnabled = false
		return
	}

	if configColor != nil {
		colorEnabled = *configColor
		emojiEnabled = *configColor
		return
	}

	if os.Getenv("NO_COLOR") != "" {
		colorEnabled = false
		emojiEnabled = false
		return
	}
	if os.Getenv("TERM") == "dumb" {
		colorEnabled = false
		emojiEnabled = false
		return
	}
	colorEnabled = true
	emojiEnabled = true
}

// applyEmojiOverride applies the `output.emoji: true` config-file override,
// which re-enables emoji output even when NO_COLOR/config disabled color,
// per AI.md PART 8's EmojiEnabled priority order (config override sits above
// NO_COLOR/TERM=dumb for emoji specifically). It never disables emoji beyond
// what applyColorMode already resolved — configEmoji == nil or false is a
// no-op. Callers apply this after applyColorMode once config is available.
func applyEmojiOverride(configEmoji *bool) {
	if configEmoji != nil && *configEmoji {
		emojiEnabled = true
	}
}
