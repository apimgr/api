package terminal

import "github.com/apimgr/api/src/common/display"

// Symbols contains status and table glyphs selected for terminal capability.
type Symbols struct {
	OK          string
	Error       string
	Warning     string
	Info        string
	Horizontal  string
	Vertical    string
	TopLeft     string
	TopRight    string
	BottomLeft  string
	BottomRight string
}

// GetSymbols chooses plain ASCII fallbacks for terminals without Unicode or
// emoji support and Unicode glyphs for capable terminals.
func GetSymbols(env display.DisplayEnv) Symbols {
	if env.IsDumbTerminal() {
		return Symbols{
			OK: "[OK]", Error: "[ERROR]", Warning: "[WARN]", Info: "[INFO]",
			Horizontal: "-", Vertical: "|", TopLeft: "+", TopRight: "+", BottomLeft: "+", BottomRight: "+",
		}
	}
	return Symbols{
		OK: "✓", Error: "✗", Warning: "⚠", Info: "ℹ",
		Horizontal: "─", Vertical: "│", TopLeft: "┌", TopRight: "┐", BottomLeft: "└", BottomRight: "┘",
	}
}
