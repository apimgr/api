package display

type DisplayMode int

const (
	DisplayModeHeadless DisplayMode = iota
	DisplayModeCLI
	DisplayModeTUI
	DisplayModeGUI
)

type DisplayEnv struct {
	Mode         DisplayMode
	HasDisplay   bool
	DisplayType  string
	IsTerminal   bool
	IsSSH        bool
	IsMosh       bool
	IsScreen     bool
	TerminalType string
	Cols         int
	Rows         int
}

func (e *DisplayEnv) autoDetectDisplayMode() DisplayMode {
	if !e.IsTerminal && !e.HasDisplay {
		return DisplayModeHeadless
	}
	if e.IsDumbTerminal() {
		return DisplayModeCLI
	}
	if e.HasDisplay && !e.IsSSH && !e.IsMosh {
		return DisplayModeGUI
	}
	if e.IsTerminal {
		return DisplayModeTUI
	}
	return DisplayModeCLI
}

func (e *DisplayEnv) IsDumbTerminal() bool { return e.TerminalType == "dumb" }

func (e DisplayEnv) IsAutoDetectDisplayModeGUI() bool      { return e.Mode == DisplayModeGUI }
func (e DisplayEnv) IsAutoDetectDisplayModeTUI() bool      { return e.Mode == DisplayModeTUI }
func (e DisplayEnv) IsAutoDetectDisplayModeCLI() bool      { return e.Mode == DisplayModeCLI }
func (e DisplayEnv) IsAutoDetectDisplayModeHeadless() bool { return e.Mode == DisplayModeHeadless }
