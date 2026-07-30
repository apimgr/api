package terminal

import "testing"

func TestSizeModeForDimensions(t *testing.T) {
	tests := []struct {
		name string
		cols int
		rows int
		want SizeMode
	}{
		{"micro cols", 39, 24, SizeModeMicro},
		{"micro rows", 80, 9, SizeModeMicro},
		{"minimal", 50, 24, SizeModeMinimal},
		{"minimal rows", 80, 12, SizeModeMinimal},
		{"compact", 70, 24, SizeModeCompact},
		{"compact rows", 80, 20, SizeModeCompact},
		{"standard", 80, 24, SizeModeStandard},
		{"standard upper", 119, 39, SizeModeStandard},
		{"wide", 120, 40, SizeModeWide},
		{"wide upper", 199, 59, SizeModeWide},
		{"ultrawide", 200, 60, SizeModeUltrawide},
		{"ultrawide upper", 399, 79, SizeModeUltrawide},
		{"massive", 400, 80, SizeModeMassive},
		{"massive huge", 1000, 200, SizeModeMassive},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SizeModeForDimensions(tt.cols, tt.rows)
			if got != tt.want {
				t.Errorf("SizeModeForDimensions(%d, %d) = %v, want %v", tt.cols, tt.rows, got, tt.want)
			}
		})
	}
}

func TestSizeModeHelpers(t *testing.T) {
	if SizeModeMicro.ShowASCIIArt() {
		t.Error("SizeModeMicro.ShowASCIIArt() = true, want false")
	}
	if !SizeModeStandard.ShowASCIIArt() {
		t.Error("SizeModeStandard.ShowASCIIArt() = false, want true")
	}

	if SizeModeMinimal.ShowBorders() {
		t.Error("SizeModeMinimal.ShowBorders() = true, want false")
	}
	if !SizeModeCompact.ShowBorders() {
		t.Error("SizeModeCompact.ShowBorders() = false, want true")
	}

	if SizeModeStandard.ShowSidebar() {
		t.Error("SizeModeStandard.ShowSidebar() = true, want false")
	}
	if !SizeModeWide.ShowSidebar() {
		t.Error("SizeModeWide.ShowSidebar() = false, want true")
	}

	if SizeModeMicro.ShowIcons() {
		t.Error("SizeModeMicro.ShowIcons() = true, want false")
	}
	if !SizeModeMinimal.ShowIcons() {
		t.Error("SizeModeMinimal.ShowIcons() = false, want true")
	}
}

func TestSizeModeString(t *testing.T) {
	tests := map[SizeMode]string{
		SizeModeMicro:     "micro",
		SizeModeMinimal:   "minimal",
		SizeModeCompact:   "compact",
		SizeModeStandard:  "standard",
		SizeModeWide:      "wide",
		SizeModeUltrawide: "ultrawide",
		SizeModeMassive:   "massive",
		SizeMode(99):      "unknown",
	}
	for mode, want := range tests {
		if got := mode.String(); got != want {
			t.Errorf("SizeMode(%d).String() = %q, want %q", mode, got, want)
		}
	}
}

func TestGetTerminalSize(t *testing.T) {
	// Not a terminal in test environment, so GetTerminalSize should fall
	// back to the documented 80x24 default rather than 0x0.
	size := GetTerminalSize()
	if size.Cols <= 0 || size.Rows <= 0 {
		t.Errorf("GetTerminalSize() = %+v, want positive Cols/Rows", size)
	}
}
