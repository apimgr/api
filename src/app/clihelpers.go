package app

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
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

// statusColorCode maps the well-known status emoji this binary's CLI output
// leads lines with to its standard ANSI SGR color code, used to colorize
// cprintf/cprintln output when colorEnabled, per AI.md PART 8 "NO_COLOR
// Support".
var statusColorCode = map[string]string{
	"✅":  "32", // green: success
	"❌":  "31", // red: error/failure
	"⚠️": "33", // yellow: warning
	"🛑":  "31", // red: shutdown/stop
	"ℹ️": "36", // cyan: info
}

// colorCodeFor returns the ANSI SGR code matching s's leading status emoji
// (ignoring leading whitespace/newlines), or "" if colorEnabled is false or
// s does not start with a recognized status emoji.
func colorCodeFor(s string) string {
	if !colorEnabled {
		return ""
	}
	trimmed := strings.TrimLeft(s, "\n \t")
	for emoji, code := range statusColorCode {
		if strings.HasPrefix(trimmed, emoji) {
			return code
		}
	}
	return ""
}

// cprintf is fmt.Printf gated by colorEnabled/emojiEnabled: the output is
// wrapped in the ANSI color matching its leading status emoji when color
// output is enabled, and emoji are stripped from the formatted output when
// emoji output is disabled (NO_COLOR, --color=no, config, or non-TTY
// auto-detection), per AI.md PART 8.
func cprintf(format string, args ...interface{}) {
	out := fmt.Sprintf(format, args...)
	code := colorCodeFor(out)
	if !emojiEnabled {
		out = stripEmoji(out)
	}
	if code != "" {
		out = "\033[" + code + "m" + out + "\033[0m"
	}
	fmt.Print(out)
}

// cprintln is fmt.Println gated by colorEnabled/emojiEnabled, mirroring cprintf.
func cprintln(args ...interface{}) {
	out := fmt.Sprintln(args...)
	code := colorCodeFor(out)
	if !emojiEnabled {
		trimmed := stripEmoji(strings.TrimRight(out, "\n"))
		out = trimmed + "\n"
	}
	if code != "" {
		trimmed := strings.TrimRight(out, "\n")
		out = "\033[" + code + "m" + trimmed + "\033[0m\n"
	}
	fmt.Print(out)
}

// applyColorMode resolves --color against a config-file override, NO_COLOR,
// and TTY/TERM auto-detection (term.IsTerminal(os.Stdout.Fd()) plus
// TERM=dumb, per AI.md PART 8's ColorEnabled pseudocode), per AI.md PART 8
// "NO_COLOR Support" priority order: CLI flag > config file > NO_COLOR env
// var > auto-detect. configColor
// is nil when the caller wants to resolve only CLI/NO_COLOR/auto-detection
// before applying the config-file tier. Run reapplies this after resolving
// command-line options. Setting colorEnabled also resets emojiEnabled to
// match, since disabling color disables emojis by default; call
// applyEmojiOverride afterward to apply the `output.emoji: true` override.
func applyColorMode(colorFlag string, configColor *bool) {
	switch strings.ToLower(strings.TrimSpace(colorFlag)) {
	case "yes", "true", "on":
		colorEnabled = true
		emojiEnabled = defaultEmojiEnabled()
		return
	case "no", "false", "off":
		colorEnabled = false
		emojiEnabled = false
		return
	}

	if configColor != nil {
		colorEnabled = *configColor
		emojiEnabled = *configColor && defaultEmojiEnabled()
		return
	}

	if os.Getenv("NO_COLOR") != "" {
		colorEnabled = false
		emojiEnabled = false
		return
	}
	if !term.IsTerminal(int(os.Stdout.Fd())) {
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
	emojiEnabled = defaultEmojiEnabled()
}

func defaultEmojiEnabled() bool {
	return emojiOutputEnabled(term.IsTerminal(int(os.Stdout.Fd())), os.Getenv("NO_COLOR"), os.Getenv("TERM"))
}

func emojiOutputEnabled(terminal bool, noColor, terminalType string) bool {
	return terminal && noColor == "" && terminalType != "dumb"
}

// applyEmojiOverride applies the `output.emoji: true` config-file override
// above NO_COLOR/config color settings, while preserving PART 7's unconditional
// no-emoji rule for TERM=dumb. A nil or false setting is a no-op.
func applyEmojiOverride(configEmoji *bool) {
	if configEmoji != nil && *configEmoji && os.Getenv("TERM") != "dumb" {
		emojiEnabled = true
	}
}
