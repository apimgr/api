// Package terminal detects terminal dimensions and classifies them into a
// SizeMode breakpoint, per AI.md PART 7 (Terminal Package) and PART 32
// (TUI window-awareness requirements).
package terminal

import (
	"os"

	"golang.org/x/term"
)

// SizeMode classifies terminal dimensions into a breakpoint used to choose
// TUI layout density, per AI.md PART 7.
type SizeMode int

const (
	// SizeModeMicro is <40 cols or <10 rows.
	SizeModeMicro SizeMode = iota
	// SizeModeMinimal is 40-59 cols or 10-15 rows.
	SizeModeMinimal
	// SizeModeCompact is 60-79 cols or 16-23 rows.
	SizeModeCompact
	// SizeModeStandard is 80-119 cols and 24-39 rows.
	SizeModeStandard
	// SizeModeWide is 120-199 cols and 40-59 rows.
	SizeModeWide
	// SizeModeUltrawide is 200-399 cols and 60-79 rows.
	SizeModeUltrawide
	// SizeModeMassive is 400+ cols and 80+ rows.
	SizeModeMassive
)

// TerminalSize is the detected terminal dimensions and derived SizeMode.
type TerminalSize struct {
	Cols int
	Rows int
	Mode SizeMode
}

// GetTerminalSize detects the current stdout terminal size and classifies
// it into a SizeMode. Falls back to 80x24 when size cannot be determined
// (e.g. not a terminal).
func GetTerminalSize() TerminalSize {
	cols, rows, _ := term.GetSize(int(os.Stdout.Fd()))
	if cols == 0 {
		cols = 80
	}
	if rows == 0 {
		rows = 24
	}

	return TerminalSize{
		Cols: cols,
		Rows: rows,
		Mode: calculateMode(cols, rows),
	}
}

// SizeModeForDimensions classifies explicit dimensions into a SizeMode
// without querying the OS. Used by TUI window-resize handlers (e.g.
// bubbletea's tea.WindowSizeMsg) that already carry cols/rows.
func SizeModeForDimensions(cols, rows int) SizeMode {
	return calculateMode(cols, rows)
}

func calculateMode(cols, rows int) SizeMode {
	switch {
	case cols < 40 || rows < 10:
		return SizeModeMicro
	case cols < 60 || rows < 16:
		return SizeModeMinimal
	case cols < 80 || rows < 24:
		return SizeModeCompact
	case cols < 120 || rows < 40:
		return SizeModeStandard
	case cols < 200 || rows < 60:
		return SizeModeWide
	case cols < 400 || rows < 80:
		return SizeModeUltrawide
	default:
		return SizeModeMassive
	}
}

// ShowASCIIArt reports whether the mode is wide/tall enough for ASCII art.
func (s SizeMode) ShowASCIIArt() bool { return s >= SizeModeStandard }

// ShowBorders reports whether the mode is wide/tall enough for borders.
func (s SizeMode) ShowBorders() bool { return s >= SizeModeCompact }

// ShowSidebar reports whether the mode is wide/tall enough for a sidebar.
func (s SizeMode) ShowSidebar() bool { return s >= SizeModeWide }

// ShowIcons reports whether the mode is wide/tall enough for icons.
func (s SizeMode) ShowIcons() bool { return s >= SizeModeMinimal }

// String renders a human-readable breakpoint name.
func (s SizeMode) String() string {
	switch s {
	case SizeModeMicro:
		return "micro"
	case SizeModeMinimal:
		return "minimal"
	case SizeModeCompact:
		return "compact"
	case SizeModeStandard:
		return "standard"
	case SizeModeWide:
		return "wide"
	case SizeModeUltrawide:
		return "ultrawide"
	case SizeModeMassive:
		return "massive"
	default:
		return "unknown"
	}
}
