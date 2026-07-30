package tui

import "github.com/apimgr/api/src/common/terminal"

// LayoutConfig provides TUI-specific layout settings based on SizeMode, per
// AI.md PART 32 (TUI Responsive Layout).
type LayoutConfig struct {
	ShowBorders    bool
	ShowHeader     bool
	ShowFooter     bool
	ShowSidebar    bool
	SidebarWidth   int
	MaxColumns     int
	TruncateAt     int
	UseAbbrev      bool
	VerticalScroll bool
	MultiPane      bool
	TileLayout     bool
}

// GetLayoutConfig returns the layout config for a SizeMode, per AI.md
// PART 32's canonical breakpoint table.
func GetLayoutConfig(mode terminal.SizeMode) LayoutConfig {
	configs := map[terminal.SizeMode]LayoutConfig{
		terminal.SizeModeMicro: {
			ShowBorders:    false,
			ShowHeader:     false,
			ShowFooter:     false,
			ShowSidebar:    false,
			MaxColumns:     2,
			TruncateAt:     30,
			UseAbbrev:      true,
			VerticalScroll: true,
		},
		terminal.SizeModeMinimal: {
			ShowBorders:    false,
			ShowHeader:     true,
			ShowFooter:     true,
			ShowSidebar:    false,
			MaxColumns:     3,
			TruncateAt:     40,
			UseAbbrev:      true,
			VerticalScroll: true,
		},
		terminal.SizeModeCompact: {
			ShowBorders:    true,
			ShowHeader:     true,
			ShowFooter:     true,
			ShowSidebar:    false,
			MaxColumns:     4,
			TruncateAt:     60,
			UseAbbrev:      false,
			VerticalScroll: true,
		},
		terminal.SizeModeStandard: {
			ShowBorders:    true,
			ShowHeader:     true,
			ShowFooter:     true,
			ShowSidebar:    false,
			MaxColumns:     6,
			TruncateAt:     80,
			UseAbbrev:      false,
			VerticalScroll: true,
		},
		terminal.SizeModeWide: {
			ShowBorders:    true,
			ShowHeader:     true,
			ShowFooter:     true,
			ShowSidebar:    true,
			SidebarWidth:   30,
			MaxColumns:     8,
			TruncateAt:     120,
			UseAbbrev:      false,
			VerticalScroll: true,
		},
		terminal.SizeModeUltrawide: {
			ShowBorders:    true,
			ShowHeader:     true,
			ShowFooter:     true,
			ShowSidebar:    true,
			SidebarWidth:   40,
			MaxColumns:     12,
			TruncateAt:     200,
			UseAbbrev:      false,
			VerticalScroll: false,
			MultiPane:      true,
		},
		terminal.SizeModeMassive: {
			ShowBorders:    true,
			ShowHeader:     true,
			ShowFooter:     true,
			ShowSidebar:    true,
			SidebarWidth:   50,
			MaxColumns:     20,
			TruncateAt:     0,
			UseAbbrev:      false,
			VerticalScroll: false,
			MultiPane:      true,
			TileLayout:     true,
		},
	}
	return configs[mode]
}

// Spacing units, per AI.md PART 32 (Spacing and Alignment).
const (
	// SpaceXS is micro spacing.
	SpaceXS = 1
	// SpaceS is small spacing.
	SpaceS = 2
	// SpaceM is medium spacing.
	SpaceM = 4
	// SpaceL is large spacing.
	SpaceL = 6
	// SpaceXL is extra large spacing.
	SpaceXL = 8
)

// GetSpacingForMode applies spacing based on terminal size mode, per AI.md
// PART 32.
func GetSpacingForMode(m terminal.SizeMode) int {
	switch m {
	case terminal.SizeModeMicro, terminal.SizeModeMinimal:
		return SpaceXS
	case terminal.SizeModeCompact:
		return SpaceS
	case terminal.SizeModeStandard:
		return SpaceM
	case terminal.SizeModeWide:
		return SpaceL
	// Ultrawide, Massive
	default:
		return SpaceXL
	}
}
