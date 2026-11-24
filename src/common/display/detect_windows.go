//go:build windows

package display

import (
	"os"

	"golang.org/x/sys/windows"
)

func (e *DisplayEnv) detectPlatformDisplay() {
	var sessionID uint32
	if err := windows.ProcessIdToSessionId(windows.GetCurrentProcessId(), &sessionID); err != nil || sessionID == 0 {
		e.HasDisplay, e.DisplayType = false, "none"
		return
	}
	if os.Getenv("SESSIONNAME") != "" {
		e.HasDisplay, e.DisplayType = true, "windows-rdp"
		return
	}
	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleWindow")
	if err := proc.Find(); err != nil {
		e.HasDisplay, e.DisplayType = false, "none"
		return
	}
	hwnd, _, _ := proc.Call()
	e.HasDisplay = hwnd != 0
	if e.HasDisplay {
		e.DisplayType = "windows"
	} else {
		e.DisplayType = "none"
	}
}
